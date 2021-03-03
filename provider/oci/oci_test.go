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

package oci

import (
	"context"
	"sort"
	"strings"
	"testing"

	"github.com/oracle/oci-go-sdk/common"
	"github.com/oracle/oci-go-sdk/dns"
	"github.com/pkg/errors"
	"github.com/stretchr/testify/require"

	"sigs.k8s.io/external-dns/endpoint"
	"sigs.k8s.io/external-dns/plan"
	"sigs.k8s.io/external-dns/provider"
)

type mockOCIDNSClient struct{}

func (c *mockOCIDNSClient) ListZones(ctx context.Context, request dns.ListZonesRequest) (response dns.ListZonesResponse, err error) {
	if request.Page == nil || *request.Page == "0" {
		return dns.ListZonesResponse{
			Items: []dns.ZoneSummary{
				{
					Id:   common.String("ocid1.dns-zone.oc1..e1e042ef0bfbb5c251b9713fd7bf8959"),
					Name: common.String("foo.com"),
				},
			},
			OpcNextPage: common.String("1"),
		}, nil
	}
	return dns.ListZonesResponse{
		Items: []dns.ZoneSummary{
			{
				Id:   common.String("ocid1.dns-zone.oc1..502aeddba262b92fd13ed7874f6f1404"),
				Name: common.String("bar.com"),
			},
		},
	}, nil
}

func (c *mockOCIDNSClient) GetZoneRecords(ctx context.Context, request dns.GetZoneRecordsRequest) (response dns.GetZoneRecordsResponse, err error) {
	if request.ZoneNameOrId == nil {
		return
	}

	switch *request.ZoneNameOrId {
	case "ocid1.dns-zone.oc1..e1e042ef0bfbb5c251b9713fd7bf8959":
		if request.Page == nil || *request.Page == "0" {
			response.Items = []dns.Record{{
				Domain: common.String("foo.foo.com"),
				Rdata:  common.String("127.0.0.1"),
				Rtype:  common.String(endpoint.RecordTypeA),
				Ttl:    common.Int(ociRecordTTL),
			}, {
				Domain: common.String("foo.foo.com"),
				Rdata:  common.String("heritage=external-dns,external-dns/owner=default,external-dns/resource=service/default/my-svc"),
				Rtype:  common.String(endpoint.RecordTypeTXT),
				Ttl:    common.Int(ociRecordTTL),
			}}
			response.OpcNextPage = common.String("1")
		} else {
			response.Items = []dns.Record{{Domain: common.String("bar.foo.com"),
				Rdata: common.String("bar.com."),
				Rtype: common.String(endpoint.RecordTypeCNAME),
				Ttl:   common.Int(ociRecordTTL),
			}}
		}
	case "ocid1.dns-zone.oc1..502aeddba262b92fd13ed7874f6f1404":
		if request.Page == nil || *request.Page == "0" {
			response.Items = []dns.Record{{
				Domain: common.String("foo.bar.com"),
				Rdata:  common.String("127.0.0.1"),
				Rtype:  common.String(endpoint.RecordTypeA),
				Ttl:    common.Int(ociRecordTTL),
			}}
		}
	}

	return
}

func (c *mockOCIDNSClient) PatchZoneRecords(ctx context.Context, request dns.PatchZoneRecordsRequest) (response dns.PatchZoneRecordsResponse, err error) {
	return // Provider does not use the response so nothing to do here.
}

// newOCIProvider creates an OCI provider with API calls mocked out.
func newOCIProvider(client ociDNSClient, domainFilter endpoint.DomainFilter, zoneIDFilter provider.ZoneIDFilter, dryRun bool) *OCIProvider {
	return &OCIProvider{
		client: client,
		cfg: OCIConfig{
			CompartmentID: "ocid1.compartment.oc1..aaaaaaaaujjg4lf3v6uaqeml7xfk7stzvrxeweaeyolhh75exuoqxpqjb4qq",
		},
		domainFilter: domainFilter,
		zoneIDFilter: zoneIDFilter,
		dryRun:       dryRun,
	}
}

func validateOCIZones(t *testing.T, actual, expected map[string]dns.ZoneSummary) {
	require.Len(t, actual, len(expected))

	for k, a := range actual {
		e, ok := expected[k]
		require.True(t, ok, "unexpected zone %q (%q)", *a.Name, *a.Id)
		require.Equal(t, e, a)
	}
}

