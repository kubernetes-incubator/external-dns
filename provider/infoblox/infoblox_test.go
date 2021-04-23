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

package infoblox

import (
	"context"
	"encoding/base64"
	"fmt"
	"regexp"
	"strings"
	"testing"

	ibclient "github.com/infobloxopen/infoblox-go-client"
	"github.com/stretchr/testify/assert"

	"sigs.k8s.io/external-dns/endpoint"
	"sigs.k8s.io/external-dns/internal/testutils"
	"sigs.k8s.io/external-dns/plan"
	"sigs.k8s.io/external-dns/provider"
)

type mockIBConnector struct {
	mockInfobloxZones   *[]ibclient.ZoneAuth
	mockInfobloxObjects *[]ibclient.IBObject
	createdEndpoints    []*endpoint.Endpoint
	deletedEndpoints    []*endpoint.Endpoint
	updatedEndpoints    []*endpoint.Endpoint
}

func (client *mockIBConnector) CreateObject(obj ibclient.IBObject) (ref string, err error) {
	switch obj.ObjectType() {
	case "record:a":
		client.createdEndpoints = append(
			client.createdEndpoints,
			endpoint.NewEndpoint(
				obj.(*ibclient.RecordA).Name,
				endpoint.RecordTypeA,
				obj.(*ibclient.RecordA).Ipv4Addr,
			),
		)
		ref = fmt.Sprintf("%s/%s:%s/default", obj.ObjectType(), base64.StdEncoding.EncodeToString([]byte(obj.(*ibclient.RecordA).Name)), obj.(*ibclient.RecordA).Name)
		obj.(*ibclient.RecordA).Ref = ref
	case "record:cname":
		client.createdEndpoints = append(
			client.createdEndpoints,
			endpoint.NewEndpoint(
				obj.(*ibclient.RecordCNAME).Name,
				endpoint.RecordTypeCNAME,
				obj.(*ibclient.RecordCNAME).Canonical,
			),
		)
		ref = fmt.Sprintf("%s/%s:%s/default", obj.ObjectType(), base64.StdEncoding.EncodeToString([]byte(obj.(*ibclient.RecordCNAME).Name)), obj.(*ibclient.RecordCNAME).Name)
		obj.(*ibclient.RecordCNAME).Ref = ref
	case "record:host":
		for _, i := range obj.(*ibclient.HostRecord).Ipv4Addrs {
			client.createdEndpoints = append(
				client.createdEndpoints,
				endpoint.NewEndpoint(
					obj.(*ibclient.HostRecord).Name,
					endpoint.RecordTypeA,
					i.Ipv4Addr,
				),
			)
		}
		ref = fmt.Sprintf("%s/%s:%s/default", obj.ObjectType(), base64.StdEncoding.EncodeToString([]byte(obj.(*ibclient.HostRecord).Name)), obj.(*ibclient.HostRecord).Name)
		obj.(*ibclient.HostRecord).Ref = ref
	case "record:txt":
		client.createdEndpoints = append(
			client.createdEndpoints,
			endpoint.NewEndpoint(
				obj.(*ibclient.RecordTXT).Name,
				endpoint.RecordTypeTXT,
				obj.(*ibclient.RecordTXT).Text,
			),
		)
		obj.(*ibclient.RecordTXT).Ref = ref
		ref = fmt.Sprintf("%s/%s:%s/default", obj.ObjectType(), base64.StdEncoding.EncodeToString([]byte(obj.(*ibclient.RecordTXT).Name)), obj.(*ibclient.RecordTXT).Name)
	}
	*client.mockInfobloxObjects = append(
		*client.mockInfobloxObjects,
		obj,
	)
	return ref, nil
}

