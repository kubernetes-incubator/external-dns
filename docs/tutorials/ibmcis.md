# Setting up ExternalDNS for IBM Cloud Internet Service


## Prerequisites
This tutorial describes how to setup ExternalDNS for usage with [IBM Cloud Internet Service](https://cloud.ibm.com/catalog/services/internet-services).

Make sure to use **>=0.7.4** version of ExternalDNS for this tutorial. 

The IBM Cloud Internet Service (ibmcis) expects that you have your Instance or Instances of ibmcis setup already with zones and configured correctly. It does not add, remove or configure new zones in anyway. If you need to do this please refer to the documentation for the [IBM Cloud Internet Service](https://cloud.ibm.com/catalog/services/internet-services).

What you need to provide in order for getting the external-dns running with the ibmcis provider is:
* Create an API-key that grants manager access to the ibmcis instance(s). You will need to make this available via an environment varible named `IC_API_KEY` for the external-dns pod.
* Extract the CRN(s) for the ibmcis instance(s).You will need to make this available via an environment variable named `IC_CIS_INSTANCE_CRN` for the external-dns pod.

### Creating API-key -- IC_API_KEY
Assuming you want to take the "long road" and create the API-key via a dediciated service id these are the steps you can use for inspiration. Notice the example is a command line example and requires IBM Cloud CLI. If you need help on getting that in place take a look at [Getting started with the IBM Cloud CLI](https://cloud.ibm.com/docs/cli?topic=cli-getting-started).

1. Generate a service id.
Generate a Service ID name `external-dns-service-id`.
And in this example give it `manager` rights on *ALL* ibmcis instances on the account, be sure to narrow this down if you only want to give it access to specific instances!

```bash
ibmcloud iam service-id-create external-dns-service-id -d "Service id that external-dns is going to use to manager the ibmcis instance(s)"

ibmcloud iam service-policy-create external-dns-service-id --service-name internet-svcs  --roles Manager

```

2. Generate an API-key
The step here will generate and extract an API-key. In case you loose this key it can not be extracted again, you will have to generate a new one. So do capture it and save it for the next step! 

```bash
ibmcloud iam service-api-key-create external-dns-ibmcis-apikey external-dns-service-id -d "API key that external-dns pod is going to work with
ibmcis to manage exposed dns"

Creating API key external-dns-ibmcis-apikey of service ID external-dns-service-id under account *** as ***...
OK
Service ID API key external-dns-ibmcis-apikey is created

Please preserve the API key! It cannot be retrieved after it is created.

'
ID            ApiKey-****   
Name          external-dns-ibmcis-apikey   
Description   API key that external-dns pod is going to work with   
              ibmcis to manage exposed dns   
Created At    ******
API Key       <<SAVE THIS KEY FOR NEXT STEP>>   
Locked        false   
'
```

3. Create (namespace and) secret
Create the namespace external-dns and save the API-key.
Connect your `kubectl` client to the cluster you want to use external-dns with.


```bash
kubectl create namespace external-dns
# Remember to put in your recorded key here
IC_API_KEY=<<YOUR KEY FROM PREVIOUS STEP>>
kubectl create secret generic external-dns -n external-dns \
      --from-literal=IC_API_KEY=${IC_API_KEY}

```


### Extracting instance ID(s) - IC_CIS_INSTANCE_CRN
To find the CRN or resource id consider using `ibmcloud resource service-instances` command. What you should be looking for is CRNs containing `crn:v1:bluemix:public:internet-svcs:global:`. An example could be to do it like this:
```bash
ibmcloud resource service-instances --all-resource-groups --long

crn:v1:bluemix:public:internet-svcs:global:****************                                                                                                                     ********               global     active   service_instance   **********  

```
If you have more than one then comma (`,`) seperate them into one string and apply them to `IC_CIS_INSTANCE_CRN` for the deployment as described below.

## Deploy ExternalDNS

Connect your `kubectl` client to the cluster you want to test ExternalDNS with.
Then apply one of the following manifests file to deploy ExternalDNS.

### Manifest (for clusters with RBAC enabled)

```yaml
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
  resources: ["services","endpoints","pods"]
  verbs: ["get","watch","list"]
- apiGroups: ["extensions","networking.k8s.io"]
  resources: ["ingresses"] 
  verbs: ["get","watch","list"]
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
  labels:
    app: external-dns
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
        image: k8s.gcr.io/external-dns/external-dns:v0.7.3
        args:
        - --source=ingress
        - --domain-filter=borup.work        # (optional) limit to only example.com domains.
        - --provider=ibmcis
        env:
        - name: "IC_CIS_INSTANCE_CRN"
          value: "xxxx"
        - name: "IC_API_KEY"
          valueFrom:
            secretKeyRef:
              name: external-dns
              key: IC_API_KEY
        
```

## Deploying an Nginx Service

Create a file called 'nginx.yaml' with the following contents, except changing the hostname under the Ingress to a domain you control, and is within the `--domain-filter=xxx` specified for the external-dns deployment above.

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
kind: Service
apiVersion: v1
metadata:
  name: nginx
  labels:
    app: nginx
    tier: web
spec:
  ports:
    - name: http
      protocol: TCP
      port: 80
      targetPort: 80
  selector:
    app: nginx
---
apiVersion: networking.k8s.io/v1
kind: Ingress
metadata:
  name: nginx
spec:
  rules:
   - host: 'external-dns.borup.work'
     http:
      paths:
        - path: /
          pathType: Prefix
          backend:
            service:
              name: nginx
              port:
                number: 80

```

Create the deployment, service and ingress:

```bash
 kubectl create -f nginx.yaml
```

It takes a little while for the external-dns to pickup the changes, default interval is 60 seconds. Look at the logs from the external-dns pod and you should see entries containing `ApplyChange - CreateDns` that will illustrate the needs for DNS records and the fact they are going to be created.

## Verifying the nginx service

Go to the hostname put into the hosts field and see a default nginx instance is now available, like http://<<fqdn name>>/.


## Clean up

### Nginx service
```bash
 kubectl delete -f nginx.yaml
```

### Remove external-dns service
Do kubectl delete of the resources defined in manitest section above.

