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

package awssd

import (
	"context"
	"errors"
	"math/rand"
	"reflect"
	"strconv"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	sd "github.com/aws/aws-sdk-go/service/servicediscovery"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"sigs.k8s.io/external-dns/endpoint"
	"sigs.k8s.io/external-dns/internal/testutils"
	"sigs.k8s.io/external-dns/plan"
)

// Compile time check for interface conformance
var _ AWSSDClient = &AWSSDClientStub{}

type AWSSDClientStub struct {
	// map[namespace_id]namespace
	namespaces map[string]*sd.Namespace

	// map[namespace_id] => map[service_id]instance
	services map[string]map[string]*sd.Service

	// map[service_id] => map[inst_id]instance
	instances map[string]map[string]*sd.Instance
}

func (s *AWSSDClientStub) CreateService(input *sd.CreateServiceInput) (*sd.CreateServiceOutput, error) {

	srv := &sd.Service{
		Id:               aws.String(strconv.Itoa(rand.Intn(10000))),
		DnsConfig:        input.DnsConfig,
		Name:             input.Name,
		Description:      input.Description,
		CreateDate:       aws.Time(time.Now()),
		CreatorRequestId: input.CreatorRequestId,
	}

	nsServices, ok := s.services[*input.NamespaceId]
	if !ok {
		nsServices = make(map[string]*sd.Service)
		s.services[*input.NamespaceId] = nsServices
	}
	nsServices[*srv.Id] = srv

	return &sd.CreateServiceOutput{
		Service: srv,
	}, nil
}

func (s *AWSSDClientStub) DeregisterInstance(input *sd.DeregisterInstanceInput) (*sd.DeregisterInstanceOutput, error) {
	serviceInstances := s.instances[*input.ServiceId]
	delete(serviceInstances, *input.InstanceId)

	return &sd.DeregisterInstanceOutput{}, nil
}

func (s *AWSSDClientStub) GetService(input *sd.GetServiceInput) (*sd.GetServiceOutput, error) {
	for _, entry := range s.services {
		srv, ok := entry[*input.Id]
		if ok {
			return &sd.GetServiceOutput{
				Service: srv,
			}, nil
		}
	}

	return nil, errors.New("service not found")
}

func (s *AWSSDClientStub) ListInstancesPages(input *sd.ListInstancesInput, fn func(*sd.ListInstancesOutput, bool) bool) error {
	instances := make([]*sd.InstanceSummary, 0)

	for _, inst := range s.instances[*input.ServiceId] {
		instances = append(instances, instanceToInstanceSummary(inst))
	}

	fn(&sd.ListInstancesOutput{
		Instances: instances,
	}, true)

	return nil
}

func (s *AWSSDClientStub) ListNamespacesPages(input *sd.ListNamespacesInput, fn func(*sd.ListNamespacesOutput, bool) bool) error {
	namespaces := make([]*sd.NamespaceSummary, 0)

	filter := input.Filters[0]

	for _, ns := range s.namespaces {
		if filter != nil && *filter.Name == sd.NamespaceFilterNameType {
			if *ns.Type != *filter.Values[0] {
				// skip namespaces not matching filter
				continue
			}
		}
		namespaces = append(namespaces, namespaceToNamespaceSummary(ns))
	}

	fn(&sd.ListNamespacesOutput{
		Namespaces: namespaces,
	}, true)

	return nil
}

func (s *AWSSDClientStub) ListServicesPages(input *sd.ListServicesInput, fn func(*sd.ListServicesOutput, bool) bool) error {
	services := make([]*sd.ServiceSummary, 0)

	// get namespace filter
	filter := input.Filters[0]
	if filter == nil || *filter.Name != sd.ServiceFilterNameNamespaceId {
		return errors.New("missing namespace filter")
	}
	nsID := filter.Values[0]

	for _, srv := range s.services[*nsID] {
		services = append(services, serviceToServiceSummary(srv))
	}

	fn(&sd.ListServicesOutput{
		Services: services,
	}, true)

	return nil
}

func (s *AWSSDClientStub) RegisterInstance(input *sd.RegisterInstanceInput) (*sd.RegisterInstanceOutput, error) {

	srvInstances, ok := s.instances[*input.ServiceId]
	if !ok {
		srvInstances = make(map[string]*sd.Instance)
		s.instances[*input.ServiceId] = srvInstances
	}

	srvInstances[*input.InstanceId] = &sd.Instance{
		Id:               input.InstanceId,
		Attributes:       input.Attributes,
		CreatorRequestId: input.CreatorRequestId,
	}

	return &sd.RegisterInstanceOutput{}, nil
}

func (s *AWSSDClientStub) UpdateService(input *sd.UpdateServiceInput) (*sd.UpdateServiceOutput, error) {
	out, err := s.GetService(&sd.GetServiceInput{Id: input.Id})
	if err != nil {
		return nil, err
	}

	origSrv := out.Service
	updateSrv := input.Service

	origSrv.Description = updateSrv.Description
	origSrv.DnsConfig.DnsRecords = updateSrv.DnsConfig.DnsRecords

	return &sd.UpdateServiceOutput{}, nil
}

func newTestAWSSDProvider(api AWSSDClient, domainFilter endpoint.DomainFilter, namespaceTypeFilter string) *AWSSDProvider {
	return &AWSSDProvider{
		client:              api,
		namespaceFilter:     domainFilter,
		namespaceTypeFilter: newSdNamespaceFilter(namespaceTypeFilter),
		dryRun:              false,
	}
}