func (client *mockIBConnector) GetObject(obj ibclient.IBObject, ref string, res interface{}) (err error) {
	switch obj.ObjectType() {
	case "record:a":
		var result []ibclient.RecordA
		for _, object := range *client.mockInfobloxObjects {
			if object.ObjectType() == "record:a" {
				if ref != "" &&
					ref != object.(*ibclient.RecordA).Ref {
					continue
				}
				if obj.(*ibclient.RecordA).Name != "" &&
					obj.(*ibclient.RecordA).Name != object.(*ibclient.RecordA).Name {
					continue
				}
				result = append(result, *object.(*ibclient.RecordA))
			}
		}
		*res.(*[]ibclient.RecordA) = result
	case "record:cname":
		var result []ibclient.RecordCNAME
		for _, object := range *client.mockInfobloxObjects {
			if object.ObjectType() == "record:cname" {
				if ref != "" &&
					ref != object.(*ibclient.RecordCNAME).Ref {
					continue
				}
				if obj.(*ibclient.RecordCNAME).Name != "" &&
					obj.(*ibclient.RecordCNAME).Name != object.(*ibclient.RecordCNAME).Name {
					continue
				}
				result = append(result, *object.(*ibclient.RecordCNAME))
			}
		}
		*res.(*[]ibclient.RecordCNAME) = result
	case "record:host":
		var result []ibclient.HostRecord
		for _, object := range *client.mockInfobloxObjects {
			if object.ObjectType() == "record:host" {
				if ref != "" &&
					ref != object.(*ibclient.HostRecord).Ref {
					continue
				}
				if obj.(*ibclient.HostRecord).Name != "" &&
					obj.(*ibclient.HostRecord).Name != object.(*ibclient.HostRecord).Name {
					continue
				}
				result = append(result, *object.(*ibclient.HostRecord))
			}
		}
		*res.(*[]ibclient.HostRecord) = result
	case "record:txt":
		var result []ibclient.RecordTXT
		for _, object := range *client.mockInfobloxObjects {
			if object.ObjectType() == "record:txt" {
				if ref != "" &&
					ref != object.(*ibclient.RecordTXT).Ref {
					continue
				}
				if obj.(*ibclient.RecordTXT).Name != "" &&
					obj.(*ibclient.RecordTXT).Name != object.(*ibclient.RecordTXT).Name {
					continue
				}
				result = append(result, *object.(*ibclient.RecordTXT))
			}
		}
		*res.(*[]ibclient.RecordTXT) = result
	case "zone_auth":
		*res.(*[]ibclient.ZoneAuth) = *client.mockInfobloxZones
	}
	return
}

func (client *mockIBConnector) DeleteObject(ref string) (refRes string, err error) {
	re := regexp.MustCompile(`([^/]+)/[^:]+:([^/]+)/default`)
	result := re.FindStringSubmatch(ref)

	switch result[1] {
	case "record:a":
		var records []ibclient.RecordA
		obj := ibclient.NewRecordA(
			ibclient.RecordA{
				Name: result[2],
			},
		)
		client.GetObject(obj, ref, &records)
		for _, record := range records {
			client.deletedEndpoints = append(
				client.deletedEndpoints,
				endpoint.NewEndpoint(
					record.Name,
					endpoint.RecordTypeA,
					"",
				),
			)
		}
	case "record:cname":
		var records []ibclient.RecordCNAME
		obj := ibclient.NewRecordCNAME(
			ibclient.RecordCNAME{
				Name: result[2],
			},
		)
		client.GetObject(obj, ref, &records)
		for _, record := range records {
			client.deletedEndpoints = append(
				client.deletedEndpoints,
				endpoint.NewEndpoint(
					record.Name,
					endpoint.RecordTypeCNAME,
					"",
				),
			)
		}
	case "record:host":
		var records []ibclient.HostRecord
		obj := ibclient.NewHostRecord(
			ibclient.HostRecord{
				Name: result[2],
			},
		)
		client.GetObject(obj, ref, &records)
		for _, record := range records {
			client.deletedEndpoints = append(
				client.deletedEndpoints,
				endpoint.NewEndpoint(
					record.Name,
					endpoint.RecordTypeA,
					"",
				),
			)
		}
	case "record:txt":
		var records []ibclient.RecordTXT
		obj := ibclient.NewRecordTXT(
			ibclient.RecordTXT{
				Name: result[2],
			},
		)
		client.GetObject(obj, ref, &records)
		for _, record := range records {
			client.deletedEndpoints = append(
				client.deletedEndpoints,
				endpoint.NewEndpoint(
					record.Name,
					endpoint.RecordTypeTXT,
					"",
				),
			)
		}
	}
	return "", nil
}

