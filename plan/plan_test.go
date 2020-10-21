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

package plan

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/suite"

	"sigs.k8s.io/external-dns/endpoint"
	"sigs.k8s.io/external-dns/internal/testutils"
)

type PlanTestSuite struct {
	suite.Suite
	fooV1Cname                       *endpoint.Endpoint
	fooV1SRV                         *endpoint.Endpoint
	fooV2SRV                         *endpoint.Endpoint
	fooV2Cname                       *endpoint.Endpoint
	fooV2TXT                         *endpoint.Endpoint
	fooV2CnameNoLabel                *endpoint.Endpoint
	fooV3CnameSameResource           *endpoint.Endpoint
	fooA5                            *endpoint.Endpoint
	bar127A                          *endpoint.Endpoint
	bar127AWithTTL                   *endpoint.Endpoint
	bar127AWithProviderSpecificTrue  *endpoint.Endpoint
	bar127AWithProviderSpecificFalse *endpoint.Endpoint
	bar127AWithProviderSpecificUnset *endpoint.Endpoint
	bar192A                          *endpoint.Endpoint
	multiple1                        *endpoint.Endpoint
	multiple2                        *endpoint.Endpoint
	multiple3                        *endpoint.Endpoint
	domainFilterFiltered1            *endpoint.Endpoint
	domainFilterFiltered2            *endpoint.Endpoint
	domainFilterFiltered3            *endpoint.Endpoint
	domainFilterExcluded             *endpoint.Endpoint
}

