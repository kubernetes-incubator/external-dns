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

package alibabacloud

import (
	"context"
	"fmt"
	"io/ioutil"
	"strings"
	"sync"
	"time"

	"github.com/aliyun/alibaba-cloud-sdk-go/sdk/requests"
	"github.com/aliyun/alibaba-cloud-sdk-go/services/alidns"
	"github.com/aliyun/alibaba-cloud-sdk-go/services/pvtz"
	"github.com/denverdino/aliyungo/metadata"
	log "github.com/sirupsen/logrus"
	"gopkg.in/yaml.v2"

	"sigs.k8s.io/external-dns/endpoint"
	"sigs.k8s.io/external-dns/plan"
	"sigs.k8s.io/external-dns/provider"
)

const (
	defaultAlibabaCloudRecordTTL            = 600
	defaultAlibabaCloudPrivateZoneRecordTTL = 60
	defaultAlibabaCloudPageSize             = 50
	nullHostAlibabaCloud                    = "@"
	pVTZDoamin                              = "pvtz.aliyuncs.com"
)

// AlibabaCloudDNSAPI is a minimal implementation of DNS API that we actually use, used primarily for unit testing.
// See https://help.aliyun.com/document_detail/29739.html for descriptions of all of its methods.
type AlibabaCloudDNSAPI interface {
	AddDomainRecord(request *alidns.AddDomainRecordRequest) (response *alidns.AddDomainRecordResponse, err error)
	DeleteDomainRecord(request *alidns.DeleteDomainRecordRequest) (response *alidns.DeleteDomainRecordResponse, err error)
	UpdateDomainRecord(request *alidns.UpdateDomainRecordRequest) (response *alidns.UpdateDomainRecordResponse, err error)
	DescribeDomainRecords(request *alidns.DescribeDomainRecordsRequest) (response *alidns.DescribeDomainRecordsResponse, err error)
	DescribeDomains(request *alidns.DescribeDomainsRequest) (response *alidns.DescribeDomainsResponse, err error)
}

// AlibabaCloudPrivateZoneAPI is a minimal implementation of Private Zone API that we actually use, used primarily for unit testing.
// See https://help.aliyun.com/document_detail/66234.html for descriptions of all of its methods.
type AlibabaCloudPrivateZoneAPI interface {
	AddZoneRecord(request *pvtz.AddZoneRecordRequest) (response *pvtz.AddZoneRecordResponse, err error)
	DeleteZoneRecord(request *pvtz.DeleteZoneRecordRequest) (response *pvtz.DeleteZoneRecordResponse, err error)
	UpdateZoneRecord(request *pvtz.UpdateZoneRecordRequest) (response *pvtz.UpdateZoneRecordResponse, err error)
	DescribeZoneRecords(request *pvtz.DescribeZoneRecordsRequest) (response *pvtz.DescribeZoneRecordsResponse, err error)
	DescribeZones(request *pvtz.DescribeZonesRequest) (response *pvtz.DescribeZonesResponse, err error)
	DescribeZoneInfo(request *pvtz.DescribeZoneInfoRequest) (response *pvtz.DescribeZoneInfoResponse, err error)
}

// AlibabaCloudProvider implements the DNS provider for Alibaba Cloud.
type AlibabaCloudProvider struct {
	provider.BaseProvider
	domainFilter         endpoint.DomainFilter
	zoneIDFilter         provider.ZoneIDFilter // Private Zone only
	MaxChangeCount       int
	EvaluateTargetHealth bool
	AssumeRole           string
	vpcID                string // Private Zone only
	dryRun               bool
	dnsClient            AlibabaCloudDNSAPI
	pvtzClient           AlibabaCloudPrivateZoneAPI
	privateZone          bool
	clientLock           sync.RWMutex
	nextExpire           time.Time
}

type alibabaCloudConfig struct {
	RegionID        string    `json:"regionId" yaml:"regionId"`
	AccessKeyID     string    `json:"accessKeyId" yaml:"accessKeyId"`
	AccessKeySecret string    `json:"accessKeySecret" yaml:"accessKeySecret"`
	VPCID           string    `json:"vpcId" yaml:"vpcId"`
	RoleName        string    `json:"-" yaml:"-"` // For ECS RAM role only
	StsToken        string    `json:"-" yaml:"-"`
	ExpireTime      time.Time `json:"-" yaml:"-"`
}