func (client *mockIBConnector) UpdateObject(obj ibclient.IBObject, ref string) (refRes string, err error) {
	switch obj.ObjectType() {
	case "record:a":
		client.updatedEndpoints = append(
			client.updatedEndpoints,
			endpoint.NewEndpoint(
				obj.(*ibclient.RecordA).Name,
				obj.(*ibclient.RecordA).Ipv4Addr,
				endpoint.RecordTypeA,
			),
		)
	case "record:cname":
		client.updatedEndpoints = append(
			client.updatedEndpoints,
			endpoint.NewEndpoint(
				obj.(*ibclient.RecordCNAME).Name,
				obj.(*ibclient.RecordCNAME).Canonical,
				endpoint.RecordTypeCNAME,
			),
		)
	case "record:host":
		for _, i := range obj.(*ibclient.HostRecord).Ipv4Addrs {
			client.updatedEndpoints = append(
				client.updatedEndpoints,
				endpoint.NewEndpoint(
					obj.(*ibclient.HostRecord).Name,
					i.Ipv4Addr,
					endpoint.RecordTypeA,
				),
			)
		}
	case "record:txt":
		client.updatedEndpoints = append(
			client.updatedEndpoints,
			endpoint.NewEndpoint(
				obj.(*ibclient.RecordTXT).Name,
				obj.(*ibclient.RecordTXT).Text,
				endpoint.RecordTypeTXT,
			),
		)
	}
	return "", nil
}

func createMockInfobloxZone(fqdn string) ibclient.ZoneAuth {
	return ibclient.ZoneAuth{
		Fqdn: fqdn,
	}
}

func createMockInfobloxObject(name, recordType, value string) ibclient.IBObject {
	ref := fmt.Sprintf("record:%s/%s:%s/default", strings.ToLower(recordType), base64.StdEncoding.EncodeToString([]byte(name)), name)
	switch recordType {
	case endpoint.RecordTypeA:
		return ibclient.NewRecordA(
			ibclient.RecordA{
				Ref:      ref,
				Name:     name,
				Ipv4Addr: value,
			},
		)
	case endpoint.RecordTypeCNAME:
		return ibclient.NewRecordCNAME(
			ibclient.RecordCNAME{
				Ref:       ref,
				Name:      name,
				Canonical: value,
			},
		)
	case endpoint.RecordTypeTXT:
		return ibclient.NewRecordTXT(
			ibclient.RecordTXT{
				Ref:  ref,
				Name: name,
				Text: value,
			},
		)
	case "HOST":
		return ibclient.NewHostRecord(
			ibclient.HostRecord{
				Ref:  ref,
				Name: name,
				Ipv4Addrs: []ibclient.HostRecordIpv4Addr{
					{
						Ipv4Addr: value,
					},
				},
			},
		)
	}
	return nil
}

func newInfobloxProvider(domainFilter endpoint.DomainFilter, zoneIDFilter provider.ZoneIDFilter, dryRun bool, client ibclient.IBConnector) *InfobloxProvider {
	return &InfobloxProvider{
		client:       client,
		domainFilter: domainFilter,
		zoneIDFilter: zoneIDFilter,
		dryRun:       dryRun,
	}
}

