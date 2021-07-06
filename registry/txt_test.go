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
	"reflect"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"sigs.k8s.io/external-dns/endpoint"
	"sigs.k8s.io/external-dns/internal/testutils"
	"sigs.k8s.io/external-dns/plan"
	"sigs.k8s.io/external-dns/provider"
	"sigs.k8s.io/external-dns/provider/inmemory"
)

const (
	testZone = "test-zone.example.org"
)

func TestTXTRegistry(t *testing.T) {
	t.Run("TestNewTXTRegistry", testTXTRegistryNew)
	t.Run("TestRecords", testTXTRegistryRecords)
	t.Run("TestApplyChanges", testTXTRegistryApplyChanges)
}

func testTXTRegistryNew(t *testing.T) {
	p := inmemory.NewInMemoryProvider()
	_, err := NewTXTRegistry(p, "txt", "", "", time.Hour, "")
	require.Error(t, err)

	_, err = NewTXTRegistry(p, "", "txt", "", time.Hour, "")
	require.Error(t, err)

	r, err := NewTXTRegistry(p, "txt", "", "owner", time.Hour, "")
	require.NoError(t, err)
	assert.Equal(t, p, r.provider)

	r, err = NewTXTRegistry(p, "", "txt", "owner", time.Hour, "")
	require.NoError(t, err)

	_, err = NewTXTRegistry(p, "txt", "txt", "owner", time.Hour, "")
	require.Error(t, err)

	_, ok := r.mapper.(affixNameMapper)
	require.True(t, ok)
	assert.Equal(t, "owner", r.ownerID)
	assert.Equal(t, p, r.provider)

	r, err = NewTXTRegistry(p, "", "", "owner", time.Hour, "")
	require.NoError(t, err)

	_, ok = r.mapper.(affixNameMapper)
	assert.True(t, ok)
}

func testTXTRegistryRecords(t *testing.T) {
	t.Run("With prefix", testTXTRegistryRecordsPrefixed)
	t.Run("With suffix", testTXTRegistryRecordsSuffixed)
	t.Run("No prefix", testTXTRegistryRecordsNoPrefix)
}

func testTXTRegistryRecordsPrefixed(t *testing.T) {
	ctx := context.Background()
	p := inmemory.NewInMemoryProvider()
	p.CreateZone(testZone)
	p.ApplyChanges(ctx, &plan.Changes{
		Create: []*endpoint.Endpoint{
			newEndpointWithOwnerAndLabels("foo.test-zone.example.org", "foo.loadbalancer.com", endpoint.RecordTypeCNAME, "", endpoint.Labels{"foo": "somefoo"}),
			newEndpointWithOwnerAndLabels("bar.test-zone.example.org", "my-domain.com", endpoint.RecordTypeCNAME, "", endpoint.Labels{"bar": "somebar"}),
			newEndpointWithOwner("txt.bar.test-zone.example.org", "\"heritage=external-dns,external-dns/owner=owner\"", endpoint.RecordTypeTXT, ""),
			newEndpointWithOwner("txt.bar.test-zone.example.org", "baz.test-zone.example.org", endpoint.RecordTypeCNAME, ""),
			newEndpointWithOwner("qux.test-zone.example.org", "random", endpoint.RecordTypeTXT, ""),
			newEndpointWithOwnerAndLabels("tar.test-zone.example.org", "tar.loadbalancer.com", endpoint.RecordTypeCNAME, "", endpoint.Labels{"tar": "sometar"}),
			newEndpointWithOwner("TxT.tar.test-zone.example.org", "\"heritage=external-dns,external-dns/owner=owner-2\"", endpoint.RecordTypeTXT, ""), // case-insensitive TXT prefix
			newEndpointWithOwner("foobar.test-zone.example.org", "foobar.loadbalancer.com", endpoint.RecordTypeCNAME, ""),
			newEndpointWithOwner("foobar.test-zone.example.org", "\"heritage=external-dns,external-dns/owner=owner\"", endpoint.RecordTypeTXT, ""),
			newEndpointWithOwner("multiple.test-zone.example.org", "lb1.loadbalancer.com", endpoint.RecordTypeCNAME, "").WithSetIdentifier("test-set-1"),
			newEndpointWithOwner("multiple.test-zone.example.org", "\"heritage=external-dns,external-dns/owner=owner\"", endpoint.RecordTypeTXT, "").WithSetIdentifier("test-set-1"),
			newEndpointWithOwner("multiple.test-zone.example.org", "lb2.loadbalancer.com", endpoint.RecordTypeCNAME, "").WithSetIdentifier("test-set-2"),
			newEndpointWithOwner("multiple.test-zone.example.org", "\"heritage=external-dns,external-dns/owner=owner\"", endpoint.RecordTypeTXT, "").WithSetIdentifier("test-set-2"),
			newEndpointWithOwner("*.wildcard.test-zone.example.org", "foo.loadbalancer.com", endpoint.RecordTypeCNAME, ""),
			newEndpointWithOwner("txt.wc.wildcard.test-zone.example.org", "\"heritage=external-dns,external-dns/owner=owner\"", endpoint.RecordTypeTXT, ""),
		},
	})
	expectedRecords := []*endpoint.Endpoint{
		{
			DNSName:    "foo.test-zone.example.org",
			Targets:    endpoint.Targets{"foo.loadbalancer.com"},
			RecordType: endpoint.RecordTypeCNAME,
			Labels: map[string]string{
				endpoint.OwnerLabelKey: "",
				"foo":                  "somefoo",
			},
		},
		{
			DNSName:    "bar.test-zone.example.org",
			Targets:    endpoint.Targets{"my-domain.com"},
			RecordType: endpoint.RecordTypeCNAME,
			Labels: map[string]string{
				endpoint.OwnerLabelKey: "owner",
				"bar":                  "somebar",
			},
		},
		{
			DNSName:    "txt.bar.test-zone.example.org",
			Targets:    endpoint.Targets{"baz.test-zone.example.org"},
			RecordType: endpoint.RecordTypeCNAME,
			Labels: map[string]string{
				endpoint.OwnerLabelKey: "",
			},
		},
		{
			DNSName:    "qux.test-zone.example.org",
			Targets:    endpoint.Targets{"random"},
			RecordType: endpoint.RecordTypeTXT,
			Labels: map[string]string{
				endpoint.OwnerLabelKey: "",
			},
		},
		{
			DNSName:    "tar.test-zone.example.org",
			Targets:    endpoint.Targets{"tar.loadbalancer.com"},
			RecordType: endpoint.RecordTypeCNAME,
			Labels: map[string]string{
				endpoint.OwnerLabelKey: "owner-2",
				"tar":                  "sometar",
			},
		},
		{
			DNSName:    "foobar.test-zone.example.org",
			Targets:    endpoint.Targets{"foobar.loadbalancer.com"},
			RecordType: endpoint.RecordTypeCNAME,
			Labels: map[string]string{
				endpoint.OwnerLabelKey: "",
			},
		},
		{
			DNSName:       "multiple.test-zone.example.org",
			Targets:       endpoint.Targets{"lb1.loadbalancer.com"},
			SetIdentifier: "test-set-1",
			RecordType:    endpoint.RecordTypeCNAME,
			Labels: map[string]string{
				endpoint.OwnerLabelKey: "",
			},
		},
		{
			DNSName:       "multiple.test-zone.example.org",
			Targets:       endpoint.Targets{"lb2.loadbalancer.com"},
			SetIdentifier: "test-set-2",
			RecordType:    endpoint.RecordTypeCNAME,
			Labels: map[string]string{
				endpoint.OwnerLabelKey: "",
			},
		},
		{
			DNSName:    "*.wildcard.test-zone.example.org",
			Targets:    endpoint.Targets{"foo.loadbalancer.com"},
			RecordType: endpoint.RecordTypeCNAME,
			Labels: map[string]string{
				endpoint.OwnerLabelKey: "owner",
			},
		},
	}

	r, _ := NewTXTRegistry(p, "txt.", "", "owner", time.Hour, "wc")
	records, _ := r.Records(ctx)

	assert.True(t, testutils.SameEndpoints(records, expectedRecords))

	// Ensure prefix is case-insensitive
	r, _ = NewTXTRegistry(p, "TxT.", "", "owner", time.Hour, "")
	records, _ = r.Records(ctx)

	assert.True(t, testutils.SameEndpointLabels(records, expectedRecords))
}

