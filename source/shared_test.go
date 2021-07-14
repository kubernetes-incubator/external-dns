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
	"reflect"
	"sort"
	"strings"
	"testing"

	"sigs.k8s.io/external-dns/endpoint"
)

// test helper functions

func validateEndpoints(t *testing.T, endpoints, expected []*endpoint.Endpoint) {
	if len(endpoints) != len(expected) {
		t.Fatalf("expected %d endpoints, got %d", len(expected), len(endpoints))
	}
	// Make sure endpoints are sorted - validateEndpoint() depends on it.
	sort.SliceStable(endpoints, func(i, j int) bool {
		return strings.Compare(endpoints[i].DNSName, endpoints[j].DNSName) < 0
	})
	sort.SliceStable(expected, func(i, j int) bool {
		return strings.Compare(expected[i].DNSName, expected[j].DNSName) < 0
	})

	for i := range endpoints {
		validateEndpoint(t, endpoints[i], expected[i])
	}
}

func validateEndpoint(t *testing.T, endpoint, expected *endpoint.Endpoint) {
	if endpoint.DNSName != expected.DNSName {
		t.Errorf("expected %s, got %s", expected.DNSName, endpoint.DNSName)
	}

	if !endpoint.Targets.Same(expected.Targets) {
		t.Errorf("expected %s, got %s", expected.Targets, endpoint.Targets)
	}

	if endpoint.RecordTTL != expected.RecordTTL {
		t.Errorf("expected %v, got %v", expected.RecordTTL, endpoint.RecordTTL)
	}

	// if non-empty record type is expected, check that it matches.
	if expected.RecordType != "" && endpoint.RecordType != expected.RecordType {
		t.Errorf("expected %s, got %s", expected.RecordType, endpoint.RecordType)
	}

	// if non-empty labels are expected, check that they matches.
	if expected.Labels != nil && !reflect.DeepEqual(endpoint.Labels, expected.Labels) {
		t.Errorf("expected %s, got %s", expected.Labels, endpoint.Labels)
	}
}