func TestInfobloxRecords(t *testing.T) {
	client := mockIBConnector{
		mockInfobloxZones: &[]ibclient.ZoneAuth{
			createMockInfobloxZone("example.com"),
			createMockInfobloxZone("other.com"),
		},
		mockInfobloxObjects: &[]ibclient.IBObject{
			createMockInfobloxObject("example.com", endpoint.RecordTypeA, "123.123.123.122"),
			createMockInfobloxObject("example.com", endpoint.RecordTypeTXT, "heritage=external-dns,external-dns/owner=default"),
			createMockInfobloxObject("nginx.example.com", endpoint.RecordTypeA, "123.123.123.123"),
			createMockInfobloxObject("nginx.example.com", endpoint.RecordTypeTXT, "heritage=external-dns,external-dns/owner=default"),
			createMockInfobloxObject("whitespace.example.com", endpoint.RecordTypeA, "123.123.123.124"),
			createMockInfobloxObject("whitespace.example.com", endpoint.RecordTypeTXT, "heritage=external-dns,external-dns/owner=white space"),
			createMockInfobloxObject("hack.example.com", endpoint.RecordTypeCNAME, "cerberus.infoblox.com"),
			createMockInfobloxObject2("multiple.example.com", "HOST", "123.123.123.121", "123.123.123.122"),
			createMockInfobloxObject("multiple.example.com", endpoint.RecordTypeA, "123.123.123.123"),
			createMockInfobloxObject("multiple.example.com", endpoint.RecordTypeA, "123.123.123.124"),
			createMockInfobloxObject("multiple.example.com", endpoint.RecordTypeTXT, "heritage=external-dns,external-dns/owner=default"),
			createMockInfobloxObject("existing.example.com", endpoint.RecordTypeA, "124.1.1.1"),
			createMockInfobloxObject("existing.example.com", endpoint.RecordTypeA, "124.1.1.2"),
			createMockInfobloxObject("existing.example.com", endpoint.RecordTypeTXT, "heritage=external-dns,external-dns/owner=existing"),
			createMockInfobloxObject("host.example.com", "HOST", "125.1.1.1"),
		},
	}

	provider := newInfobloxProvider(endpoint.NewDomainFilter([]string{"example.com"}), provider.NewZoneIDFilter([]string{""}), true, &client)
	actual, err := provider.Records(context.Background())

	if err != nil {
		t.Fatal(err)
	}
	expected := []*endpoint.Endpoint{
		endpoint.NewEndpoint("example.com", endpoint.RecordTypeA, "123.123.123.122"),
		endpoint.NewEndpoint("example.com", endpoint.RecordTypeTXT, "\"heritage=external-dns,external-dns/owner=default\""),
		endpoint.NewEndpoint("nginx.example.com", endpoint.RecordTypeA, "123.123.123.123"),
		endpoint.NewEndpoint("nginx.example.com", endpoint.RecordTypeTXT, "\"heritage=external-dns,external-dns/owner=default\""),
		endpoint.NewEndpoint("whitespace.example.com", endpoint.RecordTypeA, "123.123.123.124"),
		endpoint.NewEndpoint("whitespace.example.com", endpoint.RecordTypeTXT, "\"heritage=external-dns,external-dns/owner=white space\""),
		endpoint.NewEndpoint("hack.example.com", endpoint.RecordTypeCNAME, "cerberus.infoblox.com"),
		endpoint.NewEndpoint("multiple.example.com", endpoint.RecordTypeA, "123.123.123.121", "123.123.123.122", "123.123.123.123", "123.123.123.124"),
		endpoint.NewEndpoint("multiple.example.com", endpoint.RecordTypeTXT, "\"heritage=external-dns,external-dns/owner=default\""),
		endpoint.NewEndpoint("existing.example.com", endpoint.RecordTypeA, "124.1.1.1", "124.1.1.2"),
		endpoint.NewEndpoint("existing.example.com", endpoint.RecordTypeTXT, "\"heritage=external-dns,external-dns/owner=existing\""),
		endpoint.NewEndpoint("host.example.com", endpoint.RecordTypeA, "125.1.1.1"),
	}
	validateEndpoints(t, actual, expected)
}

