/*
Copyright 2020 The Kubernetes Authors.

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
	"sort"
	"strings"
	"time"

	ambassador "github.com/datawire/ambassador/pkg/api/getambassador.io/v2"
	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/dynamic/dynamicinformer"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/cache"
	api "k8s.io/kubernetes/pkg/apis/core"

	"sigs.k8s.io/external-dns/endpoint"
)

// ambHostAnnotation is the annotation in the Host that maps to a Service
const ambHostAnnotation = "external-dns.ambassador-service"

// groupName is the group name for the Ambassador API
const groupName = "getambassador.io"

var schemeGroupVersion = schema.GroupVersion{Group: groupName, Version: "v2"}

var ambHostGVR = schemeGroupVersion.WithResource("hosts")

// ambassadorHostSource is an implementation of Source for Ambassador Host objects.
// The IngressRoute implementation uses the spec.virtualHost.fqdn value for the hostname.
// Use targetAnnotationKey to explicitly set Endpoint.
type ambassadorHostSource struct {
	dynamicKubeClient      dynamic.Interface
	kubeClient             kubernetes.Interface
	namespace              string
	annotationFilter       string
	ambassadorHostInformer informers.GenericInformer
	unstructuredConverter  *unstructuredConverter
}

// NewAmbassadorHostSource creates a new ambassadorHostSource with the given config.
func NewAmbassadorHostSource(
	dynamicKubeClient dynamic.Interface,
	kubeClient kubernetes.Interface,
	namespace string,
	annotationFilter string) (Source, error) {
	var err error

	// Use shared informer to listen for add/update/delete of Host in the specified namespace.
	// Set resync period to 0, to prevent processing when nothing has changed.
	informerFactory := dynamicinformer.NewFilteredDynamicSharedInformerFactory(dynamicKubeClient, 0, namespace, nil)
	ambassadorHostInformer := informerFactory.ForResource(ambHostGVR)

	// Add default resource event handlers to properly initialize informer.
	ambassadorHostInformer.Informer().AddEventHandler(
		cache.ResourceEventHandlerFuncs{
			AddFunc: func(obj interface{}) {
			},
		},
	)

	// TODO informer is not explicitly stopped since controller is not passing in its channel.
	informerFactory.Start(wait.NeverStop)

	// wait for the local cache to be populated.
	err = poll(time.Second, 60*time.Second, func() (bool, error) {
		return ambassadorHostInformer.Informer().HasSynced(), nil
	})
	if err != nil {
		return nil, errors.Wrapf(err, "failed to sync cache")
	}

	uc, err := newUnstructuredConverter()
	if err != nil {
		return nil, errors.Wrapf(err, "failed to setup Unstructured Converter")
	}

	return &ambassadorHostSource{
		dynamicKubeClient:      dynamicKubeClient,
		kubeClient:             kubeClient,
		namespace:              namespace,
		annotationFilter:       annotationFilter,
		ambassadorHostInformer: ambassadorHostInformer,
		unstructuredConverter:  uc,
	}, nil
}

// Endpoints returns endpoint objects for each host-target combination that should be processed.
// Retrieves all Hosts in the source's namespace(s).
func (sc *ambassadorHostSource) Endpoints(ctx context.Context) ([]*endpoint.Endpoint, error) {
	h, err := sc.ambassadorHostInformer.Lister().ByNamespace(sc.namespace).List(labels.Everything())
	if err != nil {
		return nil, err
	}

	// Convert to []*ambassador.Host
	var hosts []*ambassador.Host

	endpoints := []*endpoint.Endpoint{}
	for _, hostObj := range h {
		unstructuredHost, ok := hostObj.(*unstructured.Unstructured)
		if !ok {
			return nil, errors.New("could not convert")
		}

		host := &ambassador.Host{}
		err := sc.unstructuredConverter.scheme.Convert(unstructuredHost, host, nil)
		if err != nil {
			return nil, err
		}

		hosts = append(hosts, host)
	}

	hosts, err = sc.filterByAnnotations(hosts)
	if err != nil {
		return nil, errors.Wrap(err, "failed to filter Hosts")
	}

	for _, host := range hosts {
		fullname := fmt.Sprintf("%s/%s", host.Namespace, host.Name)

		// look for the "exernal-dns.ambassador-service" annotation. If it is not there then just ignore this `Host`
		service, found := host.Annotations[ambHostAnnotation]
		if !found {
			log.Debugf("Host %s ignored: no annotation %q found", fullname, ambHostAnnotation)
			continue
		}

		targets, err := sc.targetsFromAmbassadorLoadBalancer(ctx, service)
		if err != nil {
			return nil, err
		}

		hostEndpoints, err := sc.endpointsFromHost(ctx, host, targets)
		if err != nil {
			return nil, err
		}
		if len(hostEndpoints) == 0 {
			log.Debugf("No endpoints could be generated from Host %s", fullname)
			continue
		}

		log.Debugf("Endpoints generated from Host: %s: %v", fullname, hostEndpoints)
		endpoints = append(endpoints, hostEndpoints...)
	}

	for _, ep := range endpoints {
		sort.Sort(ep.Targets)
	}

	return endpoints, nil
}

// endpointsFromHost extracts the endpoints from a Host object
func (sc *ambassadorHostSource) endpointsFromHost(ctx context.Context, host *ambassador.Host, targets endpoint.Targets) ([]*endpoint.Endpoint, error) {
	var endpoints []*endpoint.Endpoint

	providerSpecific := endpoint.ProviderSpecific{}
	setIdentifier := ""

	annotations := host.Annotations
	ttl, err := getTTLFromAnnotations(annotations)
	if err != nil {
		return nil, err
	}

	if host.Spec != nil {
		hostname := host.Spec.Hostname
		if hostname != "" {
			endpoints = append(endpoints, endpointsForHostname(hostname, targets, ttl, providerSpecific, setIdentifier)...)
		}
	}

	return endpoints, nil
}

func (sc *ambassadorHostSource) targetsFromAmbassadorLoadBalancer(ctx context.Context, service string) (targets endpoint.Targets, err error) {
	lbNamespace, lbName, err := parseAmbLoadBalancerService(service)
	if err != nil {
		return nil, err
	}

	svc, err := sc.kubeClient.CoreV1().Services(lbNamespace).Get(ctx, lbName, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}

	for _, lb := range svc.Status.LoadBalancer.Ingress {
		if lb.IP != "" {
			targets = append(targets, lb.IP)
		}
		if lb.Hostname != "" {
			targets = append(targets, lb.Hostname)
		}
	}

	return
}

// parseAmbLoadBalancerService returns a name/namespace tuple from the annotation in
// an Ambassador Host CRD
//
// This is a thing because Ambassador has historically supported cross-namespace
// references using a name.namespace syntax, but here we want to also support
// namespace/name.
//
// Returns namespace, name, error.

func parseAmbLoadBalancerService(service string) (namespace, name string, err error) {
	// Start by assuming that we have namespace/name.
	parts := strings.Split(service, "/")

	if len(parts) == 1 {
		// No "/" at all, so let's try for name.namespace. To be consistent with the
		// rest of Ambassador, use SplitN to limit this to one split, so that e.g.
		// svc.foo.bar uses service "svc" in namespace "foo.bar".
		parts = strings.SplitN(service, ".", 2)

		if len(parts) == 2 {
			// We got a namespace, great.
			name := parts[0]
			namespace := parts[1]

			return namespace, name, nil
		}

		// If here, we have no separator, so the whole string is the service, and
		// we can assume the default namespace.
		name := service
		namespace := api.NamespaceDefault

		return namespace, name, nil
	} else if len(parts) == 2 {
		// This is "namespace/name". Note that the name could be qualified,
		// which is fine.
		namespace := parts[0]
		name := parts[1]

		return namespace, name, nil
	}

	// If we got here, this string is simply ill-formatted. Return an error.
	return "", "", errors.New(fmt.Sprintf("invalid external-dns service: %s", service))
}

func (sc *ambassadorHostSource) AddEventHandler(ctx context.Context, handler func()) {
}

// unstructuredConverter handles conversions between unstructured.Unstructured and Ambassador types
type unstructuredConverter struct {
	// scheme holds an initializer for converting Unstructured to a type
	scheme *runtime.Scheme
}

// newUnstructuredConverter returns a new unstructuredConverter initialized
func newUnstructuredConverter() (*unstructuredConverter, error) {
	uc := &unstructuredConverter{
		scheme: runtime.NewScheme(),
	}

	// Setup converter to understand custom CRD types
	ambassador.AddToScheme(uc.scheme)

	// Add the core types we need
	if err := scheme.AddToScheme(uc.scheme); err != nil {
		return nil, err
	}

	return uc, nil
}

// filterByAnnotations filters a list of configs by a given annotation selector.
func (sc *ambassadorHostSource) filterByAnnotations(hosts []*ambassador.Host) ([]*ambassador.Host, error) {
	labelSelector, err := metav1.ParseToLabelSelector(sc.annotationFilter)
	if err != nil {
		return nil, err
	}
	selector, err := metav1.LabelSelectorAsSelector(labelSelector)
	if err != nil {
		return nil, err
	}

	// empty filter returns original list
	if selector.Empty() {
		return hosts, nil
	}

	filteredList := []*ambassador.Host{}

	for _, host := range hosts {
		// convert the Host's annotations to an equivalent label selector
		annotations := labels.Set(host.Annotations)

		// include Host if its annotations match the selector
		if selector.Matches(annotations) {
			filteredList = append(filteredList, host)
		}
	}

	return filteredList, nil
}
