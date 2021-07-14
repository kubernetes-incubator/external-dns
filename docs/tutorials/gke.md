# Setting up ExternalDNS on Google Kubernetes Engine

This tutorial describes how to setup ExternalDNS for usage within a GKE cluster. Make sure to use **>=0.4** version of ExternalDNS for this tutorial

## Set up your environment

Setup your environment to work with Google Cloud Platform. Fill in your values as needed, e.g. target project.

```console
$ gcloud config set project "zalando-external-dns-test"
$ gcloud config set compute/region "europe-west1"
$ gcloud config set compute/zone "europe-west1-d"
```

## GKE Node Scopes

*If you prefer to try-out ExternalDNS in one of the existing environments you can skip this step*

The following instructions use instance scopes to provide ExternalDNS with the
permissions it needs to manage DNS records. Note that since these permissions
are associated with the instance, all pods in the cluster will also have these
permissions. As such, this approach is not suitable for anything but testing
environments.

Create a GKE cluster.

```console
$ gcloud container clusters create "external-dns" \
    --num-nodes 1 \
    --scopes "https://www.googleapis.com/auth/ndev.clouddns.readwrite"
```

Create a DNS zone which will contain the managed DNS records.

```console
$ gcloud dns managed-zones create "external-dns-test-gcp-zalan-do" \
    --dns-name "external-dns-test.gcp.zalan.do." \
    --description "Automatically managed zone by kubernetes.io/external-dns"
```

Make a note of the nameservers that were assigned to your new zone.

```console
$ gcloud dns record-sets list \
    --zone "external-dns-test-gcp-zalan-do" \
    --name "external-dns-test.gcp.zalan.do." \
    --type NS
NAME                             TYPE  TTL    DATA
external-dns-test.gcp.zalan.do.  NS    21600  ns-cloud-e1.googledomains.com.,ns-cloud-e2.googledomains.com.,ns-cloud-e3.googledomains.com.,ns-cloud-e4.googledomains.com.
```

In this case it's `ns-cloud-{e1-e4}.googledomains.com.` but your's could slightly differ, e.g. `{a1-a4}`, `{b1-b4}` etc.

Tell the parent zone where to find the DNS records for this zone by adding the corresponding NS records there. Assuming the parent zone is "gcp-zalan-do" and the domain is "gcp.zalan.do" and that it's also hosted at Google we would do the following.

```console
$ gcloud dns record-sets transaction start --zone "gcp-zalan-do"
$ gcloud dns record-sets transaction add ns-cloud-e{1..4}.googledomains.com. \
    --name "external-dns-test.gcp.zalan.do." --ttl 300 --type NS --zone "gcp-zalan-do"
$ gcloud dns record-sets transaction execute --zone "gcp-zalan-do"
```

### Deploy ExternalDNS

Then apply the following manifests file to deploy ExternalDNS.

```yaml
apiVersion: v1
kind: ServiceAccount
metadata:
  name: external-dns
---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: external-dns
rules:
- apiGroups: [""]
  resources: ["services","endpoints","pods"]
  verbs: ["get","watch","list"]
- apiGroups: ["extensions","networking.k8s.io"]
  resources: ["ingresses"] 
  verbs: ["get","watch","list"]
- apiGroups: [""]
  resources: ["nodes"]
  verbs: ["get", "watch", "list"]
---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRoleBinding
metadata:
  name: external-dns-viewer
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: ClusterRole
  name: external-dns
subjects:
- kind: ServiceAccount
  name: external-dns
  namespace: default
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: external-dns
spec:
  strategy:
    type: Recreate
  selector:
    matchLabels:
      app: external-dns
  template:
    metadata:
      labels:
        app: external-dns
    spec:
      serviceAccountName: external-dns
      containers:
      - name: external-dns
        image: k8s.gcr.io/external-dns/external-dns:v0.8.0
        args:
        - --source=service
        - --source=ingress
        - --domain-filter=external-dns-test.gcp.zalan.do # will make ExternalDNS see only the hosted zones matching provided domain, omit to process all available hosted zones
        - --provider=google
#        - --google-project=zalando-external-dns-test # Use this to specify a project different from the one external-dns is running inside
        - --google-zone-visibility=private # Use this to filter to only zones with this visibility. Set to either 'public' or 'private'. Omitting will match public and private zones
        - --policy=upsert-only # would prevent ExternalDNS from deleting any records, omit to enable full synchronization
        - --registry=txt
        - --txt-owner-id=my-identifier
```

