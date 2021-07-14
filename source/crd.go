/*
Copyright 2018 The Kubernetes Authors.

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
	"context"
	"fmt"
	"os"
	"strings"

	log "github.com/sirupsen/logrus"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"

	"sigs.k8s.io/external-dns/endpoint"
)

// crdSource is an implementation of Source that provides endpoints by listing
// specified CRD and fetching Endpoints embedded in Spec.
type crdSource struct {
	crdClient        rest.Interface
	namespace        string
	crdResource      string
	codec            runtime.ParameterCodec
	annotationFilter string
	labelFilter      string
}

func addKnownTypes(scheme *runtime.Scheme, groupVersion schema.GroupVersion) error {
	scheme.AddKnownTypes(groupVersion,
		&endpoint.DNSEndpoint{},
		&endpoint.DNSEndpointList{},
	)
	metav1.AddToGroupVersion(scheme, groupVersion)
	return nil
}

// NewCRDClientForAPIVersionKind return rest client for the given apiVersion and kind of the CRD
func NewCRDClientForAPIVersionKind(client kubernetes.Interface, kubeConfig, apiServerURL, apiVersion, kind string) (*rest.RESTClient, *runtime.Scheme, error) {
	if kubeConfig == "" {
		if _, err := os.Stat(clientcmd.RecommendedHomeFile); err == nil {
			kubeConfig = clientcmd.RecommendedHomeFile
		}
	}

	config, err := clientcmd.BuildConfigFromFlags(apiServerURL, kubeConfig)
	if err != nil {
		return nil, nil, err
	}

	groupVersion, err := schema.ParseGroupVersion(apiVersion)
	if err != nil {
		return nil, nil, err
	}
	apiResourceList, err := client.Discovery().ServerResourcesForGroupVersion(groupVersion.String())
	if err != nil {
		return nil, nil, fmt.Errorf("error listing resources in GroupVersion %q: %s", groupVersion.String(), err)
	}

	var crdAPIResource *metav1.APIResource
	for _, apiResource := range apiResourceList.APIResources {
		if apiResource.Kind == kind {
			crdAPIResource = &apiResource
			break
		}
	}
	if crdAPIResource == nil {
		return nil, nil, fmt.Errorf("unable to find Resource Kind %q in GroupVersion %q", kind, apiVersion)
	}

	scheme := runtime.NewScheme()
	addKnownTypes(scheme, groupVersion)

	config.ContentConfig.GroupVersion = &groupVersion
	config.APIPath = "/apis"
	config.NegotiatedSerializer = serializer.WithoutConversionCodecFactory{CodecFactory: serializer.NewCodecFactory(scheme)}

	crdClient, err := rest.UnversionedRESTClientFor(config)
	if err != nil {
		return nil, nil, err
	}
	return crdClient, scheme, nil
}

// NewCRDSource creates a new crdSource with the given config.
func NewCRDSource(crdClient rest.Interface, namespace, kind string, annotationFilter string, labelFilter string, scheme *runtime.Scheme) (Source, error) {
	return &crdSource{
		crdResource:      strings.ToLower(kind) + "s",
		namespace:        namespace,
		annotationFilter: annotationFilter,
		labelFilter:      labelFilter,
		crdClient:        crdClient,
		codec:            runtime.NewParameterCodec(scheme),
	}, nil
}

func (cs *crdSource) AddEventHandler(ctx context.Context, handler func()) {
}

// Endpoints returns endpoint objects.
func (cs *crdSource) Endpoints(ctx context.Context) ([]*endpoint.Endpoint, error) {
	endpoints := []*endpoint.Endpoint{}

	var (
		result *endpoint.DNSEndpointList
		err    error
	)

	if cs.labelFilter != "" {
		result, err = cs.List(ctx, &metav1.ListOptions{LabelSelector: cs.labelFilter})
	} else {
		result, err = cs.List(ctx, &metav1.ListOptions{})
	}
	if err != nil {
		return nil, err
	}

	result, err = cs.filterByAnnotations(result)

	if err != nil {
		return nil, err
	}

	for _, dnsEndpoint := range result.Items {
		// Make sure that all endpoints have targets for A or CNAME type
		crdEndpoints := []*endpoint.Endpoint{}
		for _, ep := range dnsEndpoint.Spec.Endpoints {
			if (ep.RecordType == "CNAME" || ep.RecordType == "A" || ep.RecordType == "AAAA" || ep.RecordType == "PTR") && len(ep.Targets) < 1 {
				log.Warnf("Endpoint %s with DNSName %s has an empty list of targets", dnsEndpoint.ObjectMeta.Name, ep.DNSName)
				continue
			}

			illegalTarget := false
			for _, target := range ep.Targets {
				if ep.RecordType == "PTR" {
					// PTR record targets must end with a dot
					if !strings.HasSuffix(target, ".") {
						illegalTarget = true
						break
					}
				} else if strings.HasSuffix(target, ".") {
					illegalTarget = true
					break
				}
			}
			if illegalTarget {
				log.Warnf("Endpoint %s with DNSName %s has an illegal target. The subdomain must consist of lower case alphanumeric characters, '-' or '.', and must start and end with an alphanumeric character (e.g. 'example.com')", dnsEndpoint.ObjectMeta.Name, ep.DNSName)
				continue
			}

			if ep.Labels == nil {
				ep.Labels = endpoint.NewLabels()
			}

			crdEndpoints = append(crdEndpoints, ep)
		}

		cs.setResourceLabel(&dnsEndpoint, crdEndpoints)
		endpoints = append(endpoints, crdEndpoints...)

		if dnsEndpoint.Status.ObservedGeneration == dnsEndpoint.Generation {
			continue
		}

		dnsEndpoint.Status.ObservedGeneration = dnsEndpoint.Generation
		// Update the ObservedGeneration
		_, err = cs.UpdateStatus(ctx, &dnsEndpoint)
		if err != nil {
			log.Warnf("Could not update ObservedGeneration of the CRD: %v", err)
		}
	}

	return endpoints, nil
}

func (cs *crdSource) setResourceLabel(crd *endpoint.DNSEndpoint, endpoints []*endpoint.Endpoint) {
	for _, ep := range endpoints {
		ep.Labels[endpoint.ResourceLabelKey] = fmt.Sprintf("crd/%s/%s", crd.ObjectMeta.Namespace, crd.ObjectMeta.Name)
	}
}

func (cs *crdSource) List(ctx context.Context, opts *metav1.ListOptions) (result *endpoint.DNSEndpointList, err error) {
	result = &endpoint.DNSEndpointList{}
	err = cs.crdClient.Get().
		Namespace(cs.namespace).
		Resource(cs.crdResource).
		VersionedParams(opts, cs.codec).
		Do(ctx).
		Into(result)
	return
}

func (cs *crdSource) UpdateStatus(ctx context.Context, dnsEndpoint *endpoint.DNSEndpoint) (result *endpoint.DNSEndpoint, err error) {
	result = &endpoint.DNSEndpoint{}
	err = cs.crdClient.Put().
		Namespace(dnsEndpoint.Namespace).
		Resource(cs.crdResource).
		Name(dnsEndpoint.Name).
		SubResource("status").
		Body(dnsEndpoint).
		Do(ctx).
		Into(result)
	return
}

// filterByAnnotations filters a list of dnsendpoints by a given annotation selector.
func (cs *crdSource) filterByAnnotations(dnsendpoints *endpoint.DNSEndpointList) (*endpoint.DNSEndpointList, error) {
	labelSelector, err := metav1.ParseToLabelSelector(cs.annotationFilter)
	if err != nil {
		return nil, err
	}
	selector, err := metav1.LabelSelectorAsSelector(labelSelector)
	if err != nil {
		return nil, err
	}

	// empty filter returns original list
	if selector.Empty() {
		return dnsendpoints, nil
	}

	filteredList := endpoint.DNSEndpointList{}

	for _, dnsendpoint := range dnsendpoints.Items {
		// convert the dnsendpoint' annotations to an equivalent label selector
		annotations := labels.Set(dnsendpoint.Annotations)

		// include dnsendpoint if its annotations match the selector
		if selector.Matches(annotations) {
			filteredList.Items = append(filteredList.Items, dnsendpoint)
		}
	}

	return &filteredList, nil
}
