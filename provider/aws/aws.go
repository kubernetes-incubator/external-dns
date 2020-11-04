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
	// provider specific key that designates whether an AWS ALIAS record has the EvaluateTargetHealth
	// field set to true.
	providerSpecificEvaluateTargetHealth       = "aws/evaluate-target-health"
	providerSpecificWeight                     = "aws/weight"
	providerSpecificRegion                     = "aws/region"
	providerSpecificFailover                   = "aws/failover"
	providerSpecificGeolocationContinentCode   = "aws/geolocation-continent-code"
	providerSpecificGeolocationCountryCode     = "aws/geolocation-country-code"
	providerSpecificGeolocationSubdivisionCode = "aws/geolocation-subdivision-code"
	providerSpecificMultiValueAnswer           = "aws/multi-value-answer"
	providerSpecificHealthCheckID              = "aws/health-check-id"
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
	// queue for collecting changes to submit them in the next iteration, but after all other changes
	failedChangesQueue map[string][]*route53.Change
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
		failedChangesQueue:   make(map[string][]*route53.Change),
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

			if !provider.SupportedRecordType(aws.StringValue(r.Type)) {
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

			if r.AliasTarget != nil && aws.StringValue(r.AliasTarget.HostedZoneId) != endpoint.OwnedRecordLabelKey { // see HACK in `newChanges`
				// Alias records don't have TTLs so provide the default to match the TXT generation
				if ttl == 0 {
					ttl = recordTTL
				}
				ep := endpoint.
					NewEndpointWithTTL(wildcardUnescape(aws.StringValue(r.Name)), endpoint.RecordTypeCNAME, ttl, aws.StringValue(r.AliasTarget.DNSName)).
					WithProviderSpecific(providerSpecificEvaluateTargetHealth, fmt.Sprintf("%t", aws.BoolValue(r.AliasTarget.EvaluateTargetHealth)))
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

		// group changes into new changes and into changes that failed in a previous iteration and are retried
		var newChanges, retriedChanges []*route53.Change
		for _, c := range cs {
			found := false
			if p.failedChangesQueue[z] != nil {
				for _, failedChange := range p.failedChangesQueue[z] {
					if c == failedChange {
						retriedChanges = append(retriedChanges, c)
						found = true
					}
				}
				p.failedChangesQueue[z] = nil // clear the queue
			}
			if !found {
				newChanges = append(newChanges, c)
			}
		}
		batchCs := append(batchChangeSet(newChanges, p.batchChangeSize), batchChangeSet(retriedChanges, p.batchChangeSize)...)

		for i, b := range batchCs {
			if len(b) == 0 {
				continue
			}

			for _, c := range b {
				log.Infof("Desired change: %s %s %s [Id: %s]", *c.Action, *c.ResourceRecordSet.Name, *c.ResourceRecordSet.Type, z)
			}

			if !p.dryRun {
				params := &route53.ChangeResourceRecordSetsInput{
					HostedZoneId: aws.String(z),
					ChangeBatch: &route53.ChangeBatch{
						Changes: ownedRecordRemoved(b),
					},
				}

				successfulChanges := 0

				if _, err := p.client.ChangeResourceRecordSetsWithContext(ctx, params); err != nil {
					log.Errorf("Failure in zone %s [Id: %s] when submitting change batch", aws.StringValue(zones[z].Name), z)
					log.Error(err)

					changesByOwnership := groupChangesByNameAndOwnership(b)

					if len(changesByOwnership) > 1 {
						log.Error("Trying to submit change sets one-by-one instead")

						for _, changes := range changesByOwnership {
							for _, c := range changes {
								log.Infof("Desired change: %s %s %s [Id: %s]", *c.Action, *c.ResourceRecordSet.Name, *c.ResourceRecordSet.Type, z)
							}
							params.ChangeBatch = &route53.ChangeBatch{
								Changes: ownedRecordRemoved(changes),
							}
							if _, err := p.client.ChangeResourceRecordSetsWithContext(ctx, params); err != nil {
								failedUpdate = true
								log.Error("Failed submitting change, it will be retried in a separate change batch in the next iteration")
								if _, ok := p.failedChangesQueue[z]; !ok {
									p.failedChangesQueue[z] = changes
								} else {
									p.failedChangesQueue[z] = append(p.failedChangesQueue[z], changes...)
								}
							} else {
								log.Info("Change(s) successful")
								successfulChanges = successfulChanges + len(changes)
							}
						}
					} else {
						failedUpdate = true
					}
				} else {
					successfulChanges = len(b)
				}

				if successfulChanges > 0 {
					// z is the R53 Hosted Zone ID already as aws.StringValue
					log.Infof("%d record(s) in zone %s [Id: %s] were successfully updated", successfulChanges, aws.StringValue(zones[z].Name), z)
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

	if useAlias(ep, p.preferCNAME) {
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
			HostedZoneId:         aws.String(canonicalHostedZone(ep.Targets[0])),
			EvaluateTargetHealth: aws.Bool(evalTargetHealth),
		}
	} else if hostedZone := isAWSAlias(ep, recordsCache); hostedZone != "" {
		for _, zone := range zones {
			change.ResourceRecordSet.Type = aws.String(route53.RRTypeA)
			change.ResourceRecordSet.AliasTarget = &route53.AliasTarget{
				DNSName:              aws.String(ep.Targets[0]),
				HostedZoneId:         aws.String(cleanZoneID(*zone.Id)),
				EvaluateTargetHealth: aws.Bool(p.evaluateTargetHealth),
			}
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

	setOwnedRecord(change, ep)

	return change, dualstack
}

// HACK: we have no easy way of passing the "ownedRecord" label of endpoints to `submitChanges` for grouping
// transactions by owner relationship; we cheat here by passing the label as the `AliasTarget` of the record;
// the `AliasTarget` setting is recognized and removed in `submitChanges` before submission of the change to Route53;
// the "ownedRecord" label should only ever appear on TXT endpoints created by the TXT registry, so it should not
// interfere with any other record that could possibly have an actual `AliasTarget` setting
func setOwnedRecord(change *route53.Change, ep *endpoint.Endpoint) {
	if ownedRecord, ok := ep.Labels[endpoint.OwnedRecordLabelKey]; ok && change.ResourceRecordSet.AliasTarget == nil {
		change.ResourceRecordSet.AliasTarget = &route53.AliasTarget{
			HostedZoneId: aws.String(endpoint.OwnedRecordLabelKey),
			DNSName:      aws.String(ownedRecord),
		}
	}
}

// returns the value of the ownedRecord label of the original endpoint (encoded in the AliasTarget of the change)
func getOwnedRecord(change *route53.Change) string {
	if aws.StringValue(change.ResourceRecordSet.Type) == "TXT" && change.ResourceRecordSet.AliasTarget != nil && aws.StringValue(change.ResourceRecordSet.AliasTarget.HostedZoneId) == endpoint.OwnedRecordLabelKey {
		return aws.StringValue(change.ResourceRecordSet.AliasTarget.DNSName)
	}
	return ""
}

// returns a copy of the given changes with the ownedRecord encoding via AliasTarget removed;
// this should be used directly before passing the change set to Route53
func ownedRecordRemoved(changes []*route53.Change) []*route53.Change {
	ret := []*route53.Change{}
	for _, change := range changes {
		recordSetCopy := *change.ResourceRecordSet
		if getOwnedRecord(change) != "" {
			recordSetCopy.AliasTarget = nil
		}
		ret = append(ret, &route53.Change{
			Action:            change.Action,
			ResourceRecordSet: &recordSetCopy,
		})
	}
	return ret
}

// group the given changes by name and ownership relation to ensure these are always submitted in the same transaction to Route53;
// grouping by name is done to always submit changes with the same name but different set identifier together,
// grouping by ownership relation is done to always submit changes of records and e.g. their corresponding TXT registry records in together
func groupChangesByNameAndOwnership(cs []*route53.Change) map[string][]*route53.Change {
	changesByOwnership := make(map[string][]*route53.Change)
	for _, v := range cs {
		key := getOwnedRecord(v)
		if key == "" {
			key = aws.StringValue(v.ResourceRecordSet.Name)
		}
		changesByOwnership[key] = append(changesByOwnership[key], v)
	}
	return changesByOwnership
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

	changesByOwnership := groupChangesByNameAndOwnership(cs)

	names := make([]string, 0)
	for v := range changesByOwnership {
		names = append(names, v)
	}
	sort.Strings(names)

	currentBatch := []*route53.Change{}
	for k, name := range names {
		v := changesByOwnership[name]
		if len(v) > batchSize {
			log.Warnf("Total changes for %v exceeds max batch size of %d, total changes: %d; changes will not be performed", k, batchSize, len(v))
			continue
		}

		if len(currentBatch)+len(v) > batchSize {
			// currentBatch would be too large if we add this changeset;
			// add currentBatch to batchChanges and start a new currentBatch
			batchChanges = append(batchChanges, sortChangesByActionNameType(currentBatch))
			currentBatch = append([]*route53.Change{}, v...)
		} else {
			currentBatch = append(currentBatch, v...)
		}
	}
	if len(currentBatch) > 0 {
		// add final currentBatch
		batchChanges = append(batchChanges, sortChangesByActionNameType(currentBatch))
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

// isAWSAlias determines if a given hostname belongs to an AWS Alias record by doing an reverse lookup.
func isAWSAlias(ep *endpoint.Endpoint, addrs []*endpoint.Endpoint) string {
	if prop, exists := ep.GetProviderSpecificProperty("alias"); ep.RecordType == endpoint.RecordTypeCNAME && exists && prop.Value == "true" {
		for _, addr := range addrs {
			if len(ep.Targets) > 0 && addr.DNSName == ep.Targets[0] {
				if hostedZone := canonicalHostedZone(addr.Targets[0]); hostedZone != "" {
					return hostedZone
				}
			}
		}
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
