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

package testutils

import (
	"reflect"
	"sort"

	"sigs.k8s.io/external-dns/endpoint"
)

/** test utility functions for endpoints verifications */

type byNames endpoint.ProviderSpecific

func (p byNames) Len() int           { return len(p) }
func (p byNames) Swap(i, j int)      { p[i], p[j] = p[j], p[i] }
func (p byNames) Less(i, j int) bool { return p[i].Name < p[j].Name }

type byAllFields []*endpoint.Endpoint

func (b byAllFields) Len() int      { return len(b) }
func (b byAllFields) Swap(i, j int) { b[i], b[j] = b[j], b[i] }
func (b byAllFields) Less(i, j int) bool {
	if b[i].DNSName < b[j].DNSName {
		return true
	}
	if b[i].DNSName == b[j].DNSName {
		// This rather bad, we need a more complex comparison for Targets, which considers all elements
		if b[i].Targets.Same(b[j].Targets) {
			return b[i].RecordType <= b[j].RecordType
		}
		return b[i].Targets.String() <= b[j].Targets.String()
	}
	return false
}

// SameEndpoint returns true if two endpoints are same
// considers example.org. and example.org DNSName/Target as different endpoints
func SameEndpoint(a, b *endpoint.Endpoint) bool {
	return a.DNSName == b.DNSName && a.Targets.Same(b.Targets) && a.RecordType == b.RecordType && a.SetIdentifier == b.SetIdentifier &&
		a.Labels[endpoint.OwnerLabelKey] == b.Labels[endpoint.OwnerLabelKey] && a.RecordTTL == b.RecordTTL &&
		a.Labels[endpoint.ResourceLabelKey] == b.Labels[endpoint.ResourceLabelKey] &&
		a.Labels[endpoint.PermitClaimByOwnerLabelKey] == b.Labels[endpoint.PermitClaimByOwnerLabelKey] &&
		a.Labels[endpoint.PermitClaimByResourceLabelKey] == b.Labels[endpoint.PermitClaimByResourceLabelKey] &&
		SameProviderSpecific(a.ProviderSpecific, b.ProviderSpecific)
}

// SameEndpoints compares two slices of endpoints regardless of order
// [x,y,z] == [z,x,y]
// [x,x,z] == [x,z,x]
// [x,y,y] != [x,x,y]
// [x,x,x] != [x,x,z]
func SameEndpoints(a, b []*endpoint.Endpoint) bool {
	if len(a) != len(b) {
		return false
	}

	sa := a
	sb := b
	sort.Sort(byAllFields(sa))
	sort.Sort(byAllFields(sb))

	for i := range sa {
		if !SameEndpoint(sa[i], sb[i]) {
			return false
		}
	}
	return true
}

// SameEndpointLabels verifies that labels of the two slices of endpoints are the same
func SameEndpointLabels(a, b []*endpoint.Endpoint) bool {
	if len(a) != len(b) {
		return false
	}

	sa := a
	sb := b
	sort.Sort(byAllFields(sa))
	sort.Sort(byAllFields(sb))

	for i := range sa {
		if !reflect.DeepEqual(sa[i].Labels, sb[i].Labels) {
			return false
		}
	}
	return true
}

// SamePlanChanges verifies that two set of changes are the same
func SamePlanChanges(a, b map[string][]*endpoint.Endpoint) bool {
	return SameEndpoints(a["Create"], b["Create"]) && SameEndpoints(a["Delete"], b["Delete"]) &&
		SameEndpoints(a["UpdateOld"], b["UpdateOld"]) && SameEndpoints(a["UpdateNew"], b["UpdateNew"])
}

// SameProviderSpecific verifies that two maps contain the same string/string key/value pairs
func SameProviderSpecific(a, b endpoint.ProviderSpecific) bool {
	sa := a
	sb := b
	sort.Sort(byNames(sa))
	sort.Sort(byNames(sb))
	return reflect.DeepEqual(sa, sb)
}
