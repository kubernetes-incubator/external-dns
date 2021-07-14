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

package google

import (
	"fmt"
	"net/http"
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/net/context"
	dns "google.golang.org/api/dns/v1"
	"google.golang.org/api/googleapi"

	"sigs.k8s.io/external-dns/endpoint"
	"sigs.k8s.io/external-dns/internal/testutils"
	"sigs.k8s.io/external-dns/plan"
	"sigs.k8s.io/external-dns/provider"
)

var (
	testZones                    = map[string]*dns.ManagedZone{}
	testRecords                  = map[string]map[string]*dns.ResourceRecordSet{}
	googleDefaultBatchChangeSize = 4000
)

type mockManagedZonesCreateCall struct {
	project     string
	managedZone *dns.ManagedZone
}

func (m *mockManagedZonesCreateCall) Do(opts ...googleapi.CallOption) (*dns.ManagedZone, error) {
	zoneKey := zoneKey(m.project, m.managedZone.Name)

	if _, ok := testZones[zoneKey]; ok {
		return nil, &googleapi.Error{Code: http.StatusConflict}
	}

	testZones[zoneKey] = m.managedZone

	return m.managedZone, nil
}

type mockManagedZonesListCall struct {
	project string
}

func (m *mockManagedZonesListCall) Pages(ctx context.Context, f func(*dns.ManagedZonesListResponse) error) error {
	zones := []*dns.ManagedZone{}

	for k, v := range testZones {
		if strings.HasPrefix(k, m.project+"/") {
			zones = append(zones, v)
		}
	}

	return f(&dns.ManagedZonesListResponse{ManagedZones: zones})
}

type mockManagedZonesClient struct{}

func (m *mockManagedZonesClient) Create(project string, managedZone *dns.ManagedZone) managedZonesCreateCallInterface {
	return &mockManagedZonesCreateCall{project: project, managedZone: managedZone}
}

func (m *mockManagedZonesClient) List(project string) managedZonesListCallInterface {
	return &mockManagedZonesListCall{project: project}
}

type mockResourceRecordSetsListCall struct {
	project     string
	managedZone string
}

func (m *mockResourceRecordSetsListCall) Pages(ctx context.Context, f func(*dns.ResourceRecordSetsListResponse) error) error {
	zoneKey := zoneKey(m.project, m.managedZone)

	if _, ok := testZones[zoneKey]; !ok {
		return &googleapi.Error{Code: http.StatusNotFound}
	}

	resp := []*dns.ResourceRecordSet{}

	for _, v := range testRecords[zoneKey] {
		resp = append(resp, v)
	}

	return f(&dns.ResourceRecordSetsListResponse{Rrsets: resp})
}

type mockResourceRecordSetsClient struct{}

func (m *mockResourceRecordSetsClient) List(project string, managedZone string) resourceRecordSetsListCallInterface {
	return &mockResourceRecordSetsListCall{project: project, managedZone: managedZone}
}

type mockChangesCreateCall struct {
	project     string
	managedZone string
	change      *dns.Change
}

func (m *mockChangesCreateCall) Do(opts ...googleapi.CallOption) (*dns.Change, error) {
	zoneKey := zoneKey(m.project, m.managedZone)

	if _, ok := testZones[zoneKey]; !ok {
		return nil, &googleapi.Error{Code: http.StatusNotFound}
	}

	if _, ok := testRecords[zoneKey]; !ok {
		testRecords[zoneKey] = make(map[string]*dns.ResourceRecordSet)
	}

	for _, c := range append(m.change.Additions, m.change.Deletions...) {
		if !isValidRecordSet(c) {
			return nil, &googleapi.Error{
				Code:    http.StatusBadRequest,
				Message: fmt.Sprintf("invalid record: %v", c),
			}
		}
	}

	for _, del := range m.change.Deletions {
		recordKey := recordKey(del.Type, del.Name)
		delete(testRecords[zoneKey], recordKey)
	}

	for _, add := range m.change.Additions {
		recordKey := recordKey(add.Type, add.Name)
		testRecords[zoneKey][recordKey] = add
	}

	return m.change, nil
}

type mockChangesClient struct{}

func (m *mockChangesClient) Create(project string, managedZone string, change *dns.Change) changesCreateCallInterface {
	return &mockChangesCreateCall{project: project, managedZone: managedZone, change: change}
}

func zoneKey(project, zoneName string) string {
	return project + "/" + zoneName
}

func recordKey(recordType, recordName string) string {
	return recordType + "/" + recordName
}

func isValidRecordSet(recordSet *dns.ResourceRecordSet) bool {
	if !hasTrailingDot(recordSet.Name) {
		return false
	}

	switch recordSet.Type {
	case endpoint.RecordTypeCNAME:
		for _, rrd := range recordSet.Rrdatas {
			if !hasTrailingDot(rrd) {
				return false
			}
		}
	case endpoint.RecordTypeA, endpoint.RecordTypeTXT:
		for _, rrd := range recordSet.Rrdatas {
			if hasTrailingDot(rrd) {
				return false
			}
		}
	default:
		panic("unhandled record type")
	}

	return true
}

func hasTrailingDot(target string) bool {
	return strings.HasSuffix(target, ".")
}

func TestGoogleZonesIDFilter(t *testing.T) {
	provider := newGoogleProviderZoneOverlap(t, endpoint.NewDomainFilter([]string{"cluster.local."}), provider.NewZoneIDFilter([]string{"10002"}), provider.NewZoneTypeFilter(""), false, []*endpoint.Endpoint{})

	zones, err := provider.Zones(context.Background())
	require.NoError(t, err)

	validateZones(t, zones, map[string]*dns.ManagedZone{
		"internal-2": {Name: "internal-2", DnsName: "cluster.local.", Id: 10002, Visibility: "private"},
	})
}