func TestInfobloxApplyChanges(t *testing.T) {
	client := mockIBConnector{}

	testInfobloxApplyChangesInternal(t, false, &client)

	validateEndpoints(t, client.createdEndpoints, []*endpoint.Endpoint{
		endpoint.NewEndpoint("example.com", endpoint.RecordTypeA, "1.2.3.4"),
		endpoint.NewEndpoint("example.com", endpoint.RecordTypeTXT, "tag"),
		endpoint.NewEndpoint("foo.example.com", endpoint.RecordTypeA, "1.2.3.4"),
		endpoint.NewEndpoint("foo.example.com", endpoint.RecordTypeTXT, "tag"),
		endpoint.NewEndpoint("bar.example.com", endpoint.RecordTypeCNAME, "other.com"),
		endpoint.NewEndpoint("bar.example.com", endpoint.RecordTypeTXT, "tag"),
		endpoint.NewEndpoint("other.com", endpoint.RecordTypeA, "5.6.7.8"),
		endpoint.NewEndpoint("other.com", endpoint.RecordTypeTXT, "tag"),
		endpoint.NewEndpoint("new.example.com", endpoint.RecordTypeA, "111.222.111.222"),
		endpoint.NewEndpoint("newcname.example.com", endpoint.RecordTypeCNAME, "other.com"),
		endpoint.NewEndpoint("multiple.example.com", endpoint.RecordTypeA, "1.2.3.4,3.4.5.6,8.9.10.11"),
		endpoint.NewEndpoint("multiple.example.com", endpoint.RecordTypeTXT, "tag-multiple-A-records"),
	})

	validateEndpoints(t, client.deletedEndpoints, []*endpoint.Endpoint{
		endpoint.NewEndpoint("old.example.com", endpoint.RecordTypeA, ""),
		endpoint.NewEndpoint("oldcname.example.com", endpoint.RecordTypeCNAME, ""),
		endpoint.NewEndpoint("deleted.example.com", endpoint.RecordTypeA, ""),
		endpoint.NewEndpoint("deleted.example.com", endpoint.RecordTypeTXT, ""),
		endpoint.NewEndpoint("deletedcname.example.com", endpoint.RecordTypeCNAME, ""),
	})

	validateEndpoints(t, client.updatedEndpoints, []*endpoint.Endpoint{})
}

func TestInfobloxApplyChangesDryRun(t *testing.T) {
	client := mockIBConnector{
		mockInfobloxObjects: &[]ibclient.IBObject{},
	}

	testInfobloxApplyChangesInternal(t, true, &client)

	validateEndpoints(t, client.createdEndpoints, []*endpoint.Endpoint{})

	validateEndpoints(t, client.deletedEndpoints, []*endpoint.Endpoint{})

	validateEndpoints(t, client.updatedEndpoints, []*endpoint.Endpoint{})
}

