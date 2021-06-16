/*
Copyright 2019 The Kubernetes Authors.

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

package rcode0

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"strings"

	rc0 "github.com/nic-at/rc0go"
	log "github.com/sirupsen/logrus"

	"sigs.k8s.io/external-dns/endpoint"
	"sigs.k8s.io/external-dns/plan"
	"sigs.k8s.io/external-dns/provider"
)

// RcodeZeroProvider implements the DNS provider for RcodeZero Anycast DNS.
type RcodeZeroProvider struct {
	provider.BaseProvider
	Client *rc0.Client

	DomainFilter endpoint.DomainFilter
	DryRun       bool
	TXTEncrypt   bool
	Key          []byte
}

// NewRcodeZeroProvider creates a new RcodeZero Anycast DNS provider.
//
// Returns the provider or an error if a provider could not be created.
func NewRcodeZeroProvider(domainFilter endpoint.DomainFilter, dryRun bool, txtEnc bool) (*RcodeZeroProvider, error) {
	client, err := rc0.NewClient(os.Getenv("RC0_API_KEY"))

	if err != nil {
		return nil, err
	}

	value := os.Getenv("RC0_BASE_URL")
	if len(value) != 0 {
		client.BaseURL, err = url.Parse(os.Getenv("RC0_BASE_URL"))
	}

	if err != nil {
		return nil, fmt.Errorf("failed to initialize rcodezero provider: %v", err)
	}

	provider := &RcodeZeroProvider{
		Client:       client,
		DomainFilter: domainFilter,
		DryRun:       dryRun,
		TXTEncrypt:   txtEnc,
	}

	if txtEnc {
		provider.Key = []byte(os.Getenv("RC0_ENC_KEY"))
	}

	return provider, nil
}

// Zones returns filtered zones if filter is set
func (p *RcodeZeroProvider) Zones() ([]*rc0.Zone, error) {
	var result []*rc0.Zone

	zones, err := p.fetchZones()
	if err != nil {
		return nil, err
	}

	for _, zone := range zones {
		if p.DomainFilter.Match(zone.Domain) {
			result = append(result, zone)
		}
	}

	return result, nil
}

// Records returns resource records
//
// Decrypts TXT records if TXT-Encrypt flag is set and key is provided
func (p *RcodeZeroProvider) Records(ctx context.Context) ([]*endpoint.Endpoint, error) {
	zones, err := p.Zones()
	if err != nil {
		return nil, err
	}

	var endpoints []*endpoint.Endpoint

	for _, zone := range zones {
		rrset, err := p.fetchRecords(zone.Domain)

		if err != nil {
			return nil, err
		}

		for _, r := range rrset {
			if endpoint.SupportedRecordType(r.Type) {
				if p.TXTEncrypt && (p.Key != nil) && strings.EqualFold(r.Type, "TXT") {
					p.Client.RRSet.DecryptTXT(p.Key, r)
				}
				if len(r.Records) > 1 {
					for _, _r := range r.Records {
						if !_r.Disabled {
							endpoints = append(endpoints, endpoint.NewEndpointWithTTL(r.Name, r.Type, endpoint.TTL(r.TTL), _r.Content))
						}
					}
				} else if !r.Records[0].Disabled {
					endpoints = append(endpoints, endpoint.NewEndpointWithTTL(r.Name, r.Type, endpoint.TTL(r.TTL), r.Records[0].Content))
				}
			}
		}
	}

	return endpoints, nil
}

// ApplyChanges applies a given set of changes in a given zone.
func (p *RcodeZeroProvider) ApplyChanges(ctx context.Context, changes *plan.Changes) error {
	combinedChanges := make([]*rc0.RRSetChange, 0, len(changes.Create)+len(changes.UpdateNew)+len(changes.Delete))

	combinedChanges = append(combinedChanges, p.NewRcodezeroChanges(rc0.ChangeTypeADD, changes.Create)...)
	combinedChanges = append(combinedChanges, p.NewRcodezeroChanges(rc0.ChangeTypeUPDATE, changes.UpdateNew)...)
	combinedChanges = append(combinedChanges, p.NewRcodezeroChanges(rc0.ChangeTypeDELETE, changes.Delete)...)

	return p.submitChanges(combinedChanges)
}

// Helper function
func rcodezeroChangesByZone(zones []*rc0.Zone, changeSet []*rc0.RRSetChange) map[string][]*rc0.RRSetChange {
	changes := make(map[string][]*rc0.RRSetChange)
	zoneNameIDMapper := provider.ZoneIDName{}
	for _, z := range zones {
		zoneNameIDMapper.Add(z.Domain, z.Domain)
		changes[z.Domain] = []*rc0.RRSetChange{}
	}

	for _, c := range changeSet {
		zone, _ := zoneNameIDMapper.FindZone(c.Name)
		if zone == "" {
			log.Debugf("Skipping record %s because no hosted zone matching record DNS Name was detected", c.Name)
			continue
		}
		changes[zone] = append(changes[zone], c)
	}

	return changes
}

// Helper function
func (p *RcodeZeroProvider) fetchRecords(zoneName string) ([]*rc0.RRType, error) {
	var allRecords []*rc0.RRType

	listOptions := rc0.NewListOptions()

	for {
		records, page, err := p.Client.RRSet.List(zoneName, listOptions)

		if err != nil {
			return nil, err
		}

		allRecords = append(allRecords, records...)

		if page == nil || (page.CurrentPage == page.LastPage) {
			break
		}

		listOptions.SetPageNumber(page.CurrentPage + 1)
	}

	return allRecords, nil
}

// Helper function
func (p *RcodeZeroProvider) fetchZones() ([]*rc0.Zone, error) {
	var allZones []*rc0.Zone

	listOptions := rc0.NewListOptions()

	for {
		zones, page, err := p.Client.Zones.List(listOptions)
		if err != nil {
			return nil, err
		}
		allZones = append(allZones, zones...)

		if page == nil || page.IsLastPage() {
			break
		}

		listOptions.SetPageNumber(page.CurrentPage + 1)
	}

	return allZones, nil
}

// Helper function to submit changes.
//
// Changes are submitted by change type.
func (p *RcodeZeroProvider) submitChanges(changes []*rc0.RRSetChange) error {
	if len(changes) == 0 {
		return nil
	}

	zones, err := p.Zones()
	if err != nil {
		return err
	}

	// separate into per-zone change sets to be passed to the API.
	changesByZone := rcodezeroChangesByZone(zones, changes)
	for zoneName, changes := range changesByZone {
		for _, change := range changes {
			logFields := log.Fields{
				"record":  change.Name,
				"content": change.Records[0].Content,
				"type":    change.Type,
				"action":  change.ChangeType,
				"zone":    zoneName,
			}

			log.WithFields(logFields).Info("Changing record.")

			if p.DryRun {
				continue
			}

			// to avoid accidentally adding extra dot if already present
			change.Name = strings.TrimSuffix(change.Name, ".") + "."

			switch change.ChangeType {
			case rc0.ChangeTypeADD:
				sr, err := p.Client.RRSet.Create(zoneName, []*rc0.RRSetChange{change})

				if err != nil {
					return err
				}

				if sr.HasError() {
					return fmt.Errorf("adding new RR resulted in an error: %v", sr.Message)
				}

			case rc0.ChangeTypeUPDATE:
				sr, err := p.Client.RRSet.Edit(zoneName, []*rc0.RRSetChange{change})

				if err != nil {
					return err
				}

				if sr.HasError() {
					return fmt.Errorf("updating existing RR resulted in an error: %v", sr.Message)
				}

			case rc0.ChangeTypeDELETE:
				sr, err := p.Client.RRSet.Delete(zoneName, []*rc0.RRSetChange{change})

				if err != nil {
					return err
				}

				if sr.HasError() {
					return fmt.Errorf("deleting existing RR resulted in an error: %v", sr.Message)
				}

			default:
				return fmt.Errorf("unsupported changeType submitted: %v", change.ChangeType)
			}
		}
	}
	return nil
}

// NewRcodezeroChanges returns a RcodeZero specific array with rrset change objects.
func (p *RcodeZeroProvider) NewRcodezeroChanges(action string, endpoints []*endpoint.Endpoint) []*rc0.RRSetChange {
	changes := make([]*rc0.RRSetChange, 0, len(endpoints))

	for _, _endpoint := range endpoints {
		changes = append(changes, p.NewRcodezeroChange(action, _endpoint))
	}

	return changes
}

// NewRcodezeroChange returns a RcodeZero specific rrset change object.
func (p *RcodeZeroProvider) NewRcodezeroChange(action string, endpoint *endpoint.Endpoint) *rc0.RRSetChange {
	change := &rc0.RRSetChange{
		Type:       endpoint.RecordType,
		ChangeType: action,
		Name:       endpoint.DNSName,
		Records: []*rc0.Record{{
			Disabled: false,
			Content:  endpoint.Targets[0],
		}},
	}

	if p.TXTEncrypt && (p.Key != nil) && strings.EqualFold(endpoint.RecordType, "TXT") {
		p.Client.RRSet.EncryptTXT(p.Key, change)
	}

	return change
}
