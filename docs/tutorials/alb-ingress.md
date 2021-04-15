# Using ExternalDNS with alb-ingress-controller

This tutorial describes how to use ExternalDNS with the [aws-alb-ingress-controller][1].

[1]: https://kubernetes-sigs.github.io/aws-load-balancer-controller

## Setting up ExternalDNS and aws-alb-ingress-controller

Follow the [AWS tutorial](aws.md) to setup ExternalDNS for use in Kubernetes clusters
running in AWS. Specify the `source=ingress` argument so that ExternalDNS will look
for hostnames in Ingress objects. In addition, you may wish to limit which Ingress
objects are used as an ExternalDNS source via the `ingress-class` argument, but
this is not required.

For help setting up the ALB Ingress Controller, follow the [Setup Guide][2].

[2]: https://kubernetes-sigs.github.io/aws-load-balancer-controller/latest/deploy/installation/

Note that the ALB ingress controller uses the same tags for [subnet auto-discovery][3]
as Kubernetes does with the AWS cloud provider.

[3]: https://kubernetes-sigs.github.io/aws-load-balancer-controller/latest/deploy/subnet_discovery/

In the examples that follow, it is assumed that you configured the ALB Ingress
Controller with the `ingress-class=alb` argument (not to be confused with the
same argument to ExternalDNS) so that the controller will only respect Ingress
objects with the `kubernetes.io/ingress.class` annotation set to "alb".

## Deploy an example application

Create the following sample "echoserver" application to demonstrate how
ExternalDNS works with ALB ingress objects.

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: echoserver
spec:
  replicas: 1
  selector:
    matchLabels:
      app: echoserver
  template:
    metadata:
      labels:
        app: echoserver
    spec:
      containers:
      - image: gcr.io/google_containers/echoserver:1.4
        imagePullPolicy: Always
        name: echoserver
        ports:
        - containerPort: 8080
---
apiVersion: v1
kind: Service
metadata:
  name: echoserver
spec:
  ports:
    - port: 80
      targetPort: 8080
      protocol: TCP
  type: NodePort
  selector:
    app: echoserver
```

Note that the Service object is of type `NodePort`. We don't need a Service of
type `LoadBalancer` here, since we will be using an Ingress to create an ALB.

## Ingress examples

Create the following Ingress to expose the echoserver application to the Internet.

```yaml
apiVersion: networking.k8s.io/v1
kind: Ingress
metadata:
  annotations:
    alb.ingress.kubernetes.io/scheme: internet-facing
    kubernetes.io/ingress.class: alb
  name: echoserver
spec:
  rules:
  - host: echoserver.mycluster.example.org
    http: &echoserver_root
      paths:
      - backend:
          serviceName: echoserver
          servicePort: 80
        path: /
  - host: echoserver.example.org
    http: *echoserver_root
```

The above should result in the creation of an (ipv4) ALB in AWS which will forward
traffic to the echoserver application.

If the `source=ingress` argument is specified, then ExternalDNS will create DNS
records based on the hosts specified in ingress objects. The above example would
result in two alias records being created, `echoserver.mycluster.example.org` and
`echoserver.example.org`, which both alias the ALB that is associated with the
Ingress object.

Note that the above example makes use of the YAML anchor feature to avoid having
to repeat the http section for multiple hosts that use the exact same paths. If
this Ingress object will only be fronting one backend Service, we might instead
create the following:

```yaml
apiVersion: networking.k8s.io/v1
kind: Ingress
metadata:
  annotations:
    alb.ingress.kubernetes.io/scheme: internet-facing
    external-dns.alpha.kubernetes.io/hostname: echoserver.mycluster.example.org, echoserver.example.org
    kubernetes.io/ingress.class: alb
  name: echoserver
spec:
  rules:
  - http:
      paths:
      - backend:
          serviceName: echoserver
          servicePort: 80
        path: /
```

In the above example we create a default path that works for any hostname, and
make use of the `external-dns.alpha.kubernetes.io/hostname` annotation to create
multiple aliases for the resulting ALB.

## Dualstack ALBs

AWS [supports][4] both IPv4 and "dualstack" (both IPv4 and IPv6) interfaces for ALBs.
The ALB ingress controller uses the `alb.ingress.kubernetes.io/ip-address-type`
annotation (which defaults to `ipv4`) to determine this. If this annotation is
set to `dualstack` then ExternalDNS will create two alias records (one A record
and one AAAA record) for each hostname associated with the Ingress object.

[4]: https://docs.aws.amazon.com/elasticloadbalancing/latest/application/application-load-balancers.html#ip-address-type

Example:

```yaml
apiVersion: networking.k8s.io/v1
kind: Ingress
metadata:
  annotations:
    alb.ingress.kubernetes.io/scheme: internet-facing
    alb.ingress.kubernetes.io/ip-address-type: dualstack
    kubernetes.io/ingress.class: alb
  name: echoserver
spec:
  rules:
  - host: echoserver.example.org
    http:
      paths:
      - backend:
          serviceName: echoserver
          servicePort: 80
        path: /
```

The above Ingress object will result in the creation of an ALB with a dualstack
interface. ExternalDNS will create both an A `echoserver.example.org` record and
an AAAA record of the same name, that each are aliases for the same ALB.

## Dualstack NLBs

[AWS Load Balancer Controller][5] satisifies service of "LoadBalancer"
type with annotation `service.beta.kubernetes.io/aws-load-balancer-type` value
`nlb-ip`.  This service supports dualstack mode via
`service.beta.kubernetes.io/aws-load-balancer-ip-address-type` [annotation][6]
(which defaults to `ipv4`). If this annotation is set to `dualstack` then
ExternalDNS will create two alias records (one A record and one AAAA record)
for each hostname associated with the Ingress object.

[5]: https://github.com/kubernetes-sigs/aws-load-balancer-controller#aws-load-balancer-controller
[6]: https://github.com/kubernetes-sigs/aws-load-balancer-controller/blob/main/docs/guide/service/annotations.md

Example:

```yaml
---
apiVersion: v1
kind: Service
metadata:
  name: my-svc
  labels:
    app.kubernetes.io/name: my-app
  annotations:
    external-dns.alpha.kubernetes.io/hostname: foo.example.com
    service.beta.kubernetes.io/aws-load-balancer-type: "nlb-ip"
    service.beta.kubernetes.io/aws-load-balancer-backend-protocol: "tcp"
    service.beta.kubernetes.io/aws-load-balancer-ip-address-type: "dualstack"
spec:
  type: LoadBalancer
  ports:
  - port: 80
    targetPort: 80
    protocol: TCP
    name: http
  selector:
    app: my-app
```

The above Service object will result in the creation of an NLB with a dualstack
interface. ExternalDNS will create both an A `echoserver.example.org` record and
an AAAA record of the same name, that each are aliases for the same NLB.
