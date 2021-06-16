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

package azure

import (
	"context"
	"strings"
	"testing"

	"github.com/Azure/azure-sdk-for-go/services/dns/mgmt/2018-05-01/dns"
	"github.com/Azure/go-autorest/autorest"
	"github.com/Azure/go-autorest/autorest/azure"
	"github.com/Azure/go-autorest/autorest/to"
	"github.com/stretchr/testify/assert"

	"sigs.k8s.io/external-dns/endpoint"
	"sigs.k8s.io/external-dns/internal/testutils"
	"sigs.k8s.io/external-dns/plan"
	"sigs.k8s.io/external-dns/provider"
)

// mockZonesClient implements the methods of the Azure DNS Zones Client which are used in the Azure Provider
// and returns static results which are defined per test
type mockZonesClient struct {
	mockZonesClientIterator *dns.ZoneListResultIterator
}

// mockZonesClient implements the methods of the Azure DNS RecordSet Client which are used in the Azure Provider
// and returns static results which are defined per test
type mockRecordSetsClient struct {
	mockRecordSetListIterator *dns.RecordSetListResultIterator
	deletedEndpoints          []*endpoint.Endpoint
	updatedEndpoints          []*endpoint.Endpoint
}

// mockZoneListResultPageIterator is used to paginate forward through a list of zones
type mockZoneListResultPageIterator struct {
	offset  int
	results []dns.ZoneListResult
}

// getNextPage provides the next page based on the offset of the mockZoneListResultPageIterator
func (m *mockZoneListResultPageIterator) getNextPage(context.Context, dns.ZoneListResult) (dns.ZoneListResult, error) {
	// it assumed that instances of this kind of iterator are only skimmed through once per test
	// otherwise a real implementation is required, e.g. based on a linked list
	if m.offset < len(m.results) {
		m.offset++
		return m.results[m.offset-1], nil
	}

	// paged to last page or empty
	return dns.ZoneListResult{}, nil
}

// mockZoneListResultPageIterator is used to paginate forward through a list of recordsets
type mockRecordSetListResultPageIterator struct {
	offset  int
	results []dns.RecordSetListResult
}

// getNextPage provides the next page based on the offset of the mockRecordSetListResultPageIterator
func (m *mockRecordSetListResultPageIterator) getNextPage(context.Context, dns.RecordSetListResult) (dns.RecordSetListResult, error) {
	// it assumed that instances of this kind of iterator are only skimmed through once per test
	// otherwise a real implementation is required, e.g. based on a linked list
	if m.offset < len(m.results) {
		m.offset++
		return m.results[m.offset-1], nil
	}

	// paged to last page or empty
	return dns.RecordSetListResult{}, nil
}

func createMockZone(zone string, id string) dns.Zone {
	return dns.Zone{
		ID:   to.StringPtr(id),
		Name: to.StringPtr(zone),
	}
}

func (client *mockZonesClient) ListByResourceGroupComplete(ctx context.Context, resourceGroupName string, top *int32) (result dns.ZoneListResultIterator, err error) {
	// pre-iterate to first item to emulate behaviour of Azure SDK
	err = client.mockZonesClientIterator.NextWithContext(ctx)
	if err != nil {
		return *client.mockZonesClientIterator, err
	}

	return *client.mockZonesClientIterator, nil
}

func aRecordSetPropertiesGetter(values []string, ttl int64) *dns.RecordSetProperties {
	aRecords := make([]dns.ARecord, len(values))
	for i, value := range values {
		aRecords[i] = dns.ARecord{
			Ipv4Address: to.StringPtr(value),
		}
	}
	return &dns.RecordSetProperties{
		TTL:      to.Int64Ptr(ttl),
		ARecords: &aRecords,
	}
}

func cNameRecordSetPropertiesGetter(values []string, ttl int64) *dns.RecordSetProperties {
	return &dns.RecordSetProperties{
		TTL: to.Int64Ptr(ttl),
		CnameRecord: &dns.CnameRecord{
			Cname: to.StringPtr(values[0]),
		},
	}
}

func txtRecordSetPropertiesGetter(values []string, ttl int64) *dns.RecordSetProperties {
	return &dns.RecordSetProperties{
		TTL: to.Int64Ptr(ttl),
		TxtRecords: &[]dns.TxtRecord{
			{
				Value: &[]string{values[0]},
			},
		},
	}
}

