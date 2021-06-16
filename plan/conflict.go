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

package plan

import (
	"sort"

	"sigs.k8s.io/external-dns/endpoint"
)

// ConflictResolver is used to make a decision in case of two or more different kubernetes resources
// are trying to acquire same DNS name
type ConflictResolver interface {
	ResolveCreate(candidates []*endpoint.Endpoint) *endpoint.Endpoint
	ResolveUpdate(current *endpoint.Endpoint, candidates []*endpoint.Endpoint) *endpoint.Endpoint
}

// PerResource allows only one resource to own a given dns name
type PerResource struct{}

// ResolveCreate is invoked when dns name is not owned by any resource
// ResolveCreate takes "minimal" (string comparison of Target) endpoint to acquire the DNS record
func (s PerResource) ResolveCreate(candidates []*endpoint.Endpoint) *endpoint.Endpoint {
	var min *endpoint.Endpoint
	for _, ep := range candidates {
		if min == nil || s.less(ep, min) {
			min = ep
		}
	}
	return min
}

// ResolveUpdate is invoked when dns name is already owned by "current" endpoint
// ResolveUpdate uses "current" record as base and updates it accordingly with new version of same resource
// if it doesn't exist then pick min
func (s PerResource) ResolveUpdate(current *endpoint.Endpoint, candidates []*endpoint.Endpoint) *endpoint.Endpoint {
	currentResource := current.Labels[endpoint.ResourceLabelKey] // resource which has already acquired the DNS
	// TODO: sort candidates only needed because we can still have two endpoints from same resource here. We sort for consistency
	// TODO: remove once single endpoint can have multiple targets
	sort.SliceStable(candidates, func(i, j int) bool {
		return s.less(candidates[i], candidates[j])
	})

	// we are dealing with a resource that has specific permissions set on who may claim ownership
	// if there are endpoints present that are eligible to do so
	if claimPermittedResource, ok := current.Labels[endpoint.PermitClaimByResourceLabelKey]; ok {
		var candidate *endpoint.Endpoint
		for _, ep := range candidates {
			if ep.Labels[endpoint.ResourceLabelKey] == currentResource {
				if candidate == nil {
					candidate = ep
				}
				continue
			}
			if claim, ok := ep.Labels[endpoint.ClaimLabelKey]; ok && claim == "true" {
				if ep.Labels[endpoint.ResourceLabelKey] == claimPermittedResource {
					ep.ClaimStatus.ClaimRequested = true
					// if no explicit owner claim permission is set, assume
					// that only the current owner of the record is permitted to claim a resource
					ep.ClaimStatus.PermittedOwner = current.Labels[endpoint.OwnerLabelKey]
					if claimPermittedOwner, ok := current.Labels[endpoint.PermitClaimByOwnerLabelKey]; ok {
						ep.ClaimStatus.PermittedOwner = claimPermittedOwner
					}
					return ep
				}
			}
		}

		if candidate != nil {
			return candidate
		}
		return current
	}

	for _, ep := range candidates {
		if ep.Labels[endpoint.ResourceLabelKey] == currentResource {
			return ep
		}
	}
	return s.ResolveCreate(candidates)
}

// less returns true if endpoint x is less than y
func (s PerResource) less(x, y *endpoint.Endpoint) bool {
	return x.Targets.IsLess(y.Targets)
}

// TODO: with cross-resource/cross-cluster setup alternative variations of ConflictResolver can be used
