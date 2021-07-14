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
	"strings"

	"crypto/sha256"
	"encoding/hex"
	"fmt"

	"regexp"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/credentials/stscreds"
	"github.com/aws/aws-sdk-go/aws/request"
	"github.com/aws/aws-sdk-go/aws/session"
	sd "github.com/aws/aws-sdk-go/service/servicediscovery"
	"github.com/linki/instrumented_http"
	log "github.com/sirupsen/logrus"

	"sigs.k8s.io/external-dns/endpoint"
	"sigs.k8s.io/external-dns/pkg/apis/externaldns"
	"sigs.k8s.io/external-dns/plan"
	"sigs.k8s.io/external-dns/provider"
)

const (
	sdDefaultRecordTTL = 300

	sdNamespaceTypePublic  = "public"
	sdNamespaceTypePrivate = "private"

	sdInstanceAttrIPV4  = "AWS_INSTANCE_IPV4"
	sdInstanceAttrCname = "AWS_INSTANCE_CNAME"
	sdInstanceAttrAlias = "AWS_ALIAS_DNS_NAME"
)

var (
	// matches ELB with hostname format load-balancer.us-east-1.elb.amazonaws.com
	sdElbHostnameRegex = regexp.MustCompile(`.+\.[^.]+\.elb\.amazonaws\.com$`)

	// matches NLB with hostname format load-balancer.elb.us-east-1.amazonaws.com
	sdNlbHostnameRegex = regexp.MustCompile(`.+\.elb\.[^.]+\.amazonaws\.com$`)
)

// AWSSDClient is the subset of the AWS Cloud Map API that we actually use. Add methods as required.
// Signatures must match exactly. Taken from https://github.com/aws/aws-sdk-go/blob/HEAD/service/servicediscovery/api.go
type AWSSDClient interface {
	CreateService(input *sd.CreateServiceInput) (*sd.CreateServiceOutput, error)
	DeregisterInstance(input *sd.DeregisterInstanceInput) (*sd.DeregisterInstanceOutput, error)
	GetService(input *sd.GetServiceInput) (*sd.GetServiceOutput, error)
	ListInstancesPages(input *sd.ListInstancesInput, fn func(*sd.ListInstancesOutput, bool) bool) error
	ListNamespacesPages(input *sd.ListNamespacesInput, fn func(*sd.ListNamespacesOutput, bool) bool) error
	ListServicesPages(input *sd.ListServicesInput, fn func(*sd.ListServicesOutput, bool) bool) error
	RegisterInstance(input *sd.RegisterInstanceInput) (*sd.RegisterInstanceOutput, error)
	UpdateService(input *sd.UpdateServiceInput) (*sd.UpdateServiceOutput, error)
}

// AWSSDProvider is an implementation of Provider for AWS Cloud Map.
type AWSSDProvider struct {
	provider.BaseProvider
	client AWSSDClient
	dryRun bool
	// only consider namespaces ending in this suffix
	namespaceFilter endpoint.DomainFilter
	// filter namespace by type (private or public)
	namespaceTypeFilter *sd.NamespaceFilter
}

// NewAWSSDProvider initializes a new AWS Cloud Map based Provider.
func NewAWSSDProvider(domainFilter endpoint.DomainFilter, namespaceType string, assumeRole string, dryRun bool) (*AWSSDProvider, error) {
	config := aws.NewConfig()

	config = config.WithHTTPClient(
		instrumented_http.NewClient(config.HTTPClient, &instrumented_http.Callbacks{
			PathProcessor: func(path string) string {
				parts := strings.Split(path, "/")
				return parts[len(parts)-1]
			},
		}),
	)

	sess, err := session.NewSessionWithOptions(session.Options{
		Config:            *config,
		SharedConfigState: session.SharedConfigEnable,
	})
	if err != nil {
		return nil, err
	}

	if assumeRole != "" {
		log.Infof("Assuming role: %s", assumeRole)
		sess.Config.WithCredentials(stscreds.NewCredentials(sess, assumeRole))
	}

	sess.Handlers.Build.PushBack(request.MakeAddToUserAgentHandler("ExternalDNS", externaldns.Version))

	provider := &AWSSDProvider{
		client:              sd.New(sess),
		namespaceFilter:     domainFilter,
		namespaceTypeFilter: newSdNamespaceFilter(namespaceType),
		dryRun:              dryRun,
	}

	return provider, nil
}

