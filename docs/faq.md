# Frequently asked questions

### How is ExternalDNS useful to me?

You've probably created many deployments. Typically, you expose your deployment to the Internet by creating a Service with `type=LoadBalancer`. Depending on your environment, this usually assigns a random publicly available endpoint to your service that you can access from anywhere in the world. On Google Kubernetes Engine, this is a public IP address:

```console
$ kubectl get svc
NAME      CLUSTER-IP     EXTERNAL-IP     PORT(S)        AGE
nginx     10.3.249.226   35.187.104.85   80:32281/TCP   1m
```

But dealing with IPs for service discovery isn't nice, so you register this IP with your DNS provider under a better name—most likely, one that corresponds to your service name. If the IP changes, you update the DNS record accordingly.

Those times are over! ExternalDNS takes care of that last step for you by keeping your DNS records synchronized with your external entry points.

ExternalDNS' usefulness also becomes clear when you use Ingresses to allow external traffic into your cluster. Via Ingress, you can tell Kubernetes to route traffic to different services based on certain HTTP request attributes, e.g. the Host header:

```console
$ kubectl get ing
NAME         HOSTS                                      ADDRESS         PORTS     AGE
entrypoint   frontend.example.org,backend.example.org   35.186.250.78   80        1m
```

But there's nothing that actually makes clients resolve those hostnames to the Ingress' IP address. Again, you normally have to register each entry with your DNS provider. Only if you're lucky can you use a wildcard, like in the example above.

ExternalDNS can solve this for you as well.

### Which DNS providers are supported?

