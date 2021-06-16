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

package linode

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"

	"github.com/linode/linodego"
	log "github.com/sirupsen/logrus"
	"golang.org/x/oauth2"

	"sigs.k8s.io/external-dns/endpoint"
	"sigs.k8s.io/external-dns/plan"
	"sigs.k8s.io/external-dns/provider"
)

// LinodeDomainClient interface to ease testing
type LinodeDomainClient interface {
	ListDomainRecords(ctx context.Context, domainID int, opts *linodego.ListOptions) ([]linodego.DomainRecord, error)
	ListDomains(ctx context.Context, opts *linodego.ListOptions) ([]linodego.Domain, error)
	CreateDomainRecord(ctx context.Context, domainID int, domainrecord linodego.DomainRecordCreateOptions) (*linodego.DomainRecord, error)
	DeleteDomainRecord(ctx context.Context, domainID int, id int) error
	UpdateDomainRecord(ctx context.Context, domainID int, id int, domainrecord linodego.DomainRecordUpdateOptions) (*linodego.DomainRecord, error)
}

// LinodeProvider is an implementation of Provider for Digital Ocean's DNS.
type LinodeProvider struct {
	provider.BaseProvider
	Client       LinodeDomainClient
	domainFilter endpoint.DomainFilter
	DryRun       bool
}

// LinodeChanges All API calls calculated from the plan
type LinodeChanges struct {
	Creates []LinodeChangeCreate
	Deletes []LinodeChangeDelete
	Updates []LinodeChangeUpdate
}

// LinodeChangeCreate Linode Domain Record Creates
type LinodeChangeCreate struct {
	Domain  linodego.Domain
	Options linodego.DomainRecordCreateOptions
}

// LinodeChangeUpdate Linode Domain Record Updates
type LinodeChangeUpdate struct {
	Domain       linodego.Domain
	DomainRecord linodego.DomainRecord
	Options      linodego.DomainRecordUpdateOptions
}

// LinodeChangeDelete Linode Domain Record Deletes
type LinodeChangeDelete struct {
	Domain       linodego.Domain
	DomainRecord linodego.DomainRecord
}

// NewLinodeProvider initializes a new Linode DNS based Provider.
func NewLinodeProvider(domainFilter endpoint.DomainFilter, dryRun bool, appVersion string) (*LinodeProvider, error) {
	token, ok := os.LookupEnv("LINODE_TOKEN")
	if !ok {
		return nil, fmt.Errorf("no token found")
	}

	tokenSource := oauth2.StaticTokenSource(&oauth2.Token{AccessToken: token})

	oauth2Client := &http.Client{
		Transport: &oauth2.Transport{
			Source: tokenSource,
		},
	}

	linodeClient := linodego.NewClient(oauth2Client)
	linodeClient.SetUserAgent(fmt.Sprintf("ExternalDNS/%s linodego/%s", appVersion, linodego.Version))

	provider := &LinodeProvider{
		Client:       &linodeClient,
		domainFilter: domainFilter,
		DryRun:       dryRun,
	}
	return provider, nil
}

// Zones returns the list of hosted zones.
func (p *LinodeProvider) Zones(ctx context.Context) ([]linodego.Domain, error) {
	zones, err := p.fetchZones(ctx)
	if err != nil {
		return nil, err
	}

	return zones, nil
}

// Records returns the list of records in a given zone.
func (p *LinodeProvider) Records(ctx context.Context) ([]*endpoint.Endpoint, error) {
	zones, err := p.Zones(ctx)
	if err != nil {
		return nil, err
	}

	var endpoints []*endpoint.Endpoint

	for _, zone := range zones {
		records, err := p.fetchRecords(ctx, zone.ID)
		if err != nil {
			return nil, err
		}

		for _, r := range records {
			if endpoint.SupportedRecordType(string(r.Type)) {
				name := fmt.Sprintf("%s.%s", r.Name, zone.Domain)

				// root name is identified by the empty string and should be
				// translated to zone name for the endpoint entry.
				if r.Name == "" {
					name = zone.Domain
				}

				endpoints = append(endpoints, endpoint.NewEndpointWithTTL(name, string(r.Type), endpoint.TTL(r.TTLSec), r.Target))
			}
		}
	}

	return endpoints, nil
}

