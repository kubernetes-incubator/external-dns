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
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"sigs.k8s.io/external-dns/endpoint"
	"sigs.k8s.io/external-dns/plan"
	"sigs.k8s.io/external-dns/provider"
)

var (
	_ provider.Provider = &IbmCisProvider{}
)

func TestIbmCisProvider(t *testing.T) {
	t.Run("NewIbmCisProvider", testNewIbmCisProvider)
	t.Run("Records", testIbmCisRecords)
	t.Run("ApplyChanges", testIbmCisApplyChanges)
}

func testNewIbmCisProvider(t *testing.T) {
	zoneNameFilter := endpoint.NewDomainFilter([]string{"foo.bar.com", "example.com"})

	_ = os.Unsetenv("IC_API_KEY")
	_ = os.Unsetenv("IC_CIS_INSTANCE_CRN")
	providerfail := NewIbmCisProvider(zoneNameFilter, true)
	if providerfail != nil {
		t.Errorf("expected to fail")
	}

	_ = os.Setenv("IC_API_KEY", "xxxxxxxxxxxxxxxxx")
	_ = os.Setenv("IC_CIS_INSTANCE_CRN", "xxxxxxxxxxxxxxxxx")
	provider := NewIbmCisProvider(zoneNameFilter, true)
	if provider == nil {
		t.Error("should not fail")
	}
	assert.NotNil(t, provider.dryrun)
	assert.True(t, provider.dryrun)
	assert.NotNil(t, provider.filter)
	assert.Nil(t, provider.ibmCisAPI)
	assert.NotNil(t, provider.icCisCrn)

}
func testIbmCisRecords(t *testing.T) {

	zoneNameFilter := endpoint.NewDomainFilter([]string{"foo.bar.com", "example.com"})
	provider := NewIbmCisProvider(zoneNameFilter, true)

	_, err := provider.Records(context.Background())
	require.NoError(t, err)
}

func testIbmCisApplyChanges(t *testing.T) {
	zoneNameFilter := endpoint.NewDomainFilter([]string{"foo.bar.com", "example.com"})
	provider := NewIbmCisProvider(zoneNameFilter, true)

	createRecords := []*endpoint.Endpoint{
		endpoint.NewEndpoint("create-test.zone-1.ext-dns-test-2.gcp.zalan.do", endpoint.RecordTypeA, "8.8.8.8"),
		endpoint.NewEndpointWithTTL("create-test-ttl.zone-2.ext-dns-test-2.gcp.zalan.do", endpoint.RecordTypeA, endpoint.TTL(15), "8.8.4.4"),
		endpoint.NewEndpoint("create-test-cname.zone-1.ext-dns-test-2.gcp.zalan.do", endpoint.RecordTypeCNAME, "foo.elb.amazonaws.com"),
		endpoint.NewEndpoint("filter-create-test.zone-3.ext-dns-test-2.gcp.zalan.do", endpoint.RecordTypeA, "4.2.2.2"),
		endpoint.NewEndpoint("nomatch-create-test.zone-0.ext-dns-test-2.gcp.zalan.do", endpoint.RecordTypeA, "4.2.2.1"),
	}

	currentRecords := []*endpoint.Endpoint{
		endpoint.NewEndpoint("update-test.zone-1.ext-dns-test-2.gcp.zalan.do", endpoint.RecordTypeA, "8.8.8.8"),
		endpoint.NewEndpoint("update-test.zone-2.ext-dns-test-2.gcp.zalan.do", endpoint.RecordTypeA, "8.8.4.4"),
		endpoint.NewEndpoint("update-test-cname.zone-1.ext-dns-test-2.gcp.zalan.do", endpoint.RecordTypeCNAME, "bar.elb.amazonaws.com"),
		endpoint.NewEndpoint("filter-update-test.zone-3.ext-dns-test-2.gcp.zalan.do", endpoint.RecordTypeA, "4.2.2.2"),
	}
	updatedRecords := []*endpoint.Endpoint{
		endpoint.NewEndpoint("update-test.zone-1.ext-dns-test-2.gcp.zalan.do", endpoint.RecordTypeA, "1.2.3.4"),
		endpoint.NewEndpointWithTTL("update-test-ttl.zone-2.ext-dns-test-2.gcp.zalan.do", endpoint.RecordTypeA, endpoint.TTL(25), "4.3.2.1"),
		endpoint.NewEndpoint("update-test-cname.zone-1.ext-dns-test-2.gcp.zalan.do", endpoint.RecordTypeCNAME, "baz.elb.amazonaws.com"),
		endpoint.NewEndpoint("filter-update-test.zone-3.ext-dns-test-2.gcp.zalan.do", endpoint.RecordTypeA, "5.6.7.8"),
		endpoint.NewEndpoint("nomatch-update-test.zone-0.ext-dns-test-2.gcp.zalan.do", endpoint.RecordTypeA, "8.7.6.5"),
	}

	deleteRecords := []*endpoint.Endpoint{
		endpoint.NewEndpoint("delete-test.zone-1.ext-dns-test-2.gcp.zalan.do", endpoint.RecordTypeA, "8.8.8.8"),
		endpoint.NewEndpoint("delete-test.zone-2.ext-dns-test-2.gcp.zalan.do", endpoint.RecordTypeA, "8.8.4.4"),
		endpoint.NewEndpoint("delete-test-cname.zone-1.ext-dns-test-2.gcp.zalan.do", endpoint.RecordTypeCNAME, "qux.elb.amazonaws.com"),
		endpoint.NewEndpoint("filter-delete-test.zone-3.ext-dns-test-2.gcp.zalan.do", endpoint.RecordTypeA, "4.2.2.2"),
		endpoint.NewEndpoint("nomatch-delete-test.zone-0.ext-dns-test-2.gcp.zalan.do", endpoint.RecordTypeA, "4.2.2.1"),
	}

	changes := &plan.Changes{
		Create:    createRecords,
		UpdateNew: updatedRecords,
		UpdateOld: currentRecords,
		Delete:    deleteRecords,
	}

	require.NoError(t, provider.ApplyChanges(context.Background(), changes))
}
