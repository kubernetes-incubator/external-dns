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

package ibmcis

import (
	"context"
	"errors"
	"os"
	"strings"

	log "github.com/sirupsen/logrus"

	cis "github.com/IBM-Cloud/bluemix-go/api/cis/cisv1"
	session "github.com/IBM-Cloud/bluemix-go/session"

	"sigs.k8s.io/external-dns/endpoint"
	"sigs.k8s.io/external-dns/plan"
	"sigs.k8s.io/external-dns/provider"
)

var (
	// ErrZoneAlreadyExists error returned when zone cannot be created when it already exists
	ErrZoneAlreadyExists = errors.New("specified zone already exists")
	// ErrZoneNotFound error returned when specified zone does not exists
	ErrZoneNotFound = errors.New("specified zone not found")
	// ErrRecordAlreadyExists when create request is sent but record already exists
	ErrRecordAlreadyExists = errors.New("record already exists")
	// ErrRecordNotFound when update/delete request is sent but record not found
	ErrRecordNotFound = errors.New("record not found")
	// ErrDuplicateRecordFound when record is repeated in create/update/delete
	ErrDuplicateRecordFound = errors.New("invalid batch request")
)

// IbmCisProvider - dns provider for IBM Cloud
// using Cloud Internet Services API and services
type IbmCisProvider struct {
	provider.BaseProvider
	domain      endpoint.DomainFilter
	filter      *filter
	ibmCisAPI   cis.CisServiceAPI
	dryrun      bool
	icCisCrn    string
	ibmdnszones ibmDNSZones
}

// ibmDnsZones - dns zones map for IBM Cloud
// using Cloud Internet Services API and services
// Primary reason for this is to be able to lookup CRN and IDs for zones and DNS records
type ibmDNSZones struct {
	zones map[string]*ibmDNSZone
}

// ibmDnsZone - dns zone mapper for IBM Cloud
// using Cloud Internet Services API and services
type ibmDNSZone struct {
	ID         string
	Name       string
	CisCRN     string
	dnsRecords map[string]*ibmDNSRecord
}

type ibmDNSRecord struct {
	Type          string
	SetIdentifier string
	Name          string
	Target        string
	Labels        endpoint.Labels
	ID            string
}

// NewIbmCisProvider - the method used by external-dns to create the provider instance
// inspiration: https://github.com/IBM-Cloud/bluemix-go/blob/master/examples/cis/cisv1/dns/main.go
func NewIbmCisProvider(domainfilter endpoint.DomainFilter, dryrun bool) *IbmCisProvider {
	log.Info("ibmcis provider activated.")

	// For now remember that login to IBM cloud works because ENV variable IC_API_KEY needs to be set!
	if os.Getenv("IC_API_KEY") == "" {
		log.Error("failed to initialize ibmdns provider. Please set IC_API_KEY to a valid API key")
		return nil
	}

	if os.Getenv("IC_CIS_INSTANCE_CRN") == "" {
		log.Fatal("failed to initialize ibmdns provider. Please set IC_CIS_INSTANCE_CRN to a valid CRN for a Cloud Internet Service (CIS) instance")
		return nil
	}
	icCisCrn := os.Getenv("IC_CIS_INSTANCE_CRN")

	if !dryrun {
		ibmSession, ibmErr := session.New()

		if ibmErr != nil {
			log.Fatalf("IBM Cloud session failed: %s", ibmErr)
		}

		ibmCisAPI, ibmErr := cis.New(ibmSession)
		if ibmErr != nil {
			log.Fatal(ibmErr)
		}

		im := &IbmCisProvider{
			filter:    &filter{},
			domain:    endpoint.NewDomainFilter([]string{""}),
			ibmCisAPI: ibmCisAPI,
			dryrun:    dryrun,
			icCisCrn:  icCisCrn,
		}
		return im
	}
	im := &IbmCisProvider{
		filter:    &filter{},
		domain:    endpoint.NewDomainFilter([]string{""}),
		ibmCisAPI: nil,
		dryrun:    dryrun,
		icCisCrn:  icCisCrn,
	}
	return im
}

