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
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/credentials/stscreds"
	"github.com/aws/aws-sdk-go/aws/request"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/route53"
	"github.com/linki/instrumented_http"
	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"

	"sigs.k8s.io/external-dns/endpoint"
	"sigs.k8s.io/external-dns/plan"
	"sigs.k8s.io/external-dns/provider"
)

const (
	recordTTL = 300
	// From the experiments, it seems that the default MaxItems applied is 100,
	// and that, on the server side, there is a hard limit of 300 elements per page.
	// After a discussion with AWS representants, clients should accept
	// when less items are returned, and still paginate accordingly.
	// As we are using the standard AWS client, this should already be compliant.
	// Hence, ifever AWS decides to raise this limit, we will automatically reduce the pressure on rate limits
	route53PageSize = "300"
	// provider specific key that designates whether an AWS ALIAS record has the EvaluateTargetHealth
	// field set to true.
	providerSpecificAlias                      = "alias"
	providerSpecificTargetHostedZone           = "aws/target-hosted-zone"
	providerSpecificEvaluateTargetHealth       = "aws/evaluate-target-health"
	providerSpecificWeight                     = "aws/weight"
	providerSpecificRegion                     = "aws/region"
	providerSpecificFailover                   = "aws/failover"
	providerSpecificGeolocationContinentCode   = "aws/geolocation-continent-code"
	providerSpecificGeolocationCountryCode     = "aws/geolocation-country-code"
	providerSpecificGeolocationSubdivisionCode = "aws/geolocation-subdivision-code"
	providerSpecificMultiValueAnswer           = "aws/multi-value-answer"
	providerSpecificHealthCheckID              = "aws/health-check-id"
	sameZoneAlias                              = "same-zone"
)

var (
	// see: https://docs.aws.amazon.com/general/latest/gr/elb.html
	canonicalHostedZones = map[string]string{
		// Application Load Balancers and Classic Load Balancers
		"us-east-2.elb.amazonaws.com":         "Z3AADJGX6KTTL2",
		"us-east-1.elb.amazonaws.com":         "Z35SXDOTRQ7X7K",
		"us-west-1.elb.amazonaws.com":         "Z368ELLRRE2KJ0",
		"us-west-2.elb.amazonaws.com":         "Z1H1FL5HABSF5",
		"ca-central-1.elb.amazonaws.com":      "ZQSVJUPU6J1EY",
		"ap-east-1.elb.amazonaws.com":         "Z3DQVH9N71FHZ0",
		"ap-south-1.elb.amazonaws.com":        "ZP97RAFLXTNZK",
		"ap-northeast-2.elb.amazonaws.com":    "ZWKZPGTI48KDX",
		"ap-northeast-3.elb.amazonaws.com":    "Z5LXEXXYW11ES",
		"ap-southeast-1.elb.amazonaws.com":    "Z1LMS91P8CMLE5",
		"ap-southeast-2.elb.amazonaws.com":    "Z1GM3OXH4ZPM65",
		"ap-northeast-1.elb.amazonaws.com":    "Z14GRHDCWA56QT",
		"eu-central-1.elb.amazonaws.com":      "Z215JYRZR1TBD5",
		"eu-west-1.elb.amazonaws.com":         "Z32O12XQLNTSW2",
		"eu-west-2.elb.amazonaws.com":         "ZHURV8PSTC4K8",
		"eu-west-3.elb.amazonaws.com":         "Z3Q77PNBQS71R4",
		"eu-north-1.elb.amazonaws.com":        "Z23TAZ6LKFMNIO",
		"eu-south-1.elb.amazonaws.com":        "Z3ULH7SSC9OV64",
		"sa-east-1.elb.amazonaws.com":         "Z2P70J7HTTTPLU",
		"cn-north-1.elb.amazonaws.com.cn":     "Z1GDH35T77C1KE",
		"cn-northwest-1.elb.amazonaws.com.cn": "ZM7IZAIOVVDZF",
		"us-gov-west-1.elb.amazonaws.com":     "Z33AYJ8TM3BH4J",
		"us-gov-east-1.elb.amazonaws.com":     "Z166TLBEWOO7G0",
		"me-south-1.elb.amazonaws.com":        "ZS929ML54UICD",
		"af-south-1.elb.amazonaws.com":        "Z268VQBMOI5EKX",
		// Network Load Balancers
		"elb.us-east-2.amazonaws.com":         "ZLMOA37VPKANP",
		"elb.us-east-1.amazonaws.com":         "Z26RNL4JYFTOTI",
		"elb.us-west-1.amazonaws.com":         "Z24FKFUX50B4VW",
		"elb.us-west-2.amazonaws.com":         "Z18D5FSROUN65G",
		"elb.ca-central-1.amazonaws.com":      "Z2EPGBW3API2WT",
		"elb.ap-east-1.amazonaws.com":         "Z12Y7K3UBGUAD1",
		"elb.ap-south-1.amazonaws.com":        "ZVDDRBQ08TROA",
		"elb.ap-northeast-2.amazonaws.com":    "ZIBE1TIR4HY56",
		"elb.ap-southeast-1.amazonaws.com":    "ZKVM4W9LS7TM",
		"elb.ap-southeast-2.amazonaws.com":    "ZCT6FZBF4DROD",
		"elb.ap-northeast-1.amazonaws.com":    "Z31USIVHYNEOWT",
		"elb.eu-central-1.amazonaws.com":      "Z3F0SRJ5LGBH90",
		"elb.eu-west-1.amazonaws.com":         "Z2IFOLAFXWLO4F",
		"elb.eu-west-2.amazonaws.com":         "ZD4D7Y8KGAS4G",
		"elb.eu-west-3.amazonaws.com":         "Z1CMS0P5QUZ6D5",
		"elb.eu-north-1.amazonaws.com":        "Z1UDT6IFJ4EJM",
		"elb.eu-south-1.amazonaws.com":        "Z23146JA1KNAFP",
		"elb.sa-east-1.amazonaws.com":         "ZTK26PT1VY4CU",
		"elb.cn-north-1.amazonaws.com.cn":     "Z3QFB96KMJ7ED6",
		"elb.cn-northwest-1.amazonaws.com.cn": "ZQEIKTCZ8352D",
		"elb.us-gov-west-1.amazonaws.com":     "ZMG1MZ2THAWF1",
		"elb.us-gov-east-1.amazonaws.com":     "Z1ZSMQQ6Q24QQ8",
		"elb.me-south-1.amazonaws.com":        "Z3QSRYVP46NYYV",
		"elb.af-south-1.amazonaws.com":        "Z203XCE67M25HM",
		// Global Accelerator
		"awsglobalaccelerator.com": "Z2BJ6XQ5FK7U4H",
	}
)

