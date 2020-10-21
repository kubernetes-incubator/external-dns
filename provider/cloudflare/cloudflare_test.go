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

package cloudflare

import (
	"context"
	"errors"
	"os"
	"testing"

	cloudflare "github.com/cloudflare/cloudflare-go"
	"github.com/stretchr/testify/assert"

	"github.com/maxatome/go-testdeep/td"
	"sigs.k8s.io/external-dns/endpoint"
	"sigs.k8s.io/external-dns/plan"
	"sigs.k8s.io/external-dns/provider"
)

type MockAction struct {
	Name       string
	ZoneId     string
	RecordId   string
	RecordData cloudflare.DNSRecord
}

type mockCloudFlareClient struct {
	User            cloudflare.User
	Zones           map[string]string
	Records         map[string]map[string]cloudflare.DNSRecord
	Actions         []MockAction
	listZonesError  error
	dnsRecordsError error
}

var ExampleDomain = []cloudflare.DNSRecord{
	{
		ID:      "1234567890",
		ZoneID:  "001",
		Name:    "foobar.bar.com",
		Type:    endpoint.RecordTypeA,
		TTL:     120,
		Content: "1.2.3.4",
		Proxied: false,
	},
	{
		ID:      "2345678901",
		ZoneID:  "001",
		Name:    "foobar.bar.com",
		Type:    endpoint.RecordTypeA,
		TTL:     120,
		Content: "3.4.5.6",
		Proxied: false,
	},
	{
		ID:      "1231231233",
		ZoneID:  "002",
		Name:    "bar.foo.com",
		Type:    endpoint.RecordTypeA,
		TTL:     1,
		Content: "2.3.4.5",
		Proxied: false,
	},
}

func NewMockCloudFlareClient() *mockCloudFlareClient {
	return &mockCloudFlareClient{
		User: cloudflare.User{ID: "xxxxxxxxxxxxxxxxxxx"},
		Zones: map[string]string{
			"001": "bar.com",
			"002": "foo.com",
		},
		Records: map[string]map[string]cloudflare.DNSRecord{
			"001": {},
			"002": {},
		},
	}
}

func NewMockCloudFlareClientWithRecords(records map[string][]cloudflare.DNSRecord) *mockCloudFlareClient {
	m := NewMockCloudFlareClient()

	for zoneID, zoneRecords := range records {
		if zone, ok := m.Records[zoneID]; ok {
			for _, record := range zoneRecords {
				zone[record.ID] = record
			}
		}
	}

	return m
}

func (m *mockCloudFlareClient) CreateDNSRecord(zoneID string, rr cloudflare.DNSRecord) (*cloudflare.DNSRecordResponse, error) {
	m.Actions = append(m.Actions, MockAction{
		Name:       "Create",
		ZoneId:     zoneID,
		RecordId:   rr.ID,
		RecordData: rr,
	})
	if zone, ok := m.Records[zoneID]; ok {
		zone[rr.ID] = rr
	}
	return nil, nil
}

func (m *mockCloudFlareClient) DNSRecords(zoneID string, rr cloudflare.DNSRecord) ([]cloudflare.DNSRecord, error) {
	if m.dnsRecordsError != nil {
		return nil, m.dnsRecordsError
	}
	result := []cloudflare.DNSRecord{}
	if zone, ok := m.Records[zoneID]; ok {
		for _, record := range zone {
			result = append(result, record)
		}
		return result, nil
	}
	return result, nil
}

func (m *mockCloudFlareClient) UpdateDNSRecord(zoneID, recordID string, rr cloudflare.DNSRecord) error {
	m.Actions = append(m.Actions, MockAction{
		Name:       "Update",
		ZoneId:     zoneID,
		RecordId:   recordID,
		RecordData: rr,
	})
	if zone, ok := m.Records[zoneID]; ok {
		if _, ok := zone[recordID]; ok {
			zone[recordID] = rr
		}
	}
	return nil
}

func (m *mockCloudFlareClient) DeleteDNSRecord(zoneID, recordID string) error {
	m.Actions = append(m.Actions, MockAction{
		Name:     "Delete",
		ZoneId:   zoneID,
		RecordId: recordID,
	})
	if zone, ok := m.Records[zoneID]; ok {
		if _, ok := zone[recordID]; ok {
			delete(zone, recordID)
			return nil
		}
	}
	return nil
}

