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
	"net"
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

type nodeSource struct {
	client           kubernetes.Interface
	annotationFilter string
	fqdnTemplate     *template.Template
	nodeInformer     coreinformers.NodeInformer
}

// NewNodeSource creates a new nodeSource with the given config.
func NewNodeSource(kubeClient kubernetes.Interface, annotationFilter, fqdnTemplate string) (Source, error) {
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

	// Use shared informers to listen for add/update/delete of nodes.
	// Set resync period to 0, to prevent processing when nothing has changed
	informerFactory := kubeinformers.NewSharedInformerFactoryWithOptions(kubeClient, 0)
	nodeInformer := informerFactory.Core().V1().Nodes()

	// Add default resource event handler to properly initialize informer.
	nodeInformer.Informer().AddEventHandler(
		cache.ResourceEventHandlerFuncs{
			AddFunc: func(obj interface{}) {
				log.Debug("node added")
			},
		},
	)

	// TODO informer is not explicitly stopped since controller is not passing in its channel.
	informerFactory.Start(wait.NeverStop)

	// wait for the local cache to be populated.
	err = poll(time.Second, 60*time.Second, func() (bool, error) {
		return nodeInformer.Informer().HasSynced(), nil
	})
	if err != nil {
		return nil, fmt.Errorf("failed to sync cache: %v", err)
	}

	return &nodeSource{
		client:           kubeClient,
		annotationFilter: annotationFilter,
		fqdnTemplate:     tmpl,
		nodeInformer:     nodeInformer,
	}, nil
}

// Endpoints returns endpoint objects for each service that should be processed.
func (ns *nodeSource) Endpoints(ctx context.Context) ([]*endpoint.Endpoint, error) {
	nodes, err := ns.nodeInformer.Lister().List(labels.Everything())
	if err != nil {
		return nil, err
	}

	nodes, err = ns.filterByAnnotations(nodes)
	if err != nil {
		return nil, err
	}

	endpoints := map[string]map[string]*endpoint.Endpoint{}

	// create endpoints for all nodes
	for _, node := range nodes {
		// Check controller annotation to see if we are responsible.
		controller, ok := node.Annotations[controllerAnnotationKey]
		if ok && controller != controllerAnnotationValue {
			log.Debugf("Skipping node %s because controller value does not match, found: %s, required: %s",
				node.Name, controller, controllerAnnotationValue)
			continue
		}

		log.Debugf("creating endpoint for node %s", node.Name)

		ttl, err := getTTLFromAnnotations(node.Annotations)
		if err != nil {
			log.Warn(err)
		}

		var dnsName string

		if ns.fqdnTemplate != nil {
			// Process the whole template string
			var buf bytes.Buffer
			err := ns.fqdnTemplate.Execute(&buf, node)
			if err != nil {
				return nil, fmt.Errorf("failed to apply template on node %s: %v", node.Name, err)
			}

			dnsName = buf.String()
			log.Debugf("applied template for %s, converting to %s", node.Name, dnsName)
		} else {
			dnsName = node.Name
			log.Debugf("not applying template for %s", node.Name)
		}

		addrs, err := ns.nodeAddresses(node)
		if err != nil {
			return nil, fmt.Errorf("failed to get node address from %s: %s", node.Name, err.Error())
		}

		// Categorize node addresses by necessary record type
		var v4Addrs endpoint.Targets
		var v6Addrs endpoint.Targets
		for _, addr := range addrs {
			if isIPv6String(addr) {
				v6Addrs = append(v6Addrs, addr)
			} else {
				v4Addrs = append(v4Addrs, addr)
			}
		}

		var nodeEps []*endpoint.Endpoint
		if len(v4Addrs) > 0 {
			nodeEps = append(nodeEps, &endpoint.Endpoint{
				DNSName:    dnsName,
				RecordType: "A",
				RecordTTL:  ttl,
				Targets:    v4Addrs,
			})
		}
		if len(v6Addrs) > 0 {
			nodeEps = append(nodeEps, &endpoint.Endpoint{
				DNSName:    dnsName,
				RecordType: "AAAA",
				RecordTTL:  ttl,
				Targets:    v6Addrs,
			})
		}

		for _, ep := range nodeEps {
			ep.Labels = endpoint.NewLabels()

			log.Debugf("adding endpoint %s", ep)
			if typeMap, ok := endpoints[ep.DNSName]; ok {
				if _, ok := typeMap[ep.RecordType]; ok {
					typeMap[ep.RecordType].Targets = append(typeMap[ep.RecordType].Targets, ep.Targets...)
				} else {
					typeMap[ep.RecordType] = ep
				}
			} else {
				endpoints[ep.DNSName] = make(map[string]*endpoint.Endpoint)
				endpoints[ep.DNSName][ep.RecordType] = ep
			}
		}
	}

	endpointsSlice := []*endpoint.Endpoint{}
	for _, epType := range endpoints {
		for _, ep := range epType {
			endpointsSlice = append(endpointsSlice, ep)
		}
	}

	return endpointsSlice, nil
}

func (ns *nodeSource) AddEventHandler(ctx context.Context, handler func()) {
}

// nodeAddress returns node's externalIP and if that's not found, node's internalIP
// basically what k8s.io/kubernetes/pkg/util/node.GetPreferredNodeAddress does
func (ns *nodeSource) nodeAddresses(node *v1.Node) ([]string, error) {
	addresses := map[v1.NodeAddressType][]string{
		v1.NodeExternalIP: {},
		v1.NodeInternalIP: {},
	}

	for _, addr := range node.Status.Addresses {
		addresses[addr.Type] = append(addresses[addr.Type], addr.Address)
	}

	if len(addresses[v1.NodeExternalIP]) > 0 {
		return addresses[v1.NodeExternalIP], nil
	}

	if len(addresses[v1.NodeInternalIP]) > 0 {
		return addresses[v1.NodeInternalIP], nil
	}

	return nil, fmt.Errorf("could not find node address for %s", node.Name)
}

// filterByAnnotations filters a list of nodes by a given annotation selector.
func (ns *nodeSource) filterByAnnotations(nodes []*v1.Node) ([]*v1.Node, error) {
	labelSelector, err := metav1.ParseToLabelSelector(ns.annotationFilter)
	if err != nil {
		return nil, err
	}
	selector, err := metav1.LabelSelectorAsSelector(labelSelector)
	if err != nil {
		return nil, err
	}

	// empty filter returns original list
	if selector.Empty() {
		return nodes, nil
	}

	filteredList := []*v1.Node{}

	for _, node := range nodes {
		// convert the node's annotations to an equivalent label selector
		annotations := labels.Set(node.Annotations)

		// include node if its annotations match the selector
		if selector.Matches(annotations) {
			filteredList = append(filteredList, node)
		}
	}

	return filteredList, nil
}

// isIPv6String returns if ip is IPv6.
func isIPv6String(ip string) bool {
	netIP := net.ParseIP(ip)
	return netIP != nil && netIP.To4() == nil
}
