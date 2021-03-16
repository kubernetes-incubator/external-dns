/*
Copyright 2021 The Kubernetes Authors.

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

package inwx

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/nrdcg/goinwx"
	log "github.com/sirupsen/logrus"

	"sigs.k8s.io/external-dns/endpoint"
	"sigs.k8s.io/external-dns/plan"
	"sigs.k8s.io/external-dns/provider"
)

const (
	providerTypeRoID   = "roId"
	providerTypeDomain = "Domain"
)

// InwxProvider - dns provider targeting DNS service on inwx.de
type InwxProvider struct {
	provider.BaseProvider

	Domain         endpoint.DomainFilter
	Client         InwxClient
	DryRun         bool
	ReloadInterval time.Duration

	domains           []*goinwx.NameserverDomain
	cachedRecords     []*endpoint.Endpoint
	cachedRecordsTime time.Time
}

// InwxConfig - INWX provider configuration
type InwxConfig struct {
	Username       string
	Password       string
	Sandbox        bool
	DryRun         bool
	DomainFilter   endpoint.DomainFilter
	ReloadInterval time.Duration
}

// InwxClient - provides abstraction layer to goinwx library to allow better testing/mocking
type InwxClient interface {
	Login() (*goinwx.LoginResponse, error)
	Logout() error

	NameserverInfo(request *goinwx.NameserverInfoRequest) (*goinwx.NamserverInfoResponse, error)
	ListNameserverEntries(domain string) (*goinwx.NamserverListResponse, error)
	CreateNameserverRecord(request *goinwx.NameserverRecordRequest) (int, error)
	UpdateNameserverRecord(recID int, request *goinwx.NameserverRecordRequest) error
	DeleteNameserverRecord(recID int) error
}

// InwxClientProxy - implementation that just proxies the requests to the original goinwx Client library
type InwxClientProxy struct {
	client *goinwx.Client
}

func (c *InwxClientProxy) Login() (*goinwx.LoginResponse, error) {
	return c.client.Account.Login()
}

func (c *InwxClientProxy) Logout() error {
	return c.client.Account.Logout()
}

func (c *InwxClientProxy) ListNameserverEntries(domain string) (*goinwx.NamserverListResponse, error) {
	return c.client.Nameservers.List(domain)
}

func (c *InwxClientProxy) CreateNameserverRecord(request *goinwx.NameserverRecordRequest) (int, error) {
	return c.client.Nameservers.CreateRecord(request)
}

func (c *InwxClientProxy) UpdateNameserverRecord(recID int, request *goinwx.NameserverRecordRequest) error {
	return c.client.Nameservers.UpdateRecord(recID, request)
}

func (c *InwxClientProxy) DeleteNameserverRecord(recID int) error {
	return c.client.Nameservers.DeleteRecord(recID)
}

func (c *InwxClientProxy) NameserverInfo(request *goinwx.NameserverInfoRequest) (*goinwx.NamserverInfoResponse, error) {
	return c.client.Nameservers.Info(request)
}

// NewInwxProvider returns InwxProvider DNS provider interface implementation
func NewInwxProvider(opts InwxConfig) (*InwxProvider, error) {
	inwxClient := &InwxClientProxy{
		client: goinwx.NewClient(opts.Username, opts.Password, &goinwx.ClientOptions{Sandbox: opts.Sandbox}),
	}

	inwx := &InwxProvider{
		Client:         inwxClient,
		Domain:         opts.DomainFilter,
		DryRun:         opts.DryRun,
		ReloadInterval: opts.ReloadInterval,
	}

	if opts.ReloadInterval == 0 {
		// fallback to 1 hour in case the reload interval is zero
		inwx.ReloadInterval = time.Hour
	}

	return inwx, nil
}

func (inwx *InwxProvider) fetchDomains() ([]*goinwx.NameserverDomain, error) {
	var domains []*goinwx.NameserverDomain

	listResponse, err := inwx.Client.ListNameserverEntries("")
	if err != nil {
		return nil, err
	}

	for idx := range listResponse.Domains {
		if inwx.Domain.Match(listResponse.Domains[idx].Domain) {
			domains = append(domains, &listResponse.Domains[idx])
		}
	}

	inwx.domains = domains
	log.Infof("[INWX] Fetched %d domains", len(domains))

	return domains, nil
}

func (inwx *InwxProvider) findDomainByDNSName(dnsName string) *goinwx.NameserverDomain {
	for _, domain := range inwx.domains {
		if strings.HasSuffix(dnsName, "."+domain.Domain) {
			return domain
		}
	}

	return nil
}

func (inwx *InwxProvider) createRecords(ctx context.Context, endpoints []*endpoint.Endpoint) error {
	var err error
	var clearCache bool

	for _, ep := range endpoints {
		var record *goinwx.NameserverRecordRequest
		record, err = inwx.createCreateRecordRequest(ep)
		if err != nil {
			break
		}

		log.Infof("[INWX] CREATE: name=%s, Domain=%s, type=%s, ttl=%d, content=%s",
			record.Name, record.Domain, record.Type, record.TTL, record.Content)

		if !inwx.DryRun {
			var recID int
			recID, err = inwx.Client.CreateNameserverRecord(record)

			if err != nil {
				break
			}

			log.Debugf("[INWX] Created new record: recID=%d", recID)

			clearCache = true
		}
	}

	if clearCache {
		inwx.clearCachedRecords()
	}

	return err
}

func (inwx *InwxProvider) updateRecords(ctx context.Context, endpoints []*updatedEndpoint) error {
	var err error
	var clearCache bool

	for _, ep := range endpoints {
		// update provider-specifics
		ep.endpointNew.ProviderSpecific = ep.endpointOld.ProviderSpecific
		log.Debugf("[INWX] Update ProviderSpecific for dnsName=%s, type=%s",
			ep.endpointNew.DNSName, ep.endpointNew.RecordType)

		if ep.dnsUpdateRequired() {
			var record *goinwx.NameserverRecordRequest
			record, err = inwx.createUpdateRecordRequest(ep.endpointNew)
			if err != nil {
				break
			}

			var recID int
			recID, err = inwx.findRecordID(ep.endpointNew)
			if err != nil {
				break
			}

			log.Infof("[INWX] UPDATE: recID=%d, name=%s, Domain=%s, type=%s, ttl=%d, content=%s",
				recID, record.Name, record.Domain, record.Type, record.TTL, record.Content)

			if !inwx.DryRun {
				err = inwx.Client.UpdateNameserverRecord(recID, record)
				if err != nil {
					break
				}

				log.Debugf("[INWX] Updated record: recID=%d", recID)

				clearCache = true
			}
		} else {
			log.Debugf("[INWX] No DNS update required for endpointOld=\"%s\" and endpointNew=\"%s\"",
				ep.endpointOld, ep.endpointNew)
		}
	}

	if clearCache {
		inwx.clearCachedRecords()
	}

	return err
}

func (inwx *InwxProvider) findRecordID(endpoint *endpoint.Endpoint) (int, error) {
	domain, err := getProviderSpecificStringProperty(endpoint, providerTypeDomain, true)
	if err != nil {
		return 0, err
	}
	roID, err := getProviderSpecificIntProperty(endpoint, providerTypeRoID, true)
	if err != nil {
		return 0, err
	}

	resp, err := inwx.Client.NameserverInfo(&goinwx.NameserverInfoRequest{
		Domain: domain,
		RoID:   roID,
	})
	if err != nil {
		return 0, err
	}

	for _, record := range resp.Records {
		if endpoint.RecordType == record.Type && endpoint.DNSName == record.Name {
			return record.ID, nil
		}
	}

	return 0, fmt.Errorf(
		"no record found for type=%s and dnsName=%s", endpoint.RecordType, endpoint.DNSName,
	)
}

func (inwx *InwxProvider) deleteRecords(ctx context.Context, endpoints []*endpoint.Endpoint) error {
	var err error
	var clearCache bool

	for _, ep := range endpoints {
		var recID int
		recID, err = inwx.findRecordID(ep)
		if err != nil {
			break
		}

		log.Infof("[INWX] DELETE: recID=%d, dnsName=%s, type=%s, content=%s",
			recID, ep.DNSName, ep.RecordType, ep.Targets[0])

		if !inwx.DryRun {
			err = inwx.Client.DeleteNameserverRecord(recID)
			if err != nil {
				break
			}

			log.Debugf("[INWX] Deleted record: recID=%d", recID)

			clearCache = true
		}
	}

	if clearCache {
		inwx.clearCachedRecords()
	}

	return err
}

func (inwx *InwxProvider) ApplyChanges(ctx context.Context, changes *plan.Changes) error {
	if _, err := inwx.Client.Login(); err != nil {
		return err
	}

	defer func() {
		if err := inwx.Client.Logout(); err != nil {
			log.Errorf("[INWX] Failed to logout: %v", err)
		}
	}()

	if len(changes.Create) > 0 {
		if err := inwx.createRecords(ctx, changes.Create); err != nil {
			return err
		}
	}
	if len(changes.UpdateNew) > 0 {
		if err := inwx.updateRecords(ctx, mergePlanUpdates(changes.UpdateOld, changes.UpdateNew)); err != nil {
			return err
		}
	}
	if len(changes.Delete) > 0 {
		if err := inwx.deleteRecords(ctx, changes.Delete); err != nil {
			return err
		}
	}

	return nil
}

func (inwx *InwxProvider) Records(ctx context.Context) ([]*endpoint.Endpoint, error) {
	if !inwx.cachedRecordsTime.IsZero() && inwx.cachedRecordsTime.Add(inwx.ReloadInterval).After(time.Now()) {
		// directly return cachedRecords if no reload is required
		log.Debugf("[INWX] Loaded %d endpoint cachedRecords from cache", len(inwx.cachedRecords))

		return inwx.cachedRecords, nil
	}

	if _, err := inwx.Client.Login(); err != nil {
		return nil, err
	}

	defer func() {
		if err := inwx.Client.Logout(); err != nil {
			log.Errorf("[INWX] Failed to logout: %v", err)
		}
	}()

	domains, err := inwx.fetchDomains()
	if err != nil {
		return nil, err
	}

	var endpoints []*endpoint.Endpoint
	for _, domain := range domains {
		log.Debugf("[INWX] Fetch cachedRecords for Domain %s", domain.Domain)

		resp, err := inwx.Client.NameserverInfo(&goinwx.NameserverInfoRequest{
			Domain: domain.Domain,
			RoID:   domain.RoID,
		})

		if err != nil {
			return nil, err
		}

		for _, record := range resp.Records {
			log.Debugf("[INWX] Found record with name '%s', type '%s' and content '%s'", record.Name, record.Type, record.Content)

			switch record.Type {
			// we only consider A, CNAME and TXT entries
			case "A", "CNAME", "TXT":
				break
			default:
				continue
			}

			log.Debugf("[INWX] Found record with name=%s, type=%s, content=%s", record.Name, record.Type, record.Content)

			endpoints = append(endpoints, createEndpoint(&record, domain.RoID, domain.Domain))
		}
	}

	// store endpoints
	inwx.cachedRecords = endpoints
	inwx.cachedRecordsTime = time.Now()

	log.Debugf("[INWX] Loaded %d endpoint cachedRecords", len(endpoints))

	return endpoints, nil
}

func (inwx *InwxProvider) createCreateRecordRequest(e *endpoint.Endpoint) (*goinwx.NameserverRecordRequest, error) {
	ttl := 0
	if e.RecordTTL.IsConfigured() {
		ttl = int(e.RecordTTL)
	}

	roID, err := getProviderSpecificIntProperty(e, providerTypeRoID, false)
	if err != nil {
		return nil, err
	}

	domain, err := getProviderSpecificStringProperty(e, providerTypeDomain, false)
	if err != nil {
		return nil, err
	}

	if roID == 0 || domain == "" {
		d := inwx.findDomainByDNSName(e.DNSName)
		if d != nil {
			roID = d.RoID
			domain = d.Domain
		}
	}

	record := goinwx.NameserverRecordRequest{
		RoID:    roID,
		Domain:  domain,
		Name:    getNameFromDNSName(e.DNSName, domain),
		Type:    e.RecordType,
		Content: e.Targets[0],
		TTL:     ttl,
	}

	return &record, nil
}

func (inwx *InwxProvider) createUpdateRecordRequest(e *endpoint.Endpoint) (*goinwx.NameserverRecordRequest, error) {
	ttl := 0
	if e.RecordTTL.IsConfigured() {
		ttl = int(e.RecordTTL)
	}

	domain, err := getProviderSpecificStringProperty(e, providerTypeDomain, false)
	if err != nil {
		return nil, err
	}

	if domain == "" {
		d := inwx.findDomainByDNSName(e.DNSName)
		if d != nil {
			domain = d.Domain
		}
	}

	record := goinwx.NameserverRecordRequest{
		Name:    getNameFromDNSName(e.DNSName, domain),
		Type:    e.RecordType,
		Content: e.Targets[0],
		TTL:     ttl,
	}

	return &record, nil
}

// clearCachedRecords cleares the record cache, useful e. g. after updates
func (inwx *InwxProvider) clearCachedRecords() {
	inwx.cachedRecords = nil
	inwx.cachedRecordsTime = time.Time{}

	log.Debugf("[INWX] Cleared cached records")
}

// createEndpoint creates an endpoint from an INWX nameserver record
func createEndpoint(record *goinwx.NameserverRecord, roID int, domain string) *endpoint.Endpoint {
	e := endpoint.NewEndpointWithTTL(
		record.Name,
		record.Type,
		endpoint.TTL(record.TTL),
		record.Content,
	)

	return e.WithProviderSpecific(providerTypeRoID, strconv.Itoa(roID)).
		WithProviderSpecific(providerTypeDomain, domain)
}

// getProviderSpecificIntProperty returns a ProviderSpecific property as an int
func getProviderSpecificIntProperty(e *endpoint.Endpoint, key string, required bool) (int, error) {
	property, found := e.GetProviderSpecificProperty(key)
	if !found && required {
		return 0, errors.New("Required property " + key + " not found for endpoint with DNS name " + e.DNSName)
	} else if !found {
		return 0, nil
	}

	val, err := strconv.Atoi(property.Value)
	if err != nil {
		return 0, err
	}

	return val, nil
}

// getProviderSpecificStringProperty returns a ProviderSpecific property as a string
func getProviderSpecificStringProperty(e *endpoint.Endpoint, key string, required bool) (string, error) {
	property, found := e.GetProviderSpecificProperty(key)
	if !found && required {
		return "", errors.New("Required property " + key + " not found for endpoint with DNS name " + e.DNSName)
	} else if !found {
		return "", nil
	}

	return property.Value, nil
}

// getNameFromDNSName returns the DNS name without the domain suffix.
//
// For example 'foo' will be returned for dnsName='foo.example.com' and domain='example.com'
func getNameFromDNSName(dnsName string, domain string) string {
	return strings.TrimSuffix(dnsName, "."+domain)
}

// mergePlanUpdates searches for the same endpoints in old + new and brings them together.
//
// This is helpful to access e. g. ProviderSpecifics from the old endpoint
func mergePlanUpdates(endpointsOld []*endpoint.Endpoint, endpointsNew []*endpoint.Endpoint) []*updatedEndpoint {
	var endpoints []*updatedEndpoint

	for idx := range endpointsNew {
		for idy := range endpointsOld {
			endpointOld := endpointsOld[idy]
			endpointNew := endpointsNew[idx]

			if endpointsAreTheSame(endpointOld, endpointNew) {
				endpoints = append(endpoints, &updatedEndpoint{
					endpointOld: endpointOld,
					endpointNew: endpointNew,
				})

				break
			}
		}
	}

	return endpoints
}

type updatedEndpoint struct {
	endpointOld *endpoint.Endpoint
	endpointNew *endpoint.Endpoint
}

// dnsUpdateRequired checks if there were changes to the endpoint that needs to be persisted
func (u *updatedEndpoint) dnsUpdateRequired() bool {
	if endpointTargetChanged(u.endpointOld, u.endpointNew) {
		return true
	}
	// only change if TTL from new endpoint is configured and different from the original one
	if u.endpointNew.RecordTTL.IsConfigured() && endpointTTLChanged(u.endpointOld, u.endpointNew) {
		return true
	}

	return false
}

// endpointsAreTheSame checks if the given endpoints are the same based on the DNS name and Record type
func endpointsAreTheSame(e1 *endpoint.Endpoint, e2 *endpoint.Endpoint) bool {
	return e1.DNSName == e2.DNSName && e1.RecordType == e2.RecordType
}

// endpointTargetChanged checks if the targets of the endpoint changed
func endpointTargetChanged(e1 *endpoint.Endpoint, e2 *endpoint.Endpoint) bool {
	if len(e1.Targets) > 1 {
		log.Warnf("[INWX] More than one target for first endpoint (dnsName=%s, recordType=%s) available, "+
			"but expected only one. Targets=%s", e1.DNSName, e1.RecordType, e1.Targets)
	}
	if len(e2.Targets) > 1 {
		log.Warnf("[INWX] More than one target for second endpoint (dnsName=%s, recordType=%s) available, "+
			"but expected only one. Targets=%s", e2.DNSName, e2.RecordType, e2.Targets)
	}

	return e1.Targets[0] != e2.Targets[0]
}

// endpointTTLChanged checks if the TTL of both endpoints are the same
func endpointTTLChanged(e1 *endpoint.Endpoint, e2 *endpoint.Endpoint) bool {
	// if TTL was configured on one of the endpoints -> changes found
	if e1.RecordTTL.IsConfigured() != e2.RecordTTL.IsConfigured() {
		return true
	}

	// both endpoints doesnt have a TTL configured
	if !e1.RecordTTL.IsConfigured() {
		return false
	}

	// if TTLs are not the same -> changes found
	return e1.RecordTTL != e2.RecordTTL
}