func TestNewOCIProvider(t *testing.T) {
	testCases := map[string]struct {
		config OCIConfig
		err    error
	}{
		"valid": {
			config: OCIConfig{
				Auth: OCIAuthConfig{
					TenancyID:   "ocid1.tenancy.oc1..aaaaaaaaxf3fuazosc6xng7l75rj6uist5jb6ken64t3qltimxnkymddqbma",
					UserID:      "ocid1.user.oc1..aaaaaaaahx2vpvm4of5nqq3t274ike7ygyk2aexvokk3gyv4eyumzqajcrvq",
					Region:      "us-ashburn-1",
					Fingerprint: "48:ba:d4:21:63:53:db:10:65:20:d4:09:ce:01:f5:97",
					PrivateKey: `-----BEGIN RSA PRIVATE KEY-----
MIIEowIBAAKCAQEAv2JspZyO14kqcO/X4iz3ZdcyAf1GQJqYsBb6wyrlU0PB9Fee
H23/HLtMSqeqo+2KQHmdV1OHFQ/S6tx7zcBaby/+2b+z3/gJO4PGxohe2812AJ/J
W8Fp/4EnwbaRqDhoLN7ms0/e566zE3z40kCSW0NAIzv/F+0nNaka1xrypBqzvaNm
N49dAGvqWRpzFFUb8CbvKmgE6c/H4a2zVNW3G7/K6Og4HQGeEP3NKSVvi0BiQlvd
tVJTg7084kKcrngsS2N3qI3pzsr5wgpzPPefuPHWRKokZ20kpu8tXdFt+mAC2NHh
eWbtY3jsR6JFaXCyZLMXInwDvRgdP0T5+uh8WwIDAQABAoIBAG0rr94omDLKw7L4
naUfEWC+iIAqAdEIXuDTuudpqLb+h7zh3gj/re6tyK8tRWGNNrfgp6gQtZWGGUJv
0w9jEjMqpa2AdRLlYh7Y5KKLV9D6Or3QaAQ3KEffXNZbVmsnAgXWgLL4dKakOPJ8
71LAEryMeCGhL7puRVeOxwi9Dnwc4pcloimdggw/uwVHMK9eY5ylyt5ziiiWfhAo
cnNJNPHRSTqSiCoEhk/8BLZT5gxf1YX0hVSEdQh2WNyxmPmVSC9uuzKOqcEBfHf5
hmLnsUET1REM9IxCLqC9ebW263lIO/KdGiCu+YgIdwIi3wrLhaKXAZQmp4oMvWlE
n5eYlcECgYEA5AhctPWCQBCJhcD39pSWgnSq1O9bt8yQi2P2stqlxKV9ZBepCK49
OT42OYPUgWn7/y//6/LLzsPY58VTDHF3xZN1qu+fU0IM22D3Jqc19pnfVEb6TXSc
0jJIiaYCWTdqRQ4p2DuDcI+EzRB+V1Z7tFWxshZWXwNvtMXNoYPOYaUCgYEA1ttn
R3pCuGYJ5XbBwPzD5J+hvdZ6TQf8oTDraUBPxjtFOr7ea42T6KeYRFvnK2AQDnKL
Mw3I55lNO4I2W9gahUFG28dhxEuxeyvXGqXEJvPCUYePstab/BkUrm7/jkS3CLcJ
dlRXjqOfGwi5+NPUZMoOkZ54ZR4ZpdhIAeEpBf8CgYEAyMyMRlVCowNs9jkcoSfq
+Wme3O8BhvI9/mDCZnCfNHC94Bvtn1U/WF7uBOuPf35Ch05PQAiHa8WOBVn/bZ+l
ZngZT7K+S+SHyc6zFHh9zm9k96Og2f/r8DSTJ5Ll0oY3sCNuuZh+f+oBeUoi1umy
+PPVDAsbd4NhJIBiOO4GGHkCgYA1p4i9Es0Cm4ixItzzwqtwtmR/scXM4se1wS+o
kwTY7gg1yWBl328mVGPz/jdWX6Di2rvkPfcDzwa4a6YDfY3x5QE69Sl3CagCqEoJ
P4giahEGpyG9eVZuuBywCswKzSIgLQVR5XIQDtA2whEfEFcj7EmDF93c8o1ZGw+w
WHgUJQKBgEXr0HgxGG+v8bsXdrJ87Avx/nuA2rrFfECDPa4zuPkEK+cSFibdAq/H
u6OIV+z59AD2s84gxR+KLzEDfQAqBt7cVA5ZH6hrO+bkCtK9ycLL+koOuB+1EV+Y
hKRtDhmSdWBo3tJK12RrAe4t7CUe8gMgTvU7ExlcA3xQkseFPx9K
-----END RSA PRIVATE KEY-----
`,
				},
			},
		},
		"instance-principal": {
			// testing the InstancePrincipalConfigurationProvider is tricky outside of an OCI context, because it tries
			// to request a token from the internal OCI systems; this test-case just confirms that the expected error is
			// observed, confirming that the instance-principal provider was instantiated.
			config: OCIConfig{
				Auth: OCIAuthConfig{
					UseInstancePrincipal: true,
				},
			},
			err: errors.New("error creating OCI instance principal config provider: failed to create a new key provider for instance principal"),
		},
		"invalid": {
			config: OCIConfig{
				Auth: OCIAuthConfig{
					TenancyID:   "ocid1.tenancy.oc1..aaaaaaaaxf3fuazosc6xng7l75rj6uist5jb6ken64t3qltimxnkymddqbma",
					UserID:      "ocid1.user.oc1..aaaaaaaahx2vpvm4of5nqq3t274ike7ygyk2aexvokk3gyv4eyumzqajcrvq",
					Region:      "us-ashburn-1",
					Fingerprint: "48:ba:d4:21:63:53:db:10:65:20:d4:09:ce:01:f5:97",
					PrivateKey: `-----BEGIN RSA PRIVATE KEY-----
`,
				},
			},
			err: errors.New("initializing OCI DNS API client: can not create client, bad configuration: PEM data was not found in buffer"),
		},
	}
	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			_, err := NewOCIProvider(
				tc.config,
				endpoint.NewDomainFilter([]string{"com"}),
				provider.NewZoneIDFilter([]string{""}),
				false,
			)
			if err == nil {
				require.NoError(t, err)
			} else {
				// have to use prefix testing because the expected instance-principal error strings vary after a known prefix
				require.Truef(t, strings.HasPrefix(err.Error(), tc.err.Error()), "observed: %s", err.Error())
			}
		})
	}
}