func (m *mockCloudFlareClient) UserDetails() (cloudflare.User, error) {
	return m.User, nil
}

func (m *mockCloudFlareClient) ZoneIDByName(zoneName string) (string, error) {
	for id, name := range m.Zones {
		if name == zoneName {
			return id, nil
		}
	}

	return "", errors.New("Unknown zone: " + zoneName)
}

func (m *mockCloudFlareClient) ListZones(zoneID ...string) ([]cloudflare.Zone, error) {
	if m.listZonesError != nil {
		return nil, m.listZonesError
	}

	result := []cloudflare.Zone{}

	for zoneID, zoneName := range m.Zones {
		result = append(result, cloudflare.Zone{
			ID:   zoneID,
			Name: zoneName,
		})
	}

	return result, nil
}

func (m *mockCloudFlareClient) ListZonesContext(ctx context.Context, opts ...cloudflare.ReqOption) (cloudflare.ZonesResponse, error) {
	if m.listZonesError != nil {
		return cloudflare.ZonesResponse{}, m.listZonesError
	}

	result := []cloudflare.Zone{}

	for zoneId, zoneName := range m.Zones {
		result = append(result, cloudflare.Zone{
			ID:   zoneId,
			Name: zoneName,
		})
	}

	return cloudflare.ZonesResponse{
		Result: result,
		ResultInfo: cloudflare.ResultInfo{
			Page:       1,
			TotalPages: 1,
		},
	}, nil
}

func (m *mockCloudFlareClient) ZoneDetails(zoneID string) (cloudflare.Zone, error) {
	for id, zoneName := range m.Zones {
		if zoneID == id {
			return cloudflare.Zone{
				ID:   zoneID,
				Name: zoneName,
			}, nil
		}
	}

	return cloudflare.Zone{}, errors.New("Unknown zoneID: " + zoneID)
}

func AssertActions(t *testing.T, provider *CloudFlareProvider, endpoints []*endpoint.Endpoint, actions []MockAction, args ...interface{}) {
	t.Helper()

	var client *mockCloudFlareClient

	if provider.Client == nil {
		client = NewMockCloudFlareClient()
		provider.Client = client
	} else {
		client = provider.Client.(*mockCloudFlareClient)
	}

	ctx := context.Background()

	records, err := provider.Records(ctx)

	if err != nil {
		t.Fatalf("cannot fetch records, %s", err)
	}

	plan := &plan.Plan{
		Current:      records,
		Desired:      endpoints,
		DomainFilter: endpoint.NewDomainFilter([]string{"bar.com"}),
	}

	changes := plan.Calculate().Changes

	// Records other than A and CNAME are not supported by planner, just create them
	for _, endpoint := range endpoints {
		if endpoint.RecordType != "A" && endpoint.RecordType != "CNAME" && endpoint.RecordType != "SRV" {
			changes.Create = append(changes.Create, endpoint)
		}
	}

	err = provider.ApplyChanges(context.Background(), changes)

	if err != nil {
		t.Fatalf("cannot apply changes, %s", err)
	}

	td.Cmp(t, client.Actions, actions, args...)
}

func TestCloudflareA(t *testing.T) {
	endpoints := []*endpoint.Endpoint{
		{
			RecordType: "A",
			DNSName:    "bar.com",
			Targets:    endpoint.Targets{"127.0.0.1", "127.0.0.2"},
		},
	}

	AssertActions(t, &CloudFlareProvider{}, endpoints, []MockAction{
		{
			Name:   "Create",
			ZoneId: "001",
			RecordData: cloudflare.DNSRecord{
				Type:    "A",
				Name:    "bar.com",
				Content: "127.0.0.1",
				TTL:     1,
				Proxied: false,
			},
		},
		{
			Name:   "Create",
			ZoneId: "001",
			RecordData: cloudflare.DNSRecord{
				Type:    "A",
				Name:    "bar.com",
				Content: "127.0.0.2",
				TTL:     1,
				Proxied: false,
			},
		},
	})
}

