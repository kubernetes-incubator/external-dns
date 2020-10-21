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
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/util/wait"
	kubeinformers "k8s.io/client-go/informers"
	coreinformers "k8s.io/client-go/informers/core/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"

	"sigs.k8s.io/external-dns/endpoint"
)

const (
	defaultTargetsCapacity = 10
)

// serviceSource is an implementation of Source for Kubernetes service objects.
// It will find all services that are under our jurisdiction, i.e. annotated
// desired hostname and matching or no controller annotation. For each of the
// matched services' entrypoints it will return a corresponding
// Endpoint object.
type serviceSource struct {
	client           kubernetes.Interface
	namespace        string
	annotationFilter string

	// process Services with legacy annotations
	compatibility                  string
	fqdnTemplate                   *template.Template
	combineFQDNAnnotation          bool
	ignoreHostnameAnnotation       bool
	publishInternal                bool
	publishHostIP                  bool
	nodePortEnabled                bool
	nodePortLabels                 labels.Set
	alwaysPublishNotReadyAddresses bool
	serviceInformer                coreinformers.ServiceInformer
	endpointsInformer              coreinformers.EndpointsInformer
	podInformer                    coreinformers.PodInformer
	nodeInformer                   coreinformers.NodeInformer
	serviceTypeFilter              map[string]struct{}
}

// NewServiceSource creates a new serviceSource with the given config.
func NewServiceSource(kubeClient kubernetes.Interface, namespace, annotationFilter string, fqdnTemplate string, combineFqdnAnnotation bool, compatibility string, publishInternal bool, publishHostIP bool, alwaysPublishNotReadyAddresses bool, serviceTypeFilter []string, ignoreHostnameAnnotation, nodePortEnabled bool, nodePortSelector string) (Source, error) {
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

	nodePortLabels, err := labels.ConvertSelectorToLabelsMap(nodePortSelector)
	if err != nil {
		return nil, err
	}

	// Use shared informers to listen for add/update/delete of services/pods/nodes in the specified namespace.
	// Set resync period to 0, to prevent processing when nothing has changed
	informerFactory := kubeinformers.NewSharedInformerFactoryWithOptions(kubeClient, 0, kubeinformers.WithNamespace(namespace))
	serviceInformer := informerFactory.Core().V1().Services()
	endpointsInformer := informerFactory.Core().V1().Endpoints()
	podInformer := informerFactory.Core().V1().Pods()
	nodeInformer := informerFactory.Core().V1().Nodes()

	// Add default resource event handlers to properly initialize informer.
	serviceInformer.Informer().AddEventHandler(
		cache.ResourceEventHandlerFuncs{
			AddFunc: func(obj interface{}) {
			},
		},
	)
	endpointsInformer.Informer().AddEventHandler(
		cache.ResourceEventHandlerFuncs{
			AddFunc: func(obj interface{}) {
			},
		},
	)
	podInformer.Informer().AddEventHandler(
		cache.ResourceEventHandlerFuncs{
			AddFunc: func(obj interface{}) {
			},
		},
	)
	nodeInformer.Informer().AddEventHandler(
		cache.ResourceEventHandlerFuncs{
			AddFunc: func(obj interface{}) {
			},
		},
	)

	// TODO informer is not explicitly stopped since controller is not passing in its channel.
	informerFactory.Start(wait.NeverStop)

	// wait for the local cache to be populated.
	err = poll(time.Second, 60*time.Second, func() (bool, error) {
		return serviceInformer.Informer().HasSynced() &&
			endpointsInformer.Informer().HasSynced() &&
			podInformer.Informer().HasSynced() &&
			nodeInformer.Informer().HasSynced(), nil
	})
	if err != nil {
		return nil, fmt.Errorf("failed to sync cache: %v", err)
	}

	// Transform the slice into a map so it will
	// be way much easier and fast to filter later
	serviceTypes := make(map[string]struct{})
	for _, serviceType := range serviceTypeFilter {
		serviceTypes[serviceType] = struct{}{}
	}

	return &serviceSource{
		client:                         kubeClient,
		namespace:                      namespace,
		annotationFilter:               annotationFilter,
		compatibility:                  compatibility,
		fqdnTemplate:                   tmpl,
		combineFQDNAnnotation:          combineFqdnAnnotation,
		ignoreHostnameAnnotation:       ignoreHostnameAnnotation,
		publishInternal:                publishInternal,
		publishHostIP:                  publishHostIP,
		nodePortEnabled:                nodePortEnabled,
		nodePortLabels:                 nodePortLabels,
		alwaysPublishNotReadyAddresses: alwaysPublishNotReadyAddresses,
		serviceInformer:                serviceInformer,
		endpointsInformer:              endpointsInformer,
		podInformer:                    podInformer,
		nodeInformer:                   nodeInformer,
		serviceTypeFilter:              serviceTypes,
	}, nil
}

