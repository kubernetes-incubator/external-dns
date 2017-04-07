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

package controller

import (
	"errors"
	"testing"

	"github.com/kubernetes-incubator/external-dns/endpoint"
	"github.com/kubernetes-incubator/external-dns/internal/testutils"
	"github.com/kubernetes-incubator/external-dns/plan"
	"github.com/kubernetes-incubator/external-dns/provider"
	"github.com/kubernetes-incubator/external-dns/registry"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockProvider returns mock endpoints and validates changes.
type mockProvider struct {
	RecordsStore  []*endpoint.Endpoint
	ExpectChanges *plan.Changes
}

// Records returns the desired mock endpoints.
func (p *mockProvider) Records() ([]*endpoint.Endpoint, error) {
	return p.RecordsStore, nil
}

// ApplyChanges validates that the passed in changes satisfy the assumtions.
func (p *mockProvider) ApplyChanges(changes *plan.Changes) error {
	if !testutils.SameEndpoints(p.ExpectChanges.Create, changes.Create) {
		return errors.New("created record is wrong")
	}

	if !testutils.SameEndpoints(p.ExpectChanges.UpdateNew, changes.UpdateNew) {
		return errors.New("created record is wrong")
	}

	if !testutils.SameEndpoints(p.ExpectChanges.UpdateOld, changes.UpdateOld) {
		return errors.New("created record is wrong")
	}

	if !testutils.SameEndpoints(p.ExpectChanges.Delete, changes.Delete) {
		return errors.New("created record is wrong")
	}

	return nil
}

// newMockProvider creates a new mockProvider returning the given endpoints and validating the desired changes.
func newMockProvider(endpoints []*endpoint.Endpoint, changes *plan.Changes) provider.Provider {
	dnsProvider := &mockProvider{
		RecordsStore:  endpoints,
		ExpectChanges: changes,
	}

	return dnsProvider
}

// TestRunOnce tests that RunOnce correctly orchestrates the different components.
func TestRunOnce(t *testing.T) {
	// Fake some desired endpoints coming from our source.
	source := new(testutils.MockSource)
	source.On("Endpoints").Return([]*endpoint.Endpoint{
		{
			DNSName: "create-record",
			Targets: []string{"1.2.3.4"},
		},
		{
			DNSName: "update-record",
			Targets: []string{"8.8.4.4"},
		},
	}, nil)

	// Fake some existing records in our DNS provider and validate some desired changes.
	provider := newMockProvider(
		[]*endpoint.Endpoint{
			{
				DNSName: "update-record",
				Targets: []string{"8.8.8.8"},
			},
			{
				DNSName: "delete-record",
				Targets: []string{"4.3.2.1"},
			},
		},
		&plan.Changes{
			Create: []*endpoint.Endpoint{
				{DNSName: "create-record", Targets: []string{"1.2.3.4"}},
			},
			UpdateNew: []*endpoint.Endpoint{
				{DNSName: "update-record", Targets: []string{"8.8.4.4"}},
			},
			UpdateOld: []*endpoint.Endpoint{
				{DNSName: "update-record", Targets: []string{"8.8.8.8"}},
			},
			Delete: []*endpoint.Endpoint{
				{DNSName: "delete-record", Targets: []string{"4.3.2.1"}},
			},
		},
	)

	r, err := registry.NewNoopRegistry(provider)
	require.NoError(t, err)

	// Run our controller once to trigger the validation.
	ctrl := &Controller{
		Source:   source,
		Registry: r,
		Policy:   &plan.SyncPolicy{},
	}

	assert.NoError(t, ctrl.RunOnce())

	// Validate that the mock source was called.
	source.AssertExpectations(t)
}