Please check the [provider status table](https://github.com/kubernetes-sigs/external-dns#status-of-providers) for the list of supported providers and their status.

As stated in the README, we are currently looking for stable maintainers for those providers, to ensure that bugfixes and new features will be available for all of those.

### Which Kubernetes objects are supported?

Services exposed via `type=LoadBalancer`, `type=ExternalName` and for the hostnames defined in Ingress objects as well as headless hostPort services. An initial effort to support type `NodePort` was started as of May 2018 and it is in progress at the time of writing.

### How do I specify a DNS name for my Kubernetes objects?

There are three sources of information for ExternalDNS to decide on DNS name. ExternalDNS will pick one in order as listed below:

1. For ingress objects ExternalDNS will create a DNS record based on the hosts specified for the ingress object, as well as the `external-dns.alpha.kubernetes.io/hostname` annotation. For services ExternalDNS will look for the annotation `external-dns.alpha.kubernetes.io/hostname` on the service and use the loadbalancer IP, it also will look for the annotation `external-dns.alpha.kubernetes.io/internal-hostname` on the service and use the service IP.
    - For ingresses, you can optionally force ExternalDNS to create records based on _either_ the hosts specified or the `external-dns.alpha.kubernetes.io/hostname` annotation. This behavior is controlled by
      setting the `external-dns.alpha.kubernetes.io/ingress-hostname-source` annotation on that ingress to either `defined-hosts-only` or `annotation-only`.

2. If compatibility mode is enabled (e.g. `--compatibility={mate,molecule}` flag), External DNS will parse annotations used by Zalando/Mate, wearemolecule/route53-kubernetes. Compatibility mode with Kops DNS Controller is planned to be added in the future.

3. If `--fqdn-template` flag is specified, e.g. `--fqdn-template={{.Name}}.my-org.com`, ExternalDNS will use service/ingress specifications for the provided template to generate DNS name.

### Can I specify multiple global FQDN templates?

Yes, you can. Pass in a comma separated list to `--fqdn-template`. Beaware this will double (triple, etc) the amount of DNS entries based on how many services, ingresses and so on you have and will get you faster towards the API request limit of your DNS provider.

### Which Service and Ingress controllers are supported?

Regarding Services, we'll support the OSI Layer 4 load balancers that Kubernetes creates on AWS and Google Kubernetes Engine, and possibly other clusters running on Google Compute Engine.

Regarding Ingress, we'll support:
* Google's Ingress Controller on GKE that integrates with their Layer 7 load balancers (GLBC)
* nginx-ingress-controller v0.9.x with a fronting Service
* Zalando's [AWS Ingress controller](https://github.com/zalando-incubator/kube-ingress-aws-controller), based on AWS ALBs and [Skipper](https://github.com/zalando/skipper)
* [Traefik](https://github.com/containous/traefik)
  * version 1.7, when [`kubernetes.ingressEndpoint`](https://docs.traefik.io/v1.7/configuration/backends/kubernetes/#ingressendpoint) is configured (`kubernetes.ingressEndpoint.useDefaultPublishedService` in the [Helm chart](https://github.com/helm/charts/tree/HEAD/stable/traefik#configuration))
  * versions \>=2.0, when [`providers.kubernetesIngress.ingressEndpoint`](https://doc.traefik.io/traefik/providers/kubernetes-ingress/#ingressendpoint) is configured (`providers.kubernetesIngress.publishedService.enabled` is set to `true` in the [new Helm chart](https://github.com/traefik/traefik-helm-chart))

### Are other Ingress Controllers supported?

For Ingress objects, ExternalDNS will attempt to discover the target hostname of the relevant Ingress Controller automatically. If you are using an Ingress Controller that is not listed above you may have issues with ExternalDNS not discovering Endpoints and consequently not creating any DNS records. As a workaround, it is possible to force create an Endpoint by manually specifying a target host/IP for the records to be created by setting the annotation `external-dns.alpha.kubernetes.io/target` in the Ingress object.

Another reason you may want to override the ingress hostname or IP address is if you have an external mechanism for handling failover across ingress endpoints. Possible scenarios for this would include using [keepalived-vip](https://github.com/kubernetes/contrib/tree/HEAD/keepalived-vip) to manage failover faster than DNS TTLs might expire.

Note that if you set the target to a hostname, then a CNAME record will be created. In this case, the hostname specified in the Ingress object's annotation must already exist. (i.e. you have a Service resource for your Ingress Controller with the `external-dns.alpha.kubernetes.io/hostname` annotation set to the same value.)

### What about other projects similar to ExternalDNS?

ExternalDNS is a joint effort to unify different projects accomplishing the same goals, namely:

* Kops' [DNS Controller](https://github.com/kubernetes/kops/tree/HEAD/dns-controller)
* Zalando's [Mate](https://github.com/linki/mate)
* Molecule Software's [route53-kubernetes](https://github.com/wearemolecule/route53-kubernetes)

We strive to make the migration from these implementations a smooth experience. This means that, for some time, we'll support their annotation semantics in ExternalDNS and allow both implementations to run side-by-side. This enables you to migrate incrementally and slowly phase out the other implementation.

### How does it work with other implementations and legacy records?

ExternalDNS will allow you to opt into any Services and Ingresses that you want it to consider, by an annotation. This way, it can co-exist with other implementations running in the same cluster if they also support this pattern. However, we'll most likely declare ExternalDNS to be the default implementation. This means that ExternalDNS will consider Services and Ingresses that don't specifically declare which controller they want to be processed by; this is similar to the `ingress.class` annotation on GKE.

### I'm afraid you will mess up my DNS records!

ExternalDNS since v0.3 implements the concept of owning DNS records. This means that ExternalDNS will keep track of which records it has control over, and will never modify any records over which it doesn't have control. This is a fundamental requirement to operate ExternalDNS safely when there might be other actors creating DNS records in the same target space.

For now ExternalDNS uses TXT records to label owned records, and there might be other alternatives coming in the future releases.

### Does anyone use ExternalDNS in production?

Yes, multiple companies are using ExternalDNS in production. Zalando, as an example, has been using it in production since its v0.3 release, mostly using the AWS provider.

### How can we start using ExternalDNS?

Check out the following descriptive tutorials on how to run ExternalDNS in [GKE](tutorials/gke.md) and [AWS](tutorials/aws.md) or any other supported provider.

### Why is ExternalDNS only adding a single IP address in Route 53 on AWS when using the `nginx-ingress-controller`? How do I get it to use the FQDN of the ELB assigned to my `nginx-ingress-controller` Service instead?

By default the `nginx-ingress-controller` assigns a single IP address to an Ingress resource when it's created. ExternalDNS uses what's assigned to the Ingress resource, so it too will use this single IP address when adding the record in Route 53.

In most AWS deployments, you'll instead want the Route 53 entry to be the FQDN of the ELB that is assigned to the `nginx-ingress-controller` Service. To accomplish this, when you create the `nginx-ingress-controller` Deployment, you need to provide the `--publish-service` option to the `/nginx-ingress-controller` executable under `args`. Once this is deployed new Ingress resources will get the ELB's FQDN and ExternalDNS will use the same when creating records in Route 53.

According to the `nginx-ingress-controller` [docs](https://kubernetes.github.io/ingress-nginx/) the value you need to provide `--publish-service` is:

> Service fronting the ingress controllers. Takes the form namespace/name. The controller will set the endpoint records on the ingress objects to reflect those on the service.

For example if your `nginx-ingress-controller` Service's name is `nginx-ingress-controller-svc` and it's in the `default` namespace the start of your resource YAML might look like the following. Note the second to last line.

```
apiVersion: apps/v1
kind: Deployment
metadata:
  name: nginx-ingress-controller
spec:
  replicas: 1
  selector:
    matchLabels:
      app: nginx-ingress
  template:
    metadata:
      labels:
        app: nginx-ingress
    spec:
      hostNetwork: false
      containers:
        - name: nginx-ingress-controller
          image: "gcr.io/google_containers/nginx-ingress-controller:0.9.0-beta.11"
          imagePullPolicy: "IfNotPresent"
          args:
            - /nginx-ingress-controller
            - --default-backend-service={your-backend-service}
            - --publish-service=default/nginx-ingress-controller-svc
            - --configmap={your-configmap}
```

### I have a Service/Ingress but it's ignored by ExternalDNS. Why?

ExternalDNS can be configured to only use Services or Ingresses as source. In case Services or Ingresses seem to be ignored in your setup, consider checking how the flag `--source` was configured when deployed. For reference, see the issue https://github.com/kubernetes-sigs/external-dns/issues/267.

### I'm using an ELB with TXT registry but the CNAME record clashes with the TXT record. How to avoid this?

CNAMEs cannot co-exist with other records, therefore you can use the `--txt-prefix` flag which makes sure to create a TXT record with a name following the pattern `prefix.<CNAME record>`. For reference, see the issue https://github.com/kubernetes-sigs/external-dns/issues/262.

### Can I force ExternalDNS to create CNAME records for ELB/ALB?

The default logic is: when a target looks like an ELB/ALB, ExternalDNS will create ALIAS records for it.
Under certain circumstances you want to force ExternalDNS to create CNAME records instead. If you want to do that, start ExternalDNS with the `--aws-prefer-cname` flag.

Why should I want to force ExternalDNS to create CNAME records for ELB/ALB? Some motivations of users were:

> "Our hosted zones records are synchronized with our enterprise DNS. The record type ALIAS is an AWS proprietary record type and AWS allows you to set a DNS record directly on AWS resources. Since this is not a DNS RfC standard and therefore can not be transferred and created in our enterprise DNS. So we need to force CNAME creation instead."

or

> "In case of ALIAS if we do nslookup with domain name, it will return only IPs of ELB. So it is always difficult for us to locate ELB in AWS console to which domain is pointing. If we configure it with CNAME it will return exact ELB CNAME, which is more helpful.!"

### Which permissions do I need when running ExternalDNS on a GCE or GKE node.

You need to add either https://www.googleapis.com/auth/ndev.clouddns.readwrite or https://www.googleapis.com/auth/cloud-platform on your instance group's scope.

### What metrics can I get from ExternalDNS and what do they mean?

ExternalDNS exposes 2 types of metrics: Sources and Registry errors.

`Source`s are mostly Kubernetes API objects. Examples of `source` errors may be connection errors to the Kubernetes API server itself or missing RBAC permissions. It can also stem from incompatible configuration in the objects itself like invalid characters, processing a broken fqdnTemplate, etc.

`Registry` errors are mostly Provider errors, unless there's some coding flaw in the registry package. Provider errors often arise due to accessing their APIs due to network or missing cloud-provider permissions when reading records. When applying a changeset, errors will arise if the changeset applied is incompatible with the current state.

In case of an increased error count, you could correlate them with the `http_request_duration_seconds{handler="instrumented_http"}` metric which should show increased numbers for status codes 4xx (permissions, configuration, invalid changeset) or 5xx (apiserver down).

You can use the host label in the metric to figure out if the request was against the Kubernetes API server (Source errors) or the DNS provider API (Registry/Provider errors).

Here is the full list of available metrics provided by ExternalDNS:

| Name                                                | Description                                             | Type    |
| --------------------------------------------------- | ------------------------------------------------------- | ------- |
| external_dns_controller_last_sync_timestamp_seconds | Timestamp of last successful sync with the DNS provider | Gauge   |
| external_dns_registry_endpoints_total               | Number of Endpoints in all sources                      | Gauge   |
| external_dns_registry_errors_total                  | Number of Registry errors                               | Counter |
| external_dns_source_endpoints_total                 | Number of Endpoints in the registry                     | Gauge   |
| external_dns_source_errors_total                    | Number of Source errors                                 | Counter |

### How can I run ExternalDNS under a specific GCP Service Account, e.g. to access DNS records in other projects?

Have a look at https://github.com/linki/mate/blob/v0.6.2/examples/google/README.md#permissions

### How do I configure multiple Sources via environment variables? (also applies to domain filters)

Separate the individual values via a line break. The equivalent of `--source=service --source=ingress` would be `service\ningress`. However, it can be tricky do define that depending on your environment. The following examples work (zsh):

Via docker:

```console
$ docker run \
  -e EXTERNAL_DNS_SOURCE=$'service\ningress' \
  -e EXTERNAL_DNS_PROVIDER=google \
  -e EXTERNAL_DNS_DOMAIN_FILTER=$'foo.com\nbar.com' \
  k8s.gcr.io/external-dns/external-dns:v0.7.6
time="2017-08-08T14:10:26Z" level=info msg="config: &{APIServerURL: KubeConfig: Sources:[service ingress] Namespace: ...
```


Locally:

```console
$ export EXTERNAL_DNS_SOURCE=$'service\ningress'
$ external-dns --provider=google
INFO[0000] config: &{APIServerURL: KubeConfig: Sources:[service ingress] Namespace: ...
```

```
$ EXTERNAL_DNS_SOURCE=$'service\ningress' external-dns --provider=google
INFO[0000] config: &{APIServerURL: KubeConfig: Sources:[service ingress] Namespace: ...
```

In a Kubernetes manifest:

```yaml
spec:
  containers:
  - name: external-dns
    args:
    - --provider=google
    env:
    - name: EXTERNAL_DNS_SOURCE
      value: "service\ningress"
```

Or preferably:

```yaml
spec:
  containers:
  - name: external-dns
    args:
    - --provider=google
    env:
    - name: EXTERNAL_DNS_SOURCE
      value: |-
        service
        ingress
```


### Running an internal and external dns service

Sometimes you need to run an internal and an external dns service.
The internal one should provision hostnames used on the internal network (perhaps inside a VPC), and the external
one to expose DNS to the internet.

To do this with ExternalDNS you can use the `--annotation-filter` to specifically tie an instance of ExternalDNS to
an instance of a ingress controller. Let's assume you have two ingress controllers `nginx-internal` and `nginx-external`
then you can start two ExternalDNS providers one with `--annotation-filter=kubernetes.io/ingress.class in (nginx-internal)`
and one with `--annotation-filter=kubernetes.io/ingress.class in (nginx-external)`.

If you need to search for multiple values of said annotation, you can provide a comma separated list, like so:
`--annotation-filter=kubernetes.io/ingress.class in (nginx-internal, alb-ingress-internal)`.

Beware when using multiple sources, e.g. `--source=service --source=ingress`, `--annotation-filter` will filter every given source objects.
If you need to filter only one specific source you have to run a separated external dns service containing only the wanted `--source`  and `--annotation-filter`.

### How do I specify that I want the DNS record to point to either the Node's public or private IP when it has both?

If your Nodes have both public and private IP addresses, you might want to write DNS records with one or the other.
For example, you may want to write a DNS record in a private zone that resolves to your Nodes' private IPs so that traffic never leaves your private network.

To accomplish this, set this annotation on your service: `external-dns.alpha.kubernetes.io/access=private`
Conversely, to force the public IP: `external-dns.alpha.kubernetes.io/access=public`

If this annotation is not set, and the node has both public and private IP addresses, then the public IP will be used by default.

### Can external-dns manage(add/remove) records in a hosted zone which is setup in different AWS account?

Yes, give it the correct cross-account/assume-role permissions and use the `--aws-assume-role` flag https://github.com/kubernetes-sigs/external-dns/pull/524#issue-181256561

### How do I provide multiple values to the annotation `external-dns.alpha.kubernetes.io/hostname`?

Separate them by `,`.


### Are there official Docker images provided?

When we tag a new release, we push a container image to the Kubernetes projects official container registry with the following name:

```
k8s.gcr.io/external-dns/external-dns
```

As tags, you use the external-dns release of choice(i.e. `v0.7.6`). A `latest` tag is not provided in the container registry.

If you wish to build your own image, you can use the provided [Dockerfile](../Dockerfile) as a starting point.

### Which architectures are supported?

From `v0.7.5` on we support `amd64`, `arm32v7` and `arm64v8`. This means that you can run ExternalDNS on a Kubernetes cluster backed by Rasperry Pis or on ARM instances in the cloud as well as more traditional machines backed by `amd64` compatible CPUs.

### Which operating systems are supported?

At the time of writing we only support GNU/linux and we have no plans of supporting Windows or other operating systems.

### Why am I seeing time out errors even though I have connectivity to my cluster?

If you're seeing an error such as this:
```
FATA[0060] failed to sync cache: timed out waiting for the condition
```

You may not have the correct permissions required to query all the necessary resources in your kubernetes cluster. Specifically, you may be running in a `namespace` that you don't have these permissions in. By default, commands are run against the `default` namespace. Try changing this to your particular namespace to see if that fixes the issue.
