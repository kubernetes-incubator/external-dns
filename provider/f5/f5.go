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

package f5

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	factory "github.com/F5Networks/f5cs-sdk/dnsfactory"
	authenticationApi "github.com/F5Networks/f5cs-sdk/generated/authentication"
	subscriptionApi "github.com/F5Networks/f5cs-sdk/generated/subscription"
	"github.com/antihax/optional"
	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"

	"sigs.k8s.io/external-dns/endpoint"
	"sigs.k8s.io/external-dns/plan"
	"sigs.k8s.io/external-dns/provider"
)

const (
	defaultClientTimeout  = 10
	defaultF5DNSRecordTTL = 30
	defaultCatalogID      = "c-aaQnOrPjGu"
)

// Config for F5 DNS Provider
type F5DNSConfig struct {
	DryRun       bool
	DomainFilter endpoint.DomainFilter
	Auth         AuthConfig
	AccountID    string
}

// AuthConfig to login to F5 DNS Service
type AuthConfig struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

// Interface for Subscription service API
type F5DNSSubscriptionAPI interface {
	ListSubscriptions(context.Context, string, *subscriptionApi.ListSubscriptionsOpts) (subscriptionApi.V1Subscriptions, *http.Response, error)
	UpdateSubscription(context.Context, string, subscriptionApi.V1UpdateSubscriptionRequest) (subscriptionApi.V1Subscription, *http.Response, error)
	BatchActivateSubscription(context.Context, subscriptionApi.V1BatchSubscriptionIdRequest) (subscriptionApi.V1BatchSubscriptionStatusResponse, *http.Response, error)
	BatchRetireSubscription(context.Context, subscriptionApi.V1BatchSubscriptionIdRequest) (subscriptionApi.V1BatchSubscriptionStatusResponse, *http.Response, error)
	CreateSubscription(context.Context, subscriptionApi.V1CreateSubscriptionRequest) (subscriptionApi.V1Subscription, *http.Response, error)
	ActivateSubscription(context.Context, string) (subscriptionApi.V1SubscriptionStatusResponse, *http.Response, error)
	RetireSubscription(context.Context, string) (subscriptionApi.V1SubscriptionStatusResponse, *http.Response, error)
}

// Interface for Authentication service API
type F5DNSAuthenticationAPI interface {
	Login(context.Context, authenticationApi.AuthenticationServiceLoginRequest) (authenticationApi.AuthenticationServiceLoginReply, *http.Response, error)
}

// F5Client using F5 SDK
type F5Client struct {
	authClientAPI   F5DNSAuthenticationAPI
	subClientAPI    F5DNSSubscriptionAPI
	f               factory.Factory
	AccessToken     string
	TokenExpiryTime time.Time
}

// F5DNSProvider is the DNS provider
type F5DNSProvider struct {
	provider.BaseProvider              // base provider
	client                *F5Client    // corresponds to the F5 SDK Client
	config                *F5DNSConfig // F5 Config
}

// NewF5DNSProvider to instantiate F5 provider
func NewF5DNSProvider(cfg *F5DNSConfig, zoneFilter string) (*F5DNSProvider, error) {
	authCfg := authenticationApi.NewConfiguration()
	authCfg.HTTPClient = &http.Client{
		Timeout: time.Second * defaultClientTimeout,
	}
	authClient := authenticationApi.NewAPIClient(authCfg)
	subCfg := subscriptionApi.NewConfiguration()
	subCfg.HTTPClient = &http.Client{
		Timeout: time.Second * defaultClientTimeout,
	}
	subClient := subscriptionApi.NewAPIClient(subCfg)
	return &F5DNSProvider{
		config: cfg,
		client: &F5Client{
			authClientAPI: authClient.AuthenticationServiceApi,
			subClientAPI:  subClient.SubscriptionServiceApi,
			f:             factory.NewFactory(),
		},
	}, nil
}

// Subscriptions to track subscriptions to be created/edited in an invocation of
// ApplyChanges
type Subscriptions struct {
	create map[string]factory.ServiceRequest
	edit   map[string]factory.ServiceRequest
}

