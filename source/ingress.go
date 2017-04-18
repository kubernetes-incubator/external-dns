/*
Copyright 2017 The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package source

import (
	"bytes"
	"html/template"
	"strings"

	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/pkg/api/v1"
	"k8s.io/client-go/pkg/apis/extensions/v1beta1"

	"github.com/kubernetes-incubator/external-dns/endpoint"
)

// ingressSource is an implementation of Source for Kubernetes ingress objects.
// Ingress implementation will use the spec.rules.host value for the hostname
// Ingress annotations are ignored
type ingressSource struct {
	client       kubernetes.Interface
	namespace    string
	fqdntemplate string
}

// NewIngressSource creates a new ingressSource with the given client and namespace scope.
func NewIngressSource(client kubernetes.Interface, namespace string, fqdntemplate string) Source {
	return &ingressSource{
		client:       client,
		namespace:    namespace,
		fqdntemplate: fqdntemplate,
	}
}

// Endpoints returns endpoint objects for each host-target combination that should be processed.
// Retrieves all ingress resources on all namespaces
func (sc *ingressSource) Endpoints() ([]*endpoint.Endpoint, error) {
	ingresses, err := sc.client.Extensions().Ingresses(sc.namespace).List(v1.ListOptions{})
	if err != nil {
		return nil, err
	}

	endpoints := []*endpoint.Endpoint{}

	for _, ing := range ingresses.Items {
		ingEndpoints := endpointsFromIngress(&ing, sc.fqdntemplate)
		endpoints = append(endpoints, ingEndpoints...)
	}

	return endpoints, nil
}

// endpointsFromIngress extracts the endpoints from ingress object
func endpointsFromIngress(ing *v1beta1.Ingress, fqdntemplate string) []*endpoint.Endpoint {
	var endpoints []*endpoint.Endpoint

	// Check controller annotation to see if we are responsible.
	controller, exists := ing.Annotations[controllerAnnotationKey]
	if exists && controller != controllerAnnotationValue {
		return endpoints
	}

	if len(ing.Spec.Rules) == 0 && fqdntemplate != "" {
		tmpl, err := template.New("endpoint").Funcs(template.FuncMap{
			"trimPrefix": strings.TrimPrefix,
		}).Parse(fqdntemplate)
		if err != nil {
			return nil
		}

		var buf bytes.Buffer

		tmpl.Execute(&buf, ing)

		for _, i := range ing.Status.LoadBalancer.Ingress {
			if i.IP != "" {
				endpoints = append(endpoints, endpoint.NewEndpoint(buf.String(), i.IP, ""))
			}
			if i.Hostname != "" {
				endpoints = append(endpoints, endpoint.NewEndpoint(buf.String(), i.Hostname, ""))
			}
		}
	}

	for _, rule := range ing.Spec.Rules {
		if rule.Host == "" {
			continue
		}
		for _, lb := range ing.Status.LoadBalancer.Ingress {
			if lb.IP != "" {
				endpoints = append(endpoints, endpoint.NewEndpoint(rule.Host, lb.IP, ""))
			}
			if lb.Hostname != "" {
				endpoints = append(endpoints, endpoint.NewEndpoint(rule.Host, lb.Hostname, ""))
			}
		}
	}

	return endpoints
}
