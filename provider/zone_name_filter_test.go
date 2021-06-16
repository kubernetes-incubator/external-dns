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

	"github.com/stretchr/testify/assert"
)

type zoneNameFilterTest struct {
	zoneNameFilter []string
	zone           string
	expected       bool
}

func TestZoneNameFilterMatch(t *testing.T) {
	zone := "example.org."

	for _, tt := range []zoneNameFilterTest{
		{
			[]string{},
			zone,
			true,
		},
		{
			[]string{"example.org"},
			zone,
			true,
		},
		{
			[]string{"example.org."},
			zone,
			true,
		},
		{
			[]string{" example.org. "},
			zone,
			true,
		},
		{
			[]string{"example"},
			zone,
			false,
		},
		{
			[]string{"org."},
			zone,
			false,
		},
		{
			[]string{"example.org", "company.com"},
			zone,
			true,
		},
		{
			[]string{"company.com", "example.org"},
			zone,
			true,
		},
		{
			[]string{"company.com", " example.org. "},
			zone,
			true,
		},
		{
			[]string{"company.org", "example.com"},
			zone,
			false,
		},
	} {
		zoneNameFilter := NewZoneNameFilter(tt.zoneNameFilter)
		assert.Equal(t, tt.expected, zoneNameFilter.Match(tt.zone))
	}
}

func TestZoneNameFilterIsConfigured(t *testing.T) {
	for _, tt := range []struct {
		zoneNameFilter []string
		expected       bool
	}{
		{
			[]string{},
			false,
		},
		{
			[]string{""},
			false,
		},
		{
			[]string{"   "},
			false,
		},
		{
			[]string{" . "},
			false,
		},
		{
			[]string{"", ""},
			true,
		},
		{
			[]string{"example.org"},
			true,
		},
		{
			[]string{"example.org", "company.com"},
			true,
		},
	} {
		t.Run("test IsConfigured", func(t *testing.T) {
			f := NewZoneNameFilter(tt.zoneNameFilter)
			assert.Equal(t, tt.expected, f.IsConfigured())
		})
	}
}