// ApplyChanges is the implementation for F5 Provider.
// For each subscription whose zone matches the domain-filter(s), the changes are applied if there are any
// applicable changes.
func (p *F5DNSProvider) ApplyChanges(ctx context.Context, changes *plan.Changes) error {
	log.Infof("ApplyChanges invoked with Changes Delete %v Create %v UpdateOld %v UpdateNew %v", changes.Delete, changes.Create, changes.UpdateOld, changes.UpdateNew)
	if len(changes.Create) == 0 && len(changes.Delete) == 0 && len(changes.UpdateNew) == 0 && len(changes.UpdateOld) == 0 {
		log.Info("No changes to DNS records needed.")
		return nil
	}
	// Find subscriptions matching the domainFilter
	subscriptions, err := p.GetFilteredSubscriptions()
	if err != nil {
		return err
	}
	// Subscriptions to be created or edited
	subsMap := &Subscriptions{
		create: make(map[string]factory.ServiceRequest),
		edit:   subscriptions,
	}
	p.HandleChangesDelete(subsMap, changes.Delete)
	p.HandleChangesUpdateNew(subsMap, changes.UpdateNew, changes.UpdateOld)
	p.HandleChangesCreate(subsMap, changes.Create)

	// Set AccessToken as we have already logged in
	authCtx := context.WithValue(context.Background(), subscriptionApi.ContextAccessToken, p.client.AccessToken)

	// Now process subscriptions to figure out which need to be Deleted/Created/Updated
	// No Transaction Support. So possible that Updates might succeed but Deletes might fails.

	var failedUpdateSubscriptions []string
	var failedRetireSubscriptions []string
	var failedZonesCreateSubscription []string
	var failedActivateSubscriptions []string
	for _, subscription := range subsMap.edit {
		if len(subscription.Configuration.GslbService.GetLoadBalancedRecords()) == 0 {
			if !p.config.DryRun {
				_, _, err = p.client.subClientAPI.RetireSubscription(authCtx, subscription.SubscriptionId)
				if err != nil {
					log.Errorf("Failed to retire subscription: %v. Error %s", subscription.SubscriptionId, err)
					failedRetireSubscriptions = append(failedRetireSubscriptions, subscription.SubscriptionId)
					continue
				}
			} else {
				log.Debugf("DryRun: F5 DNS RetireSubscription %s", subscription.SubscriptionId)
			}
		} else {
			if !p.config.DryRun {
				apiJSON, err := json.Marshal(subscription)
				if err != nil {
					log.Errorf("Failed to marshal JSON for update subscription %s. Error %s", subscription.SubscriptionId, err)
					failedUpdateSubscriptions = append(failedUpdateSubscriptions, subscription.SubscriptionId)
					continue
				}
				// UpdateSubscriptions first
				// Updates will be automatically activated.
				updateRequest := subscriptionApi.V1UpdateSubscriptionRequest{}
				err = json.Unmarshal(apiJSON, &updateRequest)
				if err != nil {
					log.Errorf("Failed to unmarshal for update subscription %s. Error %s", subscription.SubscriptionId, err)
					continue
				}
				_, _, err = p.client.subClientAPI.UpdateSubscription(authCtx, subscription.SubscriptionId, updateRequest)
				if err != nil {
					errDetails := ""
					if _, ok := err.(subscriptionApi.GenericOpenAPIError); ok {
						errDetails = string(err.(subscriptionApi.GenericOpenAPIError).Body())
					}
					log.Errorf("Failed to update subscription %s. Error %s, Error Details %s, Body %s", subscription.SubscriptionId, err, errDetails, apiJSON)
					failedUpdateSubscriptions = append(failedUpdateSubscriptions, subscription.SubscriptionId)
					continue
				}
			} else {
				log.Debugf("DryRun: F5 DNS UpdateSubscription %s", subscription.SubscriptionId)
			}
		}
	}

	if !p.config.DryRun {
		for _, subscription := range subsMap.create {
			apiJSON, err := json.Marshal(subscription)
			if err != nil {
				log.Errorf("Failed to marshal JSON subscription with zone %s. Error %s", subscription.Configuration.GslbService.GetZone(), err)
				failedZonesCreateSubscription = append(failedZonesCreateSubscription, subscription.Configuration.GslbService.GetZone())
				continue
			}
			createRequest := subscriptionApi.V1CreateSubscriptionRequest{}
			err = json.Unmarshal(apiJSON, &createRequest)
			if err != nil {
				log.Errorf("Failed to unmarshal create subscription with zone %s. Error %s", subscription.Configuration.GslbService.GetZone(), err)
			}
			// CreateSubscriptions
			createdSub, _, err := p.client.subClientAPI.CreateSubscription(authCtx, createRequest)
			if err != nil {
				errDetails := ""
				if _, ok := err.(subscriptionApi.GenericOpenAPIError); ok {
					errDetails = string(err.(subscriptionApi.GenericOpenAPIError).Body())
				}
				log.Errorf("Failed to create subscription for zone %s. Error %s, Error Details %s, Body %s", subscription.Configuration.GslbService.GetZone(), err, errDetails, apiJSON)
				failedZonesCreateSubscription = append(failedZonesCreateSubscription, subscription.Configuration.GslbService.GetZone())
				continue
			}
			_, _, err = p.client.subClientAPI.ActivateSubscription(authCtx, createdSub.SubscriptionId)
			if err != nil {
				errDetails := ""
				if _, ok := err.(subscriptionApi.GenericOpenAPIError); ok {
					errDetails = string(err.(subscriptionApi.GenericOpenAPIError).Body())
				}
				log.Errorf("Failed to activate subscription %v. Error %s, Error Details %s", createdSub.SubscriptionId, err, errDetails)
				failedActivateSubscriptions = append(failedActivateSubscriptions, createdSub.SubscriptionId)
			}
			log.Debugf("CreatedSubscription %v, Error %v", createdSub, err)
		}
	} else {
		log.Debugf("DryRun: F5 DNS CreateSubscriptions %d", len(subsMap.create))
	}

	if len(failedUpdateSubscriptions) != 0 || len(failedRetireSubscriptions) != 0 || len(failedZonesCreateSubscription) != 0 || len(failedActivateSubscriptions) != 0 {
		errMsg := fmt.Sprintf("Failed to apply changes, failedUpdateSubscriptions: %v, failedRetireSubscriptions: %v, failedActivateSubscriptions: %v, failedZonesCreateSubscription: %v", failedUpdateSubscriptions, failedRetireSubscriptions, failedActivateSubscriptions, failedZonesCreateSubscription)
		log.Error(errMsg)
		return errors.New(errMsg)
	}
	log.Debugf("Successfully updated records. Modified/Deleted %d, Created %d", len(subsMap.edit), len(subsMap.create))

	return nil
}

