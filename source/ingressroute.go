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

	"github.com/pkg/errors"
	contour "github.com/projectcontour/contour/apis/contour/v1beta1"
	log "github.com/sirupsen/logrus"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/dynamic/dynamicinformer"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"

	"sigs.k8s.io/external-dns/endpoint"
)

// ingressRouteSource is an implementation of Source for ProjectContour IngressRoute objects.
// The IngressRoute implementation uses the spec.virtualHost.fqdn value for the hostname.
// Use targetAnnotationKey to explicitly set Endpoint.
type ingressRouteSource struct {
	dynamicKubeClient          dynamic.Interface
	kubeClient                 kubernetes.Interface
	contourLoadBalancerService string
	namespace                  string
	annotationFilter           string
	fqdnTemplate               *template.Template
	combineFQDNAnnotation      bool
	ignoreHostnameAnnotation   bool
	ingressRouteInformer       informers.GenericInformer
	unstructuredConverter      *UnstructuredConverter
}

// NewContourIngressRouteSource creates a new contourIngressRouteSource with the given config.
func NewContourIngressRouteSource(
	dynamicKubeClient dynamic.Interface,
	kubeClient kubernetes.Interface,
	contourLoadBalancerService string,
	namespace string,
	annotationFilter string,
	fqdnTemplate string,
	combineFqdnAnnotation bool,
	ignoreHostnameAnnotation bool,
) (Source, error) {
	var (
		tmpl *template.Template
		err  error
	)
	if fqdnTemplate != "" {
		tmpl, err = template.New("endpoint").Funcs(template.FuncMap{
			"trimPrefix": strings.TrimPrefix,
			"trimSuffix": strings.TrimSuffix,
		}).Parse(fqdnTemplate)
		if err != nil {
			return nil, err
		}
	}

	if _, _, err = parseContourLoadBalancerService(contourLoadBalancerService); err != nil {
		return nil, err
	}

	// Use shared informer to listen for add/update/delete of ingressroutes in the specified namespace.
	// Set resync period to 0, to prevent processing when nothing has changed.
	informerFactory := dynamicinformer.NewFilteredDynamicSharedInformerFactory(dynamicKubeClient, 0, namespace, nil)
	ingressRouteInformer := informerFactory.ForResource(contour.IngressRouteGVR)

	// Add default resource event handlers to properly initialize informer.
	ingressRouteInformer.Informer().AddEventHandler(
		cache.ResourceEventHandlerFuncs{
			AddFunc: func(obj interface{}) {
			},
		},
	)

	// TODO informer is not explicitly stopped since controller is not passing in its channel.
	informerFactory.Start(wait.NeverStop)

	// wait for the local cache to be populated.
	err = poll(time.Second, 60*time.Second, func() (bool, error) {
		return ingressRouteInformer.Informer().HasSynced(), nil
	})
	if err != nil {
		return nil, fmt.Errorf("failed to sync cache: %v", err)
	}

	uc, err := NewUnstructuredConverter()
	if err != nil {
		return nil, fmt.Errorf("failed to setup Unstructured Converter: %v", err)
	}

	return &ingressRouteSource{
		dynamicKubeClient:          dynamicKubeClient,
		kubeClient:                 kubeClient,
		contourLoadBalancerService: contourLoadBalancerService,
		namespace:                  namespace,
		annotationFilter:           annotationFilter,
		fqdnTemplate:               tmpl,
		combineFQDNAnnotation:      combineFqdnAnnotation,
		ignoreHostnameAnnotation:   ignoreHostnameAnnotation,
		ingressRouteInformer:       ingressRouteInformer,
		unstructuredConverter:      uc,
	}, nil
}

