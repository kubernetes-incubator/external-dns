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
	networkingv1alpha3 "istio.io/client-go/pkg/apis/networking/v1alpha3"
	istioclient "istio.io/client-go/pkg/clientset/versioned"
	istioinformers "istio.io/client-go/pkg/informers/externalversions"
	networkingv1alpha3informer "istio.io/client-go/pkg/informers/externalversions/networking/v1alpha3"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/util/wait"
	kubeinformers "k8s.io/client-go/informers"
	coreinformers "k8s.io/client-go/informers/core/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"

	"sigs.k8s.io/external-dns/endpoint"
)

// gatewaySource is an implementation of Source for Istio Gateway objects.
// The gateway implementation uses the spec.servers.hosts values for the hostnames.
// Use targetAnnotationKey to explicitly set Endpoint.
type gatewaySource struct {
	kubeClient               kubernetes.Interface
	istioClient              istioclient.Interface
	namespace                string
	annotationFilter         string
	fqdnTemplate             *template.Template
	combineFQDNAnnotation    bool
	ignoreHostnameAnnotation bool
	serviceInformer          coreinformers.ServiceInformer
	gatewayInformer          networkingv1alpha3informer.GatewayInformer
}

// NewIstioGatewaySource creates a new gatewaySource with the given config.
func NewIstioGatewaySource(
	kubeClient kubernetes.Interface,
	istioClient istioclient.Interface,
	namespace string,
	annotationFilter string,
	fqdnTemplate string,
	combineFQDNAnnotation bool,
	ignoreHostnameAnnotation bool,
) (Source, error) {
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

	// Use shared informers to listen for add/update/delete of services/pods/nodes in the specified namespace.
	// Set resync period to 0, to prevent processing when nothing has changed
	informerFactory := kubeinformers.NewSharedInformerFactoryWithOptions(kubeClient, 0, kubeinformers.WithNamespace(namespace))
	serviceInformer := informerFactory.Core().V1().Services()
	istioInformerFactory := istioinformers.NewSharedInformerFactory(istioClient, 0)
	gatewayInformer := istioInformerFactory.Networking().V1alpha3().Gateways()

	// Add default resource event handlers to properly initialize informer.
	serviceInformer.Informer().AddEventHandler(
		cache.ResourceEventHandlerFuncs{
			AddFunc: func(obj interface{}) {
				log.Debug("service added")
			},
		},
	)

	gatewayInformer.Informer().AddEventHandler(
		cache.ResourceEventHandlerFuncs{
			AddFunc: func(obj interface{}) {
				log.Debug("gateway added")
			},
		},
	)

	// TODO informer is not explicitly stopped since controller is not passing in its channel.
	informerFactory.Start(wait.NeverStop)
	istioInformerFactory.Start(wait.NeverStop)

	// wait for the local cache to be populated.
	err = poll(time.Second, 60*time.Second, func() (bool, error) {
		return serviceInformer.Informer().HasSynced(), nil
	})
	if err != nil {
		return nil, fmt.Errorf("failed to sync cache: %v", err)
	}

	// wait for the local cache to be populated.
	err = poll(time.Second, 60*time.Second, func() (bool, error) {
		return gatewayInformer.Informer().HasSynced(), nil
	})
	if err != nil {
		return nil, fmt.Errorf("failed to sync cache: %v", err)
	}

	return &gatewaySource{
		kubeClient:               kubeClient,
		istioClient:              istioClient,
		namespace:                namespace,
		annotationFilter:         annotationFilter,
		fqdnTemplate:             tmpl,
		combineFQDNAnnotation:    combineFQDNAnnotation,
		ignoreHostnameAnnotation: ignoreHostnameAnnotation,
		serviceInformer:          serviceInformer,
		gatewayInformer:          gatewayInformer,
	}, nil
}

// Endpoints returns endpoint objects for each host-target combination that should be processed.
// Retrieves all gateway resources in the source's namespace(s).
func (sc *gatewaySource) Endpoints(ctx context.Context) ([]*endpoint.Endpoint, error) {
	gwList, err := sc.istioClient.NetworkingV1alpha3().Gateways(sc.namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, err
	}

	gateways := gwList.Items
	gateways, err = sc.filterByAnnotations(gateways)
	if err != nil {
		return nil, err
	}

	var endpoints []*endpoint.Endpoint

	for _, gateway := range gateways {
		// Check controller annotation to see if we are responsible.
		controller, ok := gateway.Annotations[controllerAnnotationKey]
		if ok && controller != controllerAnnotationValue {
			log.Debugf("Skipping gateway %s/%s because controller value does not match, found: %s, required: %s",
				gateway.Namespace, gateway.Name, controller, controllerAnnotationValue)
			continue
		}

		gwHostnames, err := sc.hostNamesFromGateway(gateway)
		if err != nil {
			return nil, err
		}

		// apply template if host is missing on gateway
		if (sc.combineFQDNAnnotation || len(gwHostnames) == 0) && sc.fqdnTemplate != nil {
			iHostnames, err := sc.hostNamesFromTemplate(gateway)
			if err != nil {
				return nil, err
			}

			if sc.combineFQDNAnnotation {
				gwHostnames = append(gwHostnames, iHostnames...)
			} else {
				gwHostnames = iHostnames
			}
		}

		if len(gwHostnames) == 0 {
			log.Debugf("No hostnames could be generated from gateway %s/%s", gateway.Namespace, gateway.Name)
			continue
		}

		gwEndpoints, err := sc.endpointsFromGateway(gwHostnames, gateway)
		if err != nil {
			return nil, err
		}

		log.Debugf("Endpoints generated from gateway: %s/%s: %v", gateway.Namespace, gateway.Name, gwEndpoints)
		sc.setResourceLabel(gateway, gwEndpoints)
		addClaimLabels(gateway.Annotations, gwEndpoints)
		endpoints = append(endpoints, gwEndpoints...)
	}

	for _, ep := range endpoints {
		sort.Sort(ep.Targets)
	}

	return endpoints, nil
}

