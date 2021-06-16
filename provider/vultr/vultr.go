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

package vultr

import (
	"context"
	"fmt"
	"os"
	"strings"

	log "github.com/sirupsen/logrus"
	"github.com/vultr/govultr/v2"
	"golang.org/x/oauth2"

	"sigs.k8s.io/external-dns/endpoint"
	"sigs.k8s.io/external-dns/plan"
	"sigs.k8s.io/external-dns/provider"
)

const (
	vultrCreate = "CREATE"
	vultrDelete = "DELETE"
	vultrUpdate = "UPDATE"
	vultrTTL    = 3600
)

// VultrProvider is an implementation of Provider for Vultr DNS.
type VultrProvider struct {
	provider.BaseProvider
	client govultr.Client

	domainFilter endpoint.DomainFilter
	DryRun       bool
}

// VultrChanges differentiates between ChangActions.
type VultrChanges struct {
	Action string

	ResourceRecordSet *govultr.DomainRecordReq
}

// NewVultrProvider initializes a new Vultr BNS based provider
func NewVultrProvider(ctx context.Context, domainFilter endpoint.DomainFilter, dryRun bool) (*VultrProvider, error) {
	apiKey, ok := os.LookupEnv("VULTR_API_KEY")
	if !ok {
		return nil, fmt.Errorf("no token found")
	}

	oauthClient := oauth2.NewClient(ctx, oauth2.StaticTokenSource(&oauth2.Token{
		AccessToken: apiKey,
	}))
	client := govultr.NewClient(oauthClient)
	client.SetUserAgent(fmt.Sprintf("ExternalDNS/%s", client.UserAgent))

	p := &VultrProvider{
		client:       *client,
		domainFilter: domainFilter,
		DryRun:       dryRun,
	}

	return p, nil
}

// Zones returns list of hosted zones
func (p *VultrProvider) Zones(ctx context.Context) ([]govultr.Domain, error) {
	zones, err := p.fetchZones(ctx)
	if err != nil {
		return nil, err
	}

	return zones, nil
}

// Records returns the list of records.
func (p *VultrProvider) Records(ctx context.Context) ([]*endpoint.Endpoint, error) {
	zones, err := p.Zones(ctx)
	if err != nil {
		return nil, err
	}

	var endpoints []*endpoint.Endpoint

	for _, zone := range zones {
		records, err := p.fetchRecords(ctx, zone.Domain)
		if err != nil {
			return nil, err
		}

		for _, r := range records {
			if endpoint.SupportedRecordType(r.Type) {
				name := fmt.Sprintf("%s.%s", r.Name, zone.Domain)

				// root name is identified by the empty string and should be
				// translated to zone name for the endpoint entry.
				if r.Name == "" {
					name = zone.Domain
				}

				endpoints = append(endpoints, endpoint.NewEndpointWithTTL(name, r.Type, endpoint.TTL(r.TTL), r.Data))
			}
		}
	}

	return endpoints, nil
}

func (p *VultrProvider) fetchRecords(ctx context.Context, domain string) ([]govultr.DomainRecord, error) {
	var allRecords []govultr.DomainRecord
	listOptions := &govultr.ListOptions{}

	for {
		records, meta, err := p.client.DomainRecord.List(ctx, domain, listOptions)
		if err != nil {
			return nil, err
		}

		allRecords = append(allRecords, records...)

		if meta.Links.Next == "" {
			break
		} else {
			listOptions.Cursor = meta.Links.Next
			continue
		}
	}

	return allRecords, nil
}

func (p *VultrProvider) fetchZones(ctx context.Context) ([]govultr.Domain, error) {
	var zones []govultr.Domain
	listOptions := &govultr.ListOptions{}

	for {
		allZones, meta, err := p.client.Domain.List(ctx, listOptions)
		if err != nil {
			return nil, err
		}

		for _, zone := range allZones {
			if p.domainFilter.Match(zone.Domain) {
				zones = append(zones, zone)
			}
		}

		if meta.Links.Next == "" {
			break
		} else {
			listOptions.Cursor = meta.Links.Next
			continue
		}
	}

	return zones, nil
}

