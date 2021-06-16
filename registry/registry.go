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

package registry

import (
	"context"

	log "github.com/sirupsen/logrus"

	"sigs.k8s.io/external-dns/endpoint"
	"sigs.k8s.io/external-dns/plan"
)

// Registry is an interface which should enables ownership concept in external-dns
// Records() returns ALL records registered with DNS provider
// each entry includes owner information
// ApplyChanges(changes *plan.Changes) propagates the changes to the DNS Provider API and correspondingly updates ownership depending on type of registry being used
type Registry interface {
	Records(ctx context.Context) ([]*endpoint.Endpoint, error)
	ApplyChanges(ctx context.Context, changes *plan.Changes) error
	PropertyValuesEqual(attribute string, previous string, current string) bool
	AdjustEndpoints(endpoints []*endpoint.Endpoint) []*endpoint.Endpoint
}

//TODO(ideahitme): consider moving this to Plan
func filterOwnedRecords(ownerID string, eps []*endpoint.Endpoint) []*endpoint.Endpoint {
	filtered := []*endpoint.Endpoint{}
	for _, ep := range eps {
		if endpointOwner, ok := ep.Labels[endpoint.OwnerLabelKey]; !ok || endpointOwner != ownerID || ep.OwnerClaimProhibited(ownerID) {
			if ep.ClaimStatus.ClaimRequested {
				log.Debugf(`Skipping endpoint %v because it is not permitted to claim owner id, found: "%s", required: "%s"`, ep, ep.ClaimStatus.PermittedOwner, ownerID)
				continue
			}
			log.Debugf(`Skipping endpoint %v because owner id does not match, found: "%s", required: "%s"`, ep, endpointOwner, ownerID)
			continue
		}
		filtered = append(filtered, ep)
	}
	return filtered
}

func filterLabels(ep *endpoint.Endpoint) {
	for k := range ep.Labels {
		switch k {
		case endpoint.ClaimLabelKey:
			delete(ep.Labels, k)
		case endpoint.PermitClaimByOwnerLabelKey:
			delete(ep.Labels, k)
		case endpoint.PermitClaimByResourceLabelKey:
			delete(ep.Labels, k)
		}
	}
}
