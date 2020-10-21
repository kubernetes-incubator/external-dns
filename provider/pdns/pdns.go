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

package pdns

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"math"
	"net"
	"net/http"
	"sort"
	"strings"
	"time"

	pgo "github.com/ffledgling/pdns-go"
	log "github.com/sirupsen/logrus"

	"sigs.k8s.io/external-dns/endpoint"
	"sigs.k8s.io/external-dns/pkg/tlsutils"
	"sigs.k8s.io/external-dns/plan"
	"sigs.k8s.io/external-dns/provider"
)

type pdnsChangeType string

const (
	apiBase = "/api/v1"

	// Unless we use something like pdnsproxy (discontinued upstream), this value will _always_ be localhost
	defaultServerID = "localhost"
	defaultTTL      = 300

	// PdnsDelete and PdnsReplace are effectively an enum for "pgo.RrSet.changetype"
	// TODO: Can we somehow get this from the pgo swagger client library itself?

	// PdnsDelete : PowerDNS changetype used for deleting rrsets
	// ref: https://doc.powerdns.com/authoritative/http-api/zone.html#rrset (see "changetype")
	PdnsDelete pdnsChangeType = "DELETE"
	// PdnsReplace : PowerDNS changetype for creating, updating and patching rrsets
	PdnsReplace pdnsChangeType = "REPLACE"
	// Number of times to retry failed PDNS requests
	retryLimit = 3
	// time in milliseconds
	retryAfterTime = 250 * time.Millisecond
)

// PDNSConfig is comprised of the fields necessary to create a new PDNSProvider
type PDNSConfig struct {
	DomainFilter endpoint.DomainFilter
	DryRun       bool
	Server       string
	APIKey       string
	TLSConfig    TLSConfig
}

// TLSConfig is comprised of the TLS-related fields necessary to create a new PDNSProvider
type TLSConfig struct {
	TLSEnabled            bool
	CAFilePath            string
	ClientCertFilePath    string
	ClientCertKeyFilePath string
}

func (tlsConfig *TLSConfig) setHTTPClient(pdnsClientConfig *pgo.Configuration) error {
	if !tlsConfig.TLSEnabled {
		log.Debug("Skipping TLS for PDNS Provider.")
		return nil
	}

	log.Debug("Configuring TLS for PDNS Provider.")
	if tlsConfig.CAFilePath == "" {
		return errors.New("certificate authority file path must be specified if TLS is enabled")
	}

	tlsClientConfig, err := tlsutils.NewTLSConfig(tlsConfig.ClientCertFilePath, tlsConfig.ClientCertKeyFilePath, tlsConfig.CAFilePath, "", false, tls.VersionTLS12)
	if err != nil {
		return err
	}

	// Timeouts taken from net.http.DefaultTransport
	transporter := &http.Transport{
		Proxy: http.ProxyFromEnvironment,
		DialContext: (&net.Dialer{
			Timeout:   30 * time.Second,
			KeepAlive: 30 * time.Second,
			DualStack: true,
		}).DialContext,
		MaxIdleConns:          100,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
		TLSClientConfig:       tlsClientConfig,
	}
	pdnsClientConfig.HTTPClient = &http.Client{
		Transport: transporter,
	}

	return nil
}

// Function for debug printing
func stringifyHTTPResponseBody(r *http.Response) (body string) {
	if r == nil {
		return ""
	}

	buf := new(bytes.Buffer)
	buf.ReadFrom(r.Body)
	body = buf.String()
	return body
}

// PDNSAPIProvider : Interface used and extended by the PDNSAPIClient struct as
// well as mock APIClients used in testing
type PDNSAPIProvider interface {
	ListZones() ([]pgo.Zone, *http.Response, error)
	PartitionZones(zones []pgo.Zone) ([]pgo.Zone, []pgo.Zone)
	ListZone(zoneID string) (pgo.Zone, *http.Response, error)
	PatchZone(zoneID string, zoneStruct pgo.Zone) (*http.Response, error)
}

// PDNSAPIClient : Struct that encapsulates all the PowerDNS specific implementation details
type PDNSAPIClient struct {
	dryRun       bool
	authCtx      context.Context
	client       *pgo.APIClient
	domainFilter endpoint.DomainFilter
}

