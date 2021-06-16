# Transferring ownership between resources

When using a [registry](../proposal/registry.md) (aside from noop), it is possible to transfer ownership from
one Kubernets resource to another, even between different instances of external-dns. For this purpose the following
annotations are available:

```
external-dns.alpha.kubernetes.io/permit-claim-by-owner: "<owner id>"
external-dns.alpha.kubernetes.io/permit-claim-by-resource: "<source>/<namespace>/<resource name>"
external-dns.alpha.kubernetes.io/claim: "<true|false>"
```

The "owner id" is a value as set by the `--txt-owner-id` parameter. "source" is an identifier used by the sources activated
with  the `--source` parameter. Check the documentation of the different source (or the source if necessary) for the different
ids used by the providers.

If a resource wants to permit another resource to claim the record it holds ownership of, it has to set
the `external-dns.alpha.kubernetes.io/permit-claim-by-resource` and optionally the `external-dns.alpha.kubernetes.io/permit-claim-by-owner` annotations. If the `permit-claim-by-owner` annotation is not set, it is assumed to have the same
value as the current owner of the resource (e.g. given a resource with the owner id of "x", setting the `permit-claim-by-resource` and leaving the `permit-claim-by-owner` annotation away would result in `permit-claim-by-owner` being set to "x").

Setting only the 

If a resource wants to claim ownership of a record for which it has permission to do so (e.g. its `<source>/<namespace>/<resource name>` matches and it is managed by an external-dns instance configured with the proper owner id), it has to set the `external-dns.alpha.kubernetes.io/claim` annotation to "true".

Note: the `<source>/<namespace>/<resource name>` value of a claiming resource cannot be set manually, it actually has to be a
resource with the proper name in the proper namespace, managed by the appropriate [source module](../contributing/sources-and-providers.md) of external-dns.

## Example

The resource that wants to transfer its ownership:

```yaml
apiVersion: externaldns.k8s.io/v1alpha1
kind: DNSEndpoint
metadata:
  name: ns-record-a
  annotations:
    external-dns.alpha.kubernetes.io/permit-claim-by-resource: "crd/external-dns/ns-record-a"
    external-dns.alpha.kubernetes.io/permit-claim-by-owner: "someowner"
spec:
  endpoints:
  - dnsName: zone.example.com
    recordTTL: 300
    recordType: NS
    targets:
    - ns1.example.com
    - ns2.example.com
```

The example assumes the resource is managed by an external-dns instance with `--txt-owner-id` != "someowner" and `--source=crd`.

The resource that wants to claim ownership:

```yaml
apiVersion: externaldns.k8s.io/v1alpha1
kind: DNSEndpoint
metadata:
  name: ns-record-b
  namespace: external-dns
  annotations:
    external-dns.alpha.kubernetes.io/claim: "true"
spec:
  endpoints:
  - dnsName: zone.example.com
    recordTTL: 300
    recordType: NS
    targets:
    - ns1.example.com
    - ns2.example.com
```

The example assumes the resource is managed by an external-dns instance with `--txt-owner-id=someowner` and `--source=crd`.

## Resource priority

To allow for ownership transfers between Kubernetes resources managed by the same external-dns instance, 
Kubernetes resources with `external-dns.alpha.kubernetes.io/claim: "true"` have a higher priority than resources
without that label, if the dns record has proper claim permissions set.

Given the above resources, if both would be managed by the same external-dns instance (e.g. both have the same owner,
but the owner id not "someowner"), updates to the "ns-record-a" resource would be ignored by external-dns because
the "ns-record-b" resource has a higher priority and matches the `external-dns.alpha.kubernetes.io/permit-claim-by-resource`
annotation of "ns-record-a".