func TestGoogleZonesNameFilter(t *testing.T) {
	provider := newGoogleProviderZoneOverlap(t, endpoint.NewDomainFilter([]string{"cluster.local."}), provider.NewZoneIDFilter([]string{"internal-2"}), provider.NewZoneTypeFilter(""), false, []*endpoint.Endpoint{})

	zones, err := provider.Zones(context.Background())
	require.NoError(t, err)

	validateZones(t, zones, map[string]*dns.ManagedZone{
		"internal-2": {Name: "internal-2", DnsName: "cluster.local.", Id: 10002, Visibility: "private"},
	})
}

func TestGoogleZonesVisibilityFilterPublic(t *testing.T) {
	provider := newGoogleProviderZoneOverlap(t, endpoint.NewDomainFilter([]string{"cluster.local."}), provider.NewZoneIDFilter([]string{"split-horizon-1"}), provider.NewZoneTypeFilter("public"), false, []*endpoint.Endpoint{})

	zones, err := provider.Zones(context.Background())
	require.NoError(t, err)

	validateZones(t, zones, map[string]*dns.ManagedZone{
		"split-horizon-1": {Name: "split-horizon-1", DnsName: "cluster.local.", Id: 10001, Visibility: "public"},
	})
}

func TestGoogleZonesVisibilityFilterPrivate(t *testing.T) {
	provider := newGoogleProviderZoneOverlap(t, endpoint.NewDomainFilter([]string{"cluster.local."}), provider.NewZoneIDFilter([]string{"split-horizon-1"}), provider.NewZoneTypeFilter("public"), false, []*endpoint.Endpoint{})

	zones, err := provider.Zones(context.Background())
	require.NoError(t, err)

	validateZones(t, zones, map[string]*dns.ManagedZone{
		"split-horizon-1": {Name: "split-horizon-1", DnsName: "cluster.local.", Id: 10001, Visibility: "public"},
	})
}

func TestGoogleZones(t *testing.T) {
	provider := newGoogleProvider(t, endpoint.NewDomainFilter([]string{"ext-dns-test-2.gcp.zalan.do."}), provider.NewZoneIDFilter([]string{""}), false, []*endpoint.Endpoint{})

	zones, err := provider.Zones(context.Background())
	require.NoError(t, err)

	validateZones(t, zones, map[string]*dns.ManagedZone{
		"zone-1-ext-dns-test-2-gcp-zalan-do": {Name: "zone-1-ext-dns-test-2-gcp-zalan-do", DnsName: "zone-1.ext-dns-test-2.gcp.zalan.do."},
		"zone-2-ext-dns-test-2-gcp-zalan-do": {Name: "zone-2-ext-dns-test-2-gcp-zalan-do", DnsName: "zone-2.ext-dns-test-2.gcp.zalan.do."},
		"zone-3-ext-dns-test-2-gcp-zalan-do": {Name: "zone-3-ext-dns-test-2-gcp-zalan-do", DnsName: "zone-3.ext-dns-test-2.gcp.zalan.do."},
	})
}

func TestGoogleRecords(t *testing.T) {
	originalEndpoints := []*endpoint.Endpoint{
		endpoint.NewEndpointWithTTL("list-test.zone-1.ext-dns-test-2.gcp.zalan.do", endpoint.RecordTypeA, endpoint.TTL(1), "1.2.3.4"),
		endpoint.NewEndpointWithTTL("list-test.zone-2.ext-dns-test-2.gcp.zalan.do", endpoint.RecordTypeA, endpoint.TTL(2), "8.8.8.8"),
		endpoint.NewEndpointWithTTL("list-test-alias.zone-1.ext-dns-test-2.gcp.zalan.do", endpoint.RecordTypeCNAME, endpoint.TTL(3), "foo.elb.amazonaws.com"),
	}

	provider := newGoogleProvider(t, endpoint.NewDomainFilter([]string{"ext-dns-test-2.gcp.zalan.do."}), provider.NewZoneIDFilter([]string{""}), false, originalEndpoints)

	records, err := provider.Records(context.Background())
	require.NoError(t, err)

	validateEndpoints(t, records, originalEndpoints)
}

