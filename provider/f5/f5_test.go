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

package f5

import (
	"context"
	"encoding/json"
	factory "github.com/F5Networks/f5cs-sdk/dnsfactory"
	authenticationApi "github.com/F5Networks/f5cs-sdk/generated/authentication"
	subscriptionApi "github.com/F5Networks/f5cs-sdk/generated/subscription"
	"github.com/pkg/errors"
	"github.com/stretchr/testify/assert"
	"math/rand"
	"net/http"
	"os"
	"sigs.k8s.io/external-dns/endpoint"
	"sigs.k8s.io/external-dns/internal/testutils"
	"sigs.k8s.io/external-dns/plan"
	"testing"
)

// Stub API for Unit testing
type F5DNSSubscriptionAPIStub struct {
	zones     map[string]*factory.ServiceRequest
	activated map[string]bool
}

type F5DNSAuthenticationAPIStub struct{}

func (f5 *F5DNSSubscriptionAPIStub) ListSubscriptions(ctx context.Context, accountID string, opts *subscriptionApi.ListSubscriptionsOpts) (subscriptionApi.V1Subscriptions, *http.Response, error) {
	res := subscriptionApi.V1Subscriptions{}
	token := ctx.Value(subscriptionApi.ContextAccessToken).(string)
	if token == "failtest" {
		return res, nil, errors.New("Invalid token")
	}
	for _, sr := range f5.zones {
		subJson, err := json.Marshal(sr)
		if err != nil {
			return subscriptionApi.V1Subscriptions{}, nil, err
		}
		var sub subscriptionApi.V1Subscription
		err = json.Unmarshal(subJson, &sub)
		if err != nil {
			return subscriptionApi.V1Subscriptions{}, nil, err
		}
		activated := f5.activated[sub.SubscriptionId]
		if !activated {
			continue
		}
		res.Subscriptions = append(res.Subscriptions, sub)
	}
	return res, nil, nil
}
func (f5 *F5DNSSubscriptionAPIStub) UpdateSubscription(ctx context.Context, subscriptionID string, request subscriptionApi.V1UpdateSubscriptionRequest) (subscriptionApi.V1Subscription, *http.Response, error) {
	srJson, err := json.Marshal(request)
	if err != nil {
		return subscriptionApi.V1Subscription{}, nil, err
	}
	var sr factory.ServiceRequest
	err = json.Unmarshal(srJson, &sr)
	if err != nil {
		return subscriptionApi.V1Subscription{}, nil, err
	}
	if _, ok := f5.zones[sr.Configuration.GslbService.GetZone()]; !ok {
		return subscriptionApi.V1Subscription{}, nil, errors.New("Zone not found for update")
	}
	sr.ServiceType = "gslb"
	f5.zones[sr.Configuration.GslbService.GetZone()] = &sr
	f5.activated[sr.SubscriptionId] = true
	return subscriptionApi.V1Subscription{Configuration: request.Configuration}, nil, nil
}

func (f5 *F5DNSSubscriptionAPIStub) BatchActivateSubscription(ctx context.Context, request subscriptionApi.V1BatchSubscriptionIdRequest) (subscriptionApi.V1BatchSubscriptionStatusResponse, *http.Response, error) {
	token := ctx.Value(subscriptionApi.ContextAccessToken).(string)
	if token == "failbatchactivatetest" {
		return subscriptionApi.V1BatchSubscriptionStatusResponse{}, nil, errors.New("Invalid token")
	}
	for _, subId := range request.SubscriptionIds {
		f5.activated[subId] = true
	}
	return subscriptionApi.V1BatchSubscriptionStatusResponse{}, nil, nil
}

func (f5 *F5DNSSubscriptionAPIStub) BatchRetireSubscription(ctx context.Context, request subscriptionApi.V1BatchSubscriptionIdRequest) (subscriptionApi.V1BatchSubscriptionStatusResponse, *http.Response, error) {
	token := ctx.Value(subscriptionApi.ContextAccessToken).(string)
	if token == "failbatchretiretest" {
		return subscriptionApi.V1BatchSubscriptionStatusResponse{}, nil, errors.New("Invalid token")
	}
	for _, subId := range request.SubscriptionIds {
		f5.activated[subId] = false
	}
	return subscriptionApi.V1BatchSubscriptionStatusResponse{}, nil, nil
}