func testTXTRegistryRecordsSuffixed(t *testing.T) {
	ctx := context.Background()
	p := inmemory.NewInMemoryProvider()
	p.CreateZone(testZone)
	p.ApplyChanges(ctx, &plan.Changes{
		Create: []*endpoint.Endpoint{
			newEndpointWithOwnerAndLabels("foo.test-zone.example.org", "foo.loadbalancer.com", endpoint.RecordTypeCNAME, "", endpoint.Labels{"foo": "somefoo"}),
			newEndpointWithOwnerAndLabels("bar.test-zone.example.org", "my-domain.com", endpoint.RecordTypeCNAME, "", endpoint.Labels{"bar": "somebar"}),
			newEndpointWithOwner("bar-txt.test-zone.example.org", "\"heritage=external-dns,external-dns/owner=owner\"", endpoint.RecordTypeTXT, ""),
			newEndpointWithOwner("bar-txt.test-zone.example.org", "baz.test-zone.example.org", endpoint.RecordTypeCNAME, ""),
			newEndpointWithOwner("qux.test-zone.example.org", "random", endpoint.RecordTypeTXT, ""),
			newEndpointWithOwnerAndLabels("tar.test-zone.example.org", "tar.loadbalancer.com", endpoint.RecordTypeCNAME, "", endpoint.Labels{"tar": "sometar"}),
			newEndpointWithOwner("tar-TxT.test-zone.example.org", "\"heritage=external-dns,external-dns/owner=owner-2\"", endpoint.RecordTypeTXT, ""), // case-insensitive TXT prefix
			newEndpointWithOwner("foobar.test-zone.example.org", "foobar.loadbalancer.com", endpoint.RecordTypeCNAME, ""),
			newEndpointWithOwner("foobar.test-zone.example.org", "\"heritage=external-dns,external-dns/owner=owner\"", endpoint.RecordTypeTXT, ""),
			newEndpointWithOwner("multiple.test-zone.example.org", "lb1.loadbalancer.com", endpoint.RecordTypeCNAME, "").WithSetIdentifier("test-set-1"),
			newEndpointWithOwner("multiple.test-zone.example.org", "\"heritage=external-dns,external-dns/owner=owner\"", endpoint.RecordTypeTXT, "").WithSetIdentifier("test-set-1"),
			newEndpointWithOwner("multiple.test-zone.example.org", "lb2.loadbalancer.com", endpoint.RecordTypeCNAME, "").WithSetIdentifier("test-set-2"),
			newEndpointWithOwner("multiple.test-zone.example.org", "\"heritage=external-dns,external-dns/owner=owner\"", endpoint.RecordTypeTXT, "").WithSetIdentifier("test-set-2"),
		},
	})
	expectedRecords := []*endpoint.Endpoint{
		{
			DNSName:    "foo.test-zone.example.org",
			Targets:    endpoint.Targets{"foo.loadbalancer.com"},
			RecordType: endpoint.RecordTypeCNAME,
			Labels: map[string]string{
				endpoint.OwnerLabelKey: "",
				"foo":                  "somefoo",
			},
		},
		{
			DNSName:    "bar.test-zone.example.org",
			Targets:    endpoint.Targets{"my-domain.com"},
			RecordType: endpoint.RecordTypeCNAME,
			Labels: map[string]string{
				endpoint.OwnerLabelKey: "owner",
				"bar":                  "somebar",
			},
		},
		{
			DNSName:    "bar-txt.test-zone.example.org",
			Targets:    endpoint.Targets{"baz.test-zone.example.org"},
			RecordType: endpoint.RecordTypeCNAME,
			Labels: map[string]string{
				endpoint.OwnerLabelKey: "",
			},
		},
		{
			DNSName:    "qux.test-zone.example.org",
			Targets:    endpoint.Targets{"random"},
			RecordType: endpoint.RecordTypeTXT,
			Labels: map[string]string{
				endpoint.OwnerLabelKey: "",
			},
		},
		{
			DNSName:    "tar.test-zone.example.org",
			Targets:    endpoint.Targets{"tar.loadbalancer.com"},
			RecordType: endpoint.RecordTypeCNAME,
			Labels: map[string]string{
				endpoint.OwnerLabelKey: "owner-2",
				"tar":                  "sometar",
			},
		},
		{
			DNSName:    "foobar.test-zone.example.org",
			Targets:    endpoint.Targets{"foobar.loadbalancer.com"},
			RecordType: endpoint.RecordTypeCNAME,
			Labels: map[string]string{
				endpoint.OwnerLabelKey: "",
			},
		},
		{
			DNSName:       "multiple.test-zone.example.org",
			Targets:       endpoint.Targets{"lb1.loadbalancer.com"},
			SetIdentifier: "test-set-1",
			RecordType:    endpoint.RecordTypeCNAME,
			Labels: map[string]string{
				endpoint.OwnerLabelKey: "",
			},
		},
		{
			DNSName:       "multiple.test-zone.example.org",
			Targets:       endpoint.Targets{"lb2.loadbalancer.com"},
			SetIdentifier: "test-set-2",
			RecordType:    endpoint.RecordTypeCNAME,
			Labels: map[string]string{
				endpoint.OwnerLabelKey: "",
			},
		},
	}

	r, _ := NewTXTRegistry(p, "", "-txt", "owner", time.Hour, "")
	records, _ := r.Records(ctx)

	assert.True(t, testutils.SameEndpoints(records, expectedRecords))

	// Ensure prefix is case-insensitive
	r, _ = NewTXTRegistry(p, "", "-TxT", "owner", time.Hour, "")
	records, _ = r.Records(ctx)

	assert.True(t, testutils.SameEndpointLabels(records, expectedRecords))
}