func TestGoogleRecordsFilter(t *testing.T) {
	originalEndpoints := []*endpoint.Endpoint{
		endpoint.NewEndpointWithTTL("update-test.zone-1.ext-dns-test-2.gcp.zalan.do", endpoint.RecordTypeA, googleRecordTTL, "8.8.8.8"),
		endpoint.NewEndpointWithTTL("delete-test.zone-1.ext-dns-test-2.gcp.zalan.do", endpoint.RecordTypeA, googleRecordTTL, "8.8.8.8"),
		endpoint.NewEndpointWithTTL("update-test.zone-2.ext-dns-test-2.gcp.zalan.do", endpoint.RecordTypeA, googleRecordTTL, "8.8.4.4"),
		endpoint.NewEndpointWithTTL("delete-test.zone-2.ext-dns-test-2.gcp.zalan.do", endpoint.RecordTypeA, googleRecordTTL, "8.8.4.4"),
		endpoint.NewEndpointWithTTL("update-test-cname.zone-1.ext-dns-test-2.gcp.zalan.do", endpoint.RecordTypeCNAME, googleRecordTTL, "bar.elb.amazonaws.com"),
		endpoint.NewEndpointWithTTL("delete-test-cname.zone-1.ext-dns-test-2.gcp.zalan.do", endpoint.RecordTypeCNAME, googleRecordTTL, "qux.elb.amazonaws.com"),
	}

	provider := newGoogleProvider(
		t,
		endpoint.NewDomainFilter([]string{
			// our two valid zones
			"zone-1.ext-dns-test-2.gcp.zalan.do.",
			"zone-2.ext-dns-test-2.gcp.zalan.do.",
			// we filter for a zone that doesn't exist, should have no effect.
			"zone-0.ext-dns-test-2.gcp.zalan.do.",
			// there exists a third zone "zone-3" that we want to exclude from being managed.
		}),
		provider.NewZoneIDFilter([]string{""}),
		false,
		originalEndpoints,
	)

	// these records should be filtered out since they don't match a hosted zone or domain filter.
	ignoredEndpoints := []*endpoint.Endpoint{
		endpoint.NewEndpoint("filter-create-test.zone-0.ext-dns-test-2.gcp.zalan.do", endpoint.RecordTypeA, "4.2.2.2"),
		endpoint.NewEndpoint("filter-update-test.zone-0.ext-dns-test-2.gcp.zalan.do", endpoint.RecordTypeA, "4.2.2.2"),
		endpoint.NewEndpoint("filter-delete-test.zone-0.ext-dns-test-2.gcp.zalan.do", endpoint.RecordTypeA, "4.2.2.2"),
		endpoint.NewEndpoint("filter-create-test.zone-3.ext-dns-test-2.gcp.zalan.do", endpoint.RecordTypeA, "4.2.2.2"),
		endpoint.NewEndpoint("filter-update-test.zone-3.ext-dns-test-2.gcp.zalan.do", endpoint.RecordTypeA, "4.2.2.2"),
		endpoint.NewEndpoint("filter-delete-test.zone-3.ext-dns-test-2.gcp.zalan.do", endpoint.RecordTypeA, "4.2.2.2"),
	}

	require.NoError(t, provider.CreateRecords(ignoredEndpoints))

	records, err := provider.Records(context.Background())
	require.NoError(t, err)

	// assert that due to filtering no changes were made.
	validateEndpoints(t, records, originalEndpoints)
}

func TestGoogleCreateRecords(t *testing.T) {
	provider := newGoogleProvider(t, endpoint.NewDomainFilter([]string{"ext-dns-test-2.gcp.zalan.do."}), provider.NewZoneIDFilter([]string{""}), false, []*endpoint.Endpoint{})

	records := []*endpoint.Endpoint{
		endpoint.NewEndpoint("create-test.zone-1.ext-dns-test-2.gcp.zalan.do", endpoint.RecordTypeA, "1.2.3.4"),
		endpoint.NewEndpointWithTTL("create-test-ttl.zone-2.ext-dns-test-2.gcp.zalan.do", endpoint.RecordTypeA, endpoint.TTL(15), "8.8.8.8"),
		endpoint.NewEndpoint("create-test-cname.zone-1.ext-dns-test-2.gcp.zalan.do", endpoint.RecordTypeCNAME, "foo.elb.amazonaws.com"),
	}

	require.NoError(t, provider.CreateRecords(records))

	records, err := provider.Records(context.Background())
	require.NoError(t, err)

	validateEndpoints(t, records, []*endpoint.Endpoint{
		endpoint.NewEndpointWithTTL("create-test.zone-1.ext-dns-test-2.gcp.zalan.do", endpoint.RecordTypeA, googleRecordTTL, "1.2.3.4"),
		endpoint.NewEndpointWithTTL("create-test-ttl.zone-2.ext-dns-test-2.gcp.zalan.do", endpoint.RecordTypeA, endpoint.TTL(15), "8.8.8.8"),
		endpoint.NewEndpointWithTTL("create-test-cname.zone-1.ext-dns-test-2.gcp.zalan.do", endpoint.RecordTypeCNAME, googleRecordTTL, "foo.elb.amazonaws.com"),
	})
}

func TestGoogleUpdateRecords(t *testing.T) {
	currentRecords := []*endpoint.Endpoint{
		endpoint.NewEndpointWithTTL("update-test.zone-1.ext-dns-test-2.gcp.zalan.do", endpoint.RecordTypeA, googleRecordTTL, "8.8.8.8"),
		endpoint.NewEndpointWithTTL("update-test-ttl.zone-2.ext-dns-test-2.gcp.zalan.do", endpoint.RecordTypeA, endpoint.TTL(15), "8.8.4.4"),
		endpoint.NewEndpointWithTTL("update-test-cname.zone-1.ext-dns-test-2.gcp.zalan.do", endpoint.RecordTypeCNAME, googleRecordTTL, "foo.elb.amazonaws.com"),
	}
	provider := newGoogleProvider(t, endpoint.NewDomainFilter([]string{"ext-dns-test-2.gcp.zalan.do."}), provider.NewZoneIDFilter([]string{""}), false, currentRecords)
	updatedRecords := []*endpoint.Endpoint{
		endpoint.NewEndpoint("update-test.zone-1.ext-dns-test-2.gcp.zalan.do", endpoint.RecordTypeA, "1.2.3.4"),
		endpoint.NewEndpointWithTTL("update-test-ttl.zone-2.ext-dns-test-2.gcp.zalan.do", endpoint.RecordTypeA, endpoint.TTL(25), "4.3.2.1"),
		endpoint.NewEndpoint("update-test-cname.zone-1.ext-dns-test-2.gcp.zalan.do", endpoint.RecordTypeCNAME, "bar.elb.amazonaws.com"),
	}

	require.NoError(t, provider.UpdateRecords(updatedRecords, currentRecords))

	records, err := provider.Records(context.Background())
	require.NoError(t, err)

	validateEndpoints(t, records, []*endpoint.Endpoint{
		endpoint.NewEndpointWithTTL("update-test.zone-1.ext-dns-test-2.gcp.zalan.do", endpoint.RecordTypeA, googleRecordTTL, "1.2.3.4"),
		endpoint.NewEndpointWithTTL("update-test-ttl.zone-2.ext-dns-test-2.gcp.zalan.do", endpoint.RecordTypeA, endpoint.TTL(25), "4.3.2.1"),
		endpoint.NewEndpointWithTTL("update-test-cname.zone-1.ext-dns-test-2.gcp.zalan.do", endpoint.RecordTypeCNAME, googleRecordTTL, "bar.elb.amazonaws.com"),
	})
}

