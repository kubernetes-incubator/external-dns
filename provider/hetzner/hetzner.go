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

package hetzner

import (
	"context"
	"errors"
	"os"
	"strings"

	hclouddns "git.blindage.org/21h/hcloud-dns"
	log "github.com/sirupsen/logrus"

	"sigs.k8s.io/external-dns/endpoint"
	"sigs.k8s.io/external-dns/plan"
	"sigs.k8s.io/external-dns/provider"
)

const (
	hetznerCreate = "CREATE"
	hetznerDelete = "DELETE"
	hetznerUpdate = "UPDATE"
	hetznerTTL    = 600
)

type HetznerChanges struct {
	Action            string
	ZoneID            string
	ZoneName          string
	ResourceRecordSet hclouddns.HCloudRecord
}

type HetznerProvider struct {
	provider.BaseProvider
	Client       hclouddns.HCloudClientAdapter
	domainFilter endpoint.DomainFilter
	DryRun       bool
}

func NewHetznerProvider(ctx context.Context, domainFilter endpoint.DomainFilter, dryRun bool) (*HetznerProvider, error) {
	token, ok := os.LookupEnv("HETZNER_TOKEN")
	if !ok {
		return nil, errors.New("no environment variable HETZNER_TOKEN provided")
	}

	client := hclouddns.New(token)

	provider := &HetznerProvider{
		Client:       client,
		domainFilter: domainFilter,
		DryRun:       dryRun,
	}
	return provider, nil
}

func (p *HetznerProvider) Records(ctx context.Context) ([]*endpoint.Endpoint, error) {
	zones, err := p.Client.GetZones(hclouddns.HCloudGetZonesParams{})
	if err != nil {
		return nil, err
	}
	endpoints := []*endpoint.Endpoint{}
	for _, zone := range zones.Zones {
		records, err := p.Client.GetRecords(hclouddns.HCloudGetRecordsParams{ZoneID: zone.ID})
		if err != nil {
			return nil, err
		}

		for _, r := range records.Records {
			if endpoint.SupportedRecordType(string(r.RecordType)) {
				name := r.Name + "." + zone.Name

				if r.Name == "@" {
					name = zone.Name
				}

				endpoints = append(endpoints, endpoint.NewEndpoint(name, string(r.RecordType), r.Value))
			}
		}
	}

	return endpoints, nil
}

func (p *HetznerProvider) ApplyChanges(ctx context.Context, changes *plan.Changes) error {
	combinedChanges := make([]*HetznerChanges, 0, len(changes.Create)+len(changes.UpdateNew)+len(changes.Delete))

	combinedChanges = append(combinedChanges, p.newHetznerChanges(hetznerCreate, changes.Create)...)
	combinedChanges = append(combinedChanges, p.newHetznerChanges(hetznerUpdate, changes.UpdateNew)...)
	combinedChanges = append(combinedChanges, p.newHetznerChanges(hetznerDelete, changes.Delete)...)

	return p.submitChanges(ctx, combinedChanges)
}

