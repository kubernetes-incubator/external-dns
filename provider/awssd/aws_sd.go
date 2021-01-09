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

	sdInstanceAttrIPV4        = "AWS_INSTANCE_IPV4"
	sdInstanceAttrCname       = "AWS_INSTANCE_CNAME"
	sdInstanceAttrAlias       = "AWS_ALIAS_DNS_NAME"
	sdInstanceAttrPort        = "AWS_INSTANCE_PORT"
	sdInstanceAttrOriginalSrv = "externaldns_originalsrv_target"
)

var (
	// matches ELB with hostname format load-balancer.us-east-1.elb.amazonaws.com
	sdElbHostnameRegex = regexp.MustCompile(`.+\.[^.]+\.elb\.amazonaws\.com$`)

	// matches NLB with hostname format load-balancer.elb.us-east-1.amazonaws.com
	sdNlbHostnameRegex = regexp.MustCompile(`.+\.elb\.[^.]+\.amazonaws\.com$`)

	// matches a target of an SRV endponit in the format "priority weight port hostname", e.g. "0 50 80 example.com",
	// as originally sourced by external-dns to ApplyChanges.
	sdSrvHostTargetRegex = regexp.MustCompile(`^[0-9]{1,5} [0-9]{1,5} [0-9]{1,5} [^\s]+$`)

	// matches IP plus original SRV target: "IP_priority_weight_port_hostname". This is a provider-specific format,
	// IP-based targets are needed for SRV records (host-based targets are not supported by awssd).
	sdSrvIPTargetRegex = regexp.MustCompile(`^[0-9]{1,3}\.[0-9]{1,3}\.[0-9]{1,3}\.[0-9]{1,3}_[0-9]{1,5}_[0-9]{1,5}_[0-9]{1,5}_[^\s]+$`)
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

type srvEpHostnameToIP func(*endpoint.Endpoint, map[string]*sd.Service) error

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

// newSdNamespaceFilter initialized AWS SD Namespace Filter based on given string config.
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
		nsEndpoints := make([]*endpoint.Endpoint, 0)
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
				nsEndpoints = append(nsEndpoints, ep)
			}
		}
		err = p.srvEndpointsIPToHost(&nsEndpoints)
		if err != nil {
			return nil, err
		}
		endpoints = append(endpoints, nsEndpoints...)
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
		// CNAME
		if inst.Attributes[sdInstanceAttrCname] != nil && aws.StringValue(srv.DnsConfig.DnsRecords[0].Type) == sd.RecordTypeCname {
			newEndpoint.RecordType = endpoint.RecordTypeCNAME
			newEndpoint.Targets = append(newEndpoint.Targets, aws.StringValue(inst.Attributes[sdInstanceAttrCname]))

			// ALIAS
		} else if inst.Attributes[sdInstanceAttrAlias] != nil {
			newEndpoint.RecordType = endpoint.RecordTypeCNAME
			newEndpoint.Targets = append(newEndpoint.Targets, aws.StringValue(inst.Attributes[sdInstanceAttrAlias]))

			// SRV
		} else if inst.Attributes[sdInstanceAttrOriginalSrv] != nil && aws.StringValue(srv.DnsConfig.DnsRecords[0].Type) == sd.RecordTypeSrv {
			newEndpoint.RecordType = endpoint.RecordTypeSRV
			newEndpoint.Targets = append(newEndpoint.Targets, p.srvIPTargetCombine(
				aws.StringValue(inst.Attributes[sdInstanceAttrIPV4]), aws.StringValue(inst.Attributes[sdInstanceAttrOriginalSrv])))

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

//for each endpoint of type SRV, convert n IP-based targets to 1 host-based target, only when:
// (1) IP targets of SRV endpoint are equal to all IP targets of the corresponding A endpoint, and
// (2) IP targets of SRV endpoint have the same: "priority weight port host" (as originally sourced by externaldns to ApplyChanges)
// this is needed because awssd supports IP-based target but not host-based target when record type is SRV.
// https://docs.aws.amazon.com/cloud-map/latest/api/API_RegisterInstance.html#cloudmap-RegisterInstance-request-Attributes
func (p *AWSSDProvider) srvEndpointsIPToHost(endpoints *[]*endpoint.Endpoint) error {
	// new map of A records by DNS name
	aEpMap := map[string]*endpoint.Endpoint{}
	for _, e := range *endpoints {
		if e.RecordType == endpoint.RecordTypeA {
			aEpMap[e.DNSName] = e
		}
	}

	for _, e := range *endpoints {
		// skip non-SRV endpoints
		if e.RecordType != endpoint.RecordTypeSRV {
			continue
		}

		// new map of IP-based records by "priority weight port host"
		srvTgMap := map[string]endpoint.Targets{}
		for _, tgt := range e.Targets {
			_, origTgt, err := p.srvIPTargetUncombine(tgt)
			if err != nil {
				return err
			}
			if srvTgMap[origTgt] == nil {
				srvTgMap[origTgt] = make(endpoint.Targets, 0)
			}
			srvTgMap[origTgt] = append(srvTgMap[origTgt], tgt)
		}

		convertedTargets := make(endpoint.Targets, 0)

		// loop "priority weight port host" (i.e. originally sourced targets)
		for origTgt, targets := range srvTgMap {
			alignedToA := true
			var err error
			_, host, _, _, err := p.srvHostTargetSplit(origTgt)
			if err != nil {
				return err
			}
			aSrv, ok := aEpMap[host]
			// if no corresponding A record is found, or SRV targets count is different than A targets count, then skip this targets group...
			if !ok || len(targets) != len(aSrv.Targets) {
				alignedToA = false
			} else {
				for _, target := range targets {
					ip, _, err := p.srvIPTargetUncombine(target)
					if err != nil {
						return err
					}
					// if target IPs in SRV endpoint are different than those in A endpoint, then skip this targets group...
					if !p.sliceContainsString(aSrv.Targets, ip) {
						alignedToA = false
						break
					}
				}
			}

			if alignedToA {
				//convert n IP-based targets to 1 host-based target
				convertedTargets = append(convertedTargets, origTgt)
			} else {
				//keep n IP-based targets (as these targets are not aligned with those of a corresponding A record)
				convertedTargets = append(convertedTargets, targets...)
			}
		}
		e.Targets = convertedTargets
	}

	return nil
}

// ApplyChanges applies Kubernetes changes in endpoints to AWS API.
func (p *AWSSDProvider) ApplyChanges(ctx context.Context, changes *plan.Changes) error {
	// return early if there is nothing to change
	if len(changes.Create) == 0 && len(changes.Delete) == 0 && len(changes.UpdateNew) == 0 {
		log.Info("All records are already up to date")
		return nil
	}

	// split NotSrv and Srv changes, because Srv changes need additional handling
	changesNotSrv, changesSrv := p.splitChangesAndSrvChanges(changes)

	// convert updates to delete and create operation if applicable (updates not supported)
	upCreatesNotSrv, upDeletesNotSRV := p.updatesToCreates(changesNotSrv)
	changesNotSrv.Delete = append(changesNotSrv.Delete, upDeletesNotSRV...)
	changesNotSrv.Create = append(changesNotSrv.Create, upCreatesNotSrv...)

	// remove redundant targets from Create and Delete
	notSrvCreate, notSrvDelete, err := p.DedupDeletesAndCreates(changesNotSrv.Create, changesNotSrv.Delete)
	if err != nil {
		return err
	}

	namespaces, err := p.ListNamespaces()
	if err != nil {
		return err
	}

	err = p.submitDeletes(namespaces, notSrvDelete)
	if err != nil {
		return err
	}

	err = p.submitCreates(namespaces, notSrvCreate)
	if err != nil {
		return err
	}

	// Convert hostname-based targets (as passed to ApplyChanges in some cases for SRV records)
	// to IP-based targets (as supported by AWS servicediscovery for SRV records).
	// srvChangesHostnameToIP must be invoked after submitDeletes and submitCreates of NotSrv records,
	// so that host-based target is converted to IP-based target using the latest IPs from the corresponding A instances.
	err = p.srvChangesHostnameToIP(namespaces, changesSrv)
	if err != nil {
		return err
	}

	// convert updates to delete and create operation if applicable (updates not supported)
	upCreatesSRV, upDeletesSRV := p.updatesToCreates(changesSrv)
	changesSrv.Delete = append(changesSrv.Delete, upDeletesSRV...)
	changesSrv.Create = append(changesSrv.Create, upCreatesSRV...)

	srvCreate, srvDelete, err := p.DedupDeletesAndCreates(changesSrv.Create, changesSrv.Delete)
	if err != nil {
		return err
	}

	err = p.submitDeletes(namespaces, srvDelete)
	if err != nil {
		return err
	}

	err = p.submitCreates(namespaces, srvCreate)
	if err != nil {
		return err
	}

	return nil
}

// Split SRV records from all other record types. Records of type SRV will be handled differently,
// because ApplyChanges receives a host-based target,
// but aws servicediscovery only supports IP-based targets for services of type SRV.
func (p *AWSSDProvider) splitChangesAndSrvChanges(changesAll *plan.Changes) (*plan.Changes, *plan.Changes) {
	changesSrv := &plan.Changes{}
	changesNotSrv := &plan.Changes{}

	applySplit := func(chAll []*endpoint.Endpoint, chNotSrv *[]*endpoint.Endpoint, chSrv *[]*endpoint.Endpoint) {
		for _, change := range chAll {
			if change.RecordType == endpoint.RecordTypeSRV {
				*chSrv = append(*chSrv, change)
			} else {
				*chNotSrv = append(*chNotSrv, change)
			}
		}
	}

	applySplit(changesAll.Create, &changesNotSrv.Create, &changesSrv.Create)
	applySplit(changesAll.UpdateOld, &changesNotSrv.UpdateOld, &changesSrv.UpdateOld)
	applySplit(changesAll.UpdateNew, &changesNotSrv.UpdateNew, &changesSrv.UpdateNew)
	applySplit(changesAll.Delete, &changesNotSrv.Delete, &changesSrv.Delete)

	return changesNotSrv, changesSrv
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

// DedupDeletesAndCreates removes targets that appear identically as both deletes and creates.
// These targets are redundant and result in overlapping API calls. Without deduplication, RegisterInstance could be
// invoked while DeregisterInstance is still in progress in AWS, resulting in failure to register the instance, and
// therefore in service disruption. Redundant targets may have been introduced by updatesToCreates or srvChangesHostnameToIP.
func (p *AWSSDProvider) DedupDeletesAndCreates(creates []*endpoint.Endpoint, deletes []*endpoint.Endpoint) ([]*endpoint.Endpoint, []*endpoint.Endpoint, error) {
	// contains all targets appearing in "deletes", mapped by the DNS name of the respective endpoint
	targetsByDeleteEp := map[string]map[string]bool{}
	// contains all duplicate targets (appearing in both "deletes" and "creates"), mapped by the DNS name of the respective endpoint
	dupTargetsByEp := map[string]map[string]bool{}

	// populate targetsByDeleteEp
	for _, e := range deletes {
		if targetsByDeleteEp[e.DNSName] == nil {
			targetsByDeleteEp[e.DNSName] = map[string]bool{}
		}
		for _, t := range e.Targets {
			if _, ok := targetsByDeleteEp[e.DNSName][t]; !ok {
				targetsByDeleteEp[e.DNSName][t] = true
			}
		}
	}

	// loop create endpoints and remove duplicate targets
	for _, create := range creates {
		// if no delete endpoint for this DNS name, then skip this endpoint
		if targetsByDeleteEp[create.DNSName] == nil {
			continue
		}
		TargetsDelete := targetsByDeleteEp[create.DNSName]
		// i is the length of the deduplicated targets for the endpoint (initial targets count - duplicate targets count)
		i := 0
		// loop all targets in this endpoint
		for _, createTarget := range create.Targets {
			// if the target is not duplicate
			if _, ok := TargetsDelete[createTarget]; !ok {
				// copy the target and increment i
				create.Targets[i] = createTarget
				i++
				// if the target is duplicate, add it to dupTargetsByEp
			} else {
				if dupTargetsByEp[create.DNSName] == nil {
					dupTargetsByEp[create.DNSName] = map[string]bool{}
				}
				dupTargetsByEp[create.DNSName][createTarget] = true
			}
		}
		// cut the slice up to i (count of deduplicated targets)
		create.Targets = create.Targets[:i]
	}

	// loop delete endpoints and remove duplicate targets
	for _, delete := range deletes {
		if dupTargetsByEp[delete.DNSName] == nil {
			continue
		}
		TargetsDup := dupTargetsByEp[delete.DNSName]
		i := 0
		for _, deleteTarget := range delete.Targets {
			if _, ok := TargetsDup[deleteTarget]; !ok {
				delete.Targets[i] = deleteTarget
				i++
			}
		}
		delete.Targets = delete.Targets[:i]
	}

	return creates, deletes, nil
}

// submitCreates initiates endpoint creation
func (p *AWSSDProvider) submitCreates(namespaces []*sd.NamespaceSummary, changes []*endpoint.Endpoint) error {
	changesByNamespaceID := p.changesByNamespaceID(namespaces, changes)

	for nsID, changeList := range changesByNamespaceID {
		services, err := p.ListServicesByNamespaceID(aws.String(nsID))
		if err != nil {
			return err
		}
		for _, ch := range changeList {
			_, srvName := p.parseHostname(ch.DNSName, ch.RecordType)

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

// submitDeletes initiates endpoint deletion
func (p *AWSSDProvider) submitDeletes(namespaces []*sd.NamespaceSummary, changes []*endpoint.Endpoint) error {
	changesByNamespaceID := p.changesByNamespaceID(namespaces, changes)

	for nsID, changeList := range changesByNamespaceID {
		services, err := p.ListServicesByNamespaceID(aws.String(nsID))
		if err != nil {
			return err
		}

		for _, ch := range changeList {
			hostname := ch.DNSName
			_, srvName := p.parseHostname(hostname, ch.RecordType)

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

// srvChangesHostnameToIP, for each endpoint of type SRV, converts a single host-based target
// (as passed to ApplyChanges in some cases for SRV records)
// to multiple IP-based targets (as supported by AWS servicediscovery for SRV records).
// This is needed because awssd supports IP-based target but not canonical host-based target when service is an SRV record.
// https://docs.aws.amazon.com/cloud-map/latest/api/API_RegisterInstance.html#cloudmap-RegisterInstance-request-Attributes
func (p *AWSSDProvider) srvChangesHostnameToIP(namespaces []*sd.NamespaceSummary, changes *plan.Changes) error {
	applyConversion := func(changes []*endpoint.Endpoint, convertFn srvEpHostnameToIP) error {
		chByNsID := p.changesByNamespaceID(namespaces, changes)
		for nsID, changeList := range chByNsID {
			services, err := p.ListServicesByNamespaceID(aws.String(nsID))
			if err != nil {
				return err
			}
			for _, change := range changeList {
				err := convertFn(change, services)
				if err != nil {
					return err
				}
			}
		}
		return nil
	}

	err := applyConversion(changes.Create, p.srvNewEpHostnameToIP)
	if err != nil {
		return err
	}
	err = applyConversion(changes.UpdateNew, p.srvNewEpHostnameToIP)
	if err != nil {
		return err
	}
	err = applyConversion(changes.UpdateOld, p.srvOldEpHostnameToIP)
	if err != nil {
		return err
	}
	err = applyConversion(changes.Delete, p.srvOldEpHostnameToIP)
	if err != nil {
		return err
	}
	return nil
}

// srvNewEpHostnameToIP converts endpoint targets from host-based to ip-based.
// Applies to new endpoints (i.e. Changes.Create & Changes.UpdateNew).
func (p *AWSSDProvider) srvNewEpHostnameToIP(ep *endpoint.Endpoint, services map[string]*sd.Service) error {
	convertedTargets := make(endpoint.Targets, 0)

	for _, hostTarget := range ep.Targets {
		// keep a target if it's already IP-based
		if p.isSrvIPTarget(hostTarget) {
			convertedTargets = append(convertedTargets, hostTarget)
			continue
		}

		_, host, _, _, err := p.srvHostTargetSplit(hostTarget)
		if err != nil {
			return err
		}

		_, aServiceName := p.parseHostname(host, endpoint.RecordTypeA)
		aService, ok := services[aServiceName]
		if !ok || aws.StringValue(aService.DnsConfig.DnsRecords[0].Type) != sd.RecordTypeA {
			return fmt.Errorf("error registering SRV record. The corresponding record (%s) is not an A record", aServiceName)
		}
		aInstances, err := p.ListInstancesByServiceID(aService.Id)
		if err != nil {
			return err
		}

		for _, instance := range aInstances {
			tgt := p.srvIPTargetCombine(aws.StringValue(instance.Attributes[sdInstanceAttrIPV4]), hostTarget)
			convertedTargets = append(convertedTargets, tgt)
		}
	}

	ep.Targets = convertedTargets

	return nil
}

// srvOldEpHostnameToIP converts endpoint targets from host-based to ip-based.
// Applies to new endpoints (i.e. Changes.UpdateOld & Changes.Delete).
func (p *AWSSDProvider) srvOldEpHostnameToIP(ep *endpoint.Endpoint, services map[string]*sd.Service) error {
	_, srvName := p.parseHostname(ep.DNSName, ep.RecordType)
	srvService, exists := services[srvName]
	if !exists || aws.StringValue(srvService.DnsConfig.DnsRecords[0].Type) != sd.RecordTypeSrv {
		return fmt.Errorf("error deregistering SRV record. The corresponding record (%s) is not an SRV record", srvName)
	}
	srvInstances, err := p.ListInstancesByServiceID(srvService.Id)
	if err != nil {
		return err
	}

	convertedTargets := make(endpoint.Targets, 0)

	for _, hostTarget := range ep.Targets {
		// keep a target if it's already an IP-based
		if p.isSrvIPTarget(hostTarget) {
			convertedTargets = append(convertedTargets, hostTarget)
			continue
		}
		if !p.isSrvHostTarget(hostTarget) {
			return fmt.Errorf("error deregistering SRV record. The target (%s) is not a valid SRV target", hostTarget)
		}

		for _, instance := range srvInstances {
			instIP := aws.StringValue(instance.Attributes[sdInstanceAttrIPV4])
			instOrigSrv := aws.StringValue(instance.Attributes[sdInstanceAttrOriginalSrv])
			if hostTarget == instOrigSrv {
				convertedTargets = append(convertedTargets, p.srvIPTargetCombine(instIP, instOrigSrv))
			}
		}
	}
	ep.Targets = convertedTargets
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

// GetServiceDetail returns detail of given service.
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
		} else if ep.RecordType == endpoint.RecordTypeSRV {
			ip, port, _, _, _, err := p.srvIPTargetSplit(target)
			if err != nil {
				return err
			}
			_, origSrvTgt, err := p.srvIPTargetUncombine(target)
			if err != nil {
				return err
			}
			attr[sdInstanceAttrIPV4] = aws.String(ip)
			attr[sdInstanceAttrPort] = aws.String(port)
			attr[sdInstanceAttrOriginalSrv] = aws.String(origSrvTgt)
		} else {
			return fmt.Errorf("invalid endpoint type (%v)", ep)
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
		nsName, _ := p.parseHostname(hostname, c.RecordType)

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
func (p *AWSSDProvider) parseHostname(hostname string, recordtype string) (namespace string, service string) {
	parts := strings.Split(hostname, ".")

	// if the record is an SRV record, matching the _<port>._<protocol>.name
	if recordtype == endpoint.RecordTypeSRV && len(parts) >= 3 && strings.HasPrefix(parts[0], "_") && strings.HasPrefix(parts[1], "_") {
		service = strings.Join(parts[:3], ".")
		namespace = strings.Join(parts[3:], ".")

		// if the record is NOT an SRV record
	} else {
		service = parts[0]
		namespace = strings.Join(parts[1:], ".")
	}

	return
}

// routingPolicyFromEndpoint determines service routing policy based on endpoint type
func (p *AWSSDProvider) routingPolicyFromEndpoint(ep *endpoint.Endpoint) string {
	if ep.RecordType == endpoint.RecordTypeA || ep.RecordType == endpoint.RecordTypeSRV {
		return sd.RoutingPolicyMultivalue
	}

	return sd.RoutingPolicyWeighted
}

// determine service type (A, CNAME, SRV) from given endpoint.
func (p *AWSSDProvider) serviceTypeFromEndpoint(ep *endpoint.Endpoint) string {
	if ep.RecordType == endpoint.RecordTypeCNAME {
		// FIXME service type is derived from the first target only. Theoretically this may be problem.
		// But I don't see a scenario where one endpoint contains targets of different types.
		if p.isAWSLoadBalancer(ep.Targets[0]) {
			// ALIAS target uses DNS record type of A
			return sd.RecordTypeA
		}
		return sd.RecordTypeCname
	} else if ep.RecordType == endpoint.RecordTypeSRV {
		return sd.RecordTypeSrv
	}
	return sd.RecordTypeA
}

// determine if a given hostname belongs to an AWS load balancer
func (p *AWSSDProvider) isAWSLoadBalancer(hostname string) bool {
	matchElb := sdElbHostnameRegex.MatchString(hostname)
	matchNlb := sdNlbHostnameRegex.MatchString(hostname)

	return matchElb || matchNlb
}

func (p *AWSSDProvider) srvIPTargetCombine(ip string, srvTarget string) string {
	srvTarget = strings.Replace(srvTarget, " ", "_", -1)
	return fmt.Sprintf("%s_%s", ip, srvTarget)
}

func (p *AWSSDProvider) srvIPTargetUncombine(target string) (ip string, srvTarget string, err error) {
	if !p.isSrvIPTarget(target) {
		err = fmt.Errorf("endpoint target %s is not an IP-based SRV target", target)
		return
	}
	parts := strings.Split(target, "_")
	ip = parts[0]
	srvTarget = strings.Join(parts[1:], " ")
	return
}

func (p *AWSSDProvider) srvIPTargetSplit(target string) (ip string, port string, host string, prio string, weight string, err error) {
	if !p.isSrvIPTarget(target) {
		err = fmt.Errorf("endpoint target %s is not an IP-based SRV target", target)
		return
	}
	parts := strings.Split(target, "_")
	ip = parts[0]
	port = parts[3]
	host = strings.Join(parts[4:], "_")
	prio = parts[1]
	weight = parts[2]
	return
}

func (p *AWSSDProvider) srvHostTargetSplit(target string) (port string, host string, prio string, weight string, err error) {
	if !p.isSrvHostTarget(target) {
		err = fmt.Errorf("endpoint target %s is not an host-based SRV target", target)
		return
	}
	parts := strings.Split(target, " ")
	port = parts[2]
	host = parts[3]
	prio = parts[0]
	weight = parts[1]
	return
}

func (p *AWSSDProvider) isSrvIPTarget(target string) bool {
	return sdSrvIPTargetRegex.MatchString(target)
}

func (p *AWSSDProvider) isSrvHostTarget(target string) bool {
	return sdSrvHostTargetRegex.MatchString(target)
}

// sliceContainsString determines if a given slice of strings contains a given string.
func (p *AWSSDProvider) sliceContainsString(slice []string, val string) bool {
	for _, item := range slice {
		if item == val {
			return true
		}
	}
	return false
}
