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

package aws

import (
	"context"
	"fmt"
	"net"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/request"
	"github.com/aws/aws-sdk-go/service/route53"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"sigs.k8s.io/external-dns/endpoint"
	"sigs.k8s.io/external-dns/internal/testutils"
	"sigs.k8s.io/external-dns/plan"
	"sigs.k8s.io/external-dns/provider"
)

const (
	defaultBatchChangeSize      = 4000
	defaultBatchChangeInterval  = time.Second
	defaultEvaluateTargetHealth = true
)

// Compile time check for interface conformance
var _ Route53API = &Route53APIStub{}

// Route53APIStub is a minimal implementation of Route53API, used primarily for unit testing.
// See http://http://docs.aws.amazon.com/sdk-for-go/api/service/route53.html for descriptions
// of all of its methods.
// mostly taken from: https://github.com/kubernetes/kubernetes/blob/853167624edb6bc0cfdcdfb88e746e178f5db36c/federation/pkg/dnsprovider/providers/aws/route53/stubs/route53api.go
type Route53APIStub struct {
	zones      map[string]*route53.HostedZone
	recordSets map[string]map[string][]*route53.ResourceRecordSet
	zoneTags   map[string][]*route53.Tag
	m          dynamicMock
}

// MockMethod starts a description of an expectation of the specified method
// being called.
//
//     Route53APIStub.MockMethod("MyMethod", arg1, arg2)
func (r *Route53APIStub) MockMethod(method string, args ...interface{}) *mock.Call {
	return r.m.On(method, args...)
}

// NewRoute53APIStub returns an initialized Route53APIStub
func NewRoute53APIStub() *Route53APIStub {
	return &Route53APIStub{
		zones:      make(map[string]*route53.HostedZone),
		recordSets: make(map[string]map[string][]*route53.ResourceRecordSet),
		zoneTags:   make(map[string][]*route53.Tag),
	}
}

func (r *Route53APIStub) ListResourceRecordSetsPagesWithContext(ctx context.Context, input *route53.ListResourceRecordSetsInput, fn func(p *route53.ListResourceRecordSetsOutput, lastPage bool) (shouldContinue bool), opts ...request.Option) error {
	output := route53.ListResourceRecordSetsOutput{} // TODO: Support optional input args.
	if len(r.recordSets) == 0 {
		output.ResourceRecordSets = []*route53.ResourceRecordSet{}
	} else if _, ok := r.recordSets[aws.StringValue(input.HostedZoneId)]; !ok {
		output.ResourceRecordSets = []*route53.ResourceRecordSet{}
	} else {
		for _, rrsets := range r.recordSets[aws.StringValue(input.HostedZoneId)] {
			output.ResourceRecordSets = append(output.ResourceRecordSets, rrsets...)
		}
	}
	lastPage := true
	fn(&output, lastPage)
	return nil
}

type Route53APICounter struct {
	wrapped Route53API
	calls   map[string]int
}

func NewRoute53APICounter(w Route53API) *Route53APICounter {
	return &Route53APICounter{
		wrapped: w,
		calls:   map[string]int{},
	}
}

func (c *Route53APICounter) ListResourceRecordSetsPagesWithContext(ctx context.Context, input *route53.ListResourceRecordSetsInput, fn func(resp *route53.ListResourceRecordSetsOutput, lastPage bool) (shouldContinue bool), opts ...request.Option) error {
	c.calls["ListResourceRecordSetsPages"]++
	return c.wrapped.ListResourceRecordSetsPagesWithContext(ctx, input, fn)
}

func (c *Route53APICounter) ChangeResourceRecordSetsWithContext(ctx context.Context, input *route53.ChangeResourceRecordSetsInput, opts ...request.Option) (*route53.ChangeResourceRecordSetsOutput, error) {
	c.calls["ChangeResourceRecordSets"]++
	return c.wrapped.ChangeResourceRecordSetsWithContext(ctx, input)
}

func (c *Route53APICounter) CreateHostedZoneWithContext(ctx context.Context, input *route53.CreateHostedZoneInput, opts ...request.Option) (*route53.CreateHostedZoneOutput, error) {
	c.calls["CreateHostedZone"]++
	return c.wrapped.CreateHostedZoneWithContext(ctx, input)
}

func (c *Route53APICounter) ListHostedZonesPagesWithContext(ctx context.Context, input *route53.ListHostedZonesInput, fn func(resp *route53.ListHostedZonesOutput, lastPage bool) (shouldContinue bool), opts ...request.Option) error {
	c.calls["ListHostedZonesPages"]++
	return c.wrapped.ListHostedZonesPagesWithContext(ctx, input, fn)
}

func (c *Route53APICounter) ListTagsForResourceWithContext(ctx context.Context, input *route53.ListTagsForResourceInput, opts ...request.Option) (*route53.ListTagsForResourceOutput, error) {
	c.calls["ListTagsForResource"]++
	return c.wrapped.ListTagsForResourceWithContext(ctx, input)
}

// Route53 stores wildcards escaped: http://docs.aws.amazon.com/Route53/latest/DeveloperGuide/DomainNameFormat.html?shortFooter=true#domain-name-format-asterisk
func wildcardEscape(s string) string {
	if strings.Contains(s, "*") {
		s = strings.Replace(s, "*", "\\052", 1)
	}
	return s
}

func (r *Route53APIStub) ListTagsForResourceWithContext(ctx context.Context, input *route53.ListTagsForResourceInput, opts ...request.Option) (*route53.ListTagsForResourceOutput, error) {
	if aws.StringValue(input.ResourceType) == "hostedzone" {
		tags := r.zoneTags[aws.StringValue(input.ResourceId)]
		return &route53.ListTagsForResourceOutput{
			ResourceTagSet: &route53.ResourceTagSet{
				ResourceId:   input.ResourceId,
				ResourceType: input.ResourceType,
				Tags:         tags,
			},
		}, nil
	}
	return &route53.ListTagsForResourceOutput{}, nil
}

func (r *Route53APIStub) ChangeResourceRecordSetsWithContext(ctx context.Context, input *route53.ChangeResourceRecordSetsInput, opts ...request.Option) (*route53.ChangeResourceRecordSetsOutput, error) {
	if r.m.isMocked("ChangeResourceRecordSets", input) {
		return r.m.ChangeResourceRecordSets(input)
	}

	_, ok := r.zones[aws.StringValue(input.HostedZoneId)]
	if !ok {
		return nil, fmt.Errorf("Hosted zone doesn't exist: %s", aws.StringValue(input.HostedZoneId))
	}

	if len(input.ChangeBatch.Changes) == 0 {
		return nil, fmt.Errorf("ChangeBatch doesn't contain any changes")
	}

	output := &route53.ChangeResourceRecordSetsOutput{}
	recordSets, ok := r.recordSets[aws.StringValue(input.HostedZoneId)]
	if !ok {
		recordSets = make(map[string][]*route53.ResourceRecordSet)
	}

	for _, change := range input.ChangeBatch.Changes {
		if aws.StringValue(change.ResourceRecordSet.Type) == route53.RRTypeA {
			for _, rrs := range change.ResourceRecordSet.ResourceRecords {
				if net.ParseIP(aws.StringValue(rrs.Value)) == nil {
					return nil, fmt.Errorf("A records must point to IPs")
				}
			}
		}

		change.ResourceRecordSet.Name = aws.String(wildcardEscape(provider.EnsureTrailingDot(aws.StringValue(change.ResourceRecordSet.Name))))

		if change.ResourceRecordSet.AliasTarget != nil {
			change.ResourceRecordSet.AliasTarget.DNSName = aws.String(wildcardEscape(provider.EnsureTrailingDot(aws.StringValue(change.ResourceRecordSet.AliasTarget.DNSName))))
		}

		setID := ""
		if change.ResourceRecordSet.SetIdentifier != nil {
			setID = aws.StringValue(change.ResourceRecordSet.SetIdentifier)
		}
		key := aws.StringValue(change.ResourceRecordSet.Name) + "::" + aws.StringValue(change.ResourceRecordSet.Type) + "::" + setID
		switch aws.StringValue(change.Action) {
		case route53.ChangeActionCreate:
			if _, found := recordSets[key]; found {
				return nil, fmt.Errorf("Attempt to create duplicate rrset %s", key) // TODO: Return AWS errors with codes etc
			}
			recordSets[key] = append(recordSets[key], change.ResourceRecordSet)
		case route53.ChangeActionDelete:
			if _, found := recordSets[key]; !found {
				return nil, fmt.Errorf("Attempt to delete non-existent rrset %s", key) // TODO: Check other fields too
			}
			delete(recordSets, key)
		case route53.ChangeActionUpsert:
			recordSets[key] = []*route53.ResourceRecordSet{change.ResourceRecordSet}
		}
	}
	r.recordSets[aws.StringValue(input.HostedZoneId)] = recordSets
	return output, nil // TODO: We should ideally return status etc, but we don't' use that yet.
}

func (r *Route53APIStub) ListHostedZonesPagesWithContext(ctx context.Context, input *route53.ListHostedZonesInput, fn func(p *route53.ListHostedZonesOutput, lastPage bool) (shouldContinue bool), opts ...request.Option) error {
	output := &route53.ListHostedZonesOutput{}
	for _, zone := range r.zones {
		output.HostedZones = append(output.HostedZones, zone)
	}
	lastPage := true
	fn(output, lastPage)
	return nil
}

func (r *Route53APIStub) CreateHostedZoneWithContext(ctx context.Context, input *route53.CreateHostedZoneInput, opts ...request.Option) (*route53.CreateHostedZoneOutput, error) {
	name := aws.StringValue(input.Name)
	id := "/hostedzone/" + name
	if _, ok := r.zones[id]; ok {
		return nil, fmt.Errorf("Error creating hosted DNS zone: %s already exists", id)
	}
	r.zones[id] = &route53.HostedZone{
		Id:     aws.String(id),
		Name:   aws.String(name),
		Config: input.HostedZoneConfig,
	}
	return &route53.CreateHostedZoneOutput{HostedZone: r.zones[id]}, nil
}

type dynamicMock struct {
	mock.Mock
}

func (m *dynamicMock) ChangeResourceRecordSets(input *route53.ChangeResourceRecordSetsInput) (*route53.ChangeResourceRecordSetsOutput, error) {
	args := m.Called(input)
	if args.Get(0) != nil {
		return args.Get(0).(*route53.ChangeResourceRecordSetsOutput), args.Error(1)
	}
	return nil, args.Error(1)
}

func (m *dynamicMock) isMocked(method string, arguments ...interface{}) bool {
	for _, call := range m.ExpectedCalls {
		if call.Method == method && call.Repeatability > -1 {
			_, diffCount := call.Arguments.Diff(arguments)
			if diffCount == 0 {
				return true
			}
		}
	}
	return false
}