func testTXTRegistryRecordsNoPrefix(t *testing.T) {
	p := inmemory.NewInMemoryProvider()
	ctx := context.Background()
	p.CreateZone(testZone)
	p.ApplyChanges(ctx, &plan.Changes{
		Create: []*endpoint.Endpoint{
			newEndpointWithOwner("foo.test-zone.example.org", "foo.loadbalancer.com", endpoint.RecordTypeCNAME, ""),
			newEndpointWithOwner("bar.test-zone.example.org", "my-domain.com", endpoint.RecordTypeCNAME, ""),
			newEndpointWithOwner("txt.bar.test-zone.example.org", "\"heritage=external-dns,external-dns/owner=owner,external-dns/resource=ingress/default/my-ingress\"", endpoint.RecordTypeTXT, ""),
			newEndpointWithOwner("txt.bar.test-zone.example.org", "baz.test-zone.example.org", endpoint.RecordTypeCNAME, ""),
			newEndpointWithOwner("qux.test-zone.example.org", "random", endpoint.RecordTypeTXT, ""),
			newEndpointWithOwner("tar.test-zone.example.org", "tar.loadbalancer.com", endpoint.RecordTypeCNAME, ""),
			newEndpointWithOwner("txt.tar.test-zone.example.org", "\"heritage=external-dns,external-dns/owner=owner-2\"", endpoint.RecordTypeTXT, ""),
			newEndpointWithOwner("foobar.test-zone.example.org", "foobar.loadbalancer.com", endpoint.RecordTypeCNAME, ""),
			newEndpointWithOwner("foobar.test-zone.example.org", "\"heritage=external-dns,external-dns/owner=owner\"", endpoint.RecordTypeTXT, ""),
		},
	})
	expectedRecords := []*endpoint.Endpoint{
		{
			DNSName:    "foo.test-zone.example.org",
			Targets:    endpoint.Targets{"foo.loadbalancer.com"},
			RecordType: endpoint.RecordTypeCNAME,
			Labels: map[string]string{
				endpoint.OwnerLabelKey: "",
			},
		},
		{
			DNSName:    "bar.test-zone.example.org",
			Targets:    endpoint.Targets{"my-domain.com"},
			RecordType: endpoint.RecordTypeCNAME,
			Labels: map[string]string{
				endpoint.OwnerLabelKey: "",
			},
		},
		{
			DNSName:    "txt.bar.test-zone.example.org",
			Targets:    endpoint.Targets{"baz.test-zone.example.org"},
			RecordType: endpoint.RecordTypeCNAME,
			Labels: map[string]string{
				endpoint.OwnerLabelKey:    "owner",
				endpoint.ResourceLabelKey: "ingress/default/my-ingress",
			},
		},
		{
			DNSName:    "qux.test-zone.example.org",
			Targets:    endpoint.Targets{"random"},
			RecordType: endpoint.RecordTypeTXT,
			Labels: map[string]string{
				endpoint.OwnerLabelKey: "",
			},
		},
		{
			DNSName:    "tar.test-zone.example.org",
			Targets:    endpoint.Targets{"tar.loadbalancer.com"},
			RecordType: endpoint.RecordTypeCNAME,
			Labels: map[string]string{
				endpoint.OwnerLabelKey: "",
			},
		},
		{
			DNSName:    "foobar.test-zone.example.org",
			Targets:    endpoint.Targets{"foobar.loadbalancer.com"},
			RecordType: endpoint.RecordTypeCNAME,
			Labels: map[string]string{
				endpoint.OwnerLabelKey: "owner",
			},
		},
	}

	r, _ := NewTXTRegistry(p, "", "", "owner", time.Hour, "")
	records, _ := r.Records(ctx)

	assert.True(t, testutils.SameEndpoints(records, expectedRecords))
}

func testTXTRegistryApplyChanges(t *testing.T) {
	t.Run("With Prefix", testTXTRegistryApplyChangesWithPrefix)
	t.Run("With Suffix", testTXTRegistryApplyChangesWithSuffix)
	t.Run("No prefix", testTXTRegistryApplyChangesNoPrefix)
}