func (p *LinodeProvider) fetchRecords(ctx context.Context, domainID int) ([]linodego.DomainRecord, error) {
	records, err := p.Client.ListDomainRecords(ctx, domainID, nil)
	if err != nil {
		return nil, err
	}

	return records, nil
}

func (p *LinodeProvider) fetchZones(ctx context.Context) ([]linodego.Domain, error) {
	var zones []linodego.Domain

	allZones, err := p.Client.ListDomains(ctx, linodego.NewListOptions(0, ""))

	if err != nil {
		return nil, err
	}

	for _, zone := range allZones {
		if !p.domainFilter.Match(zone.Domain) {
			continue
		}

		zones = append(zones, zone)
	}

	return zones, nil
}

// submitChanges takes a zone and a collection of Changes and sends them as a single transaction.
func (p *LinodeProvider) submitChanges(ctx context.Context, changes LinodeChanges) error {
	for _, change := range changes.Creates {
		logFields := log.Fields{
			"record":   change.Options.Name,
			"type":     change.Options.Type,
			"action":   "Create",
			"zoneName": change.Domain.Domain,
			"zoneID":   change.Domain.ID,
		}

		log.WithFields(logFields).Info("Creating record.")

		if p.DryRun {
			log.WithFields(logFields).Info("Would create record.")
		} else if _, err := p.Client.CreateDomainRecord(ctx, change.Domain.ID, change.Options); err != nil {
			log.WithFields(logFields).Errorf(
				"Failed to Create record: %v",
				err,
			)
		}
	}

	for _, change := range changes.Deletes {
		logFields := log.Fields{
			"record":   change.DomainRecord.Name,
			"type":     change.DomainRecord.Type,
			"action":   "Delete",
			"zoneName": change.Domain.Domain,
			"zoneID":   change.Domain.ID,
		}

		log.WithFields(logFields).Info("Deleting record.")

		if p.DryRun {
			log.WithFields(logFields).Info("Would delete record.")
		} else if err := p.Client.DeleteDomainRecord(ctx, change.Domain.ID, change.DomainRecord.ID); err != nil {
			log.WithFields(logFields).Errorf(
				"Failed to Delete record: %v",
				err,
			)
		}
	}

	for _, change := range changes.Updates {
		logFields := log.Fields{
			"record":   change.Options.Name,
			"type":     change.Options.Type,
			"action":   "Update",
			"zoneName": change.Domain.Domain,
			"zoneID":   change.Domain.ID,
		}

		log.WithFields(logFields).Info("Updating record.")

		if p.DryRun {
			log.WithFields(logFields).Info("Would update record.")
		} else if _, err := p.Client.UpdateDomainRecord(ctx, change.Domain.ID, change.DomainRecord.ID, change.Options); err != nil {
			log.WithFields(logFields).Errorf(
				"Failed to Update record: %v",
				err,
			)
		}
	}

	return nil
}

func getWeight() *int {
	weight := 1
	return &weight
}

func getPort() *int {
	port := 0
	return &port
}

func getPriority() *int {
	priority := 0
	return &priority
}

