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

package akamai

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"

	dns "github.com/akamai/AkamaiOPEN-edgegrid-golang/configdns-v2"
	"github.com/akamai/AkamaiOPEN-edgegrid-golang/edgegrid"
	log "github.com/sirupsen/logrus"

	"sigs.k8s.io/external-dns/endpoint"
	"sigs.k8s.io/external-dns/plan"
	"sigs.k8s.io/external-dns/provider"
)

const (
	// Default Record TTL
	edgeDNSRecordTTL = 600
	maxUint          = ^uint(0)
	maxInt           = int(maxUint >> 1)
)

// edgeDNSClient is a proxy interface of the Akamai edgegrid configdns-v2 package that can be stubbed for testing.
type AkamaiDNSService interface {
	ListZones(queryArgs dns.ZoneListQueryArgs) (*dns.ZoneListResponse, error)
	GetRecordsets(zone string, queryArgs dns.RecordsetQueryArgs) (*dns.RecordSetResponse, error)
	GetRecord(zone string, name string, recordtype string) (*dns.RecordBody, error)
	DeleteRecord(record *dns.RecordBody, zone string, recLock bool) error
	UpdateRecord(record *dns.RecordBody, zone string, recLock bool) error
	CreateRecordsets(recordsets *dns.Recordsets, zone string, recLock bool) error
}

type AkamaiConfig struct {
	DomainFilter          endpoint.DomainFilter
	ZoneIDFilter          provider.ZoneIDFilter
	ServiceConsumerDomain string
	ClientToken           string
	ClientSecret          string
	AccessToken           string
	EdgercPath            string
	EdgercSection         string
	MaxBody               int
	AccountKey            string
	DryRun                bool
}

// AkamaiProvider implements the DNS provider for Akamai.
type AkamaiProvider struct {
	provider.BaseProvider
	// Edgedns zones to filter on
	domainFilter endpoint.DomainFilter
	// Contract Ids to filter on
	zoneIDFilter provider.ZoneIDFilter
	// Edgegrid library configuration
	config *edgegrid.Config
	dryRun bool
	// Defines client. Allows for mocking.
	client AkamaiDNSService
}

type akamaiZones struct {
	Zones []akamaiZone `json:"zones"`
}

type akamaiZone struct {
	ContractID string `json:"contractId"`
	Zone       string `json:"zone"`
}

// NewAkamaiProvider initializes a new Akamai DNS based Provider.
func NewAkamaiProvider(akamaiConfig AkamaiConfig, akaService AkamaiDNSService) (provider.Provider, error) {
	var edgeGridConfig edgegrid.Config

	/*
		log.Debugf("Host: %s", akamaiConfig.ServiceConsumerDomain)
		log.Debugf("ClientToken: %s", akamaiConfig.ClientToken)
		log.Debugf("ClientSecret: %s", akamaiConfig.ClientSecret)
		log.Debugf("AccessToken: %s", akamaiConfig.AccessToken)
		log.Debugf("EdgePath: %s", akamaiConfig.EdgercPath)
		log.Debugf("EdgeSection: %s", akamaiConfig.EdgercSection)
	*/
	// environment overrides edgerc file but config needs to be complete
	if akamaiConfig.ServiceConsumerDomain == "" || akamaiConfig.ClientToken == "" || akamaiConfig.ClientSecret == "" || akamaiConfig.AccessToken == "" {
		// Kubernetes config incomplete or non existent. Can't mix and match.
		// Look for Akamai environment or .edgerd creds
		var err error
		edgeGridConfig, err = edgegrid.Init(akamaiConfig.EdgercPath, akamaiConfig.EdgercSection) // use default .edgerc location and section
		if err != nil {
			log.Errorf("Edgegrid Init Failed")
			return &AkamaiProvider{}, err // return empty provider for backward compatibility
		}
		edgeGridConfig.HeaderToSign = append(edgeGridConfig.HeaderToSign, "X-External-DNS")
	} else {
		// Use external-dns config
		edgeGridConfig = edgegrid.Config{
			Host:         akamaiConfig.ServiceConsumerDomain,
			ClientToken:  akamaiConfig.ClientToken,
			ClientSecret: akamaiConfig.ClientSecret,
			AccessToken:  akamaiConfig.AccessToken,
			MaxBody:      131072, // same default val as used by Edgegrid
			HeaderToSign: []string{
				"X-External-DNS",
			},
			Debug: false,
		}
		// Check for edgegrid overrides
		if envval, ok := os.LookupEnv("AKAMAI_MAX_BODY"); ok {
			if i, err := strconv.Atoi(envval); err == nil {
				edgeGridConfig.MaxBody = i
				log.Debugf("Edgegrid maxbody set to %s", envval)
			}
		}
		if envval, ok := os.LookupEnv("AKAMAI_ACCOUNT_KEY"); ok {
			edgeGridConfig.AccountKey = envval
			log.Debugf("Edgegrid applying account key %s", envval)
		}
		if envval, ok := os.LookupEnv("AKAMAI_DEBUG"); ok {
			if dbgval, err := strconv.ParseBool(envval); err == nil {
				edgeGridConfig.Debug = dbgval
				log.Debugf("Edgegrid debug set to %s", envval)
			}
		}
	}

	provider := &AkamaiProvider{
		domainFilter: akamaiConfig.DomainFilter,
		zoneIDFilter: akamaiConfig.ZoneIDFilter,
		config:       &edgeGridConfig,
		dryRun:       akamaiConfig.DryRun,
	}
	if akaService != nil {
		log.Debugf("Using STUB")
		provider.client = akaService
	} else {
		provider.client = provider
	}

	// Init library for direct endpoint calls
	dns.Init(edgeGridConfig)

	return provider, nil
}