func othersRecordSetPropertiesGetter(values []string, ttl int64) *dns.RecordSetProperties {
	return &dns.RecordSetProperties{
		TTL: to.Int64Ptr(ttl),
	}
}
func createMockRecordSet(name, recordType string, values ...string) dns.RecordSet {
	return createMockRecordSetMultiWithTTL(name, recordType, 0, values...)
}
func createMockRecordSetWithTTL(name, recordType, value string, ttl int64) dns.RecordSet {
	return createMockRecordSetMultiWithTTL(name, recordType, ttl, value)
}
func createMockRecordSetMultiWithTTL(name, recordType string, ttl int64, values ...string) dns.RecordSet {
	var getterFunc func(values []string, ttl int64) *dns.RecordSetProperties

	switch recordType {
	case endpoint.RecordTypeA:
		getterFunc = aRecordSetPropertiesGetter
	case endpoint.RecordTypeCNAME:
		getterFunc = cNameRecordSetPropertiesGetter
	case endpoint.RecordTypeTXT:
		getterFunc = txtRecordSetPropertiesGetter
	default:
		getterFunc = othersRecordSetPropertiesGetter
	}
	return dns.RecordSet{
		Name:                to.StringPtr(name),
		Type:                to.StringPtr("Microsoft.Network/dnszones/" + recordType),
		RecordSetProperties: getterFunc(values, ttl),
	}

}

func (client *mockRecordSetsClient) ListAllByDNSZoneComplete(ctx context.Context, resourceGroupName string, zoneName string, top *int32, recordSetNameSuffix string) (result dns.RecordSetListResultIterator, err error) {
	// pre-iterate to first item to emulate behaviour of Azure SDK
	err = client.mockRecordSetListIterator.NextWithContext(ctx)
	if err != nil {
		return *client.mockRecordSetListIterator, err
	}

	return *client.mockRecordSetListIterator, nil
}

func (client *mockRecordSetsClient) Delete(ctx context.Context, resourceGroupName string, zoneName string, relativeRecordSetName string, recordType dns.RecordType, ifMatch string) (result autorest.Response, err error) {
	client.deletedEndpoints = append(
		client.deletedEndpoints,
		endpoint.NewEndpoint(
			formatAzureDNSName(relativeRecordSetName, zoneName),
			string(recordType),
			"",
		),
	)
	return autorest.Response{}, nil
}

func (client *mockRecordSetsClient) CreateOrUpdate(ctx context.Context, resourceGroupName string, zoneName string, relativeRecordSetName string, recordType dns.RecordType, parameters dns.RecordSet, ifMatch string, ifNoneMatch string) (result dns.RecordSet, err error) {
	var ttl endpoint.TTL
	if parameters.TTL != nil {
		ttl = endpoint.TTL(*parameters.TTL)
	}
	client.updatedEndpoints = append(
		client.updatedEndpoints,
		endpoint.NewEndpointWithTTL(
			formatAzureDNSName(relativeRecordSetName, zoneName),
			string(recordType),
			ttl,
			extractAzureTargets(&parameters)...,
		),
	)
	return parameters, nil
}

// newMockedAzureProvider creates an AzureProvider comprising the mocked clients for zones and recordsets
func newMockedAzureProvider(domainFilter endpoint.DomainFilter, zoneNameFilter provider.ZoneNameFilter, zoneIDFilter provider.ZoneIDFilter, dryRun bool, resourceGroup string, userAssignedIdentityClientID string, zones *[]dns.Zone, recordSets *[]dns.RecordSet) (*AzureProvider, error) {
	// init zone-related parts of the mock-client
	pageIterator := mockZoneListResultPageIterator{
		results: []dns.ZoneListResult{
			{
				Value: zones,
			},
		},
	}

	mockZoneListResultPage := dns.NewZoneListResultPage(pageIterator.getNextPage)
	mockZoneClientIterator := dns.NewZoneListResultIterator(mockZoneListResultPage)
	zonesClient := mockZonesClient{
		mockZonesClientIterator: &mockZoneClientIterator,
	}

	// init record-related parts of the mock-client
	resultPageIterator := mockRecordSetListResultPageIterator{
		results: []dns.RecordSetListResult{
			{
				Value: recordSets,
			},
		},
	}
	mockRecordSetListResultPage := dns.NewRecordSetListResultPage(resultPageIterator.getNextPage)
	mockRecordSetListIterator := dns.NewRecordSetListResultIterator(mockRecordSetListResultPage)
	recordSetsClient := mockRecordSetsClient{
		mockRecordSetListIterator: &mockRecordSetListIterator,
	}

	return newAzureProvider(domainFilter, zoneNameFilter, zoneIDFilter, dryRun, resourceGroup, userAssignedIdentityClientID, &zonesClient, &recordSetsClient), nil
}

