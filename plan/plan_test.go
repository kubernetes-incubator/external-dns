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

	"fmt"
	"github.com/kubernetes-incubator/external-dns/endpoint"
	"github.com/kubernetes-incubator/external-dns/internal/testutils"
	"github.com/stretchr/testify/suite"
)

type PlanTestSuite struct {
	suite.Suite
	fooV1Cname *endpoint.Endpoint
	fooV2Cname *endpoint.Endpoint
	fooA       *endpoint.Endpoint
	bar127A    *endpoint.Endpoint
	bar192A    *endpoint.Endpoint
}

func (suite *PlanTestSuite) SetupTest() {
	suite.fooV1Cname = &endpoint.Endpoint{
		DNSName:    "foo",
		Target:     "v1",
		RecordType: "CNAME",
	}
	suite.fooV2Cname = &endpoint.Endpoint{
		DNSName:    "foo",
		Target:     "v2",
		RecordType: "CNAME",
	}
	suite.bar127A = &endpoint.Endpoint{
		DNSName:    "bar",
		Target:     "127.0.0.1",
		RecordType: "A",
	}
	suite.bar192A = &endpoint.Endpoint{
		DNSName:    "bar",
		Target:     "192.168.0.1",
		RecordType: "A",
	}
	suite.fooA = &endpoint.Endpoint{
		DNSName:    "foo",
		Target:     "5.5.5.5",
		RecordType: "A",
	}
}