func TestOCIZones(t *testing.T) {
	testCases := []struct {
		name         string
		domainFilter endpoint.DomainFilter
		zoneIDFilter provider.ZoneIDFilter
		expected     map[string]dns.ZoneSummary
	}{
		{
			name:         "DomainFilter_com",
			domainFilter: endpoint.NewDomainFilter([]string{"com"}),
			zoneIDFilter: provider.NewZoneIDFilter([]string{""}),
			expected: map[string]dns.ZoneSummary{
				"foo.com": {
					Id:   common.String("ocid1.dns-zone.oc1..e1e042ef0bfbb5c251b9713fd7bf8959"),
					Name: common.String("foo.com"),
				},
				"bar.com": {
					Id:   common.String("ocid1.dns-zone.oc1..502aeddba262b92fd13ed7874f6f1404"),
					Name: common.String("bar.com"),
				},
			},
		}, {
			name:         "DomainFilter_foo.com",
			domainFilter: endpoint.NewDomainFilter([]string{"foo.com"}),
			zoneIDFilter: provider.NewZoneIDFilter([]string{""}),
			expected: map[string]dns.ZoneSummary{
				"foo.com": {
					Id:   common.String("ocid1.dns-zone.oc1..e1e042ef0bfbb5c251b9713fd7bf8959"),
					Name: common.String("foo.com"),
				},
			},
		}, {
			name:         "ZoneIDFilter_ocid1.dns-zone.oc1..e1e042ef0bfbb5c251b9713fd7bf8959",
			domainFilter: endpoint.NewDomainFilter([]string{""}),
			zoneIDFilter: provider.NewZoneIDFilter([]string{"ocid1.dns-zone.oc1..e1e042ef0bfbb5c251b9713fd7bf8959"}),
			expected: map[string]dns.ZoneSummary{
				"foo.com": {
					Id:   common.String("ocid1.dns-zone.oc1..e1e042ef0bfbb5c251b9713fd7bf8959"),
					Name: common.String("foo.com"),
				},
			},
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			provider := newOCIProvider(&mockOCIDNSClient{}, tc.domainFilter, tc.zoneIDFilter, false)
			zones, err := provider.zones(context.Background())
			require.NoError(t, err)
			validateOCIZones(t, zones, tc.expected)
		})
	}
}

