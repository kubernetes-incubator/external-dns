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

package ns1

import (
	"context"
	"crypto/tls"
	"fmt"
	"net/http"
	"os"
	"strings"

	log "github.com/sirupsen/logrus"
	api "gopkg.in/ns1/ns1-go.v2/rest"
	"gopkg.in/ns1/ns1-go.v2/rest/model/dns"

	"sigs.k8s.io/external-dns/endpoint"
	"sigs.k8s.io/external-dns/plan"
	"sigs.k8s.io/external-dns/provider"
)

const (
	// ns1Create is a ChangeAction enum value
	ns1Create = "CREATE"
	// ns1Delete is a ChangeAction enum value
	ns1Delete = "DELETE"
	// ns1Update is a ChangeAction enum value
	ns1Update = "UPDATE"
	// ns1DefaultTTL is the default ttl for ttls that are not set
	ns1DefaultTTL = 10
)

// NS1DomainClient is a subset of the NS1 API the the provider uses, to ease testing
type NS1DomainClient interface {
	CreateRecord(r *dns.Record) (*http.Response, error)
	DeleteRecord(zone string, domain string, t string) (*http.Response, error)
	UpdateRecord(r *dns.Record) (*http.Response, error)
	GetZone(zone string) (*dns.Zone, *http.Response, error)
	ListZones() ([]*dns.Zone, *http.Response, error)
}

// NS1DomainService wraps the API and fulfills the NS1DomainClient interface
type NS1DomainService struct {
	service *api.Client
}

// CreateRecord wraps the Create method of the API's Record service
func (n NS1DomainService) CreateRecord(r *dns.Record) (*http.Response, error) {
	return n.service.Records.Create(r)
}

// DeleteRecord wraps the Delete method of the API's Record service
func (n NS1DomainService) DeleteRecord(zone string, domain string, t string) (*http.Response, error) {
	return n.service.Records.Delete(zone, domain, t)
}

// UpdateRecord wraps the Update method of the API's Record service
func (n NS1DomainService) UpdateRecord(r *dns.Record) (*http.Response, error) {
	return n.service.Records.Update(r)
}

// GetZone wraps the Get method of the API's Zones service
func (n NS1DomainService) GetZone(zone string) (*dns.Zone, *http.Response, error) {
	return n.service.Zones.Get(zone)
}

// ListZones wraps the List method of the API's Zones service
func (n NS1DomainService) ListZones() ([]*dns.Zone, *http.Response, error) {
	return n.service.Zones.List()
}

// NS1Config passes cli args to the NS1Provider
type NS1Config struct {
	DomainFilter  endpoint.DomainFilter
	ZoneIDFilter  provider.ZoneIDFilter
	NS1Endpoint   string
	NS1IgnoreSSL  bool
	DryRun        bool
	MinTTLSeconds int
}

// NS1Provider is the NS1 provider
type NS1Provider struct {
	provider.BaseProvider
	client        NS1DomainClient
	domainFilter  endpoint.DomainFilter
	zoneIDFilter  provider.ZoneIDFilter
	dryRun        bool
	minTTLSeconds int
}

// NewNS1Provider creates a new NS1 Provider
func NewNS1Provider(config NS1Config) (*NS1Provider, error) {
	return newNS1ProviderWithHTTPClient(config, http.DefaultClient)
}

func newNS1ProviderWithHTTPClient(config NS1Config, client *http.Client) (*NS1Provider, error) {
	token, ok := os.LookupEnv("NS1_APIKEY")
	if !ok {
		return nil, fmt.Errorf("NS1_APIKEY environment variable is not set")
	}
	clientArgs := []func(*api.Client){api.SetAPIKey(token)}
	if config.NS1Endpoint != "" {
		log.Infof("ns1-endpoint flag is set, targeting endpoint at %s", config.NS1Endpoint)
		clientArgs = append(clientArgs, api.SetEndpoint(config.NS1Endpoint))
	}

	if config.NS1IgnoreSSL {
		log.Info("ns1-ignoressl flag is True, skipping SSL verification")
		defaultTransport := http.DefaultTransport.(*http.Transport)
		tr := &http.Transport{
			Proxy:                 defaultTransport.Proxy,
			DialContext:           defaultTransport.DialContext,
			MaxIdleConns:          defaultTransport.MaxIdleConns,
			IdleConnTimeout:       defaultTransport.IdleConnTimeout,
			ExpectContinueTimeout: defaultTransport.ExpectContinueTimeout,
			TLSHandshakeTimeout:   defaultTransport.TLSHandshakeTimeout,
			TLSClientConfig:       &tls.Config{InsecureSkipVerify: true},
		}
		client.Transport = tr
	}

	apiClient := api.NewClient(client, clientArgs...)

	provider := &NS1Provider{
		client:        NS1DomainService{apiClient},
		domainFilter:  config.DomainFilter,
		zoneIDFilter:  config.ZoneIDFilter,
		minTTLSeconds: config.MinTTLSeconds,
	}
	return provider, nil
}

