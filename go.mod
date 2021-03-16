module sigs.k8s.io/external-dns

go 1.15

require (
	cloud.google.com/go v0.50.0
	git.blindage.org/21h/hcloud-dns v0.0.0-20200807003420-f768ffe03f8d
	github.com/Azure/azure-sdk-for-go v45.1.0+incompatible
	github.com/Azure/go-autorest/autorest v0.11.10
	github.com/Azure/go-autorest/autorest/adal v0.9.5
	github.com/Azure/go-autorest/autorest/to v0.4.0
	github.com/akamai/AkamaiOPEN-edgegrid-golang v1.0.0
	github.com/alecthomas/assert v0.0.0-20170929043011-405dbfeb8e38
	github.com/alecthomas/colour v0.1.0 // indirect
	github.com/alecthomas/kingpin v2.2.5+incompatible
	github.com/alecthomas/repr v0.0.0-20200325044227-4184120f674c // indirect
	github.com/aliyun/alibaba-cloud-sdk-go v1.61.357
	github.com/aws/aws-sdk-go v1.31.4
	github.com/bodgit/tsig v0.0.2
	github.com/cloudflare/cloudflare-go v0.10.1
	github.com/cloudfoundry-community/go-cfclient v0.0.0-20190201205600-f136f9222381
	github.com/datawire/ambassador v1.6.0
	github.com/denverdino/aliyungo v0.0.0-20190125010748-a747050bb1ba
	github.com/digitalocean/godo v1.36.0
	github.com/dnsimple/dnsimple-go v0.60.0
	github.com/exoscale/egoscale v0.18.1
	github.com/ffledgling/pdns-go v0.0.0-20180219074714-524e7daccd99
	github.com/golang/sync v0.0.0-20180314180146-1d60e4601c6f
	github.com/google/go-cmp v0.4.1
	github.com/gophercloud/gophercloud v0.1.0
	github.com/gorilla/mux v1.7.4 // indirect
	github.com/infobloxopen/infoblox-go-client v0.0.0-20180606155407-61dc5f9b0a65
	github.com/linki/instrumented_http v0.2.0
	github.com/linode/linodego v0.19.0
	github.com/maxatome/go-testdeep v1.4.0
	github.com/miekg/dns v1.1.36-0.20210109083720-731b191cabd1
	github.com/nesv/go-dynect v0.6.0
	github.com/nic-at/rc0go v1.1.1
	github.com/nrdcg/goinwx v0.8.1
	github.com/openshift/api v0.0.0-20200605231317-fb2a6ca106ae
	github.com/openshift/client-go v0.0.0-20200608144219-584632b8fc73
	github.com/oracle/oci-go-sdk v21.4.0+incompatible
	github.com/ovh/go-ovh v0.0.0-20181109152953-ba5adb4cf014
	github.com/pkg/errors v0.9.1
	github.com/projectcontour/contour v1.5.0
	github.com/prometheus/client_golang v1.7.1
	github.com/sanyu/dynectsoap v0.0.0-20181203081243-b83de5edc4e0
	github.com/scaleway/scaleway-sdk-go v1.0.0-beta.7.0.20210127161313-bd30bebeac4f
	github.com/sirupsen/logrus v1.6.0
	github.com/smartystreets/gunit v1.3.4 // indirect
	github.com/stretchr/testify v1.6.1
	github.com/terra-farm/udnssdk v1.3.5 // indirect
	github.com/transip/gotransip v5.8.2+incompatible
	github.com/ultradns/ultradns-sdk-go v0.0.0-20200616202852-e62052662f60
	github.com/vinyldns/go-vinyldns v0.0.0-20200211145900-fe8a3d82e556
	github.com/vultr/govultr v0.4.2
	go.etcd.io/etcd v0.5.0-alpha.5.0.20200401174654-e694b7bb0875
	go.uber.org/ratelimit v0.1.0
	golang.org/x/net v0.0.0-20201224014010-6772e930b67b
	golang.org/x/oauth2 v0.0.0-20200107190931-bf48bf16ab8d
	golang.org/x/sync v0.0.0-20200625203802-6e8e738ad208
	golang.org/x/tools v0.0.0-20200708003708-134513de8882 // indirect
	google.golang.org/api v0.15.0
	gopkg.in/ns1/ns1-go.v2 v2.0.0-20190322154155-0dafb5275fd1
	gopkg.in/yaml.v2 v2.3.0
	honnef.co/go/tools v0.0.1-2020.1.4 // indirect
	istio.io/api v0.0.0-20210128181506-0c4b8e54850f
	istio.io/client-go v0.0.0-20210128182905-ee2edd059e02
	k8s.io/api v0.18.8
	k8s.io/apimachinery v0.18.8
	k8s.io/client-go v0.18.8
	k8s.io/kubernetes v1.13.0
)

replace (
	github.com/golang/glog => github.com/kubermatic/glog-logrus v0.0.0-20180829085450-3fa5b9870d1d
	// TODO(jpg): Pin gRPC to work around breaking change until all dependences are upgraded: https://github.com/etcd-io/etcd/issues/11563
	google.golang.org/grpc => google.golang.org/grpc v1.26.0
	k8s.io/klog => github.com/mikkeloscar/knolog v0.0.0-20190326191552-80742771eb6b
)
