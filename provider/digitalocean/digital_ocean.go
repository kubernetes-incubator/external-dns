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

package digitalocean

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/digitalocean/godo"
	log "github.com/sirupsen/logrus"
	"golang.org/x/oauth2"

	"sigs.k8s.io/external-dns/endpoint"
	"sigs.k8s.io/external-dns/pkg/apis/externaldns"
	"sigs.k8s.io/external-dns/plan"
	"sigs.k8s.io/external-dns/provider"
)

const (
	// digitalOceanRecordTTL is the default TTL value
	digitalOceanRecordTTL = 300
)

// DigitalOceanProvider is an implementation of Provider for Digital Ocean's DNS.
type DigitalOceanProvider struct {
	provider.BaseProvider
	Client godo.DomainsService
	// only consider hosted zones managing domains ending in this suffix
	domainFilter endpoint.DomainFilter
	// page size when querying paginated APIs
	apiPageSize int
	DryRun      bool
}

type digitalOceanChangeCreate struct {
	Domain  string
	Options *godo.DomainRecordEditRequest
}

type digitalOceanChangeUpdate struct {
	Domain       string
	DomainRecord godo.DomainRecord
	Options      *godo.DomainRecordEditRequest
}

type digitalOceanChangeDelete struct {
	Domain   string
	RecordID int
}

// DigitalOceanChange contains all changes to apply to DNS
type digitalOceanChanges struct {
	Creates []*digitalOceanChangeCreate
	Updates []*digitalOceanChangeUpdate
	Deletes []*digitalOceanChangeDelete
}

func (c *digitalOceanChanges) Empty() bool {
	return len(c.Creates) == 0 && len(c.Updates) == 0 && len(c.Deletes) == 0
}

// NewDigitalOceanProvider initializes a new DigitalOcean DNS based Provider.
func NewDigitalOceanProvider(ctx context.Context, domainFilter endpoint.DomainFilter, dryRun bool, apiPageSize int) (*DigitalOceanProvider, error) {
	token, ok := os.LookupEnv("DO_TOKEN")
	if !ok {
		return nil, fmt.Errorf("no token found")
	}
	oauthClient := oauth2.NewClient(ctx, oauth2.StaticTokenSource(&oauth2.Token{
		AccessToken: token,
	}))
	client, err := godo.New(oauthClient, godo.SetUserAgent("ExternalDNS/"+externaldns.Version))
	if err != nil {
		return nil, err
	}

	p := &DigitalOceanProvider{
		Client:       client.Domains,
		domainFilter: domainFilter,
		apiPageSize:  apiPageSize,
		DryRun:       dryRun,
	}
	return p, nil
}

// Zones returns the list of hosted zones.
func (p *DigitalOceanProvider) Zones(ctx context.Context) ([]godo.Domain, error) {
	result := []godo.Domain{}

	zones, err := p.fetchZones(ctx)
	if err != nil {
		return nil, err
	}

	for _, zone := range zones {
		if p.domainFilter.Match(zone.Name) {
			result = append(result, zone)
		}
	}

	return result, nil
}

// Merge Endpoints with the same Name and Type into a single endpoint with multiple Targets.
func mergeEndpointsByNameType(endpoints []*endpoint.Endpoint) []*endpoint.Endpoint {
	endpointsByNameType := map[string][]*endpoint.Endpoint{}

	for _, e := range endpoints {
		key := fmt.Sprintf("%s-%s", e.DNSName, e.RecordType)
		endpointsByNameType[key] = append(endpointsByNameType[key], e)
	}

	// If no merge occurred, just return the existing endpoints.
	if len(endpointsByNameType) == len(endpoints) {
		return endpoints
	}

	// Otherwise, construct a new list of endpoints with the endpoints merged.
	var result []*endpoint.Endpoint
	for _, endpoints := range endpointsByNameType {
		dnsName := endpoints[0].DNSName
		recordType := endpoints[0].RecordType

		targets := make([]string, len(endpoints))
		for i, e := range endpoints {
			targets[i] = e.Targets[0]
		}

		e := endpoint.NewEndpoint(dnsName, recordType, targets...)
		result = append(result, e)
	}

	return result
}

