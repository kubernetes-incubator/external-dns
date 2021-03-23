/*
Copyright 2018 The Kubernetes Authors.

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
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"sigs.k8s.io/external-dns/endpoint"
	"sigs.k8s.io/external-dns/internal/testutils"
	"sigs.k8s.io/external-dns/plan"
	"sigs.k8s.io/external-dns/provider"
)

type inMemoryProvider struct {
	provider.BaseProvider
	endpoints      []*endpoint.Endpoint
	onApplyChanges func(changes *plan.Changes)
}

func (p *inMemoryProvider) Records(ctx context.Context) ([]*endpoint.Endpoint, error) {
	return p.endpoints, nil
}

func (p *inMemoryProvider) ApplyChanges(ctx context.Context, changes *plan.Changes) error {
	p.onApplyChanges(changes)
	return nil
}

type erroredRecordsProvider struct {
	provider.BaseProvider
}

func (p *erroredRecordsProvider) Records(ctx context.Context) ([]*endpoint.Endpoint, error) {
	return nil, errors.New("testing-error")
}

func (p *erroredRecordsProvider) ApplyChanges(ctx context.Context, changes *plan.Changes) error {
	return errors.New("testing-error")
}

func newInMemoryProvider(endpoints []*endpoint.Endpoint, onApplyChanges func(changes *plan.Changes)) *inMemoryProvider {
	return &inMemoryProvider{
		endpoints:      endpoints,
		onApplyChanges: onApplyChanges,
	}
}

func TestAWSSDRegistry_NewAWSSDRegistry(t *testing.T) {
	p := newInMemoryProvider(nil, nil)

	_, err := NewAWSSDRegistry(p)
	require.NoError(t, err)
}

func TestAWSSDRegistryTest_Records(t *testing.T) {
	p := newInMemoryProvider([]*endpoint.Endpoint{
		newEndpointWithOwnerAndDescription("foo1.test-zone.example.org", "1.2.3.4", endpoint.RecordTypeA, "", ""),
		newEndpointWithOwnerAndDescription("foo2.test-zone.example.org", "1.2.3.4", endpoint.RecordTypeA, "owner", "\"heritage=external-dns,external-dns/owner=owner\""),
		newEndpointWithOwnerAndDescription("foo3.test-zone.example.org", "my-domain.com", endpoint.RecordTypeCNAME, "", ""),
		newEndpointWithOwnerAndDescription("foo4.test-zone.example.org", "my-domain.com", endpoint.RecordTypeCNAME, "owner", "\"heritage=external-dns,external-dns/owner=owner\""),
	}, nil)
	expectedRecords := []*endpoint.Endpoint{
		{
			DNSName:    "foo1.test-zone.example.org",
			Targets:    endpoint.Targets{"1.2.3.4"},
			RecordType: endpoint.RecordTypeA,
			Labels: map[string]string{
				endpoint.OwnerLabelKey: "",
			},
		},
		{
			DNSName:    "foo2.test-zone.example.org",
			Targets:    endpoint.Targets{"1.2.3.4"},
			RecordType: endpoint.RecordTypeA,
			Labels: map[string]string{
				endpoint.OwnerLabelKey: "owner",
			},
		},
		{
			DNSName:    "foo3.test-zone.example.org",
			Targets:    endpoint.Targets{"my-domain.com"},
			RecordType: endpoint.RecordTypeCNAME,
			Labels: map[string]string{
				endpoint.OwnerLabelKey: "",
			},
		},
		{
			DNSName:    "foo4.test-zone.example.org",
			Targets:    endpoint.Targets{"my-domain.com"},
			RecordType: endpoint.RecordTypeCNAME,
			Labels: map[string]string{
				endpoint.OwnerLabelKey: "owner",
			},
		},
	}

	r, _ := NewAWSSDRegistry(p)
	records, _ := r.Records(context.Background())

	assert.True(t, testutils.SameEndpoints(records, expectedRecords))

	r, err := NewAWSSDRegistry(&erroredRecordsProvider{})
	require.NoError(t, err)
	records, err = r.Records(context.Background())
	assert.Nil(t, records)
	assert.Error(t, err)
}

func TestAWSSDRegistry_Records_ApplyChanges(t *testing.T) {
	changes := &plan.Changes{
		Create: []*endpoint.Endpoint{
			newEndpointWithOwner("new-record-1.test-zone.example.org", "new-loadbalancer-1.lb.com", endpoint.RecordTypeCNAME, "owner"),
		},
		Delete: []*endpoint.Endpoint{
			newEndpointWithOwner("foobar.test-zone.example.org", "1.2.3.4", endpoint.RecordTypeA, "owner"),
		},
		UpdateNew: []*endpoint.Endpoint{
			newEndpointWithOwner("tar.test-zone.example.org", "new-tar.loadbalancer.com", endpoint.RecordTypeCNAME, "owner"),
		},
		UpdateOld: []*endpoint.Endpoint{
			newEndpointWithOwner("tar.test-zone.example.org", "tar.loadbalancer.com", endpoint.RecordTypeCNAME, "owner"),
		},
	}
	expected := &plan.Changes{
		Create: []*endpoint.Endpoint{
			newEndpointWithOwnerAndDescription("new-record-1.test-zone.example.org", "new-loadbalancer-1.lb.com", endpoint.RecordTypeCNAME, "owner", "\"heritage=external-dns,external-dns/owner=owner\""),
		},
		Delete: []*endpoint.Endpoint{
			newEndpointWithOwnerAndDescription("foobar.test-zone.example.org", "1.2.3.4", endpoint.RecordTypeA, "owner", "\"heritage=external-dns,external-dns/owner=owner\""),
		},
		UpdateNew: []*endpoint.Endpoint{
			newEndpointWithOwnerAndDescription("tar.test-zone.example.org", "new-tar.loadbalancer.com", endpoint.RecordTypeCNAME, "owner", "\"heritage=external-dns,external-dns/owner=owner\""),
		},
		UpdateOld: []*endpoint.Endpoint{
			newEndpointWithOwnerAndDescription("tar.test-zone.example.org", "tar.loadbalancer.com", endpoint.RecordTypeCNAME, "owner", "\"heritage=external-dns,external-dns/owner=owner\""),
		},
	}
	p := newInMemoryProvider(nil, func(got *plan.Changes) {
		mExpected := map[string][]*endpoint.Endpoint{
			"Create":    expected.Create,
			"UpdateNew": expected.UpdateNew,
			"UpdateOld": expected.UpdateOld,
			"Delete":    expected.Delete,
		}
		mGot := map[string][]*endpoint.Endpoint{
			"Create":    got.Create,
			"UpdateNew": got.UpdateNew,
			"UpdateOld": got.UpdateOld,
			"Delete":    got.Delete,
		}
		assert.True(t, testutils.SamePlanChanges(mGot, mExpected))
	})
	r, err := NewAWSSDRegistry(p)
	require.NoError(t, err)

	err = r.ApplyChanges(context.Background(), changes)
	require.NoError(t, err)
}

func newEndpointWithOwnerAndDescription(dnsName, target, recordType, ownerID string, description string) *endpoint.Endpoint {
	e := endpoint.NewEndpoint(dnsName, recordType, target)
	e.Labels[endpoint.OwnerLabelKey] = ownerID
	e.Labels[endpoint.AWSSDDescriptionLabel] = description
	return e
}