func TestCloudflareCname(t *testing.T) {
	endpoints := []*endpoint.Endpoint{
		{
			RecordType: "CNAME",
			DNSName:    "cname.bar.com",
			Targets:    endpoint.Targets{"google.com", "facebook.com"},
		},
	}

	AssertActions(t, &CloudFlareProvider{}, endpoints, []MockAction{
		{
			Name:   "Create",
			ZoneId: "001",
			RecordData: cloudflare.DNSRecord{
				Type:    "CNAME",
				Name:    "cname.bar.com",
				Content: "google.com",
				TTL:     1,
				Proxied: false,
			},
		},
		{
			Name:   "Create",
			ZoneId: "001",
			RecordData: cloudflare.DNSRecord{
				Type:    "CNAME",
				Name:    "cname.bar.com",
				Content: "facebook.com",
				TTL:     1,
				Proxied: false,
			},
		},
	})
}

func TestCloudflareCustomTTL(t *testing.T) {
	endpoints := []*endpoint.Endpoint{
		{
			RecordType: "A",
			DNSName:    "ttl.bar.com",
			Targets:    endpoint.Targets{"127.0.0.1"},
			RecordTTL:  120,
		},
	}

	AssertActions(t, &CloudFlareProvider{}, endpoints, []MockAction{
		{
			Name:   "Create",
			ZoneId: "001",
			RecordData: cloudflare.DNSRecord{
				Type:    "A",
				Name:    "ttl.bar.com",
				Content: "127.0.0.1",
				TTL:     120,
				Proxied: false,
			},
		},
	})
}

func TestCloudflareProxiedDefault(t *testing.T) {
	endpoints := []*endpoint.Endpoint{
		{
			RecordType: "A",
			DNSName:    "bar.com",
			Targets:    endpoint.Targets{"127.0.0.1"},
		},
	}

	AssertActions(t, &CloudFlareProvider{proxiedByDefault: true}, endpoints, []MockAction{
		{
			Name:   "Create",
			ZoneId: "001",
			RecordData: cloudflare.DNSRecord{
				Type:    "A",
				Name:    "bar.com",
				Content: "127.0.0.1",
				TTL:     1,
				Proxied: true,
			},
		},
	})
}

func TestCloudflareProxiedOverrideTrue(t *testing.T) {
	endpoints := []*endpoint.Endpoint{
		{
			RecordType: "A",
			DNSName:    "bar.com",
			Targets:    endpoint.Targets{"127.0.0.1"},
			ProviderSpecific: endpoint.ProviderSpecific{
				endpoint.ProviderSpecificProperty{
					Name:  "external-dns.alpha.kubernetes.io/cloudflare-proxied",
					Value: "true",
				},
			},
		},
	}

	AssertActions(t, &CloudFlareProvider{}, endpoints, []MockAction{
		{
			Name:   "Create",
			ZoneId: "001",
			RecordData: cloudflare.DNSRecord{
				Type:    "A",
				Name:    "bar.com",
				Content: "127.0.0.1",
				TTL:     1,
				Proxied: true,
			},
		},
	})
}

func TestCloudflareProxiedOverrideFalse(t *testing.T) {
	endpoints := []*endpoint.Endpoint{
		{
			RecordType: "A",
			DNSName:    "bar.com",
			Targets:    endpoint.Targets{"127.0.0.1"},
			ProviderSpecific: endpoint.ProviderSpecific{
				endpoint.ProviderSpecificProperty{
					Name:  "external-dns.alpha.kubernetes.io/cloudflare-proxied",
					Value: "false",
				},
			},
		},
	}

	AssertActions(t, &CloudFlareProvider{proxiedByDefault: true}, endpoints, []MockAction{
		{
			Name:   "Create",
			ZoneId: "001",
			RecordData: cloudflare.DNSRecord{
				Type:    "A",
				Name:    "bar.com",
				Content: "127.0.0.1",
				TTL:     1,
				Proxied: false,
			},
		},
	})
}

