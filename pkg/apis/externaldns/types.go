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
	"fmt"
	"reflect"
	"strconv"
	"time"

	"github.com/alecthomas/kingpin"
	"github.com/sirupsen/logrus"

	"sigs.k8s.io/external-dns/source"
)

const (
	passwordMask = "******"
)

var (
	// Version is the current version of the app, generated at build time
	Version = "unknown"
)

// Config is a project-wide configuration
type Config struct {
	APIServerURL                      string
	KubeConfig                        string
	RequestTimeout                    time.Duration
	ContourLoadBalancerService        string
	SkipperRouteGroupVersion          string
	Sources                           []string
	Namespace                         string
	AnnotationFilter                  string
	LabelFilter                       string
	FQDNTemplate                      string
	CombineFQDNAndAnnotation          bool
	IgnoreHostnameAnnotation          bool
	IgnoreIngressTLSSpec              bool
	Compatibility                     string
	PublishInternal                   bool
	PublishHostIP                     bool
	AlwaysPublishNotReadyAddresses    bool
	ConnectorSourceServer             string
	Provider                          string
	GoogleProject                     string
	GoogleBatchChangeSize             int
	GoogleBatchChangeInterval         time.Duration
	DomainFilter                      []string
	ExcludeDomains                    []string
	ZoneNameFilter                    []string
	ZoneIDFilter                      []string
	AlibabaCloudConfigFile            string
	AlibabaCloudZoneType              string
	AWSZoneType                       string
	AWSZoneTagFilter                  []string
	AWSAssumeRole                     string
	AWSBatchChangeSize                int
	AWSBatchChangeInterval            time.Duration
	AWSEvaluateTargetHealth           bool
	AWSAPIRetries                     int
	AWSPreferCNAME                    bool
	AWSZoneCacheDuration              time.Duration
	AzureConfigFile                   string
	AzureResourceGroup                string
	AzureSubscriptionID               string
	AzureUserAssignedIdentityClientID string
	CloudflareProxied                 bool
	CloudflareZonesPerPage            int
	CoreDNSPrefix                     string
	RcodezeroTXTEncrypt               bool
	AkamaiServiceConsumerDomain       string
	AkamaiClientToken                 string
	AkamaiClientSecret                string
	AkamaiAccessToken                 string
	InfobloxGridHost                  string
	InfobloxWapiPort                  int
	InfobloxWapiUsername              string
	InfobloxWapiPassword              string `secure:"yes"`
	InfobloxWapiVersion               string
	InfobloxSSLVerify                 bool
	InfobloxView                      string
	InfobloxMaxResults                int
	DynCustomerName                   string
	DynUsername                       string
	DynPassword                       string `secure:"yes"`
	DynMinTTLSeconds                  int
	OCIConfigFile                     string
	InMemoryZones                     []string
	OVHEndpoint                       string
	OVHApiRateLimit                   int
	PDNSServer                        string
	PDNSAPIKey                        string `secure:"yes"`
	PDNSTLSEnabled                    bool
	TLSCA                             string
	TLSClientCert                     string
	TLSClientCertKey                  string
	Policy                            string
	Registry                          string
	TXTOwnerID                        string
	TXTPrefix                         string
	TXTSuffix                         string
	Interval                          time.Duration
	Once                              bool
	DryRun                            bool
	UpdateEvents                      bool
	LogFormat                         string
	MetricsAddress                    string
	LogLevel                          string
	TXTCacheInterval                  time.Duration
	ExoscaleEndpoint                  string
	ExoscaleAPIKey                    string `secure:"yes"`
	ExoscaleAPISecret                 string `secure:"yes"`
	CRDSourceAPIVersion               string
	CRDSourceKind                     string
	ServiceTypeFilter                 []string
	CFAPIEndpoint                     string
	CFUsername                        string
	CFPassword                        string
	RFC2136Host                       string
	RFC2136Port                       int
	RFC2136Zone                       string
	RFC2136Insecure                   bool
	RFC2136TSIGKeyName                string
	RFC2136TSIGSecret                 string `secure:"yes"`
	RFC2136TSIGSecretAlg              string
	RFC2136TAXFR                      bool
	RFC2136MinTTL                     time.Duration
	NS1Endpoint                       string
	NS1IgnoreSSL                      bool
	NS1MinTTLSeconds                  int
	TransIPAccountName                string
	TransIPPrivateKeyFile             string
	DigitalOceanAPIPageSize           int
}

