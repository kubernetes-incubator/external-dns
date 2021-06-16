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

package transip

import (
	"context"
	"errors"
	"fmt"
	"strings"

	log "github.com/sirupsen/logrus"
	"github.com/transip/gotransip/v6"
	"github.com/transip/gotransip/v6/domain"

	"sigs.k8s.io/external-dns/endpoint"
	"sigs.k8s.io/external-dns/plan"
	"sigs.k8s.io/external-dns/provider"
)

const (
	// 60 seconds is the current minimal TTL for TransIP and will replace unconfigured
	// TTL's for Endpoints
	transipMinimalValidTTL = 60
)

// TransIPProvider is an implementation of Provider for TransIP.
type TransIPProvider struct {
	provider.BaseProvider
	domainRepo   domain.Repository
	domainFilter endpoint.DomainFilter
	dryRun       bool

	zoneMap provider.ZoneIDName
}

// NewTransIPProvider initializes a new TransIP Provider.
func NewTransIPProvider(accountName, privateKeyFile string, domainFilter endpoint.DomainFilter, dryRun bool) (*TransIPProvider, error) {
	// check given arguments
	if accountName == "" {
		return nil, errors.New("required --transip-account not set")
	}

	if privateKeyFile == "" {
		return nil, errors.New("required --transip-keyfile not set")
	}

	var apiMode gotransip.APIMode
	if dryRun {
		apiMode = gotransip.APIModeReadOnly
	} else {
		apiMode = gotransip.APIModeReadWrite
	}

	// create new TransIP API client
	client, err := gotransip.NewClient(gotransip.ClientConfiguration{
		AccountName:    accountName,
		PrivateKeyPath: privateKeyFile,
		Mode:           apiMode,
	})
	if err != nil {
		return nil, fmt.Errorf("could not setup TransIP API client: %s", err.Error())
	}

	// return TransIPProvider struct
	return &TransIPProvider{
		domainRepo:   domain.Repository{Client: client},
		domainFilter: domainFilter,
		dryRun:       dryRun,
		zoneMap:      provider.ZoneIDName{},
	}, nil
}

// ApplyChanges applies a given set of changes in a given zone.
func (p *TransIPProvider) ApplyChanges(ctx context.Context, changes *plan.Changes) error {
	// fetch all zones we currently have
	// this does NOT include any DNS entries, so we'll have to fetch these for
	// each zone that gets updated
	zones, err := p.domainRepo.GetAll()
	if err != nil {
		return err
	}

	// refresh zone mapping
	zoneMap := provider.ZoneIDName{}
	for _, zone := range zones {
		// TransIP API doesn't expose a unique identifier for zones, other than than
		// the domain name itself
		zoneMap.Add(zone.Name, zone.Name)
	}
	p.zoneMap = zoneMap

	// first remove obsolete DNS records
	for _, ep := range changes.Delete {
		epLog := log.WithFields(log.Fields{
			"record": ep.DNSName,
			"type":   ep.RecordType,
		})
		epLog.Info("endpoint has to go")

		zoneName, entries, err := p.entriesForEndpoint(ep)
		if err != nil {
			epLog.WithError(err).Error("could not get DNS entries")
			return err
		}

		epLog = epLog.WithField("zone", zoneName)

		if len(entries) == 0 {
			epLog.Info("no matching entries found")
			continue
		}

		if p.dryRun {
			epLog.Info("not removing DNS entries in dry-run mode")
			continue
		}

		for _, entry := range entries {
			log.WithFields(log.Fields{
				"domain":  zoneName,
				"name":    entry.Name,
				"type":    entry.Type,
				"content": entry.Content,
				"ttl":     entry.Expire,
			}).Info("removing DNS entry")

			err = p.domainRepo.RemoveDNSEntry(zoneName, entry)
			if err != nil {
				epLog.WithError(err).Error("could not remove DNS entry")
				return err
			}
		}
	}

	// then create new DNS records
	for _, ep := range changes.Create {
		epLog := log.WithFields(log.Fields{
			"record": ep.DNSName,
			"type":   ep.RecordType,
		})
		epLog.Info("endpoint should be created")

		zoneName, err := p.zoneNameForDNSName(ep.DNSName)
		if err != nil {
			epLog.WithError(err).Warn("could not find zone for endpoint")
			continue
		}

		epLog = epLog.WithField("zone", zoneName)

		if p.dryRun {
			epLog.Info("not adding DNS entries in dry-run mode")
			continue
		}

		for _, entry := range dnsEntriesForEndpoint(ep, zoneName) {
			log.WithFields(log.Fields{
				"domain":  zoneName,
				"name":    entry.Name,
				"type":    entry.Type,
				"content": entry.Content,
				"ttl":     entry.Expire,
			}).Info("creating DNS entry")

			err = p.domainRepo.AddDNSEntry(zoneName, entry)
			if err != nil {
				epLog.WithError(err).Error("could not add DNS entry")
				return err
			}
		}
	}

	// then update existing DNS records
	for _, ep := range changes.UpdateNew {
		epLog := log.WithFields(log.Fields{
			"record": ep.DNSName,
			"type":   ep.RecordType,
		})
		epLog.Debug("endpoint needs updating")

		zoneName, entries, err := p.entriesForEndpoint(ep)
		if err != nil {
			epLog.WithError(err).Error("could not get DNS entries")
			return err
		}

		epLog = epLog.WithField("zone", zoneName)

		if len(entries) == 0 {
			epLog.Info("no matching entries found")
			continue
		}

		newEntries := dnsEntriesForEndpoint(ep, zoneName)

		// check to see if actually anything changed in the DNSEntry set
		if dnsEntriesAreEqual(newEntries, entries) {
			epLog.Debug("not updating identical DNS entries")
			continue
		}

		if p.dryRun {
			epLog.Info("not updating DNS entries in dry-run mode")
			continue
		}

		// TransIP API client does have an UpdateDNSEntry call but that does only
		// allow you to update the content of a DNSEntry, not the TTL
		// to work around this, remove the old entry first and add the new entry
		for _, entry := range entries {
			log.WithFields(log.Fields{
				"domain":  zoneName,
				"name":    entry.Name,
				"type":    entry.Type,
				"content": entry.Content,
				"ttl":     entry.Expire,
			}).Info("removing DNS entry")

			err = p.domainRepo.RemoveDNSEntry(zoneName, entry)
			if err != nil {
				epLog.WithError(err).Error("could not remove DNS entry")
				return err
			}
		}

		for _, entry := range newEntries {
			log.WithFields(log.Fields{
				"domain":  zoneName,
				"name":    entry.Name,
				"type":    entry.Type,
				"content": entry.Content,
				"ttl":     entry.Expire,
			}).Info("adding DNS entry")

			err = p.domainRepo.AddDNSEntry(zoneName, entry)
			if err != nil {
				epLog.WithError(err).Error("could not add DNS entry")
				return err
			}
		}
	}

	return nil
}