// newSdNamespaceFilter initialized AWS SD Namespace Filter based on given string config
func newSdNamespaceFilter(namespaceTypeConfig string) *sd.NamespaceFilter {
	switch namespaceTypeConfig {
	case sdNamespaceTypePublic:
		return &sd.NamespaceFilter{
			Name:   aws.String(sd.NamespaceFilterNameType),
			Values: []*string{aws.String(sd.NamespaceTypeDnsPublic)},
		}
	case sdNamespaceTypePrivate:
		return &sd.NamespaceFilter{
			Name:   aws.String(sd.NamespaceFilterNameType),
			Values: []*string{aws.String(sd.NamespaceTypeDnsPrivate)},
		}
	default:
		return nil
	}
}

// Records returns list of all endpoints.
func (p *AWSSDProvider) Records(ctx context.Context) (endpoints []*endpoint.Endpoint, err error) {
	namespaces, err := p.ListNamespaces()
	if err != nil {
		return nil, err
	}

	for _, ns := range namespaces {
		services, err := p.ListServicesByNamespaceID(ns.Id)
		if err != nil {
			return nil, err
		}

		for _, srv := range services {
			instances, err := p.ListInstancesByServiceID(srv.Id)
			if err != nil {
				return nil, err
			}

			if len(instances) > 0 {
				ep := p.instancesToEndpoint(ns, srv, instances)
				endpoints = append(endpoints, ep)
			}
		}
	}

	return endpoints, nil
}

func (p *AWSSDProvider) instancesToEndpoint(ns *sd.NamespaceSummary, srv *sd.Service, instances []*sd.InstanceSummary) *endpoint.Endpoint {
	// DNS name of the record is a concatenation of service and namespace
	recordName := *srv.Name + "." + *ns.Name

	labels := endpoint.NewLabels()
	labels[endpoint.AWSSDDescriptionLabel] = aws.StringValue(srv.Description)

	newEndpoint := &endpoint.Endpoint{
		DNSName:   recordName,
		RecordTTL: endpoint.TTL(aws.Int64Value(srv.DnsConfig.DnsRecords[0].TTL)),
		Targets:   make(endpoint.Targets, 0, len(instances)),
		Labels:    labels,
	}

	for _, inst := range instances {
		// Ignore any instances that don't match our expected owner label.
		if inst.Attributes[endpoint.OwnerLabelKey] == nil || *inst.Attributes[endpoint.OwnerLabelKey] != *srv.Description {
			log.Debugf("Skipping instance \"%v\" found in service \"%v\", not under management", *inst.Id, *srv.Name)
			continue
		}

		// CNAME
		if inst.Attributes[sdInstanceAttrCname] != nil && aws.StringValue(srv.DnsConfig.DnsRecords[0].Type) == sd.RecordTypeCname {
			newEndpoint.RecordType = endpoint.RecordTypeCNAME
			newEndpoint.Targets = append(newEndpoint.Targets, aws.StringValue(inst.Attributes[sdInstanceAttrCname]))

			// ALIAS
		} else if inst.Attributes[sdInstanceAttrAlias] != nil {
			newEndpoint.RecordType = endpoint.RecordTypeCNAME
			newEndpoint.Targets = append(newEndpoint.Targets, aws.StringValue(inst.Attributes[sdInstanceAttrAlias]))

			// IP-based target
		} else if inst.Attributes[sdInstanceAttrIPV4] != nil {
			newEndpoint.RecordType = endpoint.RecordTypeA
			newEndpoint.Targets = append(newEndpoint.Targets, aws.StringValue(inst.Attributes[sdInstanceAttrIPV4]))
		} else {
			log.Warnf("Invalid instance \"%v\" found in service \"%v\"", inst, srv.Name)
		}
	}

	return newEndpoint
}