// Endpoints returns endpoint objects for each service that should be processed.
func (sc *serviceSource) Endpoints(ctx context.Context) ([]*endpoint.Endpoint, error) {
	services, err := sc.serviceInformer.Lister().Services(sc.namespace).List(labels.Everything())
	if err != nil {
		return nil, err
	}
	services, err = sc.filterByAnnotations(services)
	if err != nil {
		return nil, err
	}

	// filter on service types if at least one has been provided
	if len(sc.serviceTypeFilter) > 0 {
		services = sc.filterByServiceType(services)
	}

	endpoints := []*endpoint.Endpoint{}

	for _, svc := range services {
		// Check controller annotation to see if we are responsible.
		controller, ok := svc.Annotations[controllerAnnotationKey]
		if ok && controller != controllerAnnotationValue {
			log.Debugf("Skipping service %s/%s because controller value does not match, found: %s, required: %s",
				svc.Namespace, svc.Name, controller, controllerAnnotationValue)
			continue
		}

		svcEndpoints := sc.endpoints(svc)

		// process legacy annotations if no endpoints were returned and compatibility mode is enabled.
		if len(svcEndpoints) == 0 && sc.compatibility != "" {
			svcEndpoints = legacyEndpointsFromService(svc, sc.compatibility)
		}

		// apply template if none of the above is found
		if (sc.combineFQDNAnnotation || len(svcEndpoints) == 0) && sc.fqdnTemplate != nil {
			sEndpoints, err := sc.endpointsFromTemplate(svc)
			if err != nil {
				return nil, err
			}

			if sc.combineFQDNAnnotation {
				svcEndpoints = append(svcEndpoints, sEndpoints...)
			} else {
				svcEndpoints = sEndpoints
			}
		}

		if len(svcEndpoints) == 0 {
			log.Debugf("No endpoints could be generated from service %s/%s", svc.Namespace, svc.Name)
			continue
		}

		log.Debugf("Endpoints generated from service: %s/%s: %v", svc.Namespace, svc.Name, svcEndpoints)
		sc.setResourceLabel(svc, svcEndpoints)
		endpoints = append(endpoints, svcEndpoints...)
	}

	// this sorting is required to make merging work.
	// after we merge endpoints that have same DNS, we want to ensure that we end up with the same service being an "owner"
	// of all those records, as otherwise each time we update, we will end up with a different service that gets data merged in
	// and that will cause external-dns to recreate dns record due to different service owner in TXT record.
	// if new service is added or old one removed, that might cause existing record to get re-created due to potentially new
	// owner being selected. Which is fine, since it shouldn't happen often and shouldn't cause any disruption.
	if len(endpoints) > 1 {
		sort.Slice(endpoints, func(i, j int) bool {
			return endpoints[i].Labels[endpoint.ResourceLabelKey] < endpoints[j].Labels[endpoint.ResourceLabelKey]
		})
		// Use stable sort to not disrupt the order of services
		sort.SliceStable(endpoints, func(i, j int) bool {
			return endpoints[i].DNSName < endpoints[j].DNSName
		})
		mergedEndpoints := []*endpoint.Endpoint{}
		mergedEndpoints = append(mergedEndpoints, endpoints[0])
		for i := 1; i < len(endpoints); i++ {
			lastMergedEndpoint := len(mergedEndpoints) - 1
			if mergedEndpoints[lastMergedEndpoint].DNSName == endpoints[i].DNSName &&
				mergedEndpoints[lastMergedEndpoint].RecordType == endpoints[i].RecordType &&
				mergedEndpoints[lastMergedEndpoint].RecordTTL == endpoints[i].RecordTTL {
				mergedEndpoints[lastMergedEndpoint].Targets = append(mergedEndpoints[lastMergedEndpoint].Targets, endpoints[i].Targets[0])
			} else {
				mergedEndpoints = append(mergedEndpoints, endpoints[i])
			}
		}
		endpoints = mergedEndpoints
	}

	for _, ep := range endpoints {
		sort.Sort(ep.Targets)
	}

	return endpoints, nil
}