// Records returns the list of records in all zones
func (p *TransIPProvider) Records(ctx context.Context) ([]*endpoint.Endpoint, error) {
	zones, err := p.domainRepo.GetAll()
	if err != nil {
		return nil, err
	}

	var endpoints []*endpoint.Endpoint
	// go over all zones and their DNS entries and create endpoints for them
	for _, zone := range zones {
		entries, err := p.domainRepo.GetDNSEntries(zone.Name)
		if err != nil {
			return nil, err
		}

		for _, r := range entries {
			if !endpoint.SupportedRecordType(r.Type) {
				continue
			}

			name := endpointNameForRecord(r, zone.Name)
			endpoints = append(endpoints, endpoint.NewEndpointWithTTL(name, r.Type, endpoint.TTL(r.Expire), r.Content))
		}
	}

	return endpoints, nil
}

func (p *TransIPProvider) entriesForEndpoint(ep *endpoint.Endpoint) (string, []domain.DNSEntry, error) {
	zoneName, err := p.zoneNameForDNSName(ep.DNSName)
	if err != nil {
		return "", nil, err
	}

	epName := recordNameForEndpoint(ep, zoneName)
	dnsEntries, err := p.domainRepo.GetDNSEntries(zoneName)
	if err != nil {
		return zoneName, nil, err
	}

	matches := []domain.DNSEntry{}
	for _, entry := range dnsEntries {
		if ep.RecordType != entry.Type {
			continue
		}

		if entry.Name == epName {
			matches = append(matches, entry)
		}
	}

	return zoneName, matches, nil
}

// endpointNameForRecord returns "www.example.org" for DNSEntry with Name "www" and
// Domain with Name "example.org"
func endpointNameForRecord(r domain.DNSEntry, zoneName string) string {
	// root name is identified by "@" and should be translated to domain name for
	// the endpoint entry.
	if r.Name == "@" {
		return zoneName
	}

	return fmt.Sprintf("%s.%s", r.Name, zoneName)
}

// recordNameForEndpoint returns "www" for Endpoint with DNSName "www.example.org"
// and Domain with Name "example.org"
func recordNameForEndpoint(ep *endpoint.Endpoint, zoneName string) string {
	// root name is identified by "@" and should be translated to domain name for
	// the endpoint entry.
	if ep.DNSName == zoneName {
		return "@"
	}

	return strings.TrimSuffix(ep.DNSName, "."+zoneName)
}

// getMinimalValidTTL returns max between given Endpoint's RecordTTL and
// transipMinimalValidTTL
func getMinimalValidTTL(ep *endpoint.Endpoint) int {
	// TTL cannot be lower than transipMinimalValidTTL
	if ep.RecordTTL < transipMinimalValidTTL {
		return transipMinimalValidTTL
	}

	return int(ep.RecordTTL)
}

// dnsEntriesAreEqual compares the entries in 2 sets and returns true if the
// content of the entries is equal
func dnsEntriesAreEqual(a, b []domain.DNSEntry) bool {
	if len(a) != len(b) {
		return false
	}

	match := 0
	for _, aa := range a {
		for _, bb := range b {
			if aa.Content != bb.Content {
				continue
			}

			if aa.Name != bb.Name {
				continue
			}

			if aa.Expire != bb.Expire {
				continue
			}

			if aa.Type != bb.Type {
				continue
			}

			match++
		}
	}

	return (len(a) == match)
}

// dnsEntriesForEndpoint creates DNS entries for given endpoint and returns
// resulting DNS entry set
func dnsEntriesForEndpoint(ep *endpoint.Endpoint, zoneName string) []domain.DNSEntry {
	ttl := getMinimalValidTTL(ep)

	entries := []domain.DNSEntry{}
	for _, target := range ep.Targets {
		// external hostnames require a trailing dot in TransIP API
		if ep.RecordType == "CNAME" {
			target = provider.EnsureTrailingDot(target)
		}

		entries = append(entries, domain.DNSEntry{
			Name:    recordNameForEndpoint(ep, zoneName),
			Expire:  ttl,
			Type:    ep.RecordType,
			Content: target,
		})
	}

	return entries
}

// zoneForZoneName returns the zone mapped to given name or error if zone could
// not be found
func (p *TransIPProvider) zoneNameForDNSName(name string) (string, error) {
	_, zoneName := p.zoneMap.FindZone(name)
	if zoneName == "" {
		return "", fmt.Errorf("could not find zoneName for %s", name)
	}

	return zoneName, nil
}