// NewAlibabaCloudProvider creates a new Alibaba Cloud provider.
//
// Returns the provider or an error if a provider could not be created.
func NewAlibabaCloudProvider(configFile string, domainFilter endpoint.DomainFilter, zoneIDFileter provider.ZoneIDFilter, zoneType string, dryRun bool) (*AlibabaCloudProvider, error) {
	cfg := alibabaCloudConfig{}
	if configFile != "" {
		contents, err := ioutil.ReadFile(configFile)
		if err != nil {
			return nil, fmt.Errorf("failed to read Alibaba Cloud config file '%s': %v", configFile, err)
		}
		err = yaml.Unmarshal(contents, &cfg)
		if err != nil {
			return nil, fmt.Errorf("failed to parse Alibaba Cloud config file '%s': %v", configFile, err)
		}
	} else {
		var tmpError error
		cfg, tmpError = getCloudConfigFromStsToken()
		if tmpError != nil {
			return nil, fmt.Errorf("failed to getCloudConfigFromStsToken: %v", tmpError)
		}
	}

	// Public DNS service
	var dnsClient AlibabaCloudDNSAPI
	var err error

	if cfg.RoleName == "" {
		dnsClient, err = alidns.NewClientWithAccessKey(
			cfg.RegionID,
			cfg.AccessKeyID,
			cfg.AccessKeySecret,
		)
	} else {
		dnsClient, err = alidns.NewClientWithStsToken(
			cfg.RegionID,
			cfg.AccessKeyID,
			cfg.AccessKeySecret,
			cfg.StsToken,
		)
	}

	if err != nil {
		return nil, fmt.Errorf("failed to create Alibaba Cloud DNS client: %v", err)
	}

	// Private DNS service
	var pvtzClient AlibabaCloudPrivateZoneAPI
	if cfg.RoleName == "" {
		pvtzClient, err = pvtz.NewClientWithAccessKey(
			"cn-hangzhou", // The Private Zone location is fixed
			cfg.AccessKeyID,
			cfg.AccessKeySecret,
		)
	} else {
		pvtzClient, err = pvtz.NewClientWithStsToken(
			cfg.RegionID,
			cfg.AccessKeyID,
			cfg.AccessKeySecret,
			cfg.StsToken,
		)
	}

	if err != nil {
		return nil, err
	}

	provider := &AlibabaCloudProvider{
		domainFilter: domainFilter,
		zoneIDFilter: zoneIDFileter,
		vpcID:        cfg.VPCID,
		dryRun:       dryRun,
		dnsClient:    dnsClient,
		pvtzClient:   pvtzClient,
		privateZone:  zoneType == "private",
	}

	if cfg.RoleName != "" {
		provider.setNextExpire(cfg.ExpireTime)
		go provider.refreshStsToken(1 * time.Second)
	}
	return provider, nil
}

func getCloudConfigFromStsToken() (alibabaCloudConfig, error) {
	cfg := alibabaCloudConfig{}
	// Load config from Metadata Service
	m := metadata.NewMetaData(nil)
	roleName := ""
	var err error
	if roleName, err = m.RoleName(); err != nil {
		return cfg, fmt.Errorf("failed to get role name from Metadata Service: %v", err)
	}
	vpcID, err := m.VpcID()
	if err != nil {
		return cfg, fmt.Errorf("failed to get VPC ID from Metadata Service: %v", err)
	}
	regionID, err := m.Region()
	if err != nil {
		return cfg, fmt.Errorf("failed to get Region ID from Metadata Service: %v", err)
	}
	role, err := m.RamRoleToken(roleName)
	if err != nil {
		return cfg, fmt.Errorf("failed to get STS Token from Metadata Service: %v", err)
	}
	cfg.RegionID = regionID
	cfg.RoleName = roleName
	cfg.VPCID = vpcID
	cfg.AccessKeyID = role.AccessKeyId
	cfg.AccessKeySecret = role.AccessKeySecret
	cfg.StsToken = role.SecurityToken
	cfg.ExpireTime = role.Expiration
	return cfg, nil
}

func (p *AlibabaCloudProvider) getDNSClient() AlibabaCloudDNSAPI {
	p.clientLock.RLock()
	defer p.clientLock.RUnlock()
	return p.dnsClient
}

func (p *AlibabaCloudProvider) getPvtzClient() AlibabaCloudPrivateZoneAPI {
	p.clientLock.RLock()
	defer p.clientLock.RUnlock()
	return p.pvtzClient
}

func (p *AlibabaCloudProvider) setNextExpire(expireTime time.Time) {
	p.clientLock.Lock()
	defer p.clientLock.Unlock()
	p.nextExpire = expireTime
}

