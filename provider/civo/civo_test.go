/*
Copyright 2020 The Kubernetes Authors.

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

package civo

import (
	"context"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/civo/civogo"
	"github.com/stretchr/testify/require"
	"gotest.tools/assert"
	"sigs.k8s.io/external-dns/endpoint"
	"sigs.k8s.io/external-dns/plan"
)

func TestNewCivoProvider(t *testing.T) {
	_ = os.Setenv("CIVO_TOKEN", "xxxxxxxxxxxxxxx")
	_, err := NewCivoProvider(endpoint.NewDomainFilter([]string{"test.civo.com"}), true)
	require.NoError(t, err)

	_ = os.Unsetenv("CIVO_TOKEN")
	_, err = NewCivoProvider(endpoint.NewDomainFilter([]string{"test.civo.com"}), true)
	require.Error(t, err)
}

func TestCivoProviderZones(t *testing.T) {
	client, server, _ := civogo.NewClientForTesting(map[string]string{
		"/v2/dns": `[
			{"id": "12345", "account_id": "1", "name": "example.com"},
			{"id": "12346", "account_id": "1", "name": "example.net"}
			]`,
	})
	defer server.Close()
	provider := &CivoProvider{
		Client: *client,
	}

	expected, err := client.ListDNSDomains()
	if err != nil {
		t.Fatal(err)
	}
	zones, err := provider.Zones(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(expected, zones) {
		t.Fatal(err)
	}
}

func TestCivoProviderRecords(t *testing.T) {
	client, server, _ := civogo.NewClientForTesting(map[string]string{
		"/v2/dns/12345/records": `[
			{"id": "1", "domain_id":"12345", "account_id": "1", "name": "www", "type": "A", "value": "10.0.0.0", "ttl": 600},
			{"id": "2", "account_id": "1", "domain_id":"12345", "name": "mail", "type": "A", "value": "10.0.0.1", "ttl": 600}
			]`,
		"/v2/dns": `[
			{"id": "12345", "account_id": "1", "name": "example.com"},
			{"id": "12346", "account_id": "1", "name": "example.net"}
			]`,
	})
	defer server.Close()
	provider := &CivoProvider{
		Client:       *client,
		domainFilter: endpoint.NewDomainFilter([]string{"example.com"}),
	}

	expected, _ := client.ListDNSRecords("12345")
	records, err := provider.Records(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	assert.Equal(t, strings.TrimSuffix(records[0].DNSName, ".example.com"), expected[0].Name)
	assert.Equal(t, records[0].RecordType, string(expected[0].Type))
	assert.Equal(t, int(records[0].RecordTTL), expected[0].TTL)

	assert.Equal(t, strings.TrimSuffix(records[1].DNSName, ".example.com"), expected[1].Name)
	assert.Equal(t, records[1].RecordType, string(expected[1].Type))
	assert.Equal(t, int(records[1].RecordTTL), expected[1].TTL)

}

func TestCivoProviderApplyChanges(t *testing.T) {
	changes := &plan.Changes{}
	client, server, _ := civogo.NewClientForTesting(map[string]string{
		"/v2/dns/12345/records": `[
			{"id": "1", "domain_id":"12345", "account_id": "1", "name": "www", "type": "A", "value": "10.0.0.0", "ttl": 600},
			{"id": "2", "account_id": "1", "domain_id":"12345", "name": "mail", "type": "A", "value": "10.0.0.1", "ttl": 600}
			]`,
		"/v2/dns": `[
			{"id": "12345", "account_id": "1", "name": "example.com"},
			{"id": "12346", "account_id": "1", "name": "example.net"}
			]`,
	})
	defer server.Close()
	provider := &CivoProvider{
		Client: *client,
	}

	changes.Create = []*endpoint.Endpoint{
		{DNSName: "test.com", Targets: endpoint.Targets{"target"}},
		{DNSName: "ttl.test.com", Targets: endpoint.Targets{"target"}, RecordTTL: 600},
	}
	changes.UpdateNew = []*endpoint.Endpoint{{DNSName: "test.test.com", Targets: endpoint.Targets{"target-new"}, RecordType: "A", RecordTTL: 600}}
	changes.Delete = []*endpoint.Endpoint{{DNSName: "test.test.com", Targets: endpoint.Targets{"target"}, RecordType: "A"}}
	err := provider.ApplyChanges(context.Background(), changes)
	if err != nil {
		t.Errorf("should not fail, %s", err)
	}
	require.NoError(t, err)
}

func TestCivoProviderFetchZones(t *testing.T) {
	client, server, _ := civogo.NewClientForTesting(map[string]string{
		"/v2/dns": `[
			{"id": "12345", "account_id": "1", "name": "example.com"},
			{"id": "12346", "account_id": "1", "name": "example.net"}
			]`,
	})
	defer server.Close()
	provider := &CivoProvider{
		Client: *client,
	}

	expected, err := client.ListDNSDomains()
	if err != nil {
		t.Errorf("should not fail, %s", err)
	}
	zones, err := provider.fetchZones(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	assert.DeepEqual(t, zones, expected)
}
func TestCivoProviderFetchZonesWithFilter(t *testing.T) {
	client, server, _ := civogo.NewClientForTesting(map[string]string{
		"/v2/dns": `[
			{"id": "12345", "account_id": "1", "name": "example.com"},
			{"id": "12346", "account_id": "1", "name": "example.net"}
			]`,
	})
	defer server.Close()
	provider := &CivoProvider{
		Client:       *client,
		domainFilter: endpoint.NewDomainFilter([]string{".com"}),
	}

	expected := []civogo.DNSDomain{
		{ID: "12345", Name: "example.com", AccountID: "1"},
	}

	actual, err := provider.fetchZones(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	assert.DeepEqual(t, expected, actual)
}

func TestCivoProviderFetchRecords(t *testing.T) {
	client, server, _ := civogo.NewClientForTesting(map[string]string{
		"/v2/dns/12345/records": `[
			{"id": "1", "domain_id":"12345", "account_id": "1", "name": "www", "type": "A", "value": "10.0.0.0", "ttl": 600},
			{"id": "2", "account_id": "1", "domain_id":"12345", "name": "mail", "type": "A", "value": "10.0.0.1", "ttl": 600}
			]`,
	})
	defer server.Close()
	provider := &CivoProvider{
		Client: *client,
	}

	expected, err := client.ListDNSRecords("12345")
	if err != nil {
		t.Errorf("should not fail, %s", err)
	}
	actual, err := provider.fetchRecords(context.Background(), "12345")
	if err != nil {
		t.Fatal(err)
	}
	assert.DeepEqual(t, expected, actual)
}

func TestCivoGetStrippedRecordName(t *testing.T) {
	assert.Equal(t, "", getStrippedRecordName(civogo.DNSDomain{
		Name: "foo.com",
	}, endpoint.Endpoint{
		DNSName: "foo.com",
	}))

	assert.Equal(t, "api", getStrippedRecordName(civogo.DNSDomain{
		Name: "foo.com",
	}, endpoint.Endpoint{
		DNSName: "api.foo.com",
	}))
}

func TestCivoConvertRecordType(t *testing.T) {
	record, err := convertRecordType("A")
	recordA := civogo.DNSRecordType(civogo.DNSRecordTypeA)
	require.NoError(t, err)
	assert.Equal(t, recordA, record)

	record, err = convertRecordType("CNAME")
	recordCName := civogo.DNSRecordType(civogo.DNSRecordTypeCName)
	require.NoError(t, err)
	assert.Equal(t, recordCName, record)

	record, err = convertRecordType("TXT")
	recordTXT := civogo.DNSRecordType(civogo.DNSRecordTypeTXT)
	require.NoError(t, err)
	assert.Equal(t, recordTXT, record)

	record, err = convertRecordType("SRV")
	recordSRV := civogo.DNSRecordType(civogo.DNSRecordTypeSRV)
	require.NoError(t, err)
	assert.Equal(t, recordSRV, record)

	_, err = convertRecordType("INVALID")
	require.Error(t, err)
}

func TestCivoProviderGetRecordID(t *testing.T) {
	zone := civogo.DNSDomain{
		ID:   "12345",
		Name: "test.com",
	}

	record := []civogo.DNSRecord{{
		ID:          "1",
		Type:        "A",
		Name:        "www",
		Value:       "10.0.0.0",
		DNSDomainID: "12345",
		TTL:         600,
	}, {
		ID:          "2",
		Type:        "A",
		Name:        "api",
		Value:       "10.0.0.1",
		DNSDomainID: "12345",
		TTL:         600,
	}}

	endPoint := endpoint.Endpoint{DNSName: "www.test.com", Targets: endpoint.Targets{"10.0.0.0"}, RecordType: "A"}
	id := getRecordID(record, zone, endPoint)

	assert.Equal(t, id[0].ID, record[0].ID)
}