// HandleChangesDelete will handle Changes.Delete and update the subscriptions by
// deleting the LBR's, pool, etc.
func (p *F5DNSProvider) HandleChangesDelete(subsMap *Subscriptions, toDelete []*endpoint.Endpoint) {
	subscriptions := subsMap.edit
	for _, endPt := range toDelete {
		if endPt.RecordType != "CNAME" && endPt.RecordType != "A" && endPt.RecordType != "AAAA" {
			continue
		}
		dnsName := endPt.DNSName
		var alias string
		var zoneName string
		alias, zoneName = getZoneFromDNSName(dnsName)
		if _, ok := subscriptions[zoneName]; !ok {
			// Search for apex
			if _, ok := subscriptions[dnsName]; !ok {
				log.Errorf("DNSName %s to be deleted not found in subscriptions", dnsName)
				continue
			} else {
				zoneName = dnsName
				alias = ""
			}
		}
		// Find matching LBR
		subscription := subscriptions[zoneName]
		lbrs := subscription.Configuration.GslbService.GetLoadBalancedRecords()
		if _, ok := lbrs[getLBRKey(alias)]; !ok {
			log.Errorf("Load Balanced Record not found for DNSName %s", dnsName)
			continue
		}
		lbr := lbrs[getLBRKey(alias)]
		if *lbr.RRType != "CNAME" {
			// Remove Virtual Servers
			pool := subscription.Configuration.GslbService.GetPool(getPoolKey(alias))
			if pool != nil {
				for _, pm := range pool.Members {
					vs := pm.VirtualServer
					subscription.Configuration.GslbService.RemoveVirtualServer(*vs)
				}
			}
		}
		subscription.Configuration.GslbService.RemovePool(getPoolKey(alias))
		subscription.Configuration.GslbService.RemoveLoadBalancedRecord(getLBRKey(alias))
	}
}