func testInfobloxApplyChangesInternal(t *testing.T, dryRun bool, client ibclient.IBConnector) {
	client.(*mockIBConnector).mockInfobloxZones = &[]ibclient.ZoneAuth{
		createMockInfobloxZone("example.com"),
		createMockInfobloxZone("other.com"),
	}
	client.(*mockIBConnector).mockInfobloxObjects = &[]ibclient.IBObject{
		createMockInfobloxObject("deleted.example.com", endpoint.RecordTypeA, "121.212.121.212"),
		createMockInfobloxObject("deleted.example.com", endpoint.RecordTypeTXT, "test-deleting-txt"),
		createMockInfobloxObject("deletedcname.example.com", endpoint.RecordTypeCNAME, "other.com"),
		createMockInfobloxObject("old.example.com", endpoint.RecordTypeA, "121.212.121.212"),
		createMockInfobloxObject("oldcname.example.com", endpoint.RecordTypeCNAME, "other.com"),
	}

	provider := newInfobloxProvider(
		endpoint.NewDomainFilter([]string{""}),
		provider.NewZoneIDFilter([]string{""}),
		dryRun,
		client,
	)

	createRecords := []*endpoint.Endpoint{
		endpoint.NewEndpoint("example.com", endpoint.RecordTypeA, "1.2.3.4"),
		endpoint.NewEndpoint("example.com", endpoint.RecordTypeTXT, "tag"),
		endpoint.NewEndpoint("foo.example.com", endpoint.RecordTypeA, "1.2.3.4"),
		endpoint.NewEndpoint("foo.example.com", endpoint.RecordTypeTXT, "tag"),
		endpoint.NewEndpoint("bar.example.com", endpoint.RecordTypeCNAME, "other.com"),
		endpoint.NewEndpoint("bar.example.com", endpoint.RecordTypeTXT, "tag"),
		endpoint.NewEndpoint("other.com", endpoint.RecordTypeA, "5.6.7.8"),
		endpoint.NewEndpoint("other.com", endpoint.RecordTypeTXT, "tag"),
		endpoint.NewEndpoint("nope.com", endpoint.RecordTypeA, "4.4.4.4"),
		endpoint.NewEndpoint("nope.com", endpoint.RecordTypeTXT, "tag"),
		endpoint.NewEndpoint("multiple.example.com", endpoint.RecordTypeA, "1.2.3.4,3.4.5.6,8.9.10.11"),
		endpoint.NewEndpoint("multiple.example.com", endpoint.RecordTypeTXT, "tag-multiple-A-records"),
	}

	updateOldRecords := []*endpoint.Endpoint{
		endpoint.NewEndpoint("old.example.com", endpoint.RecordTypeA, "121.212.121.212"),
		endpoint.NewEndpoint("oldcname.example.com", endpoint.RecordTypeCNAME, "other.com"),
		endpoint.NewEndpoint("old.nope.com", endpoint.RecordTypeA, "121.212.121.212"),
	}

	updateNewRecords := []*endpoint.Endpoint{
		endpoint.NewEndpoint("new.example.com", endpoint.RecordTypeA, "111.222.111.222"),
		endpoint.NewEndpoint("newcname.example.com", endpoint.RecordTypeCNAME, "other.com"),
		endpoint.NewEndpoint("new.nope.com", endpoint.RecordTypeA, "222.111.222.111"),
	}

	deleteRecords := []*endpoint.Endpoint{
		endpoint.NewEndpoint("deleted.example.com", endpoint.RecordTypeA, "121.212.121.212"),
		endpoint.NewEndpoint("deleted.example.com", endpoint.RecordTypeTXT, "test-deleting-txt"),
		endpoint.NewEndpoint("deletedcname.example.com", endpoint.RecordTypeCNAME, "other.com"),
		endpoint.NewEndpoint("deleted.nope.com", endpoint.RecordTypeA, "222.111.222.111"),
	}

	changes := &plan.Changes{
		Create:    createRecords,
		UpdateNew: updateNewRecords,
		UpdateOld: updateOldRecords,
		Delete:    deleteRecords,
	}

	if err := provider.ApplyChanges(context.Background(), changes); err != nil {
		t.Fatal(err)
	}
}