// ListZones : Method returns all enabled zones from PowerDNS
// ref: https://doc.powerdns.com/authoritative/http-api/zone.html#get--servers-server_id-zones
func (c *PDNSAPIClient) ListZones() (zones []pgo.Zone, resp *http.Response, err error) {
	for i := 0; i < retryLimit; i++ {
		zones, resp, err = c.client.ZonesApi.ListZones(c.authCtx, defaultServerID)
		if err != nil {
			log.Debugf("Unable to fetch zones %v", err)
			log.Debugf("Retrying ListZones() ... %d", i)
			time.Sleep(retryAfterTime * (1 << uint(i)))
			continue
		}
		return zones, resp, err
	}

	log.Errorf("Unable to fetch zones. %v", err)
	return zones, resp, err
}

// PartitionZones : Method returns a slice of zones that adhere to the domain filter and a slice of ones that does not adhere to the filter
func (c *PDNSAPIClient) PartitionZones(zones []pgo.Zone) (filteredZones []pgo.Zone, residualZones []pgo.Zone) {
	if c.domainFilter.IsConfigured() {
		for _, zone := range zones {
			if c.domainFilter.Match(zone.Name) {
				filteredZones = append(filteredZones, zone)
			} else {
				residualZones = append(residualZones, zone)
			}
		}
	} else {
		filteredZones = zones
	}
	return filteredZones, residualZones
}

// ListZone : Method returns the details of a specific zone from PowerDNS
// ref: https://doc.powerdns.com/authoritative/http-api/zone.html#get--servers-server_id-zones-zone_id
func (c *PDNSAPIClient) ListZone(zoneID string) (zone pgo.Zone, resp *http.Response, err error) {
	for i := 0; i < retryLimit; i++ {
		zone, resp, err = c.client.ZonesApi.ListZone(c.authCtx, defaultServerID, zoneID)
		if err != nil {
			log.Debugf("Unable to fetch zone %v", err)
			log.Debugf("Retrying ListZone() ... %d", i)
			time.Sleep(retryAfterTime * (1 << uint(i)))
			continue
		}
		return zone, resp, err
	}

	log.Errorf("Unable to list zone. %v", err)
	return zone, resp, err
}

// PatchZone : Method used to update the contents of a particular zone from PowerDNS
// ref: https://doc.powerdns.com/authoritative/http-api/zone.html#patch--servers-server_id-zones-zone_id
func (c *PDNSAPIClient) PatchZone(zoneID string, zoneStruct pgo.Zone) (resp *http.Response, err error) {
	for i := 0; i < retryLimit; i++ {
		resp, err = c.client.ZonesApi.PatchZone(c.authCtx, defaultServerID, zoneID, zoneStruct)
		if err != nil {
			log.Debugf("Unable to patch zone %v", err)
			log.Debugf("Retrying PatchZone() ... %d", i)
			time.Sleep(retryAfterTime * (1 << uint(i)))
			continue
		}
		return resp, err
	}

	log.Errorf("Unable to patch zone. %v", err)
	return resp, err
}

// PDNSProvider is an implementation of the Provider interface for PowerDNS
type PDNSProvider struct {
	provider.BaseProvider
	client PDNSAPIProvider
}

// NewPDNSProvider initializes a new PowerDNS based Provider.
func NewPDNSProvider(ctx context.Context, config PDNSConfig) (*PDNSProvider, error) {
	// Do some input validation

	if config.APIKey == "" {
		return nil, errors.New("missing API Key for PDNS. Specify using --pdns-api-key=")
	}

	// We do not support dry running, exit safely instead of surprising the user
	// TODO: Add Dry Run support
	if config.DryRun {
		return nil, errors.New("PDNS Provider does not currently support dry-run")
	}

	if config.Server == "localhost" {
		log.Warnf("PDNS Server is set to localhost, this may not be what you want. Specify using --pdns-server=")
	}

	pdnsClientConfig := pgo.NewConfiguration()
	pdnsClientConfig.BasePath = config.Server + apiBase
	if err := config.TLSConfig.setHTTPClient(pdnsClientConfig); err != nil {
		return nil, err
	}

	provider := &PDNSProvider{
		client: &PDNSAPIClient{
			dryRun:       config.DryRun,
			authCtx:      context.WithValue(ctx, pgo.ContextAPIKey, pgo.APIKey{Key: config.APIKey}),
			client:       pgo.NewAPIClient(pdnsClientConfig),
			domainFilter: config.DomainFilter,
		},
	}
	return provider, nil
}