func TestAWSZones(t *testing.T) {
	publicZones := map[string]*route53.HostedZone{
		"/hostedzone/zone-1.ext-dns-test-2.teapot.zalan.do.": {
			Id:   aws.String("/hostedzone/zone-1.ext-dns-test-2.teapot.zalan.do."),
			Name: aws.String("zone-1.ext-dns-test-2.teapot.zalan.do."),
		},
		"/hostedzone/zone-2.ext-dns-test-2.teapot.zalan.do.": {
			Id:   aws.String("/hostedzone/zone-2.ext-dns-test-2.teapot.zalan.do."),
			Name: aws.String("zone-2.ext-dns-test-2.teapot.zalan.do."),
		},
	}

	privateZones := map[string]*route53.HostedZone{
		"/hostedzone/zone-3.ext-dns-test-2.teapot.zalan.do.": {
			Id:   aws.String("/hostedzone/zone-3.ext-dns-test-2.teapot.zalan.do."),
			Name: aws.String("zone-3.ext-dns-test-2.teapot.zalan.do."),
		},
	}

	allZones := map[string]*route53.HostedZone{}
	for k, v := range publicZones {
		allZones[k] = v
	}
	for k, v := range privateZones {
		allZones[k] = v
	}

	noZones := map[string]*route53.HostedZone{}

	for _, ti := range []struct {
		msg            string
		zoneIDFilter   provider.ZoneIDFilter
		zoneTypeFilter provider.ZoneTypeFilter
		zoneTagFilter  provider.ZoneTagFilter
		expectedZones  map[string]*route53.HostedZone
	}{
		{"no filter", provider.NewZoneIDFilter([]string{}), provider.NewZoneTypeFilter(""), provider.NewZoneTagFilter([]string{}), allZones},
		{"public filter", provider.NewZoneIDFilter([]string{}), provider.NewZoneTypeFilter("public"), provider.NewZoneTagFilter([]string{}), publicZones},
		{"private filter", provider.NewZoneIDFilter([]string{}), provider.NewZoneTypeFilter("private"), provider.NewZoneTagFilter([]string{}), privateZones},
		{"unknown filter", provider.NewZoneIDFilter([]string{}), provider.NewZoneTypeFilter("unknown"), provider.NewZoneTagFilter([]string{}), noZones},
		{"zone id filter", provider.NewZoneIDFilter([]string{"/hostedzone/zone-3.ext-dns-test-2.teapot.zalan.do."}), provider.NewZoneTypeFilter(""), provider.NewZoneTagFilter([]string{}), privateZones},
		{"tag filter", provider.NewZoneIDFilter([]string{}), provider.NewZoneTypeFilter(""), provider.NewZoneTagFilter([]string{"zone=3"}), privateZones},
	} {
		provider, _ := newAWSProviderWithTagFilter(t, endpoint.NewDomainFilter([]string{"ext-dns-test-2.teapot.zalan.do."}), ti.zoneIDFilter, ti.zoneTypeFilter, ti.zoneTagFilter, defaultEvaluateTargetHealth, false, []*endpoint.Endpoint{})

		zones, err := provider.Zones(context.Background())
		require.NoError(t, err)

		validateAWSZones(t, zones, ti.expectedZones)
	}
}

func TestAWSRecords(t *testing.T) {
	provider, _ := newAWSProvider(t, endpoint.NewDomainFilter([]string{"ext-dns-test-2.teapot.zalan.do."}), provider.NewZoneIDFilter([]string{}), provider.NewZoneTypeFilter(""), false, false, []*endpoint.Endpoint{
		endpoint.NewEndpointWithTTL("list-test.zone-1.ext-dns-test-2.teapot.zalan.do", endpoint.RecordTypeA, endpoint.TTL(recordTTL), "1.2.3.4"),
		endpoint.NewEndpointWithTTL("list-test.zone-2.ext-dns-test-2.teapot.zalan.do", endpoint.RecordTypeA, endpoint.TTL(recordTTL), "8.8.8.8"),
		endpoint.NewEndpointWithTTL("*.wildcard-test.zone-2.ext-dns-test-2.teapot.zalan.do", endpoint.RecordTypeA, endpoint.TTL(recordTTL), "8.8.8.8"),
		endpoint.NewEndpoint("list-test-alias.zone-1.ext-dns-test-2.teapot.zalan.do", endpoint.RecordTypeCNAME, "foo.eu-central-1.elb.amazonaws.com").WithProviderSpecific(providerSpecificEvaluateTargetHealth, "false"),
		endpoint.NewEndpoint("*.wildcard-test-alias.zone-1.ext-dns-test-2.teapot.zalan.do", endpoint.RecordTypeCNAME, "foo.eu-central-1.elb.amazonaws.com").WithProviderSpecific(providerSpecificEvaluateTargetHealth, "false"),
		endpoint.NewEndpoint("list-test-alias-evaluate.zone-1.ext-dns-test-2.teapot.zalan.do", endpoint.RecordTypeCNAME, "foo.eu-central-1.elb.amazonaws.com").WithProviderSpecific(providerSpecificEvaluateTargetHealth, "true"),
		endpoint.NewEndpointWithTTL("list-test-multiple.zone-1.ext-dns-test-2.teapot.zalan.do", endpoint.RecordTypeA, endpoint.TTL(recordTTL), "8.8.8.8", "8.8.4.4"),
		endpoint.NewEndpointWithTTL("prefix-*.wildcard.zone-1.ext-dns-test-2.teapot.zalan.do", endpoint.RecordTypeTXT, endpoint.TTL(recordTTL), "random"),
		endpoint.NewEndpointWithTTL("weight-test.zone-1.ext-dns-test-2.teapot.zalan.do", endpoint.RecordTypeA, endpoint.TTL(recordTTL), "1.2.3.4").WithSetIdentifier("test-set-1").WithProviderSpecific(providerSpecificWeight, "10"),
		endpoint.NewEndpointWithTTL("weight-test.zone-1.ext-dns-test-2.teapot.zalan.do", endpoint.RecordTypeA, endpoint.TTL(recordTTL), "4.3.2.1").WithSetIdentifier("test-set-2").WithProviderSpecific(providerSpecificWeight, "20"),
		endpoint.NewEndpointWithTTL("latency-test.zone-1.ext-dns-test-2.teapot.zalan.do", endpoint.RecordTypeA, endpoint.TTL(recordTTL), "1.2.3.4").WithSetIdentifier("test-set").WithProviderSpecific(providerSpecificRegion, "us-east-1"),
		endpoint.NewEndpointWithTTL("failover-test.zone-1.ext-dns-test-2.teapot.zalan.do", endpoint.RecordTypeA, endpoint.TTL(recordTTL), "1.2.3.4").WithSetIdentifier("test-set").WithProviderSpecific(providerSpecificFailover, "PRIMARY"),
		endpoint.NewEndpointWithTTL("multi-value-answer-test.zone-1.ext-dns-test-2.teapot.zalan.do", endpoint.RecordTypeA, endpoint.TTL(recordTTL), "1.2.3.4").WithSetIdentifier("test-set").WithProviderSpecific(providerSpecificMultiValueAnswer, ""),
		endpoint.NewEndpointWithTTL("geolocation-test.zone-1.ext-dns-test-2.teapot.zalan.do", endpoint.RecordTypeA, endpoint.TTL(recordTTL), "1.2.3.4").WithSetIdentifier("test-set-1").WithProviderSpecific(providerSpecificGeolocationContinentCode, "EU"),
		endpoint.NewEndpointWithTTL("geolocation-test.zone-1.ext-dns-test-2.teapot.zalan.do", endpoint.RecordTypeA, endpoint.TTL(recordTTL), "4.3.2.1").WithSetIdentifier("test-set-2").WithProviderSpecific(providerSpecificGeolocationCountryCode, "DE"),
		endpoint.NewEndpoint("healthcheck-test.zone-1.ext-dns-test-2.teapot.zalan.do", endpoint.RecordTypeCNAME, "foo.example.com").WithSetIdentifier("test-set-1").WithProviderSpecific(providerSpecificWeight, "10").WithProviderSpecific(providerSpecificHealthCheckID, "foo-bar-healthcheck-id"),
		endpoint.NewEndpointWithTTL("healthcheck-test.zone-1.ext-dns-test-2.teapot.zalan.do", endpoint.RecordTypeA, endpoint.TTL(recordTTL), "4.3.2.1").WithSetIdentifier("test-set-2").WithProviderSpecific(providerSpecificWeight, "20").WithProviderSpecific(providerSpecificHealthCheckID, "abc-def-healthcheck-id"),
	})

	records, err := provider.Records(context.Background())
	require.NoError(t, err)

	validateEndpoints(t, records, []*endpoint.Endpoint{
		endpoint.NewEndpointWithTTL("list-test.zone-1.ext-dns-test-2.teapot.zalan.do", endpoint.RecordTypeA, endpoint.TTL(recordTTL), "1.2.3.4"),
		endpoint.NewEndpointWithTTL("list-test.zone-2.ext-dns-test-2.teapot.zalan.do", endpoint.RecordTypeA, endpoint.TTL(recordTTL), "8.8.8.8"),
		endpoint.NewEndpointWithTTL("*.wildcard-test.zone-2.ext-dns-test-2.teapot.zalan.do", endpoint.RecordTypeA, endpoint.TTL(recordTTL), "8.8.8.8"),
		endpoint.NewEndpointWithTTL("list-test-alias.zone-1.ext-dns-test-2.teapot.zalan.do", endpoint.RecordTypeCNAME, endpoint.TTL(recordTTL), "foo.eu-central-1.elb.amazonaws.com").WithProviderSpecific(providerSpecificEvaluateTargetHealth, "false"),
		endpoint.NewEndpointWithTTL("*.wildcard-test-alias.zone-1.ext-dns-test-2.teapot.zalan.do", endpoint.RecordTypeCNAME, endpoint.TTL(recordTTL), "foo.eu-central-1.elb.amazonaws.com").WithProviderSpecific(providerSpecificEvaluateTargetHealth, "false"),
		endpoint.NewEndpointWithTTL("list-test-alias-evaluate.zone-1.ext-dns-test-2.teapot.zalan.do", endpoint.RecordTypeCNAME, endpoint.TTL(recordTTL), "foo.eu-central-1.elb.amazonaws.com").WithProviderSpecific(providerSpecificEvaluateTargetHealth, "true"),
		endpoint.NewEndpointWithTTL("list-test-multiple.zone-1.ext-dns-test-2.teapot.zalan.do", endpoint.RecordTypeA, endpoint.TTL(recordTTL), "8.8.8.8", "8.8.4.4"),
		endpoint.NewEndpointWithTTL("prefix-*.wildcard.zone-1.ext-dns-test-2.teapot.zalan.do", endpoint.RecordTypeTXT, endpoint.TTL(recordTTL), "random"),
		endpoint.NewEndpointWithTTL("weight-test.zone-1.ext-dns-test-2.teapot.zalan.do", endpoint.RecordTypeA, endpoint.TTL(recordTTL), "1.2.3.4").WithSetIdentifier("test-set-1").WithProviderSpecific(providerSpecificWeight, "10"),
		endpoint.NewEndpointWithTTL("weight-test.zone-1.ext-dns-test-2.teapot.zalan.do", endpoint.RecordTypeA, endpoint.TTL(recordTTL), "4.3.2.1").WithSetIdentifier("test-set-2").WithProviderSpecific(providerSpecificWeight, "20"),
		endpoint.NewEndpointWithTTL("latency-test.zone-1.ext-dns-test-2.teapot.zalan.do", endpoint.RecordTypeA, endpoint.TTL(recordTTL), "1.2.3.4").WithSetIdentifier("test-set").WithProviderSpecific(providerSpecificRegion, "us-east-1"),
		endpoint.NewEndpointWithTTL("failover-test.zone-1.ext-dns-test-2.teapot.zalan.do", endpoint.RecordTypeA, endpoint.TTL(recordTTL), "1.2.3.4").WithSetIdentifier("test-set").WithProviderSpecific(providerSpecificFailover, "PRIMARY"),
		endpoint.NewEndpointWithTTL("multi-value-answer-test.zone-1.ext-dns-test-2.teapot.zalan.do", endpoint.RecordTypeA, endpoint.TTL(recordTTL), "1.2.3.4").WithSetIdentifier("test-set").WithProviderSpecific(providerSpecificMultiValueAnswer, ""),
		endpoint.NewEndpointWithTTL("geolocation-test.zone-1.ext-dns-test-2.teapot.zalan.do", endpoint.RecordTypeA, endpoint.TTL(recordTTL), "1.2.3.4").WithSetIdentifier("test-set-1").WithProviderSpecific(providerSpecificGeolocationContinentCode, "EU"),
		endpoint.NewEndpointWithTTL("geolocation-test.zone-1.ext-dns-test-2.teapot.zalan.do", endpoint.RecordTypeA, endpoint.TTL(recordTTL), "4.3.2.1").WithSetIdentifier("test-set-2").WithProviderSpecific(providerSpecificGeolocationCountryCode, "DE"),
		endpoint.NewEndpointWithTTL("healthcheck-test.zone-1.ext-dns-test-2.teapot.zalan.do", endpoint.RecordTypeCNAME, endpoint.TTL(recordTTL), "foo.example.com").WithSetIdentifier("test-set-1").WithProviderSpecific(providerSpecificWeight, "10").WithProviderSpecific(providerSpecificHealthCheckID, "foo-bar-healthcheck-id"),
		endpoint.NewEndpointWithTTL("healthcheck-test.zone-1.ext-dns-test-2.teapot.zalan.do", endpoint.RecordTypeA, endpoint.TTL(recordTTL), "4.3.2.1").WithSetIdentifier("test-set-2").WithProviderSpecific(providerSpecificWeight, "20").WithProviderSpecific(providerSpecificHealthCheckID, "abc-def-healthcheck-id"),
	})
}

