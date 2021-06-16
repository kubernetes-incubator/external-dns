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
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"sync"
	"text/template"
	"time"

	log "github.com/sirupsen/logrus"

	"sigs.k8s.io/external-dns/endpoint"
)

const (
	defaultIdleConnTimeout = 30 * time.Second
	// DefaultRoutegroupVersion is the default version for route groups.
	DefaultRoutegroupVersion     = "zalando.org/v1"
	routeGroupListResource       = "/apis/%s/routegroups"
	routeGroupNamespacedResource = "/apis/%s/namespaces/%s/routegroups"
)

type routeGroupSource struct {
	cli                      routeGroupListClient
	apiServer                string
	namespace                string
	apiEndpoint              string
	annotationFilter         string
	fqdnTemplate             *template.Template
	combineFQDNAnnotation    bool
	ignoreHostnameAnnotation bool
}

// for testing
type routeGroupListClient interface {
	getRouteGroupList(string) (*routeGroupList, error)
}

type routeGroupClient struct {
	mu        sync.Mutex
	quit      chan struct{}
	client    *http.Client
	token     string
	tokenFile string
}

func newRouteGroupClient(token, tokenPath string, timeout time.Duration) *routeGroupClient {
	const (
		tokenFile  = "/var/run/secrets/kubernetes.io/serviceaccount/token"
		rootCAFile = "/var/run/secrets/kubernetes.io/serviceaccount/ca.crt"
	)
	if tokenPath != "" {
		tokenPath = tokenFile
	}

	tr := &http.Transport{
		DialContext: (&net.Dialer{
			Timeout:   timeout,
			KeepAlive: 30 * time.Second,
			DualStack: true,
		}).DialContext,
		TLSHandshakeTimeout:   3 * time.Second,
		ResponseHeaderTimeout: timeout,
		IdleConnTimeout:       defaultIdleConnTimeout,
		MaxIdleConns:          5,
		MaxIdleConnsPerHost:   5,
	}
	cli := &routeGroupClient{
		client: &http.Client{
			Transport: tr,
		},
		quit:      make(chan struct{}),
		tokenFile: tokenPath,
		token:     token,
	}

	go func() {
		for {
			select {
			case <-time.After(tr.IdleConnTimeout):
				tr.CloseIdleConnections()
				cli.updateToken()
			case <-cli.quit:
				return
			}
		}
	}()

	// in cluster config, errors are treated as not running in cluster
	cli.updateToken()

	// cluster internal use custom CA to reach TLS endpoint
	rootCA, err := ioutil.ReadFile(rootCAFile)
	if err != nil {
		return cli
	}
	certPool := x509.NewCertPool()
	if !certPool.AppendCertsFromPEM(rootCA) {
		return cli
	}

	tr.TLSClientConfig = &tls.Config{
		MinVersion: tls.VersionTLS12,
		RootCAs:    certPool,
	}

	return cli
}

func (cli *routeGroupClient) updateToken() {
	if cli.tokenFile == "" {
		return
	}

	token, err := ioutil.ReadFile(cli.tokenFile)
	if err != nil {
		log.Errorf("Failed to read token from file (%s): %v", cli.tokenFile, err)
		return
	}

	cli.mu.Lock()
	cli.token = string(token)
	cli.mu.Unlock()
}

func (cli *routeGroupClient) getToken() string {
	cli.mu.Lock()
	defer cli.mu.Unlock()
	return cli.token
}

func (cli *routeGroupClient) getRouteGroupList(url string) (*routeGroupList, error) {
	resp, err := cli.get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("failed to get routegroup list from %s, got: %s", url, resp.Status)
	}

	var rgs routeGroupList
	err = json.NewDecoder(resp.Body).Decode(&rgs)
	if err != nil {
		return nil, err
	}

	return &rgs, nil
}

func (cli *routeGroupClient) get(url string) (*http.Response, error) {
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}
	return cli.do(req)
}

func (cli *routeGroupClient) do(req *http.Request) (*http.Response, error) {
	if tok := cli.getToken(); tok != "" && req.Header.Get("Authorization") == "" {
		req.Header.Set("Authorization", "Bearer "+tok)
	}
	return cli.client.Do(req)
}

func parseTemplate(fqdnTemplate string) (tmpl *template.Template, err error) {
	if fqdnTemplate != "" {
		tmpl, err = template.New("endpoint").Funcs(template.FuncMap{
			"trimPrefix": strings.TrimPrefix,
		}).Parse(fqdnTemplate)
	}
	return tmpl, err
}