func (suite *PlanTestSuite) SetupTest() {
	suite.fooV1Cname = &endpoint.Endpoint{
		DNSName:    "foo",
		Targets:    endpoint.Targets{"v1"},
		RecordType: "CNAME",
		Labels: map[string]string{
			endpoint.ResourceLabelKey: "ingress/default/foo-v1",
			endpoint.OwnerLabelKey:    "pwner",
		},
	}
	// same resource as fooV1Cname, but target is different. It will never be picked because its target lexicographically bigger than "v1"
	suite.fooV3CnameSameResource = &endpoint.Endpoint{ // TODO: remove this once endpoint can support multiple targets
		DNSName:    "foo",
		Targets:    endpoint.Targets{"v3"},
		RecordType: "CNAME",
		Labels: map[string]string{
			endpoint.ResourceLabelKey: "ingress/default/foo-v1",
			endpoint.OwnerLabelKey:    "pwner",
		},
	}
	suite.fooV2Cname = &endpoint.Endpoint{
		DNSName:    "foo",
		Targets:    endpoint.Targets{"v2"},
		RecordType: "CNAME",
		Labels: map[string]string{
			endpoint.ResourceLabelKey: "ingress/default/foo-v2",
		},
	}
	suite.fooV1SRV = &endpoint.Endpoint{
		DNSName:    "_32554._tcp.foo.com.",
		Targets:    endpoint.Targets{"v1"},
		RecordType: "SRV",
		Labels: map[string]string{
			endpoint.ResourceLabelKey: "ingress/default/foo-v1",
		},
	}
	suite.fooV2SRV = &endpoint.Endpoint{
		DNSName:    "_32554._tcp.foo.com.",
		Targets:    endpoint.Targets{"v2"},
		RecordType: "SRV",
		Labels: map[string]string{
			endpoint.ResourceLabelKey: "ingress/default/foo-v2",
		},
	}
	suite.fooV2TXT = &endpoint.Endpoint{
		DNSName:    "foo",
		RecordType: "TXT",
	}
	suite.fooV2CnameNoLabel = &endpoint.Endpoint{
		DNSName:    "foo",
		Targets:    endpoint.Targets{"v2"},
		RecordType: "CNAME",
	}
	suite.fooA5 = &endpoint.Endpoint{
		DNSName:    "foo",
		Targets:    endpoint.Targets{"5.5.5.5"},
		RecordType: "A",
		Labels: map[string]string{
			endpoint.ResourceLabelKey: "ingress/default/foo-5",
		},
	}
	suite.bar127A = &endpoint.Endpoint{
		DNSName:    "bar",
		Targets:    endpoint.Targets{"127.0.0.1"},
		RecordType: "A",
		Labels: map[string]string{
			endpoint.ResourceLabelKey: "ingress/default/bar-127",
		},
	}
	suite.bar127AWithTTL = &endpoint.Endpoint{
		DNSName:    "bar",
		Targets:    endpoint.Targets{"127.0.0.1"},
		RecordType: "A",
		RecordTTL:  300,
		Labels: map[string]string{
			endpoint.ResourceLabelKey: "ingress/default/bar-127",
		},
	}
	suite.bar127AWithProviderSpecificTrue = &endpoint.Endpoint{
		DNSName:    "bar",
		Targets:    endpoint.Targets{"127.0.0.1"},
		RecordType: "A",
		Labels: map[string]string{
			endpoint.ResourceLabelKey: "ingress/default/bar-127",
		},
		ProviderSpecific: endpoint.ProviderSpecific{
			endpoint.ProviderSpecificProperty{
				Name:  "external-dns.alpha.kubernetes.io/cloudflare-proxied",
				Value: "true",
			},
		},
	}
	suite.bar127AWithProviderSpecificFalse = &endpoint.Endpoint{
		DNSName:    "bar",
		Targets:    endpoint.Targets{"127.0.0.1"},
		RecordType: "A",
		Labels: map[string]string{
			endpoint.ResourceLabelKey: "ingress/default/bar-127",
		},
		ProviderSpecific: endpoint.ProviderSpecific{
			endpoint.ProviderSpecificProperty{
				Name:  "external-dns.alpha.kubernetes.io/cloudflare-proxied",
				Value: "false",
			},
		},
	}
	suite.bar127AWithProviderSpecificUnset = &endpoint.Endpoint{
		DNSName:    "bar",
		Targets:    endpoint.Targets{"127.0.0.1"},
		RecordType: "A",
		Labels: map[string]string{
			endpoint.ResourceLabelKey: "ingress/default/bar-127",
		},
		ProviderSpecific: endpoint.ProviderSpecific{},
	}
	suite.bar192A = &endpoint.Endpoint{
		DNSName:    "bar",
		Targets:    endpoint.Targets{"192.168.0.1"},
		RecordType: "A",
		Labels: map[string]string{
			endpoint.ResourceLabelKey: "ingress/default/bar-192",
		},
	}
	suite.multiple1 = &endpoint.Endpoint{
		DNSName:       "multiple",
		Targets:       endpoint.Targets{"192.168.0.1"},
		RecordType:    "A",
		SetIdentifier: "test-set-1",
	}
	suite.multiple2 = &endpoint.Endpoint{
		DNSName:       "multiple",
		Targets:       endpoint.Targets{"192.168.0.2"},
		RecordType:    "A",
		SetIdentifier: "test-set-1",
	}
	suite.multiple3 = &endpoint.Endpoint{
		DNSName:       "multiple",
		Targets:       endpoint.Targets{"192.168.0.2"},
		RecordType:    "A",
		SetIdentifier: "test-set-2",
	}
	suite.domainFilterFiltered1 = &endpoint.Endpoint{
		DNSName:    "foo.domain.tld",
		Targets:    endpoint.Targets{"1.2.3.4"},
		RecordType: "A",
	}
	suite.domainFilterFiltered2 = &endpoint.Endpoint{
		DNSName:    "bar.domain.tld",
		Targets:    endpoint.Targets{"1.2.3.5"},
		RecordType: "A",
	}
	suite.domainFilterFiltered3 = &endpoint.Endpoint{
		DNSName:    "baz.domain.tld",
		Targets:    endpoint.Targets{"1.2.3.6"},
		RecordType: "A",
	}
	suite.domainFilterExcluded = &endpoint.Endpoint{
		DNSName:    "foo.ex.domain.tld",
		Targets:    endpoint.Targets{"1.1.1.1"},
		RecordType: "A",
	}
}

func (suite *PlanTestSuite) TestSyncFirstRound() {
	current := []*endpoint.Endpoint{}
	desired := []*endpoint.Endpoint{suite.fooV1Cname, suite.fooV2Cname, suite.fooV1SRV, suite.bar127A}
	expectedCreate := []*endpoint.Endpoint{suite.fooV1Cname, suite.fooV1SRV, suite.bar127A} //v1 is chosen because of resolver taking "min"
	expectedUpdateOld := []*endpoint.Endpoint{}
	expectedUpdateNew := []*endpoint.Endpoint{}
	expectedDelete := []*endpoint.Endpoint{}

	p := &Plan{
		Policies: []Policy{&SyncPolicy{}},
		Current:  current,
		Desired:  desired,
	}

	changes := p.Calculate().Changes
	validateEntries(suite.T(), changes.Create, expectedCreate)
	validateEntries(suite.T(), changes.UpdateNew, expectedUpdateNew)
	validateEntries(suite.T(), changes.UpdateOld, expectedUpdateOld)
	validateEntries(suite.T(), changes.Delete, expectedDelete)
}