// ApplyChanges applies a given set of changes in a given zone.
func (p *LinodeProvider) ApplyChanges(ctx context.Context, changes *plan.Changes) error {
	recordsByZoneID := make(map[string][]linodego.DomainRecord)

	zones, err := p.fetchZones(ctx)

	if err != nil {
		return err
	}

	zonesByID := make(map[string]linodego.Domain)

	zoneNameIDMapper := provider.ZoneIDName{}

	for _, z := range zones {
		zoneNameIDMapper.Add(strconv.Itoa(z.ID), z.Domain)
		zonesByID[strconv.Itoa(z.ID)] = z
	}

	// Fetch records for each zone
	for _, zone := range zones {
		records, err := p.fetchRecords(ctx, zone.ID)

		if err != nil {
			return err
		}

		recordsByZoneID[strconv.Itoa(zone.ID)] = append(recordsByZoneID[strconv.Itoa(zone.ID)], records...)
	}

	createsByZone := endpointsByZone(zoneNameIDMapper, changes.Create)
	updatesByZone := endpointsByZone(zoneNameIDMapper, changes.UpdateNew)
	deletesByZone := endpointsByZone(zoneNameIDMapper, changes.Delete)

	var linodeCreates []LinodeChangeCreate
	var linodeUpdates []LinodeChangeUpdate
	var linodeDeletes []LinodeChangeDelete

	// Generate Creates
	for zoneID, creates := range createsByZone {
		zone := zonesByID[zoneID]

		if len(creates) == 0 {
			log.WithFields(log.Fields{
				"zoneID":   zoneID,
				"zoneName": zone.Domain,
			}).Debug("Skipping Zone, no creates found.")
			continue
		}

		records := recordsByZoneID[zoneID]

		for _, ep := range creates {
			matchedRecords := getRecordID(records, zone, ep)

			if len(matchedRecords) != 0 {
				log.WithFields(log.Fields{
					"zoneID":     zoneID,
					"zoneName":   zone.Domain,
					"dnsName":    ep.DNSName,
					"recordType": ep.RecordType,
				}).Warn("Records found which should not exist")
			}

			recordType, err := convertRecordType(ep.RecordType)

			if err != nil {
				return err
			}

			for _, target := range ep.Targets {
				linodeCreates = append(linodeCreates, LinodeChangeCreate{
					Domain: zone,
					Options: linodego.DomainRecordCreateOptions{
						Target:   target,
						Name:     getStrippedRecordName(zone, ep),
						Type:     recordType,
						Weight:   getWeight(),
						Port:     getPort(),
						Priority: getPriority(),
						TTLSec:   int(ep.RecordTTL),
					},
				})
			}
		}
	}

	// Generate Updates
	for zoneID, updates := range updatesByZone {
		zone := zonesByID[zoneID]

		if len(updates) == 0 {
			log.WithFields(log.Fields{
				"zoneID":   zoneID,
				"zoneName": zone.Domain,
			}).Debug("Skipping Zone, no updates found.")
			continue
		}

		records := recordsByZoneID[zoneID]

		for _, ep := range updates {
			matchedRecords := getRecordID(records, zone, ep)

			if len(matchedRecords) == 0 {
				log.WithFields(log.Fields{
					"zoneID":     zoneID,
					"dnsName":    ep.DNSName,
					"zoneName":   zone.Domain,
					"recordType": ep.RecordType,
				}).Warn("Update Records not found.")
			}

			recordType, err := convertRecordType(ep.RecordType)

			if err != nil {
				return err
			}

			matchedRecordsByTarget := make(map[string]linodego.DomainRecord)

			for _, record := range matchedRecords {
				matchedRecordsByTarget[record.Target] = record
			}

			for _, target := range ep.Targets {
				if record, ok := matchedRecordsByTarget[target]; ok {
					log.WithFields(log.Fields{
						"zoneID":     zoneID,
						"dnsName":    ep.DNSName,
						"zoneName":   zone.Domain,
						"recordType": ep.RecordType,
						"target":     target,
					}).Warn("Updating Existing Target")

					linodeUpdates = append(linodeUpdates, LinodeChangeUpdate{
						Domain:       zone,
						DomainRecord: record,
						Options: linodego.DomainRecordUpdateOptions{
							Target:   target,
							Name:     getStrippedRecordName(zone, ep),
							Type:     recordType,
							Weight:   getWeight(),
							Port:     getPort(),
							Priority: getPriority(),
							TTLSec:   int(ep.RecordTTL),
						},
					})

					delete(matchedRecordsByTarget, target)
				} else {
					// Record did not previously exist, create new 'target'
					log.WithFields(log.Fields{
						"zoneID":     zoneID,
						"dnsName":    ep.DNSName,
						"zoneName":   zone.Domain,
						"recordType": ep.RecordType,
						"target":     target,
					}).Warn("Creating New Target")

					linodeCreates = append(linodeCreates, LinodeChangeCreate{
						Domain: zone,
						Options: linodego.DomainRecordCreateOptions{
							Target:   target,
							Name:     getStrippedRecordName(zone, ep),
							Type:     recordType,
							Weight:   getWeight(),
							Port:     getPort(),
							Priority: getPriority(),
							TTLSec:   int(ep.RecordTTL),
						},
					})
				}
			}

			// Any remaining records have been removed, delete them
			for _, record := range matchedRecordsByTarget {
				log.WithFields(log.Fields{
					"zoneID":     zoneID,
					"dnsName":    ep.DNSName,
					"zoneName":   zone.Domain,
					"recordType": ep.RecordType,
					"target":     record.Target,
				}).Warn("Deleting Target")

				linodeDeletes = append(linodeDeletes, LinodeChangeDelete{
					Domain:       zone,
					DomainRecord: record,
				})
			}
		}
	}

	// Generate Deletes
	for zoneID, deletes := range deletesByZone {
		zone := zonesByID[zoneID]

		if len(deletes) == 0 {
			log.WithFields(log.Fields{
				"zoneID":   zoneID,
				"zoneName": zone.Domain,
			}).Debug("Skipping Zone, no deletes found.")
			continue
		}

		records := recordsByZoneID[zoneID]

		for _, ep := range deletes {
			matchedRecords := getRecordID(records, zone, ep)

			if len(matchedRecords) == 0 {
				log.WithFields(log.Fields{
					"zoneID":     zoneID,
					"dnsName":    ep.DNSName,
					"zoneName":   zone.Domain,
					"recordType": ep.RecordType,
				}).Warn("Records to Delete not found.")
			}

			for _, record := range matchedRecords {
				linodeDeletes = append(linodeDeletes, LinodeChangeDelete{
					Domain:       zone,
					DomainRecord: record,
				})
			}
		}
	}

	return p.submitChanges(ctx, LinodeChanges{
		Creates: linodeCreates,
		Deletes: linodeDeletes,
		Updates: linodeUpdates,
	})
}

