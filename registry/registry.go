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
	log "github.com/Sirupsen/logrus"
	"github.com/kubernetes-incubator/external-dns/endpoint"
	"github.com/kubernetes-incubator/external-dns/plan"
)

// Registry is an interface which should enables ownership concept in external-dns
// Record(zone string) returns ALL records registered with DNS provider (TODO: for multi-zone support return all records)
// each entry includes owner information
// ApplyChanges(zone string, changes *plan.Changes) propagates the changes to the DNS Provider API and correspondingly updates ownership depending on type of registry being used
type Registry interface {
	Records(zone string) ([]*endpoint.Endpoint, error)
	ApplyChanges(zone string, changes *plan.Changes) error
}

//TODO(ideahitme): consider moving this to Plan
func filterOwnedRecords(ownerID string, eps []*endpoint.Endpoint) []*endpoint.Endpoint {
	filtered := []*endpoint.Endpoint{}
	for _, ep := range eps {
		if ep.Labels[endpoint.OwnerLabelKey] == ownerID {
			filtered = append(filtered, ep)
		}
	}
	return filtered
}

func logChanges(changes *plan.Changes) {
	for _, change := range changes.Create {
		log.Infof("Creating %s %s -> %s ..", change.RecordType, change.DNSName, change.Target)
	}
	for _, change := range changes.UpdateNew {
		log.Infof("Updating %s %s -> %s ..", change.RecordType, change.DNSName, change.Target)
	}
	for _, change := range changes.Delete {
		log.Infof("Deleting %s %s -> %s ..", change.RecordType, change.DNSName, change.Target)
	}
}