func (p AkamaiProvider) ListZones(queryArgs dns.ZoneListQueryArgs) (*dns.ZoneListResponse, error) {
	return dns.ListZones(queryArgs)
}

func (p AkamaiProvider) GetRecordsets(zone string, queryArgs dns.RecordsetQueryArgs) (*dns.RecordSetResponse, error) {
	return dns.GetRecordsets(zone, queryArgs)
}

func (p AkamaiProvider) CreateRecordsets(recordsets *dns.Recordsets, zone string, reclock bool) error {
	return recordsets.Save(zone, reclock)
}

func (p AkamaiProvider) GetRecord(zone string, name string, recordtype string) (*dns.RecordBody, error) {
	return dns.GetRecord(zone, name, recordtype)
}

func (p AkamaiProvider) DeleteRecord(record *dns.RecordBody, zone string, recLock bool) error {
	return record.Delete(zone, recLock)
}

func (p AkamaiProvider) UpdateRecord(record *dns.RecordBody, zone string, recLock bool) error {
	return record.Update(zone, recLock)
}

// Fetch zones using Edgegrid DNS v2 API
func (p AkamaiProvider) fetchZones() (akamaiZones, error) {
	filteredZones := akamaiZones{Zones: make([]akamaiZone, 0)}
	queryArgs := dns.ZoneListQueryArgs{Types: "primary", ShowAll: true}
	// filter based on contractIds
	if len(p.zoneIDFilter.ZoneIDs) > 0 {
		queryArgs.ContractIds = strings.Join(p.zoneIDFilter.ZoneIDs, ",")
	}
	resp, err := p.client.ListZones(queryArgs) // retrieve all primary zones filtered by contract ids

	if err != nil {
		log.Errorf("Failed to fetch zones from Akamai")
		return filteredZones, err
	}

	for _, zone := range resp.Zones {
		if p.domainFilter.Match(zone.Zone) || !p.domainFilter.IsConfigured() {
			filteredZones.Zones = append(filteredZones.Zones, akamaiZone{ContractID: zone.ContractId, Zone: zone.Zone})
			log.Debugf("Fetched zone: '%s' (ZoneID: %s)", zone.Zone, zone.ContractId)
		}
	}
	lenFilteredZones := len(filteredZones.Zones)
	if lenFilteredZones == 0 {
		log.Warnf("No zones could be fetched")
	} else {
		log.Debugf("Fetched '%d' zones from Akamai", lenFilteredZones)
	}

	return filteredZones, nil
}

//Records returns the list of records in a given zone.
func (p AkamaiProvider) Records(context.Context) (endpoints []*endpoint.Endpoint, err error) {
	zones, err := p.fetchZones() // returns a filtered set of zones
	if err != nil {
		log.Warnf("Failed to identify target zones! Error: %s", err.Error())
		return endpoints, err
	}
	for _, zone := range zones.Zones {
		recordsets, err := p.client.GetRecordsets(zone.Zone, dns.RecordsetQueryArgs{})
		if err != nil {
			log.Errorf("Recordsets retrieval for zone: '%s' failed! %s", zone.Zone, err.Error())
			continue
		}
		if len(recordsets.Recordsets) == 0 {
			log.Warnf("Zone %s contains no recordsets", zone.Zone)
		}

		for _, recordset := range recordsets.Recordsets {
			if !endpoint.SupportedRecordType(recordset.Type) {
				log.Debugf("Skipping endpoint DNSName: '%s' RecordType: '%s'. Record type not supported.", recordset.Name, recordset.Type)
				continue
			}
			if !p.domainFilter.Match(recordset.Name) {
				log.Debugf("Skipping endpoint. Record name %s doesn't match containing zone %s.", recordset.Name, zone)
				continue
			}
			var temp interface{} = int64(recordset.TTL)
			var ttl endpoint.TTL = endpoint.TTL(temp.(int64))
			endpoints = append(endpoints, endpoint.NewEndpointWithTTL(recordset.Name,
				recordset.Type,
				ttl,
				trimTxtRdata(recordset.Rdata, recordset.Type)...))
			log.Debugf("Fetched endpoint DNSName: '%s' RecordType: '%s' Rdata: '%s')", recordset.Name, recordset.Type, recordset.Rdata)
		}
	}
	lenEndpoints := len(endpoints)
	if lenEndpoints == 0 {
		log.Warnf("No endpoints could be fetched")
	} else {
		log.Debugf("Fetched '%d' endpoints from Akamai", lenEndpoints)
		log.Debugf("Endpoints [%v]", endpoints)
	}

	return endpoints, nil
}