func (p *PDNSProvider) convertRRSetToEndpoints(rr pgo.RrSet) (endpoints []*endpoint.Endpoint, _ error) {
	endpoints = []*endpoint.Endpoint{}

	for _, record := range rr.Records {
		// If a record is "Disabled", it's not supposed to be "visible"
		if !record.Disabled {
			endpoints = append(endpoints, endpoint.NewEndpointWithTTL(rr.Name, rr.Type_, endpoint.TTL(rr.Ttl), record.Content))
		}
	}
	return endpoints, nil
}

// ConvertEndpointsToZones marshals endpoints into pdns compatible Zone structs
func (p *PDNSProvider) ConvertEndpointsToZones(eps []*endpoint.Endpoint, changetype pdnsChangeType) (zonelist []pgo.Zone, _ error) {
	zonelist = []pgo.Zone{}
	endpoints := make([]*endpoint.Endpoint, len(eps))
	copy(endpoints, eps)

	// Sort the endpoints array so we have deterministic inserts
	sort.SliceStable(endpoints,
		func(i, j int) bool {
			// We only care about sorting endpoints with the same dnsname
			if endpoints[i].DNSName == endpoints[j].DNSName {
				return endpoints[i].RecordType < endpoints[j].RecordType
			}
			return endpoints[i].DNSName < endpoints[j].DNSName
		})

	zones, _, err := p.client.ListZones()
	if err != nil {
		return nil, err
	}
	filteredZones, residualZones := p.client.PartitionZones(zones)

	// Sort the zone by length of the name in descending order, we use this
	// property later to ensure we add a record to the longest matching zone

	sort.SliceStable(filteredZones, func(i, j int) bool { return len(filteredZones[i].Name) > len(filteredZones[j].Name) })

	// NOTE: Complexity of this loop is O(FilteredZones*Endpoints).
	// A possibly faster implementation would be a search of the reversed
	// DNSName in a trie of Zone names, which should be O(Endpoints), but at this point it's not
	// necessary.
	for _, zone := range filteredZones {
		zone.Rrsets = []pgo.RrSet{}
		for i := 0; i < len(endpoints); {
			ep := endpoints[i]
			dnsname := provider.EnsureTrailingDot(ep.DNSName)
			if dnsname == zone.Name || strings.HasSuffix(dnsname, "."+zone.Name) {
				// The assumption here is that there will only ever be one target
				// per (ep.DNSName, ep.RecordType) tuple, which holds true for
				// external-dns v5.0.0-alpha onwards
				records := []pgo.Record{}
				for _, t := range ep.Targets {
					if ep.RecordType == "CNAME" || ep.RecordType == "SRV" {
						t = provider.EnsureTrailingDot(t)
					}

					records = append(records, pgo.Record{Content: t})
				}
				rrset := pgo.RrSet{
					Name:       dnsname,
					Type_:      ep.RecordType,
					Records:    records,
					Changetype: string(changetype),
				}

				// DELETEs explicitly forbid a TTL, therefore only PATCHes need the TTL
				if changetype == PdnsReplace {
					if int64(ep.RecordTTL) > int64(math.MaxInt32) {
						return nil, errors.New("value of record TTL overflows, limited to int32")
					}
					if ep.RecordTTL == 0 {
						// No TTL was specified for the record, we use the default
						rrset.Ttl = int32(defaultTTL)
					} else {
						rrset.Ttl = int32(ep.RecordTTL)
					}
				}

				zone.Rrsets = append(zone.Rrsets, rrset)

				// "pop" endpoint if it's matched
				endpoints = append(endpoints[0:i], endpoints[i+1:]...)
			} else {
				// If we didn't pop anything, we move to the next item in the list
				i++
			}
		}
		if len(zone.Rrsets) > 0 {
			zonelist = append(zonelist, zone)
		}
	}

	// residualZones is unsorted by name length like its counterpart
	// since we only care to remove endpoints that do not match domain filter
	for _, zone := range residualZones {
		for i := 0; i < len(endpoints); {
			ep := endpoints[i]
			dnsname := provider.EnsureTrailingDot(ep.DNSName)
			if dnsname == zone.Name || strings.HasSuffix(dnsname, "."+zone.Name) {
				// "pop" endpoint if it's matched to a residual zone... essentially a no-op
				log.Debugf("Ignoring Endpoint because it was matched to a zone that was not specified within Domain Filter(s): %s", dnsname)
				endpoints = append(endpoints[0:i], endpoints[i+1:]...)
			} else {
				i++
			}
		}
	}
	// If we still have some endpoints left, it means we couldn't find a matching zone (filtered or residual) for them
	// We warn instead of hard fail here because we don't want a misconfig to cause everything to go down
	if len(endpoints) > 0 {
		log.Warnf("No matching zones were found for the following endpoints: %+v", endpoints)
	}

	log.Debugf("Zone List generated from Endpoints: %+v", zonelist)

	return zonelist, nil
}

