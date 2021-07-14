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
	// heritageKey is the name of the heritage label.
	heritageKey = "heritage"
	// controllerKey is the name of the controller managing labels.
	controllerName = "external-dns"
	// OwnerLabelKey is the name of the label that defines the owner of an Endpoint.
	OwnerLabelKey = "owner"
	// ResourceLabelKey is the name of the label that identifies k8s resource which wants to acquire the DNS name
	ResourceLabelKey = "resource"

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

// NewLabelsFromString constructs endpoints labels from a provided format string.
// If the heritageKey is set to a value other than controllerName, an error is returned.
// If no heritageKey value is found, the endpoint is presumably not owned by external-dns,
// so an invalidHeritage error is returned.
func NewLabelsFromString(labelText string) (Labels, error) {
	endpointLabels := map[string]string{}
	labelText = strings.Trim(labelText, "\"") // drop quotes
	tokens := strings.Split(labelText, ",")
	foundExternalDNSHeritage := false
	for _, token := range tokens {
		keyValuePair := strings.Split(token, "=")
		if len(keyValuePair) != 2 {
			continue
		}
		key := keyValuePair[0]
		val := keyValuePair[1]

		if key == heritageKey {
			if val != controllerName {
				return nil, ErrInvalidHeritage
			}
			foundExternalDNSHeritage = true
			continue
		}

		if strings.HasPrefix(key, controllerName) {
			endpointLabels[strings.TrimPrefix(key, controllerName+"/")] = val
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
	tokens = append(tokens, fmt.Sprintf("heritage=%s", controllerName))
	var keys []string
	for key := range l {
		keys = append(keys, key)
	}
	sort.Strings(keys) // sort for consistency

	for _, key := range keys {
		tokens = append(tokens, fmt.Sprintf("%s/%s=%s", controllerName, key, l[key]))
	}
	if withQuotes {
		return fmt.Sprintf("\"%s\"", strings.Join(tokens, ","))
	}
	return strings.Join(tokens, ",")
}