func TestGoogleDeleteRecords(t *testing.T) {
	originalEndpoints := []*endpoint.Endpoint{
		endpoint.NewEndpointWithTTL("delete-test.zone-1.ext-dns-test-2.gcp.zalan.do", endpoint.RecordTypeA, googleRecordTTL, "1.2.3.4"),
		endpoint.NewEndpointWithTTL("delete-test.zone-2.ext-dns-test-2.gcp.zalan.do", endpoint.RecordTypeA, googleRecordTTL, "8.8.8.8"),
		endpoint.NewEndpointWithTTL("delete-test-cname.zone-1.ext-dns-test-2.gcp.zalan.do", endpoint.RecordTypeCNAME, googleRecordTTL, "baz.elb.amazonaws.com"),
	}

	provider := newGoogleProvider(t, endpoint.NewDomainFilter([]string{"ext-dns-test-2.gcp.zalan.do."}), provider.NewZoneIDFilter([]string{""}), false, originalEndpoints)

	require.NoError(t, provider.DeleteRecords(originalEndpoints))

	records, err := provider.Records(context.Background())
	require.NoError(t, err)

	validateEndpoints(t, records, []*endpoint.Endpoint{})
}

func TestGoogleApplyChanges(t *testing.T) {
	provider := newGoogleProvider(
		t,
		endpoint.NewDomainFilter([]string{
			// our two valid zones
			"zone-1.ext-dns-test-2.gcp.zalan.do.",
			"zone-2.ext-dns-test-2.gcp.zalan.do.",
			// we filter for a zone that doesn't exist, should have no effect.
			"zone-0.ext-dns-test-2.gcp.zalan.do.",
			// there exists a third zone "zone-3" that we want to exclude from being managed.
		}),
		provider.NewZoneIDFilter([]string{""}),
		false,
		[]*endpoint.Endpoint{
			endpoint.NewEndpointWithTTL("update-test.zone-1.ext-dns-test-2.gcp.zalan.do", endpoint.RecordTypeA, googleRecordTTL, "8.8.8.8"),
			endpoint.NewEndpointWithTTL("delete-test.zone-1.ext-dns-test-2.gcp.zalan.do", endpoint.RecordTypeA, googleRecordTTL, "8.8.8.8"),
			endpoint.NewEndpointWithTTL("update-test-ttl.zone-2.ext-dns-test-2.gcp.zalan.do", endpoint.RecordTypeA, endpoint.TTL(10), "8.8.4.4"),
			endpoint.NewEndpointWithTTL("delete-test.zone-2.ext-dns-test-2.gcp.zalan.do", endpoint.RecordTypeA, googleRecordTTL, "8.8.4.4"),
			endpoint.NewEndpointWithTTL("update-test-cname.zone-1.ext-dns-test-2.gcp.zalan.do", endpoint.RecordTypeCNAME, googleRecordTTL, "bar.elb.amazonaws.com"),
			endpoint.NewEndpointWithTTL("delete-test-cname.zone-1.ext-dns-test-2.gcp.zalan.do", endpoint.RecordTypeCNAME, googleRecordTTL, "qux.elb.amazonaws.com"),
		},
	)

	createRecords := []*endpoint.Endpoint{
		endpoint.NewEndpoint("create-test.zone-1.ext-dns-test-2.gcp.zalan.do", endpoint.RecordTypeA, "8.8.8.8"),
		endpoint.NewEndpointWithTTL("create-test-ttl.zone-2.ext-dns-test-2.gcp.zalan.do", endpoint.RecordTypeA, endpoint.TTL(15), "8.8.4.4"),
		endpoint.NewEndpoint("create-test-cname.zone-1.ext-dns-test-2.gcp.zalan.do", endpoint.RecordTypeCNAME, "foo.elb.amazonaws.com"),
		endpoint.NewEndpoint("filter-create-test.zone-3.ext-dns-test-2.gcp.zalan.do", endpoint.RecordTypeA, "4.2.2.2"),
		endpoint.NewEndpoint("nomatch-create-test.zone-0.ext-dns-test-2.gcp.zalan.do", endpoint.RecordTypeA, "4.2.2.1"),
	}

	currentRecords := []*endpoint.Endpoint{
		endpoint.NewEndpoint("update-test.zone-1.ext-dns-test-2.gcp.zalan.do", endpoint.RecordTypeA, "8.8.8.8"),
		endpoint.NewEndpoint("update-test.zone-2.ext-dns-test-2.gcp.zalan.do", endpoint.RecordTypeA, "8.8.4.4"),
		endpoint.NewEndpoint("update-test-cname.zone-1.ext-dns-test-2.gcp.zalan.do", endpoint.RecordTypeCNAME, "bar.elb.amazonaws.com"),
		endpoint.NewEndpoint("filter-update-test.zone-3.ext-dns-test-2.gcp.zalan.do", endpoint.RecordTypeA, "4.2.2.2"),
	}
	updatedRecords := []*endpoint.Endpoint{
		endpoint.NewEndpoint("update-test.zone-1.ext-dns-test-2.gcp.zalan.do", endpoint.RecordTypeA, "1.2.3.4"),
		endpoint.NewEndpointWithTTL("update-test-ttl.zone-2.ext-dns-test-2.gcp.zalan.do", endpoint.RecordTypeA, endpoint.TTL(25), "4.3.2.1"),
		endpoint.NewEndpoint("update-test-cname.zone-1.ext-dns-test-2.gcp.zalan.do", endpoint.RecordTypeCNAME, "baz.elb.amazonaws.com"),
		endpoint.NewEndpoint("filter-update-test.zone-3.ext-dns-test-2.gcp.zalan.do", endpoint.RecordTypeA, "5.6.7.8"),
		endpoint.NewEndpoint("nomatch-update-test.zone-0.ext-dns-test-2.gcp.zalan.do", endpoint.RecordTypeA, "8.7.6.5"),
	}

	deleteRecords := []*endpoint.Endpoint{
		endpoint.NewEndpoint("delete-test.zone-1.ext-dns-test-2.gcp.zalan.do", endpoint.RecordTypeA, "8.8.8.8"),
		endpoint.NewEndpoint("delete-test.zone-2.ext-dns-test-2.gcp.zalan.do", endpoint.RecordTypeA, "8.8.4.4"),
		endpoint.NewEndpoint("delete-test-cname.zone-1.ext-dns-test-2.gcp.zalan.do", endpoint.RecordTypeCNAME, "qux.elb.amazonaws.com"),
		endpoint.NewEndpoint("filter-delete-test.zone-3.ext-dns-test-2.gcp.zalan.do", endpoint.RecordTypeA, "4.2.2.2"),
		endpoint.NewEndpoint("nomatch-delete-test.zone-0.ext-dns-test-2.gcp.zalan.do", endpoint.RecordTypeA, "4.2.2.1"),
	}

	changes := &plan.Changes{
		Create:    createRecords,
		UpdateNew: updatedRecords,
		UpdateOld: currentRecords,
		Delete:    deleteRecords,
	}

	require.NoError(t, provider.ApplyChanges(context.Background(), changes))

	records, err := provider.Records(context.Background())
	require.NoError(t, err)

	validateEndpoints(t, records, []*endpoint.Endpoint{
		endpoint.NewEndpointWithTTL("create-test.zone-1.ext-dns-test-2.gcp.zalan.do", endpoint.RecordTypeA, googleRecordTTL, "8.8.8.8"),
		endpoint.NewEndpointWithTTL("update-test.zone-1.ext-dns-test-2.gcp.zalan.do", endpoint.RecordTypeA, googleRecordTTL, "1.2.3.4"),
		endpoint.NewEndpointWithTTL("create-test-ttl.zone-2.ext-dns-test-2.gcp.zalan.do", endpoint.RecordTypeA, endpoint.TTL(15), "8.8.4.4"),
		endpoint.NewEndpointWithTTL("update-test-ttl.zone-2.ext-dns-test-2.gcp.zalan.do", endpoint.RecordTypeA, endpoint.TTL(25), "4.3.2.1"),
		endpoint.NewEndpointWithTTL("create-test-cname.zone-1.ext-dns-test-2.gcp.zalan.do", endpoint.RecordTypeCNAME, googleRecordTTL, "foo.elb.amazonaws.com"),
		endpoint.NewEndpointWithTTL("update-test-cname.zone-1.ext-dns-test-2.gcp.zalan.do", endpoint.RecordTypeCNAME, googleRecordTTL, "baz.elb.amazonaws.com"),
	})
}

