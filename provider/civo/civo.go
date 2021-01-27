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

package civo

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/civo/civogo"
	log "github.com/sirupsen/logrus"

	"sigs.k8s.io/external-dns/endpoint"
	"sigs.k8s.io/external-dns/plan"
	"sigs.k8s.io/external-dns/provider"
)

// CivoProvider is an implementation of Provider for Civo's DNS.
type CivoProvider struct {
	provider.BaseProvider
	Client       civogo.Client
	domainFilter endpoint.DomainFilter
	DryRun       bool
}

// CivoChanges All API calls calculated from the plan
type CivoChanges struct {
	Creates []CivoChangeCreate
	Deletes []CivoChangeDelete
	Updates []CivoChangeUpdate
}

// CivoChangeCreate Civo Domain Record Creates
type CivoChangeCreate struct {
	Domain  civogo.DNSDomain
	Options civogo.DNSRecordConfig
}

// CivoChangeUpdate Civo Domain Record Updates
type CivoChangeUpdate struct {
	Domain       civogo.DNSDomain
	DomainRecord civogo.DNSRecord
	Options      civogo.DNSRecordConfig
}

// CivoChangeDelete Civo Domain Record Deletes
type CivoChangeDelete struct {
	Domain       civogo.DNSDomain
	DomainRecord civogo.DNSRecord
}

// NewCivoProvider initializes a new Civo DNS based Provider.
func NewCivoProvider(domainFilter endpoint.DomainFilter, dryRun bool) (*CivoProvider, error) {
	token, ok := os.LookupEnv("CIVO_TOKEN")
	if !ok {
		return nil, fmt.Errorf("no token found")
	}

	civoClient, err := civogo.NewClient(token)
	if err != nil {
		return nil, err
	}
	provider := &CivoProvider{
		Client:       *civoClient,
		domainFilter: domainFilter,
		DryRun:       dryRun,
	}
	return provider, nil
}

// Zones returns the list of hosted zones.
func (p *CivoProvider) Zones(ctx context.Context) ([]civogo.DNSDomain, error) {
	zones, err := p.fetchZones(ctx)
	if err != nil {
		return nil, err
	}

	return zones, nil
}

// Records returns the list of records in a given zone.
func (p *CivoProvider) Records(ctx context.Context) ([]*endpoint.Endpoint, error) {
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
			toUpper := strings.ToUpper(string(r.Type))
			if provider.SupportedRecordType(toUpper) {
				name := fmt.Sprintf("%s.%s", r.Name, zone.Name)

				// root name is identified by the empty string and should be
				// translated to zone name for the endpoint entry.
				if r.Name == "" {
					name = zone.Name
				}

				endpoints = append(endpoints, endpoint.NewEndpointWithTTL(name, toUpper, endpoint.TTL(r.TTL), r.Value))
			}
		}
	}

	return endpoints, nil
}

func (p *CivoProvider) fetchRecords(ctx context.Context, domainID string) ([]civogo.DNSRecord, error) {
	records, err := p.Client.ListDNSRecords(domainID)
	if err != nil {
		return nil, err
	}

	return records, nil
}

func (p *CivoProvider) fetchZones(ctx context.Context) ([]civogo.DNSDomain, error) {
	var zones []civogo.DNSDomain

	allZones, err := p.Client.ListDNSDomains()
	if err != nil {
		return nil, err
	}

	for _, zone := range allZones {
		if !p.domainFilter.Match(zone.Name) {
			continue
		}

		zones = append(zones, zone)
	}

	return zones, nil
}

