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

package registry

import (
	"context"
	"errors"

	"sigs.k8s.io/external-dns/endpoint"
	"sigs.k8s.io/external-dns/plan"
	"sigs.k8s.io/external-dns/provider"
)

// AWSSDRegistry implements registry interface with ownership information associated via the Description field of SD Service
type AWSSDRegistry struct {
	provider provider.Provider
	ownerID  string
}

// NewAWSSDRegistry returns implementation of registry for AWS SD
func NewAWSSDRegistry(provider provider.Provider, ownerID string) (*AWSSDRegistry, error) {
	if ownerID == "" {
		return nil, errors.New("owner id cannot be empty")
	}
	return &AWSSDRegistry{
		provider: provider,
		ownerID:  ownerID,
	}, nil
}

func (sdr *AWSSDRegistry) GetDomainFilter() endpoint.DomainFilterInterface {
	return sdr.provider.GetDomainFilter()
}

// Records calls AWS SD API and expects AWS SD provider to provider Owner/Resource information as a serialized
// value in the AWSSDDescriptionLabel value in the Labels map
func (sdr *AWSSDRegistry) Records(ctx context.Context) ([]*endpoint.Endpoint, error) {
	records, err := sdr.provider.Records(ctx)
	if err != nil {
		return nil, err
	}

	for _, record := range records {
		labels, err := endpoint.NewLabelsFromStringPlain(record.Labels[endpoint.AWSSDDescriptionLabel])
		if err != nil {
			// if we fail to parse the output then simply assume the endpoint is not managed by any instance of External DNS
			record.Labels = endpoint.NewLabels()
			continue
		}
		record.Labels = labels
	}

	return records, nil
}

// ApplyChanges filters out records not owned the External-DNS, additionally it adds the required label
// inserted in the AWS SD instance as a CreateID field
func (sdr *AWSSDRegistry) ApplyChanges(ctx context.Context, changes *plan.Changes) error {
	filteredChanges := &plan.Changes{
		Create:    changes.Create,
		UpdateNew: filterOwnedRecords(sdr.ownerID, changes.UpdateNew),
		UpdateOld: filterOwnedRecords(sdr.ownerID, changes.UpdateOld),
		Delete:    filterOwnedRecords(sdr.ownerID, changes.Delete),
	}

	sdr.updateLabels(filteredChanges.Create)
	sdr.updateLabels(filteredChanges.UpdateNew)
	sdr.updateLabels(filteredChanges.UpdateOld)
	sdr.updateLabels(filteredChanges.Delete)

	return sdr.provider.ApplyChanges(ctx, filteredChanges)
}

func (sdr *AWSSDRegistry) updateLabels(endpoints []*endpoint.Endpoint) {
	for _, ep := range endpoints {
		if ep.Labels == nil {
			ep.Labels = make(map[string]string)
		}
		ep.Labels[endpoint.OwnerLabelKey] = sdr.ownerID
		ep.Labels[endpoint.AWSSDDescriptionLabel] = ep.Labels.SerializePlain(false)
	}
}

func (sdr *AWSSDRegistry) PropertyValuesEqual(name string, previous string, current string) bool {
	return sdr.provider.PropertyValuesEqual(name, previous, current)
}

// AdjustEndpoints modifies the endpoints as needed by the specific provider
func (sdr *AWSSDRegistry) AdjustEndpoints(endpoints []*endpoint.Endpoint) []*endpoint.Endpoint {
	return sdr.provider.AdjustEndpoints(endpoints)
}