func TestGoogleApplyChangesDryRun(t *testing.T) {
	originalEndpoints := []*endpoint.Endpoint{
		endpoint.NewEndpointWithTTL("update-test.zone-1.ext-dns-test-2.gcp.zalan.do", endpoint.RecordTypeA, googleRecordTTL, "8.8.8.8"),
		endpoint.NewEndpointWithTTL("delete-test.zone-1.ext-dns-test-2.gcp.zalan.do", endpoint.RecordTypeA, googleRecordTTL, "8.8.8.8"),
		endpoint.NewEndpointWithTTL("update-test.zone-2.ext-dns-test-2.gcp.zalan.do", endpoint.RecordTypeA, googleRecordTTL, "8.8.4.4"),
		endpoint.NewEndpointWithTTL("delete-test.zone-2.ext-dns-test-2.gcp.zalan.do", endpoint.RecordTypeA, googleRecordTTL, "8.8.4.4"),
		endpoint.NewEndpointWithTTL("update-test-cname.zone-1.ext-dns-test-2.gcp.zalan.do", endpoint.RecordTypeCNAME, googleRecordTTL, "bar.elb.amazonaws.com"),
		endpoint.NewEndpointWithTTL("delete-test-cname.zone-1.ext-dns-test-2.gcp.zalan.do", endpoint.RecordTypeCNAME, googleRecordTTL, "qux.elb.amazonaws.com"),
	}

	provider := newGoogleProvider(t, endpoint.NewDomainFilter([]string{"ext-dns-test-2.gcp.zalan.do."}), provider.NewZoneIDFilter([]string{""}), true, originalEndpoints)

	createRecords := []*endpoint.Endpoint{
		endpoint.NewEndpoint("create-test.zone-1.ext-dns-test-2.gcp.zalan.do", endpoint.RecordTypeA, "8.8.8.8"),
		endpoint.NewEndpoint("create-test.zone-2.ext-dns-test-2.gcp.zalan.do", endpoint.RecordTypeA, "8.8.4.4"),
		endpoint.NewEndpoint("create-test-cname.zone-1.ext-dns-test-2.gcp.zalan.do", endpoint.RecordTypeCNAME, "foo.elb.amazonaws.com"),
	}

	currentRecords := []*endpoint.Endpoint{
		endpoint.NewEndpoint("update-test.zone-1.ext-dns-test-2.gcp.zalan.do", endpoint.RecordTypeA, "8.8.8.8"),
		endpoint.NewEndpoint("update-test.zone-2.ext-dns-test-2.gcp.zalan.do", endpoint.RecordTypeA, "8.8.4.4"),
		endpoint.NewEndpoint("update-test-cname.zone-1.ext-dns-test-2.gcp.zalan.do", endpoint.RecordTypeCNAME, "bar.elb.amazonaws.com"),
	}
	updatedRecords := []*endpoint.Endpoint{
		endpoint.NewEndpoint("update-test.zone-1.ext-dns-test-2.gcp.zalan.do", endpoint.RecordTypeA, "1.2.3.4"),
		endpoint.NewEndpoint("update-test.zone-2.ext-dns-test-2.gcp.zalan.do", endpoint.RecordTypeA, "4.3.2.1"),
		endpoint.NewEndpoint("update-test-cname.zone-1.ext-dns-test-2.gcp.zalan.do", endpoint.RecordTypeCNAME, "baz.elb.amazonaws.com"),
	}

	deleteRecords := []*endpoint.Endpoint{
		endpoint.NewEndpoint("delete-test.zone-1.ext-dns-test-2.gcp.zalan.do", endpoint.RecordTypeA, "8.8.8.8"),
		endpoint.NewEndpoint("delete-test.zone-2.ext-dns-test-2.gcp.zalan.do", endpoint.RecordTypeA, "8.8.4.4"),
		endpoint.NewEndpoint("delete-test-cname.zone-1.ext-dns-test-2.gcp.zalan.do", endpoint.RecordTypeCNAME, "qux.elb.amazonaws.com"),
	}

	changes := &plan.Changes{
		Create:    createRecords,
		UpdateNew: updatedRecords,
		UpdateOld: currentRecords,
		Delete:    deleteRecords,
	}

	ctx := context.Background()
	require.NoError(t, provider.ApplyChanges(ctx, changes))

	records, err := provider.Records(ctx)
	require.NoError(t, err)

	validateEndpoints(t, records, originalEndpoints)
}