// BuildNewSubscription is a helper to build a new subscription object for a Endpoint.
func (p *F5DNSProvider) BuildNewSubscription(endPt *endpoint.Endpoint, zoneName string, alias string) factory.ServiceRequest {
	newSub := factory.ServiceRequest{
		AccountId:           p.config.AccountID,
		CatalogID:           defaultCatalogID,
		ServiceType:         "gslb",
		ServiceInstanceName: zoneName,
		Configuration: &factory.Configuration{
			GslbService: &factory.GSLBService{
				Zone: p.client.f.NewStringPointer(zoneName),
			},
		},
	}
	p.CreateLBRPoolVS(endPt, &newSub, alias)
	return newSub
}

// CreateLBRPoolVS is a helper to create LBR, Pool, VirtualServer corresponding to the Endpoint
// and add it to the subscription object
func (p *F5DNSProvider) CreateLBRPoolVS(endPt *endpoint.Endpoint, subscription *factory.ServiceRequest, alias string) {
	rrType := endPt.RecordType
	// Sub-domain of DNSName becomes alias for the LoadBalancedRecord
	// Add Remark based on endpoint.Label
	newLbr := factory.LoadBalancedRecord{
		Enable:      p.client.f.NewBoolPointer(true),
		DisplayName: p.client.f.NewStringPointer(alias),
		RRType:      p.client.f.NewStringPointer(rrType),
		Aliases:     []string{alias},
		Persistence: p.client.f.NewBoolPointer(false),
		ProximityRules: []factory.ProximityRule{{
			Region: p.client.f.NewStringPointer("global"),
			Pool:   p.client.f.NewStringPointer(getPoolKey(alias)),
			Score:  p.client.f.NewIntPointer(100),
		}},
		Remark: p.client.f.NewStringPointer(getRemark(endPt)),
	}
	ttl := defaultF5DNSRecordTTL
	if endPt.RecordTTL.IsConfigured() {
		ttl = int(endPt.RecordTTL)
	}
	newPool := factory.Pool{
		RRType:      rrType,
		DisplayName: p.client.f.NewStringPointer(getPoolKey(alias)),
		Enable:      p.client.f.NewBoolPointer(true),
		TTL:         p.client.f.NewIntPointer(ttl),
		Members:     []factory.Member{},
	}
	// Targets become pool-members and for A/AAAA types even virtual servers.
	for _, ip := range endPt.Targets {
		if rrType != "CNAME" {
			vsKey := getVirtualServerKey(alias, ip)
			newVS := factory.VirtualServer{
				Address:           p.client.f.NewStringPointer(ip),
				VirtualServerType: p.client.f.NewStringPointer(factory.VIRTUAL_SERVER_TYPE_CLOUD),
				Port:              p.client.f.NewIntPointer(80),
				DisplayName:       p.client.f.NewStringPointer(vsKey),
			}
			subscription.Configuration.GslbService.AddVirtualServer(vsKey, newVS)
			member := factory.Member{
				VirtualServer: p.client.f.NewStringPointer(vsKey),
				Monitor:       p.client.f.NewStringPointer("none"),
			}
			newPool.Members = append(newPool.Members, member)
		} else {
			member := factory.Member{
				Domain: p.client.f.NewStringPointer(ip),
				Final:  p.client.f.NewBoolPointer(true),
			}
			newPool.Members = append(newPool.Members, member)
		}
	}
	subscription.Configuration.GslbService.AddLoadBalancedRecord(getLBRKey(alias), newLbr)
	subscription.Configuration.GslbService.AddPool(getPoolKey(alias), newPool)
}

// HandleChangesCreate will handle changes.Create and create new subscription objects
func (p *F5DNSProvider) HandleChangesCreate(subsMap *Subscriptions, toCreate []*endpoint.Endpoint) {
	subscriptions := subsMap.edit
	newSubscriptions := subsMap.create
	for _, endPt := range toCreate {
		if endPt.RecordType != "CNAME" && endPt.RecordType != "A" && endPt.RecordType != "AAAA" {
			continue
		}
		dnsName := endPt.DNSName
		var zoneName string
		var alias string
		if _, ok := subscriptions[dnsName]; ok {
			zoneName = dnsName
			alias = ""
		} else {
			alias, zoneName = getZoneFromDNSName(dnsName)
		}
		if _, ok := subscriptions[zoneName]; !ok {
			if _, ok := newSubscriptions[zoneName]; !ok {
				// NewSubscription
				newSub := p.BuildNewSubscription(endPt, zoneName, alias)
				newSubscriptions[zoneName] = newSub
				continue
			}
			// Add LBR to new subscription
			subscription := newSubscriptions[zoneName]
			p.CreateLBRPoolVS(endPt, &subscription, alias)
			continue
		}
		subscription := subscriptions[zoneName]
		// Subscription exists. Need to add LBR, new pool, virtual servers
		p.CreateLBRPoolVS(endPt, &subscription, alias)
	}
}