func (p *VultrProvider) submitChanges(ctx context.Context, changes []*VultrChanges) error {
	if len(changes) == 0 {
		log.Infof("All records are already up to date")
		return nil
	}

	zones, err := p.Zones(ctx)
	if err != nil {
		return err
	}

	zoneChanges := separateChangesByZone(zones, changes)

	for zoneName, changes := range zoneChanges {
		for _, change := range changes {
			log.WithFields(log.Fields{
				"record": change.ResourceRecordSet.Name,
				"type":   change.ResourceRecordSet.Type,
				"ttl":    change.ResourceRecordSet.TTL,
				"action": change.Action,
				"zone":   zoneName,
			}).Info("Changing record.")

			switch change.Action {
			case vultrCreate:
				if _, err := p.client.DomainRecord.Create(ctx, zoneName, change.ResourceRecordSet); err != nil {
					return err
				}
			case vultrDelete:
				id, err := p.getRecordID(ctx, zoneName, change.ResourceRecordSet)
				if err != nil {
					return err
				}

				if err := p.client.DomainRecord.Delete(ctx, zoneName, id); err != nil {
					return err
				}
			case vultrUpdate:
				id, err := p.getRecordID(ctx, zoneName, change.ResourceRecordSet)
				if err != nil {
					return err
				}
				if err := p.client.DomainRecord.Update(ctx, zoneName, id, change.ResourceRecordSet); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

// ApplyChanges applies a given set of changes in a given zone.
func (p *VultrProvider) ApplyChanges(ctx context.Context, changes *plan.Changes) error {
	combinedChanges := make([]*VultrChanges, 0, len(changes.Create)+len(changes.UpdateNew)+len(changes.Delete))

	combinedChanges = append(combinedChanges, newVultrChanges(vultrCreate, changes.Create)...)
	combinedChanges = append(combinedChanges, newVultrChanges(vultrUpdate, changes.UpdateNew)...)
	combinedChanges = append(combinedChanges, newVultrChanges(vultrDelete, changes.Delete)...)

	return p.submitChanges(ctx, combinedChanges)
}

func newVultrChanges(action string, endpoints []*endpoint.Endpoint) []*VultrChanges {
	changes := make([]*VultrChanges, 0, len(endpoints))
	ttl := vultrTTL
	for _, e := range endpoints {
		if e.RecordTTL.IsConfigured() {
			ttl = int(e.RecordTTL)
		}

		change := &VultrChanges{
			Action: action,
			ResourceRecordSet: &govultr.DomainRecordReq{
				Type: e.RecordType,
				Name: e.DNSName,
				Data: e.Targets[0],
				TTL:  ttl,
			},
		}

		changes = append(changes, change)
	}
	return changes
}

func separateChangesByZone(zones []govultr.Domain, changes []*VultrChanges) map[string][]*VultrChanges {
	change := make(map[string][]*VultrChanges)
	zoneNameID := provider.ZoneIDName{}

	for _, z := range zones {
		zoneNameID.Add(z.Domain, z.Domain)
		change[z.Domain] = []*VultrChanges{}
	}

	for _, c := range changes {
		zone, _ := zoneNameID.FindZone(c.ResourceRecordSet.Name)
		if zone == "" {
			log.Debugf("Skipping record %s because no hosted zone matching record DNS Name was detected", c.ResourceRecordSet.Name)
			continue
		}
		change[zone] = append(change[zone], c)
	}
	return change
}

func (p *VultrProvider) getRecordID(ctx context.Context, zone string, record *govultr.DomainRecordReq) (recordID string, err error) {
	listOptions := &govultr.ListOptions{}
	for {
		records, meta, err := p.client.DomainRecord.List(ctx, zone, listOptions)
		if err != nil {
			return "0", err
		}

		for _, r := range records {
			strippedName := strings.TrimSuffix(record.Name, "."+zone)
			if record.Name == zone {
				strippedName = ""
			}

			if r.Name == strippedName && r.Type == record.Type {
				return r.ID, nil
			}
		}
		if meta.Links.Next == "" {
			break
		} else {
			listOptions.Cursor = meta.Links.Next
			continue
		}
	}

	return "", fmt.Errorf("no record was found")
}