func TestAWSSDProvider_Records(t *testing.T) {
	namespaces := map[string]*sd.Namespace{
		"private": {
			Id:   aws.String("private"),
			Name: aws.String("private.com"),
			Type: aws.String(sd.NamespaceTypeDnsPrivate),
		},
	}

	services := map[string]map[string]*sd.Service{
		"private": {
			"a-srv": {
				Id:          aws.String("a-srv"),
				Name:        aws.String("service1"),
				Description: aws.String("owner-id"),
				DnsConfig: &sd.DnsConfig{
					NamespaceId:   aws.String("private"),
					RoutingPolicy: aws.String(sd.RoutingPolicyWeighted),
					DnsRecords: []*sd.DnsRecord{{
						Type: aws.String(sd.RecordTypeA),
						TTL:  aws.Int64(100),
					}},
				},
			},
			"a-v2-srv": {
				Id:          aws.String("a-v2-srv"),
				Name:        aws.String("service1v2"),
				Description: aws.String("owner-id"),
				DnsConfig: &sd.DnsConfig{
					NamespaceId:   aws.String("private"),
					RoutingPolicy: aws.String(sd.RoutingPolicyWeighted),
					DnsRecords: []*sd.DnsRecord{{
						Type: aws.String(sd.RecordTypeA),
						TTL:  aws.Int64(100),
					}},
				},
			},
			"alias-srv": {
				Id:          aws.String("alias-srv"),
				Name:        aws.String("service2"),
				Description: aws.String("owner-id"),
				DnsConfig: &sd.DnsConfig{
					NamespaceId:   aws.String("private"),
					RoutingPolicy: aws.String(sd.RoutingPolicyWeighted),
					DnsRecords: []*sd.DnsRecord{{
						Type: aws.String(sd.RecordTypeA),
						TTL:  aws.Int64(100),
					}},
				},
			},
			"cname-srv": {
				Id:          aws.String("cname-srv"),
				Name:        aws.String("service3"),
				Description: aws.String("owner-id"),
				DnsConfig: &sd.DnsConfig{
					NamespaceId:   aws.String("private"),
					RoutingPolicy: aws.String(sd.RoutingPolicyWeighted),
					DnsRecords: []*sd.DnsRecord{{
						Type: aws.String(sd.RecordTypeCname),
						TTL:  aws.Int64(80),
					}},
				},
			},
			"srv-srv": {
				Id:          aws.String("srv-srv"),
				Name:        aws.String("_http._tcp.service1"),
				Description: aws.String("owner-id"),
				DnsConfig: &sd.DnsConfig{
					NamespaceId:   aws.String("private"),
					RoutingPolicy: aws.String(sd.RoutingPolicyWeighted),
					DnsRecords: []*sd.DnsRecord{{
						Type: aws.String(sd.RecordTypeSrv),
						TTL:  aws.Int64(120),
					}},
				},
			},
			"srv-partial-srv": {
				Id:          aws.String("srv-partial-srv"),
				Name:        aws.String("_httppartial._tcp.service1"),
				Description: aws.String("owner-id"),
				DnsConfig: &sd.DnsConfig{
					NamespaceId:   aws.String("private"),
					RoutingPolicy: aws.String(sd.RoutingPolicyWeighted),
					DnsRecords: []*sd.DnsRecord{{
						Type: aws.String(sd.RecordTypeSrv),
						TTL:  aws.Int64(120),
					}},
				},
			},
			"srv-wrongport-srv": {
				Id:          aws.String("srv-wrongport-srv"),
				Name:        aws.String("_httpwrongport._tcp.service1"),
				Description: aws.String("owner-id"),
				DnsConfig: &sd.DnsConfig{
					NamespaceId:   aws.String("private"),
					RoutingPolicy: aws.String(sd.RoutingPolicyWeighted),
					DnsRecords: []*sd.DnsRecord{{
						Type: aws.String(sd.RecordTypeSrv),
						TTL:  aws.Int64(120),
					}},
				},
			},
			"srv-standalone-srv": {
				Id:          aws.String("srv-standalone-srv"),
				Name:        aws.String("_http._tcp.standalone"),
				Description: aws.String("owner-id"),
				DnsConfig: &sd.DnsConfig{
					NamespaceId:   aws.String("private"),
					RoutingPolicy: aws.String(sd.RoutingPolicyWeighted),
					DnsRecords: []*sd.DnsRecord{{
						Type: aws.String(sd.RecordTypeSrv),
						TTL:  aws.Int64(120),
					}},
				},
			},
			"srv-multiple-srv": {
				Id:          aws.String("srv-multiple-srv"),
				Name:        aws.String("_http._tcp.multiple"),
				Description: aws.String("owner-id"),
				DnsConfig: &sd.DnsConfig{
					NamespaceId:   aws.String("private"),
					RoutingPolicy: aws.String(sd.RoutingPolicyWeighted),
					DnsRecords: []*sd.DnsRecord{{
						Type: aws.String(sd.RecordTypeSrv),
						TTL:  aws.Int64(120),
					}},
				},
			},
			"srv-multiplepartial-srv": {
				Id:          aws.String("srv-multiplepartial-srv"),
				Name:        aws.String("_http._tcp.multiplepartial"),
				Description: aws.String("owner-id"),
				DnsConfig: &sd.DnsConfig{
					NamespaceId:   aws.String("private"),
					RoutingPolicy: aws.String(sd.RoutingPolicyWeighted),
					DnsRecords: []*sd.DnsRecord{{
						Type: aws.String(sd.RecordTypeSrv),
						TTL:  aws.Int64(120),
					}},
				},
			},
		},
	}

	instances := map[string]map[string]*sd.Instance{
		"a-srv": {
			"1.2.3.4": {
				Id: aws.String("1.2.3.4"),
				Attributes: map[string]*string{
					sdInstanceAttrIPV4: aws.String("1.2.3.4"),
				},
			},
			"1.2.3.5": {
				Id: aws.String("1.2.3.5"),
				Attributes: map[string]*string{
					sdInstanceAttrIPV4: aws.String("1.2.3.5"),
				},
			},
		},
		"a-v2-srv": {
			"1.2.3.6": {
				Id: aws.String("1.2.3.6"),
				Attributes: map[string]*string{
					sdInstanceAttrIPV4: aws.String("1.2.3.6"),
				},
			},
			"1.2.3.7": {
				Id: aws.String("1.2.3.7"),
				Attributes: map[string]*string{
					sdInstanceAttrIPV4: aws.String("1.2.3.7"),
				},
			},
		},
		"alias-srv": {
			"load-balancer.us-east-1.elb.amazonaws.com": {
				Id: aws.String("load-balancer.us-east-1.elb.amazonaws.com"),
				Attributes: map[string]*string{
					sdInstanceAttrAlias: aws.String("load-balancer.us-east-1.elb.amazonaws.com"),
				},
			},
		},
		"cname-srv": {
			"cname.target.com": {
				Id: aws.String("cname.target.com"),
				Attributes: map[string]*string{
					sdInstanceAttrCname: aws.String("cname.target.com"),
				},
			},
		},
		"srv-srv": {
			"1.2.3.4_0_50_80_service1.private.com": {
				Id: aws.String("1.2.3.4_0_50_80_service1.private.com"),
				Attributes: map[string]*string{
					sdInstanceAttrIPV4:        aws.String("1.2.3.4"),
					sdInstanceAttrPort:        aws.String("80"),
					sdInstanceAttrOriginalSrv: aws.String("0 50 80 service1.private.com"),
				},
			},
			"1.2.3.5_0_50_80_service1.private.com": {
				Id: aws.String("1.2.3.5_0_50_80_service1.private.com"),
				Attributes: map[string]*string{
					sdInstanceAttrIPV4:        aws.String("1.2.3.5"),
					sdInstanceAttrPort:        aws.String("80"),
					sdInstanceAttrOriginalSrv: aws.String("0 50 80 service1.private.com"),
				},
			},
		},
		"srv-partial-srv": {
			"1.2.3.4_0_50_80_service1.private.com": {
				Id: aws.String("1.2.3.4_0_50_80_service1.private.com"),
				Attributes: map[string]*string{
					sdInstanceAttrIPV4:        aws.String("1.2.3.4"),
					sdInstanceAttrPort:        aws.String("80"),
					sdInstanceAttrOriginalSrv: aws.String("0 50 80 service1.private.com"),
				},
			},
		},
		"srv-wrongport-srv": {
			"1.2.3.4_0_50_80_service1.private.com": {
				Id: aws.String("1.2.3.4_0_50_80_service1.private.com"),
				Attributes: map[string]*string{
					sdInstanceAttrIPV4:        aws.String("1.2.3.4"),
					sdInstanceAttrPort:        aws.String("80"),
					sdInstanceAttrOriginalSrv: aws.String("0 50 80 service1.private.com"),
				},
			},
			"1.2.3.5_0_50_8080_service1.private.com": {
				Id: aws.String("1.2.3.5_0_50_8080_service1.private.com"),
				Attributes: map[string]*string{
					sdInstanceAttrIPV4:        aws.String("1.2.3.5"),
					sdInstanceAttrPort:        aws.String("999"),
					sdInstanceAttrOriginalSrv: aws.String("0 50 999 service1.private.com"),
				},
			},
		},
		"srv-standalone-srv": {
			"1.2.3.6_0_50_80_testabc.private.com": {
				Id: aws.String("1.2.3.6_0_50_80_testabc.private.com"),
				Attributes: map[string]*string{
					sdInstanceAttrIPV4:        aws.String("1.2.3.6"),
					sdInstanceAttrPort:        aws.String("80"),
					sdInstanceAttrOriginalSrv: aws.String("0 50 80 testabc.private.com"),
				},
			},
			"1.2.3.7_0_50_80_testdef.private.com": {
				Id: aws.String("1.2.3.7_0_50_80_testdef.private.com"),
				Attributes: map[string]*string{
					sdInstanceAttrIPV4:        aws.String("1.2.3.7"),
					sdInstanceAttrPort:        aws.String("80"),
					sdInstanceAttrOriginalSrv: aws.String("0 50 80 testdef.private.com"),
				},
			},
		},
		"srv-multiple-srv": {
			"1.2.3.4_0_50_80_service1.private.com": {
				Id: aws.String("1.2.3.4_0_50_80_service1.private.com"),
				Attributes: map[string]*string{
					sdInstanceAttrIPV4:        aws.String("1.2.3.4"),
					sdInstanceAttrPort:        aws.String("80"),
					sdInstanceAttrOriginalSrv: aws.String("0 50 80 service1.private.com"),
				},
			},
			"1.2.3.5_0_50_80_service1.private.com": {
				Id: aws.String("1.2.3.5_0_50_80_service1.private.com"),
				Attributes: map[string]*string{
					sdInstanceAttrIPV4:        aws.String("1.2.3.5"),
					sdInstanceAttrPort:        aws.String("80"),
					sdInstanceAttrOriginalSrv: aws.String("0 50 80 service1.private.com"),
				},
			},
			"1.2.3.6_0_50_8081_service1v2.private.com": {
				Id: aws.String("1.2.3.6_0_50_8081_service1v2.private.com"),
				Attributes: map[string]*string{
					sdInstanceAttrIPV4:        aws.String("1.2.3.6"),
					sdInstanceAttrPort:        aws.String("8081"),
					sdInstanceAttrOriginalSrv: aws.String("0 50 8081 service1v2.private.com"),
				},
			},
			"1.2.3.7_0_50_8081_service1v2.private.com": {
				Id: aws.String("1.2.3.7_0_50_8081_service1v2.private.com"),
				Attributes: map[string]*string{
					sdInstanceAttrIPV4:        aws.String("1.2.3.7"),
					sdInstanceAttrPort:        aws.String("8081"),
					sdInstanceAttrOriginalSrv: aws.String("0 50 8081 service1v2.private.com"),
				},
			},
		},
		"srv-multiplepartial-srv": {
			"1.2.3.4_0_50_80_service1.private.com": {
				Id: aws.String("1.2.3.4_0_50_80_service1.private.com"),
				Attributes: map[string]*string{
					sdInstanceAttrIPV4:        aws.String("1.2.3.4"),
					sdInstanceAttrPort:        aws.String("80"),
					sdInstanceAttrOriginalSrv: aws.String("0 50 80 service1.private.com"),
				},
			},
			"1.2.3.6_0_50_80_service1.private.com": {
				Id: aws.String("1.2.3.6_0_50_80_service1.private.com"),
				Attributes: map[string]*string{
					sdInstanceAttrIPV4:        aws.String("1.2.3.6"),
					sdInstanceAttrPort:        aws.String("80"),
					sdInstanceAttrOriginalSrv: aws.String("0 50 80 service1.private.com"),
				},
			},
			"1.2.3.6_0_50_80_service1v2.private.com": {
				Id: aws.String("1.2.3.6_0_50_80_service1v2.private.com"),
				Attributes: map[string]*string{
					sdInstanceAttrIPV4:        aws.String("1.2.3.6"),
					sdInstanceAttrPort:        aws.String("80"),
					sdInstanceAttrOriginalSrv: aws.String("0 50 80 service1v2.private.com"),
				},
			},
			"1.2.3.7_0_50_80_service1v2.private.com": {
				Id: aws.String("1.2.3.7_0_50_80_service1v2.private.com"),
				Attributes: map[string]*string{
					sdInstanceAttrIPV4:        aws.String("1.2.3.7"),
					sdInstanceAttrPort:        aws.String("80"),
					sdInstanceAttrOriginalSrv: aws.String("0 50 80 service1v2.private.com"),
				},
			},
		},
	}

	expectedEndpoints := []*endpoint.Endpoint{
		{DNSName: "service1.private.com", Targets: endpoint.Targets{"1.2.3.4", "1.2.3.5"}, RecordType: endpoint.RecordTypeA, RecordTTL: 100, Labels: map[string]string{endpoint.AWSSDDescriptionLabel: "owner-id"}},
		{DNSName: "service2.private.com", Targets: endpoint.Targets{"load-balancer.us-east-1.elb.amazonaws.com"}, RecordType: endpoint.RecordTypeCNAME, RecordTTL: 100, Labels: map[string]string{endpoint.AWSSDDescriptionLabel: "owner-id"}},
		{DNSName: "service3.private.com", Targets: endpoint.Targets{"cname.target.com"}, RecordType: endpoint.RecordTypeCNAME, RecordTTL: 80, Labels: map[string]string{endpoint.AWSSDDescriptionLabel: "owner-id"}},
		{DNSName: "service1v2.private.com", Targets: endpoint.Targets{"1.2.3.6", "1.2.3.7"}, RecordType: endpoint.RecordTypeA, RecordTTL: 100, Labels: map[string]string{endpoint.AWSSDDescriptionLabel: "owner-id"}},
		{DNSName: "_http._tcp.service1.private.com", Targets: endpoint.Targets{"0 50 80 service1.private.com"}, RecordType: endpoint.RecordTypeSRV, RecordTTL: 120},
		{DNSName: "_httppartial._tcp.service1.private.com", Targets: endpoint.Targets{"1.2.3.4_0_50_80_service1.private.com"}, RecordType: endpoint.RecordTypeSRV, RecordTTL: 120},
		{DNSName: "_httpwrongport._tcp.service1.private.com", Targets: endpoint.Targets{"1.2.3.4_0_50_80_service1.private.com", "1.2.3.5_0_50_999_service1.private.com"}, RecordType: endpoint.RecordTypeSRV, RecordTTL: 120},
		{DNSName: "_http._tcp.standalone.private.com", Targets: endpoint.Targets{"1.2.3.6_0_50_80_testabc.private.com", "1.2.3.7_0_50_80_testdef.private.com"}, RecordType: endpoint.RecordTypeSRV, RecordTTL: 120},
		{DNSName: "_http._tcp.multiple.private.com", Targets: endpoint.Targets{"0 50 80 service1.private.com", "0 50 8081 service1v2.private.com"}, RecordType: endpoint.RecordTypeSRV, RecordTTL: 120},
		{DNSName: "_http._tcp.multiplepartial.private.com", Targets: endpoint.Targets{"1.2.3.4_0_50_80_service1.private.com", "1.2.3.6_0_50_80_service1.private.com", "0 50 80 service1v2.private.com"}, RecordType: endpoint.RecordTypeSRV, RecordTTL: 120},
	}

	api := &AWSSDClientStub{
		namespaces: namespaces,
		services:   services,
		instances:  instances,
	}

	provider := newTestAWSSDProvider(api, endpoint.NewDomainFilter([]string{}), "")

	endpoints, _ := provider.Records(context.Background())

	assert.True(t, testutils.SameEndpoints(expectedEndpoints, endpoints), "expected and actual endpoints don't match, expected=%v, actual=%v", expectedEndpoints, endpoints)
}