// Endpoints returns endpoint objects for each host-target combination that should be processed.
// Retrieves all ingressroute resources in the source's namespace(s).
func (sc *ingressRouteSource) Endpoints(ctx context.Context) ([]*endpoint.Endpoint, error) {
	irs, err := sc.ingressRouteInformer.Lister().ByNamespace(sc.namespace).List(labels.Everything())
	if err != nil {
		return nil, err
	}

	// Convert to []*contour.IngressRoute
	var ingressRoutes []*contour.IngressRoute
	for _, ir := range irs {
		unstrucuredIR, ok := ir.(*unstructured.Unstructured)
		if !ok {
			return nil, errors.New("could not convert")
		}

		irConverted := &contour.IngressRoute{}
		err := sc.unstructuredConverter.scheme.Convert(unstrucuredIR, irConverted, nil)
		if err != nil {
			return nil, err
		}
		ingressRoutes = append(ingressRoutes, irConverted)
	}

	ingressRoutes, err = sc.filterByAnnotations(ingressRoutes)
	if err != nil {
		return nil, err
	}

	endpoints := []*endpoint.Endpoint{}

	for _, ir := range ingressRoutes {
		// Check controller annotation to see if we are responsible.
		controller, ok := ir.Annotations[controllerAnnotationKey]
		if ok && controller != controllerAnnotationValue {
			log.Debugf("Skipping ingressroute %s/%s because controller value does not match, found: %s, required: %s",
				ir.Namespace, ir.Name, controller, controllerAnnotationValue)
			continue
		} else if ir.CurrentStatus != "valid" {
			log.Debugf("Skipping ingressroute %s/%s because it is not valid", ir.Namespace, ir.Name)
			continue
		}

		irEndpoints, err := sc.endpointsFromIngressRoute(ctx, ir)
		if err != nil {
			return nil, err
		}

		// apply template if fqdn is missing on ingressroute
		if (sc.combineFQDNAnnotation || len(irEndpoints) == 0) && sc.fqdnTemplate != nil {
			tmplEndpoints, err := sc.endpointsFromTemplate(ctx, ir)
			if err != nil {
				return nil, err
			}

			if sc.combineFQDNAnnotation {
				irEndpoints = append(irEndpoints, tmplEndpoints...)
			} else {
				irEndpoints = tmplEndpoints
			}
		}

		if len(irEndpoints) == 0 {
			log.Debugf("No endpoints could be generated from ingressroute %s/%s", ir.Namespace, ir.Name)
			continue
		}

		log.Debugf("Endpoints generated from ingressroute: %s/%s: %v", ir.Namespace, ir.Name, irEndpoints)
		sc.setResourceLabel(ir, irEndpoints)
		endpoints = append(endpoints, irEndpoints...)
	}

	for _, ep := range endpoints {
		sort.Sort(ep.Targets)
	}

	return endpoints, nil
}

func (sc *ingressRouteSource) endpointsFromTemplate(ctx context.Context, ingressRoute *contour.IngressRoute) ([]*endpoint.Endpoint, error) {
	// Process the whole template string
	var buf bytes.Buffer
	err := sc.fqdnTemplate.Execute(&buf, ingressRoute)
	if err != nil {
		return nil, fmt.Errorf("failed to apply template on ingressroute %s/%s: %v", ingressRoute.Namespace, ingressRoute.Name, err)
	}

	hostnames := buf.String()

	ttl, err := getTTLFromAnnotations(ingressRoute.Annotations)
	if err != nil {
		log.Warn(err)
	}

	targets := getTargetsFromTargetAnnotation(ingressRoute.Annotations)

	if len(targets) == 0 {
		targets, err = sc.targetsFromContourLoadBalancer(ctx)
		if err != nil {
			return nil, err
		}
	}

	providerSpecific, setIdentifier := getProviderSpecificAnnotations(ingressRoute.Annotations)

	var endpoints []*endpoint.Endpoint
	// splits the FQDN template and removes the trailing periods
	hostnameList := strings.Split(strings.Replace(hostnames, " ", "", -1), ",")
	for _, hostname := range hostnameList {
		hostname = strings.TrimSuffix(hostname, ".")
		endpoints = append(endpoints, endpointsForHostname(hostname, targets, ttl, providerSpecific, setIdentifier)...)
	}
	return endpoints, nil
}