// submitChanges takes a zone and a collection of Changes and sends them as a single transaction.
func (p *CivoProvider) submitChanges(ctx context.Context, changes CivoChanges) error {
	for _, change := range changes.Creates {
		logFields := log.Fields{
			"Type":     change.Options.Type,
			"Name":     change.Options.Name,
			"Value":    change.Options.Value,
			"Priority": change.Options.Priority,
			"TTL":      change.Options.TTL,
			"action":   "Create",
		}

		log.WithFields(logFields).Info("Creating record.")

		if p.DryRun {
			log.WithFields(logFields).Info("Would create record.")
		} else if _, err := p.Client.CreateDNSRecord(change.Domain.ID, &change.Options); err != nil {
			log.WithFields(logFields).Errorf(
				"Failed to Create record: %v",
				err,
			)
		}
	}

	for _, change := range changes.Deletes {
		logFields := log.Fields{
			"Type":     change.DomainRecord.Type,
			"Name":     change.DomainRecord.Name,
			"Value":    change.DomainRecord.Value,
			"Priority": change.DomainRecord.Priority,
			"TTL":      change.DomainRecord.TTL,
			"action":   "Delete",
		}

		log.WithFields(logFields).Info("Deleting record.")

		if p.DryRun {
			log.WithFields(logFields).Info("Would delete record.")
		} else if _, err := p.Client.DeleteDNSRecord(&change.DomainRecord); err != nil {
			log.WithFields(logFields).Errorf(
				"Failed to Delete record: %v",
				err,
			)
		}
	}

	for _, change := range changes.Updates {
		logFields := log.Fields{
			"Type":     change.DomainRecord.Type,
			"Name":     change.DomainRecord.Name,
			"Value":    change.DomainRecord.Value,
			"Priority": change.DomainRecord.Priority,
			"TTL":      change.DomainRecord.TTL,
			"action":   "Update",
		}

		log.WithFields(logFields).Info("Updating record.")

		if p.DryRun {
			log.WithFields(logFields).Info("Would update record.")
		} else if _, err := p.Client.UpdateDNSRecord(&change.DomainRecord, &change.Options); err != nil {
			log.WithFields(logFields).Errorf(
				"Failed to Update record: %v",
				err,
			)
		}
	}

	return nil
}