func TestInfobloxZones(t *testing.T) {
	client := mockIBConnector{
		mockInfobloxZones: &[]ibclient.ZoneAuth{
			createMockInfobloxZone("example.com"),
			createMockInfobloxZone("lvl1-1.example.com"),
			createMockInfobloxZone("lvl2-1.lvl1-1.example.com"),
		},
		mockInfobloxObjects: &[]ibclient.IBObject{},
	}

	provider := newInfobloxProvider(endpoint.NewDomainFilter([]string{"example.com"}), provider.NewZoneIDFilter([]string{""}), true, &client)
	zones, _ := provider.zones()
	var emptyZoneAuth *ibclient.ZoneAuth
	assert.Equal(t, provider.findZone(zones, "example.com").Fqdn, "example.com")
	assert.Equal(t, provider.findZone(zones, "nomatch-example.com"), emptyZoneAuth)
	assert.Equal(t, provider.findZone(zones, "nginx.example.com").Fqdn, "example.com")
	assert.Equal(t, provider.findZone(zones, "lvl1-1.example.com").Fqdn, "lvl1-1.example.com")
	assert.Equal(t, provider.findZone(zones, "lvl1-2.example.com").Fqdn, "example.com")
	assert.Equal(t, provider.findZone(zones, "lvl2-1.lvl1-1.example.com").Fqdn, "lvl2-1.lvl1-1.example.com")
	assert.Equal(t, provider.findZone(zones, "lvl2-2.lvl1-1.example.com").Fqdn, "lvl1-1.example.com")
	assert.Equal(t, provider.findZone(zones, "lvl2-2.lvl1-2.example.com").Fqdn, "example.com")
}

func TestExtendedRequestFDQDRegExBuilder(t *testing.T) {
	hostConfig := ibclient.HostConfig{
		Host:     "localhost",
		Port:     "8080",
		Username: "user",
		Password: "abcd",
		Version:  "2.3.1",
	}

	requestBuilder := NewExtendedRequestBuilder(0, "^staging\\.*test\\.com$", "")
	requestBuilder.Init(hostConfig)

	obj := ibclient.NewZoneAuth(ibclient.ZoneAuth{})

	req, _ := requestBuilder.BuildRequest(ibclient.GET, obj, "", ibclient.QueryParams{})

	assert.True(t, req.URL.Query().Get("fqdn~") == "^staging\\.*test\\.com$")

	req, _ = requestBuilder.BuildRequest(ibclient.CREATE, obj, "", ibclient.QueryParams{})

	assert.True(t, req.URL.Query().Get("fqdn~") == "")
}
func TestExtendedRequestNameRegExBuilder(t *testing.T) {
	hostConfig := ibclient.HostConfig{
		Host:     "localhost",
		Port:     "8080",
		Username: "user",
		Password: "abcd",
		Version:  "2.3.1",
	}

	requestBuilder := NewExtendedRequestBuilder(0, "", "*example*")
	requestBuilder.Init(hostConfig)

	obj := ibclient.NewZoneAuth(ibclient.ZoneAuth{})

	req, _ := requestBuilder.BuildRequest(ibclient.GET, obj, "", ibclient.QueryParams{})

	assert.True(t, req.URL.Query().Get("name~") == "*example*")

	req, _ = requestBuilder.BuildRequest(ibclient.CREATE, obj, "", ibclient.QueryParams{})

	assert.True(t, req.URL.Query().Get("name~") == "")
}
func TestExtendedRequestMaxResultsBuilder(t *testing.T) {
	hostConfig := ibclient.HostConfig{
		Host:     "localhost",
		Port:     "8080",
		Username: "user",
		Password: "abcd",
		Version:  "2.3.1",
	}

	requestBuilder := NewExtendedRequestBuilder(54321, "", "")
	requestBuilder.Init(hostConfig)

	obj := ibclient.NewRecordCNAME(ibclient.RecordCNAME{Zone: "foo.bar.com"})

	req, _ := requestBuilder.BuildRequest(ibclient.GET, obj, "", ibclient.QueryParams{})

	assert.True(t, req.URL.Query().Get("_max_results") == "54321")

	req, _ = requestBuilder.BuildRequest(ibclient.CREATE, obj, "", ibclient.QueryParams{})

	assert.True(t, req.URL.Query().Get("_max_results") == "")
}

func validateEndpoints(t *testing.T, endpoints []*endpoint.Endpoint, expected []*endpoint.Endpoint) {
	assert.True(t, testutils.SameEndpoints(endpoints, expected), "actual and expected endpoints don't match. %s:%s", endpoints, expected)
}
