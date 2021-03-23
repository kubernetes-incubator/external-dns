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