// Route53API is the subset of the AWS Route53 API that we actually use.  Add methods as required. Signatures must match exactly.
// mostly taken from: https://github.com/kubernetes/kubernetes/blob/853167624edb6bc0cfdcdfb88e746e178f5db36c/federation/pkg/dnsprovider/providers/aws/route53/stubs/route53api.go
type Route53API interface {
	ListResourceRecordSetsPagesWithContext(ctx context.Context, input *route53.ListResourceRecordSetsInput, fn func(resp *route53.ListResourceRecordSetsOutput, lastPage bool) (shouldContinue bool), opts ...request.Option) error
	ChangeResourceRecordSetsWithContext(ctx context.Context, input *route53.ChangeResourceRecordSetsInput, opts ...request.Option) (*route53.ChangeResourceRecordSetsOutput, error)
	CreateHostedZoneWithContext(ctx context.Context, input *route53.CreateHostedZoneInput, opts ...request.Option) (*route53.CreateHostedZoneOutput, error)
	ListHostedZonesPagesWithContext(ctx context.Context, input *route53.ListHostedZonesInput, fn func(resp *route53.ListHostedZonesOutput, lastPage bool) (shouldContinue bool), opts ...request.Option) error
	ListTagsForResourceWithContext(ctx context.Context, input *route53.ListTagsForResourceInput, opts ...request.Option) (*route53.ListTagsForResourceOutput, error)
}

type zonesListCache struct {
	age      time.Time
	duration time.Duration
	zones    map[string]*route53.HostedZone
}

// AWSProvider is an implementation of Provider for AWS Route53.
type AWSProvider struct {
	provider.BaseProvider
	client               Route53API
	dryRun               bool
	batchChangeSize      int
	batchChangeInterval  time.Duration
	evaluateTargetHealth bool
	// only consider hosted zones managing domains ending in this suffix
	domainFilter endpoint.DomainFilter
	// filter hosted zones by id
	zoneIDFilter provider.ZoneIDFilter
	// filter hosted zones by type (e.g. private or public)
	zoneTypeFilter provider.ZoneTypeFilter
	// filter hosted zones by tags
	zoneTagFilter provider.ZoneTagFilter
	preferCNAME   bool
	zonesCache    *zonesListCache
}

// AWSConfig contains configuration to create a new AWS provider.
type AWSConfig struct {
	DomainFilter         endpoint.DomainFilter
	ZoneIDFilter         provider.ZoneIDFilter
	ZoneTypeFilter       provider.ZoneTypeFilter
	ZoneTagFilter        provider.ZoneTagFilter
	BatchChangeSize      int
	BatchChangeInterval  time.Duration
	EvaluateTargetHealth bool
	AssumeRole           string
	APIRetries           int
	PreferCNAME          bool
	DryRun               bool
	ZoneCacheDuration    time.Duration
}

