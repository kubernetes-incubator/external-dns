# Setting up ExternalDNS for F5 DNS Service

This tutorial describes how to setup ExternalDNS for usage with F5 DNS Service.


This tutorial assumes you have substituted the correct values for the following environment variables:

```
export F5_USERNAME="abcd@f5.com"
export F5_PASSWORD="abcde"
export F5_ACCOUNT="a-aavtexcy"
```

## Creating a F5 DNS Service zone

The F5 DNS provider for ExternalDNS will create DNS records for the domain-filters configured on external-dns. For each domain-filter, if a k8s service/ingress is found with an annotation which is a subdomain of the domain-filter, the DNS provider will create DNS records in F5 DNS service.

For example, if you have multiple k8s services like "hr.example.com", "dev.example.com", then you can set the domain-filter configuration on external-dns to "example.com". F5 DNS Provider will take care of creating DNS Records and Load-balanced Records for each service which uses the external-dns hostname annotation. You can create multiple sub-domains corresponding to your service if you want them to be managed by ExternalDNS.

## Creating a F5 Configuration Secret

For ExternalDNS to access the F5 API, create a Kubernetes secret.

To create the secret:

```
$ kubectl create secret generic external-dns \
      --from-literal=EXTERNAL_DNS_F5_DNS_USERNAME=${F5_USERNAME} \
      --from-literal=EXTERNAL_DNS_F5_DNS_PASSWORD=${F5_PASSWORD} \
      --from-literal=EXTERNAL_DNS_F5_DNS_ACCOUNT_ID=${F5_ACCOUNT}
```

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
      - name: external-dns
        image: external-dns:latest
        imagePullPolicy: Never
        args:
        - --source=service
        - --provider=f5
        - --domain-filter=example.com # note that this will filter only example.com domains
        env:
        - name: EXTERNAL_DNS_F5_DNS_ACCOUNT_ID
          valueFrom:
            secretKeyRef:
              name: external-dns
              key: EXTERNAL_DNS_F5_DNS_ACCOUNT_ID
        - name: EXTERNAL_DNS_F5_DNS_USERNAME
          valueFrom:
            secretKeyRef:
              name: external-dns
              key: EXTERNAL_DNS_F5_DNS_USERNAME
        - name: EXTERNAL_DNS_F5_DNS_PASSWORD
          valueFrom:
            secretKeyRef:
              name: external-dns
              key: EXTERNAL_DNS_F5_DNS_PASSWORD
```
### Manifest (for clusters with RBAC enabled)
```yaml
apiVersion: v1
kind: ServiceAccount
metadata:
  name: external-dns-test
---
apiVersion: rbac.authorization.k8s.io/v1beta1
kind: ClusterRole
metadata:
  namespace: f5aas-unstable  # Change as per your deployment
  name: external-dns-test
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
  namespace: f5aas-unstable # Change as per your deployment
  name: external-dns-test
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: ClusterRole
  name: external-dns-test
subjects:
- kind: ServiceAccount
  name: external-dns-test
  namespace: f5aas-unstable # Change as per your deployment
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: external-dns-test
spec:
  strategy:
    type: Recreate
  selector:
    matchLabels:
      app: external-dns-test
  template:
    metadata:
      labels:
        app: external-dns-test
    spec:
      serviceAccountName: external-dns-test
      containers:
      - name: external-dns-test
        image: 330787392602.dkr.ecr.us-east-1.amazonaws.com/f5aas/external-dns-f5:latest
        imagePullPolicy: Always
        args:
        - --source=service
        - --provider=f5
        - --domain-filter=example.com # Change as per your deployment
        env:
        - name: EXTERNAL_DNS_F5_DNS_ACCOUNT_ID
          valueFrom:
            secretKeyRef:
              name: external-dns
              key: EXTERNAL_DNS_F5_DNS_ACCOUNT_ID
        - name: EXTERNAL_DNS_F5_DNS_USERNAME
          valueFrom:
            secretKeyRef:
              name: external-dns
              key: EXTERNAL_DNS_F5_DNS_USERNAME
        - name: EXTERNAL_DNS_F5_DNS_PASSWORD
          valueFrom:
            secretKeyRef:
              name: external-dns
              key: EXTERNAL_DNS_F5_DNS_PASSWORD
```
## Deploying a Nginx Service

Create a service file called 'nginx.yaml' with the following contents if you want to have its DNS records managed by external-dns with F5 DNS Provider:

```yaml
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
---
apiVersion: v1
kind: Service
metadata:
  name: nginx
  annotations:
    # Not that this hostname is the sub-domain
    external-dns.alpha.kubernetes.io/hostname: test.example.com
spec:
  selector:
    app: nginx
  type: LoadBalancer
  ports:
    - protocol: TCP
      port: 80
      targetPort: 80
```

Note the annotation on the service; use the same hostname as the domain-filter specified above. The annotation may also be a subdomain of the DNS zone (e.g. 'web.example.com').

ExternalDNS uses this annotation to determine what services should be registered with DNS.  Removing the annotation will cause ExternalDNS to remove the corresponding DNS records.

Create the deployment and service:

```
$ kubectl create -f nginx.yaml
```

It takes a little while for the F5 DNS cloud provider to create an external IP for the service.  Check the status by running
`kubectl get services nginx`.  If the `EXTERNAL-IP` field shows an address, the service is ready to be accessed externally.

Once the service has an external IP assigned, ExternalDNS will notice the new service IP address and synchronize the F5 DNS records.

## Verifying F5 DNS records

Check the F5 DNS Service portal or use the REST API to find the DNS Records and Load balanced records.

This should show the external IP address of the service as the A record for your domain ('@' indicates the record is for the zone itself).

You can also use dig to check the DNS response

```
dig +norecurse @ns1.f5cloudservices.com test.example.com
```

## DNS Record Ownership

