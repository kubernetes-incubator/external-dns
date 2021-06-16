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

package vinyldns

import (
	"context"
	"fmt"
	"os"
	"strings"

	log "github.com/sirupsen/logrus"
	"github.com/vinyldns/go-vinyldns/vinyldns"

	"sigs.k8s.io/external-dns/endpoint"
	"sigs.k8s.io/external-dns/plan"
	"sigs.k8s.io/external-dns/provider"
)

const (
	vinyldnsCreate = "CREATE"
	vinyldnsDelete = "DELETE"
	vinyldnsUpdate = "UPDATE"

	vinyldnsRecordTTL = 300
)

type vinyldnsZoneInterface interface {
	Zones() ([]vinyldns.Zone, error)
	RecordSets(id string) ([]vinyldns.RecordSet, error)
	RecordSet(zoneID, recordSetID string) (vinyldns.RecordSet, error)
	RecordSetCreate(rs *vinyldns.RecordSet) (*vinyldns.RecordSetUpdateResponse, error)
	RecordSetUpdate(rs *vinyldns.RecordSet) (*vinyldns.RecordSetUpdateResponse, error)
	RecordSetDelete(zoneID, recordSetID string) (*vinyldns.RecordSetUpdateResponse, error)
}

type vinyldnsProvider struct {
	provider.BaseProvider
	client       vinyldnsZoneInterface
	zoneFilter   provider.ZoneIDFilter
	domainFilter endpoint.DomainFilter
	dryRun       bool
}

type vinyldnsChange struct {
	Action            string
	ResourceRecordSet vinyldns.RecordSet
}

// NewVinylDNSProvider provides support for VinylDNS records
func NewVinylDNSProvider(domainFilter endpoint.DomainFilter, zoneFilter provider.ZoneIDFilter, dryRun bool) (provider.Provider, error) {
	_, ok := os.LookupEnv("VINYLDNS_ACCESS_KEY")
	if !ok {
		return nil, fmt.Errorf("no vinyldns access key found")
	}

	client := vinyldns.NewClientFromEnv()

	return &vinyldnsProvider{
		client:       client,
		dryRun:       dryRun,
		zoneFilter:   zoneFilter,
		domainFilter: domainFilter,
	}, nil
}

func (p *vinyldnsProvider) Records(ctx context.Context) (endpoints []*endpoint.Endpoint, _ error) {
	zones, err := p.client.Zones()
	if err != nil {
		return nil, err
	}

	for _, zone := range zones {
		if !p.zoneFilter.Match(zone.ID) {
			continue
		}

		if !p.domainFilter.Match(zone.Name) {
			continue
		}

		log.Infof(fmt.Sprintf("Zone: [%s:%s]", zone.ID, zone.Name))
		records, err := p.client.RecordSets(zone.ID)
		if err != nil {
			return nil, err
		}

		for _, r := range records {
			if endpoint.SupportedRecordType(r.Type) {
				recordsCount := len(r.Records)
				log.Debugf(fmt.Sprintf("%s.%s.%d.%s", r.Name, r.Type, recordsCount, zone.Name))

				//TODO: AAAA Records
				if len(r.Records) > 0 {
					targets := make([]string, len(r.Records))
					for idx, rr := range r.Records {
						switch r.Type {
						case "A":
							targets[idx] = rr.Address
						case "CNAME":
							targets[idx] = rr.CName
						case "TXT":
							targets[idx] = rr.Text
						}
					}

					endpoints = append(endpoints, endpoint.NewEndpointWithTTL(r.Name+"."+zone.Name, r.Type, endpoint.TTL(r.TTL), targets...))
				}
			}
		}
	}

	return endpoints, nil
}

func vinyldnsSuitableZone(hostname string, zones []vinyldns.Zone) *vinyldns.Zone {
	var zone *vinyldns.Zone
	for _, z := range zones {
		log.Debugf("hostname: %s and zoneName: %s", hostname, z.Name)
		// Adding a . as vinyl appends it to each zone record
		if strings.HasSuffix(hostname+".", z.Name) {
			zone = &z
			break
		}
	}
	return zone
}