// NewAWSProvider initializes a new AWS Route53 based Provider.
func NewAWSProvider(awsConfig AWSConfig) (*AWSProvider, error) {
	config := aws.NewConfig().WithMaxRetries(awsConfig.APIRetries)

	config.WithHTTPClient(
		instrumented_http.NewClient(config.HTTPClient, &instrumented_http.Callbacks{
			PathProcessor: func(path string) string {
				parts := strings.Split(path, "/")
				return parts[len(parts)-1]
			},
		}),
	)

	session, err := session.NewSessionWithOptions(session.Options{
		Config:            *config,
		SharedConfigState: session.SharedConfigEnable,
	})
	if err != nil {
		return nil, errors.Wrap(err, "failed to instantiate AWS session")
	}

	if awsConfig.AssumeRole != "" {
		log.Infof("Assuming role: %s", awsConfig.AssumeRole)
		session.Config.WithCredentials(stscreds.NewCredentials(session, awsConfig.AssumeRole))
	}

	provider := &AWSProvider{
		client:               route53.New(session),
		domainFilter:         awsConfig.DomainFilter,
		zoneIDFilter:         awsConfig.ZoneIDFilter,
		zoneTypeFilter:       awsConfig.ZoneTypeFilter,
		zoneTagFilter:        awsConfig.ZoneTagFilter,
		batchChangeSize:      awsConfig.BatchChangeSize,
		batchChangeInterval:  awsConfig.BatchChangeInterval,
		evaluateTargetHealth: awsConfig.EvaluateTargetHealth,
		preferCNAME:          awsConfig.PreferCNAME,
		dryRun:               awsConfig.DryRun,
		zonesCache:           &zonesListCache{duration: awsConfig.ZoneCacheDuration},
	}

	return provider, nil
}

func (p *AWSProvider) PropertyValuesEqual(name string, previous string, current string) bool {
	if name == "aws/evaluate-target-health" {
		return true
	}
	return p.BaseProvider.PropertyValuesEqual(name, previous, current)
}

// Zones returns the list of hosted zones.
func (p *AWSProvider) Zones(ctx context.Context) (map[string]*route53.HostedZone, error) {
	if p.zonesCache.zones != nil && time.Since(p.zonesCache.age) < p.zonesCache.duration {
		log.Debug("Using cached zones list")
		return p.zonesCache.zones, nil
	}
	log.Debug("Refreshing zones list cache")

	zones := make(map[string]*route53.HostedZone)

	var tagErr error
	f := func(resp *route53.ListHostedZonesOutput, lastPage bool) (shouldContinue bool) {
		for _, zone := range resp.HostedZones {
			if !p.zoneIDFilter.Match(aws.StringValue(zone.Id)) {
				continue
			}

			if !p.zoneTypeFilter.Match(zone) {
				continue
			}

			if !p.domainFilter.Match(aws.StringValue(zone.Name)) {
				continue
			}

			// Only fetch tags if a tag filter was specified
			if !p.zoneTagFilter.IsEmpty() {
				tags, err := p.tagsForZone(ctx, *zone.Id)
				if err != nil {
					tagErr = err
					return false
				}
				if !p.zoneTagFilter.Match(tags) {
					continue
				}
			}

			zones[aws.StringValue(zone.Id)] = zone
		}

		return true
	}

	err := p.client.ListHostedZonesPagesWithContext(ctx, &route53.ListHostedZonesInput{}, f)
	if err != nil {
		return nil, errors.Wrap(err, "failed to list hosted zones")
	}
	if tagErr != nil {
		return nil, errors.Wrap(tagErr, "failed to list zones tags")
	}

	for _, zone := range zones {
		log.Debugf("Considering zone: %s (domain: %s)", aws.StringValue(zone.Id), aws.StringValue(zone.Name))
	}

	if p.zonesCache.duration > time.Duration(0) {
		p.zonesCache.zones = zones
		p.zonesCache.age = time.Now()
	}

	return zones, nil
}

// wildcardUnescape converts \\052.abc back to *.abc
// Route53 stores wildcards escaped: http://docs.aws.amazon.com/Route53/latest/DeveloperGuide/DomainNameFormat.html?shortFooter=true#domain-name-format-asterisk
func wildcardUnescape(s string) string {
	if strings.Contains(s, "\\052") {
		s = strings.Replace(s, "\\052", "*", 1)
	}
	return s
}

// Records returns the list of records in a given hosted zone.
func (p *AWSProvider) Records(ctx context.Context) (endpoints []*endpoint.Endpoint, _ error) {
	zones, err := p.Zones(ctx)
	if err != nil {
		return nil, errors.Wrap(err, "records retrieval failed")
	}

	return p.records(ctx, zones)
}

