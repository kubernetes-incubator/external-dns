# Setting up ExternalDNS for Services on INWX DNS

This tutorial describes how to setup ExternalDNS for usage within a Kubernetes cluster using InterNetworX (INWX) DNS.

Make sure to use **>=0.7.7** version of ExternalDNS for this tutorial.

## Creating a INWX DNS zone

You have to add the domains to the nameserver of INWX to be able to use this provider.

## Creating INWX Credentials

Unfortunately INWX doesn't allow creating dedicated tokens or similar for accessing the API.
Therefore, you have to use your personal username and password to login. Additionally it seems that you should not use
2FA authentication.

The environment variable `EXTERNAL_DNS_INWX_USERNAME` and `EXTERNAL_DNS_INWX_PASSWORD` can be used to set the username
and password.

## Deploy ExternalDNS

Connect your `kubectl` client to the cluster you want to test ExternalDNS with.
Then apply one of the following manifests file to deploy ExternalDNS.

### Manifest (for clusters without RBAC enabled)
```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: external-dns
spec:
  replicas: 1
  selector:
    matchLabels:
      app: external-dns
  strategy:
    type: Recreate
  template:
    metadata:
      labels:
        app: external-dns
    spec:
      containers:
      - name: external-dns
        image: k8s.gcr.io/external-dns/external-dns:v0.7.7
        args:
        - --source=service # ingress is also possible
        - --domain-filter=example.com # (optional) limit to only example.com domains; change to match the zone created above.
        - --provider=inwx
        env:
          - name: EXTERNAL_DNS_INWX_USERNAME
            valueFrom:
              secretKeyRef:
                name: external-dns
                key: username
          - name: EXTERNAL_DNS_INWX_PASSWORD
            valueFrom:
              secretKeyRef:
                name: external-dns
                key: password
```

It is recommended to create a Kubernetes secret `external-dns` in the same namespace that has the keys `username` and 
`password`.

### Manifest (for clusters with RBAC enabled)
```yaml
apiVersion: v1
kind: ServiceAccount
metadata:
  name: external-dns
---
apiVersion: rbac.authorization.k8s.io/v1beta1
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
  verbs: ["list","watch"]
---
apiVersion: rbac.authorization.k8s.io/v1beta1
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
  replicas: 1
  selector:
    matchLabels:
      app: external-dns
  strategy:
    type: Recreate
  template:
    metadata:
      labels:
        app: external-dns
    spec:
      serviceAccountName: external-dns
      containers:
      - name: external-dns
        image: k8s.gcr.io/external-dns/external-dns:v0.7.7
        args:
        - --source=service # ingress is also possible
        - --domain-filter=example.com # (optional) limit to only example.com domains; change to match the zone created above.
        - --provider=inwx
        env:
          - name: EXTERNAL_DNS_INWX_USERNAME
            valueFrom:
              secretKeyRef:
                name: external-dns
                key: username
          - name: EXTERNAL_DNS_INWX_PASSWORD
            valueFrom:
              secretKeyRef:
                name: external-dns
                key: password
```

It is recommended to create a Kubernetes secret `external-dns` in the same namespace that has the keys `username` and
`password`.

## Deploying an Nginx Service

Create a service file called 'nginx.yaml' with the following contents:

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: nginx
spec:
  replicas: 1
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
---
apiVersion: v1
kind: Service
metadata:
  name: nginx
  annotations:
    external-dns.alpha.kubernetes.io/hostname: my-app.example.com
spec:
  selector:
    app: nginx
  type: LoadBalancer
  ports:
    - protocol: TCP
      port: 80
      targetPort: 80
```

Note the annotation on the service; use the same hostname as the INWX DNS zone created above.

ExternalDNS uses this annotation to determine what services should be registered with DNS. Removing the annotation will cause ExternalDNS to remove the corresponding DNS records.

Create the deployment and service:

```console
$ kubectl create -f nginx.yaml
```

Depending where you run your service it can take a little while for your cloud provider to create an external IP for the service.

Once the service has an external IP assigned, ExternalDNS will notice the new service IP address and synchronize the INWX DNS records.

## Verifying INWX DNS records

Check your [INWx DNS UI](https://www.inwx.de/en/nameserver2#tab=ns) to view the records for your INWX DNS zone.

Click on the zone for the one created above if a different domain was used.

This should show the external IP address of the service as the A record for your domain.

## Cleanup

Now that we have verified that ExternalDNS will automatically manage INWX DNS records, we can delete the tutorial's example:

```
$ kubectl delete service -f nginx.yaml
$ kubectl delete service -f externaldns.yaml
```