func (p *HetznerProvider) submitChanges(ctx context.Context, changes []*HetznerChanges) error {
	if len(changes) == 0 {
		log.Infof("All records are already up to date")
		return nil
	}

	zones, err := p.Client.GetZones(hclouddns.HCloudGetZonesParams{})
	if err != nil {
		return err
	}

	zoneChanges := p.seperateChangesByZone(zones.Zones, changes)

	for _, changes := range zoneChanges {
		for _, change := range changes {
			// Prepare record name
			recordName := strings.TrimSuffix(change.ResourceRecordSet.Name, "."+change.ZoneName)
			if recordName == change.ZoneName {
				recordName = "@"
			}
			if change.ResourceRecordSet.RecordType == hclouddns.CNAME && !strings.HasSuffix(change.ResourceRecordSet.Value, ".") {
				change.ResourceRecordSet.Value += "."
			}
			change.ResourceRecordSet.Name = recordName

			// Get ID of record if not create operation
			if change.Action != hetznerCreate {
				allRecords, err := p.Client.GetRecords(hclouddns.HCloudGetRecordsParams{ZoneID: change.ZoneID})
				if err != nil {
					return err
				}
				for _, record := range allRecords.Records {
					if record.Name == change.ResourceRecordSet.Name && record.RecordType == change.ResourceRecordSet.RecordType {
						change.ResourceRecordSet.ID = record.ID
						break
					}
				}
			}

			log.WithFields(log.Fields{
				"id":      change.ResourceRecordSet.ID,
				"record":  change.ResourceRecordSet.Name,
				"type":    change.ResourceRecordSet.RecordType,
				"value":   change.ResourceRecordSet.Value,
				"ttl":     change.ResourceRecordSet.TTL,
				"action":  change.Action,
				"zone":    change.ZoneName,
				"zone_id": change.ZoneID,
			}).Info("Changing record")

			change.ResourceRecordSet.Name = strings.TrimSuffix(change.ResourceRecordSet.Name, "."+change.ZoneName)
			if change.ResourceRecordSet.Name == change.ZoneName {
				change.ResourceRecordSet.Name = "@"
			}
			if change.ResourceRecordSet.RecordType == endpoint.RecordTypeCNAME {
				change.ResourceRecordSet.Value += "."
			}

			switch change.Action {
			case hetznerCreate:
				record := hclouddns.HCloudRecord{
					RecordType: change.ResourceRecordSet.RecordType,
					ZoneID:     change.ZoneID,
					Name:       change.ResourceRecordSet.Name,
					Value:      change.ResourceRecordSet.Value,
					TTL:        change.ResourceRecordSet.TTL,
				}
				answer, err := p.Client.CreateRecord(record)
				if err != nil {
					log.WithFields(log.Fields{
						"Code":         answer.Error.Code,
						"Message":      answer.Error.Message,
						"Record name":  answer.Record.Name,
						"Record type":  answer.Record.RecordType,
						"Record value": answer.Record.Value,
					}).Warning("Create problem")
					return err
				}
			case hetznerDelete:
				answer, err := p.Client.DeleteRecord(change.ResourceRecordSet.ID)
				if err != nil {
					log.WithFields(log.Fields{
						"Code":    answer.Error.Code,
						"Message": answer.Error.Message,
					}).Warning("Delete problem")
					return err
				}
			case hetznerUpdate:
				record := hclouddns.HCloudRecord{
					RecordType: change.ResourceRecordSet.RecordType,
					ZoneID:     change.ZoneID,
					Name:       change.ResourceRecordSet.Name,
					Value:      change.ResourceRecordSet.Value,
					TTL:        change.ResourceRecordSet.TTL,
					ID:         change.ResourceRecordSet.ID,
				}
				answer, err := p.Client.UpdateRecord(record)
				if err != nil {
					log.WithFields(log.Fields{
						"Code":         answer.Error.Code,
						"Message":      answer.Error.Message,
						"Record name":  answer.Record.Name,
						"Record type":  answer.Record.RecordType,
						"Record value": answer.Record.Value,
					}).Warning("Update problem")
					return err
				}
			}
		}
	}

	return nil
}

func (p *HetznerProvider) newHetznerChanges(action string, endpoints []*endpoint.Endpoint) []*HetznerChanges {
	changes := make([]*HetznerChanges, 0, len(endpoints))
	ttl := hetznerTTL
	for _, e := range endpoints {
		if e.RecordTTL.IsConfigured() {
			ttl = int(e.RecordTTL)
		}
		change := &HetznerChanges{
			Action: action,
			ResourceRecordSet: hclouddns.HCloudRecord{
				RecordType: hclouddns.RecordType(e.RecordType),
				Name:       e.DNSName,
				Value:      e.Targets[0],
				TTL:        ttl,
			},
		}
		changes = append(changes, change)
	}
	return changes
}

func (p *HetznerProvider) seperateChangesByZone(zones []hclouddns.HCloudZone, changes []*HetznerChanges) map[string][]*HetznerChanges {
	change := make(map[string][]*HetznerChanges)
	zoneNameID := provider.ZoneIDName{}

	for _, z := range zones {
		zoneNameID.Add(z.ID, z.Name)
		change[z.ID] = []*HetznerChanges{}
	}

	for _, c := range changes {
		zoneID, zoneName := zoneNameID.FindZone(c.ResourceRecordSet.Name)
		if zoneName == "" {
			log.Debugf("Skipping record %s because no hosted zone matching record DNS Name was detected", c.ResourceRecordSet.Name)
			continue
		}
		c.ZoneName = zoneName
		c.ZoneID = zoneID
		change[zoneID] = append(change[zoneID], c)
	}
	return change
}
