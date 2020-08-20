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
	"net"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"

	"sigs.k8s.io/external-dns/endpoint"
)

type ServiceSuite struct {
	suite.Suite
	sc             Source
	fooWithTargets *v1.Service
}

func (suite *ServiceSuite) SetupTest() {
	fakeClient := fake.NewSimpleClientset()
	var err error

	suite.sc, err = NewServiceSource(
		fakeClient,
		"",
		"",
		"{{.Name}}",
		false,
		"",
		false,
		false,
		false,
		[]string{},
		false,
	)
	suite.fooWithTargets = &v1.Service{
		Spec: v1.ServiceSpec{
			Type: v1.ServiceTypeLoadBalancer,
		},
		ObjectMeta: metav1.ObjectMeta{
			Namespace:   "default",
			Name:        "foo-with-targets",
			Annotations: map[string]string{},
		},
		Status: v1.ServiceStatus{
			LoadBalancer: v1.LoadBalancerStatus{
				Ingress: []v1.LoadBalancerIngress{
					{IP: "8.8.8.8"},
					{Hostname: "foo"},
				},
			},
		},
	}

	suite.NoError(err, "should initialize service source")

	_, err = fakeClient.CoreV1().Services(suite.fooWithTargets.Namespace).Create(context.Background(), suite.fooWithTargets, metav1.CreateOptions{})
	suite.NoError(err, "should successfully create service")

}

func (suite *ServiceSuite) TestResourceLabelIsSet() {
	endpoints, _ := suite.sc.Endpoints(context.Background())
	for _, ep := range endpoints {
		suite.Equal("service/default/foo-with-targets", ep.Labels[endpoint.ResourceLabelKey], "should set correct resource label")
	}
}

func TestServiceSource(t *testing.T) {
	suite.Run(t, new(ServiceSuite))
	t.Run("Interface", testServiceSourceImplementsSource)
	t.Run("NewServiceSource", testServiceSourceNewServiceSource)
	t.Run("Endpoints", testServiceSourceEndpoints)
}

// testServiceSourceImplementsSource tests that serviceSource is a valid Source.
func testServiceSourceImplementsSource(t *testing.T) {
	assert.Implements(t, (*Source)(nil), new(serviceSource))
}