var defaultConfig = &Config{
	APIServerURL:                "",
	KubeConfig:                  "",
	RequestTimeout:              time.Second * 30,
	ContourLoadBalancerService:  "heptio-contour/contour",
	SkipperRouteGroupVersion:    "zalando.org/v1",
	Sources:                     nil,
	Namespace:                   "",
	AnnotationFilter:            "",
	LabelFilter:                 "",
	FQDNTemplate:                "",
	CombineFQDNAndAnnotation:    false,
	IgnoreHostnameAnnotation:    false,
	IgnoreIngressTLSSpec:        false,
	Compatibility:               "",
	PublishInternal:             false,
	PublishHostIP:               false,
	ConnectorSourceServer:       "localhost:8080",
	Provider:                    "",
	GoogleProject:               "",
	GoogleBatchChangeSize:       1000,
	GoogleBatchChangeInterval:   time.Second,
	DomainFilter:                []string{},
	ExcludeDomains:              []string{},
	AlibabaCloudConfigFile:      "/etc/kubernetes/alibaba-cloud.json",
	AWSZoneType:                 "",
	AWSZoneTagFilter:            []string{},
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
	RcodezeroTXTEncrypt:         false,
	AkamaiServiceConsumerDomain: "",
	AkamaiClientToken:           "",
	AkamaiClientSecret:          "",
	AkamaiAccessToken:           "",
	InfobloxGridHost:            "",
	InfobloxWapiPort:            443,
	InfobloxWapiUsername:        "admin",
	InfobloxWapiPassword:        "",
	InfobloxWapiVersion:         "2.3.1",
	InfobloxSSLVerify:           true,
	InfobloxView:                "",
	InfobloxMaxResults:          0,
	OCIConfigFile:               "/etc/kubernetes/oci.yaml",
	InMemoryZones:               []string{},
	OVHEndpoint:                 "ovh-eu",
	OVHApiRateLimit:             20,
	PDNSServer:                  "http://localhost:8081",
	PDNSAPIKey:                  "",
	PDNSTLSEnabled:              false,
	TLSCA:                       "",
	TLSClientCert:               "",
	TLSClientCertKey:            "",
	Policy:                      "sync",
	Registry:                    "txt",
	TXTOwnerID:                  "default",
	TXTPrefix:                   "",
	TXTSuffix:                   "",
	TXTCacheInterval:            0,
	Interval:                    time.Minute,
	Once:                        false,
	DryRun:                      false,
	UpdateEvents:                false,
	LogFormat:                   "text",
	MetricsAddress:              ":7979",
	LogLevel:                    logrus.InfoLevel.String(),
	ExoscaleEndpoint:            "https://api.exoscale.ch/dns",
	ExoscaleAPIKey:              "",
	ExoscaleAPISecret:           "",
	CRDSourceAPIVersion:         "externaldns.k8s.io/v1alpha1",
	CRDSourceKind:               "DNSEndpoint",
	ServiceTypeFilter:           []string{},
	CFAPIEndpoint:               "",
	CFUsername:                  "",
	CFPassword:                  "",
	RFC2136Host:                 "",
	RFC2136Port:                 0,
	RFC2136Zone:                 "",
	RFC2136Insecure:             false,
	RFC2136TSIGKeyName:          "",
	RFC2136TSIGSecret:           "",
	RFC2136TSIGSecretAlg:        "",
	RFC2136TAXFR:                true,
	RFC2136MinTTL:               0,
	NS1Endpoint:                 "",
	NS1IgnoreSSL:                false,
	TransIPAccountName:          "",
	TransIPPrivateKeyFile:       "",
	DigitalOceanAPIPageSize:     50,
}

// NewConfig returns new Config object
func NewConfig() *Config {
	return &Config{}
}

func (cfg *Config) String() string {
	// prevent logging of sensitive information
	temp := *cfg

	t := reflect.TypeOf(temp)
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		if val, ok := f.Tag.Lookup("secure"); ok && val == "yes" {
			if f.Type.Kind() != reflect.String {
				continue
			}
			v := reflect.ValueOf(&temp).Elem().Field(i)
			if v.String() != "" {
				v.SetString(passwordMask)
			}
		}
	}

	return fmt.Sprintf("%+v", temp)
}

// allLogLevelsAsStrings returns all logrus levels as a list of strings
func allLogLevelsAsStrings() []string {
	var levels []string
	for _, level := range logrus.AllLevels {
		levels = append(levels, level.String())
	}
	return levels
}

