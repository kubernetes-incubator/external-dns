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
	"context"
	"fmt"
	"sort"
	"strings"
	"text/template"
	"time"

	log "github.com/sirupsen/logrus"
	"k8s.io/api/extensions/v1beta1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/util/wait"
	kubeinformers "k8s.io/client-go/informers"
	extinformers "k8s.io/client-go/informers/extensions/v1beta1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"

	"sigs.k8s.io/external-dns/endpoint"
)

const (
	// ALBDualstackAnnotationKey is the annotation used for determining if an ALB ingress is dualstack
	ALBDualstackAnnotationKey = "alb.ingress.kubernetes.io/ip-address-type"
	// ALBDualstackAnnotationValue is the value of the ALB dualstack annotation that indicates it is dualstack
	ALBDualstackAnnotationValue = "dualstack"

	// Possible values for the ingress-hostname-source annotation
	IngressHostnameSourceAnnotationOnlyValue   = "annotation-only"
	IngressHostnameSourceDefinedHostsOnlyValue = "defined-hosts-only"
)

// ingressSource is an implementation of Source for Kubernetes ingress objects.
// Ingress implementation will use the spec.rules.host value for the hostname
// Use targetAnnotationKey to explicitly set Endpoint. (useful if the ingress
// controller does not update, or to override with alternative endpoint)
type ingressSource struct {
	client                   kubernetes.Interface
	namespace                string
	annotationFilter         string
	fqdnTemplate             *template.Template
	combineFQDNAnnotation    bool
	ignoreHostnameAnnotation bool
	ingressInformer          extinformers.IngressInformer
	ignoreIngressTLSSpec     bool
}

// NewIngressSource creates a new ingressSource with the given config.
func NewIngressSource(kubeClient kubernetes.Interface, namespace, annotationFilter string, fqdnTemplate string, combineFqdnAnnotation bool, ignoreHostnameAnnotation bool, ignoreIngressTLSSpec bool) (Source, error) {
	var (
		tmpl *template.Template
		err  error
	)
	if fqdnTemplate != "" {
		tmpl, err = template.New("endpoint").Funcs(template.FuncMap{
			"trimPrefix": strings.TrimPrefix,
		}).Parse(fqdnTemplate)
		if err != nil {
			return nil, err
		}
	}

	// Use shared informer to listen for add/update/delete of ingresses in the specified namespace.
	// Set resync period to 0, to prevent processing when nothing has changed.
	informerFactory := kubeinformers.NewSharedInformerFactoryWithOptions(kubeClient, 0, kubeinformers.WithNamespace(namespace))
	ingressInformer := informerFactory.Extensions().V1beta1().Ingresses()

	// Add default resource event handlers to properly initialize informer.
	ingressInformer.Informer().AddEventHandler(
		cache.ResourceEventHandlerFuncs{
			AddFunc: func(obj interface{}) {
			},
		},
	)

	// TODO informer is not explicitly stopped since controller is not passing in its channel.
	informerFactory.Start(wait.NeverStop)

	// wait for the local cache to be populated.
	err = poll(time.Second, 60*time.Second, func() (bool, error) {
		return ingressInformer.Informer().HasSynced(), nil
	})
	if err != nil {
		return nil, fmt.Errorf("failed to sync cache: %v", err)
	}

	sc := &ingressSource{
		client:                   kubeClient,
		namespace:                namespace,
		annotationFilter:         annotationFilter,
		fqdnTemplate:             tmpl,
		combineFQDNAnnotation:    combineFqdnAnnotation,
		ignoreHostnameAnnotation: ignoreHostnameAnnotation,
		ingressInformer:          ingressInformer,
		ignoreIngressTLSSpec:     ignoreIngressTLSSpec,
	}
	return sc, nil
}

