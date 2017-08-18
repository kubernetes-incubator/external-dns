package source

import (
	"fmt"
	"github.com/kubernetes-incubator/external-dns/endpoint"
	"github.com/stretchr/testify/assert"
	"testing"
)

func TestGetTTLFromAnnotations(t *testing.T) {
	for _, tc := range []struct {
		title       string
		annotations map[string]string
		expectedTTL endpoint.TTL
		expectedErr error
	}{
		{
			title:       "TTL annotation not present",
			annotations: map[string]string{"foo": "bar"},
			expectedTTL: endpoint.TTL(0),
			expectedErr: nil,
		},
		{
			title:       "TTL annotation value is not a number",
			annotations: map[string]string{ttlAnnotationKey: "foo"},
			expectedTTL: endpoint.TTL(0),
			expectedErr: fmt.Errorf("\"foo\" is not a valid TTL value"),
		},
		{
			title:       "TTL annotation value is empty",
			annotations: map[string]string{ttlAnnotationKey: ""},
			expectedTTL: endpoint.TTL(0),
			expectedErr: fmt.Errorf("\"\" is not a valid TTL value"),
		},
		{
			title:       "TTL annotation value is negative number",
			annotations: map[string]string{ttlAnnotationKey: "-1"},
			expectedTTL: endpoint.TTL(0),
			expectedErr: fmt.Errorf("TTL value must be between [%d, %d]", ttlMinimum, ttlMaximum),
		},
		{
			title:       "TTL annotation value is too high",
			annotations: map[string]string{ttlAnnotationKey: fmt.Sprintf("%d", 1<<32)},
			expectedTTL: endpoint.TTL(0),
			expectedErr: fmt.Errorf("TTL value must be between [%d, %d]", ttlMinimum, ttlMaximum),
		},
		{
			title:       "TTL annotation value is set correctly",
			annotations: map[string]string{ttlAnnotationKey: "60"},
			expectedTTL: endpoint.TTL(60),
			expectedErr: nil,
		},
	} {
		t.Run(tc.title, func(t *testing.T) {
			ttl, err := getTTLFromAnnotations(tc.annotations)
			assert.Equal(t, tc.expectedTTL, ttl)
			assert.Equal(t, tc.expectedErr, err)
		})
	}

}