// ApplyChanges applies a given set of changes in a given zone.
func (p AkamaiProvider) ApplyChanges(ctx context.Context, changes *plan.Changes) error {
	zoneNameIDMapper := provider.ZoneIDName{}
	zones, err := p.fetchZones()
	if err != nil {
		log.Errorf("Failed to fetch zones from Akamai")
		return err
	}

	for _, z := range zones.Zones {
		zoneNameIDMapper[z.Zone] = z.Zone
	}
	log.Debugf("Processing zones: [%v]", zoneNameIDMapper)

	// Create recordsets
	log.Debugf("Create Changes requested [%v]", changes.Create)
	if err := p.createRecordsets(zoneNameIDMapper, changes.Create); err != nil {
		return err
	}
	// Delete recordsets
	log.Debugf("Delete Changes requested [%v]", changes.Delete)
	if err := p.deleteRecordsets(zoneNameIDMapper, changes.Delete); err != nil {
		return err
	}
	// Update recordsets
	log.Debugf("Update Changes requested [%v]", changes.UpdateNew)
	if err := p.updateNewRecordsets(zoneNameIDMapper, changes.UpdateNew); err != nil {
		return err
	}
	// Check that all old endpoints were accounted for
	revRecs := changes.Delete
	revRecs = append(revRecs, changes.UpdateNew...)
	for _, rec := range changes.UpdateOld {
		found := false
		for _, r := range revRecs {
			if rec.DNSName == r.DNSName {
				found = true
				break
			}
		}
		if !found {
			log.Warnf("UpdateOld endpoint '%s' is not accounted for in UpdateNew|Delete endpoint list", rec.DNSName)
		}
	}

	return nil
}

// Create DNS Recordset
func newAkamaiRecordset(dnsName, recordType string, ttl int, targets []string) dns.Recordset {
	return dns.Recordset{
		Name:  strings.TrimSuffix(dnsName, "."),
		Rdata: targets,
		Type:  recordType,
		TTL:   ttl,
	}
}

// cleanTargets preps recordset rdata if necessary for EdgeDNS
func cleanTargets(rtype string, targets ...string) []string {
	log.Debugf("Targets to clean: [%v]", targets)
	if rtype == "CNAME" || rtype == "SRV" {
		for idx, target := range targets {
			targets[idx] = strings.TrimSuffix(target, ".")
		}
	} else if rtype == "TXT" {
		for idx, target := range targets {
			log.Debugf("TXT data to clean: [%s]", target)
			// need to embed text data in quotes. Make sure not piling on
			target = strings.Trim(target, "\"")
			// bug in DNS API with embedded quotes.
			if strings.Contains(target, "owner") && strings.Contains(target, "\"") {
				target = strings.ReplaceAll(target, "\"", "`")
			}
			targets[idx] = "\"" + target + "\""
		}
	}
	log.Debugf("Clean targets: [%v]", targets)

	return targets
}

// trimTxtRdata removes surrounding quotes for received TXT rdata
func trimTxtRdata(rdata []string, rtype string) []string {
	if rtype == "TXT" {
		for idx, d := range rdata {
			if strings.Contains(d, "`") {
				rdata[idx] = strings.ReplaceAll(d, "`", "\"")
			}
		}
	}
	log.Debugf("Trimmed data: [%v]", rdata)

	return rdata
}

func ttlAsInt(src endpoint.TTL) int {
	var temp interface{} = int64(src)
	var temp64 = temp.(int64)
	var ttl int = edgeDNSRecordTTL
	if temp64 > 0 && temp64 <= int64(maxInt) {
		ttl = int(temp64)
	}

	return ttl
}