// Records returns the list of records in a given zone.
func (p *DigitalOceanProvider) Records(ctx context.Context) ([]*endpoint.Endpoint, error) {
	zones, err := p.Zones(ctx)
	if err != nil {
		return nil, err
	}

	endpoints := []*endpoint.Endpoint{}
	for _, zone := range zones {
		records, err := p.fetchRecords(ctx, zone.Name)
		if err != nil {
			return nil, err
		}

		for _, r := range records {
			if endpoint.SupportedRecordType(r.Type) {
				name := r.Name + "." + zone.Name

				// root name is identified by @ and should be
				// translated to zone name for the endpoint entry.
				if r.Name == "@" {
					name = zone.Name
				}

				ep := endpoint.NewEndpoint(name, r.Type, r.Data)
				endpoints = append(endpoints, ep)
			}
		}
	}

	// Merge endpoints with the same name and type (e.g., multiple A records for a single
	// DNS name) into one endpoint with multiple targets.
	endpoints = mergeEndpointsByNameType(endpoints)

	// Log the endpoints that were found.
	log.WithFields(log.Fields{
		"endpoints": endpoints,
	}).Debug("Endpoints generated from DigitalOcean DNS")

	return endpoints, nil
}

func (p *DigitalOceanProvider) fetchRecords(ctx context.Context, zoneName string) ([]godo.DomainRecord, error) {
	allRecords := []godo.DomainRecord{}
	listOptions := &godo.ListOptions{PerPage: p.apiPageSize}
	for {
		records, resp, err := p.Client.Records(ctx, zoneName, listOptions)
		if err != nil {
			return nil, err
		}
		allRecords = append(allRecords, records...)

		if resp == nil || resp.Links == nil || resp.Links.IsLastPage() {
			break
		}

		page, err := resp.Links.CurrentPage()
		if err != nil {
			return nil, err
		}

		listOptions.Page = page + 1
	}

	return allRecords, nil
}

func (p *DigitalOceanProvider) fetchZones(ctx context.Context) ([]godo.Domain, error) {
	allZones := []godo.Domain{}
	listOptions := &godo.ListOptions{PerPage: p.apiPageSize}
	for {
		zones, resp, err := p.Client.List(ctx, listOptions)
		if err != nil {
			return nil, err
		}
		allZones = append(allZones, zones...)

		if resp == nil || resp.Links == nil || resp.Links.IsLastPage() {
			break
		}

		page, err := resp.Links.CurrentPage()
		if err != nil {
			return nil, err
		}

		listOptions.Page = page + 1
	}

	return allZones, nil
}

func (p *DigitalOceanProvider) getRecordsByDomain(ctx context.Context) (map[string][]godo.DomainRecord, provider.ZoneIDName, error) {
	recordsByDomain := map[string][]godo.DomainRecord{}

	zones, err := p.Zones(ctx)
	if err != nil {
		return nil, nil, err
	}

	zonesByDomain := make(map[string]godo.Domain)
	zoneNameIDMapper := provider.ZoneIDName{}
	for _, z := range zones {
		zoneNameIDMapper.Add(z.Name, z.Name)
		zonesByDomain[z.Name] = z
	}

	// Fetch records for each zone
	for _, zone := range zones {
		records, err := p.fetchRecords(ctx, zone.Name)
		if err != nil {
			return nil, nil, err
		}

		recordsByDomain[zone.Name] = append(recordsByDomain[zone.Name], records...)
	}

	return recordsByDomain, zoneNameIDMapper, nil
}

// Make a DomainRecordEditRequest that conforms to DigitalOcean API requirements:
// - Records at root of the zone have `@` as the name
// - CNAME records must end in a `.`
func makeDomainEditRequest(domain, name, recordType, data string, ttl int) *godo.DomainRecordEditRequest {
	// Trim the domain off the name if present.
	adjustedName := strings.TrimSuffix(name, "."+domain)

	// Record at the root should be defined as @ instead of the full domain name.
	if adjustedName == domain {
		adjustedName = "@"
	}

	// For some reason the DO API requires the '.' at the end of "data" in case of CNAME request.
	// Example: {"type":"CNAME","name":"hello","data":"www.example.com."}
	if recordType == endpoint.RecordTypeCNAME && !strings.HasSuffix(data, ".") {
		data += "."
	}

	return &godo.DomainRecordEditRequest{
		Name: adjustedName,
		Type: recordType,
		Data: data,
		TTL:  ttl,
	}
}