func TestAWSSDProvider_ApplyChanges(t *testing.T) {

	// used to test service names >64 characters (handled by targetToInstanceID)
	longServiceName := "longlonglonglonglonglonglonglonglonglonglonglonglonglonglonglonglong"

	// 1. initial endpoints to be created
	getEndpoints := func() []*endpoint.Endpoint {
		ep := []*endpoint.Endpoint{
			{DNSName: "service1.private.com", Targets: endpoint.Targets{"1.2.3.4", "1.2.3.5"}, RecordType: endpoint.RecordTypeA, RecordTTL: 60},
			{DNSName: longServiceName + ".private.com", Targets: endpoint.Targets{"1.2.3.7", "1.2.3.8"}, RecordType: endpoint.RecordTypeA, RecordTTL: 60},
			{DNSName: "_http._tcp.service1.private.com", Targets: endpoint.Targets{"0 50 80 service1.private.com"}, RecordType: endpoint.RecordTypeSRV, RecordTTL: 120},
			{DNSName: "service2.private.com", Targets: endpoint.Targets{"load-balancer.us-east-1.elb.amazonaws.com"}, RecordType: endpoint.RecordTypeCNAME, RecordTTL: 80},
			{DNSName: "service3.private.com", Targets: endpoint.Targets{"cname.target.com"}, RecordType: endpoint.RecordTypeCNAME, RecordTTL: 100},
			{DNSName: "service1.privatetwo.com", Targets: endpoint.Targets{"5.6.7.8", "5.6.7.9"}, RecordType: endpoint.RecordTypeA, RecordTTL: 60},
			{DNSName: "_http._tcp.service1.privatetwo.com", Targets: endpoint.Targets{"0 50 80 service1.privatetwo.com"}, RecordType: endpoint.RecordTypeSRV, RecordTTL: 120},
			{DNSName: "_http._tcp.multiple.private.com", Targets: endpoint.Targets{"0 50 80 service1.private.com", "0 50 8081 " + longServiceName + ".private.com"}, RecordType: endpoint.RecordTypeSRV, RecordTTL: 120},
			{DNSName: "_httpsecond._tcp.service1.private.com", Targets: endpoint.Targets{"0 50 8080 service1.private.com"}, RecordType: endpoint.RecordTypeSRV, RecordTTL: 120},
		}
		return ep
	}

	// 2. updates to be applied to initial endpoints
	getUpdates := func() ([]*endpoint.Endpoint, []*endpoint.Endpoint) {
		epOld := []*endpoint.Endpoint{
			{DNSName: "service1.private.com", Targets: endpoint.Targets{"1.2.3.4", "1.2.3.5"}, RecordType: endpoint.RecordTypeA, RecordTTL: 60},
			{DNSName: "_http._tcp.service1.private.com", Targets: endpoint.Targets{"1.2.3.4_0_50_80_service1.private.com", "1.2.3.5_0_50_80_service1.private.com"}, RecordType: endpoint.RecordTypeSRV, RecordTTL: 120},
			{DNSName: "_httpsecond._tcp.service1.private.com", Targets: endpoint.Targets{"1.2.3.4_0_50_8080_service1.private.com", "1.2.3.5_0_50_8080_service1.private.com"}, RecordType: endpoint.RecordTypeSRV, RecordTTL: 120},
			{DNSName: longServiceName + ".private.com", Targets: endpoint.Targets{"1.2.3.7", "1.2.3.8"}, RecordType: endpoint.RecordTypeA, RecordTTL: 60},
			{DNSName: "_http._tcp.multiple.private.com", Targets: endpoint.Targets{"1.2.3.4_0_50_80_service1.private.com", "1.2.3.5_0_50_80_service1.private.com"}, RecordType: endpoint.RecordTypeSRV, RecordTTL: 120},
		}
		epNew := []*endpoint.Endpoint{
			{DNSName: "service1.private.com", Targets: endpoint.Targets{"1.2.3.4", "1.2.3.5", "1.2.3.6"}, RecordType: endpoint.RecordTypeA, RecordTTL: 100},
			{DNSName: "_http._tcp.service1.private.com", Targets: endpoint.Targets{"0 10 80 service1.private.com"}, RecordType: endpoint.RecordTypeSRV, RecordTTL: 120},
			{DNSName: "_httpsecond._tcp.service1.private.com", Targets: endpoint.Targets{"0 50 8080 service1.private.com"}, RecordType: endpoint.RecordTypeSRV, RecordTTL: 240},
			{DNSName: longServiceName + ".private.com", Targets: endpoint.Targets{}, RecordType: endpoint.RecordTypeA, RecordTTL: 60},
			{DNSName: "_http._tcp.multiple.private.com", Targets: endpoint.Targets{"0 50 80 service1.private.com"}, RecordType: endpoint.RecordTypeSRV, RecordTTL: 120},
		}
		return epOld, epNew
	}

	// 3. expected endpoints after the updates
	getEndpointsAfterUpdate := func() []*endpoint.Endpoint {
		ep := []*endpoint.Endpoint{
			{DNSName: "service1.private.com", Targets: endpoint.Targets{"1.2.3.4", "1.2.3.5", "1.2.3.6"}, RecordType: endpoint.RecordTypeA, RecordTTL: 100},
			{DNSName: "_http._tcp.service1.private.com", Targets: endpoint.Targets{"0 10 80 service1.private.com"}, RecordType: endpoint.RecordTypeSRV, RecordTTL: 120},
			{DNSName: "service2.private.com", Targets: endpoint.Targets{"load-balancer.us-east-1.elb.amazonaws.com"}, RecordType: endpoint.RecordTypeCNAME, RecordTTL: 80},
			{DNSName: "service3.private.com", Targets: endpoint.Targets{"cname.target.com"}, RecordType: endpoint.RecordTypeCNAME, RecordTTL: 100},
			{DNSName: "service1.privatetwo.com", Targets: endpoint.Targets{"5.6.7.8", "5.6.7.9"}, RecordType: endpoint.RecordTypeA, RecordTTL: 60},
			{DNSName: "_http._tcp.service1.privatetwo.com", Targets: endpoint.Targets{"0 50 80 service1.privatetwo.com"}, RecordType: endpoint.RecordTypeSRV, RecordTTL: 120},
			{DNSName: "_http._tcp.multiple.private.com", Targets: endpoint.Targets{"0 50 80 service1.private.com", "1.2.3.7_0_50_8081_" + longServiceName + ".private.com", "1.2.3.8_0_50_8081_" + longServiceName + ".private.com"}, RecordType: endpoint.RecordTypeSRV, RecordTTL: 120},
			{DNSName: "_httpsecond._tcp.service1.private.com", Targets: endpoint.Targets{"0 50 8080 service1.private.com"}, RecordType: endpoint.RecordTypeSRV, RecordTTL: 240},
		}
		return ep
	}

	// 4. endpoints to be deleted (all except orphan)
	getDeletes := func() []*endpoint.Endpoint {
		ep := getEndpointsAfterUpdate()
		ep = ep[:7]
		return ep
	}

	// 5. expected orphan endpoints after deletion
	setOrphanEndpoints := func() []*endpoint.Endpoint {
		ep := []*endpoint.Endpoint{
			{DNSName: "_httpsecond._tcp.service1.private.com", Targets: endpoint.Targets{"1.2.3.4_0_50_8080_service1.private.com", "1.2.3.5_0_50_8080_service1.private.com", "1.2.3.6_0_50_8080_service1.private.com"}, RecordType: endpoint.RecordTypeSRV, RecordTTL: 240},
		}
		return ep
	}

	namespaces := map[string]*sd.Namespace{
		"private": {
			Id:   aws.String("private"),
			Name: aws.String("private.com"),
			Type: aws.String(sd.NamespaceTypeDnsPrivate),
		}, "privatetwo": {
			Id:   aws.String("privatetwo"),
			Name: aws.String("privatetwo.com"),
			Type: aws.String(sd.NamespaceTypeDnsPrivate),
		},
	}

	api := &AWSSDClientStub{
		namespaces: namespaces,
		services:   make(map[string]map[string]*sd.Service),
		instances:  make(map[string]map[string]*sd.Instance),
	}

	expectedEndpoints := []*endpoint.Endpoint{}
	expectedEndpoints = getEndpoints()

	provider := newTestAWSSDProvider(api, endpoint.NewDomainFilter([]string{}), "")

	ctx := context.Background()

	// apply creates
	provider.ApplyChanges(ctx, &plan.Changes{
		Create: expectedEndpoints,
	})

	// SRV record targets are converted by ApplyChanges (from hostname to IP). Reset endpoints is needed
	expectedEndpoints = getEndpoints()

	// make sure services were created
	assert.Len(t, api.services["private"], 7)
	assert.Len(t, api.services["privatetwo"], 2)
	existingServices, _ := provider.ListServicesByNamespaceID(namespaces["private"].Id)
	assert.NotNil(t, existingServices["service1"])
	assert.NotNil(t, existingServices["_http._tcp.service1"])
	assert.NotNil(t, existingServices["service2"])
	assert.NotNil(t, existingServices["service3"])
	assert.NotNil(t, existingServices[longServiceName])
	assert.NotNil(t, existingServices["_http._tcp.multiple"])
	existingServicesTwo, _ := provider.ListServicesByNamespaceID(namespaces["privatetwo"].Id)
	assert.NotNil(t, existingServicesTwo["service1"])
	assert.NotNil(t, existingServicesTwo["_http._tcp.service1"])
	// make sure instances were registered
	endpoints, _ := provider.Records(ctx)
	assert.True(t, testutils.SameEndpoints(expectedEndpoints, endpoints), "expected and actual endpoints don't match, expected=%v, actual=%v", expectedEndpoints, endpoints)

	// apply updates
	upOld, upNew := getUpdates()
	ctx = context.Background()
	provider.ApplyChanges(ctx, &plan.Changes{
		UpdateOld: upOld,
		UpdateNew: upNew,
	})
	// make sure instances were updated
	expectedEndpoints = getEndpointsAfterUpdate()
	endpoints, _ = provider.Records(ctx)
	assert.True(t, testutils.SameEndpoints(expectedEndpoints, endpoints), "expected and actual (UPDATED) endpoints don't match, expected=%v, actual=%v", expectedEndpoints, endpoints)

	// delete all but orphan endpoints (test for orphan SRV edpoint)
	orphanEndpoints := setOrphanEndpoints()
	expectedEndpoints = getDeletes()
	ctx = context.Background()
	provider.ApplyChanges(ctx, &plan.Changes{
		Delete: expectedEndpoints,
	})
	// make sure all instances except orphan are gone
	endpoints, _ = provider.Records(ctx)
	assert.True(t, testutils.SameEndpoints(orphanEndpoints, endpoints), "expected and actual (ORPHAN) endpoints don't match, expected=%v, actual=%v", orphanEndpoints, endpoints)

	//delete orphan endpoints
	ctx = context.Background()
	provider.ApplyChanges(ctx, &plan.Changes{
		Delete: orphanEndpoints,
	})
	// make sure all instances are gone
	endpoints, _ = provider.Records(ctx)
	assert.Empty(t, endpoints)
}

