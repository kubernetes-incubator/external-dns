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

	routeapi "github.com/openshift/api/route/v1"
	versioned "github.com/openshift/client-go/route/clientset/versioned"
	extInformers "github.com/openshift/client-go/route/informers/externalversions"
	routeInformer "github.com/openshift/client-go/route/informers/externalversions/route/v1"
	log "github.com/sirupsen/logrus"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/tools/cache"

	"sigs.k8s.io/external-dns/endpoint"
)

// ocpRouteSource is an implementation of Source for OpenShift Route objects.
// Route implementation will use the spec.host value for the hostname
// Use targetAnnotationKey to explicitly set Endpoint. (useful if the router
// does not update, or to override with alternative endpoint)
type ocpRouteSource struct {
	client                   versioned.Interface
	namespace                string
	annotationFilter         string
	fqdnTemplate             *template.Template
	combineFQDNAnnotation    bool
	ignoreHostnameAnnotation bool
	routeInformer            routeInformer.RouteInformer
}

// NewOcpRouteSource creates a new ocpRouteSource with the given config.
func NewOcpRouteSource(
	ocpClient versioned.Interface,
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

	// Use shared informer to listen for add/update/delete of Routes in the specified namespace.
	// Set resync period to 0, to prevent processing when nothing has changed.
	informerFactory := extInformers.NewFilteredSharedInformerFactory(ocpClient, 0, namespace, nil)
	routeInformer := informerFactory.Route().V1().Routes()

	// Add default resource event handlers to properly initialize informer.
	routeInformer.Informer().AddEventHandler(
		cache.ResourceEventHandlerFuncs{
			AddFunc: func(obj interface{}) {
			},
		},
	)

	// TODO informer is not explicitly stopped since controller is not passing in its channel.
	informerFactory.Start(wait.NeverStop)

	// wait for the local cache to be populated.
	err = poll(time.Second, 60*time.Second, func() (bool, error) {
		return routeInformer.Informer().HasSynced(), nil
	})
	if err != nil {
		return nil, fmt.Errorf("failed to sync cache: %v", err)
	}

	return &ocpRouteSource{
		client:                   ocpClient,
		namespace:                namespace,
		annotationFilter:         annotationFilter,
		fqdnTemplate:             tmpl,
		combineFQDNAnnotation:    combineFQDNAnnotation,
		ignoreHostnameAnnotation: ignoreHostnameAnnotation,
		routeInformer:            routeInformer,
	}, nil
}

// TODO add a meaningful EventHandler
func (ors *ocpRouteSource) AddEventHandler(ctx context.Context, handler func()) {
}

// Endpoints returns endpoint objects for each host-target combination that should be processed.
// Retrieves all OpenShift Route resources on all namespaces
func (ors *ocpRouteSource) Endpoints(ctx context.Context) ([]*endpoint.Endpoint, error) {
	ocpRoutes, err := ors.routeInformer.Lister().Routes(ors.namespace).List(labels.Everything())
	if err != nil {
		return nil, err
	}

	ocpRoutes, err = ors.filterByAnnotations(ocpRoutes)
	if err != nil {
		return nil, err
	}

	endpoints := []*endpoint.Endpoint{}

	for _, ocpRoute := range ocpRoutes {
		// Check controller annotation to see if we are responsible.
		controller, ok := ocpRoute.Annotations[controllerAnnotationKey]
		if ok && controller != controllerAnnotationValue {
			log.Debugf("Skipping OpenShift Route %s/%s because controller value does not match, found: %s, required: %s",
				ocpRoute.Namespace, ocpRoute.Name, controller, controllerAnnotationValue)
			continue
		}

		orEndpoints := endpointsFromOcpRoute(ocpRoute, ors.ignoreHostnameAnnotation)

		// apply template if host is missing on OpenShift Route
		if (ors.combineFQDNAnnotation || len(orEndpoints) == 0) && ors.fqdnTemplate != nil {
			oEndpoints, err := ors.endpointsFromTemplate(ocpRoute)
			if err != nil {
				return nil, err
			}

			if ors.combineFQDNAnnotation {
				orEndpoints = append(orEndpoints, oEndpoints...)
			} else {
				orEndpoints = oEndpoints
			}
		}

		if len(orEndpoints) == 0 {
			log.Debugf("No endpoints could be generated from OpenShift Route %s/%s", ocpRoute.Namespace, ocpRoute.Name)
			continue
		}

		log.Debugf("Endpoints generated from OpenShift Route: %s/%s: %v", ocpRoute.Namespace, ocpRoute.Name, orEndpoints)
		ors.setResourceLabel(ocpRoute, orEndpoints)
		addClaimLabels(ocpRoute.Annotations, orEndpoints)
		endpoints = append(endpoints, orEndpoints...)
	}

	for _, ep := range endpoints {
		sort.Sort(ep.Targets)
	}

	return endpoints, nil
}