Use `--dry-run` if you want to be extra careful on the first run. Note, that you will not see any records created when you are running in dry-run mode. You can, however, inspect the logs and watch what would have been done.

### Verify ExternalDNS works

Create the following sample application to test that ExternalDNS works.

```yaml
apiVersion: v1
kind: Service
metadata:
  name: nginx
  annotations:
    external-dns.alpha.kubernetes.io/hostname: nginx.external-dns-test.gcp.zalan.do.
spec:
  type: LoadBalancer
  ports:
  - port: 80
    targetPort: 80
  selector:
    app: nginx

---

apiVersion: apps/v1
kind: Deployment
metadata:
  name: nginx
spec:
  selector:
    matchLabels:
      app: nginx
  template:
    metadata:
      labels:
        app: nginx
    spec:
      containers:
      - image: nginx
        name: nginx
        ports:
        - containerPort: 80
```

After roughly two minutes check that a corresponding DNS record for your service was created.

```console
$ gcloud dns record-sets list \
    --zone "external-dns-test-gcp-zalan-do" \
    --name "nginx.external-dns-test.gcp.zalan.do."

NAME                                   TYPE  TTL  DATA
nginx.external-dns-test.gcp.zalan.do.  A     300  104.155.60.49
nginx.external-dns-test.gcp.zalan.do.  TXT   300  "heritage=external-dns,external-dns/owner=my-identifier"
```

Note created TXT record alongside A record. TXT record signifies that the corresponding A record is managed by ExternalDNS. This makes ExternalDNS safe for running in environments where there are other records managed via other means.

Let's check that we can resolve this DNS name. We'll ask the nameservers assigned to your zone first.

```console
$ dig +short @ns-cloud-e1.googledomains.com. nginx.external-dns-test.gcp.zalan.do.
104.155.60.49
```

Given you hooked up your DNS zone with its parent zone you can use `curl` to access your site.

```console
$ curl nginx.external-dns-test.gcp.zalan.do
<!DOCTYPE html>
<html>
<head>
<title>Welcome to nginx!</title>
...
</head>
<body>
...
</body>
</html>
```

Let's check that Ingress works as well. Create the following Ingress.

```yaml
apiVersion: networking.k8s.io/v1
kind: Ingress
metadata:
  name: nginx
spec:
  rules:
  - host: via-ingress.external-dns-test.gcp.zalan.do
    http:
      paths:
      - backend:
          serviceName: nginx
          servicePort: 80
```

Again, after roughly two minutes check that a corresponding DNS record for your Ingress was created.

```console
$ gcloud dns record-sets list \
    --zone "external-dns-test-gcp-zalan-do" \
    --name "via-ingress.external-dns-test.gcp.zalan.do." \

NAME                                         TYPE  TTL  DATA
via-ingress.external-dns-test.gcp.zalan.do.  A     300  130.211.46.224
via-ingress.external-dns-test.gcp.zalan.do.  TXT   300  "heritage=external-dns,external-dns/owner=my-identifier"
```

Let's check that we can resolve this DNS name as well.

```console
dig +short @ns-cloud-e1.googledomains.com. via-ingress.external-dns-test.gcp.zalan.do.
130.211.46.224
```

Try with `curl` as well.

```console
$ curl via-ingress.external-dns-test.gcp.zalan.do
<!DOCTYPE html>
<html>
<head>
<title>Welcome to nginx!</title>
...
</head>
<body>
...
</body>
</html>
```

### Clean up

Make sure to delete all Service and Ingress objects before terminating the cluster so all load balancers get cleaned up correctly.

```console
$ kubectl delete service nginx
$ kubectl delete ingress nginx
```

Give ExternalDNS some time to clean up the DNS records for you. Then delete the managed zone and cluster.

```console
$ gcloud dns managed-zones delete "external-dns-test-gcp-zalan-do"
$ gcloud container clusters delete "external-dns"
```

Also delete the NS records for your removed zone from the parent zone.

```console
$ gcloud dns record-sets transaction start --zone "gcp-zalan-do"
$ gcloud dns record-sets transaction remove ns-cloud-e{1..4}.googledomains.com. \
    --name "external-dns-test.gcp.zalan.do." --ttl 300 --type NS --zone "gcp-zalan-do"
$ gcloud dns record-sets transaction execute --zone "gcp-zalan-do"
```