// ApplyChanges applies Kubernetes changes in endpoints to AWS API
func (p *AWSSDProvider) ApplyChanges(ctx context.Context, changes *plan.Changes) error {
	// return early if there is nothing to change
	if len(changes.Create) == 0 && len(changes.Delete) == 0 && len(changes.UpdateNew) == 0 {
		log.Info("All records are already up to date")
		return nil
	}

	// convert updates to delete and create operation if applicable (updates not supported)
	creates, deletes := p.updatesToCreates(changes)
	changes.Delete = append(changes.Delete, deletes...)
	changes.Create = append(changes.Create, creates...)

	namespaces, err := p.ListNamespaces()
	if err != nil {
		return err
	}

	// Deletes must be executed first to support update case.
	// When just list of targets is updated `[1.2.3.4] -> [1.2.3.4, 1.2.3.5]` it is translated to:
	// ```
	// deletes = [1.2.3.4]
	// creates = [1.2.3.4, 1.2.3.5]
	// ```
	// then when deletes are executed after creates it will miss the `1.2.3.4` instance.
	err = p.submitDeletes(namespaces, changes.Delete)
	if err != nil {
		return err
	}

	err = p.submitCreates(namespaces, changes.Create)
	if err != nil {
		return err
	}

	return nil
}

func (p *AWSSDProvider) updatesToCreates(changes *plan.Changes) (creates []*endpoint.Endpoint, deletes []*endpoint.Endpoint) {
	updateNewMap := map[string]*endpoint.Endpoint{}
	for _, e := range changes.UpdateNew {
		updateNewMap[e.DNSName] = e
	}

	for _, old := range changes.UpdateOld {
		current := updateNewMap[old.DNSName]

		if !old.Targets.Same(current.Targets) {
			// when targets differ the old instances need to be de-registered first
			deletes = append(deletes, old)
		}

		// always register (or re-register) instance with the current data
		creates = append(creates, current)
	}

	return creates, deletes
}

func (p *AWSSDProvider) submitCreates(namespaces []*sd.NamespaceSummary, changes []*endpoint.Endpoint) error {
	changesByNamespaceID := p.changesByNamespaceID(namespaces, changes)

	for nsID, changeList := range changesByNamespaceID {
		services, err := p.ListServicesByNamespaceID(aws.String(nsID))
		if err != nil {
			return err
		}

		for _, ch := range changeList {
			_, srvName := p.parseHostname(ch.DNSName)

			srv := services[srvName]
			if srv == nil {
				// when service is missing create a new one
				srv, err = p.CreateService(&nsID, &srvName, ch)
				if err != nil {
					return err
				}
				// update local list of services
				services[*srv.Name] = srv
			} else if (ch.RecordTTL.IsConfigured() && *srv.DnsConfig.DnsRecords[0].TTL != int64(ch.RecordTTL)) ||
				aws.StringValue(srv.Description) != ch.Labels[endpoint.AWSSDDescriptionLabel] {
				// update service when TTL or Description differ
				err = p.UpdateService(srv, ch)
				if err != nil {
					return err
				}
			}

			err = p.RegisterInstance(srv, ch)
			if err != nil {
				return err
			}
		}
	}

	return nil
}

func (p *AWSSDProvider) submitDeletes(namespaces []*sd.NamespaceSummary, changes []*endpoint.Endpoint) error {
	changesByNamespaceID := p.changesByNamespaceID(namespaces, changes)

	for nsID, changeList := range changesByNamespaceID {
		services, err := p.ListServicesByNamespaceID(aws.String(nsID))
		if err != nil {
			return err
		}

		for _, ch := range changeList {
			hostname := ch.DNSName
			_, srvName := p.parseHostname(hostname)

			srv := services[srvName]
			if srv == nil {
				return fmt.Errorf("service \"%s\" is missing when trying to delete \"%v\"", srvName, hostname)
			}

			err := p.DeregisterInstance(srv, ch)
			if err != nil {
				return err
			}
		}
	}

	return nil
}