// ParseFlags adds and parses flags from command line
func (cfg *Config) ParseFlags(args []string) error {
	app := kingpin.New("external-dns", "ExternalDNS synchronizes exposed Kubernetes Services and Ingresses with DNS providers.\n\nNote that all flags may be replaced with env vars - `--flag` -> `EXTERNAL_DNS_FLAG=1` or `--flag value` -> `EXTERNAL_DNS_FLAG=value`")
	app.Version(Version)
	app.DefaultEnvars()

	// Flags related to Kubernetes
	app.Flag("server", "The Kubernetes API server to connect to (default: auto-detect)").Default(defaultConfig.APIServerURL).StringVar(&cfg.APIServerURL)
	app.Flag("kubeconfig", "Retrieve target cluster configuration from a Kubernetes configuration file (default: auto-detect)").Default(defaultConfig.KubeConfig).StringVar(&cfg.KubeConfig)
	app.Flag("request-timeout", "Request timeout when calling Kubernetes APIs. 0s means no timeout").Default(defaultConfig.RequestTimeout.String()).DurationVar(&cfg.RequestTimeout)

	// Flags related to cloud foundry
	app.Flag("cf-api-endpoint", "The fully-qualified domain name of the cloud foundry instance you are targeting").Default(defaultConfig.CFAPIEndpoint).StringVar(&cfg.CFAPIEndpoint)
	app.Flag("cf-username", "The username to log into the cloud foundry API").Default(defaultConfig.CFUsername).StringVar(&cfg.CFUsername)
	app.Flag("cf-password", "The password to log into the cloud foundry API").Default(defaultConfig.CFPassword).StringVar(&cfg.CFPassword)

	// Flags related to Contour
	app.Flag("contour-load-balancer", "The fully-qualified name of the Contour load balancer service. (default: heptio-contour/contour)").Default("heptio-contour/contour").StringVar(&cfg.ContourLoadBalancerService)

	// Flags related to Skipper RouteGroup
	app.Flag("skipper-routegroup-groupversion", "The resource version for skipper routegroup").Default(source.DefaultRoutegroupVersion).StringVar(&cfg.SkipperRouteGroupVersion)

	// Flags related to processing sources
	app.Flag("source", "The resource types that are queried for endpoints; specify multiple times for multiple sources (required, options: service, ingress, node, fake, connector, istio-gateway, istio-virtualservice, cloudfoundry, contour-ingressroute, contour-httpproxy, crd, empty, skipper-routegroup,openshift-route)").Required().PlaceHolder("source").EnumsVar(&cfg.Sources, "service", "ingress", "node", "istio-gateway", "istio-virtualservice", "cloudfoundry", "contour-ingressroute", "contour-httpproxy", "fake", "connector", "crd", "empty", "skipper-routegroup", "openshift-route")

	app.Flag("namespace", "Limit sources of endpoints to a specific namespace (default: all namespaces)").Default(defaultConfig.Namespace).StringVar(&cfg.Namespace)
	app.Flag("annotation-filter", "Filter sources managed by external-dns via annotation using label selector semantics (default: all sources)").Default(defaultConfig.AnnotationFilter).StringVar(&cfg.AnnotationFilter)
	app.Flag("label-filter", "Filter sources managed by external-dns via label selector when listing all resources; currently only supported by source CRD").Default(defaultConfig.LabelFilter).StringVar(&cfg.LabelFilter)
	app.Flag("fqdn-template", "A templated string that's used to generate DNS names from sources that don't define a hostname themselves, or to add a hostname suffix when paired with the fake source (optional). Accepts comma separated list for multiple global FQDN.").Default(defaultConfig.FQDNTemplate).StringVar(&cfg.FQDNTemplate)
	app.Flag("combine-fqdn-annotation", "Combine FQDN template and Annotations instead of overwriting").BoolVar(&cfg.CombineFQDNAndAnnotation)
	app.Flag("ignore-hostname-annotation", "Ignore hostname annotation when generating DNS names, valid only when using fqdn-template is set (optional, default: false)").BoolVar(&cfg.IgnoreHostnameAnnotation)
	app.Flag("ignore-ingress-tls-spec", "Ignore tls spec section in ingresses resources, applicable only for ingress sources (optional, default: false)").BoolVar(&cfg.IgnoreIngressTLSSpec)
	app.Flag("compatibility", "Process annotation semantics from legacy implementations (optional, options: mate, molecule)").Default(defaultConfig.Compatibility).EnumVar(&cfg.Compatibility, "", "mate", "molecule")
	app.Flag("publish-internal-services", "Allow external-dns to publish DNS records for ClusterIP services (optional)").BoolVar(&cfg.PublishInternal)
	app.Flag("publish-host-ip", "Allow external-dns to publish host-ip for headless services (optional)").BoolVar(&cfg.PublishHostIP)
	app.Flag("always-publish-not-ready-addresses", "Always publish also not ready addresses for headless services (optional)").BoolVar(&cfg.AlwaysPublishNotReadyAddresses)
	app.Flag("connector-source-server", "The server to connect for connector source, valid only when using connector source").Default(defaultConfig.ConnectorSourceServer).StringVar(&cfg.ConnectorSourceServer)
	app.Flag("crd-source-apiversion", "API version of the CRD for crd source, e.g. `externaldns.k8s.io/v1alpha1`, valid only when using crd source").Default(defaultConfig.CRDSourceAPIVersion).StringVar(&cfg.CRDSourceAPIVersion)
	app.Flag("crd-source-kind", "Kind of the CRD for the crd source in API group and version specified by crd-source-apiversion").Default(defaultConfig.CRDSourceKind).StringVar(&cfg.CRDSourceKind)
	app.Flag("service-type-filter", "The service types to take care about (default: all, expected: ClusterIP, NodePort, LoadBalancer or ExternalName)").StringsVar(&cfg.ServiceTypeFilter)

	// Flags related to providers
	app.Flag("provider", "The DNS provider where the DNS records will be created (required, options: aws, aws-sd, google, azure, azure-dns, azure-private-dns, cloudflare, rcodezero, digitalocean, hetzner, dnsimple, akamai, infoblox, dyn, designate, coredns, skydns, inmemory, ovh, pdns, oci, exoscale, linode, rfc2136, ns1, transip, vinyldns, rdns, scaleway, vultr, ultradns, vercel)").Required().PlaceHolder("provider").EnumVar(&cfg.Provider, "aws", "aws-sd", "google", "azure", "azure-dns", "hetzner", "azure-private-dns", "alibabacloud", "cloudflare", "rcodezero", "digitalocean", "dnsimple", "akamai", "infoblox", "dyn", "designate", "coredns", "skydns", "inmemory", "ovh", "pdns", "oci", "exoscale", "linode", "rfc2136", "ns1", "transip", "vinyldns", "rdns", "scaleway", "vultr", "ultradns", "vercel")
	app.Flag("domain-filter", "Limit possible target zones by a domain suffix; specify multiple times for multiple domains (optional)").Default("").StringsVar(&cfg.DomainFilter)
	app.Flag("exclude-domains", "Exclude subdomains (optional)").Default("").StringsVar(&cfg.ExcludeDomains)
	app.Flag("zone-name-filter", "Filter target zones by zone domain (For now, only AzureDNS provider is using this flag); specify multiple times for multiple zones (optional)").Default("").StringsVar(&cfg.ZoneNameFilter)
	app.Flag("zone-id-filter", "Filter target zones by hosted zone id; specify multiple times for multiple zones (optional)").Default("").StringsVar(&cfg.ZoneIDFilter)
	app.Flag("google-project", "When using the Google provider, current project is auto-detected, when running on GCP. Specify other project with this. Must be specified when running outside GCP.").Default(defaultConfig.GoogleProject).StringVar(&cfg.GoogleProject)
	app.Flag("google-batch-change-size", "When using the Google provider, set the maximum number of changes that will be applied in each batch.").Default(strconv.Itoa(defaultConfig.GoogleBatchChangeSize)).IntVar(&cfg.GoogleBatchChangeSize)
	app.Flag("google-batch-change-interval", "When using the Google provider, set the interval between batch changes.").Default(defaultConfig.GoogleBatchChangeInterval.String()).DurationVar(&cfg.GoogleBatchChangeInterval)
	app.Flag("alibaba-cloud-config-file", "When using the Alibaba Cloud provider, specify the Alibaba Cloud configuration file (required when --provider=alibabacloud").Default(defaultConfig.AlibabaCloudConfigFile).StringVar(&cfg.AlibabaCloudConfigFile)
	app.Flag("alibaba-cloud-zone-type", "When using the Alibaba Cloud provider, filter for zones of this type (optional, options: public, private)").Default(defaultConfig.AlibabaCloudZoneType).EnumVar(&cfg.AlibabaCloudZoneType, "", "public", "private")
	app.Flag("aws-zone-type", "When using the AWS provider, filter for zones of this type (optional, options: public, private)").Default(defaultConfig.AWSZoneType).EnumVar(&cfg.AWSZoneType, "", "public", "private")
	app.Flag("aws-zone-tags", "When using the AWS provider, filter for zones with these tags").Default("").StringsVar(&cfg.AWSZoneTagFilter)
	app.Flag("aws-assume-role", "When using the AWS provider, assume this IAM role. Useful for hosted zones in another AWS account. Specify the full ARN, e.g. `arn:aws:iam::123455567:role/external-dns` (optional)").Default(defaultConfig.AWSAssumeRole).StringVar(&cfg.AWSAssumeRole)
	app.Flag("aws-batch-change-size", "When using the AWS provider, set the maximum number of changes that will be applied in each batch.").Default(strconv.Itoa(defaultConfig.AWSBatchChangeSize)).IntVar(&cfg.AWSBatchChangeSize)
	app.Flag("aws-batch-change-interval", "When using the AWS provider, set the interval between batch changes.").Default(defaultConfig.AWSBatchChangeInterval.String()).DurationVar(&cfg.AWSBatchChangeInterval)
	app.Flag("aws-evaluate-target-health", "When using the AWS provider, set whether to evaluate the health of a DNS target (default: enabled, disable with --no-aws-evaluate-target-health)").Default(strconv.FormatBool(defaultConfig.AWSEvaluateTargetHealth)).BoolVar(&cfg.AWSEvaluateTargetHealth)
	app.Flag("aws-api-retries", "When using the AWS provider, set the maximum number of retries for API calls before giving up.").Default(strconv.Itoa(defaultConfig.AWSAPIRetries)).IntVar(&cfg.AWSAPIRetries)
	app.Flag("aws-prefer-cname", "When using the AWS provider, prefer using CNAME instead of ALIAS (default: disabled)").BoolVar(&cfg.AWSPreferCNAME)
	app.Flag("aws-zones-cache-duration", "When using the AWS provider, set the zones list cache TTL (0s to disable).").Default(defaultConfig.AWSZoneCacheDuration.String()).DurationVar(&cfg.AWSZoneCacheDuration)
	app.Flag("azure-config-file", "When using the Azure provider, specify the Azure configuration file (required when --provider=azure").Default(defaultConfig.AzureConfigFile).StringVar(&cfg.AzureConfigFile)
	app.Flag("azure-resource-group", "When using the Azure provider, override the Azure resource group to use (required when --provider=azure-private-dns)").Default(defaultConfig.AzureResourceGroup).StringVar(&cfg.AzureResourceGroup)
	app.Flag("azure-subscription-id", "When using the Azure provider, specify the Azure configuration file (required when --provider=azure-private-dns)").Default(defaultConfig.AzureSubscriptionID).StringVar(&cfg.AzureSubscriptionID)
	app.Flag("azure-user-assigned-identity-client-id", "When using the Azure provider, override the client id of user assigned identity in config file (optional)").Default("").StringVar(&cfg.AzureUserAssignedIdentityClientID)
	app.Flag("cloudflare-proxied", "When using the Cloudflare provider, specify if the proxy mode must be enabled (default: disabled)").BoolVar(&cfg.CloudflareProxied)
	app.Flag("cloudflare-zones-per-page", "When using the Cloudflare provider, specify how many zones per page listed, max. possible 50 (default: 50)").Default(strconv.Itoa(defaultConfig.CloudflareZonesPerPage)).IntVar(&cfg.CloudflareZonesPerPage)
	app.Flag("coredns-prefix", "When using the CoreDNS provider, specify the prefix name").Default(defaultConfig.CoreDNSPrefix).StringVar(&cfg.CoreDNSPrefix)
	app.Flag("akamai-serviceconsumerdomain", "When using the Akamai provider, specify the base URL (required when --provider=akamai)").Default(defaultConfig.AkamaiServiceConsumerDomain).StringVar(&cfg.AkamaiServiceConsumerDomain)
	app.Flag("akamai-client-token", "When using the Akamai provider, specify the client token (required when --provider=akamai)").Default(defaultConfig.AkamaiClientToken).StringVar(&cfg.AkamaiClientToken)
	app.Flag("akamai-client-secret", "When using the Akamai provider, specify the client secret (required when --provider=akamai)").Default(defaultConfig.AkamaiClientSecret).StringVar(&cfg.AkamaiClientSecret)
	app.Flag("akamai-access-token", "When using the Akamai provider, specify the access token (required when --provider=akamai)").Default(defaultConfig.AkamaiAccessToken).StringVar(&cfg.AkamaiAccessToken)
	app.Flag("infoblox-grid-host", "When using the Infoblox provider, specify the Grid Manager host (required when --provider=infoblox)").Default(defaultConfig.InfobloxGridHost).StringVar(&cfg.InfobloxGridHost)
	app.Flag("infoblox-wapi-port", "When using the Infoblox provider, specify the WAPI port (default: 443)").Default(strconv.Itoa(defaultConfig.InfobloxWapiPort)).IntVar(&cfg.InfobloxWapiPort)
	app.Flag("infoblox-wapi-username", "When using the Infoblox provider, specify the WAPI username (default: admin)").Default(defaultConfig.InfobloxWapiUsername).StringVar(&cfg.InfobloxWapiUsername)
	app.Flag("infoblox-wapi-password", "When using the Infoblox provider, specify the WAPI password (required when --provider=infoblox)").Default(defaultConfig.InfobloxWapiPassword).StringVar(&cfg.InfobloxWapiPassword)
	app.Flag("infoblox-wapi-version", "When using the Infoblox provider, specify the WAPI version (default: 2.3.1)").Default(defaultConfig.InfobloxWapiVersion).StringVar(&cfg.InfobloxWapiVersion)
	app.Flag("infoblox-ssl-verify", "When using the Infoblox provider, specify whether to verify the SSL certificate (default: true, disable with --no-infoblox-ssl-verify)").Default(strconv.FormatBool(defaultConfig.InfobloxSSLVerify)).BoolVar(&cfg.InfobloxSSLVerify)
	app.Flag("infoblox-view", "DNS view (default: \"\")").Default(defaultConfig.InfobloxView).StringVar(&cfg.InfobloxView)
	app.Flag("infoblox-max-results", "Add _max_results as query parameter to the URL on all API requests. The default is 0 which means _max_results is not set and the default of the server is used.").Default(strconv.Itoa(defaultConfig.InfobloxMaxResults)).IntVar(&cfg.InfobloxMaxResults)
	app.Flag("dyn-customer-name", "When using the Dyn provider, specify the Customer Name").Default("").StringVar(&cfg.DynCustomerName)
	app.Flag("dyn-username", "When using the Dyn provider, specify the Username").Default("").StringVar(&cfg.DynUsername)
	app.Flag("dyn-password", "When using the Dyn provider, specify the pasword").Default("").StringVar(&cfg.DynPassword)
	app.Flag("dyn-min-ttl", "Minimal TTL (in seconds) for records. This value will be used if the provided TTL for a service/ingress is lower than this.").IntVar(&cfg.DynMinTTLSeconds)
	app.Flag("oci-config-file", "When using the OCI provider, specify the OCI configuration file (required when --provider=oci").Default(defaultConfig.OCIConfigFile).StringVar(&cfg.OCIConfigFile)
	app.Flag("rcodezero-txt-encrypt", "When using the Rcodezero provider with txt registry option, set if TXT rrs are encrypted (default: false)").Default(strconv.FormatBool(defaultConfig.RcodezeroTXTEncrypt)).BoolVar(&cfg.RcodezeroTXTEncrypt)
	app.Flag("inmemory-zone", "Provide a list of pre-configured zones for the inmemory provider; specify multiple times for multiple zones (optional)").Default("").StringsVar(&cfg.InMemoryZones)
	app.Flag("ovh-endpoint", "When using the OVH provider, specify the endpoint (default: ovh-eu)").Default(defaultConfig.OVHEndpoint).StringVar(&cfg.OVHEndpoint)
	app.Flag("ovh-api-rate-limit", "When using the OVH provider, specify the API request rate limit, X operations by seconds (default: 20)").Default(strconv.Itoa(defaultConfig.OVHApiRateLimit)).IntVar(&cfg.OVHApiRateLimit)
	app.Flag("pdns-server", "When using the PowerDNS/PDNS provider, specify the URL to the pdns server (required when --provider=pdns)").Default(defaultConfig.PDNSServer).StringVar(&cfg.PDNSServer)
	app.Flag("pdns-api-key", "When using the PowerDNS/PDNS provider, specify the API key to use to authorize requests (required when --provider=pdns)").Default(defaultConfig.PDNSAPIKey).StringVar(&cfg.PDNSAPIKey)
	app.Flag("pdns-tls-enabled", "When using the PowerDNS/PDNS provider, specify whether to use TLS (default: false, requires --tls-ca, optionally specify --tls-client-cert and --tls-client-cert-key)").Default(strconv.FormatBool(defaultConfig.PDNSTLSEnabled)).BoolVar(&cfg.PDNSTLSEnabled)
	app.Flag("ns1-endpoint", "When using the NS1 provider, specify the URL of the API endpoint to target (default: https://api.nsone.net/v1/)").Default(defaultConfig.NS1Endpoint).StringVar(&cfg.NS1Endpoint)
	app.Flag("ns1-ignoressl", "When using the NS1 provider, specify whether to verify the SSL certificate (default: false)").Default(strconv.FormatBool(defaultConfig.NS1IgnoreSSL)).BoolVar(&cfg.NS1IgnoreSSL)
	app.Flag("ns1-min-ttl", "Minimal TTL (in seconds) for records. This value will be used if the provided TTL for a service/ingress is lower than this.").IntVar(&cfg.NS1MinTTLSeconds)
	app.Flag("digitalocean-api-page-size", "Configure the page size used when querying the DigitalOcean API.").Default(strconv.Itoa(defaultConfig.DigitalOceanAPIPageSize)).IntVar(&cfg.DigitalOceanAPIPageSize)

	// Flags related to TLS communication
	app.Flag("tls-ca", "When using TLS communication, the path to the certificate authority to verify server communications (optionally specify --tls-client-cert for two-way TLS)").Default(defaultConfig.TLSCA).StringVar(&cfg.TLSCA)
	app.Flag("tls-client-cert", "When using TLS communication, the path to the certificate to present as a client (not required for TLS)").Default(defaultConfig.TLSClientCert).StringVar(&cfg.TLSClientCert)
	app.Flag("tls-client-cert-key", "When using TLS communication, the path to the certificate key to use with the client certificate (not required for TLS)").Default(defaultConfig.TLSClientCertKey).StringVar(&cfg.TLSClientCertKey)

	app.Flag("exoscale-endpoint", "Provide the endpoint for the Exoscale provider").Default(defaultConfig.ExoscaleEndpoint).StringVar(&cfg.ExoscaleEndpoint)
	app.Flag("exoscale-apikey", "Provide your API Key for the Exoscale provider").Default(defaultConfig.ExoscaleAPIKey).StringVar(&cfg.ExoscaleAPIKey)
	app.Flag("exoscale-apisecret", "Provide your API Secret for the Exoscale provider").Default(defaultConfig.ExoscaleAPISecret).StringVar(&cfg.ExoscaleAPISecret)

	// Flags related to RFC2136 provider
	app.Flag("rfc2136-host", "When using the RFC2136 provider, specify the host of the DNS server").Default(defaultConfig.RFC2136Host).StringVar(&cfg.RFC2136Host)
	app.Flag("rfc2136-port", "When using the RFC2136 provider, specify the port of the DNS server").Default(strconv.Itoa(defaultConfig.RFC2136Port)).IntVar(&cfg.RFC2136Port)
	app.Flag("rfc2136-zone", "When using the RFC2136 provider, specify the zone entry of the DNS server to use").Default(defaultConfig.RFC2136Zone).StringVar(&cfg.RFC2136Zone)
	app.Flag("rfc2136-insecure", "When using the RFC2136 provider, specify whether to attach TSIG or not (default: false, requires --rfc2136-tsig-keyname and rfc2136-tsig-secret)").Default(strconv.FormatBool(defaultConfig.RFC2136Insecure)).BoolVar(&cfg.RFC2136Insecure)
	app.Flag("rfc2136-tsig-keyname", "When using the RFC2136 provider, specify the TSIG key to attached to DNS messages (required when --rfc2136-insecure=false)").Default(defaultConfig.RFC2136TSIGKeyName).StringVar(&cfg.RFC2136TSIGKeyName)
	app.Flag("rfc2136-tsig-secret", "When using the RFC2136 provider, specify the TSIG (base64) value to attached to DNS messages (required when --rfc2136-insecure=false)").Default(defaultConfig.RFC2136TSIGSecret).StringVar(&cfg.RFC2136TSIGSecret)
	app.Flag("rfc2136-tsig-secret-alg", "When using the RFC2136 provider, specify the TSIG (base64) value to attached to DNS messages (required when --rfc2136-insecure=false)").Default(defaultConfig.RFC2136TSIGSecretAlg).StringVar(&cfg.RFC2136TSIGSecretAlg)
	app.Flag("rfc2136-tsig-axfr", "When using the RFC2136 provider, specify the TSIG (base64) value to attached to DNS messages (required when --rfc2136-insecure=false)").BoolVar(&cfg.RFC2136TAXFR)
	app.Flag("rfc2136-min-ttl", "When using the RFC2136 provider, specify minimal TTL (in duration format) for records. This value will be used if the provided TTL for a service/ingress is lower than this").Default(defaultConfig.RFC2136MinTTL.String()).DurationVar(&cfg.RFC2136MinTTL)

	// Flags related to TransIP provider
	app.Flag("transip-account", "When using the TransIP provider, specify the account name (required when --provider=transip)").Default(defaultConfig.TransIPAccountName).StringVar(&cfg.TransIPAccountName)
	app.Flag("transip-keyfile", "When using the TransIP provider, specify the path to the private key file (required when --provider=transip)").Default(defaultConfig.TransIPPrivateKeyFile).StringVar(&cfg.TransIPPrivateKeyFile)

	// Flags related to policies
	app.Flag("policy", "Modify how DNS records are synchronized between sources and providers (default: sync, options: sync, upsert-only, create-only)").Default(defaultConfig.Policy).EnumVar(&cfg.Policy, "sync", "upsert-only", "create-only")

	// Flags related to the registry
	app.Flag("registry", "The registry implementation to use to keep track of DNS record ownership (default: txt, options: txt, noop, aws-sd)").Default(defaultConfig.Registry).EnumVar(&cfg.Registry, "txt", "noop", "aws-sd")
	app.Flag("txt-owner-id", "When using the TXT registry, a name that identifies this instance of ExternalDNS (default: default)").Default(defaultConfig.TXTOwnerID).StringVar(&cfg.TXTOwnerID)
	app.Flag("txt-prefix", "When using the TXT registry, a custom string that's prefixed to each ownership DNS record (optional). Mutual exclusive with txt-suffix!").Default(defaultConfig.TXTPrefix).StringVar(&cfg.TXTPrefix)
	app.Flag("txt-suffix", "When using the TXT registry, a custom string that's suffixed to the host portion of each ownership DNS record (optional). Mutual exclusive with txt-prefix!").Default(defaultConfig.TXTSuffix).StringVar(&cfg.TXTSuffix)

	// Flags related to the main control loop
	app.Flag("txt-cache-interval", "The interval between cache synchronizations in duration format (default: disabled)").Default(defaultConfig.TXTCacheInterval.String()).DurationVar(&cfg.TXTCacheInterval)
	app.Flag("interval", "The interval between two consecutive synchronizations in duration format (default: 1m)").Default(defaultConfig.Interval.String()).DurationVar(&cfg.Interval)
	app.Flag("once", "When enabled, exits the synchronization loop after the first iteration (default: disabled)").BoolVar(&cfg.Once)
	app.Flag("dry-run", "When enabled, prints DNS record changes rather than actually performing them (default: disabled)").BoolVar(&cfg.DryRun)
	app.Flag("events", "When enabled, in addition to running every interval, the reconciliation loop will get triggered when supported sources change (default: disabled)").BoolVar(&cfg.UpdateEvents)

	// Miscellaneous flags
	app.Flag("log-format", "The format in which log messages are printed (default: text, options: text, json)").Default(defaultConfig.LogFormat).EnumVar(&cfg.LogFormat, "text", "json")
	app.Flag("metrics-address", "Specify where to serve the metrics and health check endpoint (default: :7979)").Default(defaultConfig.MetricsAddress).StringVar(&cfg.MetricsAddress)
	app.Flag("log-level", "Set the level of logging. (default: info, options: panic, debug, info, warning, error, fatal").Default(defaultConfig.LogLevel).EnumVar(&cfg.LogLevel, allLogLevelsAsStrings()...)

	_, err := app.Parse(args)
	if err != nil {
		return err
	}

	return nil
}