func testTXTRegistryApplyChangesWithPrefix(t *testing.T) {
	p := inmemory.NewInMemoryProvider()
	p.CreateZone(testZone)
	ctxEndpoints := []*endpoint.Endpoint{}
	ctx := context.WithValue(context.Background(), provider.RecordsContextKey, ctxEndpoints)
	p.OnApplyChanges = func(ctx context.Context, got *plan.Changes) {
		assert.Equal(t, ctxEndpoints, ctx.Value(provider.RecordsContextKey))
	}
	p.ApplyChanges(ctx, &plan.Changes{
		Create: []*endpoint.Endpoint{
			newEndpointWithOwner("foo.test-zone.example.org", "foo.loadbalancer.com", endpoint.RecordTypeCNAME, ""),
			newEndpointWithOwner("bar.test-zone.example.org", "my-domain.com", endpoint.RecordTypeCNAME, ""),
			newEndpointWithOwner("txt.bar.test-zone.example.org", "\"heritage=external-dns,external-dns/owner=owner\"", endpoint.RecordTypeTXT, ""),
			newEndpointWithOwner("txt.bar.test-zone.example.org", "baz.test-zone.example.org", endpoint.RecordTypeCNAME, ""),
			newEndpointWithOwner("qux.test-zone.example.org", "random", endpoint.RecordTypeTXT, ""),
			newEndpointWithOwner("tar.test-zone.example.org", "tar.loadbalancer.com", endpoint.RecordTypeCNAME, ""),
			newEndpointWithOwner("txt.tar.test-zone.example.org", "\"heritage=external-dns,external-dns/owner=owner\"", endpoint.RecordTypeTXT, ""),
			newEndpointWithOwner("cname-txt.tar.test-zone.example.org", "\"heritage=external-dns,external-dns/owner=owner\"", endpoint.RecordTypeTXT, ""),
			newEndpointWithOwner("foobar.test-zone.example.org", "foobar.loadbalancer.com", endpoint.RecordTypeCNAME, ""),
			newEndpointWithOwner("txt.foobar.test-zone.example.org", "\"heritage=external-dns,external-dns/owner=owner\"", endpoint.RecordTypeTXT, ""),
			newEndpointWithOwner("cname-txt.foobar.test-zone.example.org", "\"heritage=external-dns,external-dns/owner=owner\"", endpoint.RecordTypeTXT, ""),
			newEndpointWithOwner("multiple.test-zone.example.org", "lb1.loadbalancer.com", endpoint.RecordTypeCNAME, "").WithSetIdentifier("test-set-1"),
			newEndpointWithOwner("txt.multiple.test-zone.example.org", "\"heritage=external-dns,external-dns/owner=owner\"", endpoint.RecordTypeTXT, "").WithSetIdentifier("test-set-1"),
			newEndpointWithOwner("cname-txt.multiple.test-zone.example.org", "\"heritage=external-dns,external-dns/owner=owner\"", endpoint.RecordTypeTXT, "").WithSetIdentifier("test-set-1"),
			newEndpointWithOwner("multiple.test-zone.example.org", "lb2.loadbalancer.com", endpoint.RecordTypeCNAME, "").WithSetIdentifier("test-set-2"),
			newEndpointWithOwner("txt.multiple.test-zone.example.org", "\"heritage=external-dns,external-dns/owner=owner\"", endpoint.RecordTypeTXT, "").WithSetIdentifier("test-set-2"),
			newEndpointWithOwner("cname-txt.multiple.test-zone.example.org", "\"heritage=external-dns,external-dns/owner=owner\"", endpoint.RecordTypeTXT, "").WithSetIdentifier("test-set-2"),
		},
	})
	r, _ := NewTXTRegistry(p, "txt.", "", "owner", time.Hour, "")

	changes := &plan.Changes{
		Create: []*endpoint.Endpoint{
			newEndpointWithOwnerResource("new-record-1.test-zone.example.org", "new-loadbalancer-1.lb.com", endpoint.RecordTypeCNAME, "", "ingress/default/my-ingress"),
			newEndpointWithOwnerResource("multiple.test-zone.example.org", "lb3.loadbalancer.com", endpoint.RecordTypeCNAME, "", "ingress/default/my-ingress").WithSetIdentifier("test-set-3"),
			newEndpointWithOwnerResource("example", "new-loadbalancer-1.lb.com", endpoint.RecordTypeCNAME, "", "ingress/default/my-ingress"),
		},
		Delete: []*endpoint.Endpoint{
			newEndpointWithOwner("foobar.test-zone.example.org", "foobar.loadbalancer.com", endpoint.RecordTypeCNAME, "owner"),
			newEndpointWithOwner("multiple.test-zone.example.org", "lb1.loadbalancer.com", endpoint.RecordTypeCNAME, "owner").WithSetIdentifier("test-set-1"),
		},
		UpdateNew: []*endpoint.Endpoint{
			newEndpointWithOwnerResource("tar.test-zone.example.org", "new-tar.loadbalancer.com", endpoint.RecordTypeCNAME, "owner", "ingress/default/my-ingress-2"),
			newEndpointWithOwnerResource("multiple.test-zone.example.org", "new.loadbalancer.com", endpoint.RecordTypeCNAME, "owner", "ingress/default/my-ingress-2").WithSetIdentifier("test-set-2"),
		},
		UpdateOld: []*endpoint.Endpoint{
			newEndpointWithOwner("tar.test-zone.example.org", "tar.loadbalancer.com", endpoint.RecordTypeCNAME, "owner"),
			newEndpointWithOwner("multiple.test-zone.example.org", "lb2.loadbalancer.com", endpoint.RecordTypeCNAME, "owner").WithSetIdentifier("test-set-2"),
		},
	}
	expected := &plan.Changes{
		Create: []*endpoint.Endpoint{
			newEndpointWithOwnerResource("new-record-1.test-zone.example.org", "new-loadbalancer-1.lb.com", endpoint.RecordTypeCNAME, "owner", "ingress/default/my-ingress"),
			newEndpointWithOwner("txt.new-record-1.test-zone.example.org", "\"heritage=external-dns,external-dns/owner=owner,external-dns/resource=ingress/default/my-ingress\"", endpoint.RecordTypeTXT, ""),
			newEndpointWithOwner("cname-txt.new-record-1.test-zone.example.org", "\"heritage=external-dns,external-dns/owner=owner,external-dns/resource=ingress/default/my-ingress\"", endpoint.RecordTypeTXT, ""),
			newEndpointWithOwnerResource("multiple.test-zone.example.org", "lb3.loadbalancer.com", endpoint.RecordTypeCNAME, "owner", "ingress/default/my-ingress").WithSetIdentifier("test-set-3"),
			newEndpointWithOwner("txt.multiple.test-zone.example.org", "\"heritage=external-dns,external-dns/owner=owner,external-dns/resource=ingress/default/my-ingress\"", endpoint.RecordTypeTXT, "").WithSetIdentifier("test-set-3"),
			newEndpointWithOwner("cname-txt.multiple.test-zone.example.org", "\"heritage=external-dns,external-dns/owner=owner,external-dns/resource=ingress/default/my-ingress\"", endpoint.RecordTypeTXT, "").WithSetIdentifier("test-set-3"),
			newEndpointWithOwnerResource("example", "new-loadbalancer-1.lb.com", endpoint.RecordTypeCNAME, "owner", "ingress/default/my-ingress"),
			newEndpointWithOwner("txt.example", "\"heritage=external-dns,external-dns/owner=owner,external-dns/resource=ingress/default/my-ingress\"", endpoint.RecordTypeTXT, ""),
			newEndpointWithOwner("cname-txt.example", "\"heritage=external-dns,external-dns/owner=owner,external-dns/resource=ingress/default/my-ingress\"", endpoint.RecordTypeTXT, ""),
		},
		Delete: []*endpoint.Endpoint{
			newEndpointWithOwner("foobar.test-zone.example.org", "foobar.loadbalancer.com", endpoint.RecordTypeCNAME, "owner"),
			newEndpointWithOwner("txt.foobar.test-zone.example.org", "\"heritage=external-dns,external-dns/owner=owner\"", endpoint.RecordTypeTXT, ""),
			newEndpointWithOwner("cname-txt.foobar.test-zone.example.org", "\"heritage=external-dns,external-dns/owner=owner\"", endpoint.RecordTypeTXT, ""),
			newEndpointWithOwner("multiple.test-zone.example.org", "lb1.loadbalancer.com", endpoint.RecordTypeCNAME, "owner").WithSetIdentifier("test-set-1"),
			newEndpointWithOwner("txt.multiple.test-zone.example.org", "\"heritage=external-dns,external-dns/owner=owner\"", endpoint.RecordTypeTXT, "").WithSetIdentifier("test-set-1"),
			newEndpointWithOwner("cname-txt.multiple.test-zone.example.org", "\"heritage=external-dns,external-dns/owner=owner\"", endpoint.RecordTypeTXT, "").WithSetIdentifier("test-set-1"),
		},
		UpdateNew: []*endpoint.Endpoint{
			newEndpointWithOwnerResource("tar.test-zone.example.org", "new-tar.loadbalancer.com", endpoint.RecordTypeCNAME, "owner", "ingress/default/my-ingress-2"),
			newEndpointWithOwner("txt.tar.test-zone.example.org", "\"heritage=external-dns,external-dns/owner=owner,external-dns/resource=ingress/default/my-ingress-2\"", endpoint.RecordTypeTXT, ""),
			newEndpointWithOwner("cname-txt.tar.test-zone.example.org", "\"heritage=external-dns,external-dns/owner=owner,external-dns/resource=ingress/default/my-ingress-2\"", endpoint.RecordTypeTXT, ""),
			newEndpointWithOwnerResource("multiple.test-zone.example.org", "new.loadbalancer.com", endpoint.RecordTypeCNAME, "owner", "ingress/default/my-ingress-2").WithSetIdentifier("test-set-2"),
			newEndpointWithOwner("txt.multiple.test-zone.example.org", "\"heritage=external-dns,external-dns/owner=owner,external-dns/resource=ingress/default/my-ingress-2\"", endpoint.RecordTypeTXT, "").WithSetIdentifier("test-set-2"),
			newEndpointWithOwner("cname-txt.multiple.test-zone.example.org", "\"heritage=external-dns,external-dns/owner=owner,external-dns/resource=ingress/default/my-ingress-2\"", endpoint.RecordTypeTXT, "").WithSetIdentifier("test-set-2"),
		},
		UpdateOld: []*endpoint.Endpoint{
			newEndpointWithOwner("tar.test-zone.example.org", "tar.loadbalancer.com", endpoint.RecordTypeCNAME, "owner"),
			newEndpointWithOwner("txt.tar.test-zone.example.org", "\"heritage=external-dns,external-dns/owner=owner\"", endpoint.RecordTypeTXT, ""),
			newEndpointWithOwner("cname-txt.tar.test-zone.example.org", "\"heritage=external-dns,external-dns/owner=owner\"", endpoint.RecordTypeTXT, ""),
			newEndpointWithOwner("multiple.test-zone.example.org", "lb2.loadbalancer.com", endpoint.RecordTypeCNAME, "owner").WithSetIdentifier("test-set-2"),
			newEndpointWithOwner("txt.multiple.test-zone.example.org", "\"heritage=external-dns,external-dns/owner=owner\"", endpoint.RecordTypeTXT, "").WithSetIdentifier("test-set-2"),
			newEndpointWithOwner("cname-txt.multiple.test-zone.example.org", "\"heritage=external-dns,external-dns/owner=owner\"", endpoint.RecordTypeTXT, "").WithSetIdentifier("test-set-2"),
		},
	}
	p.OnApplyChanges = func(ctx context.Context, got *plan.Changes) {
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
		assert.Equal(t, nil, ctx.Value(provider.RecordsContextKey))
	}
	err := r.ApplyChanges(ctx, changes)
	require.NoError(t, err)
}