// ListNamespaces returns all namespaces matching defined namespace filter
func (p *AWSSDProvider) ListNamespaces() ([]*sd.NamespaceSummary, error) {
	namespaces := make([]*sd.NamespaceSummary, 0)

	f := func(resp *sd.ListNamespacesOutput, lastPage bool) bool {
		for _, ns := range resp.Namespaces {
			if !p.namespaceFilter.Match(aws.StringValue(ns.Name)) {
				continue
			}
			namespaces = append(namespaces, ns)
		}

		return true
	}

	err := p.client.ListNamespacesPages(&sd.ListNamespacesInput{
		Filters: []*sd.NamespaceFilter{p.namespaceTypeFilter},
	}, f)
	if err != nil {
		return nil, err
	}

	return namespaces, nil
}

// ListServicesByNamespaceID returns list of services in given namespace. Returns map[srv_name]*sd.Service
func (p *AWSSDProvider) ListServicesByNamespaceID(namespaceID *string) (map[string]*sd.Service, error) {
	serviceIds := make([]*string, 0)

	f := func(resp *sd.ListServicesOutput, lastPage bool) bool {
		for _, srv := range resp.Services {
			serviceIds = append(serviceIds, srv.Id)
		}

		return true
	}

	err := p.client.ListServicesPages(&sd.ListServicesInput{
		Filters: []*sd.ServiceFilter{{
			Name:   aws.String(sd.ServiceFilterNameNamespaceId),
			Values: []*string{namespaceID},
		}},
	}, f)
	if err != nil {
		return nil, err
	}

	// get detail of each listed service
	services := make(map[string]*sd.Service)
	for _, serviceID := range serviceIds {
		service, err := p.GetServiceDetail(serviceID)
		if err != nil {
			return nil, err
		}

		services[aws.StringValue(service.Name)] = service
	}

	return services, nil
}

// GetServiceDetail returns detail of given service
func (p *AWSSDProvider) GetServiceDetail(serviceID *string) (*sd.Service, error) {
	output, err := p.client.GetService(&sd.GetServiceInput{
		Id: serviceID,
	})
	if err != nil {
		return nil, err
	}

	return output.Service, nil
}

// ListInstancesByServiceID returns list of instances registered in given service.
func (p *AWSSDProvider) ListInstancesByServiceID(serviceID *string) ([]*sd.InstanceSummary, error) {
	instances := make([]*sd.InstanceSummary, 0)

	f := func(resp *sd.ListInstancesOutput, lastPage bool) bool {
		instances = append(instances, resp.Instances...)

		return true
	}

	err := p.client.ListInstancesPages(&sd.ListInstancesInput{
		ServiceId: serviceID,
	}, f)
	if err != nil {
		return nil, err
	}

	return instances, nil
}

// CreateService creates a new service in AWS API. Returns the created service.
func (p *AWSSDProvider) CreateService(namespaceID *string, srvName *string, ep *endpoint.Endpoint) (*sd.Service, error) {
	log.Infof("Creating a new service \"%s\" in \"%s\" namespace", *srvName, *namespaceID)

	srvType := p.serviceTypeFromEndpoint(ep)
	routingPolicy := p.routingPolicyFromEndpoint(ep)

	ttl := int64(sdDefaultRecordTTL)
	if ep.RecordTTL.IsConfigured() {
		ttl = int64(ep.RecordTTL)
	}

	if !p.dryRun {
		out, err := p.client.CreateService(&sd.CreateServiceInput{
			Name:        srvName,
			Description: aws.String(ep.Labels[endpoint.AWSSDDescriptionLabel]),
			DnsConfig: &sd.DnsConfig{
				RoutingPolicy: aws.String(routingPolicy),
				DnsRecords: []*sd.DnsRecord{{
					Type: aws.String(srvType),
					TTL:  aws.Int64(ttl),
				}},
			},
			NamespaceId: namespaceID,
		})
		if err != nil {
			return nil, err
		}

		return out.Service, nil
	}

	// return mock service summary in case of dry run
	return &sd.Service{Id: aws.String("dry-run-service"), Name: aws.String("dry-run-service")}, nil
}