func (p *AlibabaCloudProvider) refreshStsToken(sleepTime time.Duration) {
	for {
		time.Sleep(sleepTime)
		now := time.Now()
		utcLocation, err := time.LoadLocation("")
		if err != nil {
			log.Errorf("Get utc time error %v", err)
			continue
		}
		nowTime := now.In(utcLocation)
		p.clientLock.RLock()
		sleepTime = p.nextExpire.Sub(nowTime)
		p.clientLock.RUnlock()
		log.Infof("Distance expiration time %v", sleepTime)
		if sleepTime < 10*time.Minute {
			sleepTime = time.Second * 1
		} else {
			sleepTime = 9 * time.Minute
			log.Info("Next fetch sts sleep interval : ", sleepTime.String())
			continue
		}
		cfg, err := getCloudConfigFromStsToken()
		if err != nil {
			log.Errorf("Failed to getCloudConfigFromStsToken: %v", err)
			continue
		}
		dnsClient, err := alidns.NewClientWithStsToken(
			cfg.RegionID,
			cfg.AccessKeyID,
			cfg.AccessKeySecret,
			cfg.StsToken,
		)
		if err != nil {
			log.Errorf("Failed to new client with sts token %v", err)
			continue
		}
		pvtzClient, err := pvtz.NewClientWithStsToken(
			cfg.RegionID,
			cfg.AccessKeyID,
			cfg.AccessKeySecret,
			cfg.StsToken,
		)
		if err != nil {
			log.Errorf("Failed to new client with sts token %v", err)
			continue
		}
		log.Infof("Refresh client from sts token, next expire time %v", cfg.ExpireTime)
		p.clientLock.Lock()
		p.dnsClient = dnsClient
		p.pvtzClient = pvtzClient
		p.nextExpire = cfg.ExpireTime
		p.clientLock.Unlock()
	}
}

// Records gets the current records.
//
// Returns the current records or an error if the operation failed.
func (p *AlibabaCloudProvider) Records(ctx context.Context) (endpoints []*endpoint.Endpoint, err error) {
	if p.privateZone {
		endpoints, err = p.privateZoneRecords()
	} else {
		endpoints, err = p.recordsForDNS()
	}
	return endpoints, err
}

// ApplyChanges applies the given changes.
//
// Returns nil if the operation was successful or an error if the operation failed.
func (p *AlibabaCloudProvider) ApplyChanges(ctx context.Context, changes *plan.Changes) error {
	if changes == nil || len(changes.Create)+len(changes.Delete)+len(changes.UpdateNew) == 0 {
		// No op
		return nil
	}

	if p.privateZone {
		return p.applyChangesForPrivateZone(changes)
	}
	return p.applyChangesForDNS(changes)
}

func (p *AlibabaCloudProvider) getDNSName(rr, domain string) string {
	if rr == nullHostAlibabaCloud {
		return domain
	}
	return rr + "." + domain
}

// recordsForDNS gets the current records.
//
// Returns the current records or an error if the operation failed.
func (p *AlibabaCloudProvider) recordsForDNS() (endpoints []*endpoint.Endpoint, _ error) {
	records, err := p.records()
	if err != nil {
		return nil, err
	}
	for _, recordList := range p.groupRecords(records) {
		name := p.getDNSName(recordList[0].RR, recordList[0].DomainName)
		recordType := recordList[0].Type
		ttl := recordList[0].TTL

		var targets []string
		for _, record := range recordList {
			target := record.Value
			if recordType == "TXT" {
				target = p.unescapeTXTRecordValue(target)
			}
			targets = append(targets, target)
		}
		ep := endpoint.NewEndpointWithTTL(name, recordType, endpoint.TTL(ttl), targets...)
		endpoints = append(endpoints, ep)
	}
	return endpoints, nil
}

func getNextPageNumber(pageNumber, pageSize, totalCount int64) int64 {
	if pageNumber*pageSize >= totalCount {
		return 0
	}
	return pageNumber + 1
}

func (p *AlibabaCloudProvider) getRecordKey(record alidns.Record) string {
	if record.RR == nullHostAlibabaCloud {
		return record.Type + ":" + record.DomainName
	}
	return record.Type + ":" + record.RR + "." + record.DomainName
}

func (p *AlibabaCloudProvider) getRecordKeyByEndpoint(endpoint *endpoint.Endpoint) string {
	return endpoint.RecordType + ":" + endpoint.DNSName
}

func (p *AlibabaCloudProvider) groupRecords(records []alidns.Record) (endpointMap map[string][]alidns.Record) {
	endpointMap = make(map[string][]alidns.Record)
	for _, record := range records {
		key := p.getRecordKey(record)

		recordList := endpointMap[key]
		endpointMap[key] = append(recordList, record)
	}
	return endpointMap
}

