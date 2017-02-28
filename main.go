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

package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	log "github.com/Sirupsen/logrus"

	"golang.org/x/oauth2/google"
	"google.golang.org/api/dns/v1"

	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"

	"github.com/kubernetes-incubator/external-dns/config"
	"github.com/kubernetes-incubator/external-dns/controller"
	"github.com/kubernetes-incubator/external-dns/dnsprovider"
	"github.com/kubernetes-incubator/external-dns/source"
)

func main() {
	cfg := config.NewConfig()
	cfg.ParseFlags()
	if err := cfg.Validate(); err != nil {
		log.Errorf("config validation failed: %v", err)
	}

	if cfg.LogFormat == "json" {
		log.SetFormatter(&log.JSONFormatter{})
	}
	if cfg.Debug {
		log.SetLevel(log.DebugLevel)
	}

	stopChan := make(chan struct{}, 1)

	go registerHandlers(cfg.HealthPort)
	go handleSigterm(stopChan)

	client, err := newClient(cfg)
	if err != nil {
		log.Fatal(err)
	}

	source := &source.ServiceSource{
		Client: client,
	}

	gcloud, err := google.DefaultClient(context.TODO(), dns.NdevClouddnsReadwriteScope)
	if err != nil {
		log.Fatal(err)
	}

	dnsClient, err := dns.New(gcloud)
	if err != nil {
		log.Fatal(err)
	}

	dnsProvider := &dnsprovider.GoogleProvider{
		Project:                  "zalando-teapot",
		ResourceRecordSetsClient: dnsClient.ResourceRecordSets,
		ManagedZonesClient:       dnsClient.ManagedZones,
		ChangesClient:            dnsClient.Changes,
	}

	ctrl := controller.Controller{
		Zone: "external-dns-integration-test.gcp.zalan.do",

		Source:      source,
		DNSProvider: dnsProvider,
	}

	ctrl.Run(stopChan)
	for {
		log.Infoln("pod waiting to be deleted")
		time.Sleep(time.Second * 30)
	}
}

func registerHandlers(port string) {
	http.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))
	})
	log.Fatal(http.ListenAndServe(fmt.Sprintf(":%s", port), nil))
}

func handleSigterm(stopChan chan struct{}) {
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, syscall.SIGTERM)
	<-signals
	log.Infoln("received SIGTERM. Terminating...")
	close(stopChan)
}

func newClient(cfg *config.Config) (*kubernetes.Clientset, error) {
	var (
		config *rest.Config
		err    error
	)

	if cfg.InCluster {
		config, err = rest.InClusterConfig()
		log.Debug("Using in-cluster config.")
	} else {
		config, err = clientcmd.BuildConfigFromFlags("", cfg.KubeConfig)
		log.Debugf("Using current context from kubeconfig at %s.", cfg.KubeConfig)
	}
	if err != nil {
		return nil, err
	}
	log.Infof("Targeting cluster at %s", config.Host)

	client, err := kubernetes.NewForConfig(config)
	if err != nil {
		return nil, err
	}

	return client, nil
}