// Endpoints returns endpoint objects for each host-target combination that should be processed.
// Retrieves all ingress resources on all namespaces
func (sc *ingressSource) Endpoints(ctx context.Context) ([]*endpoint.Endpoint, error) {
	ingresses, err := sc.ingressInformer.Lister().Ingresses(sc.namespace).List(labels.Everything())
	if err != nil {
		return nil, err
	}
	ingresses, err = sc.filterByAnnotations(ingresses)
	if err != nil {
		return nil, err
	}

	endpoints := []*endpoint.Endpoint{}

	for _, ing := range ingresses {
		// Check controller annotation to see if we are responsible.
		controller, ok := ing.Annotations[controllerAnnotationKey]
		if ok && controller != controllerAnnotationValue {
			log.Debugf("Skipping ingress %s/%s because controller value does not match, found: %s, required: %s",
				ing.Namespace, ing.Name, controller, controllerAnnotationValue)
			continue
		}

		ingEndpoints := endpointsFromIngress(ing, sc.ignoreHostnameAnnotation, sc.ignoreIngressTLSSpec)

		// apply template if host is missing on ingress
		if (sc.combineFQDNAnnotation || len(ingEndpoints) == 0) && sc.fqdnTemplate != nil {
			iEndpoints, err := sc.endpointsFromTemplate(ing)
			if err != nil {
				return nil, err
			}

			if sc.combineFQDNAnnotation {
				ingEndpoints = append(ingEndpoints, iEndpoints...)
			} else {
				ingEndpoints = iEndpoints
			}
		}

		if len(ingEndpoints) == 0 {
			log.Debugf("No endpoints could be generated from ingress %s/%s", ing.Namespace, ing.Name)
			continue
		}

		log.Debugf("Endpoints generated from ingress: %s/%s: %v", ing.Namespace, ing.Name, ingEndpoints)
		sc.setResourceLabel(ing, ingEndpoints)
		sc.setDualstackLabel(ing, ingEndpoints)
		addClaimLabels(ing.Annotations, ingEndpoints)
		endpoints = append(endpoints, ingEndpoints...)
	}

	for _, ep := range endpoints {
		sort.Sort(ep.Targets)
	}

	return endpoints, nil
}

func (sc *ingressSource) endpointsFromTemplate(ing *v1beta1.Ingress) ([]*endpoint.Endpoint, error) {
	// Process the whole template string
	var buf bytes.Buffer
	err := sc.fqdnTemplate.Execute(&buf, ing)
	if err != nil {
		return nil, fmt.Errorf("failed to apply template on ingress %s: %v", ing.String(), err)
	}

	hostnames := buf.String()

	ttl, err := getTTLFromAnnotations(ing.Annotations)
	if err != nil {
		log.Warn(err)
	}

	targets := getTargetsFromTargetAnnotation(ing.Annotations)

	if len(targets) == 0 {
		targets = targetsFromIngressStatus(ing.Status)
	}

	providerSpecific, setIdentifier := getProviderSpecificAnnotations(ing.Annotations)

	var endpoints []*endpoint.Endpoint
	// splits the FQDN template and removes the trailing periods
	hostnameList := strings.Split(strings.Replace(hostnames, " ", "", -1), ",")
	for _, hostname := range hostnameList {
		hostname = strings.TrimSuffix(hostname, ".")
		endpoints = append(endpoints, endpointsForHostname(hostname, targets, ttl, providerSpecific, setIdentifier)...)
	}
	return endpoints, nil
}

// filterByAnnotations filters a list of ingresses by a given annotation selector.
func (sc *ingressSource) filterByAnnotations(ingresses []*v1beta1.Ingress) ([]*v1beta1.Ingress, error) {
	selector, err := getLabelSelector(sc.annotationFilter)
	if err != nil {
		return nil, err
	}

	// empty filter returns original list
	if selector.Empty() {
		return ingresses, nil
	}

	filteredList := []*v1beta1.Ingress{}

	for _, ingress := range ingresses {
		// include ingress if its annotations match the selector
		if matchLabelSelector(selector, ingress.Annotations) {
			filteredList = append(filteredList, ingress)
		}
	}

	return filteredList, nil
}

func (sc *ingressSource) setResourceLabel(ingress *v1beta1.Ingress, endpoints []*endpoint.Endpoint) {
	for _, ep := range endpoints {
		ep.Labels[endpoint.ResourceLabelKey] = fmt.Sprintf("ingress/%s/%s", ingress.Namespace, ingress.Name)
	}
}