func (p *AlibabaCloudProvider) records() ([]alidns.Record, error) {
	log.Infof("Retrieving Alibaba Cloud DNS Domain Records")
	var results []alidns.Record

	if len(p.domainFilter.Filters) == 1 && p.domainFilter.Filters[0] == "" {
		domainNames, tmpErr := p.getDomainList()
		if tmpErr != nil {
			log.Errorf("AlibabaCloudProvider getDomainList error %v", tmpErr)
			return results, tmpErr
		}
		for _, tmpDomainName := range domainNames {
			tmpResults, err := p.getDomainRecords(tmpDomainName)
			if err != nil {
				log.Errorf("AlibabaCloudProvider getDomainRecords %s error %v", tmpDomainName, err)
				continue
			}
			results = append(results, tmpResults...)
		}
	} else {
		for _, domainName := range p.domainFilter.Filters {
			tmpResults, err := p.getDomainRecords(domainName)
			if err != nil {
				log.Errorf("getDomainRecords %s error %v", domainName, err)
				continue
			}
			results = append(results, tmpResults...)
		}
	}
	log.Infof("Found %d Alibaba Cloud DNS record(s).", len(results))
	return results, nil
}

func (p *AlibabaCloudProvider) getDomainList() ([]string, error) {
	var domainNames []string
	request := alidns.CreateDescribeDomainsRequest()
	request.PageSize = requests.NewInteger(defaultAlibabaCloudPageSize)
	request.PageNumber = "1"
	for {
		resp, err := p.dnsClient.DescribeDomains(request)
		if err != nil {
			log.Errorf("Failed to describe domains for Alibaba Cloud DNS: %v", err)
			return nil, err
		}
		for _, tmpDomain := range resp.Domains.Domain {
			domainNames = append(domainNames, tmpDomain.DomainName)
		}
		nextPage := getNextPageNumber(resp.PageNumber, defaultAlibabaCloudPageSize, resp.TotalCount)
		if nextPage == 0 {
			break
		} else {
			request.PageNumber = requests.NewInteger64(nextPage)
		}
	}
	return domainNames, nil
}

func (p *AlibabaCloudProvider) getDomainRecords(domainName string) ([]alidns.Record, error) {
	var results []alidns.Record
	request := alidns.CreateDescribeDomainRecordsRequest()
	request.DomainName = domainName
	request.PageSize = requests.NewInteger(defaultAlibabaCloudPageSize)
	request.PageNumber = "1"
	for {
		response, err := p.getDNSClient().DescribeDomainRecords(request)

		if err != nil {
			log.Errorf("Failed to describe domain records for Alibaba Cloud DNS: %v", err)
			return nil, err
		}

		for _, record := range response.DomainRecords.Record {
			domainName := record.DomainName
			recordType := record.Type

			if !p.domainFilter.Match(domainName) {
				continue
			}
			if !endpoint.SupportedRecordType(recordType) {
				continue
			}
			//TODO filter Locked record
			results = append(results, record)
		}
		nextPage := getNextPageNumber(response.PageNumber, defaultAlibabaCloudPageSize, response.TotalCount)
		if nextPage == 0 {
			break
		} else {
			request.PageNumber = requests.NewInteger64(nextPage)
		}
	}

	return results, nil
}

func (p *AlibabaCloudProvider) applyChangesForDNS(changes *plan.Changes) error {
	log.Infof("ApplyChanges to Alibaba Cloud DNS: %++v", *changes)

	records, err := p.records()
	if err != nil {
		return err
	}

	recordMap := p.groupRecords(records)

	p.createRecords(changes.Create)
	p.deleteRecords(recordMap, changes.Delete)
	p.updateRecords(recordMap, changes.UpdateNew)
	return nil
}

func (p *AlibabaCloudProvider) escapeTXTRecordValue(value string) string {
	// For unsupported chars
	return value
}

func (p *AlibabaCloudProvider) unescapeTXTRecordValue(value string) string {
	if strings.HasPrefix(value, "heritage=") {
		return fmt.Sprintf("\"%s\"", strings.Replace(value, ";", ",", -1))
	}
	return value
}

func (p *AlibabaCloudProvider) createRecord(endpoint *endpoint.Endpoint, target string) error {
	rr, domain := p.splitDNSName(endpoint)
	request := alidns.CreateAddDomainRecordRequest()
	request.DomainName = domain
	request.Type = endpoint.RecordType
	request.RR = rr

	ttl := int(endpoint.RecordTTL)
	if ttl != 0 {
		request.TTL = requests.NewInteger(ttl)
	}

	if endpoint.RecordType == "TXT" {
		target = p.escapeTXTRecordValue(target)
	}

	request.Value = target

	if p.dryRun {
		log.Infof("Dry run: Create %s record named '%s' to '%s' with ttl %d for Alibaba Cloud DNS", endpoint.RecordType, endpoint.DNSName, target, ttl)
		return nil
	}

	response, err := p.getDNSClient().AddDomainRecord(request)
	if err == nil {
		log.Infof("Create %s record named '%s' to '%s' with ttl %d for Alibaba Cloud DNS: Record ID=%s", endpoint.RecordType, endpoint.DNSName, target, ttl, response.RecordId)
	} else {
		log.Errorf("Failed to create %s record named '%s' to '%s' with ttl %d for Alibaba Cloud DNS: %v", endpoint.RecordType, endpoint.DNSName, target, ttl, err)
	}
	return err
}