func (p *vinyldnsProvider) submitChanges(changes []*vinyldnsChange) error {
	if len(changes) == 0 {
		log.Infof("All records are already up to date")
		return nil
	}

	zones, err := p.client.Zones()
	if err != nil {
		return err
	}

	for _, change := range changes {
		zone := vinyldnsSuitableZone(change.ResourceRecordSet.Name, zones)
		if zone == nil {
			log.Debugf("Skipping record %s because no hosted zone matching record DNS Name was detected", change.ResourceRecordSet.Name)
			continue
		}

		change.ResourceRecordSet.Name = strings.TrimSuffix(change.ResourceRecordSet.Name+".", "."+zone.Name)
		change.ResourceRecordSet.ZoneID = zone.ID
		log.Infof("Changing records: %s %v in zone: %s", change.Action, change.ResourceRecordSet, zone.Name)

		if !p.dryRun {
			switch change.Action {
			case vinyldnsCreate:
				_, err := p.client.RecordSetCreate(&change.ResourceRecordSet)
				if err != nil {
					return err
				}
			case vinyldnsUpdate:
				recordID, err := p.findRecordSetID(zone.ID, change.ResourceRecordSet.Name)
				if err != nil {
					return err
				}
				change.ResourceRecordSet.ID = recordID
				_, err = p.client.RecordSetUpdate(&change.ResourceRecordSet)
				if err != nil {
					return err
				}
			case vinyldnsDelete:
				recordID, err := p.findRecordSetID(zone.ID, change.ResourceRecordSet.Name)
				if err != nil {
					return err
				}
				_, err = p.client.RecordSetDelete(zone.ID, recordID)
				if err != nil {
					return err
				}
			}
		}
	}

	return nil
}

func (p *vinyldnsProvider) findRecordSetID(zoneID string, recordSetName string) (recordID string, err error) {
	records, err := p.client.RecordSets(zoneID)
	if err != nil {
		return "", err
	}

	for _, r := range records {
		if r.Name == recordSetName {
			return r.ID, nil
		}
	}

	return "", fmt.Errorf("record not found")
}

func (p *vinyldnsProvider) ApplyChanges(ctx context.Context, changes *plan.Changes) error {
	combinedChanges := make([]*vinyldnsChange, 0, len(changes.Create)+len(changes.UpdateNew)+len(changes.Delete))

	combinedChanges = append(combinedChanges, newVinylDNSChanges(vinyldnsCreate, changes.Create)...)
	combinedChanges = append(combinedChanges, newVinylDNSChanges(vinyldnsUpdate, changes.UpdateNew)...)
	combinedChanges = append(combinedChanges, newVinylDNSChanges(vinyldnsDelete, changes.Delete)...)

	return p.submitChanges(combinedChanges)
}

// newVinylDNSChanges returns a collection of Changes based on the given records and action.
func newVinylDNSChanges(action string, endpoints []*endpoint.Endpoint) []*vinyldnsChange {
	changes := make([]*vinyldnsChange, 0, len(endpoints))

	for _, e := range endpoints {
		changes = append(changes, newVinylDNSChange(action, e))
	}

	return changes
}

func newVinylDNSChange(action string, endpoint *endpoint.Endpoint) *vinyldnsChange {
	var ttl = vinyldnsRecordTTL
	if endpoint.RecordTTL.IsConfigured() {
		ttl = int(endpoint.RecordTTL)
	}

	records := []vinyldns.Record{}

	// TODO: AAAA
	if endpoint.RecordType == "CNAME" {
		records = []vinyldns.Record{
			{
				CName: endpoint.Targets[0],
			},
		}
	} else if endpoint.RecordType == "TXT" {
		records = []vinyldns.Record{
			{
				Text: endpoint.Targets[0],
			},
		}
	} else if endpoint.RecordType == "A" {
		records = []vinyldns.Record{
			{
				Address: endpoint.Targets[0],
			},
		}
	}

	change := &vinyldnsChange{
		Action: action,
		ResourceRecordSet: vinyldns.RecordSet{
			Name:    endpoint.DNSName,
			Type:    endpoint.RecordType,
			TTL:     ttl,
			Records: records,
		},
	}
	return change
}
