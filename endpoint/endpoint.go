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
	"fmt"
	"sort"
	"strings"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	// RecordTypeA is a RecordType enum value
	RecordTypeA = "A"
	// RecordTypeCNAME is a RecordType enum value
	RecordTypeCNAME = "CNAME"
	// RecordTypeTXT is a RecordType enum value
	RecordTypeTXT = "TXT"
	// RecordTypeSRV is a RecordType enum value
	RecordTypeSRV = "SRV"
	// RecordTypeNS is a RecordType enum value
	RecordTypeNS = "NS"
	// RecordTypePTR is a RecordType enum value
	RecordTypePTR = "PTR"
)

// TTL is a structure defining the TTL of a DNS record
type TTL int64

// IsConfigured returns true if TTL is configured, false otherwise
func (ttl TTL) IsConfigured() bool {
	return ttl > 0
}

// Targets is a representation of a list of targets for an endpoint.
type Targets []string

// NewTargets is a convenience method to create a new Targets object from a vararg of strings
func NewTargets(target ...string) Targets {
	t := make(Targets, 0, len(target))
	t = append(t, target...)
	return t
}

func (t Targets) String() string {
	return strings.Join(t, ";")
}

func (t Targets) Len() int {
	return len(t)
}

func (t Targets) Less(i, j int) bool {
	return t[i] < t[j]
}

func (t Targets) Swap(i, j int) {
	t[i], t[j] = t[j], t[i]
}

// Same compares to Targets and returns true if they are identical (case-insensitive)
func (t Targets) Same(o Targets) bool {
	if len(t) != len(o) {
		return false
	}
	sort.Stable(t)
	sort.Stable(o)

	for i, e := range t {
		if !strings.EqualFold(e, o[i]) {
			return false
		}
	}
	return true
}

// IsLess should fulfill the requirement to compare two targets and choose the 'lesser' one.
// In the past target was a simple string so simple string comparison could be used. Now we define 'less'
// as either being the shorter list of targets or where the first entry is less.
// FIXME We really need to define under which circumstances a list Targets is considered 'less'
// than another.
func (t Targets) IsLess(o Targets) bool {
	if len(t) < len(o) {
		return true
	}
	if len(t) > len(o) {
		return false
	}

	sort.Sort(t)
	sort.Sort(o)

	for i, e := range t {
		if e != o[i] {
			return e < o[i]
		}
	}
	return false
}

// ProviderSpecificProperty holds the name and value of a configuration which is specific to individual DNS providers
type ProviderSpecificProperty struct {
	Name  string `json:"name,omitempty"`
	Value string `json:"value,omitempty"`
}

// ProviderSpecific holds configuration which is specific to individual DNS providers
type ProviderSpecific []ProviderSpecificProperty

// Endpoint is a high-level way of a connection between a service and an IP
type Endpoint struct {
	// The hostname of the DNS record
	DNSName string `json:"dnsName,omitempty"`
	// The targets the DNS record points to
	Targets Targets `json:"targets,omitempty"`
	// RecordType type of record, e.g. CNAME, A, SRV, TXT etc
	RecordType string `json:"recordType,omitempty"`
	// Identifier to distinguish multiple records with the same name and type (e.g. Route53 records with routing policies other than 'simple')
	SetIdentifier string `json:"setIdentifier,omitempty"`
	// TTL for the record
	RecordTTL TTL `json:"recordTTL,omitempty"`
	// Labels stores labels defined for the Endpoint
	// +optional
	Labels Labels `json:"labels,omitempty"`
	// ProviderSpecific stores provider specific config
	// +optional
	ProviderSpecific ProviderSpecific `json:"providerSpecific,omitempty"`
}

// NewEndpoint initialization method to be used to create an endpoint
func NewEndpoint(dnsName, recordType string, targets ...string) *Endpoint {
	return NewEndpointWithTTL(dnsName, recordType, TTL(0), targets...)
}

// NewEndpointWithTTL initialization method to be used to create an endpoint with a TTL struct
func NewEndpointWithTTL(dnsName, recordType string, ttl TTL, targets ...string) *Endpoint {
	cleanTargets := make([]string, len(targets))
	for idx, target := range targets {
		cleanTargets[idx] = strings.TrimSuffix(target, ".")
	}

	return &Endpoint{
		DNSName:    strings.TrimSuffix(dnsName, "."),
		Targets:    cleanTargets,
		RecordType: recordType,
		Labels:     NewLabels(),
		RecordTTL:  ttl,
	}
}

// WithSetIdentifier applies the given set identifier to the endpoint.
func (e *Endpoint) WithSetIdentifier(setIdentifier string) *Endpoint {
	e.SetIdentifier = setIdentifier
	return e
}

// WithProviderSpecific attaches a key/value pair to the Endpoint and returns the Endpoint.
// This can be used to pass additional data through the stages of ExternalDNS's Endpoint processing.
// The assumption is that most of the time this will be provider specific metadata that doesn't
// warrant its own field on the Endpoint object itself. It differs from Labels in the fact that it's
// not persisted in the Registry but only kept in memory during a single record synchronization.
func (e *Endpoint) WithProviderSpecific(key, value string) *Endpoint {
	if e.ProviderSpecific == nil {
		e.ProviderSpecific = ProviderSpecific{}
	}

	e.ProviderSpecific = append(e.ProviderSpecific, ProviderSpecificProperty{Name: key, Value: value})
	return e
}

// GetProviderSpecificProperty returns a ProviderSpecificProperty if the property exists.
func (e *Endpoint) GetProviderSpecificProperty(key string) (ProviderSpecificProperty, bool) {
	for _, providerSpecific := range e.ProviderSpecific {
		if providerSpecific.Name == key {
			return providerSpecific, true
		}
	}
	return ProviderSpecificProperty{}, false
}

func (e *Endpoint) String() string {
	return fmt.Sprintf("%s %d IN %s %s %s %s", e.DNSName, e.RecordTTL, e.RecordType, e.SetIdentifier, e.Targets, e.ProviderSpecific)
}

// DNSEndpointSpec defines the desired state of DNSEndpoint
type DNSEndpointSpec struct {
	Endpoints []*Endpoint `json:"endpoints,omitempty"`
}

// DNSEndpointStatus defines the observed state of DNSEndpoint
type DNSEndpointStatus struct {
	// The generation observed by the external-dns controller.
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`
}

// +genclient
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// DNSEndpoint is a contract that a user-specified CRD must implement to be used as a source for external-dns.
// The user-specified CRD should also have the status sub-resource.
// +k8s:openapi-gen=true
// +groupName=externaldns.k8s.io
// +kubebuilder:resource:path=dnsendpoints
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +versionName=v1alpha1

type DNSEndpoint struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   DNSEndpointSpec   `json:"spec,omitempty"`
	Status DNSEndpointStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true
// DNSEndpointList is a list of DNSEndpoint objects
type DNSEndpointList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []DNSEndpoint `json:"items"`
}