func TestAWSSDProvider_ListNamespaces(t *testing.T) {
	namespaces := map[string]*sd.Namespace{
		"private": {
			Id:   aws.String("private"),
			Name: aws.String("private.com"),
			Type: aws.String(sd.NamespaceTypeDnsPrivate),
		},
		"public": {
			Id:   aws.String("public"),
			Name: aws.String("public.com"),
			Type: aws.String(sd.NamespaceTypeDnsPublic),
		},
	}

	api := &AWSSDClientStub{
		namespaces: namespaces,
	}

	for _, tc := range []struct {
		msg                 string
		domainFilter        endpoint.DomainFilter
		namespaceTypeFilter string
		expectedNamespaces  []*sd.NamespaceSummary
	}{
		{"public filter", endpoint.NewDomainFilter([]string{}), "public", []*sd.NamespaceSummary{namespaceToNamespaceSummary(namespaces["public"])}},
		{"private filter", endpoint.NewDomainFilter([]string{}), "private", []*sd.NamespaceSummary{namespaceToNamespaceSummary(namespaces["private"])}},
		{"domain filter", endpoint.NewDomainFilter([]string{"public.com"}), "", []*sd.NamespaceSummary{namespaceToNamespaceSummary(namespaces["public"])}},
		{"non-existing domain", endpoint.NewDomainFilter([]string{"xxx.com"}), "", []*sd.NamespaceSummary{}},
	} {
		provider := newTestAWSSDProvider(api, tc.domainFilter, tc.namespaceTypeFilter)

		result, err := provider.ListNamespaces()
		require.NoError(t, err)

		expectedMap := make(map[string]*sd.NamespaceSummary)
		resultMap := make(map[string]*sd.NamespaceSummary)
		for _, ns := range tc.expectedNamespaces {
			expectedMap[*ns.Id] = ns
		}
		for _, ns := range result {
			resultMap[*ns.Id] = ns
		}

		if !reflect.DeepEqual(resultMap, expectedMap) {
			t.Errorf("AWSSDProvider.ListNamespaces() error = %v, wantErr %v", result, tc.expectedNamespaces)
		}
	}
}

