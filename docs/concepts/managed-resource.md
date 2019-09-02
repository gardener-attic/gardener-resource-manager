# Managed Resource

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
