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

package vercel

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"net/http"
	"os"
	"strings"

	"sigs.k8s.io/external-dns/endpoint"
	"sigs.k8s.io/external-dns/plan"
	"sigs.k8s.io/external-dns/provider"
)

type VercelProvider struct {
	provider.BaseProvider
	accessToken  string
	domainFilter endpoint.DomainFilter
	httpClient   *http.Client
}

func NewVercelProvider(domainFilter endpoint.DomainFilter, dryRun bool) (*VercelProvider, error) {
	accessToken, ok := os.LookupEnv("VERCEL_ACCESS_TOKEN")
	if !ok {
		return nil, fmt.Errorf("no access token found")
	}
	provider := &VercelProvider{
		accessToken:  accessToken,
		domainFilter: domainFilter,
		httpClient:   &http.Client{},
	}
	return provider, nil
}

type DomainResponse struct {
	Domains []VercelDomain
}

type VercelDomain struct {
	Name string
}

func (p *VercelProvider) getDomains(ctx context.Context) ([]string, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", "https://api.vercel.com/v5/domains", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Add("Authorization", "Bearer "+p.accessToken)
	resp, err := p.httpClient.Do(req)

	if err != nil {
		return nil, err
	}
	domainResp := DomainResponse{}
	err = json.NewDecoder(resp.Body).Decode(&domainResp)

	if err != nil {
		return nil, err
	}

	var domainNames []string
	for _, d := range domainResp.Domains {
		domainNames = append(domainNames, d.Name)
	}

	return domainNames, nil
}

// Searches an array of VercelRecords to find the id of the matching endpoint.
// You should pass in the VercelRecords from getVercelRecords with the domain from the endpoint.
func (p *VercelProvider) getRecordID(records []VercelRecord, ep *endpoint.Endpoint) (string, error) {
	_, subdomain := parseDNSName(ep.DNSName)
	for _, vercelRecord := range records {
		if vercelRecord.Name == subdomain && vercelRecord.Type == ep.RecordType {
			return vercelRecord.ID, nil
		}
	}
	return "", fmt.Errorf("couldn't find a matching record when trying to find the id: %v", ep)
}

type VercelRecord struct {
	ID    string
	Name  string
	Type  string
	Value string
	TTL   int
}

type DomainRecordsResponse struct {
	Records []VercelRecord
}

func (p *VercelProvider) getVercelRecords(ctx context.Context, domain string) ([]VercelRecord, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", "https://api.vercel.com/v4/domains/"+domain+"/records", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Add("Authorization", "Bearer "+p.accessToken)
	resp, err := p.httpClient.Do(req)

	if err != nil {
		return nil, err
	}

	defer resp.Body.Close()

	decResp := DomainRecordsResponse{}
	err = json.NewDecoder(resp.Body).Decode(&decResp)

	if err != nil {
		return nil, err
	}

	return decResp.Records, nil
}

func (p *VercelProvider) Records(ctx context.Context) ([]*endpoint.Endpoint, error) {
	domains, err := p.getDomains(ctx)
	if err != nil {
		return nil, err
	}

	endpoints := make([]*endpoint.Endpoint, 0)

	for _, domain := range domains {
		if !p.domainFilter.Match(domain) {
			continue
		}
		domainRecords, err := p.getVercelRecords(ctx, domain)

		if err != nil {
			return nil, err
		}

		for _, r := range domainRecords {
			ttl := endpoint.TTL(r.TTL)
			var name string
			if r.Name == "" {
				name = ""
			} else {
				name = r.Name + "." + domain
			}

			ep := endpoint.NewEndpointWithTTL(name, r.Type, ttl, r.Value)
			endpoints = append(endpoints, ep)
		}
	}

	return endpoints, nil
}

func (p *VercelProvider) ApplyChanges(ctx context.Context, changes *plan.Changes) error {
	for _, ep := range changes.Create {
		err := p.createRecord(ctx, ep)
		if err != nil {
			return err
		}
	}

	for _, ep := range changes.Delete {
		domain, _ := parseDNSName(ep.DNSName)
		vercelRecords, err := p.getVercelRecords(ctx, domain)
		if err != nil {
			return err
		}
		recordID, err := p.getRecordID(vercelRecords, ep)
		if err != nil {
			return err
		}
		err = p.deleteRecord(ctx, domain, recordID)
		if err != nil {
			return err
		}
	}

	for _, ep := range changes.UpdateNew {
		// Creating a record on top of an existing record will update it, no need to
		// delete the ole one first
		err := p.createRecord(ctx, ep)
		if err != nil {
			return err
		}
	}

	return nil
}

func parseDNSName(name string) (string, string) {
	var domain, subdomain string
	parts := strings.Split(name, ".")
	if len(parts) <= 2 {
		domain = name
		subdomain = ""
	} else {
		domain = strings.Join(parts[len(parts)-2:], ".")
		subdomain = strings.Join(parts[0:len(parts)-2], ".")
	}

	return domain, subdomain
}

func (p *VercelProvider) createRecord(ctx context.Context, ep *endpoint.Endpoint) error {
	domain, subdomain := parseDNSName(ep.DNSName)

	params := map[string]interface{}{
		"name":  subdomain,
		"type":  ep.RecordType,
		"value": ep.Targets[0],
	}
	if ep.RecordTTL.IsConfigured() {
		params["ttl"] = ep.RecordTTL
	}
	reqBody, _ := json.Marshal(params)

	req, err := http.NewRequestWithContext(ctx, "POST", "https://api.vercel.com/v4/domains/"+domain+"/records", bytes.NewReader(reqBody))
	if err != nil {
		return err
	}
	req.Header.Add("Authorization", "Bearer "+p.accessToken)
	req.Header.Add("Content-Type", "application/json")
	resp, err := p.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		bodyBytes, err := ioutil.ReadAll(resp.Body)
		if err != nil {
			return err
		}
		body := string(bodyBytes)
		return fmt.Errorf("couldn't create the record %v:\n %v", ep, body)
	}

	return nil
}

func (p *VercelProvider) deleteRecord(ctx context.Context, domain string, id string) error {
	req, err := http.NewRequestWithContext(ctx, "DELETE", "https://api.vercel.com/v4/domains/"+domain+"/records/"+id, nil)
	if err != nil {
		return err
	}
	req.Header.Add("Authorization", "Bearer "+p.accessToken)
	resp, err := p.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return errors.New("Couldn't delete the record " + id)
	}

	return nil
}