func TestOCIRecords(t *testing.T) {
	testCases := []struct {
		name         string
		domainFilter endpoint.DomainFilter
		zoneIDFilter provider.ZoneIDFilter
		expected     []*endpoint.Endpoint
	}{
		{
			name:         "unfiltered",
			domainFilter: endpoint.NewDomainFilter([]string{""}),
			zoneIDFilter: provider.NewZoneIDFilter([]string{""}),
			expected: []*endpoint.Endpoint{
				endpoint.NewEndpointWithTTL("foo.foo.com", endpoint.RecordTypeA, endpoint.TTL(ociRecordTTL), "127.0.0.1"),
				endpoint.NewEndpointWithTTL("foo.foo.com", endpoint.RecordTypeTXT, endpoint.TTL(ociRecordTTL), "heritage=external-dns,external-dns/owner=default,external-dns/resource=service/default/my-svc"),
				endpoint.NewEndpointWithTTL("bar.foo.com", endpoint.RecordTypeCNAME, endpoint.TTL(ociRecordTTL), "bar.com."),
				endpoint.NewEndpointWithTTL("foo.bar.com", endpoint.RecordTypeA, endpoint.TTL(ociRecordTTL), "127.0.0.1"),
			},
		}, {
			name:         "DomainFilter_foo.com",
			domainFilter: endpoint.NewDomainFilter([]string{"foo.com"}),
			zoneIDFilter: provider.NewZoneIDFilter([]string{""}),
			expected: []*endpoint.Endpoint{
				endpoint.NewEndpointWithTTL("foo.foo.com", endpoint.RecordTypeA, endpoint.TTL(ociRecordTTL), "127.0.0.1"),
				endpoint.NewEndpointWithTTL("foo.foo.com", endpoint.RecordTypeTXT, endpoint.TTL(ociRecordTTL), "heritage=external-dns,external-dns/owner=default,external-dns/resource=service/default/my-svc"),
				endpoint.NewEndpointWithTTL("bar.foo.com", endpoint.RecordTypeCNAME, endpoint.TTL(ociRecordTTL), "bar.com."),
			},
		}, {
			name:         "ZoneIDFilter_ocid1.dns-zone.oc1..502aeddba262b92fd13ed7874f6f1404",
			domainFilter: endpoint.NewDomainFilter([]string{""}),
			zoneIDFilter: provider.NewZoneIDFilter([]string{"ocid1.dns-zone.oc1..502aeddba262b92fd13ed7874f6f1404"}),
			expected: []*endpoint.Endpoint{
				endpoint.NewEndpointWithTTL("foo.bar.com", endpoint.RecordTypeA, endpoint.TTL(ociRecordTTL), "127.0.0.1"),
			},
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			provider := newOCIProvider(&mockOCIDNSClient{}, tc.domainFilter, tc.zoneIDFilter, false)
			endpoints, err := provider.Records(context.Background())
			require.NoError(t, err)
			require.ElementsMatch(t, tc.expected, endpoints)
		})
	}
}

func TestNewRecordOperation(t *testing.T) {
	testCases := []struct {
		name     string
		ep       *endpoint.Endpoint
		opType   dns.RecordOperationOperationEnum
		expected dns.RecordOperation
	}{
		{
			name:   "A_record",
			opType: dns.RecordOperationOperationAdd,
			ep: endpoint.NewEndpointWithTTL(
				"foo.foo.com",
				endpoint.RecordTypeA,
				endpoint.TTL(ociRecordTTL),
				"127.0.0.1"),
			expected: dns.RecordOperation{
				Domain:    common.String("foo.foo.com"),
				Rdata:     common.String("127.0.0.1"),
				Rtype:     common.String("A"),
				Ttl:       common.Int(300),
				Operation: dns.RecordOperationOperationAdd,
			},
		}, {
			name:   "TXT_record",
			opType: dns.RecordOperationOperationAdd,
			ep: endpoint.NewEndpointWithTTL(
				"foo.foo.com",
				endpoint.RecordTypeTXT,
				endpoint.TTL(ociRecordTTL),
				"heritage=external-dns,external-dns/owner=default,external-dns/resource=service/default/my-svc"),
			expected: dns.RecordOperation{
				Domain:    common.String("foo.foo.com"),
				Rdata:     common.String("heritage=external-dns,external-dns/owner=default,external-dns/resource=service/default/my-svc"),
				Rtype:     common.String("TXT"),
				Ttl:       common.Int(300),
				Operation: dns.RecordOperationOperationAdd,
			},
		}, {
			name:   "CNAME_record",
			opType: dns.RecordOperationOperationAdd,
			ep: endpoint.NewEndpointWithTTL(
				"foo.foo.com",
				endpoint.RecordTypeCNAME,
				endpoint.TTL(ociRecordTTL),
				"bar.com."),
			expected: dns.RecordOperation{
				Domain:    common.String("foo.foo.com"),
				Rdata:     common.String("bar.com."),
				Rtype:     common.String("CNAME"),
				Ttl:       common.Int(300),
				Operation: dns.RecordOperationOperationAdd,
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			op := newRecordOperation(tc.ep, tc.opType)
			require.Equal(t, tc.expected, op)
		})
	}
}