// HandleChangesUpdateNew will handle changes.UpdateNew to update the subscription
// with the modified CNAME/A/AAAA changes
func (p *F5DNSProvider) HandleChangesUpdateNew(subsMap *Subscriptions, toUpdate []*endpoint.Endpoint, current []*endpoint.Endpoint) {
	subscriptions := subsMap.edit
	for _, endPt := range toUpdate {
		if endPt.RecordType != "CNAME" && endPt.RecordType != "A" && endPt.RecordType != "AAAA" {
			continue
		}
		dnsName := endPt.DNSName
		var zoneName string
		var alias string
		if _, ok := subscriptions[dnsName]; ok {
			zoneName = dnsName
			alias = ""
		} else {
			alias, zoneName = getZoneFromDNSName(dnsName)
		}
		if _, ok := subscriptions[zoneName]; !ok {
			log.Errorf("No existing subscription matching zone for DNSName %s", dnsName)
			continue
		}
		subscription := subscriptions[zoneName]
		var existingEndPt *endpoint.Endpoint
		for _, currentEndpt := range current {
			if currentEndpt.DNSName == dnsName {
				existingEndPt = currentEndpt
				break
			}
		}
		p.UpdateSubscriptionFromEndpoint(endPt, existingEndPt, &subscription, alias)
	}
}

// UpdateSubscriptionFromEndpoint will update the subscription body using the updatedEndpoint
func (p *F5DNSProvider) UpdateSubscriptionFromEndpoint(updatedEndpoint *endpoint.Endpoint, current *endpoint.Endpoint, subscription *factory.ServiceRequest, alias string) {
	// Overwrite the Pool-members and Virtual Servers
	dnsName := updatedEndpoint.DNSName
	rrType := updatedEndpoint.RecordType
	pool := subscription.Configuration.GslbService.GetPool(getPoolKey(alias))
	lbrs := subscription.Configuration.GslbService.GetLoadBalancedRecords()
	if pool == nil {
		log.Errorf("Pool not found in subscription for DNSName %s", dnsName)
		return
	}
	if rrType != "CNAME" {
		for _, old := range current.Targets {
			subscription.Configuration.GslbService.RemoveVirtualServer(getVirtualServerKey(alias, old))
		}
		pool.Members = nil
		for _, ip := range updatedEndpoint.Targets {
			vsKey := getVirtualServerKey(alias, ip)
			newVS := factory.VirtualServer{
				Address:           p.client.f.NewStringPointer(ip),
				VirtualServerType: p.client.f.NewStringPointer(factory.VIRTUAL_SERVER_TYPE_CLOUD),
				Port:              p.client.f.NewIntPointer(80),
				DisplayName:       p.client.f.NewStringPointer(vsKey),
			}
			subscription.Configuration.GslbService.AddVirtualServer(vsKey, newVS)
			member := factory.Member{
				VirtualServer: p.client.f.NewStringPointer(vsKey),
				Monitor:       p.client.f.NewStringPointer("none"),
			}
			pool.Members = append(pool.Members, member)
		}
	} else {
		pool.Members = nil
		for _, domain := range updatedEndpoint.Targets {
			member := factory.Member{
				Domain: p.client.f.NewStringPointer(domain),
				Final:  p.client.f.NewBoolPointer(true),
			}
			pool.Members = append(pool.Members, member)
		}
	}
	lbrKey := getLBRKey(alias)
	lbr := lbrs[lbrKey]

	lbr.Remark = p.client.f.NewStringPointer(getRemark(updatedEndpoint))

	subscription.Configuration.GslbService.AddPool(getPoolKey(alias), *pool)
	subscription.Configuration.GslbService.AddLoadBalancedRecord(lbrKey, lbr)
}