// extractHeadlessEndpoints extracts endpoints from a headless service using the "Endpoints" Kubernetes API resource
func (sc *serviceSource) extractHeadlessEndpoints(svc *v1.Service, hostname string, ttl endpoint.TTL) []*endpoint.Endpoint {
	var endpoints []*endpoint.Endpoint

	labelSelector, err := metav1.ParseToLabelSelector(labels.Set(svc.Spec.Selector).AsSelectorPreValidated().String())
	if err != nil {
		return nil
	}
	selector, err := metav1.LabelSelectorAsSelector(labelSelector)
	if err != nil {
		return nil
	}

	endpointsObject, err := sc.endpointsInformer.Lister().Endpoints(svc.Namespace).Get(svc.GetName())
	if err != nil {
		log.Errorf("Get endpoints of service[%s] error:%v", svc.GetName(), err)
		return endpoints
	}

	pods, err := sc.podInformer.Lister().Pods(svc.Namespace).List(selector)
	if err != nil {
		log.Errorf("List Pods of service[%s] error:%v", svc.GetName(), err)
		return endpoints
	}

	targetsByHeadlessDomain := make(map[string][]string)
	for _, subset := range endpointsObject.Subsets {
		addresses := subset.Addresses
		if svc.Spec.PublishNotReadyAddresses || sc.alwaysPublishNotReadyAddresses {
			addresses = append(addresses, subset.NotReadyAddresses...)
		}

		for _, address := range addresses {
			// find pod for this address
			if address.TargetRef.APIVersion != "" || address.TargetRef.Kind != "Pod" {
				log.Debugf("Skipping address because its target is not a pod: %v", address)
				continue
			}
			var pod *v1.Pod
			for _, v := range pods {
				if v.Name == address.TargetRef.Name {
					pod = v
					break
				}
			}
			if pod == nil {
				log.Errorf("Pod %s not found for address %v", address.TargetRef.Name, address)
				continue
			}

			headlessDomains := []string{hostname}
			if pod.Spec.Hostname != "" {
				headlessDomains = append(headlessDomains, fmt.Sprintf("%s.%s", pod.Spec.Hostname, hostname))
			}

			for _, headlessDomain := range headlessDomains {
				var ep string
				if sc.publishHostIP {
					ep = pod.Status.HostIP
					log.Debugf("Generating matching endpoint %s with HostIP %s", headlessDomain, ep)
				} else {
					ep = address.IP
					log.Debugf("Generating matching endpoint %s with EndpointAddress IP %s", headlessDomain, ep)
				}
				targetsByHeadlessDomain[headlessDomain] = append(targetsByHeadlessDomain[headlessDomain], ep)
			}
		}
	}

	headlessDomains := []string{}
	for headlessDomain := range targetsByHeadlessDomain {
		headlessDomains = append(headlessDomains, headlessDomain)
	}
	sort.Strings(headlessDomains)
	for _, headlessDomain := range headlessDomains {
		allTargets := targetsByHeadlessDomain[headlessDomain]
		targets := []string{}

		deduppedTargets := map[string]struct{}{}
		for _, target := range allTargets {
			if _, ok := deduppedTargets[target]; ok {
				log.Debugf("Removing duplicate target %s", target)
				continue
			}

			deduppedTargets[target] = struct{}{}
			targets = append(targets, target)
		}

		if ttl.IsConfigured() {
			endpoints = append(endpoints, endpoint.NewEndpointWithTTL(headlessDomain, endpoint.RecordTypeA, ttl, targets...))
		} else {
			endpoints = append(endpoints, endpoint.NewEndpoint(headlessDomain, endpoint.RecordTypeA, targets...))
		}
	}

	return endpoints
}