func (p *AlibabaCloudProvider) createRecords(endpoints []*endpoint.Endpoint) error {
	for _, endpoint := range endpoints {
		for _, target := range endpoint.Targets {
			p.createRecord(endpoint, target)
		}
	}
	return nil
}

func (p *AlibabaCloudProvider) deleteRecord(recordID string) error {
	if p.dryRun {
		log.Infof("Dry run: Delete record id '%s' in Alibaba Cloud DNS", recordID)
		return nil
	}

	request := alidns.CreateDeleteDomainRecordRequest()
	request.RecordId = recordID
	response, err := p.getDNSClient().DeleteDomainRecord(request)
	if err == nil {
		log.Infof("Delete record id %s in Alibaba Cloud DNS", response.RecordId)
	} else {
		log.Errorf("Failed to delete record '%s' in Alibaba Cloud DNS: %v", response.RecordId, err)
	}
	return err
}

func (p *AlibabaCloudProvider) updateRecord(record alidns.Record, endpoint *endpoint.Endpoint) error {
	request := alidns.CreateUpdateDomainRecordRequest()
	request.RecordId = record.RecordId
	request.RR = record.RR
	request.Type = record.Type
	request.Value = record.Value
	ttl := int(endpoint.RecordTTL)
	if ttl != 0 {
		request.TTL = requests.NewInteger(ttl)
	}
	response, err := p.getDNSClient().UpdateDomainRecord(request)
	if err == nil {
		log.Infof("Update record id '%s' in Alibaba Cloud DNS", response.RecordId)
	} else {
		log.Errorf("Failed to update record '%s' in Alibaba Cloud DNS: %v", response.RecordId, err)
	}
	return err
}

func (p *AlibabaCloudProvider) deleteRecords(recordMap map[string][]alidns.Record, endpoints []*endpoint.Endpoint) error {
	for _, endpoint := range endpoints {
		key := p.getRecordKeyByEndpoint(endpoint)
		records := recordMap[key]
		found := false
		for _, record := range records {
			value := record.Value
			if record.Type == "TXT" {
				value = p.unescapeTXTRecordValue(value)
			}

			for _, target := range endpoint.Targets {
				// Find matched record to delete
				if value == target {
					p.deleteRecord(record.RecordId)
					found = true
					break
				}
			}
		}
		if !found {
			log.Errorf("Failed to find %s record named '%s' to delete for Alibaba Cloud DNS", endpoint.RecordType, endpoint.DNSName)
		}
	}
	return nil
}

func (p *AlibabaCloudProvider) equals(record alidns.Record, endpoint *endpoint.Endpoint) bool {
	ttl1 := record.TTL
	if ttl1 == defaultAlibabaCloudRecordTTL {
		ttl1 = 0
	}

	ttl2 := int64(endpoint.RecordTTL)
	if ttl2 == defaultAlibabaCloudRecordTTL {
		ttl2 = 0
	}

	return ttl1 == ttl2
}

func (p *AlibabaCloudProvider) updateRecords(recordMap map[string][]alidns.Record, endpoints []*endpoint.Endpoint) error {
	for _, endpoint := range endpoints {
		key := p.getRecordKeyByEndpoint(endpoint)
		records := recordMap[key]
		for _, record := range records {
			value := record.Value
			if record.Type == "TXT" {
				value = p.unescapeTXTRecordValue(value)
			}
			found := false
			for _, target := range endpoint.Targets {
				// Find matched record to delete
				if value == target {
					found = true
				}
			}
			if found {
				if !p.equals(record, endpoint) {
					// Update record
					p.updateRecord(record, endpoint)
				}
			} else {
				p.deleteRecord(record.RecordId)
			}
		}
		for _, target := range endpoint.Targets {
			if endpoint.RecordType == "TXT" {
				target = p.escapeTXTRecordValue(target)
			}
			found := false
			for _, record := range records {
				// Find matched record to delete
				if record.Value == target {
					found = true
				}
			}
			if !found {
				p.createRecord(endpoint, target)
			}
		}
	}
	return nil
}