func (suite *PlanTestSuite) TestSyncFirstRound() {
	current := []*endpoint.Endpoint{}
	desired := []*endpoint.Endpoint{suite.fooV1Cname, suite.fooV2Cname, suite.bar127A}
	expectedCreate := []*endpoint.Endpoint{suite.fooV1Cname, suite.fooV2Cname, suite.bar127A}
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
	expectedCreate := []*endpoint.Endpoint{suite.fooV2Cname, suite.bar127A}
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
	desired := []*endpoint.Endpoint{suite.fooV1Cname, suite.fooA}
	expectedCreate := []*endpoint.Endpoint{suite.fooA}
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
	current := []*endpoint.Endpoint{suite.fooV1Cname, suite.fooV2Cname}
	desired := []*endpoint.Endpoint{suite.fooV1Cname}
	expectedCreate := []*endpoint.Endpoint{}
	expectedUpdateOld := []*endpoint.Endpoint{}
	expectedUpdateNew := []*endpoint.Endpoint{}
	expectedDelete := []*endpoint.Endpoint{suite.fooV2Cname}

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

func TestPlan(t *testing.T) {
	suite.Run(t, new(PlanTestSuite))
}

// TestCalculate tests that a plan can calculate actions to move a list of
// current records to a list of desired records.
func TestCalculate(t *testing.T) {
	// empty list of records
	empty := []*endpoint.Endpoint{}
	// a simple entry
	fooV1 := []*endpoint.Endpoint{endpoint.NewEndpoint("foo", "v1", "CNAME")}
	// the same entry but with different target
	fooV2 := []*endpoint.Endpoint{endpoint.NewEndpoint("foo", "v2", "CNAME")}
	// another simple entry
	bar := []*endpoint.Endpoint{endpoint.NewEndpoint("bar", "v1", "CNAME")}

	// test case with labels
	noLabels := []*endpoint.Endpoint{endpoint.NewEndpoint("foo", "v2", "CNAME")}
	labeledV2 := []*endpoint.Endpoint{newEndpointWithOwner("foo", "v2", "123")}
	labeledV1 := []*endpoint.Endpoint{newEndpointWithOwner("foo", "v1", "123")}

	// test case with type inheritance
	noType := []*endpoint.Endpoint{endpoint.NewEndpoint("foo", "v2", "")}
	typedV1 := []*endpoint.Endpoint{endpoint.NewEndpoint("foo", "v1", "A")}

	for _, tc := range []struct {
		policies                             []Policy
		current, desired                     []*endpoint.Endpoint
		create, updateOld, updateNew, delete []*endpoint.Endpoint
	}{
		// Nothing exists and nothing desired doesn't change anything.
		{[]Policy{&SyncPolicy{}}, empty, empty, empty, empty, empty, empty},
		// More desired than current creates the desired.
		{[]Policy{&SyncPolicy{}}, empty, fooV1, fooV1, empty, empty, empty},
		// Desired equals current doesn't change anything.
		{[]Policy{&SyncPolicy{}}, fooV1, fooV1, empty, empty, empty, empty},
		// Nothing is desired deletes the current.
		{[]Policy{&SyncPolicy{}}, fooV1, empty, empty, empty, empty, fooV1},
		// Current and desired match but Target is different triggers an update.
		{[]Policy{&SyncPolicy{}}, fooV1, fooV2, empty, fooV1, fooV2, empty},
		// Both exist but are different creates desired and deletes current.
		{[]Policy{&SyncPolicy{}}, fooV1, bar, bar, empty, empty, fooV1},
		// Nothing is desired but policy doesn't allow deletions.
		{[]Policy{&UpsertOnlyPolicy{}}, fooV1, empty, empty, empty, empty, empty},
		// Labels should be inherited
		{[]Policy{&SyncPolicy{}}, labeledV1, noLabels, empty, labeledV1, labeledV2, empty},
		// RecordType should be inherited
		{[]Policy{&SyncPolicy{}}, typedV1, noType, noType, empty, empty, typedV1},
	} {
		// setup plan
		plan := &Plan{
			Policies: tc.policies,
			Current:  tc.current,
			Desired:  tc.desired,
		}
		// calculate actions
		plan = plan.Calculate()

		// validate actions
		validateEntries(t, plan.Changes.Create, tc.create)
		validateEntries(t, plan.Changes.UpdateOld, tc.updateOld)
		validateEntries(t, plan.Changes.UpdateNew, tc.updateNew)
		validateEntries(t, plan.Changes.Delete, tc.delete)
	}
}

// BenchmarkCalculate benchmarks the Calculate method.
func BenchmarkCalculate(b *testing.B) {
	foo := endpoint.NewEndpoint("foo", "v1", "")
	barV1 := endpoint.NewEndpoint("bar", "v1", "")
	barV2 := endpoint.NewEndpoint("bar", "v2", "")
	baz := endpoint.NewEndpoint("baz", "v1", "")

	plan := &Plan{
		Current: []*endpoint.Endpoint{foo, barV1},
		Desired: []*endpoint.Endpoint{barV2, baz},
	}

	for i := 0; i < b.N; i++ {
		plan.Calculate()
	}
}

// ExamplePlan shows how plan can be used.
func ExamplePlan() {
	foo := endpoint.NewEndpoint("foo.example.com", "1.2.3.4", "")
	barV1 := endpoint.NewEndpoint("bar.example.com", "8.8.8.8", "")
	barV2 := endpoint.NewEndpoint("bar.example.com", "8.8.4.4", "")
	baz := endpoint.NewEndpoint("baz.example.com", "6.6.6.6", "")

	// Plan where
	// * foo should be deleted
	// * bar should be updated from v1 to v2
	// * baz should be created
	plan := &Plan{
		Policies: []Policy{&SyncPolicy{}},
		Current:  []*endpoint.Endpoint{foo, barV1},
		Desired:  []*endpoint.Endpoint{barV2, baz},
	}

	// calculate actions
	plan = plan.Calculate()

	// print actions
	fmt.Println("Create:")
	for _, ep := range plan.Changes.Create {
		fmt.Println(ep)
	}
	fmt.Println("UpdateOld:")
	for _, ep := range plan.Changes.UpdateOld {
		fmt.Println(ep)
	}
	fmt.Println("UpdateNew:")
	for _, ep := range plan.Changes.UpdateNew {
		fmt.Println(ep)
	}
	fmt.Println("Delete:")
	for _, ep := range plan.Changes.Delete {
		fmt.Println(ep)
	}
	// Create:
	// &{baz.example.com 6.6.6.6 map[] }
	// UpdateOld:
	// &{bar.example.com 8.8.8.8 map[] }
	// UpdateNew:
	// &{bar.example.com 8.8.4.4 map[] }
	// Delete:
	// &{foo.example.com 1.2.3.4 map[] }
}

// validateEntries validates that the list of entries matches expected.
func validateEntries(t *testing.T, entries, expected []*endpoint.Endpoint) {
	if len(entries) != len(expected) {
		t.Fatalf("expected %q to match %q", entries, expected)
	}

	for i := range entries {
		if !testutils.SameEndpoint(entries[i], expected[i]) {
			t.Fatalf("expected %q to match %q", entries, expected)
		}
	}
}

func newEndpointWithOwner(dnsName, target, ownerID string) *endpoint.Endpoint {
	e := endpoint.NewEndpoint(dnsName, target, "CNAME")
	e.Labels[endpoint.OwnerLabelKey] = ownerID
	return e
}