func (ors *ocpRouteSource) endpointsFromTemplate(ocpRoute *routeapi.Route) ([]*endpoint.Endpoint, error) {
	// Process the whole template string
	var buf bytes.Buffer
	err := ors.fqdnTemplate.Execute(&buf, ocpRoute)
	if err != nil {
		return nil, fmt.Errorf("failed to apply template on OpenShift Route %s: %s", ocpRoute.Name, err)
	}

	hostnames := buf.String()

	ttl, err := getTTLFromAnnotations(ocpRoute.Annotations)
	if err != nil {
		log.Warn(err)
	}

	targets := getTargetsFromTargetAnnotation(ocpRoute.Annotations)

	if len(targets) == 0 {
		targets = targetsFromOcpRouteStatus(ocpRoute.Status)
	}

	providerSpecific, setIdentifier := getProviderSpecificAnnotations(ocpRoute.Annotations)

	var endpoints []*endpoint.Endpoint
	// splits the FQDN template and removes the trailing periods
	hostnameList := strings.Split(strings.Replace(hostnames, " ", "", -1), ",")
	for _, hostname := range hostnameList {
		hostname = strings.TrimSuffix(hostname, ".")
		endpoints = append(endpoints, endpointsForHostname(hostname, targets, ttl, providerSpecific, setIdentifier)...)
	}
	return endpoints, nil
}

func (ors *ocpRouteSource) filterByAnnotations(ocpRoutes []*routeapi.Route) ([]*routeapi.Route, error) {
	labelSelector, err := metav1.ParseToLabelSelector(ors.annotationFilter)
	if err != nil {
		return nil, err
	}
	selector, err := metav1.LabelSelectorAsSelector(labelSelector)
	if err != nil {
		return nil, err
	}

	// empty filter returns original list
	if selector.Empty() {
		return ocpRoutes, nil
	}

	filteredList := []*routeapi.Route{}

	for _, ocpRoute := range ocpRoutes {
		// convert the Route's annotations to an equivalent label selector
		annotations := labels.Set(ocpRoute.Annotations)

		// include ocpRoute if its annotations match the selector
		if selector.Matches(annotations) {
			filteredList = append(filteredList, ocpRoute)
		}
	}

	return filteredList, nil
}

func (ors *ocpRouteSource) setResourceLabel(ocpRoute *routeapi.Route, endpoints []*endpoint.Endpoint) {
	for _, ep := range endpoints {
		ep.Labels[endpoint.ResourceLabelKey] = fmt.Sprintf("route/%s/%s", ocpRoute.Namespace, ocpRoute.Name)
	}
}

// endpointsFromOcpRoute extracts the endpoints from a OpenShift Route object
func endpointsFromOcpRoute(ocpRoute *routeapi.Route, ignoreHostnameAnnotation bool) []*endpoint.Endpoint {
	var endpoints []*endpoint.Endpoint

	ttl, err := getTTLFromAnnotations(ocpRoute.Annotations)
	if err != nil {
		log.Warn(err)
	}

	targets := getTargetsFromTargetAnnotation(ocpRoute.Annotations)

	if len(targets) == 0 {
		targets = targetsFromOcpRouteStatus(ocpRoute.Status)
	}

	providerSpecific, setIdentifier := getProviderSpecificAnnotations(ocpRoute.Annotations)

	if host := ocpRoute.Spec.Host; host != "" {
		endpoints = append(endpoints, endpointsForHostname(host, targets, ttl, providerSpecific, setIdentifier)...)
	}

	// Skip endpoints if we do not want entries from annotations
	if !ignoreHostnameAnnotation {
		hostnameList := getHostnamesFromAnnotations(ocpRoute.Annotations)
		for _, hostname := range hostnameList {
			endpoints = append(endpoints, endpointsForHostname(hostname, targets, ttl, providerSpecific, setIdentifier)...)
		}
	}
	return endpoints
}

func targetsFromOcpRouteStatus(status routeapi.RouteStatus) endpoint.Targets {
	var targets endpoint.Targets

	for _, ing := range status.Ingress {
		if ing.RouterCanonicalHostname != "" {
			targets = append(targets, ing.RouterCanonicalHostname)
		}
	}

	return targets
}