func (p *AlibabaCloudProvider) splitDNSName(endpoint *endpoint.Endpoint) (rr string, domain string) {
	name := strings.TrimSuffix(endpoint.DNSName, ".")

	found := false

	for _, filter := range p.domainFilter.Filters {
		if strings.HasSuffix(name, "."+filter) {
			rr = name[0 : len(name)-len(filter)-1]
			domain = filter
			found = true
			break
		} else if name == filter {
			domain = filter
			rr = ""
			found = true
		}
	}

	if !found {
		parts := strings.Split(name, ".")
		if len(parts) < 2 {
			rr = name
			domain = ""
		} else {
			domain = parts[len(parts)-2] + "." + parts[len(parts)-1]
			rrIndex := strings.Index(name, domain)
			if rrIndex < 1 {
				rrIndex = 1
			}
			rr = name[0 : rrIndex-1]
		}
	}

	if rr == "" {
		rr = nullHostAlibabaCloud
	}

	return rr, domain
}

func (p *AlibabaCloudProvider) matchVPC(zoneID string) bool {
	request := pvtz.CreateDescribeZoneInfoRequest()
	request.ZoneId = zoneID
	request.Domain = pVTZDoamin
	response, err := p.getPvtzClient().DescribeZoneInfo(request)
	if err != nil {
		log.Errorf("Failed to describe zone info %s in Alibaba Cloud DNS: %v", zoneID, err)
		return false
	}
	foundVPC := false
	for _, vpc := range response.BindVpcs.Vpc {
		if vpc.VpcId == p.vpcID {
			foundVPC = true
			break
		}
	}
	return foundVPC
}

func (p *AlibabaCloudProvider) privateZones() ([]pvtz.Zone, error) {
	var zones []pvtz.Zone

	request := pvtz.CreateDescribeZonesRequest()
	request.PageSize = requests.NewInteger(defaultAlibabaCloudPageSize)
	request.PageNumber = "1"
	request.Domain = pVTZDoamin
	for {
		response, err := p.getPvtzClient().DescribeZones(request)
		if err != nil {
			log.Errorf("Failed to describe zones in Alibaba Cloud DNS: %v", err)
			return nil, err
		}
		for _, zone := range response.Zones.Zone {
			log.Infof("PrivateZones zone: %++v", zone)

			if !p.zoneIDFilter.Match(zone.ZoneId) {
				continue
			}
			if !p.domainFilter.Match(zone.ZoneName) {
				continue
			}
			if !p.matchVPC(zone.ZoneId) {
				continue
			}
			zones = append(zones, zone)
		}
		nextPage := getNextPageNumber(int64(response.PageNumber), defaultAlibabaCloudPageSize, int64(response.TotalItems))
		if nextPage == 0 {
			break
		} else {
			request.PageNumber = requests.NewInteger64(nextPage)
		}
	}
	return zones, nil
}

type alibabaPrivateZone struct {
	pvtz.Zone
	records []pvtz.Record
}

func (p *AlibabaCloudProvider) getPrivateZones() (map[string]*alibabaPrivateZone, error) {
	log.Infof("Retrieving Alibaba Cloud Private Zone records")

	result := make(map[string]*alibabaPrivateZone)
	recordsCount := 0

	zones, err := p.privateZones()

	if err != nil {
		return nil, err
	}

	for _, zone := range zones {
		request := pvtz.CreateDescribeZoneRecordsRequest()
		request.ZoneId = zone.ZoneId
		request.PageSize = requests.NewInteger(defaultAlibabaCloudPageSize)
		request.PageNumber = "1"
		request.Domain = pVTZDoamin
		var records []pvtz.Record

		for {
			response, err := p.getPvtzClient().DescribeZoneRecords(request)

			if err != nil {
				log.Errorf("Failed to describe zone record '%s' in Alibaba Cloud DNS: %v", zone.ZoneId, err)
				return nil, err
			}

			for _, record := range response.Records.Record {
				recordType := record.Type

				if !endpoint.SupportedRecordType(recordType) {
					continue
				}

				//TODO filter Locked
				records = append(records, record)
			}
			nextPage := getNextPageNumber(int64(response.PageNumber), defaultAlibabaCloudPageSize, int64(response.TotalItems))
			if nextPage == 0 {
				break
			} else {
				request.PageNumber = requests.NewInteger64(nextPage)
			}
		}

		privateZone := alibabaPrivateZone{
			Zone:    zone,
			records: records,
		}
		recordsCount += len(records)
		result[zone.ZoneName] = &privateZone
	}
	log.Infof("Found %d Alibaba Cloud Private Zone record(s).", recordsCount)
	return result, nil
}