func endpointsByZone(zoneNameIDMapper provider.ZoneIDName, endpoints []*endpoint.Endpoint) map[string][]endpoint.Endpoint {
	endpointsByZone := make(map[string][]endpoint.Endpoint)

	for _, ep := range endpoints {
		zoneID, _ := zoneNameIDMapper.FindZone(ep.DNSName)
		if zoneID == "" {
			log.Debugf("Skipping record %s because no hosted zone matching record DNS Name was detected", ep.DNSName)
			continue
		}
		endpointsByZone[zoneID] = append(endpointsByZone[zoneID], *ep)
	}

	return endpointsByZone
}

func convertRecordType(recordType string) (linodego.DomainRecordType, error) {
	switch recordType {
	case "A":
		return linodego.RecordTypeA, nil
	case "AAAA":
		return linodego.RecordTypeAAAA, nil
	case "CNAME":
		return linodego.RecordTypeCNAME, nil
	case "TXT":
		return linodego.RecordTypeTXT, nil
	case "SRV":
		return linodego.RecordTypeSRV, nil
	default:
		return "", fmt.Errorf("invalid Record Type: %s", recordType)
	}
}

func getStrippedRecordName(zone linodego.Domain, ep endpoint.Endpoint) string {
	// Handle root
	if ep.DNSName == zone.Domain {
		return ""
	}

	return strings.TrimSuffix(ep.DNSName, "."+zone.Domain)
}

func getRecordID(records []linodego.DomainRecord, zone linodego.Domain, ep endpoint.Endpoint) []linodego.DomainRecord {
	var matchedRecords []linodego.DomainRecord

	for _, record := range records {
		if record.Name == getStrippedRecordName(zone, ep) && string(record.Type) == ep.RecordType {
			matchedRecords = append(matchedRecords, record)
		}
	}

	return matchedRecords
}