// UpdateService updates the specified service with information from provided endpoint.
func (p *AWSSDProvider) UpdateService(service *sd.Service, ep *endpoint.Endpoint) error {
	log.Infof("Updating service \"%s\"", *service.Name)

	srvType := p.serviceTypeFromEndpoint(ep)

	ttl := int64(sdDefaultRecordTTL)
	if ep.RecordTTL.IsConfigured() {
		ttl = int64(ep.RecordTTL)
	}

	if !p.dryRun {
		_, err := p.client.UpdateService(&sd.UpdateServiceInput{
			Id: service.Id,
			Service: &sd.ServiceChange{
				Description: aws.String(ep.Labels[endpoint.AWSSDDescriptionLabel]),
				DnsConfig: &sd.DnsConfigChange{
					DnsRecords: []*sd.DnsRecord{{
						Type: aws.String(srvType),
						TTL:  aws.Int64(ttl),
					}},
				}}})
		if err != nil {
			return err
		}
	}

	return nil
}

// RegisterInstance creates a new instance in given service.
func (p *AWSSDProvider) RegisterInstance(service *sd.Service, ep *endpoint.Endpoint) error {
	for _, target := range ep.Targets {
		log.Infof("Registering a new instance \"%s\" for service \"%s\" (%s)", target, *service.Name, *service.Id)

		attr := make(map[string]*string)

		if ep.RecordType == endpoint.RecordTypeCNAME {
			if p.isAWSLoadBalancer(target) {
				attr[sdInstanceAttrAlias] = aws.String(target)
			} else {
				attr[sdInstanceAttrCname] = aws.String(target)
			}
		} else if ep.RecordType == endpoint.RecordTypeA {
			attr[sdInstanceAttrIPV4] = aws.String(target)
		} else {
			return fmt.Errorf("invalid endpoint type (%v)", ep)
		}

		// Add the owner label to the instance's attributes.
		attr[endpoint.OwnerLabelKey] = aws.String(ep.Labels[endpoint.AWSSDDescriptionLabel])

		// Add any additional target labels to the instance's attributes.
		for key, value := range ep.TargetLabels {
			attr[key] = aws.String(value)
		}

		if !p.dryRun {
			_, err := p.client.RegisterInstance(&sd.RegisterInstanceInput{
				ServiceId:  service.Id,
				Attributes: attr,
				InstanceId: aws.String(p.targetToInstanceID(target)),
			})
			if err != nil {
				return err
			}
		}
	}

	return nil
}

// DeregisterInstance removes an instance from given service.
func (p *AWSSDProvider) DeregisterInstance(service *sd.Service, ep *endpoint.Endpoint) error {
	for _, target := range ep.Targets {
		log.Infof("De-registering an instance \"%s\" for service \"%s\" (%s)", target, *service.Name, *service.Id)

		if !p.dryRun {
			_, err := p.client.DeregisterInstance(&sd.DeregisterInstanceInput{
				InstanceId: aws.String(p.targetToInstanceID(target)),
				ServiceId:  service.Id,
			})
			if err != nil {
				return err
			}
		}
	}

	return nil
}

// Instance ID length is limited by AWS API to 64 characters. For longer strings SHA-256 hash will be used instead of
// the verbatim target to limit the length.
func (p *AWSSDProvider) targetToInstanceID(target string) string {
	if len(target) > 64 {
		hash := sha256.Sum256([]byte(strings.ToLower(target)))
		return hex.EncodeToString(hash[:])
	}

	return strings.ToLower(target)
}