// submitChanges applies an instance of `digitalOceanChanges` to the DigitalOcean API.
func (p *DigitalOceanProvider) submitChanges(ctx context.Context, changes *digitalOceanChanges) error {
	// return early if there is nothing to change
	if changes.Empty() {
		return nil
	}

	for _, c := range changes.Creates {
		log.WithFields(log.Fields{
			"domain":     c.Domain,
			"dnsName":    c.Options.Name,
			"recordType": c.Options.Type,
			"data":       c.Options.Data,
			"ttl":        c.Options.TTL,
		}).Debug("Creating domain record")

		if p.DryRun {
			continue
		}

		_, _, err := p.Client.CreateRecord(ctx, c.Domain, c.Options)
		if err != nil {
			return err
		}
	}

	for _, u := range changes.Updates {
		log.WithFields(log.Fields{
			"domain":     u.Domain,
			"dnsName":    u.Options.Name,
			"recordType": u.Options.Type,
			"data":       u.Options.Data,
			"ttl":        u.Options.TTL,
		}).Debug("Updating domain record")

		if p.DryRun {
			continue
		}

		_, _, err := p.Client.EditRecord(ctx, u.Domain, u.DomainRecord.ID, u.Options)
		if err != nil {
			return err
		}
	}

	for _, d := range changes.Deletes {
		log.WithFields(log.Fields{
			"domain":   d.Domain,
			"recordId": d.RecordID,
		}).Debug("Deleting domain record")

		if p.DryRun {
			continue
		}

		_, err := p.Client.DeleteRecord(ctx, d.Domain, d.RecordID)
		if err != nil {
			return err
		}
	}

	return nil
}

func getTTLFromEndpoint(ep *endpoint.Endpoint) int {
	if ep.RecordTTL.IsConfigured() {
		return int(ep.RecordTTL)
	}
	return digitalOceanRecordTTL
}

func endpointsByZone(zoneNameIDMapper provider.ZoneIDName, endpoints []*endpoint.Endpoint) map[string][]*endpoint.Endpoint {
	endpointsByZone := make(map[string][]*endpoint.Endpoint)

	for _, ep := range endpoints {
		zoneID, _ := zoneNameIDMapper.FindZone(ep.DNSName)
		if zoneID == "" {
			log.Debugf("Skipping record %s because no hosted zone matching record DNS Name was detected", ep.DNSName)
			continue
		}
		endpointsByZone[zoneID] = append(endpointsByZone[zoneID], ep)
	}

	return endpointsByZone
}

func getMatchingDomainRecords(records []godo.DomainRecord, domain string, ep *endpoint.Endpoint) []godo.DomainRecord {
	var name string
	if ep.DNSName != domain {
		name = strings.TrimSuffix(ep.DNSName, "."+domain)
	} else {
		name = "@"
	}

	var result []godo.DomainRecord
	for _, r := range records {
		if r.Name == name && r.Type == ep.RecordType {
			result = append(result, r)
		}
	}
	return result
}

func processCreateActions(
	recordsByDomain map[string][]godo.DomainRecord,
	createsByDomain map[string][]*endpoint.Endpoint,
	changes *digitalOceanChanges,
) error {
	// Process endpoints that need to be created.
	for domain, endpoints := range createsByDomain {
		if len(endpoints) == 0 {
			log.WithFields(log.Fields{
				"domain": domain,
			}).Debug("Skipping domain, no creates found.")
			continue
		}

		records := recordsByDomain[domain]

		for _, ep := range endpoints {
			// Warn if there are existing records since we expect to create only new records.
			matchingRecords := getMatchingDomainRecords(records, domain, ep)
			if len(matchingRecords) > 0 {
				log.WithFields(log.Fields{
					"domain":     domain,
					"dnsName":    ep.DNSName,
					"recordType": ep.RecordType,
				}).Warn("Preexisting records exist which should not exist for creation actions.")
			}

			ttl := getTTLFromEndpoint(ep)

			for _, target := range ep.Targets {
				changes.Creates = append(changes.Creates, &digitalOceanChangeCreate{
					Domain:  domain,
					Options: makeDomainEditRequest(domain, ep.DNSName, ep.RecordType, target, ttl),
				})
			}
		}
	}

	return nil
}

