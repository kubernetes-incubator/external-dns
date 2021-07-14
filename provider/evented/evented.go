package evented

import (
	"context"
	"fmt"
	"strings"

	log "github.com/sirupsen/logrus"

	"k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	typedcorev1 "k8s.io/client-go/kubernetes/typed/core/v1"
	"k8s.io/client-go/tools/record"
	"k8s.io/client-go/tools/reference"

	"sigs.k8s.io/external-dns/endpoint"
	"sigs.k8s.io/external-dns/plan"
	"sigs.k8s.io/external-dns/provider"
	"sigs.k8s.io/external-dns/source"
)

type EventedProvider struct {
	wrappedProvider provider.Provider
	eventRecorder   record.EventRecorder
	kubeClient      kubernetes.Interface
}

func NewEventedProvider(provider provider.Provider, clientGenerator source.ClientGenerator) (*EventedProvider, error) {
	client, err := clientGenerator.KubeClient()
	if err != nil {
		return nil, err
	}

	broadcaster := record.NewBroadcaster()
	broadcaster.StartRecordingToSink(&typedcorev1.EventSinkImpl{Interface: client.CoreV1().Events(v1.NamespaceAll)})
	recorder := broadcaster.NewRecorder(scheme.Scheme, v1.EventSource{Component: "externaldns"})

	eventedProvider := &EventedProvider{
		wrappedProvider: provider,
		eventRecorder:   recorder,
		kubeClient:      client,
	}

	return eventedProvider, nil
}

func (p *EventedProvider) PropertyValuesEqual(name string, previous string, current string) bool {
	return p.wrappedProvider.PropertyValuesEqual(name, previous, current)
}

func (p *EventedProvider) Records(ctx context.Context) (endpoints []*endpoint.Endpoint, _ error) {
	return p.wrappedProvider.Records(ctx)
}

func (p *EventedProvider) ApplyChanges(ctx context.Context, changes *plan.Changes) error {
	if err := p.wrappedProvider.ApplyChanges(ctx, changes); err != nil {
		return err
	}

	for _, ep := range changes.Create {
		ref, err := p.parseOwner(ctx, ep.Labels[endpoint.ResourceLabelKey])
		if err != nil {
			log.Warnf("Unable to parse owner: %v", err)
		}
		if ref != nil {
			p.eventRecorder.Eventf(ref, v1.EventTypeNormal, "CreateRecord", "Create DNS record '%s' with targets '%s'", ep.DNSName, ep.Targets)
		}
	}

	for _, ep := range changes.UpdateNew {
		ref, err := p.parseOwner(ctx, ep.Labels[endpoint.ResourceLabelKey])
		if err != nil {
			log.Warnf("Unable to parse owner: %v", err)
		}
		if ref != nil {
			p.eventRecorder.Eventf(ref, v1.EventTypeNormal, "UpdateRecord", "Update DNS record '%s' with targets '%s'", ep.DNSName, ep.Targets)
		}
	}

	for _, ep := range changes.Delete {
		ref, err := p.parseOwner(ctx, ep.Labels[endpoint.ResourceLabelKey])
		if err != nil {
			log.Warnf("Unable to parse owner: %v", err)
		}
		if ref != nil {
			p.eventRecorder.Eventf(ref, v1.EventTypeNormal, "DeleteRecord", "Delete DNS record '%s' with targets '%s'", ep.DNSName, ep.Targets)
		}
	}

	return nil
}

func (p *EventedProvider) AdjustEndpoints(endpoints []*endpoint.Endpoint) []*endpoint.Endpoint {
	return p.wrappedProvider.AdjustEndpoints(endpoints)
}

func (p *EventedProvider) parseOwner(ctx context.Context, resourceLabelValue string) (*v1.ObjectReference, error) {
	ownerResource := strings.Split(resourceLabelValue, "/")

	if len(ownerResource) != 3 {
		return nil, fmt.Errorf("incompatible owner: %s", resourceLabelValue)
	}

	var (
		obj runtime.Object
		err error
	)

	switch ownerResource[0] {
	case "ingress":
		obj, err = p.kubeClient.NetworkingV1beta1().Ingresses(ownerResource[1]).Get(ctx, ownerResource[2], metav1.GetOptions{})
		if err != nil {
			return nil, err
		}
	case "service":
		obj, err = p.kubeClient.CoreV1().Services(ownerResource[1]).Get(ctx, ownerResource[2], metav1.GetOptions{})
		if err != nil {
			return nil, err
		}
	default:
		return nil, fmt.Errorf("unknown type: %s", ownerResource[0])
	}

	return reference.GetReference(scheme.Scheme, obj)
}