func TestOperationsByZone(t *testing.T) {
	testCases := []struct {
		name     string
		zones    map[string]dns.ZoneSummary
		ops      []dns.RecordOperation
		expected map[string][]dns.RecordOperation
	}{
		{
			name: "basic",
			zones: map[string]dns.ZoneSummary{
				"foo": {
					Id:   common.String("foo"),
					Name: common.String("foo.com"),
				},
				"bar": {
					Id:   common.String("bar"),
					Name: common.String("bar.com"),
				},
			},
			ops: []dns.RecordOperation{
				{
					Domain:    common.String("foo.foo.com"),
					Rdata:     common.String("127.0.0.1"),
					Rtype:     common.String("A"),
					Ttl:       common.Int(300),
					Operation: dns.RecordOperationOperationAdd,
				},
				{
					Domain:    common.String("foo.bar.com"),
					Rdata:     common.String("127.0.0.1"),
					Rtype:     common.String("A"),
					Ttl:       common.Int(300),
					Operation: dns.RecordOperationOperationAdd,
				},
			},
			expected: map[string][]dns.RecordOperation{
				"foo": {
					{
						Domain:    common.String("foo.foo.com"),
						Rdata:     common.String("127.0.0.1"),
						Rtype:     common.String("A"),
						Ttl:       common.Int(300),
						Operation: dns.RecordOperationOperationAdd,
					},
				},
				"bar": {
					{
						Domain:    common.String("foo.bar.com"),
						Rdata:     common.String("127.0.0.1"),
						Rtype:     common.String("A"),
						Ttl:       common.Int(300),
						Operation: dns.RecordOperationOperationAdd,
					},
				},
			},
		}, {
			name: "does_not_include_zones_with_no_changes",
			zones: map[string]dns.ZoneSummary{
				"foo": {
					Id:   common.String("foo"),
					Name: common.String("foo.com"),
				},
				"bar": {
					Id:   common.String("bar"),
					Name: common.String("bar.com"),
				},
			},
			ops: []dns.RecordOperation{
				{
					Domain:    common.String("foo.foo.com"),
					Rdata:     common.String("127.0.0.1"),
					Rtype:     common.String("A"),
					Ttl:       common.Int(300),
					Operation: dns.RecordOperationOperationAdd,
				},
			},
			expected: map[string][]dns.RecordOperation{
				"foo": {
					{
						Domain:    common.String("foo.foo.com"),
						Rdata:     common.String("127.0.0.1"),
						Rtype:     common.String("A"),
						Ttl:       common.Int(300),
						Operation: dns.RecordOperationOperationAdd,
					},
				},
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result := operationsByZone(tc.zones, tc.ops)
			require.Equal(t, tc.expected, result)
		})
	}
}

type mutableMockOCIDNSClient struct {
	zones   map[string]dns.ZoneSummary
	records map[string]map[string]dns.Record
}

func newMutableMockOCIDNSClient(zones []dns.ZoneSummary, recordsByZone map[string][]dns.Record) *mutableMockOCIDNSClient {
	c := &mutableMockOCIDNSClient{
		zones:   make(map[string]dns.ZoneSummary),
		records: make(map[string]map[string]dns.Record),
	}

	for _, zone := range zones {
		c.zones[*zone.Id] = zone
		c.records[*zone.Id] = make(map[string]dns.Record)
	}

	for zoneID, records := range recordsByZone {
		for _, record := range records {
			c.records[zoneID][ociRecordKey(*record.Rtype, *record.Domain)] = record
		}
	}

	return c
}

func (c *mutableMockOCIDNSClient) ListZones(ctx context.Context, request dns.ListZonesRequest) (response dns.ListZonesResponse, err error) {
	var zones []dns.ZoneSummary
	for _, v := range c.zones {
		zones = append(zones, v)
	}
	return dns.ListZonesResponse{Items: zones}, nil
}

func (c *mutableMockOCIDNSClient) GetZoneRecords(ctx context.Context, request dns.GetZoneRecordsRequest) (response dns.GetZoneRecordsResponse, err error) {
	if request.ZoneNameOrId == nil {
		err = errors.New("no name or id")
		return
	}

	records, ok := c.records[*request.ZoneNameOrId]
	if !ok {
		err = errors.New("zone not found")
		return
	}

	var items []dns.Record
	for _, v := range records {
		items = append(items, v)
	}

	response.Items = items
	return
}