func (p *AWSProvider) records(ctx context.Context, zones map[string]*route53.HostedZone) ([]*endpoint.Endpoint, error) {
	endpoints := make([]*endpoint.Endpoint, 0)
	f := func(resp *route53.ListResourceRecordSetsOutput, lastPage bool) (shouldContinue bool) {
		for _, r := range resp.ResourceRecordSets {
			newEndpoints := make([]*endpoint.Endpoint, 0)

			// TODO(linki, ownership): Remove once ownership system is in place.
			// See: https://github.com/kubernetes-sigs/external-dns/pull/122/files/74e2c3d3e237411e619aefc5aab694742001cdec#r109863370

			if !endpoint.SupportedRecordType(aws.StringValue(r.Type)) {
				continue
			}

			var ttl endpoint.TTL
			if r.TTL != nil {
				ttl = endpoint.TTL(*r.TTL)
			}

			if len(r.ResourceRecords) > 0 {
				targets := make([]string, len(r.ResourceRecords))
				for idx, rr := range r.ResourceRecords {
					targets[idx] = aws.StringValue(rr.Value)
				}

				newEndpoints = append(newEndpoints, endpoint.NewEndpointWithTTL(wildcardUnescape(aws.StringValue(r.Name)), aws.StringValue(r.Type), ttl, targets...))
			}

			if r.AliasTarget != nil {
				// Alias records don't have TTLs so provide the default to match the TXT generation
				if ttl == 0 {
					ttl = recordTTL
				}
				ep := endpoint.
					NewEndpointWithTTL(wildcardUnescape(aws.StringValue(r.Name)), endpoint.RecordTypeCNAME, ttl, aws.StringValue(r.AliasTarget.DNSName)).
					WithProviderSpecific(providerSpecificEvaluateTargetHealth, fmt.Sprintf("%t", aws.BoolValue(r.AliasTarget.EvaluateTargetHealth))).
					WithProviderSpecific(providerSpecificAlias, "true")
				newEndpoints = append(newEndpoints, ep)
			}

			for _, ep := range newEndpoints {
				if r.SetIdentifier != nil {
					ep.SetIdentifier = aws.StringValue(r.SetIdentifier)
					switch {
					case r.Weight != nil:
						ep.WithProviderSpecific(providerSpecificWeight, fmt.Sprintf("%d", aws.Int64Value(r.Weight)))
					case r.Region != nil:
						ep.WithProviderSpecific(providerSpecificRegion, aws.StringValue(r.Region))
					case r.Failover != nil:
						ep.WithProviderSpecific(providerSpecificFailover, aws.StringValue(r.Failover))
					case r.MultiValueAnswer != nil && aws.BoolValue(r.MultiValueAnswer):
						ep.WithProviderSpecific(providerSpecificMultiValueAnswer, "")
					case r.GeoLocation != nil:
						if r.GeoLocation.ContinentCode != nil {
							ep.WithProviderSpecific(providerSpecificGeolocationContinentCode, aws.StringValue(r.GeoLocation.ContinentCode))
						} else {
							if r.GeoLocation.CountryCode != nil {
								ep.WithProviderSpecific(providerSpecificGeolocationCountryCode, aws.StringValue(r.GeoLocation.CountryCode))
							}
							if r.GeoLocation.SubdivisionCode != nil {
								ep.WithProviderSpecific(providerSpecificGeolocationSubdivisionCode, aws.StringValue(r.GeoLocation.SubdivisionCode))
							}
						}
					default:
						// one of the above needs to be set, otherwise SetIdentifier doesn't make sense
					}
				}

				if r.HealthCheckId != nil {
					ep.WithProviderSpecific(providerSpecificHealthCheckID, aws.StringValue(r.HealthCheckId))
				}

				endpoints = append(endpoints, ep)
			}
		}

		return true
	}

	for _, z := range zones {
		params := &route53.ListResourceRecordSetsInput{
			HostedZoneId: z.Id,
			MaxItems:     aws.String(route53PageSize),
		}

		if err := p.client.ListResourceRecordSetsPagesWithContext(ctx, params, f); err != nil {
			return nil, errors.Wrapf(err, "failed to list resource records sets for zone %s", *z.Id)
		}
	}

	return endpoints, nil
}

// CreateRecords creates a given set of DNS records in the given hosted zone.
func (p *AWSProvider) CreateRecords(ctx context.Context, endpoints []*endpoint.Endpoint) error {
	return p.doRecords(ctx, route53.ChangeActionCreate, endpoints)
}

// DeleteRecords deletes a given set of DNS records in a given zone.
func (p *AWSProvider) DeleteRecords(ctx context.Context, endpoints []*endpoint.Endpoint) error {
	return p.doRecords(ctx, route53.ChangeActionDelete, endpoints)
}

func (p *AWSProvider) doRecords(ctx context.Context, action string, endpoints []*endpoint.Endpoint) error {
	zones, err := p.Zones(ctx)
	if err != nil {
		return errors.Wrapf(err, "failed to list zones, aborting %s doRecords action", action)
	}

	records, err := p.records(ctx, zones)
	if err != nil {
		log.Errorf("failed to list records while preparing %s doRecords action: %s", action, err)
	}

	p.AdjustEndpoints(endpoints)

	return p.submitChanges(ctx, p.newChanges(action, endpoints, records, zones), zones)
}