func (f5 *F5DNSSubscriptionAPIStub) ActivateSubscription(ctx context.Context, subscriptionId string) (subscriptionApi.V1SubscriptionStatusResponse, *http.Response, error) {
	token := ctx.Value(subscriptionApi.ContextAccessToken).(string)
	if token == "failbatchactivatetest" {
		return subscriptionApi.V1SubscriptionStatusResponse{}, nil, errors.New("Invalid token")
	}
	f5.activated[subscriptionId] = true
	return subscriptionApi.V1SubscriptionStatusResponse{}, nil, nil
}

func (f5 *F5DNSSubscriptionAPIStub) RetireSubscription(ctx context.Context, subscriptionId string) (subscriptionApi.V1SubscriptionStatusResponse, *http.Response, error) {
	token := ctx.Value(subscriptionApi.ContextAccessToken).(string)
	if token == "failbatchretiretest" {
		return subscriptionApi.V1SubscriptionStatusResponse{}, nil, errors.New("Invalid token")
	}
	f5.activated[subscriptionId] = false
	return subscriptionApi.V1SubscriptionStatusResponse{}, nil, nil
}

func (f5 *F5DNSSubscriptionAPIStub) CreateSubscription(ctx context.Context, request subscriptionApi.V1CreateSubscriptionRequest) (subscriptionApi.V1Subscription, *http.Response, error) {
	srJson, err := json.Marshal(request)
	if err != nil {
		return subscriptionApi.V1Subscription{}, nil, err
	}
	var sr factory.ServiceRequest
	err = json.Unmarshal(srJson, &sr)
	if err != nil {
		return subscriptionApi.V1Subscription{}, nil, err
	}
	if sr.SubscriptionId == "" {
		sr.SubscriptionId, err = factory.NewResourceID("s")
		if err != nil {
			return subscriptionApi.V1Subscription{}, nil, err
		}
	}
	f5.zones[sr.Configuration.GslbService.GetZone()] = &sr
	f5.activated[sr.SubscriptionId] = false
	return subscriptionApi.V1Subscription{SubscriptionId: sr.SubscriptionId, Configuration: request.Configuration}, nil, nil
}

func (f5 *F5DNSAuthenticationAPIStub) Login(ctx context.Context, request authenticationApi.AuthenticationServiceLoginRequest) (authenticationApi.AuthenticationServiceLoginReply, *http.Response, error) {
	accessToken := RandomString(10)
	expiresAt := "1"
	if request.Username == "failbatchretiretest" {
		accessToken = "failbatchretiretest"
	} else if request.Username == "failbatchactivatetest" {
		accessToken = "failbatchactivatetest"
	} else if request.Username == "tokentest" {
		accessToken = "failtest"
	} else if request.Username != "test" {
		return authenticationApi.AuthenticationServiceLoginReply{}, nil, errors.New("Failed to authenticate user")
	}

	return authenticationApi.AuthenticationServiceLoginReply{
		AccessToken: accessToken,
		ExpiresAt:   expiresAt,
	}, nil, nil
}

func NewF5DNSSubscriptionAPISub(zones map[string]*factory.ServiceRequest, activated map[string]bool) *F5DNSSubscriptionAPIStub {
	return &F5DNSSubscriptionAPIStub{
		zones:     zones,
		activated: activated,
	}
}