func TestAWSCreateRecords(t *testing.T) {
	customTTL := endpoint.TTL(60)
	provider, _ := newAWSProvider(t, endpoint.NewDomainFilter([]string{"ext-dns-test-2.teapot.zalan.do."}), provider.NewZoneIDFilter([]string{}), provider.NewZoneTypeFilter(""), defaultEvaluateTargetHealth, false, []*endpoint.Endpoint{})

	records := []*endpoint.Endpoint{
		endpoint.NewEndpoint("create-test.zone-1.ext-dns-test-2.teapot.zalan.do", endpoint.RecordTypeA, "1.2.3.4"),
		endpoint.NewEndpoint("create-test.zone-2.ext-dns-test-2.teapot.zalan.do", endpoint.RecordTypeA, "8.8.8.8"),
		endpoint.NewEndpointWithTTL("create-test-cname-custom-ttl.zone-1.ext-dns-test-2.teapot.zalan.do", endpoint.RecordTypeA, customTTL, "172.17.0.1"),
		endpoint.NewEndpoint("create-test-cname.zone-1.ext-dns-test-2.teapot.zalan.do", endpoint.RecordTypeCNAME, "foo.elb.amazonaws.com"),
		endpoint.NewEndpoint("create-test-multiple.zone-1.ext-dns-test-2.teapot.zalan.do", endpoint.RecordTypeA, "8.8.8.8", "8.8.4.4"),
	}

	require.NoError(t, provider.CreateRecords(context.Background(), records))

	records, err := provider.Records(context.Background())
	require.NoError(t, err)

	validateEndpoints(t, records, []*endpoint.Endpoint{
		endpoint.NewEndpointWithTTL("create-test.zone-1.ext-dns-test-2.teapot.zalan.do", endpoint.RecordTypeA, endpoint.TTL(recordTTL), "1.2.3.4"),
		endpoint.NewEndpointWithTTL("create-test.zone-2.ext-dns-test-2.teapot.zalan.do", endpoint.RecordTypeA, endpoint.TTL(recordTTL), "8.8.8.8"),
		endpoint.NewEndpointWithTTL("create-test-cname-custom-ttl.zone-1.ext-dns-test-2.teapot.zalan.do", endpoint.RecordTypeA, customTTL, "172.17.0.1"),
		endpoint.NewEndpointWithTTL("create-test-cname.zone-1.ext-dns-test-2.teapot.zalan.do", endpoint.RecordTypeCNAME, endpoint.TTL(recordTTL), "foo.elb.amazonaws.com"),
		endpoint.NewEndpointWithTTL("create-test-multiple.zone-1.ext-dns-test-2.teapot.zalan.do", endpoint.RecordTypeA, endpoint.TTL(recordTTL), "8.8.8.8", "8.8.4.4"),
	})
}

func TestAWSUpdateRecords(t *testing.T) {
	provider, _ := newAWSProvider(t, endpoint.NewDomainFilter([]string{"ext-dns-test-2.teapot.zalan.do."}), provider.NewZoneIDFilter([]string{}), provider.NewZoneTypeFilter(""), defaultEvaluateTargetHealth, false, []*endpoint.Endpoint{
		endpoint.NewEndpointWithTTL("update-test.zone-1.ext-dns-test-2.teapot.zalan.do", endpoint.RecordTypeA, endpoint.TTL(recordTTL), "8.8.8.8"),
		endpoint.NewEndpointWithTTL("update-test.zone-2.ext-dns-test-2.teapot.zalan.do", endpoint.RecordTypeA, endpoint.TTL(recordTTL), "8.8.4.4"),
		endpoint.NewEndpointWithTTL("update-test-a-to-cname.zone-1.ext-dns-test-2.teapot.zalan.do", endpoint.RecordTypeA, endpoint.TTL(recordTTL), "1.1.1.1"),
		endpoint.NewEndpointWithTTL("update-test-cname.zone-1.ext-dns-test-2.teapot.zalan.do", endpoint.RecordTypeCNAME, endpoint.TTL(recordTTL), "foo.elb.amazonaws.com"),
		endpoint.NewEndpointWithTTL("create-test-multiple.zone-1.ext-dns-test-2.teapot.zalan.do", endpoint.RecordTypeA, endpoint.TTL(recordTTL), "8.8.8.8", "8.8.4.4"),
	})

	currentRecords := []*endpoint.Endpoint{
		endpoint.NewEndpoint("update-test.zone-1.ext-dns-test-2.teapot.zalan.do", endpoint.RecordTypeA, "8.8.8.8"),
		endpoint.NewEndpoint("update-test.zone-2.ext-dns-test-2.teapot.zalan.do", endpoint.RecordTypeA, "8.8.4.4"),
		endpoint.NewEndpoint("update-test-a-to-cname.zone-1.ext-dns-test-2.teapot.zalan.do", endpoint.RecordTypeA, "1.1.1.1"),
		endpoint.NewEndpoint("update-test-cname.zone-1.ext-dns-test-2.teapot.zalan.do", endpoint.RecordTypeCNAME, "foo.elb.amazonaws.com"),
		endpoint.NewEndpoint("create-test-multiple.zone-1.ext-dns-test-2.teapot.zalan.do", endpoint.RecordTypeA, "8.8.8.8", "8.8.4.4"),
	}
	updatedRecords := []*endpoint.Endpoint{
		endpoint.NewEndpoint("update-test.zone-1.ext-dns-test-2.teapot.zalan.do", endpoint.RecordTypeA, "1.2.3.4"),
		endpoint.NewEndpoint("update-test.zone-2.ext-dns-test-2.teapot.zalan.do", endpoint.RecordTypeA, "4.3.2.1"),
		endpoint.NewEndpoint("update-test-a-to-cname.zone-1.ext-dns-test-2.teapot.zalan.do", endpoint.RecordTypeCNAME, "foo.elb.amazonaws.com"),
		endpoint.NewEndpoint("update-test-cname.zone-1.ext-dns-test-2.teapot.zalan.do", endpoint.RecordTypeCNAME, "bar.elb.amazonaws.com"),
		endpoint.NewEndpoint("create-test-multiple.zone-1.ext-dns-test-2.teapot.zalan.do", endpoint.RecordTypeA, "1.2.3.4", "4.3.2.1"),
	}

	require.NoError(t, provider.UpdateRecords(context.Background(), updatedRecords, currentRecords))

	records, err := provider.Records(context.Background())
	require.NoError(t, err)

	validateEndpoints(t, records, []*endpoint.Endpoint{
		endpoint.NewEndpointWithTTL("update-test.zone-1.ext-dns-test-2.teapot.zalan.do", endpoint.RecordTypeA, endpoint.TTL(recordTTL), "1.2.3.4"),
		endpoint.NewEndpointWithTTL("update-test.zone-2.ext-dns-test-2.teapot.zalan.do", endpoint.RecordTypeA, endpoint.TTL(recordTTL), "4.3.2.1"),
		endpoint.NewEndpointWithTTL("update-test-a-to-cname.zone-1.ext-dns-test-2.teapot.zalan.do", endpoint.RecordTypeCNAME, endpoint.TTL(recordTTL), "foo.elb.amazonaws.com"),
		endpoint.NewEndpointWithTTL("update-test-cname.zone-1.ext-dns-test-2.teapot.zalan.do", endpoint.RecordTypeCNAME, endpoint.TTL(recordTTL), "bar.elb.amazonaws.com"),
		endpoint.NewEndpointWithTTL("create-test-multiple.zone-1.ext-dns-test-2.teapot.zalan.do", endpoint.RecordTypeA, endpoint.TTL(recordTTL), "1.2.3.4", "4.3.2.1"),
	})
}

func TestAWSDeleteRecords(t *testing.T) {
	originalEndpoints := []*endpoint.Endpoint{
		endpoint.NewEndpointWithTTL("delete-test.zone-1.ext-dns-test-2.teapot.zalan.do", endpoint.RecordTypeA, endpoint.TTL(recordTTL), "1.2.3.4"),
		endpoint.NewEndpointWithTTL("delete-test.zone-2.ext-dns-test-2.teapot.zalan.do", endpoint.RecordTypeA, endpoint.TTL(recordTTL), "8.8.8.8"),
		endpoint.NewEndpointWithTTL("delete-test-cname.zone-1.ext-dns-test-2.teapot.zalan.do", endpoint.RecordTypeCNAME, endpoint.TTL(recordTTL), "baz.elb.amazonaws.com"),
		endpoint.NewEndpoint("delete-test-cname.zone-1.ext-dns-test-2.teapot.zalan.do", endpoint.RecordTypeCNAME, "foo.eu-central-1.elb.amazonaws.com").WithProviderSpecific(providerSpecificEvaluateTargetHealth, "false"),
		endpoint.NewEndpoint("delete-test-cname-alias.zone-1.ext-dns-test-2.teapot.zalan.do", endpoint.RecordTypeCNAME, "foo.eu-central-1.elb.amazonaws.com").WithProviderSpecific(providerSpecificEvaluateTargetHealth, "true"),
		endpoint.NewEndpointWithTTL("delete-test-multiple.zone-1.ext-dns-test-2.teapot.zalan.do", endpoint.RecordTypeA, endpoint.TTL(recordTTL), "8.8.8.8", "8.8.4.4"),
	}

	provider, _ := newAWSProvider(t, endpoint.NewDomainFilter([]string{"ext-dns-test-2.teapot.zalan.do."}), provider.NewZoneIDFilter([]string{}), provider.NewZoneTypeFilter(""), false, false, originalEndpoints)

	require.NoError(t, provider.DeleteRecords(context.Background(), originalEndpoints))

	records, err := provider.Records(context.Background())

	require.NoError(t, err)

	validateEndpoints(t, records, []*endpoint.Endpoint{})
}