// Records is implementation of the Provider interface which should
// return the Endpoints as expected by external-dns.
func (p *F5DNSProvider) Records(ctx context.Context) ([]*endpoint.Endpoint, error) {
	subs, err := p.GetFilteredSubscriptions()
	if err != nil {
		log.Debugf("GetFilteredSubscriptions. Err %v", err)
		return nil, err
	}
	if len(subs) == 0 {
		log.Debugf("Records has empty subscriptions.")
		return nil, nil
	}
	records := p.TransformToEndpointRecords(subs)
	log.Debugf("Records %v", records)
	return records, nil
}

// TransformToEndpointRecords will transform the subscriptions to Endpoints
func (p *F5DNSProvider) TransformToEndpointRecords(subscriptions map[string]factory.ServiceRequest) []*endpoint.Endpoint {
	var result []*endpoint.Endpoint
	for zone, subscription := range subscriptions {
		lbrs := *subscription.Configuration.GslbService.LoadBalancedRecords
		for _, lbr := range lbrs {
			if lbr.Aliases == nil || len(lbr.Aliases) == 0 || subscription.Configuration.GslbService.Pools == nil {
				continue
			}
			alias := lbr.Aliases[0]
			dnsName := generateDNSName(zone, alias)
			poolKey := getPoolKey(alias)
			pools := *subscription.Configuration.GslbService.Pools
			pool := pools[poolKey]
			members := pool.Members
			// Members become endpoint.Targets and zone becomes endpoint.DNSName
			var targets []string
			for _, member := range members {
				var addr *string
				if pool.RRType == "CNAME" {
					addr = member.Domain
					targets = append(targets, *addr)
				} else {
					if subscription.Configuration.GslbService.VirtualServers == nil {
						continue
					}
					virtuals := *(subscription.Configuration.GslbService.VirtualServers)
					vsName := member.VirtualServer
					ip, ok := virtuals[*vsName]
					if !ok {
						continue
					}
					addr = ip.Address
					targets = append(targets, *addr)
				}
			}
			// Construct A/AAAA/CNAME Endpoint only if remark is present.
			// LBR's created by external-dns will have remark set to the ownership TXT
			if lbr.Remark != nil {
				ownerLabel, err := endpoint.NewLabelsFromString(*lbr.Remark)
				if err != nil {
					// if the remark is not in the form of heritage TXT records, its not
					// an LBR managed by external-dns. Skip it.
					continue
				}
				endPt := &endpoint.Endpoint{
					RecordType: pool.RRType,
					RecordTTL:  endpoint.TTL(int64(*pool.TTL)),
					Targets:    endpoint.NewTargets(targets...),
					DNSName:    dnsName,
					Labels:     ownerLabel,
				}
				result = append(result, endPt)
			}
		}
	}
	return result
}

// Login will set the accesstoken from auth response
func (p *F5DNSProvider) Login() error {
	if p.client.AccessToken != "" && !p.client.TokenExpiryTime.IsZero() && time.Until(p.client.TokenExpiryTime).Seconds() > 10 {
		// Token has not expired. Reuse it.
		return nil
	}
	login, _, err := p.client.authClientAPI.Login(context.Background(), authenticationApi.AuthenticationServiceLoginRequest{
		Username: p.config.Auth.Username,
		Password: p.config.Auth.Password,
	})
	if err != nil {
		log.Errorf("Failed to login to F5 DNS service. Err %v", err)
		return err
	}
	p.client.AccessToken = login.AccessToken
	expiryTime, err := strconv.ParseInt(login.ExpiresAt, 10, 64)
	if err != nil {
		log.Errorf("Invalid token contents")
		return err
	}
	// Get the new token's expiry time.
	// TODO - Use Refresh token in future.
	p.client.TokenExpiryTime = time.Now().Add(time.Duration(expiryTime) * time.Second)
	return nil
}

// GetSubscriptions will return all the subscriptions for the accountID from
// DNS service
func (p *F5DNSProvider) GetSubscriptions(accountID string) (*factory.ServiceRequests, error) {
	// Get subscriptions of DNS LB type
	var result factory.ServiceRequests
	opts := &subscriptionApi.ListSubscriptionsOpts{
		ServiceType: optional.NewString("gslb"),
	}
	authCtx := context.WithValue(context.Background(), subscriptionApi.ContextAccessToken, p.client.AccessToken)
	for {
		subs, _, err := p.client.subClientAPI.ListSubscriptions(authCtx, p.config.AccountID, opts)
		if err != nil {
			return nil, err
		}
		b, err := json.Marshal(subs)
		if err != nil {
			return nil, err
		}
		var subscriptions factory.ServiceRequests
		err = json.Unmarshal(b, &subscriptions)
		if err != nil {
			return nil, err
		}
		result.Subscriptions = append(result.Subscriptions, subscriptions.Subscriptions...)
		opts.PageToken = optional.NewString(subs.PageToken)
		if subs.PageToken == "" {
			break
		}
	}
	return &result, nil
}