// Begin Test cases
func TestF5DNSApplyChanges(t *testing.T) {
	// Tests map for Creates, Updates, Deletes
	// ApplyChanges
	// Records
	// Verify Records result matches expected Endpoints
	tests := []struct {
		name              string
		changes           *plan.Changes
		expectedEndpoints []*endpoint.Endpoint
	}{
		{"initial empty", &plan.Changes{
			Create:    []*endpoint.Endpoint{},
			UpdateNew: []*endpoint.Endpoint{},
			UpdateOld: []*endpoint.Endpoint{},
			Delete:    []*endpoint.Endpoint{},
		}, []*endpoint.Endpoint{},
		},
		{"create new", &plan.Changes{
			Create: []*endpoint.Endpoint{
				endpoint.NewEndpointWithTTL("lbr.hack.me", "A", endpoint.TTL(600), "13.56.180.35"),
				endpoint.NewEndpointWithTTL("clbr.hack.me", "CNAME", endpoint.TTL(600), "elb.aws.com"),
			},
			UpdateNew: []*endpoint.Endpoint{},
			UpdateOld: []*endpoint.Endpoint{},
			Delete:    []*endpoint.Endpoint{},
		}, []*endpoint.Endpoint{
			endpoint.NewEndpointWithTTL("lbr.hack.me", "A", endpoint.TTL(600), "13.56.180.35"),
			endpoint.NewEndpointWithTTL("clbr.hack.me", "CNAME", endpoint.TTL(600), "elb.aws.com"),
		}},
		{"empty plan after create new", &plan.Changes{
			Create:    []*endpoint.Endpoint{},
			UpdateNew: []*endpoint.Endpoint{},
			UpdateOld: []*endpoint.Endpoint{},
			Delete:    []*endpoint.Endpoint{},
		}, []*endpoint.Endpoint{
			endpoint.NewEndpointWithTTL("lbr.hack.me", "A", endpoint.TTL(600), "13.56.180.35"),
			endpoint.NewEndpointWithTTL("clbr.hack.me", "CNAME", endpoint.TTL(600), "elb.aws.com"),
		}},
		{"add new", &plan.Changes{
			Create: []*endpoint.Endpoint{
				endpoint.NewEndpoint("create-test1.hack.me", endpoint.RecordTypeA, "1.2.3.4"),
				endpoint.NewEndpoint("create-test2.hack.me", endpoint.RecordTypeA, "1.2.3.5"),
			},
			UpdateNew: []*endpoint.Endpoint{},
			UpdateOld: []*endpoint.Endpoint{},
			Delete:    []*endpoint.Endpoint{},
		}, []*endpoint.Endpoint{
			endpoint.NewEndpointWithTTL("lbr.hack.me", "A", endpoint.TTL(600), "13.56.180.35"),
			endpoint.NewEndpointWithTTL("clbr.hack.me", "CNAME", endpoint.TTL(600), "elb.aws.com"),
			endpoint.NewEndpointWithTTL("create-test1.hack.me", "A", endpoint.TTL(30), "1.2.3.4"),
			endpoint.NewEndpointWithTTL("create-test2.hack.me", "A", endpoint.TTL(30), "1.2.3.5"),
		}},
		{"update A records", &plan.Changes{
			Create: []*endpoint.Endpoint{},
			UpdateNew: []*endpoint.Endpoint{
				endpoint.NewEndpoint("create-test1.hack.me", endpoint.RecordTypeA, "4.3.2.1"),
				endpoint.NewEndpoint("create-test2.hack.me", endpoint.RecordTypeA, "5.3.2.1"),
			},
			UpdateOld: []*endpoint.Endpoint{
				endpoint.NewEndpoint("create-test1.hack.me", endpoint.RecordTypeA, "1.2.3.4"),
				endpoint.NewEndpoint("create-test2.hack.me", endpoint.RecordTypeA, "1.2.3.5"),
			},
			Delete: []*endpoint.Endpoint{},
		}, []*endpoint.Endpoint{
			endpoint.NewEndpointWithTTL("lbr.hack.me", "A", endpoint.TTL(600), "13.56.180.35"),
			endpoint.NewEndpointWithTTL("clbr.hack.me", "CNAME", endpoint.TTL(600), "elb.aws.com"),
			endpoint.NewEndpointWithTTL("create-test1.hack.me", "A", endpoint.TTL(30), "4.3.2.1"),
			endpoint.NewEndpointWithTTL("create-test2.hack.me", "A", endpoint.TTL(30), "5.3.2.1"),
		}},
		{"update CNAME records", &plan.Changes{
			Create: []*endpoint.Endpoint{},
			UpdateNew: []*endpoint.Endpoint{
				endpoint.NewEndpoint("clbr.hack.me", endpoint.RecordTypeCNAME, "elb2.aws.com"),
			},
			UpdateOld: []*endpoint.Endpoint{
				endpoint.NewEndpoint("clbr.hack.me", endpoint.RecordTypeCNAME, "elb.aws.com"),
			},
			Delete: []*endpoint.Endpoint{},
		}, []*endpoint.Endpoint{
			endpoint.NewEndpointWithTTL("lbr.hack.me", "A", endpoint.TTL(600), "13.56.180.35"),
			endpoint.NewEndpointWithTTL("clbr.hack.me", "CNAME", endpoint.TTL(600), "elb2.aws.com"),
			endpoint.NewEndpointWithTTL("create-test1.hack.me", "A", endpoint.TTL(30), "4.3.2.1"),
			endpoint.NewEndpointWithTTL("create-test2.hack.me", "A", endpoint.TTL(30), "5.3.2.1"),
		}},
		{"delete test1", &plan.Changes{
			Create:    []*endpoint.Endpoint{},
			UpdateNew: []*endpoint.Endpoint{},
			UpdateOld: []*endpoint.Endpoint{},
			Delete: []*endpoint.Endpoint{
				endpoint.NewEndpoint("create-test1.hack.me", endpoint.RecordTypeA, "4.3.2.1"),
			},
		}, []*endpoint.Endpoint{
			endpoint.NewEndpointWithTTL("lbr.hack.me", "A", endpoint.TTL(600), "13.56.180.35"),
			endpoint.NewEndpointWithTTL("clbr.hack.me", "CNAME", endpoint.TTL(600), "elb2.aws.com"),
			endpoint.NewEndpointWithTTL("create-test2.hack.me", "A", endpoint.TTL(30), "5.3.2.1"),
		}},
		{"create, update, delete", &plan.Changes{
			Create: []*endpoint.Endpoint{
				endpoint.NewEndpointWithTTL("create-test1.hack.me", "A", endpoint.TTL(600), "11.12.13.14"),
				endpoint.NewEndpointWithTTL("clbr2.hack.me", "CNAME", endpoint.TTL(600), "elb3.aws.com"),
				endpoint.NewEndpointWithTTL("hack.me", "CNAME", endpoint.TTL(600), "apex.aws.com"),
			},
			UpdateNew: []*endpoint.Endpoint{
				endpoint.NewEndpointWithTTL("lbr.hack.me", "A", endpoint.TTL(600), "132.56.180.35"),
			},
			UpdateOld: []*endpoint.Endpoint{
				endpoint.NewEndpointWithTTL("lbr.hack.me", "A", endpoint.TTL(600), "13.56.180.35"),
			},
			Delete: []*endpoint.Endpoint{
				endpoint.NewEndpoint("create-test2.hack.me", endpoint.RecordTypeA, "5.3.2.1"),
			},
		}, []*endpoint.Endpoint{
			endpoint.NewEndpointWithTTL("lbr.hack.me", "A", endpoint.TTL(600), "132.56.180.35"),
			endpoint.NewEndpointWithTTL("clbr.hack.me", "CNAME", endpoint.TTL(600), "elb2.aws.com"),
			endpoint.NewEndpointWithTTL("clbr2.hack.me", "CNAME", endpoint.TTL(600), "elb3.aws.com"),
			endpoint.NewEndpointWithTTL("create-test1.hack.me", "A", endpoint.TTL(600), "11.12.13.14"),
			endpoint.NewEndpointWithTTL("hack.me", "CNAME", endpoint.TTL(600), "apex.aws.com"),
		}},
		{"delete all", &plan.Changes{
			Create:    []*endpoint.Endpoint{},
			UpdateNew: []*endpoint.Endpoint{},
			UpdateOld: []*endpoint.Endpoint{},
			Delete: []*endpoint.Endpoint{
				endpoint.NewEndpointWithTTL("lbr.hack.me", "A", endpoint.TTL(600), "132.56.180.35"),
				endpoint.NewEndpointWithTTL("clbr.hack.me", "CNAME", endpoint.TTL(600), "elb2.aws.com"),
				endpoint.NewEndpointWithTTL("clbr2.hack.me", "CNAME", endpoint.TTL(600), "elb3.aws.com"),
				endpoint.NewEndpointWithTTL("create-test1.hack.me", "A", endpoint.TTL(600), "11.12.13.14"),
				endpoint.NewEndpointWithTTL("hack.me", "CNAME", endpoint.TTL(600), "apex.aws.com"),
			},
		}, []*endpoint.Endpoint{}},
		{"create new for 2 zones", &plan.Changes{
			Create: []*endpoint.Endpoint{
				endpoint.NewEndpointWithTTL("lbr.hack.me", "A", endpoint.TTL(600), "13.56.180.35"),
				endpoint.NewEndpointWithTTL("clbr.dev.example.com", "CNAME", endpoint.TTL(600), "elb.aws.com"),
			},
			UpdateNew: []*endpoint.Endpoint{},
			UpdateOld: []*endpoint.Endpoint{},
			Delete:    []*endpoint.Endpoint{},
		}, []*endpoint.Endpoint{
			endpoint.NewEndpointWithTTL("lbr.hack.me", "A", endpoint.TTL(600), "13.56.180.35"),
			endpoint.NewEndpointWithTTL("clbr.dev.example.com", "CNAME", endpoint.TTL(600), "elb.aws.com"),
		}},
		{"create AAAA record", &plan.Changes{
			Create: []*endpoint.Endpoint{
				endpoint.NewEndpointWithTTL("lbraaaa.hack.me", "AAAA", endpoint.TTL(600), "2604:e180:1021::ffff:6ba2:9e96"),
			},
			UpdateNew: []*endpoint.Endpoint{},
			UpdateOld: []*endpoint.Endpoint{},
			Delete:    []*endpoint.Endpoint{},
		}, []*endpoint.Endpoint{
			endpoint.NewEndpointWithTTL("lbraaaa.hack.me", "AAAA", endpoint.TTL(600), "2604:e180:1021::ffff:6ba2:9e96"),
			endpoint.NewEndpointWithTTL("lbr.hack.me", "A", endpoint.TTL(600), "13.56.180.35"),
			endpoint.NewEndpointWithTTL("clbr.dev.example.com", "CNAME", endpoint.TTL(600), "elb.aws.com"),
		}},
		{"plan with invalid and valid records", &plan.Changes{
			Create: []*endpoint.Endpoint{
				endpoint.NewEndpointWithTTL("lbr1.hack.me", "A", endpoint.TTL(600), "13.56.180.36"),
			},
			UpdateNew: []*endpoint.Endpoint{
				endpoint.NewEndpointWithTTL("lbr2.hack.me", "A", endpoint.TTL(600), "13.56.180.37"),
				endpoint.NewEndpointWithTTL("lbr2.hack.org", "A", endpoint.TTL(600), "13.56.180.37"),
			},
			UpdateOld: []*endpoint.Endpoint{
				endpoint.NewEndpointWithTTL("lbr2.hack.me", "A", endpoint.TTL(600), "13.56.180.36"),
			},
			Delete: []*endpoint.Endpoint{
				endpoint.NewEndpointWithTTL("deletelbraaaa.hack.me", "AAAA", endpoint.TTL(600), "2604:e180:1021::ffff:6ba2:9e96"),
				endpoint.NewEndpointWithTTL("deletelbraaaa.hack.prg", "AAAA", endpoint.TTL(600), "2604:e180:1021::ffff:6ba2:9e96"),
			},
		}, []*endpoint.Endpoint{
			endpoint.NewEndpointWithTTL("lbraaaa.hack.me", "AAAA", endpoint.TTL(600), "2604:e180:1021::ffff:6ba2:9e96"),
			endpoint.NewEndpointWithTTL("lbr.hack.me", "A", endpoint.TTL(600), "13.56.180.35"),
			endpoint.NewEndpointWithTTL("clbr.dev.example.com", "CNAME", endpoint.TTL(600), "elb.aws.com"),
			endpoint.NewEndpointWithTTL("lbr1.hack.me", "A", endpoint.TTL(600), "13.56.180.36"),
		}},
		{"cleanup all", &plan.Changes{
			Create:    []*endpoint.Endpoint{},
			UpdateNew: []*endpoint.Endpoint{},
			UpdateOld: []*endpoint.Endpoint{},
			Delete: []*endpoint.Endpoint{
				endpoint.NewEndpointWithTTL("lbraaaa.hack.me", "AAAA", endpoint.TTL(600), "2604:e180:1021::ffff:6ba2:9e96"),
				endpoint.NewEndpointWithTTL("lbr.hack.me", "A", endpoint.TTL(600), "13.56.180.35"),
				endpoint.NewEndpointWithTTL("clbr.dev.example.com", "CNAME", endpoint.TTL(600), "elb.aws.com"),
				endpoint.NewEndpointWithTTL("lbr1.hack.me", "A", endpoint.TTL(600), "13.56.180.36"),
			},
		}, []*endpoint.Endpoint{}},
		{"create new txt", &plan.Changes{
			Create: []*endpoint.Endpoint{
				endpoint.NewEndpointWithTTL("lbr.hack.me", "A", endpoint.TTL(600), "13.56.180.35"),
			},
			UpdateNew: []*endpoint.Endpoint{},
			UpdateOld: []*endpoint.Endpoint{},
			Delete:    []*endpoint.Endpoint{},
		}, []*endpoint.Endpoint{
			endpoint.NewEndpointWithTTL("lbr.hack.me", "A", endpoint.TTL(600), "13.56.180.35"),
		}},
		{"cleanup new txt", &plan.Changes{
			Create:    []*endpoint.Endpoint{},
			UpdateNew: []*endpoint.Endpoint{},
			UpdateOld: []*endpoint.Endpoint{},
			Delete: []*endpoint.Endpoint{
				endpoint.NewEndpointWithTTL("lbr.hack.me", "A", endpoint.TTL(600), "13.56.180.35"),
			},
		}, []*endpoint.Endpoint{}},
	}

	prdr := getProvider()
	for _, test := range tests {
		t.Logf("Testcase %s", test.name)
		err := prdr.ApplyChanges(context.Background(), test.changes)
		assert.NoError(t, err, "err %v", err)
		rec, err := prdr.Records(context.Background())
		assert.NoError(t, err)
		assert.Equal(t, len(test.expectedEndpoints), len(rec))
		t.Logf("Records %v", rec)
		assert.True(t, testutils.SameEndpoints(rec, test.expectedEndpoints))
	}
}