func TestAWSApplyChanges(t *testing.T) {
	tests := []struct {
		name       string
		setup      func(p *AWSProvider) context.Context
		listRRSets int
	}{
		{"no cache", func(p *AWSProvider) context.Context { return context.Background() }, 3},
		{"cached", func(p *AWSProvider) context.Context {
			ctx := context.Background()
			records, err := p.Records(ctx)
			require.NoError(t, err)
			return context.WithValue(ctx, provider.RecordsContextKey, records)
		}, 0},
	}

	for _, tt := range tests {
		provider, _ := newAWSProvider(t, endpoint.NewDomainFilter([]string{"ext-dns-test-2.teapot.zalan.do."}), provider.NewZoneIDFilter([]string{}), provider.NewZoneTypeFilter(""), defaultEvaluateTargetHealth, false, []*endpoint.Endpoint{
			endpoint.NewEndpointWithTTL("update-test.zone-1.ext-dns-test-2.teapot.zalan.do", endpoint.RecordTypeA, endpoint.TTL(recordTTL), "8.8.8.8"),
			endpoint.NewEndpointWithTTL("delete-test.zone-1.ext-dns-test-2.teapot.zalan.do", endpoint.RecordTypeA, endpoint.TTL(recordTTL), "8.8.8.8"),
			endpoint.NewEndpointWithTTL("update-test.zone-2.ext-dns-test-2.teapot.zalan.do", endpoint.RecordTypeA, endpoint.TTL(recordTTL), "8.8.4.4"),
			endpoint.NewEndpointWithTTL("delete-test.zone-2.ext-dns-test-2.teapot.zalan.do", endpoint.RecordTypeA, endpoint.TTL(recordTTL), "8.8.4.4"),
			endpoint.NewEndpointWithTTL("update-test-a-to-cname.zone-1.ext-dns-test-2.teapot.zalan.do", endpoint.RecordTypeA, endpoint.TTL(recordTTL), "1.1.1.1"),
			endpoint.NewEndpointWithTTL("update-test-alias-to-cname.zone-1.ext-dns-test-2.teapot.zalan.do", endpoint.RecordTypeCNAME, endpoint.TTL(recordTTL), "foo.eu-central-1.elb.amazonaws.com"),
			endpoint.NewEndpointWithTTL("update-test-cname.zone-1.ext-dns-test-2.teapot.zalan.do", endpoint.RecordTypeCNAME, endpoint.TTL(recordTTL), "bar.elb.amazonaws.com"),
			endpoint.NewEndpointWithTTL("delete-test-cname.zone-1.ext-dns-test-2.teapot.zalan.do", endpoint.RecordTypeCNAME, endpoint.TTL(recordTTL), "qux.elb.amazonaws.com"),
			endpoint.NewEndpointWithTTL("update-test-cname-alias.zone-1.ext-dns-test-2.teapot.zalan.do", endpoint.RecordTypeCNAME, endpoint.TTL(recordTTL), "bar.elb.amazonaws.com"),
			endpoint.NewEndpointWithTTL("delete-test-cname-alias.zone-1.ext-dns-test-2.teapot.zalan.do", endpoint.RecordTypeCNAME, endpoint.TTL(recordTTL), "qux.elb.amazonaws.com"),
			endpoint.NewEndpointWithTTL("update-test-multiple.zone-2.ext-dns-test-2.teapot.zalan.do", endpoint.RecordTypeA, endpoint.TTL(recordTTL), "8.8.8.8", "8.8.4.4"),
			endpoint.NewEndpointWithTTL("delete-test-multiple.zone-2.ext-dns-test-2.teapot.zalan.do", endpoint.RecordTypeA, endpoint.TTL(recordTTL), "1.2.3.4", "4.3.2.1"),
		})

		createRecords := []*endpoint.Endpoint{
			endpoint.NewEndpoint("create-test.zone-1.ext-dns-test-2.teapot.zalan.do", endpoint.RecordTypeA, "8.8.8.8"),
			endpoint.NewEndpoint("create-test.zone-2.ext-dns-test-2.teapot.zalan.do", endpoint.RecordTypeA, "8.8.4.4"),
			endpoint.NewEndpoint("create-test-cname.zone-1.ext-dns-test-2.teapot.zalan.do", endpoint.RecordTypeCNAME, "foo.elb.amazonaws.com"),
			endpoint.NewEndpoint("create-test-cname-alias.zone-1.ext-dns-test-2.teapot.zalan.do", endpoint.RecordTypeCNAME, "foo.elb.amazonaws.com"),
			endpoint.NewEndpoint("create-test-multiple.zone-2.ext-dns-test-2.teapot.zalan.do", endpoint.RecordTypeA, "8.8.8.8", "8.8.4.4"),
		}

		currentRecords := []*endpoint.Endpoint{
			endpoint.NewEndpoint("update-test.zone-1.ext-dns-test-2.teapot.zalan.do", endpoint.RecordTypeA, "8.8.8.8"),
			endpoint.NewEndpoint("update-test.zone-2.ext-dns-test-2.teapot.zalan.do", endpoint.RecordTypeA, "8.8.4.4"),
			endpoint.NewEndpoint("update-test-a-to-cname.zone-1.ext-dns-test-2.teapot.zalan.do", endpoint.RecordTypeA, "1.1.1.1"),
			endpoint.NewEndpoint("update-test-alias-to-cname.zone-1.ext-dns-test-2.teapot.zalan.do", endpoint.RecordTypeCNAME, "foo.eu-central-1.elb.amazonaws.com"),
			endpoint.NewEndpoint("update-test-cname.zone-1.ext-dns-test-2.teapot.zalan.do", endpoint.RecordTypeCNAME, "bar.elb.amazonaws.com"),
			endpoint.NewEndpoint("update-test-cname-alias.zone-1.ext-dns-test-2.teapot.zalan.do", endpoint.RecordTypeCNAME, "bar.elb.amazonaws.com"),
			endpoint.NewEndpoint("update-test-multiple.zone-2.ext-dns-test-2.teapot.zalan.do", endpoint.RecordTypeA, "8.8.8.8", "8.8.4.4"),
		}
		updatedRecords := []*endpoint.Endpoint{
			endpoint.NewEndpoint("update-test.zone-1.ext-dns-test-2.teapot.zalan.do", endpoint.RecordTypeA, "1.2.3.4"),
			endpoint.NewEndpoint("update-test.zone-2.ext-dns-test-2.teapot.zalan.do", endpoint.RecordTypeA, "4.3.2.1"),
			endpoint.NewEndpoint("update-test-a-to-cname.zone-1.ext-dns-test-2.teapot.zalan.do", endpoint.RecordTypeCNAME, "foo.elb.amazonaws.com"),
			endpoint.NewEndpoint("update-test-alias-to-cname.zone-1.ext-dns-test-2.teapot.zalan.do", endpoint.RecordTypeCNAME, "my-internal-host.example.com"),
			endpoint.NewEndpoint("update-test-cname.zone-1.ext-dns-test-2.teapot.zalan.do", endpoint.RecordTypeCNAME, "baz.elb.amazonaws.com"),
			endpoint.NewEndpoint("update-test-cname-alias.zone-1.ext-dns-test-2.teapot.zalan.do", endpoint.RecordTypeCNAME, "baz.elb.amazonaws.com"),
			endpoint.NewEndpoint("update-test-multiple.zone-2.ext-dns-test-2.teapot.zalan.do", endpoint.RecordTypeA, "1.2.3.4", "4.3.2.1"),
		}

		deleteRecords := []*endpoint.Endpoint{
			endpoint.NewEndpoint("delete-test.zone-1.ext-dns-test-2.teapot.zalan.do", endpoint.RecordTypeA, "8.8.8.8"),
			endpoint.NewEndpoint("delete-test.zone-2.ext-dns-test-2.teapot.zalan.do", endpoint.RecordTypeA, "8.8.4.4"),
			endpoint.NewEndpoint("delete-test-cname.zone-1.ext-dns-test-2.teapot.zalan.do", endpoint.RecordTypeCNAME, "qux.elb.amazonaws.com"),
			endpoint.NewEndpoint("delete-test-cname-alias.zone-1.ext-dns-test-2.teapot.zalan.do", endpoint.RecordTypeCNAME, "qux.elb.amazonaws.com"),
			endpoint.NewEndpoint("delete-test-multiple.zone-2.ext-dns-test-2.teapot.zalan.do", endpoint.RecordTypeA, "1.2.3.4", "4.3.2.1"),
		}

		changes := &plan.Changes{
			Create:    createRecords,
			UpdateNew: updatedRecords,
			UpdateOld: currentRecords,
			Delete:    deleteRecords,
		}

		ctx := tt.setup(provider)

		provider.zonesCache = &zonesListCache{duration: 0 * time.Minute}
		counter := NewRoute53APICounter(provider.client)
		provider.client = counter
		require.NoError(t, provider.ApplyChanges(ctx, changes))

		assert.Equal(t, 1, counter.calls["ListHostedZonesPages"], tt.name)
		assert.Equal(t, tt.listRRSets, counter.calls["ListResourceRecordSetsPages"], tt.name)

		records, err := provider.Records(ctx)
		require.NoError(t, err, tt.name)

		validateEndpoints(t, records, []*endpoint.Endpoint{
			endpoint.NewEndpointWithTTL("create-test.zone-1.ext-dns-test-2.teapot.zalan.do", endpoint.RecordTypeA, endpoint.TTL(recordTTL), "8.8.8.8"),
			endpoint.NewEndpointWithTTL("update-test.zone-1.ext-dns-test-2.teapot.zalan.do", endpoint.RecordTypeA, endpoint.TTL(recordTTL), "1.2.3.4"),
			endpoint.NewEndpointWithTTL("create-test.zone-2.ext-dns-test-2.teapot.zalan.do", endpoint.RecordTypeA, endpoint.TTL(recordTTL), "8.8.4.4"),
			endpoint.NewEndpointWithTTL("update-test.zone-2.ext-dns-test-2.teapot.zalan.do", endpoint.RecordTypeA, endpoint.TTL(recordTTL), "4.3.2.1"),
			endpoint.NewEndpointWithTTL("update-test-a-to-cname.zone-1.ext-dns-test-2.teapot.zalan.do", endpoint.RecordTypeCNAME, endpoint.TTL(recordTTL), "foo.elb.amazonaws.com"),
			endpoint.NewEndpointWithTTL("update-test-alias-to-cname.zone-1.ext-dns-test-2.teapot.zalan.do", endpoint.RecordTypeCNAME, endpoint.TTL(recordTTL), "my-internal-host.example.com"),
			endpoint.NewEndpointWithTTL("create-test-cname.zone-1.ext-dns-test-2.teapot.zalan.do", endpoint.RecordTypeCNAME, endpoint.TTL(recordTTL), "foo.elb.amazonaws.com"),
			endpoint.NewEndpointWithTTL("update-test-cname.zone-1.ext-dns-test-2.teapot.zalan.do", endpoint.RecordTypeCNAME, endpoint.TTL(recordTTL), "baz.elb.amazonaws.com"),
			endpoint.NewEndpointWithTTL("create-test-cname-alias.zone-1.ext-dns-test-2.teapot.zalan.do", endpoint.RecordTypeCNAME, endpoint.TTL(recordTTL), "foo.elb.amazonaws.com"),
			endpoint.NewEndpointWithTTL("update-test-cname-alias.zone-1.ext-dns-test-2.teapot.zalan.do", endpoint.RecordTypeCNAME, endpoint.TTL(recordTTL), "baz.elb.amazonaws.com"),
			endpoint.NewEndpointWithTTL("create-test-multiple.zone-2.ext-dns-test-2.teapot.zalan.do", endpoint.RecordTypeA, endpoint.TTL(recordTTL), "8.8.8.8", "8.8.4.4"),
			endpoint.NewEndpointWithTTL("update-test-multiple.zone-2.ext-dns-test-2.teapot.zalan.do", endpoint.RecordTypeA, endpoint.TTL(recordTTL), "1.2.3.4", "4.3.2.1"),
		})
	}
}