func TestGoogleApplyChangesEmpty(t *testing.T) {
	provider := newGoogleProvider(t, endpoint.NewDomainFilter([]string{"ext-dns-test-2.gcp.zalan.do."}), provider.NewZoneIDFilter([]string{""}), false, []*endpoint.Endpoint{})
	assert.NoError(t, provider.ApplyChanges(context.Background(), &plan.Changes{}))
}

func TestNewFilteredRecords(t *testing.T) {
	provider := newGoogleProvider(t, endpoint.NewDomainFilter([]string{"ext-dns-test-2.gcp.zalan.do."}), provider.NewZoneIDFilter([]string{""}), false, []*endpoint.Endpoint{})

	records := provider.newFilteredRecords([]*endpoint.Endpoint{
		endpoint.NewEndpointWithTTL("update-test.zone-2.ext-dns-test-2.gcp.zalan.do", endpoint.RecordTypeA, 1, "8.8.4.4"),
		endpoint.NewEndpointWithTTL("delete-test.zone-2.ext-dns-test-2.gcp.zalan.do", endpoint.RecordTypeA, 120, "8.8.4.4"),
		endpoint.NewEndpointWithTTL("update-test-cname.zone-1.ext-dns-test-2.gcp.zalan.do", endpoint.RecordTypeCNAME, 4000, "bar.elb.amazonaws.com"),
		// test fallback to Ttl:300 when Ttl==0 :
		endpoint.NewEndpointWithTTL("update-test.zone-1.ext-dns-test-2.gcp.zalan.do", endpoint.RecordTypeA, 0, "8.8.8.8"),
		endpoint.NewEndpoint("delete-test.zone-1.ext-dns-test-2.gcp.zalan.do", endpoint.RecordTypeA, "8.8.8.8"),
		endpoint.NewEndpoint("delete-test-cname.zone-1.ext-dns-test-2.gcp.zalan.do", endpoint.RecordTypeCNAME, "qux.elb.amazonaws.com"),
	})

	validateChangeRecords(t, records, []*dns.ResourceRecordSet{
		{Name: "update-test.zone-2.ext-dns-test-2.gcp.zalan.do.", Rrdatas: []string{"8.8.4.4"}, Type: "A", Ttl: 1},
		{Name: "delete-test.zone-2.ext-dns-test-2.gcp.zalan.do.", Rrdatas: []string{"8.8.4.4"}, Type: "A", Ttl: 120},
		{Name: "update-test-cname.zone-1.ext-dns-test-2.gcp.zalan.do.", Rrdatas: []string{"bar.elb.amazonaws.com."}, Type: "CNAME", Ttl: 4000},
		{Name: "update-test.zone-1.ext-dns-test-2.gcp.zalan.do.", Rrdatas: []string{"8.8.8.8"}, Type: "A", Ttl: 300},
		{Name: "delete-test.zone-1.ext-dns-test-2.gcp.zalan.do.", Rrdatas: []string{"8.8.8.8"}, Type: "A", Ttl: 300},
		{Name: "delete-test-cname.zone-1.ext-dns-test-2.gcp.zalan.do.", Rrdatas: []string{"qux.elb.amazonaws.com."}, Type: "CNAME", Ttl: 300},
	})
}

