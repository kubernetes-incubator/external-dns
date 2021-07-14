/*
Copyright 2018 The Kubernetes Authors.

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
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/rest/fake"

	"sigs.k8s.io/external-dns/endpoint"
)

type CRDSuite struct {
	suite.Suite
}

func (suite *CRDSuite) SetupTest() {

}

func defaultHeader() http.Header {
	header := http.Header{}
	header.Set("Content-Type", runtime.ContentTypeJSON)
	return header
}

func objBody(codec runtime.Encoder, obj runtime.Object) io.ReadCloser {
	return ioutil.NopCloser(bytes.NewReader([]byte(runtime.EncodeOrDie(codec, obj))))
}

func startCRDServerToServeTargets(endpoints []*endpoint.Endpoint, apiVersion, kind, namespace, name string, annotations map[string]string, labels map[string]string, t *testing.T) rest.Interface {
	groupVersion, _ := schema.ParseGroupVersion(apiVersion)
	scheme := runtime.NewScheme()
	addKnownTypes(scheme, groupVersion)

	dnsEndpointList := endpoint.DNSEndpointList{}
	dnsEndpoint := &endpoint.DNSEndpoint{
		TypeMeta: metav1.TypeMeta{
			APIVersion: apiVersion,
			Kind:       kind,
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:        name,
			Namespace:   namespace,
			Annotations: annotations,
			Labels:      labels,
			Generation:  1,
		},
		Spec: endpoint.DNSEndpointSpec{
			Endpoints: endpoints,
		},
	}

	codecFactory := serializer.WithoutConversionCodecFactory{
		CodecFactory: serializer.NewCodecFactory(scheme),
	}

	client := &fake.RESTClient{
		GroupVersion:         groupVersion,
		VersionedAPIPath:     "/apis/" + apiVersion,
		NegotiatedSerializer: codecFactory,
		Client: fake.CreateHTTPClient(func(req *http.Request) (*http.Response, error) {
			codec := codecFactory.LegacyCodec(groupVersion)
			switch p, m := req.URL.Path, req.Method; {
			case p == "/apis/"+apiVersion+"/"+strings.ToLower(kind)+"s" && m == http.MethodGet:
				fallthrough
			case p == "/apis/"+apiVersion+"/namespaces/"+namespace+"/"+strings.ToLower(kind)+"s" && m == http.MethodGet:
				dnsEndpointList.Items = dnsEndpointList.Items[:0]
				dnsEndpointList.Items = append(dnsEndpointList.Items, *dnsEndpoint)
				return &http.Response{StatusCode: http.StatusOK, Header: defaultHeader(), Body: objBody(codec, &dnsEndpointList)}, nil
			case strings.HasPrefix(p, "/apis/"+apiVersion+"/namespaces/") && strings.HasSuffix(p, strings.ToLower(kind)+"s") && m == http.MethodGet:
				return &http.Response{StatusCode: http.StatusOK, Header: defaultHeader(), Body: objBody(codec, &dnsEndpointList)}, nil
			case p == "/apis/"+apiVersion+"/namespaces/"+namespace+"/"+strings.ToLower(kind)+"s/"+name+"/status" && m == http.MethodPut:
				decoder := json.NewDecoder(req.Body)

				var body endpoint.DNSEndpoint
				decoder.Decode(&body)
				dnsEndpoint.Status.ObservedGeneration = body.Status.ObservedGeneration
				return &http.Response{StatusCode: http.StatusOK, Header: defaultHeader(), Body: objBody(codec, dnsEndpoint)}, nil
			default:
				return nil, fmt.Errorf("unexpected request: %#v\n%#v", req.URL, req)
			}
		}),
	}

	return client
}

func TestCRDSource(t *testing.T) {
	suite.Run(t, new(CRDSuite))
	t.Run("Interface", testCRDSourceImplementsSource)
	t.Run("Endpoints", testCRDSourceEndpoints)
}

// testCRDSourceImplementsSource tests that crdSource is a valid Source.
func testCRDSourceImplementsSource(t *testing.T) {
	require.Implements(t, (*Source)(nil), new(crdSource))
}

// testCRDSourceEndpoints tests various scenarios of using CRD source.
func testCRDSourceEndpoints(t *testing.T) {
	for _, ti := range []struct {
		title                string
		registeredNamespace  string
		namespace            string
		registeredAPIVersion string
		apiVersion           string
		registeredKind       string
		kind                 string
		endpoints            []*endpoint.Endpoint
		expectEndpoints      bool
		expectError          bool
		annotationFilter     string
		labelFilter          string
		annotations          map[string]string
		labels               map[string]string
	}{
		{
			title:                "invalid crd api version",
			registeredAPIVersion: "test.k8s.io/v1alpha1",
			apiVersion:           "blah.k8s.io/v1alpha1",
			registeredKind:       "DNSEndpoint",
			kind:                 "DNSEndpoint",
			endpoints: []*endpoint.Endpoint{
				{DNSName: "abc.example.org",
					Targets:    endpoint.Targets{"1.2.3.4"},
					RecordType: endpoint.RecordTypeA,
					RecordTTL:  180,
				},
			},
			expectEndpoints: false,
			expectError:     true,
		},
		{
			title:                "invalid crd kind",
			registeredAPIVersion: "test.k8s.io/v1alpha1",
			apiVersion:           "test.k8s.io/v1alpha1",
			registeredKind:       "DNSEndpoint",
			kind:                 "JustEndpoint",
			endpoints: []*endpoint.Endpoint{
				{DNSName: "abc.example.org",
					Targets:    endpoint.Targets{"1.2.3.4"},
					RecordType: endpoint.RecordTypeA,
					RecordTTL:  180,
				},
			},
			expectEndpoints: false,
			expectError:     true,
		},
		{
			title:                "endpoints within a specific namespace",
			registeredAPIVersion: "test.k8s.io/v1alpha1",
			apiVersion:           "test.k8s.io/v1alpha1",
			registeredKind:       "DNSEndpoint",
			kind:                 "DNSEndpoint",
			namespace:            "foo",
			registeredNamespace:  "foo",
			endpoints: []*endpoint.Endpoint{
				{DNSName: "abc.example.org",
					Targets:    endpoint.Targets{"1.2.3.4"},
					RecordType: endpoint.RecordTypeA,
					RecordTTL:  180,
				},
			},
			expectEndpoints: true,
			expectError:     false,
		},
		{
			title:                "no endpoints within a specific namespace",
			registeredAPIVersion: "test.k8s.io/v1alpha1",
			apiVersion:           "test.k8s.io/v1alpha1",
			registeredKind:       "DNSEndpoint",
			kind:                 "DNSEndpoint",
			namespace:            "foo",
			registeredNamespace:  "bar",
			endpoints: []*endpoint.Endpoint{
				{DNSName: "abc.example.org",
					Targets:    endpoint.Targets{"1.2.3.4"},
					RecordType: endpoint.RecordTypeA,
					RecordTTL:  180,
				},
			},
			expectEndpoints: false,
			expectError:     false,
		},
		{
			title:                "invalid crd with no targets",
			registeredAPIVersion: "test.k8s.io/v1alpha1",
			apiVersion:           "test.k8s.io/v1alpha1",
			registeredKind:       "DNSEndpoint",
			kind:                 "DNSEndpoint",
			namespace:            "foo",
			registeredNamespace:  "foo",
			endpoints: []*endpoint.Endpoint{
				{DNSName: "abc.example.org",
					Targets:    endpoint.Targets{},
					RecordType: endpoint.RecordTypeA,
					RecordTTL:  180,
				},
			},
			expectEndpoints: false,
			expectError:     false,
		},
		{
			title:                "valid crd gvk with single endpoint",
			registeredAPIVersion: "test.k8s.io/v1alpha1",
			apiVersion:           "test.k8s.io/v1alpha1",
			registeredKind:       "DNSEndpoint",
			kind:                 "DNSEndpoint",
			namespace:            "foo",
			registeredNamespace:  "foo",
			endpoints: []*endpoint.Endpoint{
				{DNSName: "abc.example.org",
					Targets:    endpoint.Targets{"1.2.3.4"},
					RecordType: endpoint.RecordTypeA,
					RecordTTL:  180,
				},
			},
			expectEndpoints: true,
			expectError:     false,
		},
		{
			title:                "valid crd gvk with multiple endpoints",
			registeredAPIVersion: "test.k8s.io/v1alpha1",
			apiVersion:           "test.k8s.io/v1alpha1",
			registeredKind:       "DNSEndpoint",
			kind:                 "DNSEndpoint",
			namespace:            "foo",
			registeredNamespace:  "foo",
			endpoints: []*endpoint.Endpoint{
				{DNSName: "abc.example.org",
					Targets:    endpoint.Targets{"1.2.3.4"},
					RecordType: endpoint.RecordTypeA,
					RecordTTL:  180,
				},
				{DNSName: "xyz.example.org",
					Targets:    endpoint.Targets{"abc.example.org"},
					RecordType: endpoint.RecordTypeCNAME,
					RecordTTL:  180,
				},
			},
			expectEndpoints: true,
			expectError:     false,
		},
		{
			title:                "valid crd gvk with annotation and non matching annotation filter",
			registeredAPIVersion: "test.k8s.io/v1alpha1",
			apiVersion:           "test.k8s.io/v1alpha1",
			registeredKind:       "DNSEndpoint",
			kind:                 "DNSEndpoint",
			namespace:            "foo",
			registeredNamespace:  "foo",
			annotations:          map[string]string{"test": "that"},
			annotationFilter:     "test=filter_something_else",
			endpoints: []*endpoint.Endpoint{
				{DNSName: "abc.example.org",
					Targets:    endpoint.Targets{"1.2.3.4"},
					RecordType: endpoint.RecordTypeA,
					RecordTTL:  180,
				},
			},
			expectEndpoints: false,
			expectError:     false,
		},
		{
			title:                "valid crd gvk with annotation and matching annotation filter",
			registeredAPIVersion: "test.k8s.io/v1alpha1",
			apiVersion:           "test.k8s.io/v1alpha1",
			registeredKind:       "DNSEndpoint",
			kind:                 "DNSEndpoint",
			namespace:            "foo",
			registeredNamespace:  "foo",
			annotations:          map[string]string{"test": "that"},
			annotationFilter:     "test=that",
			endpoints: []*endpoint.Endpoint{
				{DNSName: "abc.example.org",
					Targets:    endpoint.Targets{"1.2.3.4"},
					RecordType: endpoint.RecordTypeA,
					RecordTTL:  180,
				},
			},
			expectEndpoints: true,
			expectError:     false,
		},
		{
			title:                "valid crd gvk with label and non matching label filter",
			registeredAPIVersion: "test.k8s.io/v1alpha1",
			apiVersion:           "test.k8s.io/v1alpha1",
			registeredKind:       "DNSEndpoint",
			kind:                 "DNSEndpoint",
			namespace:            "foo",
			registeredNamespace:  "foo",
			labels:               map[string]string{"test": "that"},
			labelFilter:          "test=filter_something_else",
			endpoints: []*endpoint.Endpoint{
				{DNSName: "abc.example.org",
					Targets:    endpoint.Targets{"1.2.3.4"},
					RecordType: endpoint.RecordTypeA,
					RecordTTL:  180,
				},
			},
			expectEndpoints: false,
			expectError:     false,
		},
		{
			title:                "valid crd gvk with label and matching label filter",
			registeredAPIVersion: "test.k8s.io/v1alpha1",
			apiVersion:           "test.k8s.io/v1alpha1",
			registeredKind:       "DNSEndpoint",
			kind:                 "DNSEndpoint",
			namespace:            "foo",
			registeredNamespace:  "foo",
			labels:               map[string]string{"test": "that"},
			labelFilter:          "test=that",
			endpoints: []*endpoint.Endpoint{
				{DNSName: "abc.example.org",
					Targets:    endpoint.Targets{"1.2.3.4"},
					RecordType: endpoint.RecordTypeA,
					RecordTTL:  180,
				},
			},
			expectEndpoints: true,
			expectError:     false,
		},
		{
			title:                "Create NS record",
			registeredAPIVersion: "test.k8s.io/v1alpha1",
			apiVersion:           "test.k8s.io/v1alpha1",
			registeredKind:       "DNSEndpoint",
			kind:                 "DNSEndpoint",
			namespace:            "foo",
			registeredNamespace:  "foo",
			labels:               map[string]string{"test": "that"},
			labelFilter:          "test=that",
			endpoints: []*endpoint.Endpoint{
				{DNSName: "abc.example.org",
					Targets:    endpoint.Targets{"ns1.k8s.io", "ns2.k8s.io"},
					RecordType: endpoint.RecordTypeNS,
					RecordTTL:  180,
				},
			},
			expectEndpoints: true,
			expectError:     false,
		},
		{
			title:                "Create PTR record",
			registeredAPIVersion: "test.k8s.io/v1alpha1",
			apiVersion:           "test.k8s.io/v1alpha1",
			registeredKind:       "DNSEndpoint",
			kind:                 "DNSEndpoint",
			namespace:            "foo",
			registeredNamespace:  "foo",
			labels:               map[string]string{"test": "that"},
			labelFilter:          "test=that",
			endpoints: []*endpoint.Endpoint{
				{DNSName: "1.2.3.4.in-addr.arpa",
					Targets:    endpoint.Targets{"my.server.org."},
					RecordType: endpoint.RecordTypePTR,
					RecordTTL:  180,
				},
			},
			expectEndpoints: true,
			expectError:     false,
		},
	} {
		t.Run(ti.title, func(t *testing.T) {
			restClient := startCRDServerToServeTargets(ti.endpoints, ti.registeredAPIVersion, ti.registeredKind, ti.registeredNamespace, "test", ti.annotations, ti.labels, t)
			groupVersion, err := schema.ParseGroupVersion(ti.apiVersion)
			require.NoError(t, err)

			scheme := runtime.NewScheme()
			addKnownTypes(scheme, groupVersion)

			cs, _ := NewCRDSource(restClient, ti.namespace, ti.kind, ti.annotationFilter, ti.labelFilter, scheme)

			receivedEndpoints, err := cs.Endpoints(context.Background())
			if ti.expectError {
				require.Errorf(t, err, "Received err %v", err)
			} else {
				require.NoErrorf(t, err, "Received err %v", err)
			}

			if len(receivedEndpoints) == 0 && !ti.expectEndpoints {
				return
			}

			if err == nil {
				validateCRDResource(t, cs, ti.expectError)
			}

			// Validate received endpoints against expected endpoints.
			validateEndpoints(t, receivedEndpoints, ti.endpoints)
		})
	}
}

func validateCRDResource(t *testing.T, src Source, expectError bool) {
	cs := src.(*crdSource)
	result, err := cs.List(context.Background(), &metav1.ListOptions{})
	if expectError {
		require.Errorf(t, err, "Received err %v", err)
	} else {
		require.NoErrorf(t, err, "Received err %v", err)
	}

	for _, dnsEndpoint := range result.Items {
		if dnsEndpoint.Status.ObservedGeneration != dnsEndpoint.Generation {
			require.Errorf(t, err, "Unexpected CRD resource result: ObservedGenerations <%v> is not equal to Generation<%v>", dnsEndpoint.Status.ObservedGeneration, dnsEndpoint.Generation)
		}
	}
}