// AddEventHandler adds an event handler that should be triggered if the watched Istio Gateway changes.
func (sc *gatewaySource) AddEventHandler(ctx context.Context, handler func()) {
	log.Debug("Adding event handler for Istio Gateway")

	sc.gatewayInformer.Informer().AddEventHandler(
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

// filterByAnnotations filters a list of configs by a given annotation selector.
func (sc *gatewaySource) filterByAnnotations(gateways []networkingv1alpha3.Gateway) ([]networkingv1alpha3.Gateway, error) {
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
		return gateways, nil
	}

	var filteredList []networkingv1alpha3.Gateway

	for _, gw := range gateways {
		// convert the annotations to an equivalent label selector
		annotations := labels.Set(gw.Annotations)

		// include if the annotations match the selector
		if selector.Matches(annotations) {
			filteredList = append(filteredList, gw)
		}
	}

	return filteredList, nil
}

func (sc *gatewaySource) setResourceLabel(gateway networkingv1alpha3.Gateway, endpoints []*endpoint.Endpoint) {
	for _, ep := range endpoints {
		ep.Labels[endpoint.ResourceLabelKey] = fmt.Sprintf("gateway/%s/%s", gateway.Namespace, gateway.Name)
	}
}

func (sc *gatewaySource) targetsFromGateway(gateway networkingv1alpha3.Gateway) (targets endpoint.Targets, err error) {
	targets = getTargetsFromTargetAnnotation(gateway.Annotations)
	if len(targets) > 0 {
		return
	}

	services, err := sc.serviceInformer.Lister().Services(sc.namespace).List(labels.Everything())
	if err != nil {
		log.Error(err)
		return
	}

	for _, service := range services {
		if !gatewaySelectorMatchesServiceSelector(gateway.Spec.Selector, service.Spec.Selector) {
			continue
		}

		for _, lb := range service.Status.LoadBalancer.Ingress {
			if lb.IP != "" {
				targets = append(targets, lb.IP)
			} else if lb.Hostname != "" {
				targets = append(targets, lb.Hostname)
			}
		}
	}

	return
}

// endpointsFromGatewayConfig extracts the endpoints from an Istio Gateway Config object
func (sc *gatewaySource) endpointsFromGateway(hostnames []string, gateway networkingv1alpha3.Gateway) ([]*endpoint.Endpoint, error) {
	var endpoints []*endpoint.Endpoint

	annotations := gateway.Annotations
	ttl, err := getTTLFromAnnotations(annotations)
	if err != nil {
		log.Warn(err)
	}

	targets := getTargetsFromTargetAnnotation(annotations)

	if len(targets) == 0 {
		targets, err = sc.targetsFromGateway(gateway)
		if err != nil {
			return nil, err
		}
	}

	providerSpecific, setIdentifier := getProviderSpecificAnnotations(annotations)

	for _, host := range hostnames {
		endpoints = append(endpoints, endpointsForHostname(host, targets, ttl, providerSpecific, setIdentifier)...)
	}

	return endpoints, nil
}

func (sc *gatewaySource) hostNamesFromGateway(gateway networkingv1alpha3.Gateway) ([]string, error) {
	var hostnames []string
	for _, server := range gateway.Spec.Servers {
		for _, host := range server.Hosts {
			if host == "" {
				continue
			}

			parts := strings.Split(host, "/")

			// If the input hostname is of the form my-namespace/foo.bar.com, remove the namespace
			// before appending it to the list of endpoints to create
			if len(parts) == 2 {
				host = parts[1]
			}

			if host != "*" {
				hostnames = append(hostnames, host)
			}
		}
	}

	if !sc.ignoreHostnameAnnotation {
		hostnames = append(hostnames, getHostnamesFromAnnotations(gateway.Annotations)...)
	}

	return hostnames, nil
}

func (sc *gatewaySource) hostNamesFromTemplate(gateway networkingv1alpha3.Gateway) ([]string, error) {
	// Process the whole template string
	var buf bytes.Buffer
	err := sc.fqdnTemplate.Execute(&buf, gateway)
	if err != nil {
		return nil, fmt.Errorf("failed to apply template on istio gateway %v: %v", gateway, err)
	}

	hostnames := strings.Split(strings.Replace(buf.String(), " ", "", -1), ",")
	return hostnames, nil
}

func gatewaySelectorMatchesServiceSelector(gwSelector, svcSelector map[string]string) bool {
	for k, v := range gwSelector {
		if lbl, ok := svcSelector[k]; !ok || lbl != v {
			return false
		}
	}
	return true
}