// getProvider to get either the provider with stub api or actual F5 api
func getProvider() *F5DNSProvider {
	if os.Getenv("API") == "" {
		cfg := &F5DNSConfig{
			DryRun: false,
			DomainFilter: endpoint.DomainFilter{
				Filters: []string{"hack.me", "dev.example.com"},
			},
			Auth: AuthConfig{
				Username: "test",
				Password: "test",
			},
			AccountID: "abcd-test",
		}
		zones := make(map[string]*factory.ServiceRequest)
		activated := make(map[string]bool)
		prdr := newMockF5Provider(cfg, zones, activated)
		return prdr
	}
	cfg := &F5DNSConfig{
		DryRun: false,
		DomainFilter: endpoint.DomainFilter{
			Filters: []string{"hack.me", "dev.example.com"},
		},
		Auth: AuthConfig{
			Username: os.Getenv("F5_USERNAME"),
			Password: os.Getenv("F5_PASSWORD"),
		},
		AccountID: os.Getenv("F5_ACCOUNT"),
	}
	prdr, _ := NewF5DNSProvider(cfg, "")
	return prdr
}

var serviceRequestJSON = `{
	"subscription_id": "s-aaVORdXTHP",
	"account_id": "a-aa4DHvEfZ_",
	"user_id": "u-aaHCrtHVT6",
	"catalog_id": "c-aaQnOrPjGu",
	"service_instance_id": "gslb-aa9qy8-6R8",
	"status": "DISABLED",
	"service_instance_name": "hack.me",
	"deleted": false,
	"service_type": "gslb",
	"configuration": {
		"gslb_service": {
			"load_balanced_records": {
				"lbr": {
					"aliases": ["lbr"],
					"display_name": "lbr",
					"enable": true,
					"persist_cidr_ipv4": 24,
					"persist_cidr_ipv6": 64,
					"persistence": false,
					"persistence_ttl": 3600,
					"proximity_rules": [{
						"pool": "lbrpool",
						"region": "global",
						"score": 100
					}],
					"remark": "\"heritage=external-dns,external-dns/owner=default,external-dns/resource=service/ns/testsvc1\"",
					"rr_type": "A"
				},
				"clbr": {
					"aliases": ["clbr"],
					"display_name": "clbr",
					"enable": true,
					"persist_cidr_ipv4": 24,
					"persist_cidr_ipv6": 64,
					"persistence": false,
					"persistence_ttl": 3600,
					"proximity_rules": [{
						"pool": "clbrpool",
						"region": "global",
						"score": 100
					}],
					"remark": "\"heritage=external-dns,external-dns/owner=default,external-dns/resource=service/ns/testsvc2\"",
					"rr_type": "CNAME"
				},
				"notextclbr": {
					"aliases": ["noextlbr"],
					"display_name": "notextclbr",
					"enable": true,
					"persist_cidr_ipv4": 24,
					"persist_cidr_ipv6": 64,
					"persistence": false,
					"persistence_ttl": 3600,
					"proximity_rules": [{
						"pool": "notextclbrpool",
						"region": "global",
						"score": 100
					}],
					"remark": "notextclbr",
					"rr_type": "CNAME"
				}
			},
			"pools": {
				"lbrpool": {
					"display_name": "lbrpool",
					"enable": true,
					"load_balancing_mode": "round-robin",
					"max_answers": 1,
					"members": [{
						"final": null,
						"monitor": "basic",
						"ratio": 80,
						"virtual_server": "lbr13_56_180_35"
					}],
					"rr_type": "A",
					"ttl": 600
				},
				"clbrpool": {
					"display_name": "clbrpool",
					"enable": true,
					"load_balancing_mode": "round-robin",
					"max_answers": 1,
					"members": [{
						"final": null,
						"domain": "elb.aws.com"
					}],
					"rr_type": "CNAME",
					"ttl": 600
				},
				"notextclbrpool": {
					"display_name": "notextclbrpool",
					"enable": true,
					"load_balancing_mode": "round-robin",
					"max_answers": 1,
					"members": [{
						"final": null,
						"domain": "elb2.aws.com"
					}],
					"rr_type": "CNAME",
					"ttl": 600
				}
			},
			"virtual_servers": {
				"lbr13_56_180_35": {
					"address": "13.56.180.35",
					"display_name": "lbr13_56_180_35",
					"monitor": "none",
					"port": 80,
					"remark": "lbr13_56_180_35"
				}
			},
			"zone": "hack.me"
		}
	}
}`