// NewRouteGroupSource creates a new routeGroupSource with the given config.
func NewRouteGroupSource(timeout time.Duration, token, tokenPath, apiServerURL, namespace, annotationFilter, fqdnTemplate, routegroupVersion string, combineFqdnAnnotation, ignoreHostnameAnnotation bool) (Source, error) {
	tmpl, err := parseTemplate(fqdnTemplate)
	if err != nil {
		return nil, err
	}

	if routegroupVersion == "" {
		routegroupVersion = DefaultRoutegroupVersion
	}
	cli := newRouteGroupClient(token, tokenPath, timeout)

	u, err := url.Parse(apiServerURL)
	if err != nil {
		return nil, err
	}

	apiServer := u.String()
	// strip port if well known port, because of TLS certificate match
	if u.Scheme == "https" && u.Port() == "443" {
		apiServer = "https://" + u.Hostname()
	}

	sc := &routeGroupSource{
		cli:                      cli,
		apiServer:                apiServer,
		namespace:                namespace,
		apiEndpoint:              apiServer + fmt.Sprintf(routeGroupListResource, routegroupVersion),
		annotationFilter:         annotationFilter,
		fqdnTemplate:             tmpl,
		combineFQDNAnnotation:    combineFqdnAnnotation,
		ignoreHostnameAnnotation: ignoreHostnameAnnotation,
	}
	if namespace != "" {
		sc.apiEndpoint = apiServer + fmt.Sprintf(routeGroupNamespacedResource, routegroupVersion, namespace)
	}

	log.Infoln("Created route group source")
	return sc, nil
}

// AddEventHandler for routegroup is currently a no op, because we do not implement caching, yet.
func (sc *routeGroupSource) AddEventHandler(ctx context.Context, handler func()) {}

// Endpoints returns endpoint objects for each host-target combination that should be processed.
// Retrieves all routeGroup resources on all namespaces.
// Logic is ported from ingress without fqdnTemplate
func (sc *routeGroupSource) Endpoints(ctx context.Context) ([]*endpoint.Endpoint, error) {
	rgList, err := sc.cli.getRouteGroupList(sc.apiEndpoint)
	if err != nil {
		log.Errorf("Failed to get RouteGroup list: %v", err)
		return nil, err
	}
	rgList, err = sc.filterByAnnotations(rgList)
	if err != nil {
		return nil, err
	}

	endpoints := []*endpoint.Endpoint{}
	for _, rg := range rgList.Items {
		// Check controller annotation to see if we are responsible.
		controller, ok := rg.Metadata.Annotations[controllerAnnotationKey]
		if ok && controller != controllerAnnotationValue {
			log.Debugf("Skipping routegroup %s/%s because controller value does not match, found: %s, required: %s",
				rg.Metadata.Namespace, rg.Metadata.Name, controller, controllerAnnotationValue)
			continue
		}

		eps := sc.endpointsFromRouteGroup(rg)

		if (sc.combineFQDNAnnotation || len(eps) == 0) && sc.fqdnTemplate != nil {
			tmplEndpoints, err := sc.endpointsFromTemplate(rg)
			if err != nil {
				return nil, err
			}

			if sc.combineFQDNAnnotation {
				eps = append(eps, tmplEndpoints...)
			} else {
				eps = tmplEndpoints
			}
		}

		if len(eps) == 0 {
			log.Debugf("No endpoints could be generated from routegroup %s/%s", rg.Metadata.Namespace, rg.Metadata.Name)
			continue
		}

		log.Debugf("Endpoints generated from ingress: %s/%s: %v", rg.Metadata.Namespace, rg.Metadata.Name, eps)
		sc.setRouteGroupResourceLabel(rg, eps)
		sc.setRouteGroupDualstackLabel(rg, eps)
		addClaimLabels(rg.Metadata.Annotations, eps)
		endpoints = append(endpoints, eps...)
	}

	for _, ep := range endpoints {
		sort.Sort(ep.Targets)
	}

	return endpoints, nil
}

func (sc *routeGroupSource) endpointsFromTemplate(rg *routeGroup) ([]*endpoint.Endpoint, error) {
	// Process the whole template string
	var buf bytes.Buffer
	err := sc.fqdnTemplate.Execute(&buf, rg)
	if err != nil {
		return nil, fmt.Errorf("failed to apply template on routegroup %s/%s: %v", rg.Metadata.Namespace, rg.Metadata.Name, err)
	}

	hostnames := buf.String()

	// error handled in endpointsFromRouteGroup(), otherwise duplicate log
	ttl, _ := getTTLFromAnnotations(rg.Metadata.Annotations)

	targets := getTargetsFromTargetAnnotation(rg.Metadata.Annotations)

	if len(targets) == 0 {
		targets = targetsFromRouteGroupStatus(rg.Status)
	}

	providerSpecific, setIdentifier := getProviderSpecificAnnotations(rg.Metadata.Annotations)

	var endpoints []*endpoint.Endpoint
	// splits the FQDN template and removes the trailing periods
	hostnameList := strings.Split(strings.Replace(hostnames, " ", "", -1), ",")
	for _, hostname := range hostnameList {
		hostname = strings.TrimSuffix(hostname, ".")
		endpoints = append(endpoints, endpointsForHostname(hostname, targets, ttl, providerSpecific, setIdentifier)...)
	}
	return endpoints, nil
}