// Records returns the endpoints this provider knows about
func (p *NS1Provider) Records(ctx context.Context) ([]*endpoint.Endpoint, error) {
	zones, err := p.zonesFiltered()
	if err != nil {
		return nil, err
	}

	var endpoints []*endpoint.Endpoint

	for _, zone := range zones {
		// TODO handle Header Codes
		zoneData, _, err := p.client.GetZone(zone.String())
		if err != nil {
			return nil, err
		}

		for _, record := range zoneData.Records {
			if endpoint.SupportedRecordType(record.Type) {
				endpoints = append(endpoints, endpoint.NewEndpointWithTTL(
					record.Domain,
					record.Type,
					endpoint.TTL(record.TTL),
					record.ShortAns...,
				),
				)
			}
		}
	}

	return endpoints, nil
}

// ns1BuildRecord returns a dns.Record for a change set
func (p *NS1Provider) ns1BuildRecord(zoneName string, change *ns1Change) *dns.Record {
	record := dns.NewRecord(zoneName, change.Endpoint.DNSName, change.Endpoint.RecordType)
	for _, v := range change.Endpoint.Targets {
		record.AddAnswer(dns.NewAnswer(strings.Split(v, " ")))
	}
	// set default ttl, but respect minTTLSeconds
	var ttl = ns1DefaultTTL
	if p.minTTLSeconds > ttl {
		ttl = p.minTTLSeconds
	}
	if change.Endpoint.RecordTTL.IsConfigured() {
		ttl = int(change.Endpoint.RecordTTL)
	}
	record.TTL = ttl

	return record
}

// ns1SubmitChanges takes an array of changes and sends them to NS1
func (p *NS1Provider) ns1SubmitChanges(changes []*ns1Change) error {
	// return early if there is nothing to change
	if len(changes) == 0 {
		return nil
	}

	zones, err := p.zonesFiltered()
	if err != nil {
		return err
	}

	// separate into per-zone change sets to be passed to the API.
	changesByZone := ns1ChangesByZone(zones, changes)
	for zoneName, changes := range changesByZone {
		for _, change := range changes {
			record := p.ns1BuildRecord(zoneName, change)
			logFields := log.Fields{
				"record": record.Domain,
				"type":   record.Type,
				"ttl":    record.TTL,
				"action": change.Action,
				"zone":   zoneName,
			}

			log.WithFields(logFields).Info("Changing record.")

			if p.dryRun {
				continue
			}

			switch change.Action {
			case ns1Create:
				_, err := p.client.CreateRecord(record)
				if err != nil {
					return err
				}
			case ns1Delete:
				_, err := p.client.DeleteRecord(zoneName, record.Domain, record.Type)
				if err != nil {
					return err
				}
			case ns1Update:
				_, err := p.client.UpdateRecord(record)
				if err != nil {
					return err
				}
			}
		}
	}
	return nil
}

// Zones returns the list of hosted zones.
func (p *NS1Provider) zonesFiltered() ([]*dns.Zone, error) {
	// TODO handle Header Codes
	zones, _, err := p.client.ListZones()
	if err != nil {
		return nil, err
	}

	toReturn := []*dns.Zone{}

	for _, z := range zones {
		if p.domainFilter.Match(z.Zone) && p.zoneIDFilter.Match(z.ID) {
			toReturn = append(toReturn, z)
			log.Debugf("Matched %s", z.Zone)
		} else {
			log.Debugf("Filtered %s", z.Zone)
		}
	}

	return toReturn, nil
}

// ns1Change differentiates between ChangeActions
type ns1Change struct {
	Action   string
	Endpoint *endpoint.Endpoint
}

// ApplyChanges applies a given set of changes in a given zone.
func (p *NS1Provider) ApplyChanges(ctx context.Context, changes *plan.Changes) error {
	combinedChanges := make([]*ns1Change, 0, len(changes.Create)+len(changes.UpdateNew)+len(changes.Delete))

	combinedChanges = append(combinedChanges, newNS1Changes(ns1Create, changes.Create)...)
	combinedChanges = append(combinedChanges, newNS1Changes(ns1Update, changes.UpdateNew)...)
	combinedChanges = append(combinedChanges, newNS1Changes(ns1Delete, changes.Delete)...)

	return p.ns1SubmitChanges(combinedChanges)
}

// newNS1Changes returns a collection of Changes based on the given records and action.
func newNS1Changes(action string, endpoints []*endpoint.Endpoint) []*ns1Change {
	changes := make([]*ns1Change, 0, len(endpoints))

	for _, endpoint := range endpoints {
		changes = append(changes, &ns1Change{
			Action:   action,
			Endpoint: endpoint,
		},
		)
	}

	return changes
}

// ns1ChangesByZone separates a multi-zone change into a single change per zone.
func ns1ChangesByZone(zones []*dns.Zone, changeSets []*ns1Change) map[string][]*ns1Change {
	changes := make(map[string][]*ns1Change)
	zoneNameIDMapper := provider.ZoneIDName{}
	for _, z := range zones {
		zoneNameIDMapper.Add(z.Zone, z.Zone)
		changes[z.Zone] = []*ns1Change{}
	}

	for _, c := range changeSets {
		zone, _ := zoneNameIDMapper.FindZone(c.Endpoint.DNSName)
		if zone == "" {
			log.Debugf("Skipping record %s because no hosted zone matching record DNS Name was detected", c.Endpoint.DNSName)
			continue
		}
		changes[zone] = append(changes[zone], c)
	}

	return changes
}
