# Managed Resource

### Resource Class

By default gardener-resource-manager controller watches for ManagedResources in all namespaces. `--namespace` flag can be specified to gardener-resource-manager binary to restrict the watch to ManagedResources in a single namespace.
A ManagedResource has an optional `.spec.class` field that allows to indicate that it belongs to given class of resources. `--resource-class` flag can be specified to gardener-resource-manager binary to restrict the watch to ManagedResources with the given `.spec.class`. A default class is assumed if no class is specified.

### Conditions

A ManagedResource has a ManagedResourceStatus, which has an array of ManagedResourceConditions. ManagedResourceConditions currently include:

| Condition          | Description                                               |
| ------------------ | --------------------------------------------------------- |
| `ResourcesApplied` | `True` if all resources are applied to the target cluster |
| `ResourcesHealthy` | `True` if all resources are present and healthy           |

`ResourcesApplied` may be `False` when:
  - the resource `apiVersion` is not known to the target cluster
  - the resource spec is invalid (for example the label value does not match the required regex for it)
  - ...

`ResourcesHealthy` may be `False` when:
  - the resource is not found
  - the resource is a Deployment and the Deployment does not have the minimum availability.
  - ...

Each Kubernetes resources has different notion for being healthy. For example, a Deployment is considered healthy if the controller observed its current revision and if the number of updated replicas is equal to the number of replicas.

The following section describes a healthy ManagedResource:

```json
"conditions": [
  {
    "type": "ResourcesApplied",
    "status": "True",
    "reason": "ApplySucceeded",
    "message": "All resources are applied.",
    "lastUpdateTime": "2019-09-09T11:31:21Z",
    "lastTransitionTime": "2019-09-08T19:53:23Z"
  },
  {
    "type": "ResourcesHealthy",
    "status": "True",
    "reason": "ResourcesHealthy",
    "message": "All resources are healthy.",
    "lastUpdateTime": "2019-09-09T11:31:21Z",
    "lastTransitionTime": "2019-09-09T11:31:21Z"
  }
]  
```

## Ignoring Updates 

In some cases it is not desirable to update or re-apply some of the cluster components (for example, if customization is required or needs to be applied by the end-user). 
For these resources, the annotation "resources.gardener.cloud/ignore" needs to be set to "true" or a truthy value (Truthy values are "1", "t", "T", "true", "TRUE", "True") in the corresponding managed resource secrets, 
this can be done from the components that create the managed resource secrets, for example Gardener extensions or Gardener. Once this is done, the resource will be initially created and later ignored during reconciliation.

## Origin

All the objects managed by the resource manager get a dedicated annotation 
`resources.gardener.cloud/origin` describing the `ManagedResource` object that describes 
 this object. 
 
 By default this is in this format &lt;namespace&gt;/&lt;objectname&gt;.
 In multi-cluster scenarios (the `ManagedResource` objects are maintained in a 
 cluster different from the one the described objects are managed), it might
 be useful to include the cluster identity, as well.
 
 This can be enforced by setting the `--cluster-id` option. Here, several
 possibilities are supported:
 - given a direct value: use this as id for the source cluster
 - `<cluster>`: read the cluster identity from a `cluster-identity` config map
  in the `kube-system` namespace (attribute `cluster-identity`). This is 
  automatically maintained in all clusters managed or involved in a gardener landscape.
 - `<default>`: try to read the cluster identity from the config map. If not found,
  no identity is used
 - empty string: no cluster identity is used (completely cluster local scenarios)
 
 The format of the origin annotation with a cluster id is &lt;cluster id&gt;:&lt;namespace&gt;/&lt;objectname&gt;.
 
 The default for the cluster id is the empty value (do not use cluster id).