func TestAWSSDProvider_ListServicesByNamespace(t *testing.T) {
	namespaces := map[string]*sd.Namespace{
		"private": {
			Id:   aws.String("private"),
			Name: aws.String("private.com"),
			Type: aws.String(sd.NamespaceTypeDnsPrivate),
		},
		"public": {
			Id:   aws.String("public"),
			Name: aws.String("public.com"),
			Type: aws.String(sd.NamespaceTypeDnsPublic),
		},
	}

	services := map[string]map[string]*sd.Service{
		"private": {
			"srv1": {
				Id:   aws.String("srv1"),
				Name: aws.String("service1"),
			},
			"srv2": {
				Id:   aws.String("srv2"),
				Name: aws.String("service2"),
			},
		},
		"public": {
			"srv3": {
				Id:   aws.String("srv3"),
				Name: aws.String("service3"),
			},
		},
	}

	api := &AWSSDClientStub{
		namespaces: namespaces,
		services:   services,
	}

	for _, tc := range []struct {
		expectedServices map[string]*sd.Service
	}{
		{map[string]*sd.Service{"service1": services["private"]["srv1"], "service2": services["private"]["srv2"]}},
	} {
		provider := newTestAWSSDProvider(api, endpoint.NewDomainFilter([]string{}), "")

		result, err := provider.ListServicesByNamespaceID(namespaces["private"].Id)
		require.NoError(t, err)

		if !reflect.DeepEqual(result, tc.expectedServices) {
			t.Errorf("AWSSDProvider.ListServicesByNamespaceID() error = %v, wantErr %v", result, tc.expectedServices)
		}
	}
}

