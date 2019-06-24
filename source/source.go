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

package source

import (
	"context"
	"fmt"
	"math"
	"net"
	"strconv"
	"strings"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/util/wait"

	"sigs.k8s.io/external-dns/endpoint"
	"sigs.k8s.io/external-dns/internal/config"
)

const (
	// The annotation used for figuring out which controller is responsible
	controllerAnnotationKey = "external-dns.alpha.kubernetes.io/controller"
	// The annotation used for defining the desired hostname
	hostnameAnnotationKey = "external-dns.alpha.kubernetes.io/hostname"
	// The annotation used for specifying whether the public or private interface address is used
	accessAnnotationKey = "external-dns.alpha.kubernetes.io/access"
	// The annotation used for defining the desired ingress target
	targetAnnotationKey = "external-dns.alpha.kubernetes.io/target"
	// The annotation used for defining the desired DNS record TTL
	ttlAnnotationKey = "external-dns.alpha.kubernetes.io/ttl"
	// The annotation used for switching to the alias record types e. g. AWS Alias records instead of a normal CNAME
	aliasAnnotationKey = "external-dns.alpha.kubernetes.io/alias"
	// The annotation used for having publishHostIP use the ExternalIP of the Host Node
	externalIPAnnotationKey = "external-dns.alpha.kubernetes.io/use-external-host-ip"
	// The value of the controller annotation so that we feel responsible
	controllerAnnotationValue = "dns-controller"
	// The annotation used for defining the desired hostname
	internalHostnameAnnotationKey = "external-dns.alpha.kubernetes.io/internal-hostname"
)

// Provider-specific annotations
const (
	// The annotation used for determining if traffic will go through Cloudflare
	CloudflareProxiedKey = "external-dns.alpha.kubernetes.io/cloudflare-proxied"

	SetIdentifierKey = "external-dns.alpha.kubernetes.io/set-identifier"
)

const (
	ttlMinimum = 1
	ttlMaximum = math.MaxInt32
)

// Source defines the interface Endpoint sources should implement.
type Source interface {
	Endpoints(ctx context.Context) ([]*endpoint.Endpoint, error)
	// AddEventHandler adds an event handler that should be triggered if something in source changes
	AddEventHandler(context.Context, func())
}

func getTTLFromAnnotations(annotations map[string]string) (endpoint.TTL, error) {
	ttlNotConfigured := endpoint.TTL(0)
	ttlAnnotation, exists := annotations[ttlAnnotationKey]
	if !exists {
		return ttlNotConfigured, nil
	}
	ttlValue, err := parseTTL(ttlAnnotation)
	if err != nil {
		return ttlNotConfigured, fmt.Errorf("\"%v\" is not a valid TTL value", ttlAnnotation)
	}
	if ttlValue < ttlMinimum || ttlValue > ttlMaximum {
		return ttlNotConfigured, fmt.Errorf("TTL value must be between [%d, %d]", ttlMinimum, ttlMaximum)
	}
	return endpoint.TTL(ttlValue), nil
}

// parseTTL parses TTL from string, returning duration in seconds.
// parseTTL supports both integers like "600" and durations based
// on Go Duration like "10m", hence "600" and "10m" represent the same value.
//
// Note: for durations like "1.5s" the fraction is omitted (resulting in 1 second
// for the example).
func parseTTL(s string) (ttlSeconds int64, err error) {
	ttlDuration, err := time.ParseDuration(s)
	if err != nil {
		return strconv.ParseInt(s, 10, 64)
	}

	return int64(ttlDuration.Seconds()), nil
}

func getHostnamesFromAnnotations(annotations map[string]string) []string {
	hostnameAnnotation, exists := annotations[hostnameAnnotationKey]
	if !exists {
		return nil
	}
	return strings.Split(strings.Replace(hostnameAnnotation, " ", "", -1), ",")
}

func getAccessFromAnnotations(annotations map[string]string) string {
	return annotations[accessAnnotationKey]
}

func getInternalHostnamesFromAnnotations(annotations map[string]string) []string {
	internalHostnameAnnotation, exists := annotations[internalHostnameAnnotationKey]
	if !exists {
		return nil
	}
	return strings.Split(strings.Replace(internalHostnameAnnotation, " ", "", -1), ",")
}

func getAliasFromAnnotations(annotations map[string]string) bool {
	aliasAnnotation, exists := annotations[aliasAnnotationKey]
	return exists && aliasAnnotation == "true"
}

func getExternalIPFromAnnotations(annotations map[string]string) bool {
	aliasAnnotation, exists := annotations[externalIPAnnotationKey]
	return exists && aliasAnnotation == "true"
}