func ociRecordKey(rType, domain string) string {
	return rType + "/" + domain
}

func (c *mutableMockOCIDNSClient) PatchZoneRecords(ctx context.Context, request dns.PatchZoneRecordsRequest) (response dns.PatchZoneRecordsResponse, err error) {
	if request.ZoneNameOrId == nil {
		err = errors.New("no name or id")
		return
	}

	records, ok := c.records[*request.ZoneNameOrId]
	if !ok {
		err = errors.New("zone not found")
		return
	}

	// Ensure that ADD operations occur after REMOVE.
	sort.Slice(request.Items, func(i, j int) bool {
		return request.Items[i].Operation > request.Items[j].Operation
	})

	for _, op := range request.Items {
		k := ociRecordKey(*op.Rtype, *op.Domain)
		switch op.Operation {
		case dns.RecordOperationOperationAdd:
			records[k] = dns.Record{
				Domain: op.Domain,
				Rtype:  op.Rtype,
				Rdata:  op.Rdata,
				Ttl:    op.Ttl,
			}
		case dns.RecordOperationOperationRemove:
			delete(records, k)
		default:
			err = errors.Errorf("unsupported operation %q", op.Operation)
			return
		}
	}
	return
}

// TestMutableMockOCIDNSClient exists because one must always test one's tests
// right...?
func TestMutableMockOCIDNSClient(t *testing.T) {
	zones := []dns.ZoneSummary{{
		Id:   common.String("ocid1.dns-zone.oc1..e1e042ef0bfbb5c251b9713fd7bf8959"),
		Name: common.String("foo.com"),
	}}
	records := map[string][]dns.Record{
		"ocid1.dns-zone.oc1..e1e042ef0bfbb5c251b9713fd7bf8959": {{
			Domain: common.String("foo.foo.com"),
			Rdata:  common.String("127.0.0.1"),
			Rtype:  common.String(endpoint.RecordTypeA),
			Ttl:    common.Int(ociRecordTTL),
		}, {
			Domain: common.String("foo.foo.com"),
			Rdata:  common.String("heritage=external-dns,external-dns/owner=default,external-dns/resource=service/default/my-svc"),
			Rtype:  common.String(endpoint.RecordTypeTXT),
			Ttl:    common.Int(ociRecordTTL),
		}},
	}
	client := newMutableMockOCIDNSClient(zones, records)

	// First ListZones.
	zonesResponse, err := client.ListZones(context.Background(), dns.ListZonesRequest{})
	require.NoError(t, err)
	require.Len(t, zonesResponse.Items, 1)
	require.Equal(t, zonesResponse.Items, zones)

	// GetZoneRecords for that zone.
	recordsResponse, err := client.GetZoneRecords(context.Background(), dns.GetZoneRecordsRequest{
		ZoneNameOrId: zones[0].Id,
	})
	require.NoError(t, err)
	require.Len(t, recordsResponse.Items, 2)
	require.ElementsMatch(t, recordsResponse.Items, records["ocid1.dns-zone.oc1..e1e042ef0bfbb5c251b9713fd7bf8959"])

	// Remove the A record.
	_, err = client.PatchZoneRecords(context.Background(), dns.PatchZoneRecordsRequest{
		ZoneNameOrId: common.String("ocid1.dns-zone.oc1..e1e042ef0bfbb5c251b9713fd7bf8959"),
		PatchZoneRecordsDetails: dns.PatchZoneRecordsDetails{
			Items: []dns.RecordOperation{{
				Domain:    common.String("foo.foo.com"),
				Rdata:     common.String("127.0.0.1"),
				Rtype:     common.String("A"),
				Ttl:       common.Int(300),
				Operation: dns.RecordOperationOperationRemove,
			}},
		},
	})
	require.NoError(t, err)

	// GetZoneRecords again and check the A record was removed.
	recordsResponse, err = client.GetZoneRecords(context.Background(), dns.GetZoneRecordsRequest{
		ZoneNameOrId: zones[0].Id,
	})
	require.NoError(t, err)
	require.Len(t, recordsResponse.Items, 1)
	require.Equal(t, recordsResponse.Items[0], records["ocid1.dns-zone.oc1..e1e042ef0bfbb5c251b9713fd7bf8959"][1])

	// Add the A record back.
	_, err = client.PatchZoneRecords(context.Background(), dns.PatchZoneRecordsRequest{
		ZoneNameOrId: common.String("ocid1.dns-zone.oc1..e1e042ef0bfbb5c251b9713fd7bf8959"),
		PatchZoneRecordsDetails: dns.PatchZoneRecordsDetails{
			Items: []dns.RecordOperation{{
				Domain:    common.String("foo.foo.com"),
				Rdata:     common.String("127.0.0.1"),
				Rtype:     common.String("A"),
				Ttl:       common.Int(300),
				Operation: dns.RecordOperationOperationAdd,
			}},
		},
	})
	require.NoError(t, err)

	// GetZoneRecords and check we're back in the original state
	recordsResponse, err = client.GetZoneRecords(context.Background(), dns.GetZoneRecordsRequest{
		ZoneNameOrId: zones[0].Id,
	})
	require.NoError(t, err)
	require.Len(t, recordsResponse.Items, 2)
	require.ElementsMatch(t, recordsResponse.Items, records["ocid1.dns-zone.oc1..e1e042ef0bfbb5c251b9713fd7bf8959"])
}