func TestAWSSDProvider_ListInstancesByService(t *testing.T) {
	namespaces := map[string]*sd.Namespace{
		"private": {
			Id:   aws.String("private"),
			Name: aws.String("private.com"),
			Type: aws.String(sd.NamespaceTypeDnsPrivate),
		},
	}

	services := map[string]map[string]*sd.Service{
		"private": {
			"srv1": {
				Id:   aws.String("srv1"),
				Name: aws.String("service1"),
			},
			"srv2": {
				Id:   aws.String("srv2"),
				Name: aws.String("service2"),
			},
		},
	}

	instances := map[string]map[string]*sd.Instance{
		"srv1": {
			"inst1": {
				Id: aws.String("inst1"),
				Attributes: map[string]*string{
					sdInstanceAttrIPV4: aws.String("1.2.3.4"),
				},
			},
			"inst2": {
				Id: aws.String("inst2"),
				Attributes: map[string]*string{
					sdInstanceAttrIPV4: aws.String("1.2.3.5"),
				},
			},
		},
	}

	api := &AWSSDClientStub{
		namespaces: namespaces,
		services:   services,
		instances:  instances,
	}

	provider := newTestAWSSDProvider(api, endpoint.NewDomainFilter([]string{}), "")

	result, err := provider.ListInstancesByServiceID(services["private"]["srv1"].Id)
	require.NoError(t, err)

	expectedInstances := []*sd.InstanceSummary{instanceToInstanceSummary(instances["srv1"]["inst1"]), instanceToInstanceSummary(instances["srv1"]["inst2"])}

	expectedMap := make(map[string]*sd.InstanceSummary)
	resultMap := make(map[string]*sd.InstanceSummary)
	for _, inst := range expectedInstances {
		expectedMap[*inst.Id] = inst
	}
	for _, inst := range result {
		resultMap[*inst.Id] = inst
	}

	if !reflect.DeepEqual(resultMap, expectedMap) {
		t.Errorf("AWSSDProvider.ListInstancesByServiceID() error = %v, wantErr %v", result, expectedInstances)
	}
}

func TestAWSSDProvider_CreateService(t *testing.T) {
	namespaces := map[string]*sd.Namespace{
		"private": {
			Id:   aws.String("private"),
			Name: aws.String("private.com"),
			Type: aws.String(sd.NamespaceTypeDnsPrivate),
		},
	}

	api := &AWSSDClientStub{
		namespaces: namespaces,
		services:   make(map[string]map[string]*sd.Service),
	}

	expectedServices := make(map[string]*sd.Service)

	provider := newTestAWSSDProvider(api, endpoint.NewDomainFilter([]string{}), "")

	// A type
	provider.CreateService(aws.String("private"), aws.String("A-srv"), &endpoint.Endpoint{
		RecordType: endpoint.RecordTypeA,
		RecordTTL:  60,
		Targets:    endpoint.Targets{"1.2.3.4"},
	})
	expectedServices["A-srv"] = &sd.Service{
		Name: aws.String("A-srv"),
		DnsConfig: &sd.DnsConfig{
			RoutingPolicy: aws.String(sd.RoutingPolicyMultivalue),
			DnsRecords: []*sd.DnsRecord{{
				Type: aws.String(sd.RecordTypeA),
				TTL:  aws.Int64(60),
			}},
		},
		NamespaceId: aws.String("private"),
	}

	// CNAME type
	provider.CreateService(aws.String("private"), aws.String("CNAME-srv"), &endpoint.Endpoint{
		RecordType: endpoint.RecordTypeCNAME,
		RecordTTL:  80,
		Targets:    endpoint.Targets{"cname.target.com"},
	})
	expectedServices["CNAME-srv"] = &sd.Service{
		Name: aws.String("CNAME-srv"),
		DnsConfig: &sd.DnsConfig{
			RoutingPolicy: aws.String(sd.RoutingPolicyWeighted),
			DnsRecords: []*sd.DnsRecord{{
				Type: aws.String(sd.RecordTypeCname),
				TTL:  aws.Int64(80),
			}},
		},
		NamespaceId: aws.String("private"),
	}

	// ALIAS type
	provider.CreateService(aws.String("private"), aws.String("ALIAS-srv"), &endpoint.Endpoint{
		RecordType: endpoint.RecordTypeCNAME,
		RecordTTL:  100,
		Targets:    endpoint.Targets{"load-balancer.us-east-1.elb.amazonaws.com"},
	})
	expectedServices["ALIAS-srv"] = &sd.Service{
		Name: aws.String("ALIAS-srv"),
		DnsConfig: &sd.DnsConfig{
			RoutingPolicy: aws.String(sd.RoutingPolicyWeighted),
			DnsRecords: []*sd.DnsRecord{{
				Type: aws.String(sd.RecordTypeA),
				TTL:  aws.Int64(100),
			}},
		},
		NamespaceId: aws.String("private"),
	}

	// SRV type
	provider.CreateService(aws.String("private"), aws.String("SRV-srv"), &endpoint.Endpoint{
		RecordType: endpoint.RecordTypeSRV,
		RecordTTL:  60,
		Targets:    endpoint.Targets{"0 50 80 service1.private.com"},
	})
	expectedServices["SRV-srv"] = &sd.Service{
		Name: aws.String("SRV-srv"),
		DnsConfig: &sd.DnsConfig{
			RoutingPolicy: aws.String(sd.RoutingPolicyMultivalue),
			DnsRecords: []*sd.DnsRecord{{
				Type: aws.String(sd.RecordTypeSrv),
				TTL:  aws.Int64(60),
			}},
		},
		NamespaceId: aws.String("private"),
	}

	validateAWSSDServicesMapsEqual(t, expectedServices, api.services["private"])
}

func validateAWSSDServicesMapsEqual(t *testing.T, expected map[string]*sd.Service, services map[string]*sd.Service) {
	require.Len(t, services, len(expected))

	for _, srv := range services {
		validateAWSSDServicesEqual(t, expected[*srv.Name], srv)
	}
}

func validateAWSSDServicesEqual(t *testing.T, expected *sd.Service, srv *sd.Service) {
	assert.Equal(t, aws.StringValue(expected.Description), aws.StringValue(srv.Description))
	assert.Equal(t, aws.StringValue(expected.Name), aws.StringValue(srv.Name))
	assert.True(t, reflect.DeepEqual(*expected.DnsConfig, *srv.DnsConfig))
}