func (p *AlibabaCloudProvider) groupPrivateZoneRecords(zone *alibabaPrivateZone) (endpointMap map[string][]pvtz.Record) {
	endpointMap = make(map[string][]pvtz.Record)

	for _, record := range zone.records {
		key := record.Type + ":" + record.Rr
		recordList := endpointMap[key]
		endpointMap[key] = append(recordList, record)
	}

	return endpointMap
}

// recordsForPrivateZone gets the current records.
//
// Returns the current records or an error if the operation failed.
func (p *AlibabaCloudProvider) privateZoneRecords() (endpoints []*endpoint.Endpoint, _ error) {
	zones, err := p.getPrivateZones()
	if err != nil {
		return nil, err
	}

	for _, zone := range zones {
		recordMap := p.groupPrivateZoneRecords(zone)
		for _, recordList := range recordMap {
			name := p.getDNSName(recordList[0].Rr, zone.ZoneName)
			recordType := recordList[0].Type
			ttl := recordList[0].Ttl
			if ttl == defaultAlibabaCloudPrivateZoneRecordTTL {
				ttl = 0
			}
			var targets []string
			for _, record := range recordList {
				target := record.Value
				if recordType == "TXT" {
					target = p.unescapeTXTRecordValue(target)
				}
				targets = append(targets, target)
			}
			ep := endpoint.NewEndpointWithTTL(name, recordType, endpoint.TTL(ttl), targets...)
			endpoints = append(endpoints, ep)
		}
	}
	return endpoints, nil
}

func (p *AlibabaCloudProvider) createPrivateZoneRecord(zones map[string]*alibabaPrivateZone, endpoint *endpoint.Endpoint, target string) error {
	rr, domain := p.splitDNSName(endpoint)
	zone := zones[domain]
	if zone == nil {
		err := fmt.Errorf("failed to find private zone '%s'", domain)
		log.Errorf("Failed to create %s record named '%s' to '%s' for Alibaba Cloud Private Zone: %v", endpoint.RecordType, endpoint.DNSName, target, err)
		return err
	}

	request := pvtz.CreateAddZoneRecordRequest()
	request.ZoneId = zone.ZoneId
	request.Type = endpoint.RecordType
	request.Rr = rr
	request.Domain = pVTZDoamin

	ttl := int(endpoint.RecordTTL)
	if ttl != 0 {
		request.Ttl = requests.NewInteger(ttl)
	}

	if endpoint.RecordType == "TXT" {
		target = p.escapeTXTRecordValue(target)
	}

	request.Value = target

	if p.dryRun {
		log.Infof("Dry run: Create %s record named '%s' to '%s' with ttl %d for Alibaba Cloud Private Zone", endpoint.RecordType, endpoint.DNSName, target, ttl)
		return nil
	}

	response, err := p.getPvtzClient().AddZoneRecord(request)
	if err == nil {
		log.Infof("Create %s record named '%s' to '%s' with ttl %d for Alibaba Cloud Private Zone: Record ID=%d", endpoint.RecordType, endpoint.DNSName, target, ttl, response.RecordId)
	} else {
		log.Errorf("Failed to create %s record named '%s' to '%s' with ttl %d for Alibaba Cloud Private Zone: %v", endpoint.RecordType, endpoint.DNSName, target, ttl, err)
	}
	return err
}

func (p *AlibabaCloudProvider) createPrivateZoneRecords(zones map[string]*alibabaPrivateZone, endpoints []*endpoint.Endpoint) error {
	for _, endpoint := range endpoints {
		for _, target := range endpoint.Targets {
			p.createPrivateZoneRecord(zones, endpoint, target)
		}
	}
	return nil
}

func (p *AlibabaCloudProvider) deletePrivateZoneRecord(recordID int64) error {
	if p.dryRun {
		log.Infof("Dry run: Delete record id '%d' in Alibaba Cloud Private Zone", recordID)
	}

	request := pvtz.CreateDeleteZoneRecordRequest()
	request.RecordId = requests.NewInteger64(recordID)
	request.Domain = pVTZDoamin

	response, err := p.getPvtzClient().DeleteZoneRecord(request)
	if err == nil {
		log.Infof("Delete record id '%d' in Alibaba Cloud Private Zone", response.RecordId)
	} else {
		log.Errorf("Failed to delete record %d in Alibaba Cloud Private Zone: %v", response.RecordId, err)
	}
	return err
}

