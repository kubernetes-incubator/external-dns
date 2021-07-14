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

package provider

import (
	"testing"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/route53"

	"github.com/stretchr/testify/assert"
)

func TestZoneTypeFilterMatch(t *testing.T) {
	publicZoneStr := "public"
	privateZoneStr := "private"
	publicZoneAWS := &route53.HostedZone{Config: &route53.HostedZoneConfig{PrivateZone: aws.Bool(false)}}
	privateZoneAWS := &route53.HostedZone{Config: &route53.HostedZoneConfig{PrivateZone: aws.Bool(true)}}

	for _, tc := range []struct {
		zoneTypeFilter string
		matches        bool
		zones          []interface{}
	}{
		{
			"", true, []interface{}{ publicZoneStr, privateZoneStr, &route53.HostedZone{} },
		},
		{
			"public", true, []interface{}{ publicZoneStr, publicZoneAWS, &route53.HostedZone{} },
		},
		{
			"public", false, []interface{}{ privateZoneStr, privateZoneAWS },
		},
		{
			"private", true, []interface{}{ privateZoneStr, privateZoneAWS },
		},
		{
			"private", false, []interface{}{ publicZoneStr, publicZoneAWS, &route53.HostedZone{} },
		},
		{
			"unknown", false, []interface{}{ publicZoneStr },
		},
	} {
		zoneTypeFilter := NewZoneTypeFilter(tc.zoneTypeFilter)
		for _, zone := range tc.zones {
			assert.Equal(t, tc.matches, zoneTypeFilter.Match(zone))
		}
	}
}