// nolint: deadcode
// used from unit test
func namespaceToNamespaceSummary(namespace *sd.Namespace) *sd.NamespaceSummary {
	if namespace == nil {
		return nil
	}

	return &sd.NamespaceSummary{
		Id:   namespace.Id,
		Type: namespace.Type,
		Name: namespace.Name,
		Arn:  namespace.Arn,
	}
}

// nolint: deadcode
// used from unit test
func serviceToServiceSummary(service *sd.Service) *sd.ServiceSummary {
	if service == nil {
		return nil
	}

	return &sd.ServiceSummary{
		Name:          service.Name,
		Id:            service.Id,
		Arn:           service.Arn,
		Description:   service.Description,
		InstanceCount: service.InstanceCount,
	}
}

// nolint: deadcode
// used from unit test
func instanceToInstanceSummary(instance *sd.Instance) *sd.InstanceSummary {
	if instance == nil {
		return nil
	}

	return &sd.InstanceSummary{
		Id:         instance.Id,
		Attributes: instance.Attributes,
	}
}

func (p *AWSSDProvider) changesByNamespaceID(namespaces []*sd.NamespaceSummary, changes []*endpoint.Endpoint) map[string][]*endpoint.Endpoint {
	changesByNsID := make(map[string][]*endpoint.Endpoint)

	for _, ns := range namespaces {
		changesByNsID[*ns.Id] = []*endpoint.Endpoint{}
	}

	for _, c := range changes {
		// trim the trailing dot from hostname if any
		hostname := strings.TrimSuffix(c.DNSName, ".")
		nsName, _ := p.parseHostname(hostname)

		matchingNamespaces := matchingNamespaces(nsName, namespaces)
		if len(matchingNamespaces) == 0 {
			log.Warnf("Skipping record %s because no namespace matching record DNS Name was detected ", c.String())
			continue
		}
		for _, ns := range matchingNamespaces {
			changesByNsID[*ns.Id] = append(changesByNsID[*ns.Id], c)
		}
	}

	// separating a change could lead to empty sub changes, remove them here.
	for zone, change := range changesByNsID {
		if len(change) == 0 {
			delete(changesByNsID, zone)
		}
	}

	return changesByNsID
}

// returns list of all namespaces matching given hostname
func matchingNamespaces(hostname string, namespaces []*sd.NamespaceSummary) []*sd.NamespaceSummary {
	matchingNamespaces := make([]*sd.NamespaceSummary, 0)

	for _, ns := range namespaces {
		if *ns.Name == hostname {
			matchingNamespaces = append(matchingNamespaces, ns)
		}
	}

	return matchingNamespaces
}

// parse hostname to namespace (domain) and service
func (p *AWSSDProvider) parseHostname(hostname string) (namespace string, service string) {
	parts := strings.Split(hostname, ".")
	service = parts[0]
	namespace = strings.Join(parts[1:], ".")
	return
}

// determine service routing policy based on endpoint type
func (p *AWSSDProvider) routingPolicyFromEndpoint(ep *endpoint.Endpoint) string {
	if ep.RecordType == endpoint.RecordTypeA {
		return sd.RoutingPolicyMultivalue
	}

	return sd.RoutingPolicyWeighted
}

// determine service type (A, CNAME) from given endpoint
func (p *AWSSDProvider) serviceTypeFromEndpoint(ep *endpoint.Endpoint) string {
	if ep.RecordType == endpoint.RecordTypeCNAME {
		// FIXME service type is derived from the first target only. Theoretically this may be problem.
		// But I don't see a scenario where one endpoint contains targets of different types.
		if p.isAWSLoadBalancer(ep.Targets[0]) {
			// ALIAS target uses DNS record type of A
			return sd.RecordTypeA
		}
		return sd.RecordTypeCname
	}
	return sd.RecordTypeA
}

// determine if a given hostname belongs to an AWS load balancer
func (p *AWSSDProvider) isAWSLoadBalancer(hostname string) bool {
	matchElb := sdElbHostnameRegex.MatchString(hostname)
	matchNlb := sdNlbHostnameRegex.MatchString(hostname)

	return matchElb || matchNlb
}