// ApplyChanges applies a given set of changes in a given zone.
func (p *CivoProvider) ApplyChanges(ctx context.Context, changes *plan.Changes) error {
	recordsByZoneID := make(map[string][]civogo.DNSRecord)

	zones, err := p.fetchZones(ctx)

	if err != nil {
		return err
	}

	zonesByID := make(map[string]civogo.DNSDomain)

	zoneNameIDMapper := provider.ZoneIDName{}

	for _, z := range zones {
		zoneNameIDMapper.Add(z.ID, z.Name)
		zonesByID[z.ID] = z
	}

	// Fetch records for each zone
	for _, zone := range zones {
		records, err := p.fetchRecords(ctx, zone.ID)

		if err != nil {
			return err
		}

		recordsByZoneID[zone.ID] = append(recordsByZoneID[zone.ID], records...)
	}

	createsByZone := endpointsByZone(zoneNameIDMapper, changes.Create)
	updatesByZone := endpointsByZone(zoneNameIDMapper, changes.UpdateNew)
	deletesByZone := endpointsByZone(zoneNameIDMapper, changes.Delete)

	var civoCreates []CivoChangeCreate
	var civoUpdates []CivoChangeUpdate
	var civoDeletes []CivoChangeDelete

	// Generate Creates
	for zoneID, creates := range createsByZone {
		zone := zonesByID[zoneID]

		if len(creates) == 0 {
			log.WithFields(log.Fields{
				"zoneID":   zoneID,
				"zoneName": zone.Name,
			}).Debug("Skipping Zone, no creates found.")
			continue
		}

		records := recordsByZoneID[zoneID]

		// Generate Create
		for _, ep := range creates {
			matchedRecords := getRecordID(records, zone, ep)

			if len(matchedRecords) != 0 {
				log.WithFields(log.Fields{
					"zoneID":     zoneID,
					"zoneName":   zone.Name,
					"dnsName":    ep.DNSName,
					"recordType": ep.RecordType,
				}).Warn("Records found which should not exist")
			}

			recordType, err := convertRecordType(ep.RecordType)
			if err != nil {
				return err
			}

			for _, target := range ep.Targets {
				civoCreates = append(civoCreates, CivoChangeCreate{
					Domain: zone,
					Options: civogo.DNSRecordConfig{
						Value:    target,
						Name:     getStrippedRecordName(zone, ep),
						Type:     recordType,
						Priority: 0,
						TTL:      int(ep.RecordTTL),
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
				"zoneName": zone.Name,
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
					"zoneName":   zone.Name,
					"recordType": ep.RecordType,
				}).Warn("Update Records not found.")
			}

			recordType, err := convertRecordType(ep.RecordType)

			if err != nil {
				return err
			}

			matchedRecordsByTarget := make(map[string]civogo.DNSRecord)

			for _, record := range matchedRecords {
				matchedRecordsByTarget[record.DNSDomainID] = record
			}

			for _, target := range ep.Targets {
				if record, ok := matchedRecordsByTarget[target]; ok {
					log.WithFields(log.Fields{
						"zoneID":     zoneID,
						"dnsName":    ep.DNSName,
						"zoneName":   zone.Name,
						"recordType": ep.RecordType,
						"target":     target,
					}).Warn("Updating Existing Target")

					civoUpdates = append(civoUpdates, CivoChangeUpdate{
						Domain:       zone,
						DomainRecord: record,
						Options: civogo.DNSRecordConfig{
							Value:    target,
							Name:     getStrippedRecordName(zone, ep),
							Type:     recordType,
							Priority: 0,
							TTL:      int(ep.RecordTTL),
						},
					})

					delete(matchedRecordsByTarget, target)
				} else {
					// Record did not previously exist, create new 'target'
					log.WithFields(log.Fields{
						"zoneID":     zoneID,
						"dnsName":    ep.DNSName,
						"zoneName":   zone.Name,
						"recordType": ep.RecordType,
						"target":     target,
					}).Warn("Creating New Target")

					civoCreates = append(civoCreates, CivoChangeCreate{
						Domain: zone,
						Options: civogo.DNSRecordConfig{
							Value:    target,
							Name:     getStrippedRecordName(zone, ep),
							Type:     recordType,
							Priority: 0,
							TTL:      int(ep.RecordTTL),
						},
					})
				}
			}
		}
	}

	// Generate Deletes
	for zoneID, deletes := range deletesByZone {
		zone := zonesByID[zoneID]

		if len(deletes) == 0 {
			log.WithFields(log.Fields{
				"zoneID":   zoneID,
				"zoneName": zone.Name,
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
					"zoneName":   zone.Name,
					"recordType": ep.RecordType,
				}).Warn("Records to Delete not found.")
			}

			for _, record := range matchedRecords {
				civoDeletes = append(civoDeletes, CivoChangeDelete{
					Domain:       zone,
					DomainRecord: record,
				})
			}
		}
	}

	return p.submitChanges(ctx, CivoChanges{
		Creates: civoCreates,
		Deletes: civoDeletes,
		Updates: civoUpdates,
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

func convertRecordType(recordType string) (civogo.DNSRecordType, error) {
	switch recordType {
	case "A":
		return civogo.DNSRecordTypeA, nil
	case "CNAME":
		return civogo.DNSRecordTypeCName, nil
	case "TXT":
		return civogo.DNSRecordTypeTXT, nil
	case "SRV":
		return civogo.DNSRecordTypeSRV, nil
	default:
		return "", fmt.Errorf("invalid Record Type: %s", recordType)
	}
}

func getStrippedRecordName(zone civogo.DNSDomain, ep endpoint.Endpoint) string {
	if ep.DNSName == zone.Name {
		return ""
	}

	return strings.TrimSuffix(ep.DNSName, "."+zone.Name)
}

func getRecordID(records []civogo.DNSRecord, zone civogo.DNSDomain, ep endpoint.Endpoint) []civogo.DNSRecord {
	var matchedRecords []civogo.DNSRecord

	for _, record := range records {
		stripedName := getStrippedRecordName(zone, ep)
		toUpper := strings.ToUpper(string(record.Type))
		if record.Name == stripedName && toUpper == ep.RecordType {
			matchedRecords = append(matchedRecords, record)
		}
	}

	return matchedRecords
}