func (suite *PlanTestSuite) TestSyncSecondRound() {
	current := []*endpoint.Endpoint{suite.fooV1Cname}
	desired := []*endpoint.Endpoint{suite.fooV2Cname, suite.fooV1Cname, suite.bar127A}
	expectedCreate := []*endpoint.Endpoint{suite.bar127A}
	expectedUpdateOld := []*endpoint.Endpoint{}
	expectedUpdateNew := []*endpoint.Endpoint{}
	expectedDelete := []*endpoint.Endpoint{}

	p := &Plan{
		Policies: []Policy{&SyncPolicy{}},
		Current:  current,
		Desired:  desired,
	}

	changes := p.Calculate().Changes
	validateEntries(suite.T(), changes.Create, expectedCreate)
	validateEntries(suite.T(), changes.UpdateNew, expectedUpdateNew)
	validateEntries(suite.T(), changes.UpdateOld, expectedUpdateOld)
	validateEntries(suite.T(), changes.Delete, expectedDelete)
}

func (suite *PlanTestSuite) TestSyncSecondRoundMigration() {
	current := []*endpoint.Endpoint{suite.fooV2CnameNoLabel, suite.fooV2SRV}
	desired := []*endpoint.Endpoint{suite.fooV2Cname, suite.fooV1Cname, suite.bar127A, suite.fooV1SRV}
	expectedCreate := []*endpoint.Endpoint{suite.bar127A}
	expectedUpdateOld := []*endpoint.Endpoint{suite.fooV2CnameNoLabel, suite.fooV2SRV}
	expectedUpdateNew := []*endpoint.Endpoint{suite.fooV1Cname, suite.fooV1SRV}
	expectedDelete := []*endpoint.Endpoint{}

	p := &Plan{
		Policies: []Policy{&SyncPolicy{}},
		Current:  current,
		Desired:  desired,
	}

	changes := p.Calculate().Changes
	validateEntries(suite.T(), changes.Create, expectedCreate)
	validateEntries(suite.T(), changes.UpdateNew, expectedUpdateNew)
	validateEntries(suite.T(), changes.UpdateOld, expectedUpdateOld)
	validateEntries(suite.T(), changes.Delete, expectedDelete)
}

func (suite *PlanTestSuite) TestSyncSecondRoundWithTTLChange() {
	current := []*endpoint.Endpoint{suite.bar127A}
	desired := []*endpoint.Endpoint{suite.bar127AWithTTL}
	expectedCreate := []*endpoint.Endpoint{}
	expectedUpdateOld := []*endpoint.Endpoint{suite.bar127A}
	expectedUpdateNew := []*endpoint.Endpoint{suite.bar127AWithTTL}
	expectedDelete := []*endpoint.Endpoint{}

	p := &Plan{
		Policies: []Policy{&SyncPolicy{}},
		Current:  current,
		Desired:  desired,
	}

	changes := p.Calculate().Changes
	validateEntries(suite.T(), changes.Create, expectedCreate)
	validateEntries(suite.T(), changes.UpdateNew, expectedUpdateNew)
	validateEntries(suite.T(), changes.UpdateOld, expectedUpdateOld)
	validateEntries(suite.T(), changes.Delete, expectedDelete)
}

func (suite *PlanTestSuite) TestSyncSecondRoundWithProviderSpecificChange() {
	current := []*endpoint.Endpoint{suite.bar127AWithProviderSpecificTrue}
	desired := []*endpoint.Endpoint{suite.bar127AWithProviderSpecificFalse}
	expectedCreate := []*endpoint.Endpoint{}
	expectedUpdateOld := []*endpoint.Endpoint{suite.bar127AWithProviderSpecificTrue}
	expectedUpdateNew := []*endpoint.Endpoint{suite.bar127AWithProviderSpecificFalse}
	expectedDelete := []*endpoint.Endpoint{}

	p := &Plan{
		Policies: []Policy{&SyncPolicy{}},
		Current:  current,
		Desired:  desired,
	}

	changes := p.Calculate().Changes
	validateEntries(suite.T(), changes.Create, expectedCreate)
	validateEntries(suite.T(), changes.UpdateNew, expectedUpdateNew)
	validateEntries(suite.T(), changes.UpdateOld, expectedUpdateOld)
	validateEntries(suite.T(), changes.Delete, expectedDelete)
}

