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

package externaldns

import (
	"os"
	"strings"
	"testing"
	"time"

	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var (
	minimalConfig = &Config{
		APIServerURL:                "",
		KubeConfig:                  "",
		RequestTimeout:              time.Second * 30,
		ContourLoadBalancerService:  "heptio-contour/contour",
		SkipperRouteGroupVersion:    "zalando.org/v1",
		Sources:                     []string{"service"},
		Namespace:                   "",
		FQDNTemplate:                "",
		NodePortSelector:            "node-role.kubernetes.io/node=",
		Compatibility:               "",
		Provider:                    "google",
		GoogleProject:               "",
		GoogleBatchChangeSize:       1000,
		GoogleBatchChangeInterval:   time.Second,
		DomainFilter:                []string{""},
		ExcludeDomains:              []string{""},
		ZoneNameFilter:              []string{""},
		ZoneIDFilter:                []string{""},
		AlibabaCloudConfigFile:      "/etc/kubernetes/alibaba-cloud.json",
		AWSZoneType:                 "",
		AWSZoneTagFilter:            []string{""},
		AWSAssumeRole:               "",
		AWSBatchChangeSize:          1000,
		AWSBatchChangeInterval:      time.Second,
		AWSEvaluateTargetHealth:     true,
		AWSAPIRetries:               3,
		AWSPreferCNAME:              false,
		AWSZoneCacheDuration:        0 * time.Second,
		AzureConfigFile:             "/etc/kubernetes/azure.json",
		AzureResourceGroup:          "",
		AzureSubscriptionID:         "",
		CloudflareProxied:           false,
		CloudflareZonesPerPage:      50,
		CoreDNSPrefix:               "/skydns/",
		AkamaiServiceConsumerDomain: "",
		AkamaiClientToken:           "",
		AkamaiClientSecret:          "",
		AkamaiAccessToken:           "",
		InfobloxGridHost:            "",
		InfobloxWapiPort:            443,
		InfobloxWapiUsername:        "admin",
		InfobloxWapiPassword:        "",
		InfobloxWapiVersion:         "2.3.1",
		InfobloxView:                "",
		InfobloxSSLVerify:           true,
		InfobloxMaxResults:          0,
		OCIConfigFile:               "/etc/kubernetes/oci.yaml",
		InMemoryZones:               []string{""},
		OVHEndpoint:                 "ovh-eu",
		OVHApiRateLimit:             20,
		PDNSServer:                  "http://localhost:8081",
		PDNSAPIKey:                  "",
		Policy:                      "sync",
		Registry:                    "txt",
		TXTOwnerID:                  "default",
		TXTPrefix:                   "",
		TXTCacheInterval:            0,
		Interval:                    time.Minute,
		Once:                        false,
		DryRun:                      false,
		UpdateEvents:                false,
		LogFormat:                   "text",
		MetricsAddress:              ":7979",
		LogLevel:                    logrus.InfoLevel.String(),
		ConnectorSourceServer:       "localhost:8080",
		ExoscaleEndpoint:            "https://api.exoscale.ch/dns",
		ExoscaleAPIKey:              "",
		ExoscaleAPISecret:           "",
		CRDSourceAPIVersion:         "externaldns.k8s.io/v1alpha1",
		CRDSourceKind:               "DNSEndpoint",
		RcodezeroTXTEncrypt:         false,
		TransIPAccountName:          "",
		TransIPPrivateKeyFile:       "",
		DigitalOceanAPIPageSize:     50,
	}

	overriddenConfig = &Config{
		APIServerURL:                "http://127.0.0.1:8080",
		KubeConfig:                  "/some/path",
		RequestTimeout:              time.Second * 77,
		ContourLoadBalancerService:  "heptio-contour-other/contour-other",
		SkipperRouteGroupVersion:    "zalando.org/v2",
		Sources:                     []string{"service", "ingress", "connector"},
		Namespace:                   "namespace",
		IgnoreHostnameAnnotation:    true,
		IgnoreIngressTLSSpec:        true,
		FQDNTemplate:                "{{.Name}}.service.example.com",
		NodePortSelector:            "node-role.kubernetes.io/node=",
		Compatibility:               "mate",
		Provider:                    "google",
		GoogleProject:               "project",
		GoogleBatchChangeSize:       100,
		GoogleBatchChangeInterval:   time.Second * 2,
		DomainFilter:                []string{"example.org", "company.com"},
		ExcludeDomains:              []string{"xapi.example.org", "xapi.company.com"},
		ZoneNameFilter:              []string{"yapi.example.org", "yapi.company.com"},
		ZoneIDFilter:                []string{"/hostedzone/ZTST1", "/hostedzone/ZTST2"},
		AlibabaCloudConfigFile:      "/etc/kubernetes/alibaba-cloud.json",
		AWSZoneType:                 "private",
		AWSZoneTagFilter:            []string{"tag=foo"},
		AWSAssumeRole:               "some-other-role",
		AWSBatchChangeSize:          100,
		AWSBatchChangeInterval:      time.Second * 2,
		AWSEvaluateTargetHealth:     false,
		AWSAPIRetries:               13,
		AWSPreferCNAME:              true,
		AWSZoneCacheDuration:        10 * time.Second,
		AzureConfigFile:             "azure.json",
		AzureResourceGroup:          "arg",
		AzureSubscriptionID:         "arg",
		CloudflareProxied:           true,
		CloudflareZonesPerPage:      20,
		CoreDNSPrefix:               "/coredns/",
		AkamaiServiceConsumerDomain: "oooo-xxxxxxxxxxxxxxxx-xxxxxxxxxxxxxxxx.luna.akamaiapis.net",
		AkamaiClientToken:           "o184671d5307a388180fbf7f11dbdf46",
		AkamaiClientSecret:          "o184671d5307a388180fbf7f11dbdf46",
		AkamaiAccessToken:           "o184671d5307a388180fbf7f11dbdf46",
		InfobloxGridHost:            "127.0.0.1",
		InfobloxWapiPort:            8443,
		InfobloxWapiUsername:        "infoblox",
		InfobloxWapiPassword:        "infoblox",
		InfobloxWapiVersion:         "2.6.1",
		InfobloxView:                "internal",
		InfobloxSSLVerify:           false,
		InfobloxMaxResults:          2000,
		OCIConfigFile:               "oci.yaml",
		InMemoryZones:               []string{"example.org", "company.com"},
		OVHEndpoint:                 "ovh-ca",
		OVHApiRateLimit:             42,
		PDNSServer:                  "http://ns.example.com:8081",
		PDNSAPIKey:                  "some-secret-key",
		PDNSTLSEnabled:              true,
		TLSCA:                       "/path/to/ca.crt",
		TLSClientCert:               "/path/to/cert.pem",
		TLSClientCertKey:            "/path/to/key.pem",
		Policy:                      "upsert-only",
		Registry:                    "noop",
		TXTOwnerID:                  "owner-1",
		TXTPrefix:                   "associated-txt-record",
		TXTCacheInterval:            12 * time.Hour,
		Interval:                    10 * time.Minute,
		Once:                        true,
		DryRun:                      true,
		UpdateEvents:                true,
		LogFormat:                   "json",
		MetricsAddress:              "127.0.0.1:9099",
		LogLevel:                    logrus.DebugLevel.String(),
		ConnectorSourceServer:       "localhost:8081",
		ExoscaleEndpoint:            "https://api.foo.ch/dns",
		ExoscaleAPIKey:              "1",
		ExoscaleAPISecret:           "2",
		CRDSourceAPIVersion:         "test.k8s.io/v1alpha1",
		CRDSourceKind:               "Endpoint",
		RcodezeroTXTEncrypt:         true,
		NS1Endpoint:                 "https://api.example.com/v1",
		NS1IgnoreSSL:                true,
		TransIPAccountName:          "transip",
		TransIPPrivateKeyFile:       "/path/to/transip.key",
		DigitalOceanAPIPageSize:     100,
	}
)

func TestParseFlags(t *testing.T) {
	for _, ti := range []struct {
		title    string
		args     []string
		envVars  map[string]string
		expected *Config
	}{
		{
			title: "default config with minimal flags defined",
			args: []string{
				"--source=service",
				"--provider=google",
			},
			envVars:  map[string]string{},
			expected: minimalConfig,
		},
		{
			title: "override everything via flags",
			args: []string{
				"--server=http://127.0.0.1:8080",
				"--kubeconfig=/some/path",
				"--request-timeout=77s",
				"--contour-load-balancer=heptio-contour-other/contour-other",
				"--skipper-routegroup-groupversion=zalando.org/v2",
				"--source=service",
				"--source=ingress",
				"--source=connector",
				"--namespace=namespace",
				"--fqdn-template={{.Name}}.service.example.com",
				"--ignore-hostname-annotation",
				"--ignore-ingress-tls-spec",
				"--compatibility=mate",
				"--provider=google",
				"--google-project=project",
				"--google-batch-change-size=100",
				"--google-batch-change-interval=2s",
				"--azure-config-file=azure.json",
				"--azure-resource-group=arg",
				"--azure-subscription-id=arg",
				"--cloudflare-proxied",
				"--cloudflare-zones-per-page=20",
				"--coredns-prefix=/coredns/",
				"--akamai-serviceconsumerdomain=oooo-xxxxxxxxxxxxxxxx-xxxxxxxxxxxxxxxx.luna.akamaiapis.net",
				"--akamai-client-token=o184671d5307a388180fbf7f11dbdf46",
				"--akamai-client-secret=o184671d5307a388180fbf7f11dbdf46",
				"--akamai-access-token=o184671d5307a388180fbf7f11dbdf46",
				"--infoblox-grid-host=127.0.0.1",
				"--infoblox-wapi-port=8443",
				"--infoblox-wapi-username=infoblox",
				"--infoblox-wapi-password=infoblox",
				"--infoblox-wapi-version=2.6.1",
				"--infoblox-view=internal",
				"--infoblox-max-results=2000",
				"--inmemory-zone=example.org",
				"--inmemory-zone=company.com",
				"--ovh-endpoint=ovh-ca",
				"--ovh-api-rate-limit=42",
				"--pdns-server=http://ns.example.com:8081",
				"--pdns-api-key=some-secret-key",
				"--pdns-tls-enabled",
				"--oci-config-file=oci.yaml",
				"--tls-ca=/path/to/ca.crt",
				"--tls-client-cert=/path/to/cert.pem",
				"--tls-client-cert-key=/path/to/key.pem",
				"--no-infoblox-ssl-verify",
				"--domain-filter=example.org",
				"--domain-filter=company.com",
				"--exclude-domains=xapi.example.org",
				"--exclude-domains=xapi.company.com",
				"--zone-name-filter=yapi.example.org",
				"--zone-name-filter=yapi.company.com",
				"--zone-id-filter=/hostedzone/ZTST1",
				"--zone-id-filter=/hostedzone/ZTST2",
				"--aws-zone-type=private",
				"--aws-zone-tags=tag=foo",
				"--aws-assume-role=some-other-role",
				"--aws-batch-change-size=100",
				"--aws-batch-change-interval=2s",
				"--aws-api-retries=13",
				"--aws-prefer-cname",
				"--aws-zones-cache-duration=10s",
				"--no-aws-evaluate-target-health",
				"--policy=upsert-only",
				"--registry=noop",
				"--txt-owner-id=owner-1",
				"--txt-prefix=associated-txt-record",
				"--txt-cache-interval=12h",
				"--interval=10m",
				"--once",
				"--dry-run",
				"--events",
				"--log-format=json",
				"--metrics-address=127.0.0.1:9099",
				"--log-level=debug",
				"--connector-source-server=localhost:8081",
				"--exoscale-endpoint=https://api.foo.ch/dns",
				"--exoscale-apikey=1",
				"--exoscale-apisecret=2",
				"--crd-source-apiversion=test.k8s.io/v1alpha1",
				"--crd-source-kind=Endpoint",
				"--rcodezero-txt-encrypt",
				"--ns1-endpoint=https://api.example.com/v1",
				"--ns1-ignoressl",
				"--transip-account=transip",
				"--transip-keyfile=/path/to/transip.key",
				"--digitalocean-api-page-size=100",
			},
			envVars:  map[string]string{},
			expected: overriddenConfig,
		},
		{
			title: "override everything via environment variables",
			args:  []string{},
			envVars: map[string]string{
				"EXTERNAL_DNS_SERVER":                          "http://127.0.0.1:8080",
				"EXTERNAL_DNS_KUBECONFIG":                      "/some/path",
				"EXTERNAL_DNS_REQUEST_TIMEOUT":                 "77s",
				"EXTERNAL_DNS_CONTOUR_LOAD_BALANCER":           "heptio-contour-other/contour-other",
				"EXTERNAL_DNS_SKIPPER_ROUTEGROUP_GROUPVERSION": "zalando.org/v2",
				"EXTERNAL_DNS_SOURCE":                          "service\ningress\nconnector",
				"EXTERNAL_DNS_NAMESPACE":                       "namespace",
				"EXTERNAL_DNS_FQDN_TEMPLATE":                   "{{.Name}}.service.example.com",
				"EXTERNAL_DNS_IGNORE_HOSTNAME_ANNOTATION":      "1",
				"EXTERNAL_DNS_IGNORE_INGRESS_TLS_SPEC":         "1",
				"EXTERNAL_DNS_COMPATIBILITY":                   "mate",
				"EXTERNAL_DNS_PROVIDER":                        "google",
				"EXTERNAL_DNS_GOOGLE_PROJECT":                  "project",
				"EXTERNAL_DNS_GOOGLE_BATCH_CHANGE_SIZE":        "100",
				"EXTERNAL_DNS_GOOGLE_BATCH_CHANGE_INTERVAL":    "2s",
				"EXTERNAL_DNS_AZURE_CONFIG_FILE":               "azure.json",
				"EXTERNAL_DNS_AZURE_RESOURCE_GROUP":            "arg",
				"EXTERNAL_DNS_AZURE_SUBSCRIPTION_ID":           "arg",
				"EXTERNAL_DNS_CLOUDFLARE_PROXIED":              "1",
				"EXTERNAL_DNS_CLOUDFLARE_ZONES_PER_PAGE":       "20",
				"EXTERNAL_DNS_COREDNS_PREFIX":                  "/coredns/",
				"EXTERNAL_DNS_AKAMAI_SERVICECONSUMERDOMAIN":    "oooo-xxxxxxxxxxxxxxxx-xxxxxxxxxxxxxxxx.luna.akamaiapis.net",
				"EXTERNAL_DNS_AKAMAI_CLIENT_TOKEN":             "o184671d5307a388180fbf7f11dbdf46",
				"EXTERNAL_DNS_AKAMAI_CLIENT_SECRET":            "o184671d5307a388180fbf7f11dbdf46",
				"EXTERNAL_DNS_AKAMAI_ACCESS_TOKEN":             "o184671d5307a388180fbf7f11dbdf46",
				"EXTERNAL_DNS_INFOBLOX_GRID_HOST":              "127.0.0.1",
				"EXTERNAL_DNS_INFOBLOX_WAPI_PORT":              "8443",
				"EXTERNAL_DNS_INFOBLOX_WAPI_USERNAME":          "infoblox",
				"EXTERNAL_DNS_INFOBLOX_WAPI_PASSWORD":          "infoblox",
				"EXTERNAL_DNS_INFOBLOX_WAPI_VERSION":           "2.6.1",
				"EXTERNAL_DNS_INFOBLOX_VIEW":                   "internal",
				"EXTERNAL_DNS_INFOBLOX_SSL_VERIFY":             "0",
				"EXTERNAL_DNS_INFOBLOX_MAX_RESULTS":            "2000",
				"EXTERNAL_DNS_OCI_CONFIG_FILE":                 "oci.yaml",
				"EXTERNAL_DNS_INMEMORY_ZONE":                   "example.org\ncompany.com",
				"EXTERNAL_DNS_OVH_ENDPOINT":                    "ovh-ca",
				"EXTERNAL_DNS_OVH_API_RATE_LIMIT":              "42",
				"EXTERNAL_DNS_DOMAIN_FILTER":                   "example.org\ncompany.com",
				"EXTERNAL_DNS_EXCLUDE_DOMAINS":                 "xapi.example.org\nxapi.company.com",
				"EXTERNAL_DNS_PDNS_SERVER":                     "http://ns.example.com:8081",
				"EXTERNAL_DNS_PDNS_API_KEY":                    "some-secret-key",
				"EXTERNAL_DNS_PDNS_TLS_ENABLED":                "1",
				"EXTERNAL_DNS_RDNS_ROOT_DOMAIN":                "lb.rancher.cloud",
				"EXTERNAL_DNS_TLS_CA":                          "/path/to/ca.crt",
				"EXTERNAL_DNS_TLS_CLIENT_CERT":                 "/path/to/cert.pem",
				"EXTERNAL_DNS_TLS_CLIENT_CERT_KEY":             "/path/to/key.pem",
				"EXTERNAL_DNS_ZONE_NAME_FILTER":                "yapi.example.org\nyapi.company.com",
				"EXTERNAL_DNS_ZONE_ID_FILTER":                  "/hostedzone/ZTST1\n/hostedzone/ZTST2",
				"EXTERNAL_DNS_AWS_ZONE_TYPE":                   "private",
				"EXTERNAL_DNS_AWS_ZONE_TAGS":                   "tag=foo",
				"EXTERNAL_DNS_AWS_ASSUME_ROLE":                 "some-other-role",
				"EXTERNAL_DNS_AWS_BATCH_CHANGE_SIZE":           "100",
				"EXTERNAL_DNS_AWS_BATCH_CHANGE_INTERVAL":       "2s",
				"EXTERNAL_DNS_AWS_EVALUATE_TARGET_HEALTH":      "0",
				"EXTERNAL_DNS_AWS_API_RETRIES":                 "13",
				"EXTERNAL_DNS_AWS_PREFER_CNAME":                "true",
				"EXTERNAL_DNS_AWS_ZONES_CACHE_DURATION":        "10s",
				"EXTERNAL_DNS_POLICY":                          "upsert-only",
				"EXTERNAL_DNS_REGISTRY":                        "noop",
				"EXTERNAL_DNS_TXT_OWNER_ID":                    "owner-1",
				"EXTERNAL_DNS_TXT_PREFIX":                      "associated-txt-record",
				"EXTERNAL_DNS_TXT_CACHE_INTERVAL":              "12h",
				"EXTERNAL_DNS_INTERVAL":                        "10m",
				"EXTERNAL_DNS_ONCE":                            "1",
				"EXTERNAL_DNS_DRY_RUN":                         "1",
				"EXTERNAL_DNS_EVENTS":                          "1",
				"EXTERNAL_DNS_LOG_FORMAT":                      "json",
				"EXTERNAL_DNS_METRICS_ADDRESS":                 "127.0.0.1:9099",
				"EXTERNAL_DNS_LOG_LEVEL":                       "debug",
				"EXTERNAL_DNS_CONNECTOR_SOURCE_SERVER":         "localhost:8081",
				"EXTERNAL_DNS_EXOSCALE_ENDPOINT":               "https://api.foo.ch/dns",
				"EXTERNAL_DNS_EXOSCALE_APIKEY":                 "1",
				"EXTERNAL_DNS_EXOSCALE_APISECRET":              "2",
				"EXTERNAL_DNS_CRD_SOURCE_APIVERSION":           "test.k8s.io/v1alpha1",
				"EXTERNAL_DNS_CRD_SOURCE_KIND":                 "Endpoint",
				"EXTERNAL_DNS_RCODEZERO_TXT_ENCRYPT":           "1",
				"EXTERNAL_DNS_NS1_ENDPOINT":                    "https://api.example.com/v1",
				"EXTERNAL_DNS_NS1_IGNORESSL":                   "1",
				"EXTERNAL_DNS_TRANSIP_ACCOUNT":                 "transip",
				"EXTERNAL_DNS_TRANSIP_KEYFILE":                 "/path/to/transip.key",
				"EXTERNAL_DNS_DIGITALOCEAN_API_PAGE_SIZE":      "100",
			},
			expected: overriddenConfig,
		},
	} {
		t.Run(ti.title, func(t *testing.T) {
			originalEnv := setEnv(t, ti.envVars)
			defer func() { restoreEnv(t, originalEnv) }()

			cfg := NewConfig()
			require.NoError(t, cfg.ParseFlags(ti.args))
			assert.Equal(t, ti.expected, cfg)
		})
	}
}

// helper functions

func setEnv(t *testing.T, env map[string]string) map[string]string {
	originalEnv := map[string]string{}

	for k, v := range env {
		originalEnv[k] = os.Getenv(k)
		require.NoError(t, os.Setenv(k, v))
	}

	return originalEnv
}

func restoreEnv(t *testing.T, originalEnv map[string]string) {
	for k, v := range originalEnv {
		require.NoError(t, os.Setenv(k, v))
	}
}

func TestPasswordsNotLogged(t *testing.T) {
	cfg := Config{
		DynPassword:          "dyn-pass",
		InfobloxWapiPassword: "infoblox-pass",
		PDNSAPIKey:           "pdns-api-key",
		RFC2136TSIGSecret:    "tsig-secret",
	}

	s := cfg.String()

	assert.False(t, strings.Contains(s, "dyn-pass"))
	assert.False(t, strings.Contains(s, "infoblox-pass"))
	assert.False(t, strings.Contains(s, "pdns-api-key"))
	assert.False(t, strings.Contains(s, "tsig-secret"))
}