func newAzureProvider(domainFilter endpoint.DomainFilter, zoneNameFilter provider.ZoneNameFilter, zoneIDFilter provider.ZoneIDFilter, dryRun bool, resourceGroup string, userAssignedIdentityClientID string, zonesClient ZonesClient, recordsClient RecordSetsClient) *AzureProvider {
	return &AzureProvider{
		domainFilter:                 domainFilter,
		zoneNameFilter:               zoneNameFilter,
		zoneIDFilter:                 zoneIDFilter,
		dryRun:                       dryRun,
		resourceGroup:                resourceGroup,
		userAssignedIdentityClientID: userAssignedIdentityClientID,
		zonesClient:                  zonesClient,
		recordSetsClient:             recordsClient,
	}
}

func validateAzureEndpoints(t *testing.T, endpoints []*endpoint.Endpoint, expected []*endpoint.Endpoint) {
	assert.True(t, testutils.SameEndpoints(endpoints, expected), "expected and actual endpoints don't match. %s:%s", endpoints, expected)
}

func TestAzureRecord(t *testing.T) {
	provider, err := newMockedAzureProvider(endpoint.NewDomainFilter([]string{"example.com"}), provider.NewZoneNameFilter([]string{}), provider.NewZoneIDFilter([]string{""}), true, "k8s", "",
		&[]dns.Zone{
			createMockZone("example.com", "/dnszones/example.com"),
		},
		&[]dns.RecordSet{
			createMockRecordSet("@", "NS", "ns1-03.azure-dns.com."),
			createMockRecordSet("@", "SOA", "Email: azuredns-hostmaster.microsoft.com"),
			createMockRecordSet("@", endpoint.RecordTypeA, "123.123.123.122"),
			createMockRecordSet("@", endpoint.RecordTypeTXT, "heritage=external-dns,external-dns/owner=default"),
			createMockRecordSetWithTTL("nginx", endpoint.RecordTypeA, "123.123.123.123", 3600),
			createMockRecordSetWithTTL("nginx", endpoint.RecordTypeTXT, "heritage=external-dns,external-dns/owner=default", recordTTL),
			createMockRecordSetWithTTL("hack", endpoint.RecordTypeCNAME, "hack.azurewebsites.net", 10),
		})

	if err != nil {
		t.Fatal(err)
	}

	ctx := context.Background()
	actual, err := provider.Records(ctx)

	if err != nil {
		t.Fatal(err)
	}
	expected := []*endpoint.Endpoint{
		endpoint.NewEndpoint("example.com", endpoint.RecordTypeA, "123.123.123.122"),
		endpoint.NewEndpoint("example.com", endpoint.RecordTypeTXT, "heritage=external-dns,external-dns/owner=default"),
		endpoint.NewEndpointWithTTL("nginx.example.com", endpoint.RecordTypeA, 3600, "123.123.123.123"),
		endpoint.NewEndpointWithTTL("nginx.example.com", endpoint.RecordTypeTXT, recordTTL, "heritage=external-dns,external-dns/owner=default"),
		endpoint.NewEndpointWithTTL("hack.example.com", endpoint.RecordTypeCNAME, 10, "hack.azurewebsites.net"),
	}

	validateAzureEndpoints(t, actual, expected)

}