// UpdateRecords updates a given set of old records to a new set of records in a given hosted zone.
func (p *AWSProvider) UpdateRecords(ctx context.Context, updates, current []*endpoint.Endpoint) error {
	zones, err := p.Zones(ctx)
	if err != nil {
		return errors.Wrapf(err, "failed to list zones, aborting UpdateRecords")
	}

	records, err := p.records(ctx, zones)
	if err != nil {
		log.Errorf("failed to list records while preparing UpdateRecords: %s", err)
	}

	return p.submitChanges(ctx, p.createUpdateChanges(updates, current, records, zones), zones)
}

func (p *AWSProvider) createUpdateChanges(newEndpoints, oldEndpoints []*endpoint.Endpoint, recordsCache []*endpoint.Endpoint, zones map[string]*route53.HostedZone) []*route53.Change {
	var deletes []*endpoint.Endpoint
	var creates []*endpoint.Endpoint
	var updates []*endpoint.Endpoint

	for i, new := range newEndpoints {
		old := oldEndpoints[i]
		if new.RecordType != old.RecordType ||
			// Handle the case where an AWS ALIAS record is changing to/from a CNAME.
			(old.RecordType == endpoint.RecordTypeCNAME && useAlias(old, p.preferCNAME) != useAlias(new, p.preferCNAME)) {
			// The record type changed, so UPSERT will fail. Instead perform a DELETE followed by a CREATE.
			deletes = append(deletes, old)
			creates = append(creates, new)
		} else {
			// Safe to perform an UPSERT.
			updates = append(updates, new)
		}
	}

	combined := make([]*route53.Change, 0, len(deletes)+len(creates)+len(updates))
	combined = append(combined, p.newChanges(route53.ChangeActionCreate, creates, recordsCache, zones)...)
	combined = append(combined, p.newChanges(route53.ChangeActionUpsert, updates, recordsCache, zones)...)
	combined = append(combined, p.newChanges(route53.ChangeActionDelete, deletes, recordsCache, zones)...)
	return combined
}

// ApplyChanges applies a given set of changes in a given zone.
func (p *AWSProvider) ApplyChanges(ctx context.Context, changes *plan.Changes) error {
	zones, err := p.Zones(ctx)
	if err != nil {
		return errors.Wrap(err, "failed to list zones, not applying changes")
	}

	records, ok := ctx.Value(provider.RecordsContextKey).([]*endpoint.Endpoint)
	if !ok {
		var err error
		records, err = p.records(ctx, zones)
		if err != nil {
			log.Errorf("failed to get records while preparing to applying changes: %s", err)
		}
	}

	updateChanges := p.createUpdateChanges(changes.UpdateNew, changes.UpdateOld, records, zones)

	combinedChanges := make([]*route53.Change, 0, len(changes.Delete)+len(changes.Create)+len(updateChanges))
	combinedChanges = append(combinedChanges, p.newChanges(route53.ChangeActionCreate, changes.Create, records, zones)...)
	combinedChanges = append(combinedChanges, p.newChanges(route53.ChangeActionDelete, changes.Delete, records, zones)...)
	combinedChanges = append(combinedChanges, updateChanges...)

	return p.submitChanges(ctx, combinedChanges, zones)
}

// submitChanges takes a zone and a collection of Changes and sends them as a single transaction.
func (p *AWSProvider) submitChanges(ctx context.Context, changes []*route53.Change, zones map[string]*route53.HostedZone) error {
	// return early if there is nothing to change
	if len(changes) == 0 {
		log.Info("All records are already up to date")
		return nil
	}

	// separate into per-zone change sets to be passed to the API.
	changesByZone := changesByZone(zones, changes)
	if len(changesByZone) == 0 {
		log.Info("All records are already up to date, there are no changes for the matching hosted zones")
	}

	var failedZones []string
	for z, cs := range changesByZone {
		var failedUpdate bool

		batchCs := batchChangeSet(cs, p.batchChangeSize)

		for i, b := range batchCs {
			for _, c := range b {
				log.Infof("Desired change: %s %s %s [Id: %s]", *c.Action, *c.ResourceRecordSet.Name, *c.ResourceRecordSet.Type, z)
			}

			if !p.dryRun {
				params := &route53.ChangeResourceRecordSetsInput{
					HostedZoneId: aws.String(z),
					ChangeBatch: &route53.ChangeBatch{
						Changes: b,
					},
				}

				if _, err := p.client.ChangeResourceRecordSetsWithContext(ctx, params); err != nil {
					log.Errorf("Failure in zone %s [Id: %s]", aws.StringValue(zones[z].Name), z)
					log.Error(err) //TODO(ideahitme): consider changing the interface in cases when this error might be a concern for other components
					failedUpdate = true
				} else {
					// z is the R53 Hosted Zone ID already as aws.StringValue
					log.Infof("%d record(s) in zone %s [Id: %s] were successfully updated", len(b), aws.StringValue(zones[z].Name), z)
				}

				if i != len(batchCs)-1 {
					time.Sleep(p.batchChangeInterval)
				}
			}
		}

		if failedUpdate {
			failedZones = append(failedZones, z)
		}
	}

	if len(failedZones) > 0 {
		return errors.Errorf("failed to submit all changes for the following zones: %v", failedZones)
	}

	return nil
}

