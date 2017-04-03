package testutils

import (
	"github.com/kubernetes-incubator/external-dns/endpoint"
)

/** test utility functions for endpoints verifications */

// SameEndpoint returns true if two endpoint are same
// considers example.org. and example.org DNSName/Target as different endpoints
// TODO:might need reconsideration regarding trailing dot
func SameEndpoint(a, b endpoint.Endpoint) bool {
	return a.DNSName == b.DNSName && a.Target == b.Target
}

// SameEndpoints compares two slices of endpoints regardless of order
// [x,y,z] == [z,x,y]
// [x,x,z] == [x,z,x]
// [x,y,y] != [x,x,y]
// [x,x,x] != [x,x,z]
func SameEndpoints(a, b []endpoint.Endpoint) bool {
	if len(a) != len(b) {
		return false
	}

	calculator := map[string]map[string]uint8{} //testutils is not meant for large data sets
	for _, recordA := range a {
		if _, exists := calculator[recordA.DNSName]; !exists {
			calculator[recordA.DNSName] = map[string]uint8{}
		}
		if _, exists := calculator[recordA.DNSName][recordA.Target]; !exists {
			calculator[recordA.DNSName][recordA.Target] = 0
		}
		calculator[recordA.DNSName][recordA.Target]++
	}
	for _, recordB := range b {
		if _, exists := calculator[recordB.DNSName]; !exists {
			return false
		}
		if _, exists := calculator[recordB.DNSName][recordB.Target]; !exists {
			return false
		}
		calculator[recordB.DNSName][recordB.Target]--
	}

	for _, byDNSName := range calculator {
		for _, byCounter := range byDNSName {
			if byCounter != 0 {
				return false
			}
		}
	}

	return true
}
