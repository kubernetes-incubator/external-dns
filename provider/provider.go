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

package provider

import (
	"context"
	"net"
	"strings"

	"sigs.k8s.io/external-dns/endpoint"
	"sigs.k8s.io/external-dns/plan"
)

// Provider defines the interface DNS providers should implement.
type Provider interface {
	Records(ctx context.Context) ([]*endpoint.Endpoint, error)
	ApplyChanges(ctx context.Context, changes *plan.Changes) error
	PropertyValuesEqual(name string, previous string, current string) bool
	AdjustEndpoints(endpoints []*endpoint.Endpoint) []*endpoint.Endpoint
	GetDomainFilter() endpoint.DomainFilterInterface
}

type BaseProvider struct {
}

func (b BaseProvider) AdjustEndpoints(endpoints []*endpoint.Endpoint) []*endpoint.Endpoint {
	return endpoints
}

func (b BaseProvider) PropertyValuesEqual(name, previous, current string) bool {
	return previous == current
}

func (b BaseProvider) GetDomainFilter() endpoint.DomainFilterInterface {
	return endpoint.DomainFilter{}
}

type contextKey struct {
	name string
}

func (k *contextKey) String() string { return "provider context value " + k.name }

// RecordsContextKey is a context key. It can be used during ApplyChanges
// to access previously cached records. The associated value will be of
// type []*endpoint.Endpoint.
var RecordsContextKey = &contextKey{"records"}

// EndpointsContextKey is a context key. It can be used during loading records
// from dns provider, for filter by only registered in k8s domains
// type []*endpoint.Endpoint.
var EndpointsContextKey = &contextKey{"endpoints"}

// EnsureTrailingDot ensures that the hostname receives a trailing dot if it hasn't already.
func EnsureTrailingDot(hostname string) string {
	if net.ParseIP(hostname) != nil {
		return hostname
	}

	return strings.TrimSuffix(hostname, ".") + "."
}

// Difference tells which entries need to be respectively
// added, removed, or left untouched for "current" to be transformed to "desired"
func Difference(current, desired []string) ([]string, []string, []string) {
	add, remove, leave := []string{}, []string{}, []string{}
	index := make(map[string]struct{}, len(current))
	for _, x := range current {
		index[x] = struct{}{}
	}
	for _, x := range desired {
		if _, found := index[x]; found {
			leave = append(leave, x)
			delete(index, x)
		} else {
			add = append(add, x)
			delete(index, x)
		}
	}
	for x := range index {
		remove = append(remove, x)
	}
	return add, remove, leave
}