// mutateRecords takes a list of endpoints and creates, replaces or deletes them based on the changetype
func (p *PDNSProvider) mutateRecords(endpoints []*endpoint.Endpoint, changetype pdnsChangeType) error {
	zonelist, err := p.ConvertEndpointsToZones(endpoints, changetype)
	if err != nil {
		return err
	}
	for _, zone := range zonelist {
		jso, err := json.Marshal(zone)
		if err != nil {
			log.Errorf("JSON Marshal for zone struct failed!")
		} else {
			log.Debugf("Struct for PatchZone:\n%s", string(jso))
		}
		resp, err := p.client.PatchZone(zone.Id, zone)
		if err != nil {
			log.Debugf("PDNS API response: %s", stringifyHTTPResponseBody(resp))
			return err
		}
	}
	return nil
}

// Records returns all DNS records controlled by the configured PDNS server (for all zones)
func (p *PDNSProvider) Records(ctx context.Context) (endpoints []*endpoint.Endpoint, _ error) {
	zones, _, err := p.client.ListZones()
	if err != nil {
		return nil, err
	}
	filteredZones, _ := p.client.PartitionZones(zones)

	for _, zone := range filteredZones {
		z, _, err := p.client.ListZone(zone.Id)
		if err != nil {
			log.Warnf("Unable to fetch Records")
			return nil, err
		}

		for _, rr := range z.Rrsets {
			e, err := p.convertRRSetToEndpoints(rr)
			if err != nil {
				return nil, err
			}
			endpoints = append(endpoints, e...)
		}
	}

	log.Debugf("Records fetched:\n%+v", endpoints)
	return endpoints, nil
}

// ApplyChanges takes a list of changes (endpoints) and updates the PDNS server
// by sending the correct HTTP PATCH requests to a matching zone
func (p *PDNSProvider) ApplyChanges(ctx context.Context, changes *plan.Changes) error {
	startTime := time.Now()

	// Create
	for _, change := range changes.Create {
		log.Debugf("CREATE: %+v", change)
	}
	// We only attempt to mutate records if there are any to mutate.  A
	// call to mutate records with an empty list of endpoints is still a
	// valid call and a no-op, but we might as well not make the call to
	// prevent unnecessary logging
	if len(changes.Create) > 0 {
		// "Replacing" non-existent records creates them
		err := p.mutateRecords(changes.Create, PdnsReplace)
		if err != nil {
			return err
		}
	}

	// Update
	for _, change := range changes.UpdateOld {
		// Since PDNS "Patches", we don't need to specify the "old"
		// record. The Update New change type will automatically take
		// care of replacing the old RRSet with the new one We simply
		// leave this logging here for information
		log.Debugf("UPDATE-OLD (ignored): %+v", change)
	}

	for _, change := range changes.UpdateNew {
		log.Debugf("UPDATE-NEW: %+v", change)
	}
	if len(changes.UpdateNew) > 0 {
		err := p.mutateRecords(changes.UpdateNew, PdnsReplace)
		if err != nil {
			return err
		}
	}

	// Delete
	for _, change := range changes.Delete {
		log.Debugf("DELETE: %+v", change)
	}
	if len(changes.Delete) > 0 {
		err := p.mutateRecords(changes.Delete, PdnsDelete)
		if err != nil {
			return err
		}
	}
	log.Debugf("Changes pushed out to PowerDNS in %s\n", time.Since(startTime))
	return nil
}
