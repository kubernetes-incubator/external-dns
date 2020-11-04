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

package endpoint

import (
	"errors"
	"fmt"
	"sort"
	"strings"
)

var (
	// ErrInvalidHeritage is returned when heritage was not found, or different heritage is found
	ErrInvalidHeritage = errors.New("heritage is unknown or not found")
)

const (
	heritage = "external-dns"
	// OwnerLabelKey is the name of the label that defines the owner of an Endpoint.
	OwnerLabelKey = "owner"
	// ResourceLabelKey is the name of the label that identifies k8s resource which wants to acquire the DNS name
	ResourceLabelKey = "resource"
	// OwnedRecordLabelKey is the name of the label that identifies the record that is owned by the labeled TXT registry record
	OwnedRecordLabelKey = "ownedRecord"

	// AWSSDDescriptionLabel label responsible for storing raw owner/resource combination information in the Labels
	// supposed to be inserted by AWS SD Provider, and parsed into OwnerLabelKey and ResourceLabelKey key by AWS SD Registry
	AWSSDDescriptionLabel = "aws-sd-description"

	// DualstackLabelKey is the name of the label that identifies dualstack endpoints
	DualstackLabelKey = "dualstack"
)

// Labels store metadata related to the endpoint
// it is then stored in a persistent storage via serialization
type Labels map[string]string

// NewLabels returns empty Labels
func NewLabels() Labels {
	return map[string]string{}
}

// NewLabelsFromString constructs endpoints labels from a provided format string
// if heritage set to another value is found then error is returned
// no heritage automatically assumes is not owned by external-dns and returns invalidHeritage error
func NewLabelsFromString(labelText string) (Labels, error) {
	endpointLabels := map[string]string{}
	labelText = strings.Trim(labelText, "\"") // drop quotes
	tokens := strings.Split(labelText, ",")
	foundExternalDNSHeritage := false
	for _, token := range tokens {
		if len(strings.Split(token, "=")) != 2 {
			continue
		}
		key := strings.Split(token, "=")[0]
		val := strings.Split(token, "=")[1]
		if key == "heritage" && val != heritage {
			return nil, ErrInvalidHeritage
		}
		if key == "heritage" {
			foundExternalDNSHeritage = true
			continue
		}
		if strings.HasPrefix(key, heritage) {
			endpointLabels[strings.TrimPrefix(key, heritage+"/")] = val
		}
	}

	if !foundExternalDNSHeritage {
		return nil, ErrInvalidHeritage
	}

	return endpointLabels, nil
}

// Serialize transforms endpoints labels into a external-dns recognizable format string
// withQuotes adds additional quotes
func (l Labels) Serialize(withQuotes bool) string {
	var tokens []string
	tokens = append(tokens, fmt.Sprintf("heritage=%s", heritage))
	var keys []string
	for key := range l {
		keys = append(keys, key)
	}
	sort.Strings(keys) // sort for consistency

	for _, key := range keys {
		tokens = append(tokens, fmt.Sprintf("%s/%s=%s", heritage, key, l[key]))
	}
	if withQuotes {
		return fmt.Sprintf("\"%s\"", strings.Join(tokens, ","))
	}
	return strings.Join(tokens, ",")
}