// newChanges returns a collection of Changes based on the given records and action.
func (p *AWSProvider) newChanges(action string, endpoints []*endpoint.Endpoint, recordsCache []*endpoint.Endpoint, zones map[string]*route53.HostedZone) []*route53.Change {
	changes := make([]*route53.Change, 0, len(endpoints))

	for _, endpoint := range endpoints {
		change, dualstack := p.newChange(action, endpoint, recordsCache, zones)
		changes = append(changes, change)
		if dualstack {
			// make a copy of change, modify RRS type to AAAA, then add new change
			rrs := *change.ResourceRecordSet
			change2 := &route53.Change{Action: change.Action, ResourceRecordSet: &rrs}
			change2.ResourceRecordSet.Type = aws.String(route53.RRTypeAaaa)
			changes = append(changes, change2)
		}
	}

	return changes
}

// AdjustEndpoints modifies the provided endpoints (coming from various sources) to match
// the endpoints that the provider returns in `Records` so that the change plan will not have
// unneeded (potentially failing) changes.
// Example: CNAME endpoints pointing to ELBs will have a `alias` provider-specific property
// added to match the endpoints generated from existing alias records in Route53.
func (p *AWSProvider) AdjustEndpoints(endpoints []*endpoint.Endpoint) []*endpoint.Endpoint {
	for _, ep := range endpoints {
		alias := false
		if aliasString, ok := ep.GetProviderSpecificProperty(providerSpecificAlias); ok {
			alias = aliasString.Value == "true"
		} else if useAlias(ep, p.preferCNAME) {
			alias = true
			log.Debugf("Modifying endpoint: %v, setting %s=true", ep, providerSpecificAlias)
			ep.ProviderSpecific = append(ep.ProviderSpecific, endpoint.ProviderSpecificProperty{
				Name:  providerSpecificAlias,
				Value: "true",
			})
		}

		if _, ok := ep.GetProviderSpecificProperty(providerSpecificEvaluateTargetHealth); alias && !ok {
			log.Debugf("Modifying endpoint: %v, setting %s=%t", ep, providerSpecificEvaluateTargetHealth, p.evaluateTargetHealth)
			ep.ProviderSpecific = append(ep.ProviderSpecific, endpoint.ProviderSpecificProperty{
				Name:  providerSpecificEvaluateTargetHealth,
				Value: fmt.Sprintf("%t", p.evaluateTargetHealth),
			})
		}
	}
	return endpoints
}