func TestCloudflareProxiedOverrideIllegal(t *testing.T) {
	endpoints := []*endpoint.Endpoint{
		{
			RecordType: "A",
			DNSName:    "bar.com",
			Targets:    endpoint.Targets{"127.0.0.1"},
			ProviderSpecific: endpoint.ProviderSpecific{
				endpoint.ProviderSpecificProperty{
					Name:  "external-dns.alpha.kubernetes.io/cloudflare-proxied",
					Value: "asfasdfa",
				},
			},
		},
	}

	AssertActions(t, &CloudFlareProvider{proxiedByDefault: true}, endpoints, []MockAction{
		{
			Name:   "Create",
			ZoneId: "001",
			RecordData: cloudflare.DNSRecord{
				Type:    "A",
				Name:    "bar.com",
				Content: "127.0.0.1",
				TTL:     1,
				Proxied: true,
			},
		},
	})
}

func TestCloudflareSetProxied(t *testing.T) {
	var testCases = []struct {
		recordType string
		domain     string
		proxiable  bool
	}{
		{"A", "bar.com", true},
		{"CNAME", "bar.com", true},
		{"TXT", "bar.com", false},
		{"MX", "bar.com", false},
		{"NS", "bar.com", false},
		{"SPF", "bar.com", false},
		{"SRV", "bar.com", false},
		{"A", "*.bar.com", false},
	}

	for _, testCase := range testCases {
		endpoints := []*endpoint.Endpoint{
			{
				RecordType: testCase.recordType,
				DNSName:    testCase.domain,
				Targets:    endpoint.Targets{"127.0.0.1"},
				ProviderSpecific: endpoint.ProviderSpecific{
					endpoint.ProviderSpecificProperty{
						Name:  "external-dns.alpha.kubernetes.io/cloudflare-proxied",
						Value: "true",
					},
				},
			},
		}

		AssertActions(t, &CloudFlareProvider{}, endpoints, []MockAction{
			{
				Name:   "Create",
				ZoneId: "001",
				RecordData: cloudflare.DNSRecord{
					Type:    testCase.recordType,
					Name:    testCase.domain,
					Content: "127.0.0.1",
					TTL:     1,
					Proxied: testCase.proxiable,
				},
			},
		}, testCase.recordType+" record on "+testCase.domain)
	}
}