// GetFilteredSubscriptions will return the subscriptions for the account
// which match the DomainFilter provided in the config.
func (p *F5DNSProvider) GetFilteredSubscriptions() (map[string]factory.ServiceRequest, error) {
	err := p.Login()
	if err != nil {
		log.Debugf("Login failed. Error %v", err)
		return nil, err
	}
	subscriptions, err := p.GetSubscriptions(p.config.AccountID)
	if err != nil {
		log.Debugf("GetSubscriptions failed. Error %v", err)
		return nil, err
	}
	if subscriptions.Subscriptions == nil || len(subscriptions.Subscriptions) == 0 {
		log.Debug("No subscriptions found")
		return nil, nil
	}

	subs := make(map[string]factory.ServiceRequest)
	for _, sub := range subscriptions.Subscriptions {
		if sub.ServiceType != "gslb" {
			continue
		}
		zone := sub.Configuration.GslbService.Zone
		// Match zones which have domain-filter as suffix
		// And return a map of subscriptions
		// Use dnsName as key in map
		if len(p.config.DomainFilter.Filters) != 0 {
			foundZone := false
			for _, id := range p.config.DomainFilter.Filters {
				if id == *zone {
					foundZone = true
					break
				}
			}
			if !foundZone {
				continue
			}
		}

		log.Debugf("Matching Subscription found for zone %s subscriptionId %s", *zone, sub.SubscriptionId)
		subs[*zone] = sub
	}
	return subs, nil
}

func (p *F5DNSProvider) GetF5Client() *F5Client {
	return p.client
}

// getZoneFromDNSName will return zone and alias from the DNSName
// which is alias.zone
// alias for DNS LB's LBR is a single sub-domain
func getZoneFromDNSName(dnsName string) (string, string) {
	zoneIdx := strings.Index(dnsName, ".") + 1
	zone := dnsName[zoneIdx:]
	alias := dnsName[0 : zoneIdx-1]
	return alias, zone
}

// DNSName of the endpoint is alias.zone
func generateDNSName(zone string, alias string) string {
	nameElems := []string{alias, zone}
	if alias == "" {
		return zone
	}
	return strings.Join(nameElems, ".")
}

// Pool key is derived from sub-domain of the domain
// So sanitize it to remove invalid characters
func getPoolKey(alias string) string {
	nameElems := []string{strings.ReplaceAll(alias, "-", "_"), "pool"}
	return strings.Join(nameElems, "")
}

func min(x, y int) int {
	if x < y {
		return x
	}
	return y
}

// Remark to be set using label in the Endpoint
// TODO Fix the length when API support remark field of 512 characters
func getRemark(endpoint *endpoint.Endpoint) string {
	if endpoint == nil {
		return ""
	}
	// The Label contains the record ownership information
	txt := endpoint.Labels.Serialize(true)
	runes := []rune(txt)
	txtLen := min(len(runes), 127)
	return string(runes[0:txtLen])
}

// LBR key is derived from sub-domain of the domain
// So sanitize it to remove invalid characters
func getLBRKey(alias string) string {
	if alias == "" {
		return "F5_DNS_APEX"
	}
	return strings.ReplaceAll(alias, "-", "_")
}

// VirtualServer Key is derived from sub-domain of the domain
// and the IP address
func getVirtualServerKey(alias string, ip string) string {
	nameElems := []string{strings.ReplaceAll(alias, "-", "_"), ConvertIPToVS(ip)}
	return strings.Join(nameElems, "")
}

func ConvertIPToVS(ip string) string {
	r := []rune(ip)
	d := []rune{'v', 's'}
	for i := 0; i < len(r); i++ {
		if r[i] == '.' {
			d = append(d, '_')
		} else if r[i] == ':' {
			d = append(d, '_')
		} else {
			d = append(d, r[i])
		}
	}
	return string(d)
}