func testTXTRegistryApplyChangesWithSuffix(t *testing.T) {
	p := inmemory.NewInMemoryProvider()
	p.CreateZone(testZone)
	ctxEndpoints := []*endpoint.Endpoint{}
	ctx := context.WithValue(context.Background(), provider.RecordsContextKey, ctxEndpoints)
	p.OnApplyChanges = func(ctx context.Context, got *plan.Changes) {
		assert.Equal(t, ctxEndpoints, ctx.Value(provider.RecordsContextKey))
	}
	p.ApplyChanges(ctx, &plan.Changes{
		Create: []*endpoint.Endpoint{
			newEndpointWithOwner("foo.test-zone.example.org", "foo.loadbalancer.com", endpoint.RecordTypeCNAME, ""),
			newEndpointWithOwner("bar.test-zone.example.org", "my-domain.com", endpoint.RecordTypeCNAME, ""),
			newEndpointWithOwner("bar-txt.test-zone.example.org", "\"heritage=external-dns,external-dns/owner=owner\"", endpoint.RecordTypeTXT, ""),
			newEndpointWithOwner("cname-bar-txt.test-zone.example.org", "\"heritage=external-dns,external-dns/owner=owner\"", endpoint.RecordTypeTXT, ""),
			newEndpointWithOwner("bar-txt.test-zone.example.org", "baz.test-zone.example.org", endpoint.RecordTypeCNAME, ""),
			newEndpointWithOwner("qux.test-zone.example.org", "random", endpoint.RecordTypeTXT, ""),
			newEndpointWithOwner("tar.test-zone.example.org", "tar.loadbalancer.com", endpoint.RecordTypeCNAME, ""),
			newEndpointWithOwner("tar-txt.test-zone.example.org", "\"heritage=external-dns,external-dns/owner=owner\"", endpoint.RecordTypeTXT, ""),
			newEndpointWithOwner("cname-tar-txt.test-zone.example.org", "\"heritage=external-dns,external-dns/owner=owner\"", endpoint.RecordTypeTXT, ""),
			newEndpointWithOwner("foobar.test-zone.example.org", "foobar.loadbalancer.com", endpoint.RecordTypeCNAME, ""),
			newEndpointWithOwner("foobar-txt.test-zone.example.org", "\"heritage=external-dns,external-dns/owner=owner\"", endpoint.RecordTypeTXT, ""),
			newEndpointWithOwner("cname-foobar-txt.test-zone.example.org", "\"heritage=external-dns,external-dns/owner=owner\"", endpoint.RecordTypeTXT, ""),
			newEndpointWithOwner("multiple.test-zone.example.org", "lb1.loadbalancer.com", endpoint.RecordTypeCNAME, "").WithSetIdentifier("test-set-1"),
			newEndpointWithOwner("multiple-txt.test-zone.example.org", "\"heritage=external-dns,external-dns/owner=owner\"", endpoint.RecordTypeTXT, "").WithSetIdentifier("test-set-1"),
			newEndpointWithOwner("cname-multiple-txt.test-zone.example.org", "\"heritage=external-dns,external-dns/owner=owner\"", endpoint.RecordTypeTXT, "").WithSetIdentifier("test-set-1"),
			newEndpointWithOwner("multiple.test-zone.example.org", "lb2.loadbalancer.com", endpoint.RecordTypeCNAME, "").WithSetIdentifier("test-set-2"),
			newEndpointWithOwner("multiple-txt.test-zone.example.org", "\"heritage=external-dns,external-dns/owner=owner\"", endpoint.RecordTypeTXT, "").WithSetIdentifier("test-set-2"),
			newEndpointWithOwner("cname-multiple-txt.test-zone.example.org", "\"heritage=external-dns,external-dns/owner=owner\"", endpoint.RecordTypeTXT, "").WithSetIdentifier("test-set-2"),
		},
	})
	r, _ := NewTXTRegistry(p, "", "-txt", "owner", time.Hour, "wildcard")

	changes := &plan.Changes{
		Create: []*endpoint.Endpoint{
			newEndpointWithOwnerResource("new-record-1.test-zone.example.org", "new-loadbalancer-1.lb.com", endpoint.RecordTypeCNAME, "", "ingress/default/my-ingress"),
			newEndpointWithOwnerResource("multiple.test-zone.example.org", "lb3.loadbalancer.com", endpoint.RecordTypeCNAME, "", "ingress/default/my-ingress").WithSetIdentifier("test-set-3"),
			newEndpointWithOwnerResource("example", "new-loadbalancer-1.lb.com", endpoint.RecordTypeCNAME, "", "ingress/default/my-ingress"),
			newEndpointWithOwnerResource("*.wildcard.test-zone.example.org", "new-loadbalancer-1.lb.com", endpoint.RecordTypeCNAME, "", "ingress/default/my-ingress"),
		},
		Delete: []*endpoint.Endpoint{
			newEndpointWithOwner("foobar.test-zone.example.org", "foobar.loadbalancer.com", endpoint.RecordTypeCNAME, "owner"),
			newEndpointWithOwner("multiple.test-zone.example.org", "lb1.loadbalancer.com", endpoint.RecordTypeCNAME, "owner").WithSetIdentifier("test-set-1"),
		},
		UpdateNew: []*endpoint.Endpoint{
			newEndpointWithOwnerResource("tar.test-zone.example.org", "new-tar.loadbalancer.com", endpoint.RecordTypeCNAME, "owner", "ingress/default/my-ingress-2"),
			newEndpointWithOwnerResource("multiple.test-zone.example.org", "new.loadbalancer.com", endpoint.RecordTypeCNAME, "owner", "ingress/default/my-ingress-2").WithSetIdentifier("test-set-2"),
		},
		UpdateOld: []*endpoint.Endpoint{
			newEndpointWithOwner("tar.test-zone.example.org", "tar.loadbalancer.com", endpoint.RecordTypeCNAME, "owner"),
			newEndpointWithOwner("multiple.test-zone.example.org", "lb2.loadbalancer.com", endpoint.RecordTypeCNAME, "owner").WithSetIdentifier("test-set-2"),
		},
	}
	expected := &plan.Changes{
		Create: []*endpoint.Endpoint{
			newEndpointWithOwnerResource("new-record-1.test-zone.example.org", "new-loadbalancer-1.lb.com", endpoint.RecordTypeCNAME, "owner", "ingress/default/my-ingress"),
			newEndpointWithOwner("new-record-1-txt.test-zone.example.org", "\"heritage=external-dns,external-dns/owner=owner,external-dns/resource=ingress/default/my-ingress\"", endpoint.RecordTypeTXT, ""),
			newEndpointWithOwner("cname-new-record-1-txt.test-zone.example.org", "\"heritage=external-dns,external-dns/owner=owner,external-dns/resource=ingress/default/my-ingress\"", endpoint.RecordTypeTXT, ""),
			newEndpointWithOwnerResource("multiple.test-zone.example.org", "lb3.loadbalancer.com", endpoint.RecordTypeCNAME, "owner", "ingress/default/my-ingress").WithSetIdentifier("test-set-3"),
			newEndpointWithOwner("multiple-txt.test-zone.example.org", "\"heritage=external-dns,external-dns/owner=owner,external-dns/resource=ingress/default/my-ingress\"", endpoint.RecordTypeTXT, "").WithSetIdentifier("test-set-3"),
			newEndpointWithOwner("cname-multiple-txt.test-zone.example.org", "\"heritage=external-dns,external-dns/owner=owner,external-dns/resource=ingress/default/my-ingress\"", endpoint.RecordTypeTXT, "").WithSetIdentifier("test-set-3"),
			newEndpointWithOwnerResource("example", "new-loadbalancer-1.lb.com", endpoint.RecordTypeCNAME, "owner", "ingress/default/my-ingress"),
			newEndpointWithOwner("example-txt", "\"heritage=external-dns,external-dns/owner=owner,external-dns/resource=ingress/default/my-ingress\"", endpoint.RecordTypeTXT, ""),
			newEndpointWithOwner("cname-example-txt", "\"heritage=external-dns,external-dns/owner=owner,external-dns/resource=ingress/default/my-ingress\"", endpoint.RecordTypeTXT, ""),
			newEndpointWithOwnerResource("*.wildcard.test-zone.example.org", "new-loadbalancer-1.lb.com", endpoint.RecordTypeCNAME, "owner", "ingress/default/my-ingress"),
			newEndpointWithOwner("wildcard-txt.wildcard.test-zone.example.org", "\"heritage=external-dns,external-dns/owner=owner,external-dns/resource=ingress/default/my-ingress\"", endpoint.RecordTypeTXT, ""),
			newEndpointWithOwner("cname-wildcard-txt.wildcard.test-zone.example.org", "\"heritage=external-dns,external-dns/owner=owner,external-dns/resource=ingress/default/my-ingress\"", endpoint.RecordTypeTXT, ""),
		},
		Delete: []*endpoint.Endpoint{
			newEndpointWithOwner("foobar.test-zone.example.org", "foobar.loadbalancer.com", endpoint.RecordTypeCNAME, "owner"),
			newEndpointWithOwner("foobar-txt.test-zone.example.org", "\"heritage=external-dns,external-dns/owner=owner\"", endpoint.RecordTypeTXT, ""),
			newEndpointWithOwner("cname-foobar-txt.test-zone.example.org", "\"heritage=external-dns,external-dns/owner=owner\"", endpoint.RecordTypeTXT, ""),
			newEndpointWithOwner("multiple.test-zone.example.org", "lb1.loadbalancer.com", endpoint.RecordTypeCNAME, "owner").WithSetIdentifier("test-set-1"),
			newEndpointWithOwner("multiple-txt.test-zone.example.org", "\"heritage=external-dns,external-dns/owner=owner\"", endpoint.RecordTypeTXT, "").WithSetIdentifier("test-set-1"),
			newEndpointWithOwner("cname-multiple-txt.test-zone.example.org", "\"heritage=external-dns,external-dns/owner=owner\"", endpoint.RecordTypeTXT, "").WithSetIdentifier("test-set-1"),
		},
		UpdateNew: []*endpoint.Endpoint{
			newEndpointWithOwnerResource("tar.test-zone.example.org", "new-tar.loadbalancer.com", endpoint.RecordTypeCNAME, "owner", "ingress/default/my-ingress-2"),
			newEndpointWithOwner("tar-txt.test-zone.example.org", "\"heritage=external-dns,external-dns/owner=owner,external-dns/resource=ingress/default/my-ingress-2\"", endpoint.RecordTypeTXT, ""),
			newEndpointWithOwner("cname-tar-txt.test-zone.example.org", "\"heritage=external-dns,external-dns/owner=owner,external-dns/resource=ingress/default/my-ingress-2\"", endpoint.RecordTypeTXT, ""),
			newEndpointWithOwnerResource("multiple.test-zone.example.org", "new.loadbalancer.com", endpoint.RecordTypeCNAME, "owner", "ingress/default/my-ingress-2").WithSetIdentifier("test-set-2"),
			newEndpointWithOwner("multiple-txt.test-zone.example.org", "\"heritage=external-dns,external-dns/owner=owner,external-dns/resource=ingress/default/my-ingress-2\"", endpoint.RecordTypeTXT, "").WithSetIdentifier("test-set-2"),
			newEndpointWithOwner("cname-multiple-txt.test-zone.example.org", "\"heritage=external-dns,external-dns/owner=owner,external-dns/resource=ingress/default/my-ingress-2\"", endpoint.RecordTypeTXT, "").WithSetIdentifier("test-set-2"),
		},
		UpdateOld: []*endpoint.Endpoint{
			newEndpointWithOwner("tar.test-zone.example.org", "tar.loadbalancer.com", endpoint.RecordTypeCNAME, "owner"),
			newEndpointWithOwner("tar-txt.test-zone.example.org", "\"heritage=external-dns,external-dns/owner=owner\"", endpoint.RecordTypeTXT, ""),
			newEndpointWithOwner("cname-tar-txt.test-zone.example.org", "\"heritage=external-dns,external-dns/owner=owner\"", endpoint.RecordTypeTXT, ""),
			newEndpointWithOwner("multiple.test-zone.example.org", "lb2.loadbalancer.com", endpoint.RecordTypeCNAME, "owner").WithSetIdentifier("test-set-2"),
			newEndpointWithOwner("multiple-txt.test-zone.example.org", "\"heritage=external-dns,external-dns/owner=owner\"", endpoint.RecordTypeTXT, "").WithSetIdentifier("test-set-2"),
			newEndpointWithOwner("cname-multiple-txt.test-zone.example.org", "\"heritage=external-dns,external-dns/owner=owner\"", endpoint.RecordTypeTXT, "").WithSetIdentifier("test-set-2"),
		},
	}
	p.OnApplyChanges = func(ctx context.Context, got *plan.Changes) {
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
		assert.Equal(t, nil, ctx.Value(provider.RecordsContextKey))
	}
	err := r.ApplyChanges(ctx, changes)
	require.NoError(t, err)
}