// Records returns the list of endpoints
func (im *IbmCisProvider) Records(ctx context.Context) ([]*endpoint.Endpoint, error) {
	log.Debug("Running on Records (ibmdns)")
	endpoints := make([]*endpoint.Endpoint, 0)

	ibmdnszones := make(map[string]*ibmDNSZone)

	if im.dryrun {
		log.Debug("Records run in dryrun mode")
		return endpoints, nil
	}

	zonesAPI := im.ibmCisAPI.Zones()

	// Lets loop through the CRNs listed, if more than one is provided they should be comma-separated (,)
	for i, crn := range strings.Split(im.icCisCrn, ",") {
		log.Debugf("CRN %d - %s", i, crn)

		myZones, ibmErr := zonesAPI.ListZones(crn)

		if ibmErr != nil {
			log.Fatal(ibmErr)
		}

		dnsAPI := im.ibmCisAPI.Dns()

		for _, z := range myZones {
			log.Debugf("Loading zone id '%s' name '%s'", z.Id, z.Name)
			// Todo check if zone is filtered out or not - skip if it is

			// Also lets just be sure  no zone-overlaps exist between multiple CRNS (like defining the same twice in the CRN list)
			if _, ok := ibmdnszones[z.Name]; ok {
				log.Warningf("Skipping CRN-zone pair as it already exists, check your config: '%s'-'%s'", crn, z.Name)
				continue
			}

			ibmdnszones[z.Name] = &ibmDNSZone{
				ID:         z.Id,
				Name:       z.Name,
				CisCRN:     crn,
				dnsRecords: make(map[string]*ibmDNSRecord),
			}

			myDNS, ibmErr := dnsAPI.ListDns(crn, z.Id)

			if ibmErr != nil {
				log.Fatal(ibmErr)
			}

			for i, dnsRec := range myDNS {
				log.Debugf(" Found DNS record (%d - %s) '%s' type '%s' content '%s'", i, dnsRec.Id, dnsRec.Name, dnsRec.DnsType, dnsRec.Content)

				ibmDNSRecord := &ibmDNSRecord{
					Type:          dnsRec.DnsType,
					Name:          dnsRec.Name,
					Target:        dnsRec.Content,
					SetIdentifier: "",
					Labels:        nil,
					ID:            dnsRec.Id,
				}

				// dnsKey is constructed by combination of name and type to ensure it is unique.
				// As ## is not allowed in neither name or type this should be a safe way to make it unique
				dnsKey := dnsRec.Name + "##" + dnsRec.DnsType
				if _, ok := ibmdnszones[z.Name].dnsRecords[dnsKey]; !ok {
					ibmdnszones[z.Name].dnsRecords[dnsKey] = ibmDNSRecord
				} else {
					log.Debugf("record exists - it will be ignored: %s", dnsKey)
				}
			}
		}
	}
	for zonename, zoneinfo := range ibmdnszones {
		log.Debugf("   Zone: %s", zonename)

		for recordName, record := range zoneinfo.dnsRecords {
			log.Debugf("     Record: %s", recordName)
			ep := endpoint.NewEndpoint(record.Name, record.Type, record.Target).WithSetIdentifier(record.SetIdentifier)
			ep.Labels = record.Labels
			endpoints = append(endpoints, ep)
		}
	}

	im.ibmdnszones.zones = ibmdnszones

	log.Debug("Leaving Records (ibmdns)")

	return endpoints, nil
}