func (suite *PlanTestSuite) TestSyncSecondRoundWithProviderSpecificDefaultFalse() {
	current := []*endpoint.Endpoint{suite.bar127AWithProviderSpecificFalse}
	desired := []*endpoint.Endpoint{suite.bar127AWithProviderSpecificUnset}
	expectedCreate := []*endpoint.Endpoint{}
	expectedUpdateOld := []*endpoint.Endpoint{}
	expectedUpdateNew := []*endpoint.Endpoint{}
	expectedDelete := []*endpoint.Endpoint{}

	p := &Plan{
		Policies: []Policy{&SyncPolicy{}},
		Current:  current,
		Desired:  desired,
		PropertyComparator: func(name, previous, current string) bool {
			return CompareBoolean(false, name, previous, current)
		},
	}

	changes := p.Calculate().Changes
	validateEntries(suite.T(), changes.Create, expectedCreate)
	validateEntries(suite.T(), changes.UpdateNew, expectedUpdateNew)
	validateEntries(suite.T(), changes.UpdateOld, expectedUpdateOld)
	validateEntries(suite.T(), changes.Delete, expectedDelete)
}

func (suite *PlanTestSuite) TestSyncSecondRoundWithProviderSpecificDefualtTrue() {
	current := []*endpoint.Endpoint{suite.bar127AWithProviderSpecificTrue}
	desired := []*endpoint.Endpoint{suite.bar127AWithProviderSpecificUnset}
	expectedCreate := []*endpoint.Endpoint{}
	expectedUpdateOld := []*endpoint.Endpoint{}
	expectedUpdateNew := []*endpoint.Endpoint{}
	expectedDelete := []*endpoint.Endpoint{}

	p := &Plan{
		Policies: []Policy{&SyncPolicy{}},
		Current:  current,
		Desired:  desired,
		PropertyComparator: func(name, previous, current string) bool {
			return CompareBoolean(true, name, previous, current)
		},
	}

	changes := p.Calculate().Changes
	validateEntries(suite.T(), changes.Create, expectedCreate)
	validateEntries(suite.T(), changes.UpdateNew, expectedUpdateNew)
	validateEntries(suite.T(), changes.UpdateOld, expectedUpdateOld)
	validateEntries(suite.T(), changes.Delete, expectedDelete)
}

func (suite *PlanTestSuite) TestSyncSecondRoundWithOwnerInherited() {
	current := []*endpoint.Endpoint{suite.fooV1Cname}
	desired := []*endpoint.Endpoint{suite.fooV2Cname}

	expectedCreate := []*endpoint.Endpoint{}
	expectedUpdateOld := []*endpoint.Endpoint{suite.fooV1Cname}
	expectedUpdateNew := []*endpoint.Endpoint{{
		DNSName:    suite.fooV2Cname.DNSName,
		Targets:    suite.fooV2Cname.Targets,
		RecordType: suite.fooV2Cname.RecordType,
		RecordTTL:  suite.fooV2Cname.RecordTTL,
		Labels: map[string]string{
			endpoint.ResourceLabelKey: suite.fooV2Cname.Labels[endpoint.ResourceLabelKey],
			endpoint.OwnerLabelKey:    suite.fooV1Cname.Labels[endpoint.OwnerLabelKey],
		},
	}}
	expectedDelete := []*endpoint.Endpoint{}

	p := &Plan{
		Policies: []Policy{&SyncPolicy{}},
		Current:  current,
		Desired:  desired,
	}

	changes := p.Calculate().Changes
	validateEntries(suite.T(), changes.Create, expectedCreate)
	validateEntries(suite.T(), changes.UpdateNew, expectedUpdateNew)
	validateEntries(suite.T(), changes.UpdateOld, expectedUpdateOld)
	validateEntries(suite.T(), changes.Delete, expectedDelete)
}