## GKE with Workload Identity

The following instructions use [GKE workload
identity](https://cloud.google.com/kubernetes-engine/docs/how-to/workload-identity)
to provide ExternalDNS with the permissions it needs to manage DNS records.
Workload identity is the Google-recommended way to provide GKE workloads access
to GCP APIs.

Create a GKE cluster with workload identity enabled.

```console
$ gcloud container clusters create external-dns \
    --workload-metadata-from-node=GKE_METADATA_SERVER \
    --identity-namespace=zalando-external-dns-test.svc.id.goog
```

Create a GCP service account (GSA) for ExternalDNS and save its email address.

```console
$ sa_name="Kubernetes external-dns"
$ gcloud iam service-accounts create sa-edns --display-name="$sa_name"
$ sa_email=$(gcloud iam service-accounts list --format='value(email)' \
    --filter="displayName:$sa_name")
```

Bind the ExternalDNS GSA to the DNS admin role.

```console
$ gcloud projects add-iam-policy-binding zalando-external-dns-test \
    --member="serviceAccount:$sa_email" --role=roles/dns.admin
```

Link the ExternalDNS GSA to the Kubernetes service account (KSA) that
external-dns will run under, i.e., the external-dns KSA in the external-dns
namespaces.

```console
$ gcloud iam service-accounts add-iam-policy-binding "$sa_email" \
    --member="serviceAccount:zalando-external-dns-test.svc.id.goog[external-dns/external-dns]" \
    --role=roles/iam.workloadIdentityUser
```

Create a DNS zone which will contain the managed DNS records.

```console
$ gcloud dns managed-zones create external-dns-test-gcp-zalan-do \
    --dns-name=external-dns-test.gcp.zalan.do. \
    --description="Automatically managed zone by ExternalDNS"
```

Make a note of the nameservers that were assigned to your new zone.

```console
$ gcloud dns record-sets list \
    --zone=external-dns-test-gcp-zalan-do \
    --name=external-dns-test.gcp.zalan.do. \
    --type NS
NAME                             TYPE  TTL    DATA
external-dns-test.gcp.zalan.do.  NS    21600  ns-cloud-e1.googledomains.com.,ns-cloud-e2.googledomains.com.,ns-cloud-e3.googledomains.com.,ns-cloud-e4.googledomains.com.
```

In this case it's `ns-cloud-{e1-e4}.googledomains.com.` but your's could
slightly differ, e.g. `{a1-a4}`, `{b1-b4}` etc.

Tell the parent zone where to find the DNS records for this zone by adding the
corresponding NS records there. Assuming the parent zone is "gcp-zalan-do" and
the domain is "gcp.zalan.do" and that it's also hosted at Google we would do the
following.

```console
$ gcloud dns record-sets transaction start --zone=gcp-zalan-do
$ gcloud dns record-sets transaction add ns-cloud-e{1..4}.googledomains.com. \
    --name=external-dns-test.gcp.zalan.do. --ttl 300 --type NS --zone=gcp-zalan-do
$ gcloud dns record-sets transaction execute --zone=gcp-zalan-do
```

Connect your `kubectl` client to the cluster you just created and bind your GCP
user to the cluster admin role in Kubernetes.

```console
$ gcloud container clusters get-credentials external-dns
$ kubectl create clusterrolebinding cluster-admin-me \
    --clusterrole=cluster-admin --user="$(gcloud config get-value account)"
```

### Deploy ExternalDNS

Apply the following manifest file to deploy external-dns.

```yaml
apiVersion: v1
kind: Namespace
metadata:
  name: external-dns
---
apiVersion: v1
kind: ServiceAccount
metadata:
  name: external-dns
  namespace: external-dns
---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: external-dns
rules:
  - apiGroups: [""]
    resources: ["services", "endpoints", "pods"]
    verbs: ["get", "watch", "list"]
  - apiGroups: ["extensions", "networking.k8s.io"]
    resources: ["ingresses"]
    verbs: ["get", "watch", "list"]
  - apiGroups: [""]
    resources: ["nodes"]
    verbs: ["list"]
---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRoleBinding
metadata:
  name: external-dns-viewer
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: ClusterRole
  name: external-dns
subjects:
  - kind: ServiceAccount
    name: external-dns
    namespace: external-dns
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: external-dns
  namespace: external-dns
spec:
  strategy:
    type: Recreate
  selector:
    matchLabels:
      app: external-dns
  template:
    metadata:
      labels:
        app: external-dns
    spec:
      containers:
        - args:
            - --source=ingress
            - --source=service
            - --domain-filter=external-dns-test.gcp.zalan.do
            - --provider=google
            - --google-project=zalando-external-dns-test
            - --registry=txt
            - --txt-owner-id=my-identifier
          image: k8s.gcr.io/external-dns/external-dns:v0.8.0
          name: external-dns
      securityContext:
        fsGroup: 65534
        runAsUser: 65534
      serviceAccountName: external-dns
```

Then add the proper workload identity annotation to the cert-manager service
account.

```bash
$ kubectl annotate serviceaccount --namespace=external-dns external-dns \
    "iam.gke.io/gcp-service-account=$sa_email"
```

### Deploy a sample application

Create the following sample application to test that ExternalDNS works.

```yaml
apiVersion: networking.k8s.io/v1
kind: Ingress
metadata:
  name: nginx
spec:
  rules:
  - host: via-ingress.external-dns-test.gcp.zalan.do
    http:
      paths:
      - backend:
          serviceName: nginx
          servicePort: 80
---
apiVersion: v1
kind: Service
metadata:
  annotations:
    external-dns.alpha.kubernetes.io/hostname: nginx.external-dns-test.gcp.zalan.do.
  name: nginx
spec:
  ports:
  - port: 80
    targetPort: 80
  selector:
    app: nginx
  type: LoadBalancer
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: nginx
spec:
  selector:
    matchLabels:
      app: nginx
  template:
    metadata:
      labels:
        app: nginx
    spec:
      containers:
      - image: nginx
        name: nginx
        ports:
        - containerPort: 80
```

After roughly two minutes check that a corresponding DNS records for your
service and ingress were created.

```console
$ gcloud dns record-sets list \
    --zone "external-dns-test-gcp-zalan-do" \
    --name "via-ingress.external-dns-test.gcp.zalan.do." \
    --type A
NAME                                         TYPE  TTL  DATA
nginx.external-dns-test.gcp.zalan.do.        A     300  104.155.60.49
nginx.external-dns-test.gcp.zalan.do.        TXT   300  "heritage=external-dns,external-dns/owner=my-identifier"
via-ingress.external-dns-test.gcp.zalan.do.  TXT   300  "heritage=external-dns,external-dns/owner=my-identifier"
via-ingress.external-dns-test.gcp.zalan.do.  A     300  35.187.1.246
```

Let's check that we can resolve this DNS name as well.

```console
$ dig +short @ns-cloud-e1.googledomains.com. via-ingress.external-dns-test.gcp.zalan.do.
35.187.1.246
```

Try with `curl` as well.

```console
$ curl via-ingress.external-dns-test.gcp.zalan.do
<!DOCTYPE html>
<html>
<head>
<title>Welcome to nginx!</title>
...
</head>
<body>
...
</body>
</html>
```

### Clean up

Make sure to delete all service and ingress objects before terminating the
cluster so all load balancers and DNS entries get cleaned up correctly.

```console
$ kubectl delete ingress nginx
$ kubectl delete service nginx
```

Give ExternalDNS some time to clean up the DNS records for you. Then delete the
managed zone and cluster.

```console
$ gcloud dns managed-zones delete external-dns-test-gcp-zalan-do
$ gcloud container clusters delete external-dns
```

Also delete the NS records for your removed zone from the parent zone.

```console
$ gcloud dns record-sets transaction start --zone gcp-zalan-do
$ gcloud dns record-sets transaction remove ns-cloud-e{1..4}.googledomains.com. \
    --name=external-dns-test.gcp.zalan.do. --ttl 300 --type NS --zone=gcp-zalan-do
$ gcloud dns record-sets transaction execute --zone=gcp-zalan-do
```

## User Demo How-To Blogs and Examples

* A full demo on GKE Kubernetes + CloudDNS + SA-Permissions [How-to Kubernetes with DNS management (ssl-manager pre-req)](https://medium.com/@jpantjsoha/how-to-kubernetes-with-dns-management-for-gitops-31239ea75d8d)
* Run external-dns on GKE with workload identity. See [Kubernetes, ingress-nginx, cert-manager & external-dns](https://blog.atomist.com/kubernetes-ingress-nginx-cert-manager-external-dns/)