func TestAWSSDProvider_UpdateService(t *testing.T) {
	namespaces := map[string]*sd.Namespace{
		"private": {
			Id:   aws.String("private"),
			Name: aws.String("private.com"),
			Type: aws.String(sd.NamespaceTypeDnsPrivate),
		},
	}

	services := map[string]map[string]*sd.Service{
		"private": {
			"srv1": {
				Id:   aws.String("srv1"),
				Name: aws.String("service1"),
				DnsConfig: &sd.DnsConfig{
					NamespaceId:   aws.String("private"),
					RoutingPolicy: aws.String(sd.RoutingPolicyMultivalue),
					DnsRecords: []*sd.DnsRecord{{
						Type: aws.String(sd.RecordTypeA),
						TTL:  aws.Int64(60),
					}},
				},
			},
		},
	}

	api := &AWSSDClientStub{
		namespaces: namespaces,
		services:   services,
	}

	provider := newTestAWSSDProvider(api, endpoint.NewDomainFilter([]string{}), "")

	// update service with different TTL
	provider.UpdateService(services["private"]["srv1"], &endpoint.Endpoint{
		RecordType: endpoint.RecordTypeA,
		RecordTTL:  100,
	})

	assert.Equal(t, int64(100), *api.services["private"]["srv1"].DnsConfig.DnsRecords[0].TTL)
}

func TestAWSSDProvider_RegisterInstance(t *testing.T) {
	namespaces := map[string]*sd.Namespace{
		"private": {
			Id:   aws.String("private"),
			Name: aws.String("private.com"),
			Type: aws.String(sd.NamespaceTypeDnsPrivate),
		},
	}

	services := map[string]map[string]*sd.Service{
		"private": {
			"a-srv": {
				Id:   aws.String("a-srv"),
				Name: aws.String("service1"),
				DnsConfig: &sd.DnsConfig{
					NamespaceId:   aws.String("private"),
					RoutingPolicy: aws.String(sd.RoutingPolicyWeighted),
					DnsRecords: []*sd.DnsRecord{{
						Type: aws.String(sd.RecordTypeA),
						TTL:  aws.Int64(60),
					}},
				},
			},
			"cname-srv": {
				Id:   aws.String("cname-srv"),
				Name: aws.String("service2"),
				DnsConfig: &sd.DnsConfig{
					NamespaceId:   aws.String("private"),
					RoutingPolicy: aws.String(sd.RoutingPolicyWeighted),
					DnsRecords: []*sd.DnsRecord{{
						Type: aws.String(sd.RecordTypeCname),
						TTL:  aws.Int64(60),
					}},
				},
			},
			"alias-srv": {
				Id:   aws.String("alias-srv"),
				Name: aws.String("service3"),
				DnsConfig: &sd.DnsConfig{
					NamespaceId:   aws.String("private"),
					RoutingPolicy: aws.String(sd.RoutingPolicyWeighted),
					DnsRecords: []*sd.DnsRecord{{
						Type: aws.String(sd.RecordTypeA),
						TTL:  aws.Int64(60),
					}},
				},
			},
			"srv-srv": {
				Id:   aws.String("srv-srv"),
				Name: aws.String("_http._tcp.service1"),
				DnsConfig: &sd.DnsConfig{
					NamespaceId:   aws.String("private"),
					RoutingPolicy: aws.String(sd.RoutingPolicyWeighted),
					DnsRecords: []*sd.DnsRecord{{
						Type: aws.String(sd.RecordTypeSrv),
						TTL:  aws.Int64(60),
					}},
				},
			},
		},
	}

	api := &AWSSDClientStub{
		namespaces: namespaces,
		services:   services,
		instances:  make(map[string]map[string]*sd.Instance),
	}

	provider := newTestAWSSDProvider(api, endpoint.NewDomainFilter([]string{}), "")

	expectedInstances := make(map[string]*sd.Instance)

	// IP-based instance
	provider.RegisterInstance(services["private"]["a-srv"], &endpoint.Endpoint{
		RecordType: endpoint.RecordTypeA,
		DNSName:    "service1.private.com.",
		RecordTTL:  300,
		Targets:    endpoint.Targets{"1.2.3.4", "1.2.3.5"},
	})
	expectedInstances["1.2.3.4"] = &sd.Instance{
		Id: aws.String("1.2.3.4"),
		Attributes: map[string]*string{
			sdInstanceAttrIPV4: aws.String("1.2.3.4"),
		},
	}
	expectedInstances["1.2.3.5"] = &sd.Instance{
		Id: aws.String("1.2.3.5"),
		Attributes: map[string]*string{
			sdInstanceAttrIPV4: aws.String("1.2.3.5"),
		},
	}

	// AWS ELB instance (ALIAS)
	provider.RegisterInstance(services["private"]["alias-srv"], &endpoint.Endpoint{
		RecordType: endpoint.RecordTypeCNAME,
		DNSName:    "service1.private.com.",
		RecordTTL:  300,
		Targets:    endpoint.Targets{"load-balancer.us-east-1.elb.amazonaws.com", "load-balancer.us-west-2.elb.amazonaws.com"},
	})
	expectedInstances["load-balancer.us-east-1.elb.amazonaws.com"] = &sd.Instance{
		Id: aws.String("load-balancer.us-east-1.elb.amazonaws.com"),
		Attributes: map[string]*string{
			sdInstanceAttrAlias: aws.String("load-balancer.us-east-1.elb.amazonaws.com"),
		},
	}
	expectedInstances["load-balancer.us-west-2.elb.amazonaws.com"] = &sd.Instance{
		Id: aws.String("load-balancer.us-west-2.elb.amazonaws.com"),
		Attributes: map[string]*string{
			sdInstanceAttrAlias: aws.String("load-balancer.us-west-2.elb.amazonaws.com"),
		},
	}

	// AWS NLB instance (ALIAS)
	provider.RegisterInstance(services["private"]["alias-srv"], &endpoint.Endpoint{
		RecordType: endpoint.RecordTypeCNAME,
		DNSName:    "service1.private.com.",
		RecordTTL:  300,
		Targets:    endpoint.Targets{"load-balancer.elb.us-west-2.amazonaws.com"},
	})
	expectedInstances["load-balancer.elb.us-west-2.amazonaws.com"] = &sd.Instance{
		Id: aws.String("load-balancer.elb.us-west-2.amazonaws.com"),
		Attributes: map[string]*string{
			sdInstanceAttrAlias: aws.String("load-balancer.elb.us-west-2.amazonaws.com"),
		},
	}

	// CNAME instance
	provider.RegisterInstance(services["private"]["cname-srv"], &endpoint.Endpoint{
		RecordType: endpoint.RecordTypeCNAME,
		DNSName:    "service2.private.com.",
		RecordTTL:  300,
		Targets:    endpoint.Targets{"cname.target.com"},
	})
	expectedInstances["cname.target.com"] = &sd.Instance{
		Id: aws.String("cname.target.com"),
		Attributes: map[string]*string{
			sdInstanceAttrCname: aws.String("cname.target.com"),
		},
	}

	// IP-port-based SRV instance
	provider.RegisterInstance(services["private"]["srv-srv"], &endpoint.Endpoint{
		RecordType: endpoint.RecordTypeSRV,
		DNSName:    "_http._tcp.service1.private.com.",
		RecordTTL:  300,
		Targets:    endpoint.Targets{"1.2.3.40 50 80", "1.2.3.50 50 80"},
	})
	expectedInstances["1.2.3.40 50 80"] = &sd.Instance{
		Id: aws.String("1.2.3.40 50 80"),
		Attributes: map[string]*string{
			sdInstanceAttrIPV4: aws.String("1.2.3.4"),
			sdInstanceAttrPort: aws.String("80"),
		},
	}
	expectedInstances["1.2.3.50 50 80"] = &sd.Instance{
		Id: aws.String("1.2.3.50 50 80"),
		Attributes: map[string]*string{
			sdInstanceAttrIPV4: aws.String("1.2.3.5"),
			sdInstanceAttrPort: aws.String("80"),
		},
	}

	// validate instances
	for _, srvInst := range api.instances {
		for id, inst := range srvInst {
			if !reflect.DeepEqual(*expectedInstances[id], *inst) {
				t.Errorf("Instances don't match, expected = %v, actual %v", *expectedInstances[id], *inst)
			}
		}
	}
}