func testTXTRegistryApplyChangesNoPrefix(t *testing.T) {
	p := inmemory.NewInMemoryProvider()
	p.CreateZone(testZone)
	ctxEndpoints := []*endpoint.Endpoint{}
	ctx := context.WithValue(context.Background(), provider.RecordsContextKey, ctxEndpoints)
	p.OnApplyChanges = func(ctx context.Context, got *plan.Changes) {
		assert.Equal(t, ctxEndpoints, ctx.Value(provider.RecordsContextKey))
	}
	p.ApplyChanges(ctx, &plan.Changes{
		Create: []*endpoint.Endpoint{
			newEndpointWithOwner("foo.test-zone.example.org", "foo.loadbalancer.com", endpoint.RecordTypeCNAME, ""),
			newEndpointWithOwner("bar.test-zone.example.org", "my-domain.com", endpoint.RecordTypeCNAME, ""),
			newEndpointWithOwner("txt.bar.test-zone.example.org", "\"heritage=external-dns,external-dns/owner=owner\"", endpoint.RecordTypeTXT, ""),
			newEndpointWithOwner("txt.bar.test-zone.example.org", "baz.test-zone.example.org", endpoint.RecordTypeCNAME, ""),
			newEndpointWithOwner("qux.test-zone.example.org", "random", endpoint.RecordTypeTXT, ""),
			newEndpointWithOwner("tar.test-zone.example.org", "tar.loadbalancer.com", endpoint.RecordTypeCNAME, ""),
			newEndpointWithOwner("txt.tar.test-zone.example.org", "\"heritage=external-dns,external-dns/owner=owner\"", endpoint.RecordTypeTXT, ""),
			newEndpointWithOwner("foobar.test-zone.example.org", "foobar.loadbalancer.com", endpoint.RecordTypeCNAME, ""),
			newEndpointWithOwner("foobar.test-zone.example.org", "\"heritage=external-dns,external-dns/owner=owner\"", endpoint.RecordTypeTXT, ""),
			newEndpointWithOwner("cname-foobar.test-zone.example.org", "\"heritage=external-dns,external-dns/owner=owner\"", endpoint.RecordTypeTXT, ""),
		},
	})
	r, _ := NewTXTRegistry(p, "", "", "owner", time.Hour, "")

	changes := &plan.Changes{
		Create: []*endpoint.Endpoint{
			newEndpointWithOwner("new-record-1.test-zone.example.org", "new-loadbalancer-1.lb.com", endpoint.RecordTypeCNAME, ""),
			newEndpointWithOwner("example", "new-loadbalancer-1.lb.com", endpoint.RecordTypeCNAME, ""),
		},
		Delete: []*endpoint.Endpoint{
			newEndpointWithOwner("foobar.test-zone.example.org", "foobar.loadbalancer.com", endpoint.RecordTypeCNAME, "owner"),
		},
		UpdateNew: []*endpoint.Endpoint{
			newEndpointWithOwner("tar.test-zone.example.org", "new-tar.loadbalancer.com", endpoint.RecordTypeCNAME, "owner-2"),
		},
		UpdateOld: []*endpoint.Endpoint{
			newEndpointWithOwner("tar.test-zone.example.org", "tar.loadbalancer.com", endpoint.RecordTypeCNAME, "owner-2"),
		},
	}
	expected := &plan.Changes{
		Create: []*endpoint.Endpoint{
			newEndpointWithOwner("new-record-1.test-zone.example.org", "new-loadbalancer-1.lb.com", endpoint.RecordTypeCNAME, "owner"),
			newEndpointWithOwner("new-record-1.test-zone.example.org", "\"heritage=external-dns,external-dns/owner=owner\"", endpoint.RecordTypeTXT, ""),
			newEndpointWithOwner("cname-new-record-1.test-zone.example.org", "\"heritage=external-dns,external-dns/owner=owner\"", endpoint.RecordTypeTXT, ""),
			newEndpointWithOwner("example", "new-loadbalancer-1.lb.com", endpoint.RecordTypeCNAME, "owner"),
			newEndpointWithOwner("example", "\"heritage=external-dns,external-dns/owner=owner\"", endpoint.RecordTypeTXT, ""),
			newEndpointWithOwner("cname-example", "\"heritage=external-dns,external-dns/owner=owner\"", endpoint.RecordTypeTXT, ""),
		},
		Delete: []*endpoint.Endpoint{
			newEndpointWithOwner("foobar.test-zone.example.org", "foobar.loadbalancer.com", endpoint.RecordTypeCNAME, "owner"),
			newEndpointWithOwner("foobar.test-zone.example.org", "\"heritage=external-dns,external-dns/owner=owner\"", endpoint.RecordTypeTXT, ""),
			newEndpointWithOwner("cname-foobar.test-zone.example.org", "\"heritage=external-dns,external-dns/owner=owner\"", endpoint.RecordTypeTXT, ""),
		},
		UpdateNew: []*endpoint.Endpoint{},
		UpdateOld: []*endpoint.Endpoint{},
	}
	p.OnApplyChanges = func(ctx context.Context, got *plan.Changes) {
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
		assert.Equal(t, nil, ctx.Value(provider.RecordsContextKey))
	}
	err := r.ApplyChanges(ctx, changes)
	require.NoError(t, err)
}