func (p *AlibabaCloudProvider) deletePrivateZoneRecords(zones map[string]*alibabaPrivateZone, endpoints []*endpoint.Endpoint) error {
	for _, endpoint := range endpoints {
		rr, domain := p.splitDNSName(endpoint)

		zone := zones[domain]
		if zone == nil {
			err := fmt.Errorf("failed to find private zone '%s'", domain)
			log.Errorf("Failed to delete %s record named '%s' for Alibaba Cloud Private Zone: %v", endpoint.RecordType, endpoint.DNSName, err)
			continue
		}
		found := false
		for _, record := range zone.records {
			if rr == record.Rr && endpoint.RecordType == record.Type {
				value := record.Value
				if record.Type == "TXT" {
					value = p.unescapeTXTRecordValue(value)
				}
				for _, target := range endpoint.Targets {
					// Find matched record to delete
					if value == target {
						p.deletePrivateZoneRecord(record.RecordId)
						found = true
						break
					}
				}
			}
		}
		if !found {
			log.Errorf("Failed to find %s record named '%s' to delete for Alibaba Cloud Private Zone", endpoint.RecordType, endpoint.DNSName)
		}
	}
	return nil
}

// ApplyChanges applies the given changes.
//
// Returns nil if the operation was successful or an error if the operation failed.
func (p *AlibabaCloudProvider) applyChangesForPrivateZone(changes *plan.Changes) error {
	log.Infof("ApplyChanges to Alibaba Cloud Private Zone: %++v", *changes)

	zones, err := p.getPrivateZones()
	if err != nil {
		return err
	}

	for zoneName, zone := range zones {
		log.Debugf("%s: %++v", zoneName, zone)
	}

	p.createPrivateZoneRecords(zones, changes.Create)
	p.deletePrivateZoneRecords(zones, changes.Delete)
	p.updatePrivateZoneRecords(zones, changes.UpdateNew)
	return nil
}

func (p *AlibabaCloudProvider) updatePrivateZoneRecord(record pvtz.Record, endpoint *endpoint.Endpoint) error {
	request := pvtz.CreateUpdateZoneRecordRequest()
	request.RecordId = requests.NewInteger64(record.RecordId)
	request.Rr = record.Rr
	request.Type = record.Type
	request.Value = record.Value
	request.Domain = pVTZDoamin
	ttl := int(endpoint.RecordTTL)
	if ttl != 0 {
		request.Ttl = requests.NewInteger(ttl)
	}
	response, err := p.getPvtzClient().UpdateZoneRecord(request)
	if err == nil {
		log.Infof("Update record id '%d' in Alibaba Cloud Private Zone", response.RecordId)
	} else {
		log.Errorf("Failed to update record '%d' in Alibaba Cloud Private Zone: %v", response.RecordId, err)
	}
	return err
}

func (p *AlibabaCloudProvider) equalsPrivateZone(record pvtz.Record, endpoint *endpoint.Endpoint) bool {
	ttl1 := record.Ttl
	if ttl1 == defaultAlibabaCloudPrivateZoneRecordTTL {
		ttl1 = 0
	}

	ttl2 := int(endpoint.RecordTTL)
	if ttl2 == defaultAlibabaCloudPrivateZoneRecordTTL {
		ttl2 = 0
	}

	return ttl1 == ttl2
}

func (p *AlibabaCloudProvider) updatePrivateZoneRecords(zones map[string]*alibabaPrivateZone, endpoints []*endpoint.Endpoint) error {
	for _, endpoint := range endpoints {
		rr, domain := p.splitDNSName(endpoint)
		zone := zones[domain]
		if zone == nil {
			err := fmt.Errorf("failed to find private zone '%s'", domain)
			log.Errorf("Failed to update %s record named '%s' for Alibaba Cloud Private Zone: %v", endpoint.RecordType, endpoint.DNSName, err)
			continue
		}

		for _, record := range zone.records {
			if record.Rr != rr || record.Type != endpoint.RecordType {
				continue
			}
			value := record.Value
			if record.Type == "TXT" {
				value = p.unescapeTXTRecordValue(value)
			}
			found := false
			for _, target := range endpoint.Targets {
				// Find matched record to delete
				if value == target {
					found = true
					break
				}
			}
			if found {
				if !p.equalsPrivateZone(record, endpoint) {
					// Update record
					p.updatePrivateZoneRecord(record, endpoint)
				}
			} else {
				p.deletePrivateZoneRecord(record.RecordId)
			}
		}
		for _, target := range endpoint.Targets {
			if endpoint.RecordType == "TXT" {
				target = p.escapeTXTRecordValue(target)
			}
			found := false
			for _, record := range zone.records {
				if record.Rr != rr || record.Type != endpoint.RecordType {
					continue
				}
				// Find matched record to delete
				if record.Value == target {
					found = true
					break
				}
			}
			if !found {
				p.createPrivateZoneRecord(zones, endpoint, target)
			}
		}
	}
	return nil
}