func TestSeparateChanges(t *testing.T) {
	change := &dns.Change{
		Additions: []*dns.ResourceRecordSet{
			{Name: "qux.foo.example.org.", Ttl: 1},
			{Name: "qux.bar.example.org.", Ttl: 2},
		},
		Deletions: []*dns.ResourceRecordSet{
			{Name: "wambo.foo.example.org.", Ttl: 10},
			{Name: "wambo.bar.example.org.", Ttl: 20},
		},
	}

	zones := map[string]*dns.ManagedZone{
		"foo-example-org": {
			Name:    "foo-example-org",
			DnsName: "foo.example.org.",
		},
		"bar-example-org": {
			Name:    "bar-example-org",
			DnsName: "bar.example.org.",
		},
		"baz-example-org": {
			Name:    "baz-example-org",
			DnsName: "baz.example.org.",
		},
	}

	changes := separateChange(zones, change)
	require.Len(t, changes, 2)

	validateChange(t, changes["foo-example-org"], &dns.Change{
		Additions: []*dns.ResourceRecordSet{
			{Name: "qux.foo.example.org.", Ttl: 1},
		},
		Deletions: []*dns.ResourceRecordSet{
			{Name: "wambo.foo.example.org.", Ttl: 10},
		},
	})

	validateChange(t, changes["bar-example-org"], &dns.Change{
		Additions: []*dns.ResourceRecordSet{
			{Name: "qux.bar.example.org.", Ttl: 2},
		},
		Deletions: []*dns.ResourceRecordSet{
			{Name: "wambo.bar.example.org.", Ttl: 20},
		},
	})
}

func TestGoogleBatchChangeSet(t *testing.T) {
	cs := &dns.Change{}

	for i := 1; i <= googleDefaultBatchChangeSize; i += 2 {
		cs.Additions = append(cs.Additions, &dns.ResourceRecordSet{
			Name: fmt.Sprintf("host-%d.example.org.", i),
			Ttl:  2,
		})
		cs.Deletions = append(cs.Deletions, &dns.ResourceRecordSet{
			Name: fmt.Sprintf("host-%d.example.org.", i),
			Ttl:  20,
		})
	}

	batchCs := batchChange(cs, googleDefaultBatchChangeSize)

	require.Equal(t, 1, len(batchCs))

	sortChangesByName(cs)
	validateChange(t, batchCs[0], cs)
}

func TestGoogleBatchChangeSetExceeding(t *testing.T) {
	cs := &dns.Change{}
	const testCount = 50
	const testLimit = 11
	const expectedBatchCount = 5

	for i := 1; i <= testCount; i += 2 {
		cs.Additions = append(cs.Additions, &dns.ResourceRecordSet{
			Name: fmt.Sprintf("host-%d.example.org.", i),
			Ttl:  2,
		})
		cs.Deletions = append(cs.Deletions, &dns.ResourceRecordSet{
			Name: fmt.Sprintf("host-%d.example.org.", i),
			Ttl:  20,
		})
	}

	batchCs := batchChange(cs, testLimit)

	require.Equal(t, expectedBatchCount, len(batchCs))

	dnsChange := &dns.Change{}
	for _, c := range batchCs {
		dnsChange.Additions = append(dnsChange.Additions, c.Additions...)
		dnsChange.Deletions = append(dnsChange.Deletions, c.Deletions...)
	}

	require.Equal(t, len(cs.Additions), len(dnsChange.Additions))
	require.Equal(t, len(cs.Deletions), len(dnsChange.Deletions))

	sortChangesByName(cs)
	sortChangesByName(dnsChange)

	validateChange(t, dnsChange, cs)
}

func TestGoogleBatchChangeSetExceedingNameChange(t *testing.T) {
	cs := &dns.Change{}
	const testLimit = 1

	cs.Additions = append(cs.Additions, &dns.ResourceRecordSet{
		Name: "host-1.example.org.",
		Ttl:  2,
	})
	cs.Deletions = append(cs.Deletions, &dns.ResourceRecordSet{
		Name: "host-1.example.org.",
		Ttl:  20,
	})

	batchCs := batchChange(cs, testLimit)

	require.Equal(t, 0, len(batchCs))
}

func sortChangesByName(cs *dns.Change) {
	sort.SliceStable(cs.Additions, func(i, j int) bool {
		return cs.Additions[i].Name < cs.Additions[j].Name
	})

	sort.SliceStable(cs.Deletions, func(i, j int) bool {
		return cs.Deletions[i].Name < cs.Deletions[j].Name
	})
}

func validateZones(t *testing.T, zones map[string]*dns.ManagedZone, expected map[string]*dns.ManagedZone) {
	require.Len(t, zones, len(expected))

	for i, zone := range zones {
		validateZone(t, zone, expected[i])
	}
}

func validateZone(t *testing.T, zone *dns.ManagedZone, expected *dns.ManagedZone) {
	assert.Equal(t, expected.Name, zone.Name)
	assert.Equal(t, expected.DnsName, zone.DnsName)
	assert.Equal(t, expected.Visibility, zone.Visibility)
}

func validateChange(t *testing.T, change *dns.Change, expected *dns.Change) {
	validateChangeRecords(t, change.Additions, expected.Additions)
	validateChangeRecords(t, change.Deletions, expected.Deletions)
}

func validateChangeRecords(t *testing.T, records []*dns.ResourceRecordSet, expected []*dns.ResourceRecordSet) {
	require.Len(t, records, len(expected))

	for i := range records {
		validateChangeRecord(t, records[i], expected[i])
	}
}

func validateChangeRecord(t *testing.T, record *dns.ResourceRecordSet, expected *dns.ResourceRecordSet) {
	assert.Equal(t, expected.Name, record.Name)
	assert.Equal(t, expected.Rrdatas, record.Rrdatas)
	assert.Equal(t, expected.Ttl, record.Ttl)
	assert.Equal(t, expected.Type, record.Type)
}