// filterByAnnotations filters a list of configs by a given annotation selector.
func (sc *ingressRouteSource) filterByAnnotations(ingressRoutes []*contour.IngressRoute) ([]*contour.IngressRoute, error) {
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
		return ingressRoutes, nil
	}

	filteredList := []*contour.IngressRoute{}

	for _, ingressRoute := range ingressRoutes {
		// convert the ingressroute's annotations to an equivalent label selector
		annotations := labels.Set(ingressRoute.Annotations)

		// include ingressroute if its annotations match the selector
		if selector.Matches(annotations) {
			filteredList = append(filteredList, ingressRoute)
		}
	}

	return filteredList, nil
}

func (sc *ingressRouteSource) setResourceLabel(ingressRoute *contour.IngressRoute, endpoints []*endpoint.Endpoint) {
	for _, ep := range endpoints {
		ep.Labels[endpoint.ResourceLabelKey] = fmt.Sprintf("ingressroute/%s/%s", ingressRoute.Namespace, ingressRoute.Name)
	}
}

func (sc *ingressRouteSource) targetsFromContourLoadBalancer(ctx context.Context) (targets endpoint.Targets, err error) {
	lbNamespace, lbName, err := parseContourLoadBalancerService(sc.contourLoadBalancerService)
	if err != nil {
		return nil, err
	}
	if svc, err := sc.kubeClient.CoreV1().Services(lbNamespace).Get(ctx, lbName, metav1.GetOptions{}); err != nil {
		log.Warn(err)
	} else {
		for _, lb := range svc.Status.LoadBalancer.Ingress {
			if lb.IP != "" {
				targets = append(targets, lb.IP)
			}
			if lb.Hostname != "" {
				targets = append(targets, lb.Hostname)
			}
		}
	}

	return
}

// endpointsFromIngressRouteConfig extracts the endpoints from a Contour IngressRoute object
func (sc *ingressRouteSource) endpointsFromIngressRoute(ctx context.Context, ingressRoute *contour.IngressRoute) ([]*endpoint.Endpoint, error) {
	if ingressRoute.CurrentStatus != "valid" {
		log.Warn(errors.Errorf("cannot generate endpoints for ingressroute with status %s", ingressRoute.CurrentStatus))
		return nil, nil
	}

	var endpoints []*endpoint.Endpoint

	ttl, err := getTTLFromAnnotations(ingressRoute.Annotations)
	if err != nil {
		log.Warn(err)
	}

	targets := getTargetsFromTargetAnnotation(ingressRoute.Annotations)

	if len(targets) == 0 {
		targets, err = sc.targetsFromContourLoadBalancer(ctx)
		if err != nil {
			return nil, err
		}
	}

	providerSpecific, setIdentifier := getProviderSpecificAnnotations(ingressRoute.Annotations)

	if virtualHost := ingressRoute.Spec.VirtualHost; virtualHost != nil {
		if fqdn := virtualHost.Fqdn; fqdn != "" {
			endpoints = append(endpoints, endpointsForHostname(fqdn, targets, ttl, providerSpecific, setIdentifier)...)
		}
	}

	// Skip endpoints if we do not want entries from annotations
	if !sc.ignoreHostnameAnnotation {
		hostnameList := getHostnamesFromAnnotations(ingressRoute.Annotations)
		for _, hostname := range hostnameList {
			endpoints = append(endpoints, endpointsForHostname(hostname, targets, ttl, providerSpecific, setIdentifier)...)
		}
	}

	return endpoints, nil
}

func parseContourLoadBalancerService(service string) (namespace, name string, err error) {
	parts := strings.Split(service, "/")
	if len(parts) != 2 {
		err = fmt.Errorf("invalid contour load balancer service (namespace/name) found '%v'", service)
	} else {
		namespace, name = parts[0], parts[1]
	}

	return
}

func (sc *ingressRouteSource) AddEventHandler(ctx context.Context, handler func()) {
}