// ApplyChanges modifies DNS records in IBM Cloud Internet Service
// create record - record should not exist
// update/delete record - record should exist
// create/update/delete lists should not have overlapping records
func (im *IbmCisProvider) ApplyChanges(ctx context.Context, changes *plan.Changes) error {
	log.Debug("Running on ApplyChanges (ibmdns)")

	if im.dryrun {
		log.Info("DRY RUN MODE (ibmdns) -not implemented yet!")
		return nil
	}

	zones := im.ibmdnszones.zones

	dnsAPI := im.ibmCisAPI.Dns()

	for _, ep := range changes.Create {
		log.Infof("ApplyChange - CreateDns(%s)", ep)

		zoneciscrn, _, zoneid := im.filter.EndpointIBMZoneID(ep, zones)
		if zoneid == "" {
			log.Warnf("Ignoring request for '%s'. The domain is not handled by provided CRNs. Suggest to utilize --domain-filter or --exclude-domains to avoid this warning.", ep.DNSName)
			continue
		}

		_, ibmErr := dnsAPI.CreateDns(zoneciscrn, zoneid, cis.DnsBody{
			Name:    ep.DNSName,
			DnsType: ep.RecordType,
			Content: ep.Targets[0],
		})

		if ibmErr != nil {
			log.Error(ibmErr)
		}
	}
	for _, ep := range changes.UpdateNew {
		log.Infof("ApplyChange - UpdateDns (NEW) (%s)", ep)

		zoneciscrn, _, zoneid, dnsid := im.filter.EndpointIBMDnsID(ep, zones)
		if zoneid == "" {
			log.Warnf("Ignoring request for '%s'. The domain is not handled by provided CRNs. Suggest to utilize --domain-filter or --exclude-domains to avoid this warning.", ep.DNSName)
			continue
		}
		log.Debugf("UpdateDNS (OLD) - found record %s - %s", zoneciscrn, dnsid)
		// ibmErr := dnsAPI.UpdateDns(zoneciscrn, zoneid, dnsid)

		// if ibmErr != nil {
		// 	log.Error(ibmErr)
		// }
	}
	for _, ep := range changes.UpdateOld {
		log.Infof("ApplyChange - UpdateDns (OLD) (%s)", ep)

		zoneciscrn, _, zoneid, dnsid := im.filter.EndpointIBMDnsID(ep, zones)
		if zoneid == "" {
			log.Warnf("Ignoring request for '%s'. The domain is not handled by provided CRNs. Suggest to utilize --domain-filter or --exclude-domains to avoid this warning.", ep.DNSName)
			continue
		}
		log.Debugf("UpdateDNS (OLD) - found record %s - %s", zoneciscrn, dnsid)
		// ibmErr := dnsAPI.UpdateDns(zoneciscrn, zoneid, dnsid)

		// if ibmErr != nil {
		// 	log.Error(ibmErr)
		// }
	}
	for _, ep := range changes.Delete {
		log.Infof("ApplyChange - DeleteDns(%s)", ep)

		zoneciscrn, _, zoneid, dnsid := im.filter.EndpointIBMDnsID(ep, zones)
		if zoneid == "" {
			log.Warnf("Ignoring request for '%s'. The domain is not handled by provided CRNs. Suggest to utilize --domain-filter or --exclude-domains to avoid this warning.", ep.DNSName)
			continue
		}

		ibmErr := dnsAPI.DeleteDns(zoneciscrn, zoneid, dnsid)

		if ibmErr != nil {
			log.Error(ibmErr)
		}
	}

	log.Debug("Ending ApplyChanges (ibmdns)")

	return nil
}

func (f *filter) EndpointIBMZoneID(endpoint *endpoint.Endpoint, zones map[string]*ibmDNSZone) (zonecrn, zonename, zoneid string) {
	var matchCRN, matchZoneID, matchZoneName string
	for zonename, zone := range zones {
		if strings.HasSuffix(endpoint.DNSName, zone.Name) && len(zone.Name) > len(matchZoneName) {
			matchZoneName = zonename
			matchZoneID = zone.ID
			matchCRN = zone.CisCRN
		}
	}
	return matchCRN, matchZoneName, matchZoneID
}

func (f *filter) EndpointIBMDnsID(endpoint *endpoint.Endpoint, zones map[string]*ibmDNSZone) (zonecrn, zonename, zoneid, dnsid string) {
	var matchCRN, matchZoneID, matchZoneName, matchDNSID string
	for zonename, zone := range zones {
		if strings.HasSuffix(endpoint.DNSName, zone.Name) && len(zone.Name) > len(matchZoneName) {
			matchZoneName = zonename
			matchZoneID = zone.ID
			matchCRN = zone.CisCRN

			for _, dns := range zone.dnsRecords {
				if (endpoint.DNSName == dns.Name) && (endpoint.RecordType == dns.Type) {
					log.Debugf("EndpointIBMDnsID found record id %s for ep (%s) matching (%s)", dns.ID, endpoint, dns)
					matchDNSID = dns.ID
				}
			}
		}
	}

	return matchCRN, matchZoneName, matchZoneID, matchDNSID
}

type filter struct {
	//	domain string
}