func TestAzureMultiRecord(t *testing.T) {
	provider, err := newMockedAzureProvider(endpoint.NewDomainFilter([]string{"example.com"}), provider.NewZoneNameFilter([]string{}), provider.NewZoneIDFilter([]string{""}), true, "k8s", "",
		&[]dns.Zone{
			createMockZone("example.com", "/dnszones/example.com"),
		},
		&[]dns.RecordSet{
			createMockRecordSet("@", "NS", "ns1-03.azure-dns.com."),
			createMockRecordSet("@", "SOA", "Email: azuredns-hostmaster.microsoft.com"),
			createMockRecordSet("@", endpoint.RecordTypeA, "123.123.123.122", "234.234.234.233"),
			createMockRecordSet("@", endpoint.RecordTypeTXT, "heritage=external-dns,external-dns/owner=default"),
			createMockRecordSetMultiWithTTL("nginx", endpoint.RecordTypeA, 3600, "123.123.123.123", "234.234.234.234"),
			createMockRecordSetWithTTL("nginx", endpoint.RecordTypeTXT, "heritage=external-dns,external-dns/owner=default", recordTTL),
			createMockRecordSetWithTTL("hack", endpoint.RecordTypeCNAME, "hack.azurewebsites.net", 10),
		})

	if err != nil {
		t.Fatal(err)
	}

	ctx := context.Background()
	actual, err := provider.Records(ctx)

	if err != nil {
		t.Fatal(err)
	}
	expected := []*endpoint.Endpoint{
		endpoint.NewEndpoint("example.com", endpoint.RecordTypeA, "123.123.123.122", "234.234.234.233"),
		endpoint.NewEndpoint("example.com", endpoint.RecordTypeTXT, "heritage=external-dns,external-dns/owner=default"),
		endpoint.NewEndpointWithTTL("nginx.example.com", endpoint.RecordTypeA, 3600, "123.123.123.123", "234.234.234.234"),
		endpoint.NewEndpointWithTTL("nginx.example.com", endpoint.RecordTypeTXT, recordTTL, "heritage=external-dns,external-dns/owner=default"),
		endpoint.NewEndpointWithTTL("hack.example.com", endpoint.RecordTypeCNAME, 10, "hack.azurewebsites.net"),
	}

	validateAzureEndpoints(t, actual, expected)

}

func TestAzureApplyChanges(t *testing.T) {
	recordsClient := mockRecordSetsClient{}

	testAzureApplyChangesInternal(t, false, &recordsClient)

	validateAzureEndpoints(t, recordsClient.deletedEndpoints, []*endpoint.Endpoint{
		endpoint.NewEndpoint("old.example.com", endpoint.RecordTypeA, ""),
		endpoint.NewEndpoint("oldcname.example.com", endpoint.RecordTypeCNAME, ""),
		endpoint.NewEndpoint("deleted.example.com", endpoint.RecordTypeA, ""),
		endpoint.NewEndpoint("deletedcname.example.com", endpoint.RecordTypeCNAME, ""),
	})

	validateAzureEndpoints(t, recordsClient.updatedEndpoints, []*endpoint.Endpoint{
		endpoint.NewEndpointWithTTL("example.com", endpoint.RecordTypeA, endpoint.TTL(recordTTL), "1.2.3.4"),
		endpoint.NewEndpointWithTTL("example.com", endpoint.RecordTypeTXT, endpoint.TTL(recordTTL), "tag"),
		endpoint.NewEndpointWithTTL("foo.example.com", endpoint.RecordTypeA, endpoint.TTL(recordTTL), "1.2.3.4", "1.2.3.5"),
		endpoint.NewEndpointWithTTL("foo.example.com", endpoint.RecordTypeTXT, endpoint.TTL(recordTTL), "tag"),
		endpoint.NewEndpointWithTTL("bar.example.com", endpoint.RecordTypeCNAME, endpoint.TTL(recordTTL), "other.com"),
		endpoint.NewEndpointWithTTL("bar.example.com", endpoint.RecordTypeTXT, endpoint.TTL(recordTTL), "tag"),
		endpoint.NewEndpointWithTTL("other.com", endpoint.RecordTypeA, endpoint.TTL(recordTTL), "5.6.7.8"),
		endpoint.NewEndpointWithTTL("other.com", endpoint.RecordTypeTXT, endpoint.TTL(recordTTL), "tag"),
		endpoint.NewEndpointWithTTL("new.example.com", endpoint.RecordTypeA, 3600, "111.222.111.222"),
		endpoint.NewEndpointWithTTL("newcname.example.com", endpoint.RecordTypeCNAME, 10, "other.com"),
	})
}

func TestAzureApplyChangesDryRun(t *testing.T) {
	recordsClient := mockRecordSetsClient{}

	testAzureApplyChangesInternal(t, true, &recordsClient)

	validateAzureEndpoints(t, recordsClient.deletedEndpoints, []*endpoint.Endpoint{})

	validateAzureEndpoints(t, recordsClient.updatedEndpoints, []*endpoint.Endpoint{})
}

