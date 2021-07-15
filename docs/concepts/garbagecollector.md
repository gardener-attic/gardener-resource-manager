# Garbage Collector For Immutable `ConfigMap`s/`Secret`s

In Kubernetes, workload resources (e.g., `Pod`s) can mount `ConfigMap`s or `Secret`s or reference them via environment variables in containers.
Typically, when the content of such `ConfigMap`/`Secret` gets changed then the respective workload is usually not dynamically reloading the configuration, i.e., a restart is required.
The most commonly used approach is probably having so-called [checksum annotations in the pod template](https://helm.sh/docs/howto/charts_tips_and_tricks/#automatically-roll-deployments) which makes Kubernetes to recreate the pod if the checksum changes.
However, it has the downside that old, still running versions of the workload might not be able to properly work with the already updated content in the `ConfigMap`/`Secret`, potentially causing application outages.

In order to protect users from such outages (and to also improve the performance of the cluster), the Kubernetes community provides the ["immutable `ConfigMap`s/`Secret`s feature"](https://kubernetes.io/docs/concepts/configuration/configmap/#configmap-immutable).
Enabling immutability requires `ConfigMap`s/`Secret`s to have unique names.
Having unique names requires the client to delete `ConfigMap`s`/`Secret`s no longer in use.

In order to provide a similarly lightweight experience for clients (compared to the well-established checksum annotation approach), the Gardener Resource Manager features an optional garbage collector controller (disabled by default).
The purpose of this controller is cleaning up such immutable `ConfigMap`s/`Secret`s if they are no longer in use.

## How does the garbage collector work?

The following algorithm is implemented in the GC controller:

1. List all `ConfigMap`s and `Secret`s labeled with `resources.gardener.cloud/garbage-collectable-reference=true`.
1. List all `Deployment`s, `StatefulSet`s, `DaemonSet`s, `Job`s, `CronJob`s, `Pod`s and for each of them
   1. iterate over the `.metadata.annotations` and for each of them
      1. If the annotation key follows the `reference.resources.gardener.cloud/{configmap,secret}-<hash>` scheme then consider it as "in-use".
1. Delete all `ConfigMap`s and `Secret`s not considered as "in-use".

Consequently, clients need to

1. Create immutable `ConfigMap`s/`Secret`s with unique names (e.g., a checksum suffix based on the `.data`).
1. Label such `ConfigMap`s/`Secret`s with `resources.gardener.cloud/garbage-collectable-reference=true`.
1. Annotate their workload resources with `reference.resources.gardener.cloud/{configmap,secret}-<hash>=<name>` for all `ConfigMap`s/`Secret`s used by the containers of the respective `Pod`s.

   ⚠️ Add such annotations to `.metadata.annotations` as well as to all templates of other resources (e.g., `.spec.template.metadata.annotations` in `Deployment`s or `.spec.jobTemplate.metadata.annotations` and `.spec.jobTemplate.spec.template.metadata.annotations` for `CronJob`s.
   This ensures that the GC controller does not unintentionally consider `ConfigMap`s/`Secret`s as "not in use" just because there isn't a `Pod` referencing them anymore (e.g., they could still be used by a `Deployment` scaled down to `0`).

**Example:**

```yaml
---
apiVersion: v1
kind: ConfigMap
metadata:
  name: test-1234
  namespace: default
  labels:
    resources.gardener.cloud/garbage-collectable-reference: "true"
---
apiVersion: v1
kind: ConfigMap
metadata:
  name: test-5678
  namespace: default
  labels:
    resources.gardener.cloud/garbage-collectable-reference: "true"
---
apiVersion: v1
kind: Pod
metadata:
  name: example
  namespace: default
  annotations:
    reference.resources.gardener.cloud/configmap-82a3537f: test-5678
spec:
  containers:
  - name: nginx
    image: nginx:1.14.2
    terminationGracePeriodSeconds: 2
```

The GC controller would delete the `ConfigMap/test-1234` because it is considered as not "in-use".

ℹ️ If the GC controller is activated then the `ManagedResource` controller will no longer delete `ConfigMap`s/`Secret`s having the above label.

## How to activate the garbage collector?

The GC controller can be activated by providing the `--garbage-collector-sync-period` flag with a value larger than `0` (e.g., `1h`) to the Gardener Resource Manager.