// newChange returns a route53 Change and a boolean indicating if there should also be a change to a AAAA record
// returned Change is based on the given record by the given action, e.g.
// action=ChangeActionCreate returns a change for creation of the record and
// action=ChangeActionDelete returns a change for deletion of the record.
func (p *AWSProvider) newChange(action string, ep *endpoint.Endpoint, recordsCache []*endpoint.Endpoint, zones map[string]*route53.HostedZone) (*route53.Change, bool) {
	change := &route53.Change{
		Action: aws.String(action),
		ResourceRecordSet: &route53.ResourceRecordSet{
			Name: aws.String(ep.DNSName),
		},
	}
	dualstack := false
	if targetHostedZone := isAWSAlias(ep); targetHostedZone != "" {
		evalTargetHealth := p.evaluateTargetHealth
		if prop, ok := ep.GetProviderSpecificProperty(providerSpecificEvaluateTargetHealth); ok {
			evalTargetHealth = prop.Value == "true"
		}
		// If the endpoint has a Dualstack label, append a change for AAAA record as well.
		if val, ok := ep.Labels[endpoint.DualstackLabelKey]; ok {
			dualstack = val == "true"
		}
		change.ResourceRecordSet.Type = aws.String(route53.RRTypeA)
		change.ResourceRecordSet.AliasTarget = &route53.AliasTarget{
			DNSName:              aws.String(ep.Targets[0]),
			HostedZoneId:         aws.String(cleanZoneID(targetHostedZone)),
			EvaluateTargetHealth: aws.Bool(evalTargetHealth),
		}
	} else {
		change.ResourceRecordSet.Type = aws.String(ep.RecordType)
		if !ep.RecordTTL.IsConfigured() {
			change.ResourceRecordSet.TTL = aws.Int64(recordTTL)
		} else {
			change.ResourceRecordSet.TTL = aws.Int64(int64(ep.RecordTTL))
		}
		change.ResourceRecordSet.ResourceRecords = make([]*route53.ResourceRecord, len(ep.Targets))
		for idx, val := range ep.Targets {
			change.ResourceRecordSet.ResourceRecords[idx] = &route53.ResourceRecord{
				Value: aws.String(val),
			}
		}
	}

	setIdentifier := ep.SetIdentifier
	if setIdentifier != "" {
		change.ResourceRecordSet.SetIdentifier = aws.String(setIdentifier)
		if prop, ok := ep.GetProviderSpecificProperty(providerSpecificWeight); ok {
			weight, err := strconv.ParseInt(prop.Value, 10, 64)
			if err != nil {
				log.Errorf("Failed parsing value of %s: %s: %v; using weight of 0", providerSpecificWeight, prop.Value, err)
				weight = 0
			}
			change.ResourceRecordSet.Weight = aws.Int64(weight)
		}
		if prop, ok := ep.GetProviderSpecificProperty(providerSpecificRegion); ok {
			change.ResourceRecordSet.Region = aws.String(prop.Value)
		}
		if prop, ok := ep.GetProviderSpecificProperty(providerSpecificFailover); ok {
			change.ResourceRecordSet.Failover = aws.String(prop.Value)
		}
		if _, ok := ep.GetProviderSpecificProperty(providerSpecificMultiValueAnswer); ok {
			change.ResourceRecordSet.MultiValueAnswer = aws.Bool(true)
		}

		var geolocation = &route53.GeoLocation{}
		useGeolocation := false
		if prop, ok := ep.GetProviderSpecificProperty(providerSpecificGeolocationContinentCode); ok {
			geolocation.ContinentCode = aws.String(prop.Value)
			useGeolocation = true
		} else {
			if prop, ok := ep.GetProviderSpecificProperty(providerSpecificGeolocationCountryCode); ok {
				geolocation.CountryCode = aws.String(prop.Value)
				useGeolocation = true
			}
			if prop, ok := ep.GetProviderSpecificProperty(providerSpecificGeolocationSubdivisionCode); ok {
				geolocation.SubdivisionCode = aws.String(prop.Value)
				useGeolocation = true
			}
		}
		if useGeolocation {
			change.ResourceRecordSet.GeoLocation = geolocation
		}
	}

	if prop, ok := ep.GetProviderSpecificProperty(providerSpecificHealthCheckID); ok {
		change.ResourceRecordSet.HealthCheckId = aws.String(prop.Value)
	}

	return change, dualstack
}

func (p *AWSProvider) tagsForZone(ctx context.Context, zoneID string) (map[string]string, error) {
	response, err := p.client.ListTagsForResourceWithContext(ctx, &route53.ListTagsForResourceInput{
		ResourceType: aws.String("hostedzone"),
		ResourceId:   aws.String(zoneID),
	})
	if err != nil {
		return nil, errors.Wrapf(err, "failed to list tags for zone %s", zoneID)
	}
	tagMap := map[string]string{}
	for _, tag := range response.ResourceTagSet.Tags {
		tagMap[*tag.Key] = *tag.Value
	}
	return tagMap, nil
}

func batchChangeSet(cs []*route53.Change, batchSize int) [][]*route53.Change {
	if len(cs) <= batchSize {
		res := sortChangesByActionNameType(cs)
		return [][]*route53.Change{res}
	}

	batchChanges := make([][]*route53.Change, 0)

	changesByName := make(map[string][]*route53.Change)
	for _, v := range cs {
		changesByName[*v.ResourceRecordSet.Name] = append(changesByName[*v.ResourceRecordSet.Name], v)
	}

	names := make([]string, 0)
	for v := range changesByName {
		names = append(names, v)
	}
	sort.Strings(names)

	for _, name := range names {
		totalChangesByName := len(changesByName[name])

		if totalChangesByName > batchSize {
			log.Warnf("Total changes for %s exceeds max batch size of %d, total changes: %d", name,
				batchSize, totalChangesByName)
			continue
		}

		var existingBatch bool
		for i, b := range batchChanges {
			if len(b)+totalChangesByName <= batchSize {
				batchChanges[i] = append(batchChanges[i], changesByName[name]...)
				existingBatch = true
				break
			}
		}
		if !existingBatch {
			batchChanges = append(batchChanges, changesByName[name])
		}
	}

	for i, batch := range batchChanges {
		batchChanges[i] = sortChangesByActionNameType(batch)
	}

	return batchChanges
}

func sortChangesByActionNameType(cs []*route53.Change) []*route53.Change {
	sort.SliceStable(cs, func(i, j int) bool {
		if *cs[i].Action > *cs[j].Action {
			return true
		}
		if *cs[i].Action < *cs[j].Action {
			return false
		}
		if *cs[i].ResourceRecordSet.Name < *cs[j].ResourceRecordSet.Name {
			return true
		}
		if *cs[i].ResourceRecordSet.Name > *cs[j].ResourceRecordSet.Name {
			return false
		}
		return *cs[i].ResourceRecordSet.Type < *cs[j].ResourceRecordSet.Type
	})

	return cs
}