func (sc *serviceSource) endpointsFromTemplate(svc *v1.Service) ([]*endpoint.Endpoint, error) {
	var endpoints []*endpoint.Endpoint

	// Process the whole template string
	var buf bytes.Buffer
	err := sc.fqdnTemplate.Execute(&buf, svc)
	if err != nil {
		return nil, fmt.Errorf("failed to apply template on service %s: %v", svc.String(), err)
	}

	providerSpecific, setIdentifier := getProviderSpecificAnnotations(svc.Annotations)
	hostnameList := strings.Split(strings.Replace(buf.String(), " ", "", -1), ",")
	for _, hostname := range hostnameList {
		endpoints = append(endpoints, sc.generateEndpoints(svc, hostname, providerSpecific, setIdentifier)...)
	}

	return endpoints, nil
}

// endpointsFromService extracts the endpoints from a service object
func (sc *serviceSource) endpoints(svc *v1.Service) []*endpoint.Endpoint {
	var endpoints []*endpoint.Endpoint
	// Skip endpoints if we do not want entries from annotations
	if !sc.ignoreHostnameAnnotation {
		providerSpecific, setIdentifier := getProviderSpecificAnnotations(svc.Annotations)
		hostnameList := getHostnamesFromAnnotations(svc.Annotations)
		for _, hostname := range hostnameList {
			endpoints = append(endpoints, sc.generateEndpoints(svc, hostname, providerSpecific, setIdentifier)...)
		}
	}
	return endpoints
}

// filterByAnnotations filters a list of services by a given annotation selector.
func (sc *serviceSource) filterByAnnotations(services []*v1.Service) ([]*v1.Service, error) {
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
		return services, nil
	}

	filteredList := []*v1.Service{}

	for _, service := range services {
		// convert the service's annotations to an equivalent label selector
		annotations := labels.Set(service.Annotations)

		// include service if its annotations match the selector
		if selector.Matches(annotations) {
			filteredList = append(filteredList, service)
		}
	}

	return filteredList, nil
}

// filterByServiceType filters services according their types
func (sc *serviceSource) filterByServiceType(services []*v1.Service) []*v1.Service {
	filteredList := []*v1.Service{}
	for _, service := range services {
		// Check if the service is of the given type or not
		if _, ok := sc.serviceTypeFilter[string(service.Spec.Type)]; ok {
			filteredList = append(filteredList, service)
		}
	}

	return filteredList
}

func (sc *serviceSource) setResourceLabel(service *v1.Service, endpoints []*endpoint.Endpoint) {
	for _, ep := range endpoints {
		ep.Labels[endpoint.ResourceLabelKey] = fmt.Sprintf("service/%s/%s", service.Namespace, service.Name)
	}
}