func TestAWSSDProvider_DeregisterInstance(t *testing.T) {
	namespaces := map[string]*sd.Namespace{
		"private": {
			Id:   aws.String("private"),
			Name: aws.String("private.com"),
			Type: aws.String(sd.NamespaceTypeDnsPrivate),
		},
	}

	services := map[string]map[string]*sd.Service{
		"private": {
			"srv1": {
				Id:   aws.String("srv1"),
				Name: aws.String("service1"),
			},
		},
	}

	instances := map[string]map[string]*sd.Instance{
		"srv1": {
			"1.2.3.4": {
				Id: aws.String("1.2.3.4"),
				Attributes: map[string]*string{
					sdInstanceAttrIPV4: aws.String("1.2.3.4"),
				},
			},
		},
	}

	api := &AWSSDClientStub{
		namespaces: namespaces,
		services:   services,
		instances:  instances,
	}

	provider := newTestAWSSDProvider(api, endpoint.NewDomainFilter([]string{}), "")

	provider.DeregisterInstance(services["private"]["srv1"], endpoint.NewEndpoint("srv1.private.com.", endpoint.RecordTypeA, "1.2.3.4"))

	assert.Len(t, instances["srv1"], 0)
}

func TestAWSSDProvider_DedupDeletesAndCreates(t *testing.T) {
	namespaces := map[string]*sd.Namespace{
		"private": {
			Id:   aws.String("private"),
			Name: aws.String("private.com"),
			Type: aws.String(sd.NamespaceTypeDnsPrivate),
		},
	}

	services := map[string]map[string]*sd.Service{}

	instances := map[string]map[string]*sd.Instance{}

	deletes := []*endpoint.Endpoint{
		{DNSName: "service1.private.com", Targets: endpoint.Targets{"1.2.3.4", "1.2.3.5"}, RecordType: endpoint.RecordTypeA, RecordTTL: 60},
		{DNSName: "_http._tcp.service1.private.com", Targets: endpoint.Targets{"1.2.3.40 50 80", "1.2.3.50 50 80"}, RecordType: endpoint.RecordTypeSRV, RecordTTL: 120},
		{DNSName: "_httpsecond._tcp.service1.private.com", Targets: endpoint.Targets{"1.2.3.40 50 8080", "1.2.3.50 50 8080"}, RecordType: endpoint.RecordTypeSRV, RecordTTL: 120},
		{DNSName: "_httpthird._tcp.service1.private.com", Targets: endpoint.Targets{"1.2.3.40 50 80", "1.2.3.50 50 80"}, RecordType: endpoint.RecordTypeSRV, RecordTTL: 120},
	}
	creates := []*endpoint.Endpoint{
		{DNSName: "service1.private.com", Targets: endpoint.Targets{"1.2.3.5", "1.2.3.6"}, RecordType: endpoint.RecordTypeA, RecordTTL: 60},
		{DNSName: "_http._tcp.service1.private.com", Targets: endpoint.Targets{"1.2.3.50 50 80", "1.2.3.60 50 80"}, RecordType: endpoint.RecordTypeSRV, RecordTTL: 120},
		{DNSName: "_httpsecond._tcp.service1.private.com", Targets: endpoint.Targets{"1.2.3.40 50 8080", "1.2.3.50 50 8080"}, RecordType: endpoint.RecordTypeSRV, RecordTTL: 120},
		{DNSName: "_httpthird._tcp.service1.private.com", Targets: endpoint.Targets{"1.2.3.40 50 8080", "1.2.3.50 50 8080"}, RecordType: endpoint.RecordTypeSRV, RecordTTL: 120},
	}

	expDedupDeletes := []*endpoint.Endpoint{
		{DNSName: "service1.private.com", Targets: endpoint.Targets{"1.2.3.4"}, RecordType: endpoint.RecordTypeA, RecordTTL: 60},
		{DNSName: "_http._tcp.service1.private.com", Targets: endpoint.Targets{"1.2.3.40 50 80"}, RecordType: endpoint.RecordTypeSRV, RecordTTL: 120},
		{DNSName: "_httpsecond._tcp.service1.private.com", Targets: endpoint.Targets{}, RecordType: endpoint.RecordTypeSRV, RecordTTL: 120},
		{DNSName: "_httpthird._tcp.service1.private.com", Targets: endpoint.Targets{"1.2.3.40 50 80", "1.2.3.50 50 80"}, RecordType: endpoint.RecordTypeSRV, RecordTTL: 120},
	}
	expDedupCreates := []*endpoint.Endpoint{
		{DNSName: "service1.private.com", Targets: endpoint.Targets{"1.2.3.6"}, RecordType: endpoint.RecordTypeA, RecordTTL: 60},
		{DNSName: "_http._tcp.service1.private.com", Targets: endpoint.Targets{"1.2.3.60 50 80"}, RecordType: endpoint.RecordTypeSRV, RecordTTL: 120},
		{DNSName: "_httpsecond._tcp.service1.private.com", Targets: endpoint.Targets{}, RecordType: endpoint.RecordTypeSRV, RecordTTL: 120},
		{DNSName: "_httpthird._tcp.service1.private.com", Targets: endpoint.Targets{"1.2.3.40 50 8080", "1.2.3.50 50 8080"}, RecordType: endpoint.RecordTypeSRV, RecordTTL: 120},
	}

	api := &AWSSDClientStub{
		namespaces: namespaces,
		services:   services,
		instances:  instances,
	}

	provider := newTestAWSSDProvider(api, endpoint.NewDomainFilter([]string{}), "")

	dedupCreates, dedupDeletes, _ := provider.DedupDeletesAndCreates(creates, deletes)

	assert.True(t, testutils.SameEndpoints(expDedupDeletes, dedupDeletes), "expected and actual deduplicated delete endpoints don't match, expected=%v, actual=%v", expDedupDeletes, dedupDeletes)
	assert.True(t, testutils.SameEndpoints(expDedupCreates, dedupCreates), "expected and actual deduplicated create endpoints don't match, expected=%v, actual=%v", expDedupCreates, dedupCreates)
}