func (suite *PlanTestSuite) TestIdempotency() {
	current := []*endpoint.Endpoint{suite.fooV1Cname, suite.fooV2Cname}
	desired := []*endpoint.Endpoint{suite.fooV1Cname, suite.fooV2Cname}
	expectedCreate := []*endpoint.Endpoint{}
	expectedUpdateOld := []*endpoint.Endpoint{}
	expectedUpdateNew := []*endpoint.Endpoint{}
	expectedDelete := []*endpoint.Endpoint{}

	p := &Plan{
		Policies: []Policy{&SyncPolicy{}},
		Current:  current,
		Desired:  desired,
	}

	changes := p.Calculate().Changes
	validateEntries(suite.T(), changes.Create, expectedCreate)
	validateEntries(suite.T(), changes.UpdateNew, expectedUpdateNew)
	validateEntries(suite.T(), changes.UpdateOld, expectedUpdateOld)
	validateEntries(suite.T(), changes.Delete, expectedDelete)
}

func (suite *PlanTestSuite) TestDifferentTypes() {
	current := []*endpoint.Endpoint{suite.fooV1Cname}
	desired := []*endpoint.Endpoint{suite.fooV2Cname, suite.fooA5}
	expectedCreate := []*endpoint.Endpoint{}
	expectedUpdateOld := []*endpoint.Endpoint{suite.fooV1Cname}
	expectedUpdateNew := []*endpoint.Endpoint{suite.fooA5}
	expectedDelete := []*endpoint.Endpoint{}

	p := &Plan{
		Policies: []Policy{&SyncPolicy{}},
		Current:  current,
		Desired:  desired,
	}

	changes := p.Calculate().Changes
	validateEntries(suite.T(), changes.Create, expectedCreate)
	validateEntries(suite.T(), changes.UpdateNew, expectedUpdateNew)
	validateEntries(suite.T(), changes.UpdateOld, expectedUpdateOld)
	validateEntries(suite.T(), changes.Delete, expectedDelete)
}

func (suite *PlanTestSuite) TestIgnoreTXT() {
	current := []*endpoint.Endpoint{suite.fooV2TXT}
	desired := []*endpoint.Endpoint{suite.fooV2Cname}
	expectedCreate := []*endpoint.Endpoint{suite.fooV2Cname}
	expectedUpdateOld := []*endpoint.Endpoint{}
	expectedUpdateNew := []*endpoint.Endpoint{}
	expectedDelete := []*endpoint.Endpoint{}

	p := &Plan{
		Policies: []Policy{&SyncPolicy{}},
		Current:  current,
		Desired:  desired,
	}

	changes := p.Calculate().Changes
	validateEntries(suite.T(), changes.Create, expectedCreate)
	validateEntries(suite.T(), changes.UpdateNew, expectedUpdateNew)
	validateEntries(suite.T(), changes.UpdateOld, expectedUpdateOld)
	validateEntries(suite.T(), changes.Delete, expectedDelete)
}

func (suite *PlanTestSuite) TestRemoveEndpoint() {
	current := []*endpoint.Endpoint{suite.fooV1Cname, suite.bar192A, suite.fooV1SRV}
	desired := []*endpoint.Endpoint{suite.fooV1Cname}
	expectedCreate := []*endpoint.Endpoint{}
	expectedUpdateOld := []*endpoint.Endpoint{}
	expectedUpdateNew := []*endpoint.Endpoint{}
	expectedDelete := []*endpoint.Endpoint{suite.bar192A, suite.fooV1SRV}

	p := &Plan{
		Policies: []Policy{&SyncPolicy{}},
		Current:  current,
		Desired:  desired,
	}

	changes := p.Calculate().Changes
	validateEntries(suite.T(), changes.Create, expectedCreate)
	validateEntries(suite.T(), changes.UpdateNew, expectedUpdateNew)
	validateEntries(suite.T(), changes.UpdateOld, expectedUpdateOld)
	validateEntries(suite.T(), changes.Delete, expectedDelete)
}

func (suite *PlanTestSuite) TestRemoveEndpointWithUpsert() {
	current := []*endpoint.Endpoint{suite.fooV1Cname, suite.bar192A}
	desired := []*endpoint.Endpoint{suite.fooV1Cname}
	expectedCreate := []*endpoint.Endpoint{}
	expectedUpdateOld := []*endpoint.Endpoint{}
	expectedUpdateNew := []*endpoint.Endpoint{}
	expectedDelete := []*endpoint.Endpoint{}

	p := &Plan{
		Policies: []Policy{&UpsertOnlyPolicy{}},
		Current:  current,
		Desired:  desired,
	}

	changes := p.Calculate().Changes
	validateEntries(suite.T(), changes.Create, expectedCreate)
	validateEntries(suite.T(), changes.UpdateNew, expectedUpdateNew)
	validateEntries(suite.T(), changes.UpdateOld, expectedUpdateOld)
	validateEntries(suite.T(), changes.Delete, expectedDelete)
}