func newMockF5Provider(cfg *F5DNSConfig, zones map[string]*factory.ServiceRequest, activated map[string]bool) *F5DNSProvider {
	return &F5DNSProvider{
		config: cfg,
		client: &F5Client{
			subClientAPI:  NewF5DNSSubscriptionAPISub(zones, activated),
			authClientAPI: &F5DNSAuthenticationAPIStub{},
			f:             factory.NewFactory(),
			AccessToken:   "test",
		},
	}
}

func TestF5DNSRecords(t *testing.T) {
	cfg := &F5DNSConfig{
		DryRun: false,
		DomainFilter: endpoint.DomainFilter{
			Filters: []string{"hack.me"},
		},
		Auth: AuthConfig{
			Username: "test",
			Password: "test",
		},
		AccountID: "abcd-test",
	}
	f := factory.NewFactory(factory.Opts{JSONServiceConfig: serviceRequestJSON})
	zones := make(map[string]*factory.ServiceRequest)
	zones["hack.me"] = f.GetServiceRequest()
	def := f.ServiceRequestDefault()
	unknownZone := def.Configuration.GslbService.GetZone()
	zones[unknownZone] = &def
	g := factory.NewFactory(factory.Opts{JSONServiceConfig: serviceRequestJSON})
	// Existing subscription with zone not matching DomainFilter
	g.GetGslbService().SetZone("mydns.hack.me")
	zones["mydns.hack.me"] = g.GetServiceRequest()
	activated := map[string]bool{f.GetServiceRequest().SubscriptionId: true, def.SubscriptionId: true}
	prdr := newMockF5Provider(cfg, zones, activated)
	rec, err := prdr.Records(context.Background())
	tok := prdr.GetF5Client().AccessToken
	assert.NotEmpty(t, tok)
	assert.NoError(t, err)
	assert.NotNil(t, rec)
	assert.Equal(t, 2, len(rec))
	expected := []*endpoint.Endpoint{
		endpoint.NewEndpointWithTTL("lbr.hack.me", "A", endpoint.TTL(600), "13.56.180.35"),
		endpoint.NewEndpointWithTTL("clbr.hack.me", "CNAME", endpoint.TTL(600), "elb.aws.com"),
	}
	expected[0].Labels, err = endpoint.NewLabelsFromString("\"heritage=external-dns,external-dns/owner=default,external-dns/resource=service/ns/testsvc1\"")
	expected[1].Labels, err = endpoint.NewLabelsFromString("\"heritage=external-dns,external-dns/owner=default,external-dns/resource=service/ns/testsvc2\"")
	assert.NoError(t, err)
	assert.True(t, testutils.SameEndpoints(rec, expected))
	sr := zones["hack.me"]
	// Update sr with new lbr which is not managed by externald-dns
	sr.Configuration.GslbService.AddLoadBalancedRecord("newunmanagedlbr", factory.LoadBalancedRecord{
		Enable:      f.NewBoolPointer(true),
		DisplayName: f.NewStringPointer("newunmanagedlbr"),
		RRType:      f.NewStringPointer("CNAME"),
		Aliases:     []string{"newunmanagedlbr"},
		Persistence: f.NewBoolPointer(false),
		ProximityRules: []factory.ProximityRule{{
			Region: f.NewStringPointer("global"),
			Pool:   f.NewStringPointer("newunmanagedlbrpool"),
			Score:  f.NewIntPointer(100),
		}},
		Remark: f.NewStringPointer("unmanagedlbr"),
	})
	sr.Configuration.GslbService.AddPool("newunmanagedlbrpool", factory.Pool{
		RRType:      "CNAME",
		DisplayName: f.NewStringPointer("newunmanagedlbrpool"),
		Enable:      f.NewBoolPointer(true),
		TTL:         f.NewIntPointer(60),
		Members: []factory.Member{
			{
				Domain: f.NewStringPointer("elb4.aws.com"),
				Final:  f.NewBoolPointer(true),
			},
		},
	})
	newTok := prdr.GetF5Client().AccessToken
	assert.Equal(t, tok, newTok)
	rec, err = prdr.Records(context.Background())
	newTok = prdr.GetF5Client().AccessToken
	assert.NoError(t, err)
	assert.NotNil(t, rec)
	assert.Equal(t, 2, len(rec))
	assert.NotEqual(t, tok, newTok)
}