func getProviderSpecificAnnotations(annotations map[string]string) (endpoint.ProviderSpecific, string) {
	providerSpecificAnnotations := endpoint.ProviderSpecific{}

	v, exists := annotations[CloudflareProxiedKey]
	if exists {
		providerSpecificAnnotations = append(providerSpecificAnnotations, endpoint.ProviderSpecificProperty{
			Name:  CloudflareProxiedKey,
			Value: v,
		})
	}
	if getAliasFromAnnotations(annotations) {
		providerSpecificAnnotations = append(providerSpecificAnnotations, endpoint.ProviderSpecificProperty{
			Name:  "alias",
			Value: "true",
		})
	}
	setIdentifier := ""
	for k, v := range annotations {
		if k == SetIdentifierKey {
			setIdentifier = v
		} else if strings.HasPrefix(k, "external-dns.alpha.kubernetes.io/aws-") {
			attr := strings.TrimPrefix(k, "external-dns.alpha.kubernetes.io/aws-")
			providerSpecificAnnotations = append(providerSpecificAnnotations, endpoint.ProviderSpecificProperty{
				Name:  fmt.Sprintf("aws/%s", attr),
				Value: v,
			})
		} else if strings.HasPrefix(k, "external-dns.alpha.kubernetes.io/scw-") {
			attr := strings.TrimPrefix(k, "external-dns.alpha.kubernetes.io/scw-")
			providerSpecificAnnotations = append(providerSpecificAnnotations, endpoint.ProviderSpecificProperty{
				Name:  fmt.Sprintf("scw/%s", attr),
				Value: v,
			})
		}
	}
	return providerSpecificAnnotations, setIdentifier
}

// getTargetsFromTargetAnnotation gets endpoints from optional "target" annotation.
// Returns empty endpoints array if none are found.
func getTargetsFromTargetAnnotation(annotations map[string]string) endpoint.Targets {
	var targets endpoint.Targets

	// Get the desired hostname of the ingress from the annotation.
	targetAnnotation, exists := annotations[targetAnnotationKey]
	if exists && targetAnnotation != "" {
		// splits the hostname annotation and removes the trailing periods
		targetsList := strings.Split(strings.Replace(targetAnnotation, " ", "", -1), ",")
		for _, targetHostname := range targetsList {
			targetHostname = strings.TrimSuffix(targetHostname, ".")
			targets = append(targets, targetHostname)
		}
	}
	return targets
}

// suitableType returns the DNS resource record type suitable for the target.
// In this case type A for IPs and type CNAME for everything else.
func suitableType(target string) string {
	if net.ParseIP(target) != nil {
		return endpoint.RecordTypeA
	}
	return endpoint.RecordTypeCNAME
}

// endpointsForHostname returns the endpoint objects for each host-target combination.
func endpointsForHostname(hostname string, targets endpoint.Targets, ttl endpoint.TTL, providerSpecific endpoint.ProviderSpecific, setIdentifier string) []*endpoint.Endpoint {
	var endpoints []*endpoint.Endpoint

	var aTargets endpoint.Targets
	var cnameTargets endpoint.Targets

	for _, t := range targets {
		switch suitableType(t) {
		case endpoint.RecordTypeA:
			aTargets = append(aTargets, t)
		default:
			cnameTargets = append(cnameTargets, t)
		}
	}

	if len(aTargets) > 0 {
		epA := &endpoint.Endpoint{
			DNSName:          strings.TrimSuffix(hostname, "."),
			Targets:          aTargets,
			RecordTTL:        ttl,
			RecordType:       endpoint.RecordTypeA,
			Labels:           endpoint.NewLabels(),
			ProviderSpecific: providerSpecific,
			SetIdentifier:    setIdentifier,
		}
		endpoints = append(endpoints, epA)
	}

	if len(cnameTargets) > 0 {
		epCNAME := &endpoint.Endpoint{
			DNSName:          strings.TrimSuffix(hostname, "."),
			Targets:          cnameTargets,
			RecordTTL:        ttl,
			RecordType:       endpoint.RecordTypeCNAME,
			Labels:           endpoint.NewLabels(),
			ProviderSpecific: providerSpecific,
			SetIdentifier:    setIdentifier,
		}
		endpoints = append(endpoints, epCNAME)
	}

	return endpoints
}

func getLabelSelector(annotationFilter string) (labels.Selector, error) {
	labelSelector, err := metav1.ParseToLabelSelector(annotationFilter)
	if err != nil {
		return nil, err
	}
	return metav1.LabelSelectorAsSelector(labelSelector)
}

func matchLabelSelector(selector labels.Selector, srcAnnotations map[string]string) bool {
	annotations := labels.Set(srcAnnotations)
	return selector.Matches(annotations)
}

func poll(interval time.Duration, timeout time.Duration, condition wait.ConditionFunc) error {
	if config.FastPoll {
		time.Sleep(5 * time.Millisecond)

		ok, err := condition()

		if err != nil {
			return err
		}

		if ok {
			return nil
		}

		interval = 50 * time.Millisecond
		timeout = 10 * time.Second
	}

	return wait.Poll(interval, timeout, condition)
}