//TODO: remove once multiple-target per endpoint is supported
func (suite *PlanTestSuite) TestDuplicatedEndpointsForSameResourceReplace() {
	current := []*endpoint.Endpoint{suite.fooV3CnameSameResource, suite.bar192A}
	desired := []*endpoint.Endpoint{suite.fooV1Cname, suite.fooV3CnameSameResource}
	expectedCreate := []*endpoint.Endpoint{}
	expectedUpdateOld := []*endpoint.Endpoint{suite.fooV3CnameSameResource}
	expectedUpdateNew := []*endpoint.Endpoint{suite.fooV1Cname}
	expectedDelete := []*endpoint.Endpoint{suite.bar192A}

	p := &Plan{
		Policies: []Policy{&SyncPolicy{}},
		Current:  current,
		Desired:  desired,
	}

	changes := p.Calculate().Changes
	validateEntries(suite.T(), changes.Create, expectedCreate)
	validateEntries(suite.T(), changes.UpdateNew, expectedUpdateNew)
	validateEntries(suite.T(), changes.UpdateOld, expectedUpdateOld)
	validateEntries(suite.T(), changes.Delete, expectedDelete)
}

//TODO: remove once multiple-target per endpoint is supported
func (suite *PlanTestSuite) TestDuplicatedEndpointsForSameResourceRetain() {

	current := []*endpoint.Endpoint{suite.fooV1Cname, suite.bar192A}
	desired := []*endpoint.Endpoint{suite.fooV1Cname, suite.fooV3CnameSameResource}
	expectedCreate := []*endpoint.Endpoint{}
	expectedUpdateOld := []*endpoint.Endpoint{}
	expectedUpdateNew := []*endpoint.Endpoint{}
	expectedDelete := []*endpoint.Endpoint{suite.bar192A}

	p := &Plan{
		Policies: []Policy{&SyncPolicy{}},
		Current:  current,
		Desired:  desired,
	}

	changes := p.Calculate().Changes
	validateEntries(suite.T(), changes.Create, expectedCreate)
	validateEntries(suite.T(), changes.UpdateNew, expectedUpdateNew)
	validateEntries(suite.T(), changes.UpdateOld, expectedUpdateOld)
	validateEntries(suite.T(), changes.Delete, expectedDelete)
}

func (suite *PlanTestSuite) TestMultipleRecordsSameNameDifferentSetIdentifier() {

	current := []*endpoint.Endpoint{suite.multiple1}
	desired := []*endpoint.Endpoint{suite.multiple2, suite.multiple3}
	expectedCreate := []*endpoint.Endpoint{suite.multiple3}
	expectedUpdateOld := []*endpoint.Endpoint{suite.multiple1}
	expectedUpdateNew := []*endpoint.Endpoint{suite.multiple2}
	expectedDelete := []*endpoint.Endpoint{}

	p := &Plan{
		Policies: []Policy{&SyncPolicy{}},
		Current:  current,
		Desired:  desired,
	}

	changes := p.Calculate().Changes
	validateEntries(suite.T(), changes.Create, expectedCreate)
	validateEntries(suite.T(), changes.UpdateNew, expectedUpdateNew)
	validateEntries(suite.T(), changes.UpdateOld, expectedUpdateOld)
	validateEntries(suite.T(), changes.Delete, expectedDelete)
}

func (suite *PlanTestSuite) TestSetIdentifierUpdateCreatesAndDeletes() {

	current := []*endpoint.Endpoint{suite.multiple2}
	desired := []*endpoint.Endpoint{suite.multiple3}
	expectedCreate := []*endpoint.Endpoint{suite.multiple3}
	expectedUpdateOld := []*endpoint.Endpoint{}
	expectedUpdateNew := []*endpoint.Endpoint{}
	expectedDelete := []*endpoint.Endpoint{suite.multiple2}

	p := &Plan{
		Policies: []Policy{&SyncPolicy{}},
		Current:  current,
		Desired:  desired,
	}

	changes := p.Calculate().Changes
	validateEntries(suite.T(), changes.Create, expectedCreate)
	validateEntries(suite.T(), changes.UpdateNew, expectedUpdateNew)
	validateEntries(suite.T(), changes.UpdateOld, expectedUpdateOld)
	validateEntries(suite.T(), changes.Delete, expectedDelete)
}