// testServiceSourceNewServiceSource tests that NewServiceSource doesn't return an error.
func testServiceSourceNewServiceSource(t *testing.T) {
	for _, ti := range []struct {
		title              string
		annotationFilter   string
		fqdnTemplate       string
		serviceTypesFilter []string
		expectError        bool
	}{
		{
			title:        "invalid template",
			expectError:  true,
			fqdnTemplate: "{{.Name",
		},
		{
			title:       "valid empty template",
			expectError: false,
		},
		{
			title:        "valid template",
			expectError:  false,
			fqdnTemplate: "{{.Name}}-{{.Namespace}}.ext-dns.test.com",
		},
		{
			title:            "non-empty annotation filter label",
			expectError:      false,
			annotationFilter: "kubernetes.io/ingress.class=nginx",
		},
		{
			title:              "non-empty service types filter",
			expectError:        false,
			serviceTypesFilter: []string{string(v1.ServiceTypeClusterIP)},
		},
	} {
		t.Run(ti.title, func(t *testing.T) {
			_, err := NewServiceSource(
				fake.NewSimpleClientset(),
				"",
				ti.annotationFilter,
				ti.fqdnTemplate,
				false,
				"",
				false,
				false,
				false,
				ti.serviceTypesFilter,
				false,
			)

			if ti.expectError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

// testServiceSourceEndpoints tests that various services generate the correct endpoints.
func testServiceSourceEndpoints(t *testing.T) {
	for _, tc := range []struct {
		title                    string
		targetNamespace          string
		annotationFilter         string
		svcNamespace             string
		svcName                  string
		svcType                  v1.ServiceType
		compatibility            string
		fqdnTemplate             string
		combineFQDNAndAnnotation bool
		ignoreHostnameAnnotation bool
		labels                   map[string]string
		annotations              map[string]string
		clusterIP                string
		ports                    []v1.ServicePort
		lbs                      []string
		serviceTypesFilter       []string
		expected                 []*endpoint.Endpoint
		expectError              bool
	}{
		{
			"no annotated services return no endpoints",
			"",
			"",
			"testing",
			"foo",
			v1.ServiceTypeLoadBalancer,
			"",
			"",
			false,
			false,
			map[string]string{},
			map[string]string{},
			"",
			[]v1.ServicePort{{Port: 8888, NodePort: 30192}},
			[]string{"1.2.3.4"},
			[]string{},
			[]*endpoint.Endpoint{},
			false,
		},
		{
			"no annotated services return no endpoints when ignoreing annotations",
			"",
			"",
			"testing",
			"foo",
			v1.ServiceTypeLoadBalancer,
			"",
			"",
			false,
			true,
			map[string]string{},
			map[string]string{},
			"",
			[]v1.ServicePort{{Port: 8888, NodePort: 30192}},
			[]string{"1.2.3.4"},
			[]string{},
			[]*endpoint.Endpoint{},
			false,
		},
		{
			"annotated services return an endpoint with target IP",
			"",
			"",
			"testing",
			"foo",
			v1.ServiceTypeLoadBalancer,
			"",
			"",
			false,
			false,
			map[string]string{},
			map[string]string{
				hostnameAnnotationKey: "foo.example.org.",
			},
			"",
			[]v1.ServicePort{{Port: 8888, NodePort: 30192}},
			[]string{"1.2.3.4"},
			[]string{},
			[]*endpoint.Endpoint{
				{DNSName: "foo.example.org", Targets: endpoint.Targets{"1.2.3.4"}},
			},
			false,
		},
		{
			"hostname annotation on services is ignored",
			"",
			"",
			"testing",
			"foo",
			v1.ServiceTypeLoadBalancer,
			"",
			"",
			false,
			true,
			map[string]string{},
			map[string]string{
				hostnameAnnotationKey: "foo.example.org.",
			},
			"",
			[]v1.ServicePort{{Port: 8888, NodePort: 30192}},
			[]string{"1.2.3.4"},
			[]string{},
			[]*endpoint.Endpoint{},
			false,
		},
		{
			"annotated ClusterIp aren't processed without explicit authorization",
			"",
			"",
			"testing",
			"foo",
			v1.ServiceTypeClusterIP,
			"",
			"",
			false,
			false,
			map[string]string{},
			map[string]string{
				hostnameAnnotationKey: "foo.example.org.",
			},
			"1.2.3.4",
			[]v1.ServicePort{{Port: 8888}},
			[]string{},
			[]string{},
			[]*endpoint.Endpoint{},
			false,
		},
		{
			"FQDN template with multiple hostnames return an endpoint with target IP",
			"",
			"",
			"testing",
			"foo",
			v1.ServiceTypeLoadBalancer,
			"",
			"{{.Name}}.fqdn.org,{{.Name}}.fqdn.com",
			false,
			false,
			map[string]string{},
			map[string]string{},
			"",
			[]v1.ServicePort{{Port: 8888, NodePort: 30192}},
			[]string{"1.2.3.4"},
			[]string{},
			[]*endpoint.Endpoint{
				{DNSName: "foo.fqdn.org", Targets: endpoint.Targets{"1.2.3.4"}},
				{DNSName: "foo.fqdn.com", Targets: endpoint.Targets{"1.2.3.4"}},
			},
			false,
		},
		{
			"FQDN template with multiple hostnames return an endpoint with target IP when ignoreing annotations",
			"",
			"",
			"testing",
			"foo",
			v1.ServiceTypeLoadBalancer,
			"",
			"{{.Name}}.fqdn.org,{{.Name}}.fqdn.com",
			false,
			true,
			map[string]string{},
			map[string]string{},
			"",
			[]v1.ServicePort{{Port: 8888, NodePort: 30192}},
			[]string{"1.2.3.4"},
			[]string{},
			[]*endpoint.Endpoint{
				{DNSName: "foo.fqdn.org", Targets: endpoint.Targets{"1.2.3.4"}},
				{DNSName: "foo.fqdn.com", Targets: endpoint.Targets{"1.2.3.4"}},
			},
			false,
		},
		{
			"FQDN template and annotation both with multiple hostnames return an endpoint with target IP",
			"",
			"",
			"testing",
			"foo",
			v1.ServiceTypeLoadBalancer,
			"",
			"{{.Name}}.fqdn.org,{{.Name}}.fqdn.com",
			true,
			false,
			map[string]string{},
			map[string]string{
				hostnameAnnotationKey: "foo.example.org., bar.example.org.",
			},
			"",
			[]v1.ServicePort{{Port: 8888, NodePort: 30192}},
			[]string{"1.2.3.4"},
			[]string{},
			[]*endpoint.Endpoint{
				{DNSName: "foo.example.org", Targets: endpoint.Targets{"1.2.3.4"}},
				{DNSName: "bar.example.org", Targets: endpoint.Targets{"1.2.3.4"}},
				{DNSName: "foo.fqdn.org", Targets: endpoint.Targets{"1.2.3.4"}},
				{DNSName: "foo.fqdn.com", Targets: endpoint.Targets{"1.2.3.4"}},
			},
			false,
		},
		{
			"FQDN template and annotation both with multiple hostnames while ignoring annotations will only return FQDN endpoints",
			"",
			"",
			"testing",
			"foo",
			v1.ServiceTypeLoadBalancer,
			"",
			"{{.Name}}.fqdn.org,{{.Name}}.fqdn.com",
			true,
			true,
			map[string]string{},
			map[string]string{
				hostnameAnnotationKey: "foo.example.org., bar.example.org.",
			},
			"",
			[]v1.ServicePort{{Port: 8888, NodePort: 30192}},
			[]string{"1.2.3.4"},
			[]string{},
			[]*endpoint.Endpoint{
				{DNSName: "foo.fqdn.org", Targets: endpoint.Targets{"1.2.3.4"}},
				{DNSName: "foo.fqdn.com", Targets: endpoint.Targets{"1.2.3.4"}},
			},
			false,
		},
		{
			"annotated services with multiple hostnames return an endpoint with target IP",
			"",
			"",
			"testing",
			"foo",
			v1.ServiceTypeLoadBalancer,
			"",
			"",
			false,
			false,
			map[string]string{},
			map[string]string{
				hostnameAnnotationKey: "foo.example.org., bar.example.org.",
			},
			"",
			[]v1.ServicePort{{Port: 8888, NodePort: 30192}},
			[]string{"1.2.3.4"},
			[]string{},
			[]*endpoint.Endpoint{
				{DNSName: "foo.example.org", Targets: endpoint.Targets{"1.2.3.4"}},
				{DNSName: "bar.example.org", Targets: endpoint.Targets{"1.2.3.4"}},
			},
			false,
		},
		{
			"annotated services with multiple hostnames and without trailing period return an endpoint with target IP",
			"",
			"",
			"testing",
			"foo",
			v1.ServiceTypeLoadBalancer,
			"",
			"",
			false,
			false,
			map[string]string{},
			map[string]string{
				hostnameAnnotationKey: "foo.example.org, bar.example.org",
			},
			"",
			[]v1.ServicePort{{Port: 8888, NodePort: 30192}},
			[]string{"1.2.3.4"},
			[]string{},
			[]*endpoint.Endpoint{
				{DNSName: "foo.example.org", Targets: endpoint.Targets{"1.2.3.4"}},
				{DNSName: "bar.example.org", Targets: endpoint.Targets{"1.2.3.4"}},
			},
			false,
		},
		{
			"annotated services return an endpoint with target hostname",
			"",
			"",
			"testing",
			"foo",
			v1.ServiceTypeLoadBalancer,
			"",
			"",
			false,
			false,
			map[string]string{},
			map[string]string{
				hostnameAnnotationKey: "foo.example.org.",
			},
			"",
			[]v1.ServicePort{{Port: 8888, NodePort: 30192}},
			[]string{"lb.example.com"}, // Kubernetes omits the trailing dot
			[]string{},
			[]*endpoint.Endpoint{
				{DNSName: "foo.example.org", Targets: endpoint.Targets{"lb.example.com"}},
			},
			false,
		},
		{
			"annotated services can omit trailing dot",
			"",
			"",
			"testing",
			"foo",
			v1.ServiceTypeLoadBalancer,
			"",
			"",
			false,
			false,
			map[string]string{},
			map[string]string{
				hostnameAnnotationKey: "foo.example.org", // Trailing dot is omitted
			},
			"",
			[]v1.ServicePort{{Port: 8888, NodePort: 30192}},
			[]string{"1.2.3.4", "lb.example.com"}, // Kubernetes omits the trailing dot
			[]string{},
			[]*endpoint.Endpoint{
				{DNSName: "foo.example.org", Targets: endpoint.Targets{"1.2.3.4"}},
				{DNSName: "foo.example.org", Targets: endpoint.Targets{"lb.example.com"}},
			},
			false,
		},
		{
			"our controller type is dns-controller",
			"",
			"",
			"testing",
			"foo",
			v1.ServiceTypeLoadBalancer,
			"",
			"",
			false,
			false,
			map[string]string{},
			map[string]string{
				controllerAnnotationKey: controllerAnnotationValue,
				hostnameAnnotationKey:   "foo.example.org.",
			},
			"",
			[]v1.ServicePort{{Port: 8888, NodePort: 30192}},
			[]string{"1.2.3.4"},
			[]string{},
			[]*endpoint.Endpoint{
				{DNSName: "foo.example.org", Targets: endpoint.Targets{"1.2.3.4"}},
			},
			false,
		},
		{
			"different controller types are ignored even (with template specified)",
			"",
			"",
			"testing",
			"foo",
			v1.ServiceTypeLoadBalancer,
			"",
			"{{.Name}}.ext-dns.test.com",
			false,
			false,
			map[string]string{},
			map[string]string{
				controllerAnnotationKey: "some-other-tool",
				hostnameAnnotationKey:   "foo.example.org.",
			},
			"",
			[]v1.ServicePort{{Port: 8888, NodePort: 30192}},
			[]string{"1.2.3.4"},
			[]string{},
			[]*endpoint.Endpoint{},
			false,
		},
		{
			"services are found in target namespace",
			"testing",
			"",
			"testing",
			"foo",
			v1.ServiceTypeLoadBalancer,
			"",
			"",
			false,
			false,
			map[string]string{},
			map[string]string{
				hostnameAnnotationKey: "foo.example.org.",
			},
			"",
			[]v1.ServicePort{{Port: 8888, NodePort: 30192}},
			[]string{"1.2.3.4"},
			[]string{},
			[]*endpoint.Endpoint{
				{DNSName: "foo.example.org", Targets: endpoint.Targets{"1.2.3.4"}},
			},
			false,
		},
		{
			"services that are not in target namespace are ignored",
			"testing",
			"",
			"other-testing",
			"foo",
			v1.ServiceTypeLoadBalancer,
			"",
			"",
			false,
			false,
			map[string]string{},
			map[string]string{
				hostnameAnnotationKey: "foo.example.org.",
			},
			"",
			[]v1.ServicePort{{Port: 8888, NodePort: 30192}},
			[]string{"1.2.3.4"},
			[]string{},
			[]*endpoint.Endpoint{},
			false,
		},
		{
			"services are found in all namespaces",
			"",
			"",
			"other-testing",
			"foo",
			v1.ServiceTypeLoadBalancer,
			"",
			"",
			false,
			false,
			map[string]string{},
			map[string]string{
				hostnameAnnotationKey: "foo.example.org.",
			},
			"",
			[]v1.ServicePort{{Port: 8888, NodePort: 30192}},
			[]string{"1.2.3.4"},
			[]string{},
			[]*endpoint.Endpoint{
				{DNSName: "foo.example.org", Targets: endpoint.Targets{"1.2.3.4"}},
			},
			false,
		},
		{
			"valid matching annotation filter expression",
			"",
			"service.beta.kubernetes.io/external-traffic in (Global, OnlyLocal)",
			"testing",
			"foo",
			v1.ServiceTypeLoadBalancer,
			"",
			"",
			false,
			false,
			map[string]string{},
			map[string]string{
				hostnameAnnotationKey:                         "foo.example.org.",
				"service.beta.kubernetes.io/external-traffic": "OnlyLocal",
			},
			"",
			[]v1.ServicePort{{Port: 8888, NodePort: 30192}},
			[]string{"1.2.3.4"},
			[]string{},
			[]*endpoint.Endpoint{
				{DNSName: "foo.example.org", Targets: endpoint.Targets{"1.2.3.4"}},
			},
			false,
		},
		{
			"valid non-matching annotation filter expression",
			"",
			"service.beta.kubernetes.io/external-traffic in (Global, OnlyLocal)",
			"testing",
			"foo",
			v1.ServiceTypeLoadBalancer,
			"",
			"",
			false,
			false,
			map[string]string{},
			map[string]string{
				hostnameAnnotationKey:                         "foo.example.org.",
				"service.beta.kubernetes.io/external-traffic": "SomethingElse",
			},
			"",
			[]v1.ServicePort{{Port: 8888, NodePort: 30192}},
			[]string{"1.2.3.4"},
			[]string{},
			[]*endpoint.Endpoint{},
			false,
		},
		{
			"invalid annotation filter expression",
			"",
			"service.beta.kubernetes.io/external-traffic in (Global OnlyLocal)",
			"testing",
			"foo",
			v1.ServiceTypeLoadBalancer,
			"",
			"",
			false,
			false,
			map[string]string{},
			map[string]string{
				hostnameAnnotationKey:                         "foo.example.org.",
				"service.beta.kubernetes.io/external-traffic": "OnlyLocal",
			},
			"",
			[]v1.ServicePort{{Port: 8888, NodePort: 30192}},
			[]string{"1.2.3.4"},
			[]string{},
			[]*endpoint.Endpoint{},
			true,
		},
		{
			"valid matching annotation filter label",
			"",
			"service.beta.kubernetes.io/external-traffic=Global",
			"testing",
			"foo",
			v1.ServiceTypeLoadBalancer,
			"",
			"",
			false,
			false,
			map[string]string{},
			map[string]string{
				hostnameAnnotationKey:                         "foo.example.org.",
				"service.beta.kubernetes.io/external-traffic": "Global",
			},
			"",
			[]v1.ServicePort{{Port: 8888, NodePort: 30192}},
			[]string{"1.2.3.4"},
			[]string{},
			[]*endpoint.Endpoint{
				{DNSName: "foo.example.org", Targets: endpoint.Targets{"1.2.3.4"}},
			},
			false,
		},
		{
			"valid non-matching annotation filter label",
			"",
			"service.beta.kubernetes.io/external-traffic=Global",
			"testing",
			"foo",
			v1.ServiceTypeLoadBalancer,
			"",
			"",
			false,
			false,
			map[string]string{},
			map[string]string{
				hostnameAnnotationKey:                         "foo.example.org.",
				"service.beta.kubernetes.io/external-traffic": "OnlyLocal",
			},
			"",
			[]v1.ServicePort{{Port: 8888, NodePort: 30192}},
			[]string{"1.2.3.4"},
			[]string{},
			[]*endpoint.Endpoint{},
			false,
		},
		{
			"no external entrypoints return no endpoints",
			"",
			"",
			"testing",
			"foo",
			v1.ServiceTypeLoadBalancer,
			"",
			"",
			false,
			false,
			map[string]string{},
			map[string]string{
				hostnameAnnotationKey: "foo.example.org.",
			},
			"",
			[]v1.ServicePort{{Port: 8888, NodePort: 30192}},
			[]string{},
			[]string{},
			[]*endpoint.Endpoint{},
			false,
		},
		{
			"multiple external entrypoints return a single endpoint with multiple targets",
			"",
			"",
			"testing",
			"foo",
			v1.ServiceTypeLoadBalancer,
			"",
			"",
			false,
			false,
			map[string]string{},
			map[string]string{
				hostnameAnnotationKey: "foo.example.org.",
			},
			"",
			[]v1.ServicePort{{Port: 8888, NodePort: 30192}},
			[]string{"1.2.3.4", "8.8.8.8"},
			[]string{},
			[]*endpoint.Endpoint{
				{DNSName: "foo.example.org", Targets: endpoint.Targets{"1.2.3.4", "8.8.8.8"}},
			},
			false,
		},
		{
			"multiple named ports return a single endpoint and one SRV endpoints for each named port",
			"",
			"",
			"testing",
			"foo",
			v1.ServiceTypeLoadBalancer,
			"",
			"",
			false,
			false,
			map[string]string{},
			map[string]string{
				hostnameAnnotationKey: "foo.example.org.",
			},
			"",
			[]v1.ServicePort{
				{Name: "http", Port: 8888, NodePort: 30192},
				{Name: "https", Port: 8889, NodePort: 30193, Protocol: "UDP"},
				{Port: 8890, NodePort: 30194},
			},
			[]string{"1.2.3.4"},
			[]string{},
			[]*endpoint.Endpoint{
				{DNSName: "_http._tcp.foo.example.org", Targets: endpoint.Targets{"0 50 30192 foo.example.org"}, RecordType: endpoint.RecordTypeSRV},
				{DNSName: "_https._udp.foo.example.org", Targets: endpoint.Targets{"0 50 30193 foo.example.org"}, RecordType: endpoint.RecordTypeSRV},
				{DNSName: "foo.example.org", Targets: endpoint.Targets{"1.2.3.4"}},
			},
			false,
		},
		{
			"services annotated with legacy mate annotations are ignored in default mode",
			"",
			"",
			"testing",
			"foo",
			v1.ServiceTypeLoadBalancer,
			"",
			"",
			false,
			false,
			map[string]string{},
			map[string]string{
				"zalando.org/dnsname": "foo.example.org.",
			},
			"",
			[]v1.ServicePort{{Port: 8888, NodePort: 30192}},
			[]string{"1.2.3.4"},
			[]string{},
			[]*endpoint.Endpoint{},
			false,
		},
		{
			"services annotated with legacy mate annotations return an endpoint in compatibility mode",
			"",
			"",
			"testing",
			"foo",
			v1.ServiceTypeLoadBalancer,
			"mate",
			"",
			false,
			false,
			map[string]string{},
			map[string]string{
				"zalando.org/dnsname": "foo.example.org.",
			},
			"",
			[]v1.ServicePort{{Port: 8888, NodePort: 30192}},
			[]string{"1.2.3.4"},
			[]string{},
			[]*endpoint.Endpoint{
				{DNSName: "foo.example.org", Targets: endpoint.Targets{"1.2.3.4"}},
			},
			false,
		},
		{
			"services annotated with legacy molecule annotations return an endpoint in compatibility mode",
			"",
			"",
			"testing",
			"foo",
			v1.ServiceTypeLoadBalancer,
			"molecule",
			"",
			false,
			false,
			map[string]string{
				"dns": "route53",
			},
			map[string]string{
				"domainName": "foo.example.org., bar.example.org",
			},
			"",
			[]v1.ServicePort{{Port: 8888, NodePort: 30192}},
			[]string{"1.2.3.4"},
			[]string{},
			[]*endpoint.Endpoint{
				{DNSName: "foo.example.org", Targets: endpoint.Targets{"1.2.3.4"}},
				{DNSName: "bar.example.org", Targets: endpoint.Targets{"1.2.3.4"}},
			},
			false,
		},
		{
			"not annotated services with set fqdnTemplate return an endpoint with target IP",
			"",
			"",
			"testing",
			"foo",
			v1.ServiceTypeLoadBalancer,
			"",
			"{{.Name}}.bar.example.com",
			false,
			false,
			map[string]string{},
			map[string]string{},
			"",
			[]v1.ServicePort{{Port: 8888, NodePort: 30192}},
			[]string{"1.2.3.4", "elb.com"},
			[]string{},
			[]*endpoint.Endpoint{
				{DNSName: "foo.bar.example.com", Targets: endpoint.Targets{"1.2.3.4"}},
				{DNSName: "foo.bar.example.com", Targets: endpoint.Targets{"elb.com"}},
			},
			false,
		},
		{
			"annotated services with set fqdnTemplate annotation takes precedence",
			"",
			"",
			"testing",
			"foo",
			v1.ServiceTypeLoadBalancer,
			"",
			"{{.Name}}.bar.example.com",
			false,
			false,
			map[string]string{},
			map[string]string{
				hostnameAnnotationKey: "foo.example.org.",
			},
			"",
			[]v1.ServicePort{{Port: 8888, NodePort: 30192}},
			[]string{"1.2.3.4", "elb.com"},
			[]string{},
			[]*endpoint.Endpoint{
				{DNSName: "foo.example.org", Targets: endpoint.Targets{"1.2.3.4"}},
				{DNSName: "foo.example.org", Targets: endpoint.Targets{"elb.com"}},
			},
			false,
		},
		{
			"compatibility annotated services with tmpl. compatibility takes precedence",
			"",
			"",
			"testing",
			"foo",
			v1.ServiceTypeLoadBalancer,
			"mate",
			"{{.Name}}.bar.example.com",
			false,
			false,
			map[string]string{},
			map[string]string{
				"zalando.org/dnsname": "mate.example.org.",
			},
			"",
			[]v1.ServicePort{{Port: 8888, NodePort: 30192}},
			[]string{"1.2.3.4"},
			[]string{},
			[]*endpoint.Endpoint{
				{DNSName: "mate.example.org", Targets: endpoint.Targets{"1.2.3.4"}},
			},
			false,
		},
		{
			"not annotated services with unknown tmpl field should not return anything",
			"",
			"",
			"testing",
			"foo",
			v1.ServiceTypeLoadBalancer,
			"",
			"{{.Calibre}}.bar.example.com",
			false,
			false,
			map[string]string{},
			map[string]string{},
			"",
			[]v1.ServicePort{{Port: 8888, NodePort: 30192}},
			[]string{"1.2.3.4"},
			[]string{},
			[]*endpoint.Endpoint{},
			true,
		},
		{
			"ttl not annotated should have RecordTTL.IsConfigured set to false",
			"",
			"",
			"testing",
			"foo",
			v1.ServiceTypeLoadBalancer,
			"",
			"",
			false,
			false,
			map[string]string{},
			map[string]string{
				hostnameAnnotationKey: "foo.example.org.",
			},
			"",
			[]v1.ServicePort{{Port: 8888, NodePort: 30192}},
			[]string{"1.2.3.4"},
			[]string{},
			[]*endpoint.Endpoint{
				{DNSName: "foo.example.org", Targets: endpoint.Targets{"1.2.3.4"}, RecordTTL: endpoint.TTL(0)},
			},
			false,
		},
		{
			"ttl annotated but invalid should have RecordTTL.IsConfigured set to false",
			"",
			"",
			"testing",
			"foo",
			v1.ServiceTypeLoadBalancer,
			"",
			"",
			false,
			false,
			map[string]string{},
			map[string]string{
				hostnameAnnotationKey: "foo.example.org.",
				ttlAnnotationKey:      "foo",
			},
			"",
			[]v1.ServicePort{{Port: 8888, NodePort: 30192}},
			[]string{"1.2.3.4"},
			[]string{},
			[]*endpoint.Endpoint{
				{DNSName: "foo.example.org", Targets: endpoint.Targets{"1.2.3.4"}, RecordTTL: endpoint.TTL(0)},
			},
			false,
		},
		{
			"ttl annotated and is valid should set Record.TTL",
			"",
			"",
			"testing",
			"foo",
			v1.ServiceTypeLoadBalancer,
			"",
			"",
			false,
			false,
			map[string]string{},
			map[string]string{
				hostnameAnnotationKey: "foo.example.org.",
				ttlAnnotationKey:      "10",
			},
			"",
			[]v1.ServicePort{{Port: 8888, NodePort: 30192}},
			[]string{"1.2.3.4"},
			[]string{},
			[]*endpoint.Endpoint{
				{DNSName: "foo.example.org", Targets: endpoint.Targets{"1.2.3.4"}, RecordTTL: endpoint.TTL(10)},
			},
			false,
		},
		{
			"ttl annotated (in duration format) and is valid should set Record.TTL",
			"",
			"",
			"testing",
			"foo",
			v1.ServiceTypeLoadBalancer,
			"",
			"",
			false,
			false,
			map[string]string{},
			map[string]string{
				hostnameAnnotationKey: "foo.example.org.",
				ttlAnnotationKey:      "1m",
			},
			"",
			[]v1.ServicePort{{Port: 8888, NodePort: 30192}},
			[]string{"1.2.3.4"},
			[]string{},
			[]*endpoint.Endpoint{
				{DNSName: "foo.example.org", Targets: endpoint.Targets{"1.2.3.4"}, RecordTTL: endpoint.TTL(60)},
			},
			false,
		},
		{
			"Negative ttl is not valid",
			"",
			"",
			"testing",
			"foo",
			v1.ServiceTypeLoadBalancer,
			"",
			"",
			false,
			false,
			map[string]string{},
			map[string]string{
				hostnameAnnotationKey: "foo.example.org.",
				ttlAnnotationKey:      "-10",
			},
			"",
			[]v1.ServicePort{{Port: 8888, NodePort: 30192}},
			[]string{"1.2.3.4"},
			[]string{},
			[]*endpoint.Endpoint{
				{DNSName: "foo.example.org", Targets: endpoint.Targets{"1.2.3.4"}, RecordTTL: endpoint.TTL(0)},
			},
			false,
		},
		{
			"filter on service types should include matching services",
			"",
			"",
			"testing",
			"foo",
			v1.ServiceTypeLoadBalancer,
			"",
			"",
			false,
			false,
			map[string]string{},
			map[string]string{
				hostnameAnnotationKey: "foo.example.org.",
			},
			"",
			[]v1.ServicePort{{Port: 8888, NodePort: 30192}},
			[]string{"1.2.3.4"},
			[]string{string(v1.ServiceTypeLoadBalancer)},
			[]*endpoint.Endpoint{
				{DNSName: "foo.example.org", Targets: endpoint.Targets{"1.2.3.4"}},
			},
			false,
		},
		{
			"filter on service types should exclude non-matching services",
			"",
			"",
			"testing",
			"foo",
			v1.ServiceTypeNodePort,
			"",
			"",
			false,
			false,
			map[string]string{},
			map[string]string{
				hostnameAnnotationKey: "foo.example.org.",
			},
			"",
			[]v1.ServicePort{{Port: 8888, NodePort: 30192}},
			[]string{"1.2.3.4"},
			[]string{string(v1.ServiceTypeLoadBalancer)},
			[]*endpoint.Endpoint{},
			false,
		},
	} {
		t.Run(tc.title, func(t *testing.T) {
			// Create a Kubernetes testing client
			kubernetes := fake.NewSimpleClientset()

			// Create a service to test against
			ingresses := []v1.LoadBalancerIngress{}
			for _, lb := range tc.lbs {
				if net.ParseIP(lb) != nil {
					ingresses = append(ingresses, v1.LoadBalancerIngress{IP: lb})
				} else {
					ingresses = append(ingresses, v1.LoadBalancerIngress{Hostname: lb})
				}
			}

			service := &v1.Service{
				Spec: v1.ServiceSpec{
					Type:      tc.svcType,
					ClusterIP: tc.clusterIP,
					Ports:     tc.ports,
				},
				ObjectMeta: metav1.ObjectMeta{
					Namespace:   tc.svcNamespace,
					Name:        tc.svcName,
					Labels:      tc.labels,
					Annotations: tc.annotations,
				},
				Status: v1.ServiceStatus{
					LoadBalancer: v1.LoadBalancerStatus{
						Ingress: ingresses,
					},
				},
			}

			_, err := kubernetes.CoreV1().Services(service.Namespace).Create(context.Background(), service, metav1.CreateOptions{})
			require.NoError(t, err)

			// Create our object under test and get the endpoints.
			client, _ := NewServiceSource(
				kubernetes,
				tc.targetNamespace,
				tc.annotationFilter,
				tc.fqdnTemplate,
				tc.combineFQDNAndAnnotation,
				tc.compatibility,
				false,
				false,
				false,
				tc.serviceTypesFilter,
				tc.ignoreHostnameAnnotation,
			)
			require.NoError(t, err)

			var res []*endpoint.Endpoint

			// wait up to a few seconds for new resources to appear in informer cache.
			err = poll(time.Second, 3*time.Second, func() (bool, error) {
				res, err = client.Endpoints(context.Background())
				if err != nil {
					// stop waiting if we get an error
					return true, err
				}
				return len(res) >= len(tc.expected), nil
			})

			if tc.expectError {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}

			// Validate returned endpoints against desired endpoints.
			validateEndpoints(t, res, tc.expected)
		})
	}
}

// testServiceSourceEndpoints tests that various services generate the correct endpoints.
func TestClusterIpServices(t *testing.T) {
	for _, tc := range []struct {
		title                    string
		targetNamespace          string
		annotationFilter         string
		svcNamespace             string
		svcName                  string
		svcType                  v1.ServiceType
		compatibility            string
		fqdnTemplate             string
		ignoreHostnameAnnotation bool
		labels                   map[string]string
		annotations              map[string]string
		clusterIP                string
		ports                    []v1.ServicePort
		lbs                      []string
		expected                 []*endpoint.Endpoint
		expectError              bool
	}{
		{
			"annotated ClusterIp services return an endpoint with Cluster IP",
			"",
			"",
			"testing",
			"foo",
			v1.ServiceTypeClusterIP,
			"",
			"",
			false,
			map[string]string{},
			map[string]string{
				hostnameAnnotationKey: "foo.example.org.",
			},
			"1.2.3.4",
			[]v1.ServicePort{{Port: 8888}},
			[]string{},
			[]*endpoint.Endpoint{
				{DNSName: "foo.example.org", Targets: endpoint.Targets{"1.2.3.4"}},
			},
			false,
		},
		{
			"hostname annotated ClusterIp services are ignored",
			"",
			"",
			"testing",
			"foo",
			v1.ServiceTypeClusterIP,
			"",
			"",
			true,
			map[string]string{},
			map[string]string{
				hostnameAnnotationKey: "foo.example.org.",
			},
			"1.2.3.4",
			[]v1.ServicePort{{Port: 8888}},
			[]string{},
			[]*endpoint.Endpoint{},
			false,
		},
		{
			"non-annotated ClusterIp services with set fqdnTemplate return an endpoint with target IP",
			"",
			"",
			"testing",
			"foo",
			v1.ServiceTypeClusterIP,
			"",
			"{{.Name}}.bar.example.com",
			false,
			map[string]string{},
			map[string]string{},
			"4.5.6.7",
			[]v1.ServicePort{{Port: 8888}},
			[]string{},
			[]*endpoint.Endpoint{
				{DNSName: "foo.bar.example.com", Targets: endpoint.Targets{"4.5.6.7"}},
			},
			false,
		},
		{
			"Headless services do not generate endpoints",
			"",
			"",
			"testing",
			"foo",
			v1.ServiceTypeClusterIP,
			"",
			"",
			false,
			map[string]string{},
			map[string]string{},
			v1.ClusterIPNone,
			[]v1.ServicePort{{Port: 8888}},
			[]string{},
			[]*endpoint.Endpoint{},
			false,
		},
		{
			"annotated ClusterIp services with multiple named ports return an endpoint with Cluster IP and multiple SRV endpoints",
			"",
			"",
			"testing",
			"foo",
			v1.ServiceTypeClusterIP,
			"",
			"",
			false,
			map[string]string{},
			map[string]string{
				hostnameAnnotationKey: "foo.example.org.",
			},
			"1.2.3.4",
			[]v1.ServicePort{
				{Name: "http", Port: 8888},
				{Name: "https", Port: 8889, Protocol: "UDP"},
				{Port: 8890},
			},
			[]string{},
			[]*endpoint.Endpoint{
				{DNSName: "_http._tcp.foo.example.org", Targets: endpoint.Targets{"0 50 8888 foo.example.org"}, RecordType: endpoint.RecordTypeSRV},
				{DNSName: "_https._udp.foo.example.org", Targets: endpoint.Targets{"0 50 8889 foo.example.org"}, RecordType: endpoint.RecordTypeSRV},
				{DNSName: "foo.example.org", Targets: endpoint.Targets{"1.2.3.4"}},
			},
			false,
		},
	} {
		t.Run(tc.title, func(t *testing.T) {
			// Create a Kubernetes testing client
			kubernetes := fake.NewSimpleClientset()

			// Create a service to test against
			ingresses := []v1.LoadBalancerIngress{}
			for _, lb := range tc.lbs {
				if net.ParseIP(lb) != nil {
					ingresses = append(ingresses, v1.LoadBalancerIngress{IP: lb})
				} else {
					ingresses = append(ingresses, v1.LoadBalancerIngress{Hostname: lb})
				}
			}

			service := &v1.Service{
				Spec: v1.ServiceSpec{
					Type:      tc.svcType,
					ClusterIP: tc.clusterIP,
					Ports:     tc.ports,
				},
				ObjectMeta: metav1.ObjectMeta{
					Namespace:   tc.svcNamespace,
					Name:        tc.svcName,
					Labels:      tc.labels,
					Annotations: tc.annotations,
				},
				Status: v1.ServiceStatus{
					LoadBalancer: v1.LoadBalancerStatus{
						Ingress: ingresses,
					},
				},
			}

			_, err := kubernetes.CoreV1().Services(service.Namespace).Create(context.Background(), service, metav1.CreateOptions{})
			require.NoError(t, err)

			// Create our object under test and get the endpoints.
			client, _ := NewServiceSource(
				kubernetes,
				tc.targetNamespace,
				tc.annotationFilter,
				tc.fqdnTemplate,
				false,
				tc.compatibility,
				true,
				false,
				false,
				[]string{},
				tc.ignoreHostnameAnnotation,
			)
			require.NoError(t, err)

			endpoints, err := client.Endpoints(context.Background())
			if tc.expectError {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}

			// Validate returned endpoints against desired endpoints.
			validateEndpoints(t, endpoints, tc.expected)
		})
	}
}

// testNodePortServices tests that various services generate the correct endpoints.
func TestNodePortServices(t *testing.T) {
	for _, tc := range []struct {
		title                    string
		targetNamespace          string
		annotationFilter         string
		svcNamespace             string
		svcName                  string
		svcType                  v1.ServiceType
		svcTrafficPolicy         v1.ServiceExternalTrafficPolicyType
		compatibility            string
		fqdnTemplate             string
		ignoreHostnameAnnotation bool
		labels                   map[string]string
		annotations              map[string]string
		ports                    []v1.ServicePort
		lbs                      []string
		expected                 []*endpoint.Endpoint
		expectError              bool
		nodes                    []*v1.Node
		podnames                 []string
		nodeIndex                []int
		phases                   []v1.PodPhase
	}{
		{
			"annotated NodePort services return an endpoint with IP addresses of the cluster's nodes",
			"",
			"",
			"testing",
			"foo",
			v1.ServiceTypeNodePort,
			v1.ServiceExternalTrafficPolicyTypeCluster,
			"",
			"",
			false,
			map[string]string{},
			map[string]string{
				hostnameAnnotationKey: "foo.example.org.",
			},
			[]v1.ServicePort{{Name: "http", Port: 8888, NodePort: 30192}},
			nil,
			[]*endpoint.Endpoint{
				{DNSName: "_http._tcp.foo.example.org", Targets: endpoint.Targets{"0 50 30192 foo.example.org"}, RecordType: endpoint.RecordTypeSRV},
				{DNSName: "foo.example.org", Targets: endpoint.Targets{"54.10.11.1", "54.10.11.2"}, RecordType: endpoint.RecordTypeA},
			},
			false,
			[]*v1.Node{{
				ObjectMeta: metav1.ObjectMeta{
					Name: "node1",
				},
				Status: v1.NodeStatus{
					Addresses: []v1.NodeAddress{
						{Type: v1.NodeExternalIP, Address: "54.10.11.1"},
						{Type: v1.NodeInternalIP, Address: "10.0.1.1"},
					},
				},
			}, {
				ObjectMeta: metav1.ObjectMeta{
					Name: "node2",
				},
				Status: v1.NodeStatus{
					Addresses: []v1.NodeAddress{
						{Type: v1.NodeExternalIP, Address: "54.10.11.2"},
						{Type: v1.NodeInternalIP, Address: "10.0.1.2"},
					},
				},
			}},
			[]string{},
			[]int{},
			[]v1.PodPhase{},
		},
		{
			"hostname annotated NodePort services are ignored",
			"",
			"",
			"testing",
			"foo",
			v1.ServiceTypeNodePort,
			v1.ServiceExternalTrafficPolicyTypeCluster,
			"",
			"",
			true,
			map[string]string{},
			map[string]string{
				hostnameAnnotationKey: "foo.example.org.",
			},
			[]v1.ServicePort{{Name: "http", Port: 8888, NodePort: 30192}},
			nil,
			[]*endpoint.Endpoint{},
			false,
			[]*v1.Node{{
				ObjectMeta: metav1.ObjectMeta{
					Name: "node1",
				},
				Status: v1.NodeStatus{
					Addresses: []v1.NodeAddress{
						{Type: v1.NodeExternalIP, Address: "54.10.11.1"},
						{Type: v1.NodeInternalIP, Address: "10.0.1.1"},
					},
				},
			}, {
				ObjectMeta: metav1.ObjectMeta{
					Name: "node2",
				},
				Status: v1.NodeStatus{
					Addresses: []v1.NodeAddress{
						{Type: v1.NodeExternalIP, Address: "54.10.11.2"},
						{Type: v1.NodeInternalIP, Address: "10.0.1.2"},
					},
				},
			}},
			[]string{},
			[]int{},
			[]v1.PodPhase{},
		},
		{
			"non-annotated NodePort services with set fqdnTemplate return an endpoint with target IP",
			"",
			"",
			"testing",
			"foo",
			v1.ServiceTypeNodePort,
			v1.ServiceExternalTrafficPolicyTypeCluster,
			"",
			"{{.Name}}.bar.example.com",
			false,
			map[string]string{},
			map[string]string{},
			[]v1.ServicePort{{Name: "http", Port: 8888, NodePort: 30192}},
			nil,
			[]*endpoint.Endpoint{
				{DNSName: "_http._tcp.foo.bar.example.com", Targets: endpoint.Targets{"0 50 30192 foo.bar.example.com"}, RecordType: endpoint.RecordTypeSRV},
				{DNSName: "foo.bar.example.com", Targets: endpoint.Targets{"54.10.11.1", "54.10.11.2"}, RecordType: endpoint.RecordTypeA},
			},
			false,
			[]*v1.Node{{
				ObjectMeta: metav1.ObjectMeta{
					Name: "node1",
				},
				Status: v1.NodeStatus{
					Addresses: []v1.NodeAddress{
						{Type: v1.NodeExternalIP, Address: "54.10.11.1"},
						{Type: v1.NodeInternalIP, Address: "10.0.1.1"},
					},
				},
			}, {
				ObjectMeta: metav1.ObjectMeta{
					Name: "node2",
				},
				Status: v1.NodeStatus{
					Addresses: []v1.NodeAddress{
						{Type: v1.NodeExternalIP, Address: "54.10.11.2"},
						{Type: v1.NodeInternalIP, Address: "10.0.1.2"},
					},
				},
			}},
			[]string{},
			[]int{},
			[]v1.PodPhase{},
		},
		{
			"annotated NodePort services return an endpoint with IP addresses of the private cluster's nodes",
			"",
			"",
			"testing",
			"foo",
			v1.ServiceTypeNodePort,
			v1.ServiceExternalTrafficPolicyTypeCluster,
			"",
			"",
			false,
			map[string]string{},
			map[string]string{
				hostnameAnnotationKey: "foo.example.org.",
			},
			[]v1.ServicePort{{Name: "http", Port: 8888, NodePort: 30192}},
			nil,
			[]*endpoint.Endpoint{
				{DNSName: "_http._tcp.foo.example.org", Targets: endpoint.Targets{"0 50 30192 foo.example.org"}, RecordType: endpoint.RecordTypeSRV},
				{DNSName: "foo.example.org", Targets: endpoint.Targets{"10.0.1.1", "10.0.1.2"}, RecordType: endpoint.RecordTypeA},
			},
			false,
			[]*v1.Node{{
				ObjectMeta: metav1.ObjectMeta{
					Name: "node1",
				},
				Status: v1.NodeStatus{
					Addresses: []v1.NodeAddress{
						{Type: v1.NodeInternalIP, Address: "10.0.1.1"},
					},
				},
			}, {
				ObjectMeta: metav1.ObjectMeta{
					Name: "node2",
				},
				Status: v1.NodeStatus{
					Addresses: []v1.NodeAddress{
						{Type: v1.NodeInternalIP, Address: "10.0.1.2"},
					},
				},
			}},
			[]string{},
			[]int{},
			[]v1.PodPhase{},
		},
		{
			"annotated NodePort services with ExternalTrafficPolicy=Local return an endpoint with IP addresses of the cluster's nodes where pods is running only",
			"",
			"",
			"testing",
			"foo",
			v1.ServiceTypeNodePort,
			v1.ServiceExternalTrafficPolicyTypeLocal,
			"",
			"",
			false,
			map[string]string{},
			map[string]string{
				hostnameAnnotationKey: "foo.example.org.",
			},
			[]v1.ServicePort{{Name: "http", Port: 8888, NodePort: 30192}},
			nil,
			[]*endpoint.Endpoint{
				{DNSName: "_http._tcp.foo.example.org", Targets: endpoint.Targets{"0 50 30192 foo.example.org"}, RecordType: endpoint.RecordTypeSRV},
				{DNSName: "foo.example.org", Targets: endpoint.Targets{"54.10.11.2"}, RecordType: endpoint.RecordTypeA},
			},
			false,
			[]*v1.Node{{
				ObjectMeta: metav1.ObjectMeta{
					Name: "node1",
				},
				Status: v1.NodeStatus{
					Addresses: []v1.NodeAddress{
						{Type: v1.NodeExternalIP, Address: "54.10.11.1"},
						{Type: v1.NodeInternalIP, Address: "10.0.1.1"},
					},
				},
			}, {
				ObjectMeta: metav1.ObjectMeta{
					Name: "node2",
				},
				Status: v1.NodeStatus{
					Addresses: []v1.NodeAddress{
						{Type: v1.NodeExternalIP, Address: "54.10.11.2"},
						{Type: v1.NodeInternalIP, Address: "10.0.1.2"},
					},
				},
			}},
			[]string{"pod-0"},
			[]int{1},
			[]v1.PodPhase{v1.PodRunning},
		},
		{
			"annotated NodePort services with multiple named ports return an endpoint with IP addresses of the cluster's nodes and multiple SRV endpoints",
			"",
			"",
			"testing",
			"foo",
			v1.ServiceTypeNodePort,
			v1.ServiceExternalTrafficPolicyTypeCluster,
			"",
			"",
			false,
			map[string]string{},
			map[string]string{
				hostnameAnnotationKey: "foo.example.org.",
			},
			[]v1.ServicePort{
				{Name: "http", Port: 8888, NodePort: 30192},
				{Name: "https", Port: 8889, NodePort: 30193, Protocol: "UDP"},
				{Port: 8890, NodePort: 30194},
			},
			nil,
			[]*endpoint.Endpoint{
				{DNSName: "_http._tcp.foo.example.org", Targets: endpoint.Targets{"0 50 30192 foo.example.org"}, RecordType: endpoint.RecordTypeSRV},
				{DNSName: "_https._udp.foo.example.org", Targets: endpoint.Targets{"0 50 30193 foo.example.org"}, RecordType: endpoint.RecordTypeSRV},
				{DNSName: "foo.example.org", Targets: endpoint.Targets{"54.10.11.1", "54.10.11.2"}, RecordType: endpoint.RecordTypeA},
			},
			false,
			[]*v1.Node{{
				ObjectMeta: metav1.ObjectMeta{
					Name: "node1",
				},
				Status: v1.NodeStatus{
					Addresses: []v1.NodeAddress{
						{Type: v1.NodeExternalIP, Address: "54.10.11.1"},
						{Type: v1.NodeInternalIP, Address: "10.0.1.1"},
					},
				},
			}, {
				ObjectMeta: metav1.ObjectMeta{
					Name: "node2",
				},
				Status: v1.NodeStatus{
					Addresses: []v1.NodeAddress{
						{Type: v1.NodeExternalIP, Address: "54.10.11.2"},
						{Type: v1.NodeInternalIP, Address: "10.0.1.2"},
					},
				},
			}},
			[]string{},
			[]int{},
			[]v1.PodPhase{},
		},
	} {
		t.Run(tc.title, func(t *testing.T) {
			// Create a Kubernetes testing client
			kubernetes := fake.NewSimpleClientset()

			// Create the nodes
			for _, node := range tc.nodes {
				if _, err := kubernetes.CoreV1().Nodes().Create(context.Background(), node, metav1.CreateOptions{}); err != nil {
					t.Fatal(err)
				}
			}

			// Create  pods
			for i, podname := range tc.podnames {
				pod := &v1.Pod{
					Spec: v1.PodSpec{
						Containers: []v1.Container{},
						Hostname:   podname,
						NodeName:   tc.nodes[tc.nodeIndex[i]].Name,
					},
					ObjectMeta: metav1.ObjectMeta{
						Namespace:   tc.svcNamespace,
						Name:        podname,
						Labels:      tc.labels,
						Annotations: tc.annotations,
					},
					Status: v1.PodStatus{
						Phase: tc.phases[i],
					},
				}

				_, err := kubernetes.CoreV1().Pods(tc.svcNamespace).Create(context.Background(), pod, metav1.CreateOptions{})
				require.NoError(t, err)
			}

			// Create a service to test against
			service := &v1.Service{
				Spec: v1.ServiceSpec{
					Type:                  tc.svcType,
					ExternalTrafficPolicy: tc.svcTrafficPolicy,
					Ports:                 tc.ports,
				},
				ObjectMeta: metav1.ObjectMeta{
					Namespace:   tc.svcNamespace,
					Name:        tc.svcName,
					Labels:      tc.labels,
					Annotations: tc.annotations,
				},
			}

			_, err := kubernetes.CoreV1().Services(service.Namespace).Create(context.Background(), service, metav1.CreateOptions{})
			require.NoError(t, err)

			// Create our object under test and get the endpoints.
			client, _ := NewServiceSource(
				kubernetes,
				tc.targetNamespace,
				tc.annotationFilter,
				tc.fqdnTemplate,
				false,
				tc.compatibility,
				true,
				false,
				false,
				[]string{},
				tc.ignoreHostnameAnnotation,
			)
			require.NoError(t, err)

			endpoints, err := client.Endpoints(context.Background())
			if tc.expectError {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}

			// Validate returned endpoints against desired endpoints.
			validateEndpoints(t, endpoints, tc.expected)
		})
	}
}

// TestHeadlessServices tests that headless services generate the correct endpoints.
func TestHeadlessServices(t *testing.T) {
	for _, tc := range []struct {
		title                    string
		targetNamespace          string
		svcNamespace             string
		svcName                  string
		svcType                  v1.ServiceType
		compatibility            string
		fqdnTemplate             string
		ignoreHostnameAnnotation bool
		labels                   map[string]string
		annotations              map[string]string
		clusterIP                string
		podIPs                   []string
		selector                 map[string]string
		ports                    []v1.ServicePort
		lbs                      []string
		podnames                 []string
		hostnames                []string
		podsReady                []bool
		publishNotReadyAddresses bool
		expected                 []*endpoint.Endpoint
		expectError              bool
	}{
		{
			"annotated Headless services return endpoints for each selected Pod",
			"",
			"testing",
			"foo",
			v1.ServiceTypeClusterIP,
			"",
			"",
			false,
			map[string]string{"component": "foo"},
			map[string]string{
				hostnameAnnotationKey: "service.example.org",
			},
			v1.ClusterIPNone,
			[]string{"1.1.1.1", "1.1.1.2"},
			map[string]string{
				"component": "foo",
			},
			[]v1.ServicePort{{Port: 8888}},
			[]string{},
			[]string{"foo-0", "foo-1"},
			[]string{"foo-0", "foo-1"},
			[]bool{true, true},
			false,
			[]*endpoint.Endpoint{
				{DNSName: "foo-0.service.example.org", Targets: endpoint.Targets{"1.1.1.1"}},
				{DNSName: "foo-1.service.example.org", Targets: endpoint.Targets{"1.1.1.2"}},
				{DNSName: "service.example.org", Targets: endpoint.Targets{"1.1.1.1", "1.1.1.2"}},
			},
			false,
		},
		{
			"hostname annotated Headless services are ignored",
			"",
			"testing",
			"foo",
			v1.ServiceTypeClusterIP,
			"",
			"",
			true,
			map[string]string{"component": "foo"},
			map[string]string{
				hostnameAnnotationKey: "service.example.org",
			},
			v1.ClusterIPNone,
			[]string{"1.1.1.1", "1.1.1.2"},
			map[string]string{
				"component": "foo",
			},
			[]v1.ServicePort{{Port: 8888}},
			[]string{},
			[]string{"foo-0", "foo-1"},
			[]string{"foo-0", "foo-1"},
			[]bool{true, true},
			false,
			[]*endpoint.Endpoint{},
			false,
		},
		{
			"annotated Headless services return endpoints with TTL for each selected Pod",
			"",
			"testing",
			"foo",
			v1.ServiceTypeClusterIP,
			"",
			"",
			false,
			map[string]string{"component": "foo"},
			map[string]string{
				hostnameAnnotationKey: "service.example.org",
				ttlAnnotationKey:      "1",
			},
			v1.ClusterIPNone,
			[]string{"1.1.1.1", "1.1.1.2"},
			map[string]string{
				"component": "foo",
			},
			[]v1.ServicePort{{Port: 8888}},
			[]string{},
			[]string{"foo-0", "foo-1"},
			[]string{"foo-0", "foo-1"},
			[]bool{true, true},
			false,
			[]*endpoint.Endpoint{
				{DNSName: "foo-0.service.example.org", Targets: endpoint.Targets{"1.1.1.1"}, RecordTTL: endpoint.TTL(1)},
				{DNSName: "foo-1.service.example.org", Targets: endpoint.Targets{"1.1.1.2"}, RecordTTL: endpoint.TTL(1)},
				{DNSName: "service.example.org", Targets: endpoint.Targets{"1.1.1.1", "1.1.1.2"}, RecordTTL: endpoint.TTL(1)},
			},
			false,
		},
		{
			"annotated Headless services return endpoints for each selected Pod, which are in running state",
			"",
			"testing",
			"foo",
			v1.ServiceTypeClusterIP,
			"",
			"",
			false,
			map[string]string{"component": "foo"},
			map[string]string{
				hostnameAnnotationKey: "service.example.org",
			},
			v1.ClusterIPNone,
			[]string{"1.1.1.1", "1.1.1.2"},
			map[string]string{
				"component": "foo",
			},
			[]v1.ServicePort{{Port: 8888}},
			[]string{},
			[]string{"foo-0", "foo-1"},
			[]string{"foo-0", "foo-1"},
			[]bool{true, false},
			false,
			[]*endpoint.Endpoint{
				{DNSName: "foo-0.service.example.org", Targets: endpoint.Targets{"1.1.1.1"}},
				{DNSName: "service.example.org", Targets: endpoint.Targets{"1.1.1.1"}},
			},
			false,
		},
		{
			"annotated Headless services return endpoints for all Pod if publishNotReadyAddresses is set",
			"",
			"testing",
			"foo",
			v1.ServiceTypeClusterIP,
			"",
			"",
			false,
			map[string]string{"component": "foo"},
			map[string]string{
				hostnameAnnotationKey: "service.example.org",
			},
			v1.ClusterIPNone,
			[]string{"1.1.1.1", "1.1.1.2"},
			map[string]string{
				"component": "foo",
			},
			[]v1.ServicePort{{Port: 8888}},
			[]string{},
			[]string{"foo-0", "foo-1"},
			[]string{"foo-0", "foo-1"},
			[]bool{true, false},
			true,
			[]*endpoint.Endpoint{
				{DNSName: "foo-0.service.example.org", Targets: endpoint.Targets{"1.1.1.1"}},
				{DNSName: "foo-1.service.example.org", Targets: endpoint.Targets{"1.1.1.2"}},
				{DNSName: "service.example.org", Targets: endpoint.Targets{"1.1.1.1", "1.1.1.2"}},
			},
			false,
		},
		{
			"annotated Headless services return endpoints for pods missing hostname",
			"",
			"testing",
			"foo",
			v1.ServiceTypeClusterIP,
			"",
			"",
			false,
			map[string]string{"component": "foo"},
			map[string]string{
				hostnameAnnotationKey: "service.example.org",
			},
			v1.ClusterIPNone,
			[]string{"1.1.1.1", "1.1.1.2"},
			map[string]string{
				"component": "foo",
			},
			[]v1.ServicePort{{Port: 8888}},
			[]string{},
			[]string{"foo-0", "foo-1"},
			[]string{"", ""},
			[]bool{true, true},
			false,
			[]*endpoint.Endpoint{
				{DNSName: "service.example.org", Targets: endpoint.Targets{"1.1.1.1", "1.1.1.2"}},
			},
			false,
		},
		{
			"annotated Headless services return only a unique set of targets",
			"",
			"testing",
			"foo",
			v1.ServiceTypeClusterIP,
			"",
			"",
			false,
			map[string]string{"component": "foo"},
			map[string]string{
				hostnameAnnotationKey: "service.example.org",
			},
			v1.ClusterIPNone,
			[]string{"1.1.1.1", "1.1.1.1", "1.1.1.2"},
			map[string]string{
				"component": "foo",
			},
			[]v1.ServicePort{{Port: 8888}},
			[]string{},
			[]string{"foo-0", "foo-1", "foo-3"},
			[]string{"", "", ""},
			[]bool{true, true, true},
			false,
			[]*endpoint.Endpoint{
				{DNSName: "service.example.org", Targets: endpoint.Targets{"1.1.1.1", "1.1.1.2"}},
			},
			false,
		},
		{
			"annotated Headless services with multiple named ports return endpoints for each selected Pod and SRV endpoints for each ports",
			"",
			"testing",
			"foo",
			v1.ServiceTypeClusterIP,
			"",
			"",
			false,
			map[string]string{"component": "foo"},
			map[string]string{
				hostnameAnnotationKey: "service.example.org",
			},
			v1.ClusterIPNone,
			[]string{"1.1.1.1", "1.1.1.2"},
			map[string]string{
				"component": "foo",
			},
			[]v1.ServicePort{
				{Name: "http", Port: 8888},
				{Name: "https", Port: 8889, Protocol: "UDP"},
				{Port: 8890},
			},
			[]string{},
			[]string{"foo-0", "foo-1"},
			[]string{"foo-0", "foo-1"},
			[]bool{true, true},
			false,
			[]*endpoint.Endpoint{
				{
					DNSName: "_http._tcp.service.example.org",
					Targets: endpoint.Targets{
						"0 50 8888 foo-0.service.example.org",
						"0 50 8888 foo-1.service.example.org",
					},
					RecordType: endpoint.RecordTypeSRV,
				},
				{
					DNSName: "_https._udp.service.example.org",
					Targets: endpoint.Targets{
						"0 50 8889 foo-0.service.example.org",
						"0 50 8889 foo-1.service.example.org",
					},
					RecordType: endpoint.RecordTypeSRV,
				},
				{DNSName: "foo-0.service.example.org", Targets: endpoint.Targets{"1.1.1.1"}},
				{DNSName: "foo-1.service.example.org", Targets: endpoint.Targets{"1.1.1.2"}},
				{DNSName: "service.example.org", Targets: endpoint.Targets{"1.1.1.1", "1.1.1.2"}},
			},
			false,
		},
	} {
		t.Run(tc.title, func(t *testing.T) {
			// Create a Kubernetes testing client
			kubernetes := fake.NewSimpleClientset()

			service := &v1.Service{
				Spec: v1.ServiceSpec{
					Type:                     tc.svcType,
					ClusterIP:                tc.clusterIP,
					Selector:                 tc.selector,
					Ports:                    tc.ports,
					PublishNotReadyAddresses: tc.publishNotReadyAddresses,
				},
				ObjectMeta: metav1.ObjectMeta{
					Namespace:   tc.svcNamespace,
					Name:        tc.svcName,
					Labels:      tc.labels,
					Annotations: tc.annotations,
				},
				Status: v1.ServiceStatus{},
			}
			_, err := kubernetes.CoreV1().Services(service.Namespace).Create(context.Background(), service, metav1.CreateOptions{})
			require.NoError(t, err)

			var addresses, notReadyAddresses []v1.EndpointAddress
			for i, podname := range tc.podnames {
				pod := &v1.Pod{
					Spec: v1.PodSpec{
						Containers: []v1.Container{},
						Hostname:   tc.hostnames[i],
					},
					ObjectMeta: metav1.ObjectMeta{
						Namespace:   tc.svcNamespace,
						Name:        podname,
						Labels:      tc.labels,
						Annotations: tc.annotations,
					},
					Status: v1.PodStatus{
						PodIP: tc.podIPs[i],
					},
				}

				_, err = kubernetes.CoreV1().Pods(tc.svcNamespace).Create(context.Background(), pod, metav1.CreateOptions{})
				require.NoError(t, err)

				address := v1.EndpointAddress{
					IP: tc.podIPs[i],
					TargetRef: &v1.ObjectReference{
						APIVersion: "",
						Kind:       "Pod",
						Name:       podname,
					},
				}
				if tc.podsReady[i] {
					addresses = append(addresses, address)
				} else {
					notReadyAddresses = append(notReadyAddresses, address)
				}
			}
			endpointsObject := &v1.Endpoints{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: tc.svcNamespace,
					Name:      tc.svcName,
					Labels:    tc.labels,
				},
				Subsets: []v1.EndpointSubset{
					{
						Addresses:         addresses,
						NotReadyAddresses: notReadyAddresses,
					},
				},
			}
			_, err = kubernetes.CoreV1().Endpoints(tc.svcNamespace).Create(context.Background(), endpointsObject, metav1.CreateOptions{})
			require.NoError(t, err)

			// Create our object under test and get the endpoints.
			client, _ := NewServiceSource(
				kubernetes,
				tc.targetNamespace,
				"",
				tc.fqdnTemplate,
				false,
				tc.compatibility,
				true,
				false,
				false,
				[]string{},
				tc.ignoreHostnameAnnotation,
			)
			require.NoError(t, err)

			endpoints, err := client.Endpoints(context.Background())
			if tc.expectError {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}

			// Validate returned endpoints against desired endpoints.
			validateEndpoints(t, endpoints, tc.expected)
		})
	}
}

// TestHeadlessServices tests that headless services generate the correct endpoints.
func TestHeadlessServicesHostIP(t *testing.T) {
	for _, tc := range []struct {
		title                    string
		targetNamespace          string
		svcNamespace             string
		svcName                  string
		svcType                  v1.ServiceType
		compatibility            string
		fqdnTemplate             string
		ignoreHostnameAnnotation bool
		labels                   map[string]string
		annotations              map[string]string
		clusterIP                string
		hostIPs                  []string
		selector                 map[string]string
		ports                    []v1.ServicePort
		lbs                      []string
		podnames                 []string
		hostnames                []string
		podsReady                []bool
		publishNotReadyAddresses bool
		expected                 []*endpoint.Endpoint
		expectError              bool
	}{
		{
			"annotated Headless services return endpoints for each selected Pod",
			"",
			"testing",
			"foo",
			v1.ServiceTypeClusterIP,
			"",
			"",
			false,
			map[string]string{"component": "foo"},
			map[string]string{
				hostnameAnnotationKey: "service.example.org",
			},
			v1.ClusterIPNone,
			[]string{"1.1.1.1", "1.1.1.2"},
			map[string]string{
				"component": "foo",
			},
			[]v1.ServicePort{{Port: 8888}},
			[]string{},
			[]string{"foo-0", "foo-1"},
			[]string{"foo-0", "foo-1"},
			[]bool{true, true},
			false,
			[]*endpoint.Endpoint{
				{DNSName: "foo-0.service.example.org", Targets: endpoint.Targets{"1.1.1.1"}},
				{DNSName: "foo-1.service.example.org", Targets: endpoint.Targets{"1.1.1.2"}},
				{DNSName: "service.example.org", Targets: endpoint.Targets{"1.1.1.1", "1.1.1.2"}},
			},
			false,
		},
		{
			"hostname annotated Headless services are ignored",
			"",
			"testing",
			"foo",
			v1.ServiceTypeClusterIP,
			"",
			"",
			true,
			map[string]string{"component": "foo"},
			map[string]string{
				hostnameAnnotationKey: "service.example.org",
			},
			v1.ClusterIPNone,
			[]string{"1.1.1.1", "1.1.1.2"},
			map[string]string{
				"component": "foo",
			},
			[]v1.ServicePort{{Port: 8888}},
			[]string{},
			[]string{"foo-0", "foo-1"},
			[]string{"foo-0", "foo-1"},
			[]bool{true, true},
			false,
			[]*endpoint.Endpoint{},
			false,
		},
		{
			"annotated Headless services return endpoints with TTL for each selected Pod",
			"",
			"testing",
			"foo",
			v1.ServiceTypeClusterIP,
			"",
			"",
			false,
			map[string]string{"component": "foo"},
			map[string]string{
				hostnameAnnotationKey: "service.example.org",
				ttlAnnotationKey:      "1",
			},
			v1.ClusterIPNone,
			[]string{"1.1.1.1", "1.1.1.2"},
			map[string]string{
				"component": "foo",
			},
			[]v1.ServicePort{{Port: 8888}},
			[]string{},
			[]string{"foo-0", "foo-1"},
			[]string{"foo-0", "foo-1"},
			[]bool{true, true},
			false,
			[]*endpoint.Endpoint{
				{DNSName: "foo-0.service.example.org", Targets: endpoint.Targets{"1.1.1.1"}, RecordTTL: endpoint.TTL(1)},
				{DNSName: "foo-1.service.example.org", Targets: endpoint.Targets{"1.1.1.2"}, RecordTTL: endpoint.TTL(1)},
				{DNSName: "service.example.org", Targets: endpoint.Targets{"1.1.1.1", "1.1.1.2"}, RecordTTL: endpoint.TTL(1)},
			},
			false,
		},
		{
			"annotated Headless services return endpoints for each selected Pod, which are in running state",
			"",
			"testing",
			"foo",
			v1.ServiceTypeClusterIP,
			"",
			"",
			false,
			map[string]string{"component": "foo"},
			map[string]string{
				hostnameAnnotationKey: "service.example.org",
			},
			v1.ClusterIPNone,
			[]string{"1.1.1.1", "1.1.1.2"},
			map[string]string{
				"component": "foo",
			},
			[]v1.ServicePort{{Port: 8888, NodePort: 30192}},
			[]string{},
			[]string{"foo-0", "foo-1"},
			[]string{"foo-0", "foo-1"},
			[]bool{true, false},
			false,
			[]*endpoint.Endpoint{
				{DNSName: "foo-0.service.example.org", Targets: endpoint.Targets{"1.1.1.1"}},
				{DNSName: "service.example.org", Targets: endpoint.Targets{"1.1.1.1"}},
			},
			false,
		},
		{
			"annotated Headless services return endpoints for all Pod if publishNotReadyAddresses is set",
			"",
			"testing",
			"foo",
			v1.ServiceTypeClusterIP,
			"",
			"",
			false,
			map[string]string{"component": "foo"},
			map[string]string{
				hostnameAnnotationKey: "service.example.org",
			},
			v1.ClusterIPNone,
			[]string{"1.1.1.1", "1.1.1.2"},
			map[string]string{
				"component": "foo",
			},
			[]v1.ServicePort{{Port: 8888, NodePort: 30192}},
			[]string{},
			[]string{"foo-0", "foo-1"},
			[]string{"foo-0", "foo-1"},
			[]bool{true, false},
			true,
			[]*endpoint.Endpoint{
				{DNSName: "foo-0.service.example.org", Targets: endpoint.Targets{"1.1.1.1"}},
				{DNSName: "foo-1.service.example.org", Targets: endpoint.Targets{"1.1.1.2"}},
				{DNSName: "service.example.org", Targets: endpoint.Targets{"1.1.1.1", "1.1.1.2"}},
			},
			false,
		},
		{
			"annotated Headless services return endpoints for pods missing hostname",
			"",
			"testing",
			"foo",
			v1.ServiceTypeClusterIP,
			"",
			"",
			false,
			map[string]string{"component": "foo"},
			map[string]string{
				hostnameAnnotationKey: "service.example.org",
			},
			v1.ClusterIPNone,
			[]string{"1.1.1.1", "1.1.1.2"},
			map[string]string{
				"component": "foo",
			},
			[]v1.ServicePort{{Port: 8888, NodePort: 30192}},
			[]string{},
			[]string{"foo-0", "foo-1"},
			[]string{"", ""},
			[]bool{true, true},
			false,
			[]*endpoint.Endpoint{
				{DNSName: "service.example.org", Targets: endpoint.Targets{"1.1.1.1", "1.1.1.2"}},
			},
			false,
		},
		{
			"annotated Headless services with multiple named ports return endpoints for each selected Pod and SRV endpoints for each ports",
			"",
			"testing",
			"foo",
			v1.ServiceTypeClusterIP,
			"",
			"",
			false,
			map[string]string{"component": "foo"},
			map[string]string{
				hostnameAnnotationKey: "service.example.org",
			},
			v1.ClusterIPNone,
			[]string{"1.1.1.1", "1.1.1.2"},
			map[string]string{
				"component": "foo",
			},
			[]v1.ServicePort{
				{Name: "http", Port: 8888},
				{Name: "https", Port: 8889, Protocol: "UDP"},
				{Port: 8890},
			},
			[]string{},
			[]string{"foo-0", "foo-1"},
			[]string{"foo-0", "foo-1"},
			[]bool{true,true},
			false,
			[]*endpoint.Endpoint{
				{
					DNSName: "_http._tcp.service.example.org",
					Targets: endpoint.Targets{
						"0 50 8888 foo-0.service.example.org",
						"0 50 8888 foo-1.service.example.org",
					},
					RecordType: endpoint.RecordTypeSRV,
				},
				{
					DNSName: "_https._udp.service.example.org",
					Targets: endpoint.Targets{
						"0 50 8889 foo-0.service.example.org",
						"0 50 8889 foo-1.service.example.org",
					},
					RecordType: endpoint.RecordTypeSRV,
				},
				{DNSName: "foo-0.service.example.org", Targets: endpoint.Targets{"1.1.1.1"}},
				{DNSName: "foo-1.service.example.org", Targets: endpoint.Targets{"1.1.1.2"}},
				{DNSName: "service.example.org", Targets: endpoint.Targets{"1.1.1.1", "1.1.1.2"}},
			},
			false,
		},
	} {
		t.Run(tc.title, func(t *testing.T) {
			// Create a Kubernetes testing client
			kubernetes := fake.NewSimpleClientset()

			service := &v1.Service{
				Spec: v1.ServiceSpec{
					Type:                     tc.svcType,
					ClusterIP:                tc.clusterIP,
					Selector:                 tc.selector,
					Ports:                    tc.ports,
					PublishNotReadyAddresses: tc.publishNotReadyAddresses,
				},
				ObjectMeta: metav1.ObjectMeta{
					Namespace:   tc.svcNamespace,
					Name:        tc.svcName,
					Labels:      tc.labels,
					Annotations: tc.annotations,
				},
				Status: v1.ServiceStatus{},
			}
			_, err := kubernetes.CoreV1().Services(service.Namespace).Create(context.Background(), service, metav1.CreateOptions{})
			require.NoError(t, err)

			var addresses []v1.EndpointAddress
			var notReadyAddresses []v1.EndpointAddress
			for i, podname := range tc.podnames {
				pod := &v1.Pod{
					Spec: v1.PodSpec{
						Containers: []v1.Container{},
						Hostname:   tc.hostnames[i],
					},
					ObjectMeta: metav1.ObjectMeta{
						Namespace:   tc.svcNamespace,
						Name:        podname,
						Labels:      tc.labels,
						Annotations: tc.annotations,
					},
					Status: v1.PodStatus{
						HostIP: tc.hostIPs[i],
					},
				}

				_, err = kubernetes.CoreV1().Pods(tc.svcNamespace).Create(context.Background(), pod, metav1.CreateOptions{})
				require.NoError(t, err)

				address := v1.EndpointAddress{
					IP: "4.3.2.1",
					TargetRef: &v1.ObjectReference{
						APIVersion: "",
						Kind:       "Pod",
						Name:       podname,
					},
				}
				if tc.podsReady[i] {
					addresses = append(addresses, address)
				} else {
					notReadyAddresses = append(notReadyAddresses, address)
				}
			}
			endpointsObject := &v1.Endpoints{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: tc.svcNamespace,
					Name:      tc.svcName,
					Labels:    tc.labels,
				},
				Subsets: []v1.EndpointSubset{
					{
						Addresses:         addresses,
						NotReadyAddresses: notReadyAddresses,
					},
				},
			}
			_, err = kubernetes.CoreV1().Endpoints(tc.svcNamespace).Create(context.Background(), endpointsObject, metav1.CreateOptions{})
			require.NoError(t, err)

			// Create our object under test and get the endpoints.
			client, _ := NewServiceSource(
				kubernetes,
				tc.targetNamespace,
				"",
				tc.fqdnTemplate,
				false,
				tc.compatibility,
				true,
				true,
				false,
				[]string{},
				tc.ignoreHostnameAnnotation,
			)
			require.NoError(t, err)

			endpoints, err := client.Endpoints(context.Background())
			if tc.expectError {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}

			// Validate returned endpoints against desired endpoints.
			validateEndpoints(t, endpoints, tc.expected)
		})
	}
}

// TestExternalServices tests that external services generate the correct endpoints.
func TestExternalServices(t *testing.T) {
	for _, tc := range []struct {
		title                    string
		targetNamespace          string
		svcNamespace             string
		svcName                  string
		svcType                  v1.ServiceType
		compatibility            string
		fqdnTemplate             string
		ignoreHostnameAnnotation bool
		labels                   map[string]string
		annotations              map[string]string
		externalName             string
		expected                 []*endpoint.Endpoint
		expectError              bool
	}{
		{
			"external services return an A endpoint for the external name that is an IP address",
			"",
			"testing",
			"foo",
			v1.ServiceTypeExternalName,
			"",
			"",
			false,
			map[string]string{"component": "foo"},
			map[string]string{
				hostnameAnnotationKey: "service.example.org",
			},
			"111.111.111.111",
			[]*endpoint.Endpoint{
				{DNSName: "service.example.org", Targets: endpoint.Targets{"111.111.111.111"}, RecordType: endpoint.RecordTypeA},
			},
			false,
		},
		{
			"external services return a CNAME endpoint for the external name that is a domain",
			"",
			"testing",
			"foo",
			v1.ServiceTypeExternalName,
			"",
			"",
			false,
			map[string]string{"component": "foo"},
			map[string]string{
				hostnameAnnotationKey: "service.example.org",
			},
			"remote.example.com",
			[]*endpoint.Endpoint{
				{DNSName: "service.example.org", Targets: endpoint.Targets{"remote.example.com"}, RecordType: endpoint.RecordTypeCNAME},
			},
			false,
		},
	} {
		t.Run(tc.title, func(t *testing.T) {
			// Create a Kubernetes testing client
			kubernetes := fake.NewSimpleClientset()

			service := &v1.Service{
				Spec: v1.ServiceSpec{
					Type:         tc.svcType,
					ExternalName: tc.externalName,
				},
				ObjectMeta: metav1.ObjectMeta{
					Namespace:   tc.svcNamespace,
					Name:        tc.svcName,
					Labels:      tc.labels,
					Annotations: tc.annotations,
				},
				Status: v1.ServiceStatus{},
			}
			_, err := kubernetes.CoreV1().Services(service.Namespace).Create(context.Background(), service, metav1.CreateOptions{})
			require.NoError(t, err)

			// Create our object under test and get the endpoints.
			client, _ := NewServiceSource(
				kubernetes,
				tc.targetNamespace,
				"",
				tc.fqdnTemplate,
				false,
				tc.compatibility,
				true,
				false,
				false,
				[]string{},
				tc.ignoreHostnameAnnotation,
			)
			require.NoError(t, err)

			endpoints, err := client.Endpoints(context.Background())
			if tc.expectError {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}

			// Validate returned endpoints against desired endpoints.
			validateEndpoints(t, endpoints, tc.expected)
		})
	}
}

func BenchmarkServiceEndpoints(b *testing.B) {
	kubernetes := fake.NewSimpleClientset()

	service := &v1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "testing",
			Name:      "foo",
			Annotations: map[string]string{
				hostnameAnnotationKey: "foo.example.org.",
			},
		},
		Status: v1.ServiceStatus{
			LoadBalancer: v1.LoadBalancerStatus{
				Ingress: []v1.LoadBalancerIngress{
					{IP: "1.2.3.4"},
					{IP: "8.8.8.8"},
				},
			},
		},
	}

	_, err := kubernetes.CoreV1().Services(service.Namespace).Create(context.Background(), service, metav1.CreateOptions{})
	require.NoError(b, err)

	client, err := NewServiceSource(kubernetes, v1.NamespaceAll, "", "", false, "", false, false, false, []string{}, false)
	require.NoError(b, err)

	for i := 0; i < b.N; i++ {
		_, err := client.Endpoints(context.Background())
		require.NoError(b, err)
	}
}