func (sc *serviceSource) generateEndpoints(svc *v1.Service, hostname string, providerSpecific endpoint.ProviderSpecific, setIdentifier string) []*endpoint.Endpoint {
	hostname = strings.TrimSuffix(hostname, ".")
	ttl, err := getTTLFromAnnotations(svc.Annotations)
	if err != nil {
		log.Warn(err)
	}

	epA := &endpoint.Endpoint{
		RecordTTL:  ttl,
		RecordType: endpoint.RecordTypeA,
		Labels:     endpoint.NewLabels(),
		Targets:    make(endpoint.Targets, 0, defaultTargetsCapacity),
		DNSName:    hostname,
	}

	epCNAME := &endpoint.Endpoint{
		RecordTTL:  ttl,
		RecordType: endpoint.RecordTypeCNAME,
		Labels:     endpoint.NewLabels(),
		Targets:    make(endpoint.Targets, 0, defaultTargetsCapacity),
		DNSName:    hostname,
	}

	var endpoints []*endpoint.Endpoint
	var targets endpoint.Targets

	switch svc.Spec.Type {
	case v1.ServiceTypeLoadBalancer:
		targets = append(targets, extractLoadBalancerTargets(svc)...)
	case v1.ServiceTypeClusterIP:
		if sc.publishInternal {
			targets = append(targets, extractServiceIps(svc)...)
		}
		if svc.Spec.ClusterIP == v1.ClusterIPNone {
			endpoints = append(endpoints, sc.extractHeadlessEndpoints(svc, hostname, ttl)...)
		}
	case v1.ServiceTypeNodePort:
		// add the nodeTargets and extract an SRV endpoint
		targets, err = sc.extractNodePortTargets(svc)
		if err != nil {
			log.Errorf("Unable to extract targets from service %s/%s error: %v", svc.Namespace, svc.Name, err)
			return endpoints
		}
		// If the user wants an SRV record as well, extract that here
		if sc.nodePortEnabled {
			endpoints = append(endpoints, sc.extractNodePortSRVEndpoints(svc, targets, hostname, ttl)...)
		}
	case v1.ServiceTypeExternalName:
		targets = append(targets, extractServiceExternalName(svc)...)
	}

	for _, t := range targets {
		if suitableType(t) == endpoint.RecordTypeA {
			epA.Targets = append(epA.Targets, t)
		}
		if suitableType(t) == endpoint.RecordTypeCNAME {
			epCNAME.Targets = append(epCNAME.Targets, t)
		}
	}

	if len(epA.Targets) > 0 {
		endpoints = append(endpoints, epA)
	}
	if len(epCNAME.Targets) > 0 {
		endpoints = append(endpoints, epCNAME)
	}
	for _, endpoint := range endpoints {
		endpoint.ProviderSpecific = providerSpecific
		endpoint.SetIdentifier = setIdentifier
	}
	return endpoints
}

func extractServiceIps(svc *v1.Service) endpoint.Targets {
	if svc.Spec.ClusterIP == v1.ClusterIPNone {
		log.Debugf("Unable to associate %s headless service with a Cluster IP", svc.Name)
		return endpoint.Targets{}
	}
	return endpoint.Targets{svc.Spec.ClusterIP}
}

func extractServiceExternalName(svc *v1.Service) endpoint.Targets {
	return endpoint.Targets{svc.Spec.ExternalName}
}

func extractLoadBalancerTargets(svc *v1.Service) endpoint.Targets {
	var (
		targets     endpoint.Targets
		externalIPs endpoint.Targets
	)

	// Create a corresponding endpoint for each configured external entrypoint.
	for _, lb := range svc.Status.LoadBalancer.Ingress {
		if lb.IP != "" {
			targets = append(targets, lb.IP)
		}
		if lb.Hostname != "" {
			targets = append(targets, lb.Hostname)
		}
	}

	if svc.Spec.ExternalIPs != nil {
		for _, ext := range svc.Spec.ExternalIPs {
			externalIPs = append(externalIPs, ext)
		}
	}

	if len(externalIPs) > 0 {
		return externalIPs
	}

	return targets
}