func newGoogleProviderZoneOverlap(t *testing.T, domainFilter endpoint.DomainFilter, zoneIDFilter provider.ZoneIDFilter, zoneTypeFilter provider.ZoneTypeFilter, dryRun bool, records []*endpoint.Endpoint) *GoogleProvider {
	provider := &GoogleProvider{
		project:                  "zalando-external-dns-test",
		dryRun:                   false,
		domainFilter:             domainFilter,
		zoneIDFilter:             zoneIDFilter,
		zoneTypeFilter:           zoneTypeFilter,
		resourceRecordSetsClient: &mockResourceRecordSetsClient{},
		managedZonesClient:       &mockManagedZonesClient{},
		changesClient:            &mockChangesClient{},
	}

	createZone(t, provider, &dns.ManagedZone{
		Name:       "internal-1",
		DnsName:    "cluster.local.",
		Id:         10001,
		Visibility: "private",
	})

	createZone(t, provider, &dns.ManagedZone{
		Name:       "internal-2",
		DnsName:    "cluster.local.",
		Id:         10002,
		Visibility: "private",
	})

	createZone(t, provider, &dns.ManagedZone{
		Name:       "internal-3",
		DnsName:    "cluster.local.",
		Id:         10003,
		Visibility: "private",
	})

	createZone(t, provider, &dns.ManagedZone{
		Name:       "split-horizon-1",
		DnsName:    "cluster.local.",
		Id:         10004,
		Visibility: "public",
	})

	createZone(t, provider, &dns.ManagedZone{
		Name:       "split-horizon-1",
		DnsName:    "cluster.local.",
		Id:         10004,
		Visibility: "private",
	})

	provider.dryRun = dryRun

	return provider

}

func newGoogleProvider(t *testing.T, domainFilter endpoint.DomainFilter, zoneIDFilter provider.ZoneIDFilter, dryRun bool, records []*endpoint.Endpoint) *GoogleProvider {
	provider := &GoogleProvider{
		project:                  "zalando-external-dns-test",
		dryRun:                   false,
		domainFilter:             domainFilter,
		zoneIDFilter:             zoneIDFilter,
		resourceRecordSetsClient: &mockResourceRecordSetsClient{},
		managedZonesClient:       &mockManagedZonesClient{},
		changesClient:            &mockChangesClient{},
	}

	createZone(t, provider, &dns.ManagedZone{
		Name:    "zone-1-ext-dns-test-2-gcp-zalan-do",
		DnsName: "zone-1.ext-dns-test-2.gcp.zalan.do.",
	})

	createZone(t, provider, &dns.ManagedZone{
		Name:    "zone-2-ext-dns-test-2-gcp-zalan-do",
		DnsName: "zone-2.ext-dns-test-2.gcp.zalan.do.",
	})

	createZone(t, provider, &dns.ManagedZone{
		Name:    "zone-3-ext-dns-test-2-gcp-zalan-do",
		DnsName: "zone-3.ext-dns-test-2.gcp.zalan.do.",
	})

	// filtered out by domain filter
	createZone(t, provider, &dns.ManagedZone{
		Name:    "zone-4-ext-dns-test-3-gcp-zalan-do",
		DnsName: "zone-4.ext-dns-test-3.gcp.zalan.do.",
	})

	setupGoogleRecords(t, provider, records)

	provider.dryRun = dryRun

	return provider
}

func createZone(t *testing.T, provider *GoogleProvider, zone *dns.ManagedZone) {
	zone.Description = "Testing zone for kubernetes.io/external-dns"

	if _, err := provider.managedZonesClient.Create("zalando-external-dns-test", zone).Do(); err != nil {
		if err, ok := err.(*googleapi.Error); !ok || err.Code != http.StatusConflict {
			require.NoError(t, err)
		}
	}
}

func setupGoogleRecords(t *testing.T, provider *GoogleProvider, endpoints []*endpoint.Endpoint) {
	clearGoogleRecords(t, provider, "zone-1-ext-dns-test-2-gcp-zalan-do")
	clearGoogleRecords(t, provider, "zone-2-ext-dns-test-2-gcp-zalan-do")
	clearGoogleRecords(t, provider, "zone-3-ext-dns-test-2-gcp-zalan-do")

	ctx := context.Background()
	records, err := provider.Records(ctx)
	require.NoError(t, err)

	validateEndpoints(t, records, []*endpoint.Endpoint{})

	require.NoError(t, provider.CreateRecords(endpoints))

	records, err = provider.Records(ctx)
	require.NoError(t, err)

	validateEndpoints(t, records, endpoints)
}

func clearGoogleRecords(t *testing.T, provider *GoogleProvider, zone string) {
	recordSets := []*dns.ResourceRecordSet{}
	require.NoError(t, provider.resourceRecordSetsClient.List(provider.project, zone).Pages(context.Background(), func(resp *dns.ResourceRecordSetsListResponse) error {
		for _, r := range resp.Rrsets {
			switch r.Type {
			case endpoint.RecordTypeA, endpoint.RecordTypeCNAME:
				recordSets = append(recordSets, r)
			}
		}
		return nil
	}))

	if len(recordSets) != 0 {
		_, err := provider.changesClient.Create(provider.project, zone, &dns.Change{
			Deletions: recordSets,
		}).Do()
		require.NoError(t, err)
	}
}

func validateEndpoints(t *testing.T, endpoints []*endpoint.Endpoint, expected []*endpoint.Endpoint) {
	assert.True(t, testutils.SameEndpoints(endpoints, expected), "actual and expected endpoints don't match. %s:%s", endpoints, expected)
}