func (suite *PlanTestSuite) TestDomainFiltersInitial() {

	current := []*endpoint.Endpoint{suite.domainFilterExcluded}
	desired := []*endpoint.Endpoint{suite.domainFilterExcluded, suite.domainFilterFiltered1, suite.domainFilterFiltered2, suite.domainFilterFiltered3}
	expectedCreate := []*endpoint.Endpoint{suite.domainFilterFiltered1, suite.domainFilterFiltered2, suite.domainFilterFiltered3}
	expectedUpdateOld := []*endpoint.Endpoint{}
	expectedUpdateNew := []*endpoint.Endpoint{}
	expectedDelete := []*endpoint.Endpoint{}

	p := &Plan{
		Policies:     []Policy{&SyncPolicy{}},
		Current:      current,
		Desired:      desired,
		DomainFilter: endpoint.NewDomainFilterWithExclusions([]string{"domain.tld"}, []string{"ex.domain.tld"}),
	}

	changes := p.Calculate().Changes
	validateEntries(suite.T(), changes.Create, expectedCreate)
	validateEntries(suite.T(), changes.UpdateNew, expectedUpdateNew)
	validateEntries(suite.T(), changes.UpdateOld, expectedUpdateOld)
	validateEntries(suite.T(), changes.Delete, expectedDelete)
}

func (suite *PlanTestSuite) TestDomainFiltersUpdate() {

	current := []*endpoint.Endpoint{suite.domainFilterExcluded, suite.domainFilterFiltered1, suite.domainFilterFiltered2}
	desired := []*endpoint.Endpoint{suite.domainFilterExcluded, suite.domainFilterFiltered1, suite.domainFilterFiltered2, suite.domainFilterFiltered3}
	expectedCreate := []*endpoint.Endpoint{suite.domainFilterFiltered3}
	expectedUpdateOld := []*endpoint.Endpoint{}
	expectedUpdateNew := []*endpoint.Endpoint{}
	expectedDelete := []*endpoint.Endpoint{}

	p := &Plan{
		Policies:     []Policy{&SyncPolicy{}},
		Current:      current,
		Desired:      desired,
		DomainFilter: endpoint.NewDomainFilterWithExclusions([]string{"domain.tld"}, []string{"ex.domain.tld"}),
	}

	changes := p.Calculate().Changes
	validateEntries(suite.T(), changes.Create, expectedCreate)
	validateEntries(suite.T(), changes.UpdateNew, expectedUpdateNew)
	validateEntries(suite.T(), changes.UpdateOld, expectedUpdateOld)
	validateEntries(suite.T(), changes.Delete, expectedDelete)
}

func TestPlan(t *testing.T) {
	suite.Run(t, new(PlanTestSuite))
}

// validateEntries validates that the list of entries matches expected.
func validateEntries(t *testing.T, entries, expected []*endpoint.Endpoint) {
	if !testutils.SameEndpoints(entries, expected) {
		t.Fatalf("expected %q to match %q", entries, expected)
	}
}

func TestNormalizeDNSName(t *testing.T) {
	records := []struct {
		dnsName string
		expect  string
	}{
		{
			"3AAAA.FOO.BAR.COM    ",
			"3aaaa.foo.bar.com.",
		},
		{
			"   example.foo.com.",
			"example.foo.com.",
		},
		{
			"example123.foo.com ",
			"example123.foo.com.",
		},
		{
			"foo",
			"foo.",
		},
		{
			"123foo.bar",
			"123foo.bar.",
		},
		{
			"foo.com",
			"foo.com.",
		},
		{
			"foo.com.",
			"foo.com.",
		},
		{
			"foo123.COM",
			"foo123.com.",
		},
		{
			"my-exaMple3.FOO.BAR.COM",
			"my-example3.foo.bar.com.",
		},
		{
			"   my-example1214.FOO-1235.BAR-foo.COM   ",
			"my-example1214.foo-1235.bar-foo.com.",
		},
		{
			"my-example-my-example-1214.FOO-1235.BAR-foo.COM",
			"my-example-my-example-1214.foo-1235.bar-foo.com.",
		},
	}
	for _, r := range records {
		gotName := normalizeDNSName(r.dnsName)
		assert.Equal(t, r.expect, gotName)
	}
}