func TestCloudflareZones(t *testing.T) {
	provider := &CloudFlareProvider{
		Client:       NewMockCloudFlareClient(),
		domainFilter: endpoint.NewDomainFilter([]string{"bar.com"}),
		zoneIDFilter: provider.NewZoneIDFilter([]string{""}),
	}

	zones, err := provider.Zones(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	assert.Equal(t, 1, len(zones))
	assert.Equal(t, "bar.com", zones[0].Name)
}

func TestCloudFlareZonesWithIDFilter(t *testing.T) {
	client := NewMockCloudFlareClient()
	client.listZonesError = errors.New("shouldn't need to list zones when ZoneIDFilter in use")
	provider := &CloudFlareProvider{
		Client:       client,
		domainFilter: endpoint.NewDomainFilter([]string{"bar.com", "foo.com"}),
		zoneIDFilter: provider.NewZoneIDFilter([]string{"001"}),
	}

	zones, err := provider.Zones(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	// foo.com should *not* be returned as it doesn't match ZoneID filter
	assert.Equal(t, 1, len(zones))
	assert.Equal(t, "bar.com", zones[0].Name)
}

func TestCloudflareRecords(t *testing.T) {
	client := NewMockCloudFlareClientWithRecords(map[string][]cloudflare.DNSRecord{
		"001": ExampleDomain,
	})

	provider := &CloudFlareProvider{
		Client: client,
	}
	ctx := context.Background()

	records, err := provider.Records(ctx)
	if err != nil {
		t.Errorf("should not fail, %s", err)
	}

	assert.Equal(t, 2, len(records))
	client.dnsRecordsError = errors.New("failed to list dns records")
	_, err = provider.Records(ctx)
	if err == nil {
		t.Errorf("expected to fail")
	}
	client.dnsRecordsError = nil
	client.listZonesError = errors.New("failed to list zones")
	_, err = provider.Records(ctx)
	if err == nil {
		t.Errorf("expected to fail")
	}
}

func TestCloudflareProvider(t *testing.T) {
	_ = os.Setenv("CF_API_TOKEN", "abc123def")
	_, err := NewCloudFlareProvider(
		endpoint.NewDomainFilter([]string{"bar.com"}),
		provider.NewZoneIDFilter([]string{""}),
		25,
		false,
		true)
	if err != nil {
		t.Errorf("should not fail, %s", err)
	}
	_ = os.Unsetenv("CF_API_TOKEN")
	_ = os.Setenv("CF_API_KEY", "xxxxxxxxxxxxxxxxx")
	_ = os.Setenv("CF_API_EMAIL", "test@test.com")
	_, err = NewCloudFlareProvider(
		endpoint.NewDomainFilter([]string{"bar.com"}),
		provider.NewZoneIDFilter([]string{""}),
		1,
		false,
		true)
	if err != nil {
		t.Errorf("should not fail, %s", err)
	}
	_ = os.Unsetenv("CF_API_KEY")
	_ = os.Unsetenv("CF_API_EMAIL")
	_, err = NewCloudFlareProvider(
		endpoint.NewDomainFilter([]string{"bar.com"}),
		provider.NewZoneIDFilter([]string{""}),
		50,
		false,
		true)
	if err == nil {
		t.Errorf("expected to fail")
	}
}

func TestCloudflareApplyChanges(t *testing.T) {
	changes := &plan.Changes{}
	client := NewMockCloudFlareClient()
	provider := &CloudFlareProvider{
		Client: client,
	}
	changes.Create = []*endpoint.Endpoint{{
		DNSName: "new.bar.com",
		Targets: endpoint.Targets{"target"},
	}, {
		DNSName: "new.ext-dns-test.unrelated.to",
		Targets: endpoint.Targets{"target"},
	}}
	changes.Delete = []*endpoint.Endpoint{{
		DNSName: "foobar.bar.com",
		Targets: endpoint.Targets{"target"},
	}}
	changes.UpdateOld = []*endpoint.Endpoint{{
		DNSName: "foobar.bar.com",
		Targets: endpoint.Targets{"target-old"},
	}}
	changes.UpdateNew = []*endpoint.Endpoint{{
		DNSName: "foobar.bar.com",
		Targets: endpoint.Targets{"target-new"},
	}}
	err := provider.ApplyChanges(context.Background(), changes)

	if err != nil {
		t.Errorf("should not fail, %s", err)
	}

	td.Cmp(t, client.Actions, []MockAction{
		{
			Name:   "Create",
			ZoneId: "001",
			RecordData: cloudflare.DNSRecord{
				Name:    "new.bar.com",
				Content: "target",
				TTL:     1,
			},
		},
		{
			Name:   "Create",
			ZoneId: "001",
			RecordData: cloudflare.DNSRecord{
				Name:    "foobar.bar.com",
				Content: "target-new",
				TTL:     1,
			},
		},
	})

	// empty changes
	changes.Create = []*endpoint.Endpoint{}
	changes.Delete = []*endpoint.Endpoint{}
	changes.UpdateOld = []*endpoint.Endpoint{}
	changes.UpdateNew = []*endpoint.Endpoint{}

	err = provider.ApplyChanges(context.Background(), changes)
	if err != nil {
		t.Errorf("should not fail, %s", err)
	}
}

func TestCloudflareGetRecordID(t *testing.T) {
	p := &CloudFlareProvider{}
	records := []cloudflare.DNSRecord{
		{
			Name:    "foo.com",
			Type:    endpoint.RecordTypeCNAME,
			Content: "foobar",
			ID:      "1",
		},
		{
			Name: "bar.de",
			Type: endpoint.RecordTypeA,
			ID:   "2",
		},
		{
			Name:    "bar.de",
			Type:    endpoint.RecordTypeA,
			Content: "1.2.3.4",
			ID:      "2",
		},
	}

	assert.Equal(t, "", p.getRecordID(records, cloudflare.DNSRecord{
		Name:    "foo.com",
		Type:    endpoint.RecordTypeA,
		Content: "foobar",
	}))

	assert.Equal(t, "", p.getRecordID(records, cloudflare.DNSRecord{
		Name:    "foo.com",
		Type:    endpoint.RecordTypeCNAME,
		Content: "fizfuz",
	}))

	assert.Equal(t, "1", p.getRecordID(records, cloudflare.DNSRecord{
		Name:    "foo.com",
		Type:    endpoint.RecordTypeCNAME,
		Content: "foobar",
	}))
	assert.Equal(t, "", p.getRecordID(records, cloudflare.DNSRecord{
		Name:    "bar.de",
		Type:    endpoint.RecordTypeA,
		Content: "2.3.4.5",
	}))
	assert.Equal(t, "2", p.getRecordID(records, cloudflare.DNSRecord{
		Name:    "bar.de",
		Type:    endpoint.RecordTypeA,
		Content: "1.2.3.4",
	}))
}

func TestCloudflareGroupByNameAndType(t *testing.T) {
	testCases := []struct {
		Name              string
		Records           []cloudflare.DNSRecord
		ExpectedEndpoints []*endpoint.Endpoint
	}{
		{
			Name:              "empty",
			Records:           []cloudflare.DNSRecord{},
			ExpectedEndpoints: []*endpoint.Endpoint{},
		},
		{
			Name: "single record - single target",
			Records: []cloudflare.DNSRecord{
				{
					Name:    "foo.com",
					Type:    endpoint.RecordTypeA,
					Content: "10.10.10.1",
					TTL:     defaultCloudFlareRecordTTL,
				},
			},
			ExpectedEndpoints: []*endpoint.Endpoint{
				{
					DNSName:    "foo.com",
					Targets:    endpoint.Targets{"10.10.10.1"},
					RecordType: endpoint.RecordTypeA,
					RecordTTL:  endpoint.TTL(defaultCloudFlareRecordTTL),
					Labels:     endpoint.Labels{},
					ProviderSpecific: endpoint.ProviderSpecific{
						{
							Name:  "external-dns.alpha.kubernetes.io/cloudflare-proxied",
							Value: "false",
						},
					},
				},
			},
		},
		{
			Name: "single record - multiple targets",
			Records: []cloudflare.DNSRecord{
				{
					Name:    "foo.com",
					Type:    endpoint.RecordTypeA,
					Content: "10.10.10.1",
					TTL:     defaultCloudFlareRecordTTL,
				},
				{
					Name:    "foo.com",
					Type:    endpoint.RecordTypeA,
					Content: "10.10.10.2",
					TTL:     defaultCloudFlareRecordTTL,
				},
			},
			ExpectedEndpoints: []*endpoint.Endpoint{
				{
					DNSName:    "foo.com",
					Targets:    endpoint.Targets{"10.10.10.1", "10.10.10.2"},
					RecordType: endpoint.RecordTypeA,
					RecordTTL:  endpoint.TTL(defaultCloudFlareRecordTTL),
					Labels:     endpoint.Labels{},
					ProviderSpecific: endpoint.ProviderSpecific{
						{
							Name:  "external-dns.alpha.kubernetes.io/cloudflare-proxied",
							Value: "false",
						},
					},
				},
			},
		},
		{
			Name: "multiple record - multiple targets",
			Records: []cloudflare.DNSRecord{
				{
					Name:    "foo.com",
					Type:    endpoint.RecordTypeA,
					Content: "10.10.10.1",
					TTL:     defaultCloudFlareRecordTTL,
				},
				{
					Name:    "foo.com",
					Type:    endpoint.RecordTypeA,
					Content: "10.10.10.2",
					TTL:     defaultCloudFlareRecordTTL,
				},
				{
					Name:    "bar.de",
					Type:    endpoint.RecordTypeA,
					Content: "10.10.10.1",
					TTL:     defaultCloudFlareRecordTTL,
				},
				{
					Name:    "bar.de",
					Type:    endpoint.RecordTypeA,
					Content: "10.10.10.2",
					TTL:     defaultCloudFlareRecordTTL,
				},
			},
			ExpectedEndpoints: []*endpoint.Endpoint{
				{
					DNSName:    "foo.com",
					Targets:    endpoint.Targets{"10.10.10.1", "10.10.10.2"},
					RecordType: endpoint.RecordTypeA,
					RecordTTL:  endpoint.TTL(defaultCloudFlareRecordTTL),
					Labels:     endpoint.Labels{},
					ProviderSpecific: endpoint.ProviderSpecific{
						{
							Name:  "external-dns.alpha.kubernetes.io/cloudflare-proxied",
							Value: "false",
						},
					},
				},
				{
					DNSName:    "bar.de",
					Targets:    endpoint.Targets{"10.10.10.1", "10.10.10.2"},
					RecordType: endpoint.RecordTypeA,
					RecordTTL:  endpoint.TTL(defaultCloudFlareRecordTTL),
					Labels:     endpoint.Labels{},
					ProviderSpecific: endpoint.ProviderSpecific{
						{
							Name:  "external-dns.alpha.kubernetes.io/cloudflare-proxied",
							Value: "false",
						},
					},
				},
			},
		},
		{
			Name: "multiple record - mixed single/multiple targets",
			Records: []cloudflare.DNSRecord{
				{
					Name:    "foo.com",
					Type:    endpoint.RecordTypeA,
					Content: "10.10.10.1",
					TTL:     defaultCloudFlareRecordTTL,
				},
				{
					Name:    "foo.com",
					Type:    endpoint.RecordTypeA,
					Content: "10.10.10.2",
					TTL:     defaultCloudFlareRecordTTL,
				},
				{
					Name:    "bar.de",
					Type:    endpoint.RecordTypeA,
					Content: "10.10.10.1",
					TTL:     defaultCloudFlareRecordTTL,
				},
			},
			ExpectedEndpoints: []*endpoint.Endpoint{
				{
					DNSName:    "foo.com",
					Targets:    endpoint.Targets{"10.10.10.1", "10.10.10.2"},
					RecordType: endpoint.RecordTypeA,
					RecordTTL:  endpoint.TTL(defaultCloudFlareRecordTTL),
					Labels:     endpoint.Labels{},
					ProviderSpecific: endpoint.ProviderSpecific{
						{
							Name:  "external-dns.alpha.kubernetes.io/cloudflare-proxied",
							Value: "false",
						},
					},
				},
				{
					DNSName:    "bar.de",
					Targets:    endpoint.Targets{"10.10.10.1"},
					RecordType: endpoint.RecordTypeA,
					RecordTTL:  endpoint.TTL(defaultCloudFlareRecordTTL),
					Labels:     endpoint.Labels{},
					ProviderSpecific: endpoint.ProviderSpecific{
						{
							Name:  "external-dns.alpha.kubernetes.io/cloudflare-proxied",
							Value: "false",
						},
					},
				},
			},
		},
		{
			Name: "unsupported record type",
			Records: []cloudflare.DNSRecord{
				{
					Name:    "foo.com",
					Type:    endpoint.RecordTypeA,
					Content: "10.10.10.1",
					TTL:     defaultCloudFlareRecordTTL,
				},
				{
					Name:    "foo.com",
					Type:    endpoint.RecordTypeA,
					Content: "10.10.10.2",
					TTL:     defaultCloudFlareRecordTTL,
				},
				{
					Name:    "bar.de",
					Type:    "NOT SUPPORTED",
					Content: "10.10.10.1",
					TTL:     defaultCloudFlareRecordTTL,
				},
			},
			ExpectedEndpoints: []*endpoint.Endpoint{
				{
					DNSName:    "foo.com",
					Targets:    endpoint.Targets{"10.10.10.1", "10.10.10.2"},
					RecordType: endpoint.RecordTypeA,
					RecordTTL:  endpoint.TTL(defaultCloudFlareRecordTTL),
					Labels:     endpoint.Labels{},
					ProviderSpecific: endpoint.ProviderSpecific{
						{
							Name:  "external-dns.alpha.kubernetes.io/cloudflare-proxied",
							Value: "false",
						},
					},
				},
			},
		},
	}

	for _, tc := range testCases {
		assert.ElementsMatch(t, groupByNameAndType(tc.Records), tc.ExpectedEndpoints)
	}
}

func TestProviderPropertiesIdempotency(t *testing.T) {
	testCases := []struct {
		ProviderProxiedByDefault bool
		RecordsAreProxied        bool
		ShouldBeUpdated          bool
	}{
		{
			ProviderProxiedByDefault: false,
			RecordsAreProxied:        false,
			ShouldBeUpdated:          false,
		},
		{
			ProviderProxiedByDefault: true,
			RecordsAreProxied:        true,
			ShouldBeUpdated:          false,
		},
		{
			ProviderProxiedByDefault: true,
			RecordsAreProxied:        false,
			ShouldBeUpdated:          true,
		},
		{
			ProviderProxiedByDefault: false,
			RecordsAreProxied:        true,
			ShouldBeUpdated:          true,
		},
	}

	for _, test := range testCases {
		client := NewMockCloudFlareClientWithRecords(map[string][]cloudflare.DNSRecord{
			"001": {
				{
					ID:      "1234567890",
					ZoneID:  "001",
					Name:    "foobar.bar.com",
					Type:    endpoint.RecordTypeA,
					TTL:     120,
					Content: "1.2.3.4",
					Proxied: test.RecordsAreProxied,
				},
			},
		})

		provider := &CloudFlareProvider{
			Client:           client,
			proxiedByDefault: test.ProviderProxiedByDefault,
		}
		ctx := context.Background()

		current, err := provider.Records(ctx)
		if err != nil {
			t.Errorf("should not fail, %s", err)
		}
		assert.Equal(t, 1, len(current))

		desired := []*endpoint.Endpoint{}
		for _, c := range current {
			// Copy all except ProviderSpecific fields
			desired = append(desired, &endpoint.Endpoint{
				DNSName:       c.DNSName,
				Targets:       c.Targets,
				RecordType:    c.RecordType,
				SetIdentifier: c.SetIdentifier,
				RecordTTL:     c.RecordTTL,
				Labels:        c.Labels,
			})
		}

		plan := plan.Plan{
			Current:            current,
			Desired:            desired,
			PropertyComparator: provider.PropertyValuesEqual,
		}

		plan = *plan.Calculate()
		assert.NotNil(t, plan.Changes, "should have plan")
		if plan.Changes == nil {
			return
		}
		assert.Equal(t, 0, len(plan.Changes.Create), "should not have creates")
		assert.Equal(t, 0, len(plan.Changes.Delete), "should not have deletes")

		if test.ShouldBeUpdated {
			assert.Equal(t, 1, len(plan.Changes.UpdateNew), "should not have new updates")
			assert.Equal(t, 1, len(plan.Changes.UpdateOld), "should not have old updates")
		} else {
			assert.Equal(t, 0, len(plan.Changes.UpdateNew), "should not have new updates")
			assert.Equal(t, 0, len(plan.Changes.UpdateOld), "should not have old updates")
		}
	}
}

func TestCloudflareComplexUpdate(t *testing.T) {
	client := NewMockCloudFlareClientWithRecords(map[string][]cloudflare.DNSRecord{
		"001": ExampleDomain,
	})

	provider := &CloudFlareProvider{
		Client: client,
	}
	ctx := context.Background()

	records, err := provider.Records(ctx)

	if err != nil {
		t.Errorf("should not fail, %s", err)
	}

	plan := &plan.Plan{
		Current: records,
		Desired: []*endpoint.Endpoint{
			{
				DNSName:    "foobar.bar.com",
				Targets:    endpoint.Targets{"1.2.3.4", "2.3.4.5"},
				RecordType: endpoint.RecordTypeA,
				RecordTTL:  endpoint.TTL(defaultCloudFlareRecordTTL),
				Labels:     endpoint.Labels{},
				ProviderSpecific: endpoint.ProviderSpecific{
					{
						Name:  "external-dns.alpha.kubernetes.io/cloudflare-proxied",
						Value: "true",
					},
				},
			},
		},
		DomainFilter: endpoint.NewDomainFilter([]string{"bar.com"}),
	}

	planned := plan.Calculate()

	err = provider.ApplyChanges(context.Background(), planned.Changes)

	if err != nil {
		t.Errorf("should not fail, %s", err)
	}

	td.CmpDeeply(t, client.Actions, []MockAction{
		MockAction{
			Name:   "Create",
			ZoneId: "001",
			RecordData: cloudflare.DNSRecord{
				Name:    "foobar.bar.com",
				Type:    "A",
				Content: "2.3.4.5",
				TTL:     1,
				Proxied: true,
			},
		},
		MockAction{
			Name:     "Update",
			ZoneId:   "001",
			RecordId: "1234567890",
			RecordData: cloudflare.DNSRecord{
				Name:    "foobar.bar.com",
				Type:    "A",
				Content: "1.2.3.4",
				TTL:     1,
				Proxied: true,
			},
		},
		MockAction{
			Name:     "Delete",
			ZoneId:   "001",
			RecordId: "2345678901",
		},
	})
}