func testAzureApplyChangesInternal(t *testing.T, dryRun bool, client RecordSetsClient) {
	zlr := dns.ZoneListResult{
		Value: &[]dns.Zone{
			createMockZone("example.com", "/dnszones/example.com"),
			createMockZone("other.com", "/dnszones/other.com"),
		},
	}

	results := []dns.ZoneListResult{
		zlr,
	}

	mockZoneListResultPage := dns.NewZoneListResultPage(func(ctxParam context.Context, zlrParam dns.ZoneListResult) (dns.ZoneListResult, error) {
		if len(results) > 0 {
			result := results[0]
			results = nil
			return result, nil
		}
		return dns.ZoneListResult{}, nil
	})
	mockZoneClientIterator := dns.NewZoneListResultIterator(mockZoneListResultPage)

	zonesClient := mockZonesClient{
		mockZonesClientIterator: &mockZoneClientIterator,
	}

	provider := newAzureProvider(
		endpoint.NewDomainFilter([]string{""}),
		provider.NewZoneNameFilter([]string{""}),
		provider.NewZoneIDFilter([]string{""}),
		dryRun,
		"group",
		"",
		&zonesClient,
		client,
	)

	createRecords := []*endpoint.Endpoint{
		endpoint.NewEndpoint("example.com", endpoint.RecordTypeA, "1.2.3.4"),
		endpoint.NewEndpoint("example.com", endpoint.RecordTypeTXT, "tag"),
		endpoint.NewEndpoint("foo.example.com", endpoint.RecordTypeA, "1.2.3.5", "1.2.3.4"),
		endpoint.NewEndpoint("foo.example.com", endpoint.RecordTypeTXT, "tag"),
		endpoint.NewEndpoint("bar.example.com", endpoint.RecordTypeCNAME, "other.com"),
		endpoint.NewEndpoint("bar.example.com", endpoint.RecordTypeTXT, "tag"),
		endpoint.NewEndpoint("other.com", endpoint.RecordTypeA, "5.6.7.8"),
		endpoint.NewEndpoint("other.com", endpoint.RecordTypeTXT, "tag"),
		endpoint.NewEndpoint("nope.com", endpoint.RecordTypeA, "4.4.4.4"),
		endpoint.NewEndpoint("nope.com", endpoint.RecordTypeTXT, "tag"),
	}

	currentRecords := []*endpoint.Endpoint{
		endpoint.NewEndpoint("old.example.com", endpoint.RecordTypeA, "121.212.121.212"),
		endpoint.NewEndpoint("oldcname.example.com", endpoint.RecordTypeCNAME, "other.com"),
		endpoint.NewEndpoint("old.nope.com", endpoint.RecordTypeA, "121.212.121.212"),
	}
	updatedRecords := []*endpoint.Endpoint{
		endpoint.NewEndpointWithTTL("new.example.com", endpoint.RecordTypeA, 3600, "111.222.111.222"),
		endpoint.NewEndpointWithTTL("newcname.example.com", endpoint.RecordTypeCNAME, 10, "other.com"),
		endpoint.NewEndpoint("new.nope.com", endpoint.RecordTypeA, "222.111.222.111"),
	}

	deleteRecords := []*endpoint.Endpoint{
		endpoint.NewEndpoint("deleted.example.com", endpoint.RecordTypeA, "111.222.111.222"),
		endpoint.NewEndpoint("deletedcname.example.com", endpoint.RecordTypeCNAME, "other.com"),
		endpoint.NewEndpoint("deleted.nope.com", endpoint.RecordTypeA, "222.111.222.111"),
	}

	changes := &plan.Changes{
		Create:    createRecords,
		UpdateNew: updatedRecords,
		UpdateOld: currentRecords,
		Delete:    deleteRecords,
	}

	if err := provider.ApplyChanges(context.Background(), changes); err != nil {
		t.Fatal(err)
	}
}