func TestF5DNSNegativeCases(t *testing.T) {
	// Login failure
	cfg := &F5DNSConfig{
		DryRun: false,
		DomainFilter: endpoint.DomainFilter{
			Filters: []string{"hack.me"},
		},
		Auth: AuthConfig{
			Username: "failtest",
			Password: "test",
		},
		AccountID: "abcd-test",
	}
	zones := make(map[string]*factory.ServiceRequest)
	activated := make(map[string]bool)
	prdr := newMockF5Provider(cfg, zones, activated)
	_, err := prdr.Records(context.Background())
	assert.Error(t, err)
	err = prdr.ApplyChanges(context.Background(), &plan.Changes{
		Create: []*endpoint.Endpoint{
			endpoint.NewEndpointWithTTL("lbr.hack.me", "A", endpoint.TTL(600), "13.56.180.35"),
			endpoint.NewEndpointWithTTL("clbr.hack.me", "CNAME", endpoint.TTL(600), "elb.aws.com"),
		},
		UpdateNew: []*endpoint.Endpoint{},
		UpdateOld: []*endpoint.Endpoint{},
		Delete:    []*endpoint.Endpoint{},
	})
	assert.Error(t, err)
	// Invalid token failure
	cfg = &F5DNSConfig{
		DryRun: false,
		DomainFilter: endpoint.DomainFilter{
			Filters: []string{"hack.me"},
		},
		Auth: AuthConfig{
			Username: "tokentest",
			Password: "test",
		},
		AccountID: "abcd-test",
	}
	prdr = newMockF5Provider(cfg, zones, activated)
	_, err = prdr.Records(context.Background())
	assert.Error(t, err)
	// BatchActivate and BatchRetire failure
	cfg = &F5DNSConfig{
		DryRun: false,
		DomainFilter: endpoint.DomainFilter{
			Filters: []string{"hack.me"},
		},
		Auth: AuthConfig{
			Username: "failbatchactivatetest",
			Password: "test",
		},
		AccountID: "abcd-test",
	}
	prdr = newMockF5Provider(cfg, zones, activated)
	_, err = prdr.Records(context.Background())
	assert.NoError(t, err)
	err = prdr.ApplyChanges(context.Background(), &plan.Changes{
		Create: []*endpoint.Endpoint{
			endpoint.NewEndpointWithTTL("lbr.hack.me", "A", endpoint.TTL(600), "13.56.180.35"),
			endpoint.NewEndpointWithTTL("clbr.hack.me", "CNAME", endpoint.TTL(600), "elb.aws.com"),
		},
		UpdateNew: []*endpoint.Endpoint{},
		UpdateOld: []*endpoint.Endpoint{},
		Delete:    []*endpoint.Endpoint{},
	})
	assert.Error(t, err)
	cfg = &F5DNSConfig{
		DryRun: false,
		DomainFilter: endpoint.DomainFilter{
			Filters: []string{"hack.me"},
		},
		Auth: AuthConfig{
			Username: "failbatchretiretest",
			Password: "test",
		},
		AccountID: "abcd-test",
	}
	prdr = newMockF5Provider(cfg, zones, activated)
	_, err = prdr.Records(context.Background())
	assert.NoError(t, err)
	err = prdr.ApplyChanges(context.Background(), &plan.Changes{
		Create: []*endpoint.Endpoint{
			endpoint.NewEndpointWithTTL("lbr.hack.me", "A", endpoint.TTL(600), "13.56.180.35"),
			endpoint.NewEndpointWithTTL("clbr.hack.me", "CNAME", endpoint.TTL(600), "elb.aws.com"),
		},
		UpdateNew: []*endpoint.Endpoint{},
		UpdateOld: []*endpoint.Endpoint{},
		Delete:    []*endpoint.Endpoint{},
	})
	assert.NoError(t, err)
	err = prdr.ApplyChanges(context.Background(), &plan.Changes{
		Create:    []*endpoint.Endpoint{},
		UpdateNew: []*endpoint.Endpoint{},
		UpdateOld: []*endpoint.Endpoint{},
		Delete: []*endpoint.Endpoint{
			endpoint.NewEndpointWithTTL("lbr.hack.me", "A", endpoint.TTL(600), "13.56.180.35"),
			endpoint.NewEndpointWithTTL("clbr.hack.me", "CNAME", endpoint.TTL(600), "elb.aws.com"),
		},
	})
	assert.Error(t, err)
	// TODO
	// Existing subscription with empty alias
	// Existing subscription (not activated) with missing virtual
	// Existing subscription with adns type
	// Invalid JSON failure
}

func RandomString(n int) string {
	var letters = []rune("abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789")

	s := make([]rune, n)
	for i := range s {
		s[i] = letters[rand.Intn(len(letters))]
	}
	return string(s)
}