func (sc *serviceSource) extractNodePortTargets(svc *v1.Service) (endpoint.Targets, error) {
	var (
		internalIPs endpoint.Targets
		externalIPs endpoint.Targets
		nodes       []*v1.Node
		err         error
	)

	switch svc.Spec.ExternalTrafficPolicy {
	case v1.ServiceExternalTrafficPolicyTypeLocal:
		// k8s services with an externalTrafficPolicy of Local will ensure that traffic is only
		// routed locally within a node -- not to other nodes via NATing. In this mode, a pod is
		// only accessible by directly accessing the node on which it resides. Therefore,
		// we need to find which node(s) the pod(s) selected by the service are residing on, and
		// only include those nodes in the targets.
		nodesMap := map[*v1.Node]struct{}{}
		labelSelector, err := metav1.ParseToLabelSelector(labels.Set(svc.Spec.Selector).AsSelectorPreValidated().String())
		if err != nil {
			return nil, err
		}
		selector, err := metav1.LabelSelectorAsSelector(labelSelector)
		if err != nil {
			return nil, err
		}
		pods, err := sc.podInformer.Lister().Pods(svc.Namespace).List(selector)
		if err != nil {
			return nil, err
		}

		for _, v := range pods {
			if v.Status.Phase == v1.PodRunning {
				node, err := sc.nodeInformer.Lister().Get(v.Spec.NodeName)
				if err != nil {
					log.Debugf("Unable to find node where Pod %s is running", v.Spec.Hostname)
					continue
				}
				if _, ok := nodesMap[node]; !ok {
					nodesMap[node] = *new(struct{})
					nodes = append(nodes, node)
				}
			}
		}
	default:
		// Ensure we filter out the master from the list of nodes by only selecting
		// nodes labeled with the "worker" role, not the "master" role
		nodes, err = sc.nodeInformer.Lister().List(labels.SelectorFromSet(sc.nodePortLabels))
		if err != nil {
			return nil, err
		}
	}

	for _, node := range nodes {
		for _, address := range node.Status.Addresses {
			switch address.Type {
			case v1.NodeExternalIP:
				externalIPs = append(externalIPs, address.Address)
			case v1.NodeInternalIP:
				internalIPs = append(internalIPs, address.Address)
			}
		}
	}

	access := getAccessFromAnnotations(svc.Annotations)
	if access == "public" {
		return externalIPs, nil
	}
	if access == "private" {
		return internalIPs, nil
	}
	if len(externalIPs) > 0 {
		return externalIPs, nil
	}
	return internalIPs, nil
}

func (sc *serviceSource) extractNodePortSRVEndpoints(svc *v1.Service, nodeTargets endpoint.Targets, hostname string, ttl endpoint.TTL) []*endpoint.Endpoint {
	var endpoints []*endpoint.Endpoint

	for _, port := range svc.Spec.Ports {
		if port.NodePort > 0 {
			// build a target with a priority of 0, weight of 0, and pointing the given port on the given host
			target := fmt.Sprintf("0 50 %d %s", port.NodePort, hostname)

			// figure out the portname
			portName := port.Name
			if portName == "" {
				portName = fmt.Sprintf("%d", port.NodePort)
			}

			// figure out the protocol
			protocol := strings.ToLower(string(port.Protocol))
			if protocol == "" {
				protocol = "tcp"
			}

			recordName := fmt.Sprintf("_%s._%s.%s", portName, protocol, hostname)

			var ep *endpoint.Endpoint
			if ttl.IsConfigured() {
				ep = endpoint.NewEndpointWithTTL(recordName, endpoint.RecordTypeSRV, ttl, target)
			} else {
				ep = endpoint.NewEndpoint(recordName, endpoint.RecordTypeSRV, target)
			}

			endpoints = append(endpoints, ep)
		}
	}

	return endpoints
}

func (sc *serviceSource) AddEventHandler(ctx context.Context, handler func()) {
	log.Debug("Adding event handler for service")

	// Right now there is no way to remove event handler from informer, see:
	// https://github.com/kubernetes/kubernetes/issues/79610
	sc.serviceInformer.Informer().AddEventHandler(
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