func TestAWSApplyChangesDryRun(t *testing.T) {
	originalEndpoints := []*endpoint.Endpoint{
		endpoint.NewEndpointWithTTL("update-test.zone-1.ext-dns-test-2.teapot.zalan.do", endpoint.RecordTypeA, endpoint.TTL(recordTTL), "8.8.8.8"),
		endpoint.NewEndpointWithTTL("delete-test.zone-1.ext-dns-test-2.teapot.zalan.do", endpoint.RecordTypeA, endpoint.TTL(recordTTL), "8.8.8.8"),
		endpoint.NewEndpointWithTTL("update-test.zone-2.ext-dns-test-2.teapot.zalan.do", endpoint.RecordTypeA, endpoint.TTL(recordTTL), "8.8.4.4"),
		endpoint.NewEndpointWithTTL("delete-test.zone-2.ext-dns-test-2.teapot.zalan.do", endpoint.RecordTypeA, endpoint.TTL(recordTTL), "8.8.4.4"),
		endpoint.NewEndpointWithTTL("update-test-a-to-cname.zone-1.ext-dns-test-2.teapot.zalan.do", endpoint.RecordTypeA, endpoint.TTL(recordTTL), "1.1.1.1"),
		endpoint.NewEndpointWithTTL("update-test-cname.zone-1.ext-dns-test-2.teapot.zalan.do", endpoint.RecordTypeCNAME, endpoint.TTL(recordTTL), "bar.elb.amazonaws.com"),
		endpoint.NewEndpointWithTTL("delete-test-cname.zone-1.ext-dns-test-2.teapot.zalan.do", endpoint.RecordTypeCNAME, endpoint.TTL(recordTTL), "qux.elb.amazonaws.com"),
		endpoint.NewEndpointWithTTL("update-test-cname-alias.zone-1.ext-dns-test-2.teapot.zalan.do", endpoint.RecordTypeCNAME, endpoint.TTL(recordTTL), "bar.elb.amazonaws.com"),
		endpoint.NewEndpointWithTTL("delete-test-cname-alias.zone-1.ext-dns-test-2.teapot.zalan.do", endpoint.RecordTypeCNAME, endpoint.TTL(recordTTL), "qux.elb.amazonaws.com"),
		endpoint.NewEndpointWithTTL("update-test-multiple.zone-2.ext-dns-test-2.teapot.zalan.do", endpoint.RecordTypeA, endpoint.TTL(recordTTL), "8.8.8.8", "8.8.4.4"),
		endpoint.NewEndpointWithTTL("delete-test-multiple.zone-2.ext-dns-test-2.teapot.zalan.do", endpoint.RecordTypeA, endpoint.TTL(recordTTL), "1.2.3.4", "4.3.2.1"),
	}

	provider, _ := newAWSProvider(t, endpoint.NewDomainFilter([]string{"ext-dns-test-2.teapot.zalan.do."}), provider.NewZoneIDFilter([]string{}), provider.NewZoneTypeFilter(""), defaultEvaluateTargetHealth, true, originalEndpoints)

	createRecords := []*endpoint.Endpoint{
		endpoint.NewEndpoint("create-test.zone-1.ext-dns-test-2.teapot.zalan.do", endpoint.RecordTypeA, "8.8.8.8"),
		endpoint.NewEndpoint("create-test.zone-2.ext-dns-test-2.teapot.zalan.do", endpoint.RecordTypeA, "8.8.4.4"),
		endpoint.NewEndpoint("create-test-cname.zone-1.ext-dns-test-2.teapot.zalan.do", endpoint.RecordTypeCNAME, "foo.elb.amazonaws.com"),
		endpoint.NewEndpoint("create-test-cname-alias.zone-1.ext-dns-test-2.teapot.zalan.do", endpoint.RecordTypeCNAME, "foo.elb.amazonaws.com"),
		endpoint.NewEndpoint("create-test-multiple.zone-2.ext-dns-test-2.teapot.zalan.do", endpoint.RecordTypeA, "8.8.8.8", "8.8.4.4"),
	}

	currentRecords := []*endpoint.Endpoint{
		endpoint.NewEndpoint("update-test.zone-1.ext-dns-test-2.teapot.zalan.do", endpoint.RecordTypeA, "8.8.8.8"),
		endpoint.NewEndpoint("update-test.zone-2.ext-dns-test-2.teapot.zalan.do", endpoint.RecordTypeA, "8.8.4.4"),
		endpoint.NewEndpoint("update-test-a-to-cname.zone-1.ext-dns-test-2.teapot.zalan.do", endpoint.RecordTypeA, "1.1.1.1"),
		endpoint.NewEndpoint("update-test-cname.zone-1.ext-dns-test-2.teapot.zalan.do", endpoint.RecordTypeCNAME, "bar.elb.amazonaws.com"),
		endpoint.NewEndpoint("update-test-cname-alias.zone-1.ext-dns-test-2.teapot.zalan.do", endpoint.RecordTypeCNAME, "bar.elb.amazonaws.com"),
		endpoint.NewEndpoint("update-test-multiple.zone-2.ext-dns-test-2.teapot.zalan.do", endpoint.RecordTypeA, "8.8.8.8", "8.8.4.4"),
	}
	updatedRecords := []*endpoint.Endpoint{
		endpoint.NewEndpoint("update-test.zone-1.ext-dns-test-2.teapot.zalan.do", endpoint.RecordTypeA, "1.2.3.4"),
		endpoint.NewEndpoint("update-test.zone-2.ext-dns-test-2.teapot.zalan.do", endpoint.RecordTypeA, "4.3.2.1"),
		endpoint.NewEndpoint("update-test-a-to-cname.zone-1.ext-dns-test-2.teapot.zalan.do", endpoint.RecordTypeCNAME, "foo.elb.amazonaws.com"),
		endpoint.NewEndpoint("update-test-cname.zone-1.ext-dns-test-2.teapot.zalan.do", endpoint.RecordTypeCNAME, "baz.elb.amazonaws.com"),
		endpoint.NewEndpoint("update-test-cname-alias.zone-1.ext-dns-test-2.teapot.zalan.do", endpoint.RecordTypeCNAME, "baz.elb.amazonaws.com"),
		endpoint.NewEndpoint("update-test-multiple.zone-2.ext-dns-test-2.teapot.zalan.do", endpoint.RecordTypeA, "1.2.3.4", "4.3.2.1"),
	}

	deleteRecords := []*endpoint.Endpoint{
		endpoint.NewEndpoint("delete-test.zone-1.ext-dns-test-2.teapot.zalan.do", endpoint.RecordTypeA, "8.8.8.8"),
		endpoint.NewEndpoint("delete-test.zone-2.ext-dns-test-2.teapot.zalan.do", endpoint.RecordTypeA, "8.8.4.4"),
		endpoint.NewEndpoint("delete-test-cname.zone-1.ext-dns-test-2.teapot.zalan.do", endpoint.RecordTypeCNAME, "qux.elb.amazonaws.com"),
		endpoint.NewEndpoint("delete-test-cname-alias.zone-1.ext-dns-test-2.teapot.zalan.do", endpoint.RecordTypeCNAME, "qux.elb.amazonaws.com"),
		endpoint.NewEndpoint("delete-test-multiple.zone-2.ext-dns-test-2.teapot.zalan.do", endpoint.RecordTypeA, "1.2.3.4", "4.3.2.1"),
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

func TestAWSChangesByZones(t *testing.T) {
	changes := []*route53.Change{
		{
			Action: aws.String(route53.ChangeActionCreate),
			ResourceRecordSet: &route53.ResourceRecordSet{
				Name: aws.String("qux.foo.example.org"), TTL: aws.Int64(1),
			},
		},
		{
			Action: aws.String(route53.ChangeActionCreate),
			ResourceRecordSet: &route53.ResourceRecordSet{
				Name: aws.String("qux.bar.example.org"), TTL: aws.Int64(2),
			},
		},
		{
			Action: aws.String(route53.ChangeActionDelete),
			ResourceRecordSet: &route53.ResourceRecordSet{
				Name: aws.String("wambo.foo.example.org"), TTL: aws.Int64(10),
			},
		},
		{
			Action: aws.String(route53.ChangeActionDelete),
			ResourceRecordSet: &route53.ResourceRecordSet{
				Name: aws.String("wambo.bar.example.org"), TTL: aws.Int64(20),
			},
		},
	}

	zones := map[string]*route53.HostedZone{
		"foo-example-org": {
			Id:   aws.String("foo-example-org"),
			Name: aws.String("foo.example.org."),
		},
		"bar-example-org": {
			Id:   aws.String("bar-example-org"),
			Name: aws.String("bar.example.org."),
		},
		"bar-example-org-private": {
			Id:     aws.String("bar-example-org-private"),
			Name:   aws.String("bar.example.org."),
			Config: &route53.HostedZoneConfig{PrivateZone: aws.Bool(true)},
		},
		"baz-example-org": {
			Id:   aws.String("baz-example-org"),
			Name: aws.String("baz.example.org."),
		},
	}

	changesByZone := changesByZone(zones, changes)
	require.Len(t, changesByZone, 3)

	validateAWSChangeRecords(t, changesByZone["foo-example-org"], []*route53.Change{
		{
			Action: aws.String(route53.ChangeActionCreate),
			ResourceRecordSet: &route53.ResourceRecordSet{
				Name: aws.String("qux.foo.example.org"), TTL: aws.Int64(1),
			},
		},
		{
			Action: aws.String(route53.ChangeActionDelete),
			ResourceRecordSet: &route53.ResourceRecordSet{
				Name: aws.String("wambo.foo.example.org"), TTL: aws.Int64(10),
			},
		},
	})

	validateAWSChangeRecords(t, changesByZone["bar-example-org"], []*route53.Change{
		{
			Action: aws.String(route53.ChangeActionCreate),
			ResourceRecordSet: &route53.ResourceRecordSet{
				Name: aws.String("qux.bar.example.org"), TTL: aws.Int64(2),
			},
		},
		{
			Action: aws.String(route53.ChangeActionDelete),
			ResourceRecordSet: &route53.ResourceRecordSet{
				Name: aws.String("wambo.bar.example.org"), TTL: aws.Int64(20),
			},
		},
	})

	validateAWSChangeRecords(t, changesByZone["bar-example-org-private"], []*route53.Change{
		{
			Action: aws.String(route53.ChangeActionCreate),
			ResourceRecordSet: &route53.ResourceRecordSet{
				Name: aws.String("qux.bar.example.org"), TTL: aws.Int64(2),
			},
		},
		{
			Action: aws.String(route53.ChangeActionDelete),
			ResourceRecordSet: &route53.ResourceRecordSet{
				Name: aws.String("wambo.bar.example.org"), TTL: aws.Int64(20),
			},
		},
	})
}

func TestAWSsubmitChanges(t *testing.T) {
	provider, _ := newAWSProvider(t, endpoint.NewDomainFilter([]string{"ext-dns-test-2.teapot.zalan.do."}), provider.NewZoneIDFilter([]string{}), provider.NewZoneTypeFilter(""), defaultEvaluateTargetHealth, false, []*endpoint.Endpoint{})
	const subnets = 16
	const hosts = defaultBatchChangeSize / subnets

	endpoints := make([]*endpoint.Endpoint, 0)
	for i := 0; i < subnets; i++ {
		for j := 1; j < (hosts + 1); j++ {
			hostname := fmt.Sprintf("subnet%dhost%d.zone-1.ext-dns-test-2.teapot.zalan.do", i, j)
			ip := fmt.Sprintf("1.1.%d.%d", i, j)
			ep := endpoint.NewEndpointWithTTL(hostname, endpoint.RecordTypeA, endpoint.TTL(recordTTL), ip)
			endpoints = append(endpoints, ep)
		}
	}

	ctx := context.Background()
	zones, _ := provider.Zones(ctx)
	records, _ := provider.Records(ctx)
	cs := make([]*route53.Change, 0, len(endpoints))
	cs = append(cs, provider.newChanges(route53.ChangeActionCreate, endpoints, records, zones)...)

	require.NoError(t, provider.submitChanges(ctx, cs, zones))

	records, err := provider.Records(ctx)
	require.NoError(t, err)

	validateEndpoints(t, records, endpoints)
}

func TestAWSsubmitChangesError(t *testing.T) {
	provider, clientStub := newAWSProvider(t, endpoint.NewDomainFilter([]string{"ext-dns-test-2.teapot.zalan.do."}), provider.NewZoneIDFilter([]string{}), provider.NewZoneTypeFilter(""), defaultEvaluateTargetHealth, false, []*endpoint.Endpoint{})
	clientStub.MockMethod("ChangeResourceRecordSets", mock.Anything).Return(nil, fmt.Errorf("Mock route53 failure"))

	ctx := context.Background()
	zones, err := provider.Zones(ctx)
	require.NoError(t, err)
	records, err := provider.Records(ctx)
	require.NoError(t, err)

	ep := endpoint.NewEndpointWithTTL("fail.zone-1.ext-dns-test-2.teapot.zalan.do", endpoint.RecordTypeA, endpoint.TTL(recordTTL), "1.0.0.1")
	cs := provider.newChanges(route53.ChangeActionCreate, []*endpoint.Endpoint{ep}, records, zones)

	require.Error(t, provider.submitChanges(ctx, cs, zones))
}

func TestAWSsubmitChangesRetryOnError(t *testing.T) {
	provider, clientStub := newAWSProvider(t, endpoint.NewDomainFilter([]string{"ext-dns-test-2.teapot.zalan.do."}), provider.NewZoneIDFilter([]string{}), provider.NewZoneTypeFilter(""), defaultEvaluateTargetHealth, false, []*endpoint.Endpoint{})

	ctx := context.Background()
	zones, err := provider.Zones(ctx)
	require.NoError(t, err)
	records, err := provider.Records(ctx)
	require.NoError(t, err)

	ep1 := endpoint.NewEndpointWithTTL("success.zone-1.ext-dns-test-2.teapot.zalan.do", endpoint.RecordTypeA, endpoint.TTL(recordTTL), "1.0.0.1")
	ep2 := endpoint.NewEndpointWithTTL("fail.zone-1.ext-dns-test-2.teapot.zalan.do", endpoint.RecordTypeA, endpoint.TTL(recordTTL), "1.0.0.2")
	ep3 := endpoint.NewEndpointWithTTL("success2.zone-1.ext-dns-test-2.teapot.zalan.do", endpoint.RecordTypeA, endpoint.TTL(recordTTL), "1.0.0.3")

	ep2txt := endpoint.NewEndpointWithTTL("fail__edns_housekeeping.zone-1.ext-dns-test-2.teapot.zalan.do", endpoint.RecordTypeTXT, endpoint.TTL(recordTTL), "something") // "__edns_housekeeping" is the TXT suffix
	ep2txt.Labels = map[string]string{
		endpoint.OwnedRecordLabelKey: "fail.zone-1.ext-dns-test-2.teapot.zalan.do",
	}

	// "success" and "fail" are created in the first step, both are submitted in the same batch; this should fail
	cs1 := provider.newChanges(route53.ChangeActionCreate, []*endpoint.Endpoint{ep2, ep2txt, ep1}, records, zones)
	input1 := &route53.ChangeResourceRecordSetsInput{
		HostedZoneId: aws.String("/hostedzone/zone-1.ext-dns-test-2.teapot.zalan.do."),
		ChangeBatch: &route53.ChangeBatch{
			Changes: ownedRecordRemoved(cs1),
		},
	}
	clientStub.MockMethod("ChangeResourceRecordSets", input1).Return(nil, fmt.Errorf("Mock route53 failure"))

	// because of the failure, changes will be retried one by one; make "fail" submitted in its own batch fail as well
	cs2 := provider.newChanges(route53.ChangeActionCreate, []*endpoint.Endpoint{ep2, ep2txt}, records, zones)
	input2 := &route53.ChangeResourceRecordSetsInput{
		HostedZoneId: aws.String("/hostedzone/zone-1.ext-dns-test-2.teapot.zalan.do."),
		ChangeBatch: &route53.ChangeBatch{
			Changes: ownedRecordRemoved(cs2),
		},
	}
	clientStub.MockMethod("ChangeResourceRecordSets", input2).Return(nil, fmt.Errorf("Mock route53 failure"))

	// "success" should have been created, verify that we still get an error because "fail" failed
	require.Error(t, provider.submitChanges(ctx, cs1, zones))

	// assert that "success" was successfully created and "fail" and its TXT record were not
	records, err = provider.Records(ctx)
	require.NoError(t, err)
	ok := false
	for _, r := range records {
		if r.DNSName == "success.zone-1.ext-dns-test-2.teapot.zalan.do" {
			ok = true
		} else if r.DNSName == "fail.zone-1.ext-dns-test-2.teapot.zalan.do" || r.DNSName == "fail__edns_housekeeping.zone-1.ext-dns-test-2.teapot.zalan.do" {
			ok = false
			break
		}
	}
	require.True(t, ok)

	// next batch should contain "fail" and "success2", should succeed this time
	cs3 := provider.newChanges(route53.ChangeActionCreate, []*endpoint.Endpoint{ep2, ep2txt, ep3}, records, zones)
	require.NoError(t, provider.submitChanges(ctx, cs3, zones))

	// verify all records are there
	records, err = provider.Records(ctx)
	require.NoError(t, err)
	count := 0
	for _, r := range records {
		if r.DNSName == "success.zone-1.ext-dns-test-2.teapot.zalan.do" || r.DNSName == "fail.zone-1.ext-dns-test-2.teapot.zalan.do" || r.DNSName == "success2.zone-1.ext-dns-test-2.teapot.zalan.do" || r.DNSName == "fail__edns_housekeeping.zone-1.ext-dns-test-2.teapot.zalan.do" {
			count += 1
		}
	}
	require.Equal(t, count, 4)
}

func TestAWSBatchChangeSet(t *testing.T) {
	var cs []*route53.Change

	for i := 1; i <= defaultBatchChangeSize; i += 2 {
		cs = append(cs, &route53.Change{
			Action: aws.String(route53.ChangeActionCreate),
			ResourceRecordSet: &route53.ResourceRecordSet{
				Name: aws.String(fmt.Sprintf("host-%d", i)),
				Type: aws.String("A"),
			},
		})
		cs = append(cs, &route53.Change{
			Action: aws.String(route53.ChangeActionCreate),
			ResourceRecordSet: &route53.ResourceRecordSet{
				Name: aws.String(fmt.Sprintf("host-%d", i)),
				Type: aws.String("TXT"),
			},
		})
	}

	batchCs := batchChangeSet(cs, defaultBatchChangeSize)

	require.Equal(t, 1, len(batchCs))

	// sorting cs not needed as it should be returned as is
	validateAWSChangeRecords(t, batchCs[0], cs)
}

func TestAWSBatchChangeSetExceeding(t *testing.T) {
	var cs []*route53.Change
	const testCount = 50
	const testLimit = 11
	const expectedBatchCount = 5
	const expectedChangesCount = 10

	for i := 1; i <= testCount; i += 2 {
		cs = append(cs, &route53.Change{
			Action: aws.String(route53.ChangeActionCreate),
			ResourceRecordSet: &route53.ResourceRecordSet{
				Name: aws.String(fmt.Sprintf("host-%d", i)),
				Type: aws.String("A"),
			},
		})
		cs = append(cs, &route53.Change{
			Action: aws.String(route53.ChangeActionCreate),
			ResourceRecordSet: &route53.ResourceRecordSet{
				Name: aws.String(fmt.Sprintf("host-%d", i)),
				Type: aws.String("TXT"),
			},
		})
	}

	batchCs := batchChangeSet(cs, testLimit)

	require.Equal(t, expectedBatchCount, len(batchCs))

	// sorting cs needed to match batchCs
	for i, batch := range batchCs {
		validateAWSChangeRecords(t, batch, sortChangesByActionNameType(cs)[i*expectedChangesCount:expectedChangesCount*(i+1)])
	}
}

func TestAWSBatchChangeSetExceedingNameChange(t *testing.T) {
	var cs []*route53.Change
	const testCount = 10
	const testLimit = 1

	for i := 1; i <= testCount; i += 2 {
		cs = append(cs, &route53.Change{
			Action: aws.String(route53.ChangeActionCreate),
			ResourceRecordSet: &route53.ResourceRecordSet{
				Name: aws.String(fmt.Sprintf("host-%d", i)),
				Type: aws.String("A"),
			},
		})
		cs = append(cs, &route53.Change{
			Action: aws.String(route53.ChangeActionCreate),
			ResourceRecordSet: &route53.ResourceRecordSet{
				Name: aws.String(fmt.Sprintf("host-%d", i)),
				Type: aws.String("TXT"),
			},
		})
	}

	batchCs := batchChangeSet(cs, testLimit)

	require.Equal(t, 0, len(batchCs))
}

func validateEndpoints(t *testing.T, endpoints []*endpoint.Endpoint, expected []*endpoint.Endpoint) {
	assert.True(t, testutils.SameEndpoints(endpoints, expected), "actual and expected endpoints don't match. %s:%s", endpoints, expected)
}

func validateAWSZones(t *testing.T, zones map[string]*route53.HostedZone, expected map[string]*route53.HostedZone) {
	require.Len(t, zones, len(expected))

	for i, zone := range zones {
		validateAWSZone(t, zone, expected[i])
	}
}

func validateAWSZone(t *testing.T, zone *route53.HostedZone, expected *route53.HostedZone) {
	assert.Equal(t, aws.StringValue(expected.Id), aws.StringValue(zone.Id))
	assert.Equal(t, aws.StringValue(expected.Name), aws.StringValue(zone.Name))
}

func validateAWSChangeRecords(t *testing.T, records []*route53.Change, expected []*route53.Change) {
	require.Len(t, records, len(expected))

	for i := range records {
		validateAWSChangeRecord(t, records[i], expected[i])
	}
}

func validateAWSChangeRecord(t *testing.T, record *route53.Change, expected *route53.Change) {
	assert.Equal(t, aws.StringValue(expected.Action), aws.StringValue(record.Action))
	assert.Equal(t, aws.StringValue(expected.ResourceRecordSet.Name), aws.StringValue(record.ResourceRecordSet.Name))
	assert.Equal(t, aws.StringValue(expected.ResourceRecordSet.Type), aws.StringValue(record.ResourceRecordSet.Type))
}

func TestAWSCreateRecordsWithCNAME(t *testing.T) {
	provider, _ := newAWSProvider(t, endpoint.NewDomainFilter([]string{"ext-dns-test-2.teapot.zalan.do."}), provider.NewZoneIDFilter([]string{}), provider.NewZoneTypeFilter(""), defaultEvaluateTargetHealth, false, []*endpoint.Endpoint{})

	records := []*endpoint.Endpoint{
		{DNSName: "create-test.zone-1.ext-dns-test-2.teapot.zalan.do", Targets: endpoint.Targets{"foo.example.org"}, RecordType: endpoint.RecordTypeCNAME},
	}

	require.NoError(t, provider.CreateRecords(context.Background(), records))

	recordSets := listAWSRecords(t, provider.client, "/hostedzone/zone-1.ext-dns-test-2.teapot.zalan.do.")

	validateRecords(t, recordSets, []*route53.ResourceRecordSet{
		{
			Name: aws.String("create-test.zone-1.ext-dns-test-2.teapot.zalan.do."),
			Type: aws.String(endpoint.RecordTypeCNAME),
			TTL:  aws.Int64(300),
			ResourceRecords: []*route53.ResourceRecord{
				{
					Value: aws.String("foo.example.org"),
				},
			},
		},
	})
}

func TestAWSCreateRecordsWithALIAS(t *testing.T) {
	for key, evaluateTargetHealth := range map[string]bool{
		"true":  true,
		"false": false,
		"":      false,
	} {
		provider, _ := newAWSProvider(t, endpoint.NewDomainFilter([]string{"ext-dns-test-2.teapot.zalan.do."}), provider.NewZoneIDFilter([]string{}), provider.NewZoneTypeFilter(""), defaultEvaluateTargetHealth, false, []*endpoint.Endpoint{})

		// Test dualstack and ipv4 load balancer targets
		records := []*endpoint.Endpoint{
			{
				DNSName:    "create-test.zone-1.ext-dns-test-2.teapot.zalan.do",
				Targets:    endpoint.Targets{"foo.eu-central-1.elb.amazonaws.com"},
				RecordType: endpoint.RecordTypeCNAME,
				ProviderSpecific: endpoint.ProviderSpecific{
					endpoint.ProviderSpecificProperty{
						Name:  providerSpecificEvaluateTargetHealth,
						Value: key,
					},
				},
			},
			{
				DNSName:    "create-test-dualstack.zone-1.ext-dns-test-2.teapot.zalan.do",
				Targets:    endpoint.Targets{"bar.eu-central-1.elb.amazonaws.com"},
				RecordType: endpoint.RecordTypeCNAME,
				ProviderSpecific: endpoint.ProviderSpecific{
					endpoint.ProviderSpecificProperty{
						Name:  providerSpecificEvaluateTargetHealth,
						Value: key,
					},
				},
				Labels: map[string]string{endpoint.DualstackLabelKey: "true"},
			},
		}

		require.NoError(t, provider.CreateRecords(context.Background(), records))

		recordSets := listAWSRecords(t, provider.client, "/hostedzone/zone-1.ext-dns-test-2.teapot.zalan.do.")

		validateRecords(t, recordSets, []*route53.ResourceRecordSet{
			{
				AliasTarget: &route53.AliasTarget{
					DNSName:              aws.String("foo.eu-central-1.elb.amazonaws.com."),
					EvaluateTargetHealth: aws.Bool(evaluateTargetHealth),
					HostedZoneId:         aws.String("Z215JYRZR1TBD5"),
				},
				Name: aws.String("create-test.zone-1.ext-dns-test-2.teapot.zalan.do."),
				Type: aws.String(route53.RRTypeA),
			},
			{
				AliasTarget: &route53.AliasTarget{
					DNSName:              aws.String("bar.eu-central-1.elb.amazonaws.com."),
					EvaluateTargetHealth: aws.Bool(evaluateTargetHealth),
					HostedZoneId:         aws.String("Z215JYRZR1TBD5"),
				},
				Name: aws.String("create-test-dualstack.zone-1.ext-dns-test-2.teapot.zalan.do."),
				Type: aws.String(route53.RRTypeA),
			},
			{
				AliasTarget: &route53.AliasTarget{
					DNSName:              aws.String("bar.eu-central-1.elb.amazonaws.com."),
					EvaluateTargetHealth: aws.Bool(evaluateTargetHealth),
					HostedZoneId:         aws.String("Z215JYRZR1TBD5"),
				},
				Name: aws.String("create-test-dualstack.zone-1.ext-dns-test-2.teapot.zalan.do."),
				Type: aws.String(route53.RRTypeAaaa),
			},
		})
	}
}

func TestAWSisLoadBalancer(t *testing.T) {
	for _, tc := range []struct {
		target      string
		recordType  string
		preferCNAME bool
		expected    bool
	}{
		{"bar.eu-central-1.elb.amazonaws.com", endpoint.RecordTypeCNAME, false, true},
		{"bar.eu-central-1.elb.amazonaws.com", endpoint.RecordTypeCNAME, true, false},
		{"foo.example.org", endpoint.RecordTypeCNAME, false, false},
		{"foo.example.org", endpoint.RecordTypeCNAME, true, false},
	} {
		ep := &endpoint.Endpoint{
			Targets:    endpoint.Targets{tc.target},
			RecordType: tc.recordType,
		}
		assert.Equal(t, tc.expected, useAlias(ep, tc.preferCNAME))
	}
}

func TestAWSisAWSAlias(t *testing.T) {
	for _, tc := range []struct {
		target     string
		recordType string
		alias      string
		expected   string
	}{
		{"bar.example.org", endpoint.RecordTypeCNAME, "true", "Z215JYRZR1TBD5"},
		{"foo.example.org", endpoint.RecordTypeCNAME, "true", ""},
	} {
		ep := &endpoint.Endpoint{
			Targets:    endpoint.Targets{tc.target},
			RecordType: tc.recordType,
			ProviderSpecific: endpoint.ProviderSpecific{
				endpoint.ProviderSpecificProperty{
					Name:  "alias",
					Value: tc.alias,
				},
			},
		}
		addrs := []*endpoint.Endpoint{
			{
				DNSName: "foo.example.org",
				Targets: endpoint.Targets{"foobar.example.org"},
			},
			{
				DNSName: "bar.example.org",
				Targets: endpoint.Targets{"bar.eu-central-1.elb.amazonaws.com"},
			},
		}
		assert.Equal(t, tc.expected, isAWSAlias(ep, addrs))
	}
}

func TestAWSCanonicalHostedZone(t *testing.T) {
	for _, tc := range []struct {
		hostname string
		expected string
	}{
		// Application Load Balancers and Classic Load Balancers
		{"foo.us-east-2.elb.amazonaws.com", "Z3AADJGX6KTTL2"},
		{"foo.us-east-1.elb.amazonaws.com", "Z35SXDOTRQ7X7K"},
		{"foo.us-west-1.elb.amazonaws.com", "Z368ELLRRE2KJ0"},
		{"foo.us-west-2.elb.amazonaws.com", "Z1H1FL5HABSF5"},
		{"foo.ca-central-1.elb.amazonaws.com", "ZQSVJUPU6J1EY"},
		{"foo.ap-east-1.elb.amazonaws.com", "Z3DQVH9N71FHZ0"},
		{"foo.ap-south-1.elb.amazonaws.com", "ZP97RAFLXTNZK"},
		{"foo.ap-northeast-2.elb.amazonaws.com", "ZWKZPGTI48KDX"},
		{"foo.ap-northeast-3.elb.amazonaws.com", "Z5LXEXXYW11ES"},
		{"foo.ap-southeast-1.elb.amazonaws.com", "Z1LMS91P8CMLE5"},
		{"foo.ap-southeast-2.elb.amazonaws.com", "Z1GM3OXH4ZPM65"},
		{"foo.ap-northeast-1.elb.amazonaws.com", "Z14GRHDCWA56QT"},
		{"foo.eu-central-1.elb.amazonaws.com", "Z215JYRZR1TBD5"},
		{"foo.eu-west-1.elb.amazonaws.com", "Z32O12XQLNTSW2"},
		{"foo.eu-west-2.elb.amazonaws.com", "ZHURV8PSTC4K8"},
		{"foo.eu-west-3.elb.amazonaws.com", "Z3Q77PNBQS71R4"},
		{"foo.sa-east-1.elb.amazonaws.com", "Z2P70J7HTTTPLU"},
		{"foo.cn-north-1.elb.amazonaws.com.cn", "Z1GDH35T77C1KE"},
		{"foo.cn-northwest-1.elb.amazonaws.com.cn", "ZM7IZAIOVVDZF"},
		{"foo.af-south-1.elb.amazonaws.com", "Z268VQBMOI5EKX"},
		// Network Load Balancers
		{"foo.elb.us-east-2.amazonaws.com", "ZLMOA37VPKANP"},
		{"foo.elb.us-east-1.amazonaws.com", "Z26RNL4JYFTOTI"},
		{"foo.elb.us-west-1.amazonaws.com", "Z24FKFUX50B4VW"},
		{"foo.elb.us-west-2.amazonaws.com", "Z18D5FSROUN65G"},
		{"foo.elb.ca-central-1.amazonaws.com", "Z2EPGBW3API2WT"},
		{"foo.elb.ap-east-1.amazonaws.com", "Z12Y7K3UBGUAD1"},
		{"foo.elb.ap-south-1.amazonaws.com", "ZVDDRBQ08TROA"},
		{"foo.elb.ap-northeast-2.amazonaws.com", "ZIBE1TIR4HY56"},
		{"foo.elb.ap-southeast-1.amazonaws.com", "ZKVM4W9LS7TM"},
		{"foo.elb.ap-southeast-2.amazonaws.com", "ZCT6FZBF4DROD"},
		{"foo.elb.ap-northeast-1.amazonaws.com", "Z31USIVHYNEOWT"},
		{"foo.elb.eu-central-1.amazonaws.com", "Z3F0SRJ5LGBH90"},
		{"foo.elb.eu-west-1.amazonaws.com", "Z2IFOLAFXWLO4F"},
		{"foo.elb.eu-west-2.amazonaws.com", "ZD4D7Y8KGAS4G"},
		{"foo.elb.eu-west-3.amazonaws.com", "Z1CMS0P5QUZ6D5"},
		{"foo.elb.sa-east-1.amazonaws.com", "ZTK26PT1VY4CU"},
		{"foo.elb.cn-north-1.amazonaws.com.cn", "Z3QFB96KMJ7ED6"},
		{"foo.elb.cn-northwest-1.amazonaws.com.cn", "ZQEIKTCZ8352D"},
		{"foo.elb.af-south-1.amazonaws.com", "Z203XCE67M25HM"},
		// No Load Balancer
		{"foo.example.org", ""},
	} {
		zone := canonicalHostedZone(tc.hostname)
		assert.Equal(t, tc.expected, zone)
	}
}

func TestAWSSuitableZones(t *testing.T) {
	zones := map[string]*route53.HostedZone{
		// Public domain
		"example-org": {Id: aws.String("example-org"), Name: aws.String("example.org.")},
		// Public subdomain
		"bar-example-org": {Id: aws.String("bar-example-org"), Name: aws.String("bar.example.org."), Config: &route53.HostedZoneConfig{PrivateZone: aws.Bool(false)}},
		// Public subdomain
		"longfoo-bar-example-org": {Id: aws.String("longfoo-bar-example-org"), Name: aws.String("longfoo.bar.example.org.")},
		// Private domain
		"example-org-private": {Id: aws.String("example-org-private"), Name: aws.String("example.org."), Config: &route53.HostedZoneConfig{PrivateZone: aws.Bool(true)}},
		// Private subdomain
		"bar-example-org-private": {Id: aws.String("bar-example-org-private"), Name: aws.String("bar.example.org."), Config: &route53.HostedZoneConfig{PrivateZone: aws.Bool(true)}},
	}

	for _, tc := range []struct {
		hostname string
		expected []*route53.HostedZone
	}{
		// bar.example.org is NOT suitable
		{"foobar.example.org.", []*route53.HostedZone{zones["example-org-private"], zones["example-org"]}},

		// all matching private zones are suitable
		// https://github.com/kubernetes-sigs/external-dns/pull/356
		{"bar.example.org.", []*route53.HostedZone{zones["example-org-private"], zones["bar-example-org-private"], zones["bar-example-org"]}},

		{"foo.bar.example.org.", []*route53.HostedZone{zones["example-org-private"], zones["bar-example-org-private"], zones["bar-example-org"]}},
		{"foo.example.org.", []*route53.HostedZone{zones["example-org-private"], zones["example-org"]}},
		{"foo.kubernetes.io.", nil},
	} {
		suitableZones := suitableZones(tc.hostname, zones)
		sort.Slice(suitableZones, func(i, j int) bool {
			return *suitableZones[i].Id < *suitableZones[j].Id
		})
		sort.Slice(tc.expected, func(i, j int) bool {
			return *tc.expected[i].Id < *tc.expected[j].Id
		})
		assert.Equal(t, tc.expected, suitableZones)
	}
}

func TestAWSHealthTargetAnnotation(tt *testing.T) {
	comparator := func(name, previous, current string) bool {
		return previous == current
	}
	for _, test := range []struct {
		name               string
		current            *endpoint.Endpoint
		desired            *endpoint.Endpoint
		propertyComparator func(name, previous, current string) bool
		shouldUpdate       bool
	}{
		{
			name: "skip AWS target health",
			current: &endpoint.Endpoint{
				RecordType: "A",
				DNSName:    "foo.com",
				ProviderSpecific: []endpoint.ProviderSpecificProperty{
					{Name: "aws/evaluate-target-health", Value: "true"},
				},
			},
			desired: &endpoint.Endpoint{
				DNSName:    "foo.com",
				RecordType: "A",
				ProviderSpecific: []endpoint.ProviderSpecificProperty{
					{Name: "aws/evaluate-target-health", Value: "false"},
				},
			},
			propertyComparator: comparator,
			shouldUpdate:       false,
		},
	} {
		tt.Run(test.name, func(t *testing.T) {
			provider := &AWSProvider{}
			plan := &plan.Plan{
				Current:            []*endpoint.Endpoint{test.current},
				Desired:            []*endpoint.Endpoint{test.desired},
				PropertyComparator: provider.PropertyValuesEqual,
				ManagedRecords:     []string{endpoint.RecordTypeA, endpoint.RecordTypeCNAME},
			}
			plan = plan.Calculate()
			assert.Equal(t, test.shouldUpdate, len(plan.Changes.UpdateNew) == 1)
		})
	}
}

func createAWSZone(t *testing.T, provider *AWSProvider, zone *route53.HostedZone) {
	params := &route53.CreateHostedZoneInput{
		CallerReference:  aws.String("external-dns.alpha.kubernetes.io/test-zone"),
		Name:             zone.Name,
		HostedZoneConfig: zone.Config,
	}

	if _, err := provider.client.CreateHostedZoneWithContext(context.Background(), params); err != nil {
		require.EqualError(t, err, route53.ErrCodeHostedZoneAlreadyExists)
	}
}

func setupAWSRecords(t *testing.T, provider *AWSProvider, endpoints []*endpoint.Endpoint) {
	clearAWSRecords(t, provider, "/hostedzone/zone-1.ext-dns-test-2.teapot.zalan.do.")
	clearAWSRecords(t, provider, "/hostedzone/zone-2.ext-dns-test-2.teapot.zalan.do.")
	clearAWSRecords(t, provider, "/hostedzone/zone-3.ext-dns-test-2.teapot.zalan.do.")

	ctx := context.Background()
	records, err := provider.Records(ctx)
	require.NoError(t, err)

	validateEndpoints(t, records, []*endpoint.Endpoint{})

	require.NoError(t, provider.CreateRecords(context.Background(), endpoints))

	escapeAWSRecords(t, provider, "/hostedzone/zone-1.ext-dns-test-2.teapot.zalan.do.")
	escapeAWSRecords(t, provider, "/hostedzone/zone-2.ext-dns-test-2.teapot.zalan.do.")
	escapeAWSRecords(t, provider, "/hostedzone/zone-3.ext-dns-test-2.teapot.zalan.do.")

	_, err = provider.Records(ctx)
	require.NoError(t, err)

}

func listAWSRecords(t *testing.T, client Route53API, zone string) []*route53.ResourceRecordSet {
	recordSets := []*route53.ResourceRecordSet{}
	require.NoError(t, client.ListResourceRecordSetsPagesWithContext(context.Background(), &route53.ListResourceRecordSetsInput{
		HostedZoneId: aws.String(zone),
	}, func(resp *route53.ListResourceRecordSetsOutput, _ bool) bool {
		recordSets = append(recordSets, resp.ResourceRecordSets...)
		return true
	}))

	return recordSets
}

func clearAWSRecords(t *testing.T, provider *AWSProvider, zone string) {
	recordSets := listAWSRecords(t, provider.client, zone)

	changes := make([]*route53.Change, 0, len(recordSets))
	for _, recordSet := range recordSets {
		changes = append(changes, &route53.Change{
			Action:            aws.String(route53.ChangeActionDelete),
			ResourceRecordSet: recordSet,
		})
	}

	if len(changes) != 0 {
		_, err := provider.client.ChangeResourceRecordSetsWithContext(context.Background(), &route53.ChangeResourceRecordSetsInput{
			HostedZoneId: aws.String(zone),
			ChangeBatch: &route53.ChangeBatch{
				Changes: changes,
			},
		})
		require.NoError(t, err)
	}
}

// Route53 stores wildcards escaped: http://docs.aws.amazon.com/Route53/latest/DeveloperGuide/DomainNameFormat.html?shortFooter=true#domain-name-format-asterisk
func escapeAWSRecords(t *testing.T, provider *AWSProvider, zone string) {
	recordSets := listAWSRecords(t, provider.client, zone)

	changes := make([]*route53.Change, 0, len(recordSets))
	for _, recordSet := range recordSets {
		changes = append(changes, &route53.Change{
			Action:            aws.String(route53.ChangeActionUpsert),
			ResourceRecordSet: recordSet,
		})
	}

	if len(changes) != 0 {
		_, err := provider.client.ChangeResourceRecordSetsWithContext(context.Background(), &route53.ChangeResourceRecordSetsInput{
			HostedZoneId: aws.String(zone),
			ChangeBatch: &route53.ChangeBatch{
				Changes: changes,
			},
		})
		require.NoError(t, err)
	}
}
func newAWSProvider(t *testing.T, domainFilter endpoint.DomainFilter, zoneIDFilter provider.ZoneIDFilter, zoneTypeFilter provider.ZoneTypeFilter, evaluateTargetHealth, dryRun bool, records []*endpoint.Endpoint) (*AWSProvider, *Route53APIStub) {
	return newAWSProviderWithTagFilter(t, domainFilter, zoneIDFilter, zoneTypeFilter, provider.NewZoneTagFilter([]string{}), evaluateTargetHealth, dryRun, records)
}

func newAWSProviderWithTagFilter(t *testing.T, domainFilter endpoint.DomainFilter, zoneIDFilter provider.ZoneIDFilter, zoneTypeFilter provider.ZoneTypeFilter, zoneTagFilter provider.ZoneTagFilter, evaluateTargetHealth, dryRun bool, records []*endpoint.Endpoint) (*AWSProvider, *Route53APIStub) {
	client := NewRoute53APIStub()

	provider := &AWSProvider{
		client:               client,
		batchChangeSize:      defaultBatchChangeSize,
		batchChangeInterval:  defaultBatchChangeInterval,
		evaluateTargetHealth: evaluateTargetHealth,
		domainFilter:         domainFilter,
		zoneIDFilter:         zoneIDFilter,
		zoneTypeFilter:       zoneTypeFilter,
		zoneTagFilter:        zoneTagFilter,
		dryRun:               false,
		zonesCache:           &zonesListCache{duration: 1 * time.Minute},
		failedChangesQueue:   make(map[string][]*route53.Change),
	}

	createAWSZone(t, provider, &route53.HostedZone{
		Id:     aws.String("/hostedzone/zone-1.ext-dns-test-2.teapot.zalan.do."),
		Name:   aws.String("zone-1.ext-dns-test-2.teapot.zalan.do."),
		Config: &route53.HostedZoneConfig{PrivateZone: aws.Bool(false)},
	})

	createAWSZone(t, provider, &route53.HostedZone{
		Id:     aws.String("/hostedzone/zone-2.ext-dns-test-2.teapot.zalan.do."),
		Name:   aws.String("zone-2.ext-dns-test-2.teapot.zalan.do."),
		Config: &route53.HostedZoneConfig{PrivateZone: aws.Bool(false)},
	})

	createAWSZone(t, provider, &route53.HostedZone{
		Id:     aws.String("/hostedzone/zone-3.ext-dns-test-2.teapot.zalan.do."),
		Name:   aws.String("zone-3.ext-dns-test-2.teapot.zalan.do."),
		Config: &route53.HostedZoneConfig{PrivateZone: aws.Bool(true)},
	})

	// filtered out by domain filter
	createAWSZone(t, provider, &route53.HostedZone{
		Id:     aws.String("/hostedzone/zone-4.ext-dns-test-3.teapot.zalan.do."),
		Name:   aws.String("zone-4.ext-dns-test-3.teapot.zalan.do."),
		Config: &route53.HostedZoneConfig{PrivateZone: aws.Bool(false)},
	})

	setupZoneTags(provider.client.(*Route53APIStub))

	setupAWSRecords(t, provider, records)

	provider.dryRun = dryRun

	return provider, client
}

func setupZoneTags(client *Route53APIStub) {
	addZoneTags(client.zoneTags, "/hostedzone/zone-1.ext-dns-test-2.teapot.zalan.do.", map[string]string{
		"zone-1-tag-1": "tag-1-value",
		"domain":       "test-2",
		"zone":         "1",
	})
	addZoneTags(client.zoneTags, "/hostedzone/zone-2.ext-dns-test-2.teapot.zalan.do.", map[string]string{
		"zone-2-tag-1": "tag-1-value",
		"domain":       "test-2",
		"zone":         "2",
	})
	addZoneTags(client.zoneTags, "/hostedzone/zone-3.ext-dns-test-2.teapot.zalan.do.", map[string]string{
		"zone-3-tag-1": "tag-1-value",
		"domain":       "test-2",
		"zone":         "3",
	})
	addZoneTags(client.zoneTags, "/hostedzone/zone-4.ext-dns-test-2.teapot.zalan.do.", map[string]string{
		"zone-4-tag-1": "tag-1-value",
		"domain":       "test-3",
		"zone":         "4",
	})
}

func addZoneTags(tagMap map[string][]*route53.Tag, zoneID string, tags map[string]string) {
	tagList := make([]*route53.Tag, 0, len(tags))
	for k, v := range tags {
		tagList = append(tagList, &route53.Tag{
			Key:   aws.String(k),
			Value: aws.String(v),
		})
	}
	tagMap[zoneID] = tagList
}

func validateRecords(t *testing.T, records []*route53.ResourceRecordSet, expected []*route53.ResourceRecordSet) {
	assert.ElementsMatch(t, expected, records)
}