func TestOCIApplyChanges(t *testing.T) {
	testCases := []struct {
		name              string
		zones             []dns.ZoneSummary
		records           map[string][]dns.Record
		changes           *plan.Changes
		dryRun            bool
		err               error
		expectedEndpoints []*endpoint.Endpoint
	}{
		{
			name: "add",
			zones: []dns.ZoneSummary{{
				Id:   common.String("ocid1.dns-zone.oc1..e1e042ef0bfbb5c251b9713fd7bf8959"),
				Name: common.String("foo.com"),
			}},
			changes: &plan.Changes{
				Create: []*endpoint.Endpoint{endpoint.NewEndpointWithTTL(
					"foo.foo.com",
					endpoint.RecordTypeA,
					endpoint.TTL(ociRecordTTL),
					"127.0.0.1",
				)},
			},
			expectedEndpoints: []*endpoint.Endpoint{endpoint.NewEndpointWithTTL(
				"foo.foo.com",
				endpoint.RecordTypeA,
				endpoint.TTL(ociRecordTTL),
				"127.0.0.1",
			)},
		}, {
			name: "remove",
			zones: []dns.ZoneSummary{{
				Id:   common.String("ocid1.dns-zone.oc1..e1e042ef0bfbb5c251b9713fd7bf8959"),
				Name: common.String("foo.com"),
			}},
			records: map[string][]dns.Record{
				"ocid1.dns-zone.oc1..e1e042ef0bfbb5c251b9713fd7bf8959": {{
					Domain: common.String("foo.foo.com"),
					Rdata:  common.String("127.0.0.1"),
					Rtype:  common.String(endpoint.RecordTypeA),
					Ttl:    common.Int(ociRecordTTL),
				}, {
					Domain: common.String("foo.foo.com"),
					Rdata:  common.String("heritage=external-dns,external-dns/owner=default,external-dns/resource=service/default/my-svc"),
					Rtype:  common.String(endpoint.RecordTypeTXT),
					Ttl:    common.Int(ociRecordTTL),
				}},
			},
			changes: &plan.Changes{
				Delete: []*endpoint.Endpoint{endpoint.NewEndpointWithTTL(
					"foo.foo.com",
					endpoint.RecordTypeTXT,
					endpoint.TTL(ociRecordTTL),
					"127.0.0.1",
				)},
			},
			expectedEndpoints: []*endpoint.Endpoint{endpoint.NewEndpointWithTTL(
				"foo.foo.com",
				endpoint.RecordTypeA,
				endpoint.TTL(ociRecordTTL),
				"127.0.0.1",
			)},
		}, {
			name: "update",
			zones: []dns.ZoneSummary{{
				Id:   common.String("ocid1.dns-zone.oc1..e1e042ef0bfbb5c251b9713fd7bf8959"),
				Name: common.String("foo.com"),
			}},
			records: map[string][]dns.Record{
				"ocid1.dns-zone.oc1..e1e042ef0bfbb5c251b9713fd7bf8959": {{
					Domain: common.String("foo.foo.com"),
					Rdata:  common.String("127.0.0.1"),
					Rtype:  common.String(endpoint.RecordTypeA),
					Ttl:    common.Int(ociRecordTTL),
				}},
			},
			changes: &plan.Changes{
				UpdateOld: []*endpoint.Endpoint{endpoint.NewEndpointWithTTL(
					"foo.foo.com",
					endpoint.RecordTypeA,
					endpoint.TTL(ociRecordTTL),
					"127.0.0.1",
				)},
				UpdateNew: []*endpoint.Endpoint{endpoint.NewEndpointWithTTL(
					"foo.foo.com",
					endpoint.RecordTypeA,
					endpoint.TTL(ociRecordTTL),
					"10.0.0.1",
				)},
			},
			expectedEndpoints: []*endpoint.Endpoint{endpoint.NewEndpointWithTTL(
				"foo.foo.com",
				endpoint.RecordTypeA,
				endpoint.TTL(ociRecordTTL),
				"10.0.0.1",
			)},
		}, {
			name: "dry_run_no_changes",
			zones: []dns.ZoneSummary{{
				Id:   common.String("ocid1.dns-zone.oc1..e1e042ef0bfbb5c251b9713fd7bf8959"),
				Name: common.String("foo.com"),
			}},
			records: map[string][]dns.Record{
				"ocid1.dns-zone.oc1..e1e042ef0bfbb5c251b9713fd7bf8959": {{
					Domain: common.String("foo.foo.com"),
					Rdata:  common.String("127.0.0.1"),
					Rtype:  common.String(endpoint.RecordTypeA),
					Ttl:    common.Int(ociRecordTTL),
				}},
			},
			changes: &plan.Changes{
				Delete: []*endpoint.Endpoint{endpoint.NewEndpointWithTTL(
					"foo.foo.com",
					endpoint.RecordTypeA,
					endpoint.TTL(ociRecordTTL),
					"127.0.0.1",
				)},
			},
			dryRun: true,
			expectedEndpoints: []*endpoint.Endpoint{endpoint.NewEndpointWithTTL(
				"foo.foo.com",
				endpoint.RecordTypeA,
				endpoint.TTL(ociRecordTTL),
				"127.0.0.1",
			)},
		}, {
			name: "add_remove_update",
			zones: []dns.ZoneSummary{{
				Id:   common.String("ocid1.dns-zone.oc1..e1e042ef0bfbb5c251b9713fd7bf8959"),
				Name: common.String("foo.com"),
			}},
			records: map[string][]dns.Record{
				"ocid1.dns-zone.oc1..e1e042ef0bfbb5c251b9713fd7bf8959": {{
					Domain: common.String("foo.foo.com"),
					Rdata:  common.String("127.0.0.1"),
					Rtype:  common.String(endpoint.RecordTypeA),
					Ttl:    common.Int(ociRecordTTL),
				}, {
					Domain: common.String("bar.foo.com"),
					Rdata:  common.String("bar.com."),
					Rtype:  common.String(endpoint.RecordTypeCNAME),
					Ttl:    common.Int(ociRecordTTL),
				}},
			},
			changes: &plan.Changes{
				Delete: []*endpoint.Endpoint{endpoint.NewEndpointWithTTL(
					"foo.foo.com",
					endpoint.RecordTypeA,
					endpoint.TTL(ociRecordTTL),
					"baz.com.",
				)},
				UpdateOld: []*endpoint.Endpoint{endpoint.NewEndpointWithTTL(
					"bar.foo.com",
					endpoint.RecordTypeCNAME,
					endpoint.TTL(ociRecordTTL),
					"baz.com.",
				)},
				UpdateNew: []*endpoint.Endpoint{endpoint.NewEndpointWithTTL(
					"bar.foo.com",
					endpoint.RecordTypeCNAME,
					endpoint.TTL(ociRecordTTL),
					"foo.bar.com.",
				)},
				Create: []*endpoint.Endpoint{endpoint.NewEndpointWithTTL(
					"baz.foo.com",
					endpoint.RecordTypeA,
					endpoint.TTL(ociRecordTTL),
					"127.0.0.1",
				)},
			},
			expectedEndpoints: []*endpoint.Endpoint{
				endpoint.NewEndpointWithTTL(
					"bar.foo.com",
					endpoint.RecordTypeCNAME,
					endpoint.TTL(ociRecordTTL),
					"foo.bar.com.",
				),
				endpoint.NewEndpointWithTTL(
					"baz.foo.com",
					endpoint.RecordTypeA,
					endpoint.TTL(ociRecordTTL),
					"127.0.0.1"),
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			client := newMutableMockOCIDNSClient(tc.zones, tc.records)
			provider := newOCIProvider(
				client,
				endpoint.NewDomainFilter([]string{""}),
				provider.NewZoneIDFilter([]string{""}),
				tc.dryRun,
			)

			ctx := context.Background()
			err := provider.ApplyChanges(ctx, tc.changes)
			require.Equal(t, tc.err, err)
			endpoints, err := provider.Records(ctx)
			require.NoError(t, err)
			require.ElementsMatch(t, tc.expectedEndpoints, endpoints)
		})
	}
}