// changesByZone separates a multi-zone change into a single change per zone.
func changesByZone(zones map[string]*route53.HostedZone, changeSet []*route53.Change) map[string][]*route53.Change {
	changes := make(map[string][]*route53.Change)

	for _, z := range zones {
		changes[aws.StringValue(z.Id)] = []*route53.Change{}
	}

	for _, c := range changeSet {
		hostname := provider.EnsureTrailingDot(aws.StringValue(c.ResourceRecordSet.Name))

		zones := suitableZones(hostname, zones)
		if len(zones) == 0 {
			log.Debugf("Skipping record %s because no hosted zone matching record DNS Name was detected", c.String())
			continue
		}
		for _, z := range zones {
			if c.ResourceRecordSet.AliasTarget != nil && aws.StringValue(c.ResourceRecordSet.AliasTarget.HostedZoneId) == sameZoneAlias {
				// alias record is to be created; target needs to be in the same zone as endpoint
				// if it's not, this will fail
				rrset := *c.ResourceRecordSet
				aliasTarget := *rrset.AliasTarget
				aliasTarget.HostedZoneId = aws.String(cleanZoneID(aws.StringValue(z.Id)))
				rrset.AliasTarget = &aliasTarget
				c = &route53.Change{
					Action:            c.Action,
					ResourceRecordSet: &rrset,
				}
			}
			changes[aws.StringValue(z.Id)] = append(changes[aws.StringValue(z.Id)], c)
			log.Debugf("Adding %s to zone %s [Id: %s]", hostname, aws.StringValue(z.Name), aws.StringValue(z.Id))
		}
	}

	// separating a change could lead to empty sub changes, remove them here.
	for zone, change := range changes {
		if len(change) == 0 {
			delete(changes, zone)
		}
	}

	return changes
}

// suitableZones returns all suitable private zones and the most suitable public zone
//   for a given hostname and a set of zones.
func suitableZones(hostname string, zones map[string]*route53.HostedZone) []*route53.HostedZone {
	var matchingZones []*route53.HostedZone
	var publicZone *route53.HostedZone

	for _, z := range zones {
		if aws.StringValue(z.Name) == hostname || strings.HasSuffix(hostname, "."+aws.StringValue(z.Name)) {
			if z.Config == nil || !aws.BoolValue(z.Config.PrivateZone) {
				// Only select the best matching public zone
				if publicZone == nil || len(aws.StringValue(z.Name)) > len(aws.StringValue(publicZone.Name)) {
					publicZone = z
				}
			} else {
				// Include all private zones
				matchingZones = append(matchingZones, z)
			}
		}
	}

	if publicZone != nil {
		matchingZones = append(matchingZones, publicZone)
	}

	return matchingZones
}

// useAlias determines if AWS ALIAS should be used.
func useAlias(ep *endpoint.Endpoint, preferCNAME bool) bool {
	if preferCNAME {
		return false
	}

	if ep.RecordType == endpoint.RecordTypeCNAME && len(ep.Targets) > 0 {
		return canonicalHostedZone(ep.Targets[0]) != ""
	}

	return false
}

// isAWSAlias determines if a given endpoint is supposed to create an AWS Alias record
// and (if so) returns the target hosted zone ID
func isAWSAlias(ep *endpoint.Endpoint) string {
	prop, exists := ep.GetProviderSpecificProperty(providerSpecificAlias)
	if exists && prop.Value == "true" && ep.RecordType == endpoint.RecordTypeCNAME && len(ep.Targets) > 0 {
		// alias records can only point to canonical hosted zones (e.g. to ELBs) or other records in the same zone

		if hostedZoneID, ok := ep.GetProviderSpecificProperty(providerSpecificTargetHostedZone); ok {
			// existing Endpoint where we got the target hosted zone from the Route53 data
			return hostedZoneID.Value
		}

		// check if the target is in a canonical hosted zone
		if canonicalHostedZone := canonicalHostedZone(ep.Targets[0]); canonicalHostedZone != "" {
			return canonicalHostedZone
		}

		// if not, target needs to be in the same zone
		return sameZoneAlias
	}
	return ""
}

// canonicalHostedZone returns the matching canonical zone for a given hostname.
func canonicalHostedZone(hostname string) string {
	for suffix, zone := range canonicalHostedZones {
		if strings.HasSuffix(hostname, suffix) {
			return zone
		}
	}

	return ""
}

// cleanZoneID removes the "/hostedzone/" prefix
func cleanZoneID(id string) string {
	if strings.HasPrefix(id, "/hostedzone/") {
		id = strings.TrimPrefix(id, "/hostedzone/")
	}
	return id
}
