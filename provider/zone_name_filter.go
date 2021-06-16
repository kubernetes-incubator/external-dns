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

import "strings"

// ZoneNameFilter holds a list of zone names to filter by
type ZoneNameFilter struct {
	ZoneNames []string
}

// NewZoneNameFilter returns a new ZoneNameFilter given a list of zone names
func NewZoneNameFilter(zoneNames []string) ZoneNameFilter {
	zs := make([]string, len(zoneNames))
	for i, zone := range zoneNames {
		zs[i] = strings.TrimSuffix(strings.TrimSpace(zone), ".")
	}

	return ZoneNameFilter{zs}
}

// Match checks whether a zone matches one of the provided zone names
func (f ZoneNameFilter) Match(zoneName string) bool {
	// An empty filter includes all names.
	if len(f.ZoneNames) == 0 {
		return true
	}

	for _, name := range f.ZoneNames {
		if strings.EqualFold(strings.TrimSuffix(strings.TrimSpace(zoneName), "."), name) {
			return true
		}
	}

	return false
}

// IsConfigured returns true if ZoneNameFilter is configured, false otherwise
func (f ZoneNameFilter) IsConfigured() bool {
	if len(f.ZoneNames) == 1 {
		return f.ZoneNames[0] != ""
	}
	return len(f.ZoneNames) > 0
}