func (sc *ingressSource) setDualstackLabel(ingress *v1beta1.Ingress, endpoints []*endpoint.Endpoint) {
	val, ok := ingress.Annotations[ALBDualstackAnnotationKey]
	if ok && val == ALBDualstackAnnotationValue {
		log.Debugf("Adding dualstack label to ingress %s/%s.", ingress.Namespace, ingress.Name)
		for _, ep := range endpoints {
			ep.Labels[endpoint.DualstackLabelKey] = "true"
		}
	}
}

// endpointsFromIngress extracts the endpoints from ingress object
func endpointsFromIngress(ing *v1beta1.Ingress, ignoreHostnameAnnotation bool, ignoreIngressTLSSpec bool) []*endpoint.Endpoint {
	ttl, err := getTTLFromAnnotations(ing.Annotations)
	if err != nil {
		log.Warn(err)
	}

	targets := getTargetsFromTargetAnnotation(ing.Annotations)

	if len(targets) == 0 {
		targets = targetsFromIngressStatus(ing.Status)
	}

	providerSpecific, setIdentifier := getProviderSpecificAnnotations(ing.Annotations)

	// Gather endpoints defined on hosts sections of the ingress
	var definedHostsEndpoints []*endpoint.Endpoint
	for _, rule := range ing.Spec.Rules {
		if rule.Host == "" {
			continue
		}
		definedHostsEndpoints = append(definedHostsEndpoints, endpointsForHostname(rule.Host, targets, ttl, providerSpecific, setIdentifier)...)
	}

	// Skip endpoints if we do not want entries from tls spec section
	if !ignoreIngressTLSSpec {
		for _, tls := range ing.Spec.TLS {
			for _, host := range tls.Hosts {
				if host == "" {
					continue
				}
				definedHostsEndpoints = append(definedHostsEndpoints, endpointsForHostname(host, targets, ttl, providerSpecific, setIdentifier)...)
			}
		}
	}

	// Gather endpoints defined on annotations in the ingress
	var annotationEndpoints []*endpoint.Endpoint
	if !ignoreHostnameAnnotation {
		for _, hostname := range getHostnamesFromAnnotations(ing.Annotations) {
			annotationEndpoints = append(annotationEndpoints, endpointsForHostname(hostname, targets, ttl, providerSpecific, setIdentifier)...)
		}
	}

	// Determine which hostnames to consider in our final list
	hostnameSourceAnnotation, hostnameSourceAnnotationExists := ing.Annotations[ingressHostnameSourceKey]
	if !hostnameSourceAnnotationExists {
		return append(definedHostsEndpoints, annotationEndpoints...)
	}

	// Include endpoints according to the hostname source annotation in our final list
	var endpoints []*endpoint.Endpoint
	if strings.ToLower(hostnameSourceAnnotation) == IngressHostnameSourceDefinedHostsOnlyValue {
		endpoints = append(endpoints, definedHostsEndpoints...)
	}
	if strings.ToLower(hostnameSourceAnnotation) == IngressHostnameSourceAnnotationOnlyValue {
		endpoints = append(endpoints, annotationEndpoints...)
	}
	return endpoints
}

func targetsFromIngressStatus(status v1beta1.IngressStatus) endpoint.Targets {
	var targets endpoint.Targets

	for _, lb := range status.LoadBalancer.Ingress {
		if lb.IP != "" {
			targets = append(targets, lb.IP)
		}
		if lb.Hostname != "" {
			targets = append(targets, lb.Hostname)
		}
	}

	return targets
}

func (sc *ingressSource) AddEventHandler(ctx context.Context, handler func()) {
	log.Debug("Adding event handler for ingress")

	// Right now there is no way to remove event handler from informer, see:
	// https://github.com/kubernetes/kubernetes/issues/79610
	sc.ingressInformer.Informer().AddEventHandler(
		cache.ResourceEventHandlerFuncs{
			AddFunc: func(obj interface{}) {
				handler()
			},
			UpdateFunc: func(old interface{}, new interface{}) {
				handler()
			},
			DeleteFunc: func(obj interface{}) {
				handler()
			},
		},
	)
}
