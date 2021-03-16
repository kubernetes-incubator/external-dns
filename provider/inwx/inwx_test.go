/*
Copyright 2021 The Kubernetes Authors.

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

package inwx

import (
	"context"
	"errors"
	"strconv"
	"testing"
	"time"

	"github.com/alecthomas/assert"
	"github.com/nrdcg/goinwx"
	"github.com/stretchr/testify/mock"

	"sigs.k8s.io/external-dns/endpoint"
)

type mockInwxClient struct {
	mock.Mock
}

func (m *mockInwxClient) Login() (*goinwx.LoginResponse, error) {
	return nil, m.Called().Error(0)
}

func (m *mockInwxClient) Logout() error {
	return m.Called().Error(0)
}

func (m *mockInwxClient) ListNameserverEntries(domain string) (*goinwx.NamserverListResponse, error) {
	args := m.Called(domain)
	return args.Get(0).(*goinwx.NamserverListResponse), args.Error(1)
}

func (m *mockInwxClient) CreateNameserverRecord(request *goinwx.NameserverRecordRequest) (int, error) {
	args := m.Called(request)
	return args.Int(0), args.Error(1)
}

func (m *mockInwxClient) UpdateNameserverRecord(recID int, request *goinwx.NameserverRecordRequest) error {
	return m.Called(recID, request).Error(0)
}

func (m *mockInwxClient) DeleteNameserverRecord(recID int) error {
	return m.Called(recID).Error(0)
}

func (m *mockInwxClient) NameserverInfo(request *goinwx.NameserverInfoRequest) (*goinwx.NamserverInfoResponse, error) {
	args := m.Called(request)
	return args.Get(0).(*goinwx.NamserverInfoResponse), args.Error(1)
}

func TestInwxProvider_deleteRecords_NoRecordId(t *testing.T) {
	assert := assert.New(t)

	inwx := InwxProvider{}

	endpoints := []*endpoint.Endpoint{{}}

	err := inwx.deleteRecords(context.Background(), endpoints)

	assert.NotNil(err)
}

func TestInwxProvider_deleteRecords_DeleteRecordFails(t *testing.T) {
	assert := assert.New(t)

	mockInwxClient := &mockInwxClient{}
	inwx := InwxProvider{
		Client: mockInwxClient,
		DryRun: false,
	}

	ep1 := (&endpoint.Endpoint{RecordType: "A", DNSName: "name1", Targets: endpoint.NewTargets("target1")}).
		WithProviderSpecific(providerTypeRoID, "1").
		WithProviderSpecific(providerTypeDomain, "domain1")
	endpoints := []*endpoint.Endpoint{ep1}

	returnedError := errors.New("failure")

	mockInwxClient.On("NameserverInfo", &goinwx.NameserverInfoRequest{Domain: "domain1", RoID: 1}).Return(&goinwx.NamserverInfoResponse{
		Records: []goinwx.NameserverRecord{
			{Type: "A", Name: "name1", ID: 1},
		},
	}, nil).
		On("DeleteNameserverRecord", 1).Return(returnedError)

	err := inwx.deleteRecords(context.Background(), endpoints)

	assert.NotNil(err)
	assert.Equal(returnedError, err)
}

func TestInwxProvider_deleteRecords_DryRunNotCallingService(t *testing.T) {
	assert := assert.New(t)

	mockInwxClient := &mockInwxClient{}
	inwx := InwxProvider{
		Client: mockInwxClient,
		DryRun: true,
	}

	ep1 := (&endpoint.Endpoint{RecordType: "A", DNSName: "name1", Targets: endpoint.NewTargets("target1")}).
		WithProviderSpecific(providerTypeRoID, "1").
		WithProviderSpecific(providerTypeDomain, "domain1")
	endpoints := []*endpoint.Endpoint{ep1}

	returnedError := errors.New("failure")

	mockInwxClient.On("NameserverInfo", &goinwx.NameserverInfoRequest{Domain: "domain1", RoID: 1}).Return(&goinwx.NamserverInfoResponse{
		Records: []goinwx.NameserverRecord{
			{Type: "A", Name: "name1", ID: 1},
		},
	}, nil).
		On("DeleteNameserverRecord", 1).Return(returnedError)

	err := inwx.deleteRecords(context.Background(), endpoints)

	assert.Nil(err)

	mockInwxClient.AssertNotCalled(t, "DeleteNameserverRecord", 1)
}

func TestInwxProvider_deleteRecords_DeletingMultipleRecords(t *testing.T) {
	assert := assert.New(t)

	mockInwxClient := &mockInwxClient{}
	inwx := InwxProvider{
		Client: mockInwxClient,
		DryRun: false,
	}

	ep1 := (&endpoint.Endpoint{RecordType: "A", DNSName: "name1", Targets: endpoint.NewTargets("target1")}).
		WithProviderSpecific(providerTypeRoID, "1").
		WithProviderSpecific(providerTypeDomain, "domain1")
	ep3 := (&endpoint.Endpoint{RecordType: "CNAME", DNSName: "name3", Targets: endpoint.NewTargets("target3")}).
		WithProviderSpecific(providerTypeRoID, "1").
		WithProviderSpecific(providerTypeDomain, "domain1")
	endpoints := []*endpoint.Endpoint{ep1, ep3}

	mockInwxClient.On("NameserverInfo", &goinwx.NameserverInfoRequest{Domain: "domain1", RoID: 1}).Return(&goinwx.NamserverInfoResponse{
		Records: []goinwx.NameserverRecord{
			{Type: "A", Name: "name1", ID: 1},
			{Type: "TXT", Name: "name2", ID: 2},
			{Type: "CNAME", Name: "name3", ID: 3},
		},
	}, nil).
		On("DeleteNameserverRecord", 1).Return(nil).
		On("DeleteNameserverRecord", 3).Return(nil)

	err := inwx.deleteRecords(context.Background(), endpoints)

	assert.Nil(err)

	mockInwxClient.AssertCalled(t, "DeleteNameserverRecord", 1)
	mockInwxClient.AssertCalled(t, "DeleteNameserverRecord", 3)
}

// TODO: test ApplyChanges

func TestInwxProvider_Records_LoginError(t *testing.T) {
	assert := assert.New(t)

	mockInwxClient := &mockInwxClient{}

	inwx := InwxProvider{
		Client: mockInwxClient,
	}

	returnedError := errors.New("failed")
	mockInwxClient.On("Login").Return(returnedError)

	_, err := inwx.Records(context.Background())

	assert.NotNil(err)
	assert.Equal(err, returnedError)

	mockInwxClient.AssertCalled(t, "Login")
}

func TestInwxProvider_Records_FetchingDomainsFailedAndLogoutCalled(t *testing.T) {
	assert := assert.New(t)

	mockInwxClient := &mockInwxClient{}

	inwx := InwxProvider{
		Client: mockInwxClient,
	}

	returnedError := errors.New("failed")
	var entriesResp *goinwx.NamserverListResponse

	mockInwxClient.On("Login").Return(nil).
		On("Logout").Return(nil).
		On("ListNameserverEntries", "").Return(entriesResp, returnedError)

	_, err := inwx.Records(context.Background())

	assert.NotNil(err)
	assert.Equal(err, returnedError)

	mockInwxClient.AssertCalled(t, "ListNameserverEntries", "")
	mockInwxClient.AssertCalled(t, "Logout")
}

func TestInwxProvider_Records_NameserverInfoFailed(t *testing.T) {
	assert := assert.New(t)

	mockInwxClient := &mockInwxClient{}

	inwx := InwxProvider{
		Client: mockInwxClient,
		Domain: endpoint.DomainFilter{},
	}

	expReq := &goinwx.NameserverInfoRequest{RoID: 1, Domain: "domain1"}
	returnedError := errors.New("failed")
	entriesResp := &goinwx.NamserverListResponse{
		Domains: []goinwx.NameserverDomain{
			{RoID: 1, Domain: "domain1"},
		},
	}
	var infoResp *goinwx.NamserverInfoResponse

	mockInwxClient.On("Login").Return(nil).
		On("Logout").Return(nil).
		On("ListNameserverEntries", "").Return(entriesResp, nil).
		On("NameserverInfo", expReq).Return(infoResp, returnedError)

	_, err := inwx.Records(context.Background())

	assert.NotNil(err)
	assert.Equal(err, returnedError)

	mockInwxClient.AssertCalled(t, "NameserverInfo", expReq)
}

func TestInwxProvider_Records_MultipleRecordsOneWithInvalidType(t *testing.T) {
	assert := assert.New(t)

	mockInwxClient := &mockInwxClient{}

	inwx := InwxProvider{
		Client: mockInwxClient,
		Domain: endpoint.DomainFilter{},
	}

	entriesResp := &goinwx.NamserverListResponse{
		Domains: []goinwx.NameserverDomain{
			{RoID: 1, Domain: "domain1"},
			{RoID: 2, Domain: "domain2"},
		},
	}
	expReq1 := &goinwx.NameserverInfoRequest{RoID: 1, Domain: "domain1"}
	expReq2 := &goinwx.NameserverInfoRequest{RoID: 2, Domain: "domain2"}
	infoResp1 := &goinwx.NamserverInfoResponse{
		Records: []goinwx.NameserverRecord{
			{Type: "A", Name: "name1", Content: "content1"},
			{Type: "CNAME", Name: "name2", Content: "content2"},
		},
	}
	infoResp2 := &goinwx.NamserverInfoResponse{
		Records: []goinwx.NameserverRecord{
			{Type: "TXT", Name: "name3", Content: "content3"},
			{Type: "AAAA", Name: "name4", Content: "content4"},
		},
	}

	mockInwxClient.On("Login").Return(nil).
		On("Logout").Return(nil).
		On("ListNameserverEntries", "").Return(entriesResp, nil).
		On("NameserverInfo", expReq1).Return(infoResp1, nil).
		On("NameserverInfo", expReq2).Return(infoResp2, nil)

	result, err := inwx.Records(context.Background())

	assert.Nil(err)
	assert.NotNil(result)
	assert.Len(result, 3, "3 endpoints expected, because the fourth is AAAA and should be ignored")

	for i := 0; i < 3; i++ {
		var expRecordType string
		switch i {
		case 0:
			expRecordType = "A"
		case 1:
			expRecordType = "CNAME"
		case 2:
			expRecordType = "TXT"
		}

		expDomainId := "1"
		if i >= 2 {
			expDomainId = "2"
		}

		assert.Equal(expRecordType, result[i].RecordType)
		assert.Equal("name"+strconv.Itoa(i+1), result[i].DNSName)
		assert.Equal(endpoint.NewTargets("content"+strconv.Itoa(i+1)), result[i].Targets)
		assert.Equal(expDomainId, getProviderSpecificProperty(result[i], providerTypeRoID).Value)
		assert.Equal("domain"+expDomainId, getProviderSpecificProperty(result[i], providerTypeDomain).Value)
	}
}

func TestInwxProvider_Records_ReturnCached(t *testing.T) {
	assert := assert.New(t)

	mockInwxClient := &mockInwxClient{}

	inwx := InwxProvider{
		Client: mockInwxClient,
		Domain: endpoint.DomainFilter{},
	}

	var expRecords []*endpoint.Endpoint
	expRecords = append(expRecords, &endpoint.Endpoint{DNSName: "test1"})
	inwx.cachedRecords = expRecords
	inwx.cachedRecordsTime = time.Now().Add(-1 * time.Minute)
	inwx.ReloadInterval = 2 * time.Minute

	result, err := inwx.Records(context.Background())

	assert.Nil(err)
	assert.NotNil(result)
	assert.Len(result, 1)
	assert.Equal(expRecords, result)
}

func TestInwxProvider_CreateCreateRecordRequest(t *testing.T) {
	assert := assert.New(t)

	inwx := InwxProvider{
		domains: []*goinwx.NameserverDomain{
			{RoID: 2, Domain: "example.com"},
		},
	}

	// test: all parameters given
	ep := endpoint.NewEndpointWithTTL("foo.example.com", "A", endpoint.TTL(1), "target").
		WithProviderSpecific(providerTypeRoID, "2").
		WithProviderSpecific(providerTypeDomain, "example.com")

	result, err := inwx.createCreateRecordRequest(ep)

	assert.Nil(err)
	assert.NotNil(result)
	assert.Equal(2, result.RoID)
	assert.Equal("example.com", result.Domain)
	assert.Equal("foo", result.Name)
	assert.Equal("A", result.Type)
	assert.Equal("target", result.Content)
	assert.Equal(1, result.TTL)

	// test: only required parameters given
	ep = endpoint.NewEndpoint("foo.example.com", "A", "target")

	result, err = inwx.createCreateRecordRequest(ep)

	assert.Nil(err)
	assert.NotNil(result)
	assert.Equal(2, result.RoID)
	assert.Equal("example.com", result.Domain)
	assert.Equal("foo", result.Name)
	assert.Equal("A", result.Type)
	assert.Equal("target", result.Content)
	assert.Equal(0, result.TTL)
}

func TestInwxProvider_CreateUpdateRecordRequest(t *testing.T) {
	assert := assert.New(t)

	inwx := InwxProvider{
		domains: []*goinwx.NameserverDomain{
			{RoID: 2, Domain: "example.com"},
		},
	}

	// test: all parameters given
	ep := endpoint.NewEndpointWithTTL("foo.example.com", "A", endpoint.TTL(1), "target").
		WithProviderSpecific(providerTypeRoID, "2").
		WithProviderSpecific(providerTypeDomain, "example.com")

	result, err := inwx.createUpdateRecordRequest(ep)

	assert.Nil(err)
	assert.NotNil(result)
	assert.Equal(0, result.RoID)
	assert.Equal("", result.Domain)
	assert.Equal("foo", result.Name)
	assert.Equal("A", result.Type)
	assert.Equal("target", result.Content)
	assert.Equal(1, result.TTL)

	// test: only required parameters given
	ep = endpoint.NewEndpoint("foo.example.com", "A", "target")

	result, err = inwx.createUpdateRecordRequest(ep)

	assert.Nil(err)
	assert.NotNil(result)
	assert.Equal(0, result.RoID)
	assert.Equal("", result.Domain)
	assert.Equal("foo", result.Name)
	assert.Equal("A", result.Type)
	assert.Equal("target", result.Content)
	assert.Equal(0, result.TTL)
}

func TestCreateEndpoint(t *testing.T) {
	assert := assert.New(t)

	nsr := goinwx.NameserverRecord{
		Name:    "foo.example.com",
		Type:    "A",
		TTL:     1,
		Content: "target",
	}

	result := createEndpoint(&nsr, 2, "example.com")

	assert.NotNil(result)
	assert.Equal("foo.example.com", result.DNSName)
	assert.Equal("A", result.RecordType)
	assert.Equal(endpoint.TTL(1), result.RecordTTL)
	assert.Len(result.Targets, 1)
	assert.Equal("target", result.Targets[0])
	assert.Equal(providerTypeRoID, result.ProviderSpecific[0].Name)
	assert.Equal("2", result.ProviderSpecific[0].Value)
	assert.Equal(providerTypeDomain, result.ProviderSpecific[1].Name)
	assert.Equal("example.com", result.ProviderSpecific[1].Value)
}

func TestGetProviderSpecificIntProperty(t *testing.T) {
	assert := assert.New(t)

	ep1 := endpoint.NewEndpoint("dnsName", "A", "target").
		WithProviderSpecific("key", strconv.Itoa(1))
	ep2 := endpoint.NewEndpoint("dnsName", "A", "target")

	result1_1, err1_1 := getProviderSpecificIntProperty(ep1, "key", true)
	result1_2, err1_2 := getProviderSpecificIntProperty(ep1, "key", false)
	result2_1, err2_1 := getProviderSpecificIntProperty(ep2, "key", true)
	result2_2, err2_2 := getProviderSpecificIntProperty(ep2, "key", false)

	assert.Equal(1, result1_1)
	assert.Nil(err1_1)

	assert.Equal(1, result1_2)
	assert.Nil(err1_2)

	assert.Equal(0, result2_1)
	assert.NotNil(err2_1)

	assert.Equal(0, result2_2)
	assert.Nil(err2_2)
}

func TestGetProviderSpecificStringProperty(t *testing.T) {
	assert := assert.New(t)

	ep1 := endpoint.NewEndpoint("dnsName", "A", "target").
		WithProviderSpecific("key", "value")
	ep2 := endpoint.NewEndpoint("dnsName", "A", "target")

	result1_1, err1_1 := getProviderSpecificStringProperty(ep1, "key", true)
	result1_2, err1_2 := getProviderSpecificStringProperty(ep1, "key", false)
	result2_1, err2_1 := getProviderSpecificStringProperty(ep2, "key", true)
	result2_2, err2_2 := getProviderSpecificStringProperty(ep2, "key", false)

	assert.Equal("value", result1_1)
	assert.Nil(err1_1)

	assert.Equal("value", result1_2)
	assert.Nil(err1_2)

	assert.Equal("", result2_1)
	assert.NotNil(err2_1)

	assert.Equal("", result2_2)
	assert.Nil(err2_2)
}

func TestGetNameFromDnsName(t *testing.T) {
	assert := assert.New(t)

	assert.Equal("foo", getNameFromDNSName("foo.example.com", "example.com"))
	assert.Equal("foo.bar", getNameFromDNSName("foo.bar.example.com", "example.com"))
	assert.Equal("foo.bar", getNameFromDNSName("foo.bar", "example.com"))
}

func TestMergePlanUpdates(t *testing.T) {
	assert := assert.New(t)

	old := []*endpoint.Endpoint{
		endpoint.NewEndpoint("foo", "A", "targetOld"), // merge with new[1]
		endpoint.NewEndpoint("foo", "TXT", "txtOld"),  // merge with new[0]
		endpoint.NewEndpoint("old", "A", "target"),    // to-be-removed
	}
	new := []*endpoint.Endpoint{
		endpoint.NewEndpoint("foo", "TXT", "txtNew"),  // merge with old[1]
		endpoint.NewEndpoint("foo", "A", "targetNew"), // merge with old[0]
		endpoint.NewEndpoint("new", "A", "target"),    // to-be-removed
	}

	result := mergePlanUpdates(old, new)

	assert.NotEmpty(result)
	assert.Len(result, 2)
	assert.Equal(old[1], result[0].endpointOld)
	assert.Equal(new[0], result[0].endpointNew)
	assert.Equal(old[0], result[1].endpointOld)
	assert.Equal(new[1], result[1].endpointNew)
}

func TestUpdatedEndpoints_DnsUpdateRequired(t *testing.T) {
	assert := assert.New(t)

	// test: target changed
	ep1 := endpoint.NewEndpoint("foo", "A", "target1")
	ep2 := endpoint.NewEndpoint("foo", "A", "target2")
	ep3 := endpoint.NewEndpoint("foo", "A", "target1")

	result1 := (&updatedEndpoint{endpointOld: ep1, endpointNew: ep2}).dnsUpdateRequired()
	result2 := (&updatedEndpoint{endpointOld: ep1, endpointNew: ep3}).dnsUpdateRequired()

	assert.True(result1)
	assert.False(result2)

	// test: ttl changed
	ep1 = endpoint.NewEndpointWithTTL("foo", "A", endpoint.TTL(1), "target")
	ep2 = endpoint.NewEndpointWithTTL("foo", "A", endpoint.TTL(2), "target")
	ep3 = endpoint.NewEndpointWithTTL("foo", "A", endpoint.TTL(1), "target")
	ep4 := endpoint.NewEndpointWithTTL("foo", "A", endpoint.TTL(0), "target")

	result1 = (&updatedEndpoint{endpointOld: ep1, endpointNew: ep2}).dnsUpdateRequired()
	result2 = (&updatedEndpoint{endpointOld: ep1, endpointNew: ep3}).dnsUpdateRequired()
	result3 := (&updatedEndpoint{endpointOld: ep1, endpointNew: ep4}).dnsUpdateRequired() // dont change if TTL target is 0

	assert.True(result1)
	assert.False(result2)
	assert.False(result3)
}

func TestEndpointTargetChanged(t *testing.T) {
	assert := assert.New(t)

	ep1 := endpoint.NewEndpoint("foo", "A", "target1")
	ep2 := endpoint.NewEndpoint("foo", "A", "target2")
	ep3 := endpoint.NewEndpoint("foo", "A", "target1")

	result1 := endpointTargetChanged(ep1, ep2)
	result2 := endpointTargetChanged(ep1, ep3)

	assert.True(result1)
	assert.False(result2)
}

func TestEndpointTtlChanged(t *testing.T) {
	assert := assert.New(t)

	ep1 := endpoint.NewEndpointWithTTL("foo", "A", endpoint.TTL(1), "target")
	ep2 := endpoint.NewEndpointWithTTL("foo", "A", endpoint.TTL(2), "target")
	ep3 := endpoint.NewEndpointWithTTL("foo", "A", endpoint.TTL(1), "target")

	result1 := endpointTTLChanged(ep1, ep2)
	result2 := endpointTTLChanged(ep1, ep3)

	assert.True(result1)
	assert.False(result2)
}

func TestEndpointsAreTheSame(t *testing.T) {
	assert := assert.New(t)

	ep1 := endpoint.NewEndpoint("foo", "A", "target1")
	ep2 := endpoint.NewEndpoint("foo", "TXT", "target2")
	ep3 := endpoint.NewEndpoint("foo", "A", "target1")

	result1 := endpointsAreTheSame(ep1, ep2)
	result2 := endpointsAreTheSame(ep1, ep3)

	assert.False(result1)
	assert.True(result2)
}

func getProviderSpecificProperty(ep *endpoint.Endpoint, key string) endpoint.ProviderSpecificProperty {
	val, _ := ep.GetProviderSpecificProperty(key)

	return val
}