func processUpdateActions(
	recordsByDomain map[string][]godo.DomainRecord,
	updatesByDomain map[string][]*endpoint.Endpoint,
	changes *digitalOceanChanges,
) error {
	// Generate creates and updates based on existing
	for domain, updates := range updatesByDomain {
		if len(updates) == 0 {
			log.WithFields(log.Fields{
				"domain": domain,
			}).Debug("Skipping Zone, no updates found.")
			continue
		}

		records := recordsByDomain[domain]
		log.WithFields(log.Fields{
			"domain":  domain,
			"records": records,
		}).Debug("Records for domain")

		for _, ep := range updates {
			matchingRecords := getMatchingDomainRecords(records, domain, ep)

			log.WithFields(log.Fields{
				"endpoint":        ep,
				"matchingRecords": matchingRecords,
			}).Debug("matching records")

			if len(matchingRecords) == 0 {
				log.WithFields(log.Fields{
					"domain":     domain,
					"dnsName":    ep.DNSName,
					"recordType": ep.RecordType,
				}).Warn("Planning an update but no existing records found.")
			}

			matchingRecordsByTarget := map[string]godo.DomainRecord{}
			for _, r := range matchingRecords {
				matchingRecordsByTarget[r.Data] = r
			}

			ttl := getTTLFromEndpoint(ep)

			// Generate create and delete actions based on existence of a record for each target.
			for _, target := range ep.Targets {
				if record, ok := matchingRecordsByTarget[target]; ok {
					log.WithFields(log.Fields{
						"domain":     domain,
						"dnsName":    ep.DNSName,
						"recordType": ep.RecordType,
						"target":     target,
					}).Warn("Updating existing target")

					changes.Updates = append(changes.Updates, &digitalOceanChangeUpdate{
						Domain:       domain,
						DomainRecord: record,
						Options:      makeDomainEditRequest(domain, ep.DNSName, ep.RecordType, target, ttl),
					})

					delete(matchingRecordsByTarget, target)
				} else {
					// Record did not previously exist, create new 'target'
					log.WithFields(log.Fields{
						"domain":     domain,
						"dnsName":    ep.DNSName,
						"recordType": ep.RecordType,
						"target":     target,
					}).Warn("Creating new target")

					changes.Creates = append(changes.Creates, &digitalOceanChangeCreate{
						Domain:  domain,
						Options: makeDomainEditRequest(domain, ep.DNSName, ep.RecordType, target, ttl),
					})
				}
			}

			// Any remaining records have been removed, delete them
			for _, record := range matchingRecordsByTarget {
				log.WithFields(log.Fields{
					"domain":     domain,
					"dnsName":    ep.DNSName,
					"recordType": ep.RecordType,
					"target":     record.Data,
				}).Warn("Deleting target")

				changes.Deletes = append(changes.Deletes, &digitalOceanChangeDelete{
					Domain:   domain,
					RecordID: record.ID,
				})
			}
		}
	}

	return nil
}

func processDeleteActions(
	recordsByDomain map[string][]godo.DomainRecord,
	deletesByDomain map[string][]*endpoint.Endpoint,
	changes *digitalOceanChanges,
) error {
	// Generate delete actions for each deleted endpoint.
	for domain, deletes := range deletesByDomain {
		if len(deletes) == 0 {
			log.WithFields(log.Fields{
				"domain": domain,
			}).Debug("Skipping Zone, no deletes found.")
			continue
		}

		records := recordsByDomain[domain]

		for _, ep := range deletes {
			matchingRecords := getMatchingDomainRecords(records, domain, ep)

			if len(matchingRecords) == 0 {
				log.WithFields(log.Fields{
					"domain":     domain,
					"dnsName":    ep.DNSName,
					"recordType": ep.RecordType,
				}).Warn("Records to delete not found.")
			}

			for _, record := range matchingRecords {
				doDelete := false
				for _, t := range ep.Targets {
					v1 := t
					v2 := record.Data
					if ep.RecordType == endpoint.RecordTypeCNAME {
						v1 = strings.TrimSuffix(t, ".")
						v2 = strings.TrimSuffix(t, ".")
					}
					if v1 == v2 {
						doDelete = true
					}
				}

				if doDelete {
					changes.Deletes = append(changes.Deletes, &digitalOceanChangeDelete{
						Domain:   domain,
						RecordID: record.ID,
					})
				}
			}
		}
	}

	return nil
}

// ApplyChanges applies the given set of generic changes to the provider.
func (p *DigitalOceanProvider) ApplyChanges(ctx context.Context, planChanges *plan.Changes) error {
	// TODO: This should only retrieve zones affected by the given `planChanges`.
	recordsByDomain, zoneNameIDMapper, err := p.getRecordsByDomain(ctx)
	if err != nil {
		return err
	}

	createsByDomain := endpointsByZone(zoneNameIDMapper, planChanges.Create)
	updatesByDomain := endpointsByZone(zoneNameIDMapper, planChanges.UpdateNew)
	deletesByDomain := endpointsByZone(zoneNameIDMapper, planChanges.Delete)

	var changes digitalOceanChanges

	if err := processCreateActions(recordsByDomain, createsByDomain, &changes); err != nil {
		return err
	}

	if err := processUpdateActions(recordsByDomain, updatesByDomain, &changes); err != nil {
		return err
	}

	if err := processDeleteActions(recordsByDomain, deletesByDomain, &changes); err != nil {
		return err
	}

	return p.submitChanges(ctx, &changes)
}