func (sc *routeGroupSource) setRouteGroupResourceLabel(rg *routeGroup, eps []*endpoint.Endpoint) {
	for _, ep := range eps {
		ep.Labels[endpoint.ResourceLabelKey] = fmt.Sprintf("routegroup/%s/%s", rg.Metadata.Namespace, rg.Metadata.Name)
	}
}

func (sc *routeGroupSource) setRouteGroupDualstackLabel(rg *routeGroup, eps []*endpoint.Endpoint) {
	val, ok := rg.Metadata.Annotations[ALBDualstackAnnotationKey]
	if ok && val == ALBDualstackAnnotationValue {
		log.Debugf("Adding dualstack label to routegroup %s/%s.", rg.Metadata.Namespace, rg.Metadata.Name)
		for _, ep := range eps {
			ep.Labels[endpoint.DualstackLabelKey] = "true"
		}
	}
}

// annotation logic ported from source/ingress.go without Spec.TLS part, because it'S not supported in RouteGroup
func (sc *routeGroupSource) endpointsFromRouteGroup(rg *routeGroup) []*endpoint.Endpoint {
	endpoints := []*endpoint.Endpoint{}
	ttl, err := getTTLFromAnnotations(rg.Metadata.Annotations)
	if err != nil {
		log.Warnf("Failed to get TTL from annotation: %v", err)
	}

	targets := getTargetsFromTargetAnnotation(rg.Metadata.Annotations)
	if len(targets) == 0 {
		for _, lb := range rg.Status.LoadBalancer.RouteGroup {
			if lb.IP != "" {
				targets = append(targets, lb.IP)
			}
			if lb.Hostname != "" {
				targets = append(targets, lb.Hostname)
			}
		}
	}

	providerSpecific, setIdentifier := getProviderSpecificAnnotations(rg.Metadata.Annotations)

	for _, src := range rg.Spec.Hosts {
		if src == "" {
			continue
		}
		endpoints = append(endpoints, endpointsForHostname(src, targets, ttl, providerSpecific, setIdentifier)...)
	}

	// Skip endpoints if we do not want entries from annotations
	if !sc.ignoreHostnameAnnotation {
		hostnameList := getHostnamesFromAnnotations(rg.Metadata.Annotations)
		for _, hostname := range hostnameList {
			endpoints = append(endpoints, endpointsForHostname(hostname, targets, ttl, providerSpecific, setIdentifier)...)
		}
	}
	return endpoints
}

// filterByAnnotations filters a list of routeGroupList by a given annotation selector.
func (sc *routeGroupSource) filterByAnnotations(rgs *routeGroupList) (*routeGroupList, error) {
	selector, err := getLabelSelector(sc.annotationFilter)
	if err != nil {
		return nil, err
	}

	// empty filter returns original list
	if selector.Empty() {
		return rgs, nil
	}

	var filteredList []*routeGroup
	for _, rg := range rgs.Items {
		// include ingress if its annotations match the selector
		if matchLabelSelector(selector, rg.Metadata.Annotations) {
			filteredList = append(filteredList, rg)
		}
	}
	rgs.Items = filteredList

	return rgs, nil
}

func targetsFromRouteGroupStatus(status routeGroupStatus) endpoint.Targets {
	var targets endpoint.Targets

	for _, lb := range status.LoadBalancer.RouteGroup {
		if lb.IP != "" {
			targets = append(targets, lb.IP)
		}
		if lb.Hostname != "" {
			targets = append(targets, lb.Hostname)
		}
	}

	return targets
}

type routeGroupList struct {
	Kind       string                 `json:"kind"`
	APIVersion string                 `json:"apiVersion"`
	Metadata   routeGroupListMetadata `json:"metadata"`
	Items      []*routeGroup          `json:"items"`
}

type routeGroupListMetadata struct {
	SelfLink        string `json:"selfLink"`
	ResourceVersion string `json:"resourceVersion"`
}

type routeGroup struct {
	Metadata itemMetadata     `json:"metadata"`
	Spec     routeGroupSpec   `json:"spec"`
	Status   routeGroupStatus `json:"status"`
}

type itemMetadata struct {
	Namespace   string            `json:"namespace"`
	Name        string            `json:"name"`
	Annotations map[string]string `json:"annotations"`
}

type routeGroupSpec struct {
	Hosts []string `json:"hosts"`
}

type routeGroupStatus struct {
	LoadBalancer routeGroupLoadBalancerStatus `json:"loadBalancer"`
}

type routeGroupLoadBalancerStatus struct {
	RouteGroup []routeGroupLoadBalancer `json:"routeGroup"`
}

type routeGroupLoadBalancer struct {
	IP       string `json:"ip,omitempty"`
	Hostname string `json:"hostname,omitempty"`
}