func TestCacheMethods(t *testing.T) {
	cache := []*endpoint.Endpoint{
		newEndpointWithOwner("thing.com", "1.2.3.4", "A", "owner"),
		newEndpointWithOwner("thing1.com", "1.2.3.6", "A", "owner"),
		newEndpointWithOwner("thing2.com", "1.2.3.4", "CNAME", "owner"),
		newEndpointWithOwner("thing3.com", "1.2.3.4", "A", "owner"),
		newEndpointWithOwner("thing4.com", "1.2.3.4", "A", "owner"),
	}
	registry := &TXTRegistry{
		recordsCache:  cache,
		cacheInterval: time.Hour,
	}

	expectedCacheAfterAdd := []*endpoint.Endpoint{
		newEndpointWithOwner("thing.com", "1.2.3.4", "A", "owner"),
		newEndpointWithOwner("thing1.com", "1.2.3.6", "A", "owner"),
		newEndpointWithOwner("thing2.com", "1.2.3.4", "CNAME", "owner"),
		newEndpointWithOwner("thing3.com", "1.2.3.4", "A", "owner"),
		newEndpointWithOwner("thing4.com", "1.2.3.4", "A", "owner"),
		newEndpointWithOwner("thing5.com", "1.2.3.5", "A", "owner"),
	}

	expectedCacheAfterUpdate := []*endpoint.Endpoint{
		newEndpointWithOwner("thing1.com", "1.2.3.6", "A", "owner"),
		newEndpointWithOwner("thing2.com", "1.2.3.4", "CNAME", "owner"),
		newEndpointWithOwner("thing3.com", "1.2.3.4", "A", "owner"),
		newEndpointWithOwner("thing4.com", "1.2.3.4", "A", "owner"),
		newEndpointWithOwner("thing5.com", "1.2.3.5", "A", "owner"),
		newEndpointWithOwner("thing.com", "1.2.3.6", "A", "owner2"),
	}

	expectedCacheAfterDelete := []*endpoint.Endpoint{
		newEndpointWithOwner("thing1.com", "1.2.3.6", "A", "owner"),
		newEndpointWithOwner("thing2.com", "1.2.3.4", "CNAME", "owner"),
		newEndpointWithOwner("thing3.com", "1.2.3.4", "A", "owner"),
		newEndpointWithOwner("thing4.com", "1.2.3.4", "A", "owner"),
		newEndpointWithOwner("thing5.com", "1.2.3.5", "A", "owner"),
	}
	// test add cache
	registry.addToCache(newEndpointWithOwner("thing5.com", "1.2.3.5", "A", "owner"))

	if !reflect.DeepEqual(expectedCacheAfterAdd, registry.recordsCache) {
		t.Fatalf("expected endpoints should match endpoints from cache: expected %v, but got %v", expectedCacheAfterAdd, registry.recordsCache)
	}

	// test update cache
	registry.removeFromCache(newEndpointWithOwner("thing.com", "1.2.3.4", "A", "owner"))
	registry.addToCache(newEndpointWithOwner("thing.com", "1.2.3.6", "A", "owner2"))
	// ensure it was updated
	if !reflect.DeepEqual(expectedCacheAfterUpdate, registry.recordsCache) {
		t.Fatalf("expected endpoints should match endpoints from cache: expected %v, but got %v", expectedCacheAfterUpdate, registry.recordsCache)
	}

	// test deleting a record
	registry.removeFromCache(newEndpointWithOwner("thing.com", "1.2.3.6", "A", "owner2"))
	// ensure it was deleted
	if !reflect.DeepEqual(expectedCacheAfterDelete, registry.recordsCache) {
		t.Fatalf("expected endpoints should match endpoints from cache: expected %v, but got %v", expectedCacheAfterDelete, registry.recordsCache)
	}
}

/**

helper methods

*/

func newEndpointWithOwner(dnsName, target, recordType, ownerID string) *endpoint.Endpoint {
	return newEndpointWithOwnerAndLabels(dnsName, target, recordType, ownerID, nil)
}

func newEndpointWithOwnerAndLabels(dnsName, target, recordType, ownerID string, labels endpoint.Labels) *endpoint.Endpoint {
	e := endpoint.NewEndpoint(dnsName, recordType, target)
	e.Labels[endpoint.OwnerLabelKey] = ownerID
	for k, v := range labels {
		e.Labels[k] = v
	}
	return e
}

func newEndpointWithOwnerResource(dnsName, target, recordType, ownerID, resource string) *endpoint.Endpoint {
	e := endpoint.NewEndpoint(dnsName, recordType, target)
	e.Labels[endpoint.OwnerLabelKey] = ownerID
	e.Labels[endpoint.ResourceLabelKey] = resource
	return e
}