func TestAzureGetAccessToken(t *testing.T) {
	env := azure.PublicCloud
	cfg := config{
		ClientID:                    "",
		ClientSecret:                "",
		TenantID:                    "",
		UseManagedIdentityExtension: false,
	}

	_, err := getAccessToken(cfg, env)
	if err == nil {
		t.Fatalf("expected to fail, but got no error")
	}

	// Expect to use managed identity in this case
	cfg = config{
		ClientID:                    "msi",
		ClientSecret:                "msi",
		TenantID:                    "cefe8aef-5127-4d65-a299-012053f81f60",
		UserAssignedIdentityID:      "userAssignedIdentityClientID",
		UseManagedIdentityExtension: true,
	}
	token, err := getAccessToken(cfg, env)
	if err != nil {
		t.Fatalf("expected to construct a token successfully, but got error %v", err)
	}
	_, err = token.MarshalJSON()
	if err == nil ||
		!strings.Contains(err.Error(), "marshalling ServicePrincipalMSISecret is not supported") {
		t.Fatalf("expected to fail to marshal token, but got %v", err)
	}

	// Expect to use SPN in this case
	cfg = config{
		ClientID:                    "SPNClientID",
		ClientSecret:                "SPNSecret",
		TenantID:                    "cefe8aef-5127-4d65-a299-012053f81f60",
		UserAssignedIdentityID:      "userAssignedIdentityClientID",
		UseManagedIdentityExtension: true,
	}
	token, err = getAccessToken(cfg, env)
	if err != nil {
		t.Fatalf("expected to construct a token successfully, but got error %v", err)
	}
	innerToken, err := token.MarshalJSON()
	if err != nil {
		t.Fatalf("expected to marshal token successfully, but got error %v", err)
	}
	if !strings.Contains(string(innerToken), "SPNClientID") {
		t.Fatalf("expect the clientID of the token is SPNClientID, but got token %s", string(innerToken))
	}
}

func TestAzureNameFilter(t *testing.T) {
	provider, err := newMockedAzureProvider(endpoint.NewDomainFilter([]string{"nginx.example.com"}), provider.NewZoneNameFilter([]string{"example.com"}), provider.NewZoneIDFilter([]string{""}), true, "k8s", "",
		&[]dns.Zone{
			createMockZone("example.com", "/dnszones/example.com"),
		},

		&[]dns.RecordSet{
			createMockRecordSet("@", "NS", "ns1-03.azure-dns.com."),
			createMockRecordSet("@", "SOA", "Email: azuredns-hostmaster.microsoft.com"),
			createMockRecordSet("@", endpoint.RecordTypeA, "123.123.123.122"),
			createMockRecordSet("@", endpoint.RecordTypeTXT, "heritage=external-dns,external-dns/owner=default"),
			createMockRecordSetWithTTL("test.nginx", endpoint.RecordTypeA, "123.123.123.123", 3600),
			createMockRecordSetWithTTL("nginx", endpoint.RecordTypeA, "123.123.123.123", 3600),
			createMockRecordSetWithTTL("nginx", endpoint.RecordTypeTXT, "heritage=external-dns,external-dns/owner=default", recordTTL),
			createMockRecordSetWithTTL("hack", endpoint.RecordTypeCNAME, "hack.azurewebsites.net", 10),
		})

	if err != nil {
		t.Fatal(err)
	}

	ctx := context.Background()
	actual, err := provider.Records(ctx)

	if err != nil {
		t.Fatal(err)
	}
	expected := []*endpoint.Endpoint{
		endpoint.NewEndpointWithTTL("test.nginx.example.com", endpoint.RecordTypeA, 3600, "123.123.123.123"),
		endpoint.NewEndpointWithTTL("nginx.example.com", endpoint.RecordTypeA, 3600, "123.123.123.123"),
		endpoint.NewEndpointWithTTL("nginx.example.com", endpoint.RecordTypeTXT, recordTTL, "heritage=external-dns,external-dns/owner=default"),
	}

	validateAzureEndpoints(t, actual, expected)

}

func TestAzureApplyChangesZoneName(t *testing.T) {
	recordsClient := mockRecordSetsClient{}

	testAzureApplyChangesInternalZoneName(t, false, &recordsClient)

	validateAzureEndpoints(t, recordsClient.deletedEndpoints, []*endpoint.Endpoint{
		endpoint.NewEndpoint("old.foo.example.com", endpoint.RecordTypeA, ""),
		endpoint.NewEndpoint("oldcname.foo.example.com", endpoint.RecordTypeCNAME, ""),
		endpoint.NewEndpoint("deleted.foo.example.com", endpoint.RecordTypeA, ""),
		endpoint.NewEndpoint("deletedcname.foo.example.com", endpoint.RecordTypeCNAME, ""),
	})

	validateAzureEndpoints(t, recordsClient.updatedEndpoints, []*endpoint.Endpoint{
		endpoint.NewEndpointWithTTL("foo.example.com", endpoint.RecordTypeA, endpoint.TTL(recordTTL), "1.2.3.4", "1.2.3.5"),
		endpoint.NewEndpointWithTTL("foo.example.com", endpoint.RecordTypeTXT, endpoint.TTL(recordTTL), "tag"),
		endpoint.NewEndpointWithTTL("new.foo.example.com", endpoint.RecordTypeA, 3600, "111.222.111.222"),
		endpoint.NewEndpointWithTTL("newcname.foo.example.com", endpoint.RecordTypeCNAME, 10, "other.com"),
	})
}