// Create Endpoint Recordsets
func (p AkamaiProvider) createRecordsets(zoneNameIDMapper provider.ZoneIDName, endpoints []*endpoint.Endpoint) error {
	if len(endpoints) == 0 {
		log.Info("No endpoints to create")
		return nil
	}

	endpointsByZone := edgeChangesByZone(zoneNameIDMapper, endpoints)

	// create all recordsets by zone
	for zone, endpoints := range endpointsByZone {
		recordsets := &dns.Recordsets{Recordsets: make([]dns.Recordset, 0)}
		for _, endpoint := range endpoints {
			newrec := newAkamaiRecordset(endpoint.DNSName,
				endpoint.RecordType,
				ttlAsInt(endpoint.RecordTTL),
				cleanTargets(endpoint.RecordType, endpoint.Targets...))
			logfields := log.Fields{
				"record": newrec.Name,
				"type":   newrec.Type,
				"ttl":    newrec.TTL,
				"target": fmt.Sprintf("%v", newrec.Rdata),
				"zone":   zone,
			}
			log.WithFields(logfields).Info("Creating recordsets")
			recordsets.Recordsets = append(recordsets.Recordsets, newrec)
		}

		if p.dryRun {
			continue
		}
		// Create recordsets all at once
		err := p.client.CreateRecordsets(recordsets, zone, true)
		if err != nil {
			log.Errorf("Failed to create endpoints for DNS zone %s. Error: %s", zone, err.Error())
			return err
		}
	}

	return nil
}

func (p AkamaiProvider) deleteRecordsets(zoneNameIDMapper provider.ZoneIDName, endpoints []*endpoint.Endpoint) error {
	for _, endpoint := range endpoints {
		zoneName, _ := zoneNameIDMapper.FindZone(endpoint.DNSName)
		if zoneName == "" {
			log.Debugf("Skipping Akamai Edge DNS endpoint deletion: '%s' type: '%s', it does not match against Domain filters", endpoint.DNSName, endpoint.RecordType)
			continue
		}
		log.Infof("Akamai Edge DNS recordset deletion- Zone: '%s', DNSName: '%s', RecordType: '%s', Targets: '%+v'", zoneName, endpoint.DNSName, endpoint.RecordType, endpoint.Targets)

		if p.dryRun {
			continue
		}

		recName := strings.TrimSuffix(endpoint.DNSName, ".")
		rec, err := p.client.GetRecord(zoneName, recName, endpoint.RecordType)
		if err != nil {
			if _, ok := err.(*dns.RecordError); !ok {
				return fmt.Errorf("endpoint deletion. record validation failed. error: %s", err.Error())
			}
			log.Infof("Endpoint deletion. Record doesn't exist. Name: %s, Type: %s", recName, endpoint.RecordType)
			continue
		}
		if err := p.client.DeleteRecord(rec, zoneName, true); err != nil {
			log.Errorf("edge dns recordset deletion failed. error: %s", err.Error())
			return err
		}
	}

	return nil
}

// Update endpoint recordsets
func (p AkamaiProvider) updateNewRecordsets(zoneNameIDMapper provider.ZoneIDName, endpoints []*endpoint.Endpoint) error {
	for _, endpoint := range endpoints {
		zoneName, _ := zoneNameIDMapper.FindZone(endpoint.DNSName)
		if zoneName == "" {
			log.Debugf("Skipping Akamai Edge DNS endpoint update: '%s' type: '%s', it does not match against Domain filters", endpoint.DNSName, endpoint.RecordType)
			continue
		}
		log.Infof("Akamai Edge DNS recordset update - Zone: '%s', DNSName: '%s', RecordType: '%s', Targets: '%+v'", zoneName, endpoint.DNSName, endpoint.RecordType, endpoint.Targets)

		if p.dryRun {
			continue
		}

		recName := strings.TrimSuffix(endpoint.DNSName, ".")
		rec, err := p.client.GetRecord(zoneName, recName, endpoint.RecordType)
		if err != nil {
			log.Errorf("Endpoint update. Record validation failed. Error: %s", err.Error())
			return err
		}
		rec.TTL = ttlAsInt(endpoint.RecordTTL)
		rec.Target = cleanTargets(endpoint.RecordType, endpoint.Targets...)
		if err := p.client.UpdateRecord(rec, zoneName, true); err != nil {
			log.Errorf("Akamai Edge DNS recordset update failed. Error: %s", err.Error())
			return err
		}
	}

	return nil
}

// edgeChangesByZone separates a multi-zone change into a single change per zone.
func edgeChangesByZone(zoneMap provider.ZoneIDName, endpoints []*endpoint.Endpoint) map[string][]*endpoint.Endpoint {
	createsByZone := make(map[string][]*endpoint.Endpoint, len(zoneMap))
	for _, z := range zoneMap {
		createsByZone[z] = make([]*endpoint.Endpoint, 0)
	}
	for _, ep := range endpoints {
		zone, _ := zoneMap.FindZone(ep.DNSName)
		if zone != "" {
			createsByZone[zone] = append(createsByZone[zone], ep)
			continue
		}
		log.Debugf("Skipping Akamai Edge DNS creation of endpoint: '%s' type: '%s', it does not match against Domain filters", ep.DNSName, ep.RecordType)
	}

	return createsByZone
}
