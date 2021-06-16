/*
Copyright 2018 The Kubernetes Authors.

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

package oci

import (
	"context"
	"io/ioutil"
	"strings"

	"github.com/oracle/oci-go-sdk/common"
	"github.com/oracle/oci-go-sdk/dns"
	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"
	yaml "gopkg.in/yaml.v2"

	"sigs.k8s.io/external-dns/endpoint"
	"sigs.k8s.io/external-dns/plan"
	"sigs.k8s.io/external-dns/provider"
)

const ociRecordTTL = 300

// OCIAuthConfig holds connection parameters for the OCI API.
type OCIAuthConfig struct {
	Region      string `yaml:"region"`
	TenancyID   string `yaml:"tenancy"`
	UserID      string `yaml:"user"`
	PrivateKey  string `yaml:"key"`
	Fingerprint string `yaml:"fingerprint"`
	Passphrase  string `yaml:"passphrase"`
}

// OCIConfig holds the configuration for the OCI Provider.
type OCIConfig struct {
	Auth          OCIAuthConfig `yaml:"auth"`
	CompartmentID string        `yaml:"compartment"`
}

// OCIProvider is an implementation of Provider for Oracle Cloud Infrastructure
// (OCI) DNS.
type OCIProvider struct {
	provider.BaseProvider
	client ociDNSClient
	cfg    OCIConfig

	domainFilter endpoint.DomainFilter
	zoneIDFilter provider.ZoneIDFilter
	dryRun       bool
}

// ociDNSClient is the subset of the OCI DNS API required by the OCI Provider.
type ociDNSClient interface {
	ListZones(ctx context.Context, request dns.ListZonesRequest) (response dns.ListZonesResponse, err error)
	GetZoneRecords(ctx context.Context, request dns.GetZoneRecordsRequest) (response dns.GetZoneRecordsResponse, err error)
	PatchZoneRecords(ctx context.Context, request dns.PatchZoneRecordsRequest) (response dns.PatchZoneRecordsResponse, err error)
}

// LoadOCIConfig reads and parses the OCI ExternalDNS config file at the given
// path.
func LoadOCIConfig(path string) (*OCIConfig, error) {
	contents, err := ioutil.ReadFile(path)
	if err != nil {
		return nil, errors.Wrapf(err, "reading OCI config file %q", path)
	}

	cfg := OCIConfig{}
	if err := yaml.Unmarshal(contents, &cfg); err != nil {
		return nil, errors.Wrapf(err, "parsing OCI config file %q", path)
	}
	return &cfg, nil
}

// NewOCIProvider initializes a new OCI DNS based Provider.
func NewOCIProvider(cfg OCIConfig, domainFilter endpoint.DomainFilter, zoneIDFilter provider.ZoneIDFilter, dryRun bool) (*OCIProvider, error) {
	var client ociDNSClient
	client, err := dns.NewDnsClientWithConfigurationProvider(common.NewRawConfigurationProvider(
		cfg.Auth.TenancyID,
		cfg.Auth.UserID,
		cfg.Auth.Region,
		cfg.Auth.Fingerprint,
		cfg.Auth.PrivateKey,
		&cfg.Auth.Passphrase,
	))
	if err != nil {
		return nil, errors.Wrap(err, "initializing OCI DNS API client")
	}

	return &OCIProvider{
		client:       client,
		cfg:          cfg,
		domainFilter: domainFilter,
		zoneIDFilter: zoneIDFilter,
		dryRun:       dryRun,
	}, nil
}

func (p *OCIProvider) zones(ctx context.Context) (map[string]dns.ZoneSummary, error) {
	zones := make(map[string]dns.ZoneSummary)

	log.Debugf("Matching zones against domain filters: %v", p.domainFilter.Filters)
	var page *string
	for {
		resp, err := p.client.ListZones(ctx, dns.ListZonesRequest{
			CompartmentId: &p.cfg.CompartmentID,
			ZoneType:      dns.ListZonesZoneTypePrimary,
			Page:          page,
		})
		if err != nil {
			return nil, errors.Wrapf(err, "listing zones in %q", p.cfg.CompartmentID)
		}

		for _, zone := range resp.Items {
			if p.domainFilter.Match(*zone.Name) && p.zoneIDFilter.Match(*zone.Id) {
				zones[*zone.Name] = zone
				log.Debugf("Matched %q (%q)", *zone.Name, *zone.Id)
			} else {
				log.Debugf("Filtered %q (%q)", *zone.Name, *zone.Id)
			}
		}

		if page = resp.OpcNextPage; resp.OpcNextPage == nil {
			break
		}
	}

	if len(zones) == 0 {
		if p.domainFilter.IsConfigured() {
			log.Warnf("No zones in compartment %q match domain filters %v", p.cfg.CompartmentID, p.domainFilter.Filters)
		} else {
			log.Warnf("No zones found in compartment %q", p.cfg.CompartmentID)
		}
	}

	return zones, nil
}

func (p *OCIProvider) newFilteredRecordOperations(endpoints []*endpoint.Endpoint, opType dns.RecordOperationOperationEnum) []dns.RecordOperation {
	ops := []dns.RecordOperation{}
	for _, endpoint := range endpoints {
		if p.domainFilter.Match(endpoint.DNSName) {
			ops = append(ops, newRecordOperation(endpoint, opType))
		}
	}
	return ops
}

// Records returns the list of records in a given hosted zone.
func (p *OCIProvider) Records(ctx context.Context) ([]*endpoint.Endpoint, error) {
	zones, err := p.zones(ctx)
	if err != nil {
		return nil, errors.Wrap(err, "getting zones")
	}

	endpoints := []*endpoint.Endpoint{}
	for _, zone := range zones {
		var page *string
		for {
			resp, err := p.client.GetZoneRecords(ctx, dns.GetZoneRecordsRequest{
				ZoneNameOrId:  zone.Id,
				Page:          page,
				CompartmentId: &p.cfg.CompartmentID,
			})
			if err != nil {
				return nil, errors.Wrapf(err, "getting records for zone %q", *zone.Id)
			}

			for _, record := range resp.Items {
				if !endpoint.SupportedRecordType(*record.Rtype) {
					continue
				}
				endpoints = append(endpoints,
					endpoint.NewEndpointWithTTL(
						*record.Domain,
						*record.Rtype,
						endpoint.TTL(*record.Ttl),
						*record.Rdata,
					),
				)
			}

			if page = resp.OpcNextPage; resp.OpcNextPage == nil {
				break
			}
		}
	}

	return endpoints, nil
}

// ApplyChanges applies a given set of changes to a given zone.
func (p *OCIProvider) ApplyChanges(ctx context.Context, changes *plan.Changes) error {
	log.Debugf("Processing changes: %+v", changes)

	ops := []dns.RecordOperation{}
	ops = append(ops, p.newFilteredRecordOperations(changes.Create, dns.RecordOperationOperationAdd)...)

	ops = append(ops, p.newFilteredRecordOperations(changes.UpdateNew, dns.RecordOperationOperationAdd)...)
	ops = append(ops, p.newFilteredRecordOperations(changes.UpdateOld, dns.RecordOperationOperationRemove)...)

	ops = append(ops, p.newFilteredRecordOperations(changes.Delete, dns.RecordOperationOperationRemove)...)

	if len(ops) == 0 {
		log.Info("All records are already up to date")
		return nil
	}

	zones, err := p.zones(ctx)
	if err != nil {
		return errors.Wrap(err, "fetching zones")
	}

	// Separate into per-zone change sets to be passed to OCI API.
	opsByZone := operationsByZone(zones, ops)
	for zoneID, ops := range opsByZone {
		log.Infof("Change zone: %q", zoneID)
		for _, op := range ops {
			log.Info(op)
		}
	}

	if p.dryRun {
		return nil
	}

	for zoneID, ops := range opsByZone {
		if _, err := p.client.PatchZoneRecords(ctx, dns.PatchZoneRecordsRequest{
			CompartmentId:           &p.cfg.CompartmentID,
			ZoneNameOrId:            &zoneID,
			PatchZoneRecordsDetails: dns.PatchZoneRecordsDetails{Items: ops},
		}); err != nil {
			return err
		}
	}

	return nil
}

// newRecordOperation returns a RecordOperation based on a given endpoint.
func newRecordOperation(ep *endpoint.Endpoint, opType dns.RecordOperationOperationEnum) dns.RecordOperation {
	targets := make([]string, len(ep.Targets))
	copy(targets, []string(ep.Targets))
	if ep.RecordType == endpoint.RecordTypeCNAME {
		targets[0] = provider.EnsureTrailingDot(targets[0])
	}
	rdata := strings.Join(targets, " ")

	ttl := ociRecordTTL
	if ep.RecordTTL.IsConfigured() {
		ttl = int(ep.RecordTTL)
	}

	return dns.RecordOperation{
		Domain:    &ep.DNSName,
		Rdata:     &rdata,
		Ttl:       &ttl,
		Rtype:     &ep.RecordType,
		Operation: opType,
	}
}

// operationsByZone segments a slice of RecordOperations by their zone.
func operationsByZone(zones map[string]dns.ZoneSummary, ops []dns.RecordOperation) map[string][]dns.RecordOperation {
	changes := make(map[string][]dns.RecordOperation)

	zoneNameIDMapper := provider.ZoneIDName{}
	for _, z := range zones {
		zoneNameIDMapper.Add(*z.Id, *z.Name)
		changes[*z.Id] = []dns.RecordOperation{}
	}

	for _, op := range ops {
		if zoneID, _ := zoneNameIDMapper.FindZone(*op.Domain); zoneID != "" {
			changes[zoneID] = append(changes[zoneID], op)
		} else {
			log.Warnf("No matching zone for record operation %s", op)
		}
	}

	// Remove zones that don't have any changes.
	for zone, ops := range changes {
		if len(ops) == 0 {
			delete(changes, zone)
		}
	}

	return changes
}