func testAzureApplyChangesInternalZoneName(t *testing.T, dryRun bool, client RecordSetsClient) {
	zlr := dns.ZoneListResult{
		Value: &[]dns.Zone{
			createMockZone("example.com", "/dnszones/example.com"),
		},
	}

	results := []dns.ZoneListResult{
		zlr,
	}

	mockZoneListResultPage := dns.NewZoneListResultPage(func(ctxParam context.Context, zlrParam dns.ZoneListResult) (dns.ZoneListResult, error) {
		if len(results) > 0 {
			result := results[0]
			results = nil
			return result, nil
		}
		return dns.ZoneListResult{}, nil
	})
	mockZoneClientIterator := dns.NewZoneListResultIterator(mockZoneListResultPage)

	zonesClient := mockZonesClient{
		mockZonesClientIterator: &mockZoneClientIterator,
	}

	provider := newAzureProvider(
		endpoint.NewDomainFilter([]string{"foo.example.com"}),
		provider.NewZoneNameFilter([]string{"example.com"}),
		provider.NewZoneIDFilter([]string{""}),
		dryRun,
		"group",
		"",
		&zonesClient,
		client,
	)

	createRecords := []*endpoint.Endpoint{
		endpoint.NewEndpoint("example.com", endpoint.RecordTypeA, "1.2.3.4"),
		endpoint.NewEndpoint("example.com", endpoint.RecordTypeTXT, "tag"),
		endpoint.NewEndpoint("foo.example.com", endpoint.RecordTypeA, "1.2.3.5", "1.2.3.4"),
		endpoint.NewEndpoint("foo.example.com", endpoint.RecordTypeTXT, "tag"),
		endpoint.NewEndpoint("bar.example.com", endpoint.RecordTypeCNAME, "other.com"),
		endpoint.NewEndpoint("bar.example.com", endpoint.RecordTypeTXT, "tag"),
		endpoint.NewEndpoint("other.com", endpoint.RecordTypeA, "5.6.7.8"),
		endpoint.NewEndpoint("other.com", endpoint.RecordTypeTXT, "tag"),
		endpoint.NewEndpoint("nope.com", endpoint.RecordTypeA, "4.4.4.4"),
		endpoint.NewEndpoint("nope.com", endpoint.RecordTypeTXT, "tag"),
	}

	currentRecords := []*endpoint.Endpoint{
		endpoint.NewEndpoint("old.foo.example.com", endpoint.RecordTypeA, "121.212.121.212"),
		endpoint.NewEndpoint("oldcname.foo.example.com", endpoint.RecordTypeCNAME, "other.com"),
		endpoint.NewEndpoint("old.nope.example.com", endpoint.RecordTypeA, "121.212.121.212"),
	}
	updatedRecords := []*endpoint.Endpoint{
		endpoint.NewEndpointWithTTL("new.foo.example.com", endpoint.RecordTypeA, 3600, "111.222.111.222"),
		endpoint.NewEndpointWithTTL("newcname.foo.example.com", endpoint.RecordTypeCNAME, 10, "other.com"),
		endpoint.NewEndpoint("new.nope.example.com", endpoint.RecordTypeA, "222.111.222.111"),
	}

	deleteRecords := []*endpoint.Endpoint{
		endpoint.NewEndpoint("deleted.foo.example.com", endpoint.RecordTypeA, "111.222.111.222"),
		endpoint.NewEndpoint("deletedcname.foo.example.com", endpoint.RecordTypeCNAME, "other.com"),
		endpoint.NewEndpoint("deleted.nope.example.com", endpoint.RecordTypeA, "222.111.222.111"),
	}

	changes := &plan.Changes{
		Create:    createRecords,
		UpdateNew: updatedRecords,
		UpdateOld: currentRecords,
		Delete:    deleteRecords,
	}

	if err := provider.ApplyChanges(context.Background(), changes); err != nil {
		t.Fatal(err)
	}
}
