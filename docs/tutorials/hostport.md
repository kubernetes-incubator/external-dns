# Setting up ExternalDNS for Headless Services 

This tutorial describes how to setup ExternalDNS for usage in conjunction with a Headless service.

## Use cases
The main use cases that inspired this feature is the necessity for fixed addressable hostnames with services, such as Kafka when trying to access them from outside the cluster. In this scenario, quite often, only the Node IP addresses are actually routable and as in systems like Kafka more direct connections are preferable.

## Setup

We will go through a small example of deploying a simple Kafka with use of a headless service.

### External DNS

A simple deploy could look like this:
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
        image: k8s.gcr.io/external-dns/external-dns:v0.7.6
        args:
        - --log-level=debug
        - --source=service
        - --source=ingress
        - --namespace=dev
        - --domain-filter=example.org. 
        - --provider=aws
        - --registry=txt
        - --txt-owner-id=dev.example.org
```

### Manifest (for clusters with RBAC enabled)
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
        image: k8s.gcr.io/external-dns/external-dns:v0.7.6
        args:
        - --log-level=debug
        - --source=service
        - --source=ingress
        - --namespace=dev
        - --domain-filter=example.org. 
        - --provider=aws
        - --registry=txt
        - --txt-owner-id=dev.example.org
```


### Kafka Stateful Set

First lets deploy a Kafka Stateful set, a simple example(a lot of stuff is missing) with a headless service called `ksvc`

```yaml
apiVersion: apps/v1beta1
kind: StatefulSet
metadata:
  name: kafka
spec:
  serviceName: ksvc
  replicas: 3
  template:
    metadata:
      labels:
        component: kafka
    spec:
      containers:
      - name:  kafka        
        image: confluent/kafka
        ports:
        - containerPort: 9092
          hostPort: 9092
          name: external
        command:
        - bash
        - -c
        - " export DOMAIN=$(hostname -d) && \
            export KAFKA_BROKER_ID=$(echo $HOSTNAME|rev|cut -d '-' -f 1|rev) && \
            export KAFKA_ZOOKEEPER_CONNECT=$ZK_CSVC_SERVICE_HOST:$ZK_CSVC_SERVICE_PORT && \
            export KAFKA_ADVERTISED_LISTENERS=PLAINTEXT://$HOSTNAME.example.org:9092 && \
            /etc/confluent/docker/run"
        volumeMounts:
        - name: datadir
          mountPath: /var/lib/kafka
  volumeClaimTemplates:
  - metadata:
      name: datadir
      annotations:
          volume.beta.kubernetes.io/storage-class: st1
    spec:
      accessModes: [ "ReadWriteOnce" ]
      resources:
        requests:
          storage:  500Gi
```
Very important here, is to set the `hostPort`(only works if the PodSecurityPolicy allows it)! and in case your app requires an actual hostname inside the container, unlike Kafka, which can advertise on another address, you have to set the hostname yourself.

### Headless Service

Now we need to define a headless service to use to expose the Kafka pods. There are generally two approaches to use expose the nodeport of a Headless service:

1. Add `--fqdn-template={{name}}.example.org`
2. Use a full annotation 

If you go with #1, you just need to define the headless service, here is an example of the case #2:

```yaml
apiVersion: v1
kind: Service
metadata:
  name: ksvc
  annotations:
    external-dns.alpha.kubernetes.io/hostname:  example.org
spec:
  ports:
  - port: 9092
    name: external
  clusterIP: None
  selector:
    component: kafka
```
This will create 3 dns records:
```
kafka-0.example.org
kafka-1.example.org
kafka-2.example.org
```

If you set `--fqdn-template={{name}}.example.org` you can omit the annotation.
Generally it is a better approach to use  `--fqdn-template={{name}}.example.org`, because then
you would get the service name inside the generated A records:

```
kafka-0.ksvc.example.org
kafka-1.ksvc.example.org
kafka-2.ksvc.example.org
```

#### Using node external IPs as targets

Add the following annotation to your `Service`:

```yaml
external-dns.alpha.kubernetes.io/access: public
```

external-dns will now publish the node external IP of the node on which the pods backing your `Service` are running.

#### Using pod annotations to specify target IPs

Add the following annotation to the **pods** backing your `Service`:

```yaml
external-dns.alpha.kubernetes.io/target: "1.2.3.4"
```

external-dns will publish the IP specified in the annotation of each pod instead of using the podIP advertised by Kubernetes.

This can be useful e.g. if you are NATing public IPs onto your pod IPs and want to publish these in DNS.
