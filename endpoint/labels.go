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
	log "github.com/sirupsen/logrus"

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

	// AWSSDDescriptionLabel label responsible for storing raw owner/resource combination information in the Labels
	// supposed to be inserted by AWS SD Provider, and parsed into OwnerLabelKey and ResourceLabelKey key by AWS SD Registry
	AWSSDDescriptionLabel = "aws-sd-description"

	// DualstackLabelKey is the name of the label that identifies dualstack endpoints
	DualstackLabelKey = "dualstack"

	// txtEncryptionNonce label for keep same nonce for same txt records, for prevent different result of encryption for same txt record, it can cause issues for some providers
	txtEncryptionNonce = "txt-encryption-nonce"
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
func NewLabelsFromStringPlain(labelText string) (Labels, error) {
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

func NewLabelsFromString(labelText string, aesKey []byte) (Labels, error) {
	if aesKey != nil {
		decryptedText, encryptionNonce, err := DecryptText(strings.Trim(labelText, "\""), aesKey)
		//in case if we have decryption error, just try process original text
		//decryption errors should be ignored here, because we can already have plain-text labels in registry
		if err == nil {
			labels, err := NewLabelsFromStringPlain(decryptedText)
			if err == nil {
				labels[txtEncryptionNonce] = encryptionNonce
			}

			return labels, err
		}
	}
	return NewLabelsFromStringPlain(labelText)
}

// SerializePlain transforms endpoints labels into a external-dns recognizable format string
// withQuotes adds additional quotes
func (l Labels) SerializePlain(withQuotes bool) string {
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

// Serialize same to SerializePlain, but encrypt data, if encryption enabled
func (l Labels) Serialize(withQuotes bool, txtEncryptEnabled bool, aesKey []byte) string {
	if !txtEncryptEnabled {
		return l.SerializePlain(withQuotes)
	}

	var encryptionNonce []byte = nil
	if extractedNonce, nonceExists := l[txtEncryptionNonce]; nonceExists {
		encryptionNonce = []byte(extractedNonce)
		delete(l, txtEncryptionNonce)
	}

	text := l.SerializePlain(false)
	log.Debugf("Encrypt the serialized text %#v before returning it.", text)
	var err error
	text, err = EncryptText(text, aesKey, encryptionNonce)

	if err != nil {
		log.Debugf("Failed to encrypt the text %#v using the encryption key %#v. Got error %#v.", text, aesKey, err)
		panic(err)
	}

	if withQuotes {
		text = fmt.Sprintf("\"%s\"", text)
	}
	log.Debugf("Serialized text after encryption is %#v.", text)
	return text
}
