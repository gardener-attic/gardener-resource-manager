// Copyright (c) 2019 SAP SE or an SAP affiliate company. All rights reserved. This file is licensed under the Apache Software License, v. 2 except as noted otherwise in the LICENSE file
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package managedresources

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"sync"
	"time"

	resourcesv1alpha1 "github.com/gardener/gardener-resource-manager/pkg/apis/resources/v1alpha1"
	resourcesv1alpha1helper "github.com/gardener/gardener-resource-manager/pkg/apis/resources/v1alpha1/helper"
	"github.com/gardener/gardener-resource-manager/pkg/controller/utils"

	hvpav1alpha1 "github.com/gardener/hvpa-controller/api/v1alpha1"
	"github.com/go-logr/logr"
	"github.com/hashicorp/go-multierror"
	appsv1 "k8s.io/api/apps/v1"
	autoscalingv1 "k8s.io/api/autoscaling/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	extensionsv1beta1 "k8s.io/api/extensions/v1beta1"
	apiequality "k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/apimachinery/pkg/util/yaml"
	"k8s.io/client-go/restmapper"
	"k8s.io/client-go/util/retry"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

const (
	// FinalizerName is the finalizer base name that is injected into ManagedResources.
	// The concrete finalizer is finally containing this base name and the resource class.
	FinalizerName = "resources.gardener.cloud/gardener-resource-manager"
)

const (
	// Cluster Identity Handling
	// The Cluster identity is used to remember the origin of an object maintained
	// on behalf of a ManagedResource object.
	// There are several modes the cluster identity is determined. First of all
	// it can explicitly be specified on the command line. Here it is possible
	// to select determination methods by choosing one of the following constants.
	// If nothing is specified the empty identity will be used for cluster local
	// usage scenarios of the resource manager

	// IdentityByCluster extracts the cluster identity from the cluster-id
	// config map in the kube-system namespace. This is useful for cross-cluster
	// usage scenarios of the resource manager. This mode enforces the usage
	// of the standard cluster identity config map and fails if the identity
	// cannot be determined this way
	IdentityByCluster = "<cluster>"

	// IdentityByDefault tries to determine a unique cluster identity using
	// the cluster-identity config map. It this fails the default non-unique
	// (empty) cluster identity will be used.
	IdentifyByDefault = "<default>"
)

var (
	deletePropagationForeground = metav1.DeletePropagationForeground
	foregroundDeletionAPIGroups = sets.NewString(appsv1.GroupName, extensionsv1beta1.GroupName, batchv1.GroupName)
)

// Reconciler contains information in order to reconcile instances of ManagedResource.
type Reconciler struct {
	ctx context.Context
	log logr.Logger

	client           client.Client
	targetClient     client.Client
	targetRESTMapper *restmapper.DeferredDiscoveryRESTMapper
	targetScheme     *runtime.Scheme

	class        *ClassFilter
	alwaysUpdate bool
	syncPeriod   time.Duration
	clusterID    string
}

// NewReconciler creates a new reconciler with the given target client.
func NewReconciler(ctx context.Context, log logr.Logger, c, targetClient client.Client, targetRESTMapper *restmapper.DeferredDiscoveryRESTMapper, targetScheme *runtime.Scheme, class *ClassFilter, alwaysUpdate bool, syncPeriod time.Duration, clusterID string) *Reconciler {
	return &Reconciler{ctx, log, c, targetClient, targetRESTMapper, targetScheme, class, alwaysUpdate, syncPeriod, clusterID}
}

// Reconcile implements `reconcile.Reconciler`.
func (r *Reconciler) Reconcile(req ctrl.Request) (ctrl.Result, error) {
	log := r.log.WithValues("object", req)

	mr := &resourcesv1alpha1.ManagedResource{}
	if err := r.client.Get(r.ctx, req.NamespacedName, mr); err != nil {
		if apierrors.IsNotFound(err) {
			log.Info("Stopping reconciliation of ManagedResource, as it has been deleted")
			return reconcile.Result{}, nil
		}
		return reconcile.Result{}, fmt.Errorf("could not fetch ManagedResource: %+v", err)
	}

	action, responsible := r.class.Active(mr)
	log.Info(fmt.Sprintf("reconcile: action required: %t, responsible: %t", action, responsible))

	// If the object should be deleted or the responsibility changed
	// the actual deployments have to be deleted
	if mr.DeletionTimestamp != nil || (action && !responsible) {
		return r.delete(mr, log)
	}

	// If the deletion after a change of responsibility is still
	// pending, the handling of the object by the responsible controller
	// must be delayed, until the deletion is finished.
	if responsible && !action {
		return ctrl.Result{Requeue: true}, nil
	}
	return r.reconcile(mr, log)
}

func (r *Reconciler) determineClusterIdentity(c client.Client, force bool) (string, error) {
	cm := corev1.ConfigMap{}
	err := c.Get(r.ctx, client.ObjectKey{Name: "cluster-identity", Namespace: "kube-system"}, &cm)
	if err == nil {
		if id, ok := cm.Data["cluster-identity"]; ok {
			return id, nil
		}
		if force {
			return "", fmt.Errorf("cannot determine cluster identity from configmap: no cluster-identity entry ")
		}
	} else {
		if force {
			return "", fmt.Errorf("cannot determine cluster identity from configmap: %s", err)
		}
	}
	return "", nil
}

func (r *Reconciler) origin(mr *resourcesv1alpha1.ManagedResource) string {
	var err error
	if r.clusterID == IdentityByCluster || r.clusterID == IdentifyByDefault {
		r.clusterID, err = r.determineClusterIdentity(r.client, r.clusterID == IdentityByCluster)
		if err != nil {
			panic(err)
		}
	}
	if r.clusterID != "" {
		return r.clusterID + ":" + mr.Namespace + "/" + mr.Name
	}
	return mr.Namespace + "/" + mr.Name
}

// MatchOrigin matches a found origin spec of an object against the actual one.
func MatchOrigin(origin, found string) bool {
	if found == origin {
		return true
	}
	parts := strings.Split(origin, ":")
	if len(parts) > 1 {
		// handle format migration for adding cluster-id later
		fparts := strings.Split(found, ":")
		return len(fparts) == 1 && parts[1] == found
	}
	return false
}

func (r *Reconciler) reconcile(mr *resourcesv1alpha1.ManagedResource, log logr.Logger) (ctrl.Result, error) {
	log.Info("Starting to reconcile ManagedResource")

	if err := utils.EnsureFinalizer(r.ctx, r.client, r.class.FinalizerName(), mr); err != nil {
		return reconcile.Result{}, err
	}

	var (
		newResourcesObjects          []object
		newResourcesObjectReferences []resourcesv1alpha1.ObjectReference

		equivalences           = NewEquivalences(mr.Spec.Equivalences...)
		existingResourcesIndex = NewObjectIndex(mr.Status.Resources, equivalences)

		forceOverwriteLabels      bool
		forceOverwriteAnnotations bool

		decodingErrors []*decodingError
	)

	origin := r.origin(mr)

	if v := mr.Spec.ForceOverwriteLabels; v != nil {
		forceOverwriteLabels = *v
	}
	if v := mr.Spec.ForceOverwriteAnnotations; v != nil {
		forceOverwriteAnnotations = *v
	}

	// Initialize condition based on the current status.
	conditionResourcesApplied := resourcesv1alpha1helper.GetOrInitCondition(mr.Status.Conditions, resourcesv1alpha1.ResourcesApplied)

	for _, ref := range mr.Spec.SecretRefs {
		secret := &corev1.Secret{}
		if err := r.client.Get(r.ctx, client.ObjectKey{Namespace: mr.Namespace, Name: ref.Name}, secret); err != nil {
			conditionResourcesApplied = resourcesv1alpha1helper.UpdatedCondition(conditionResourcesApplied, resourcesv1alpha1.ConditionFalse, "CannotReadSecret", err.Error())
			if err := tryUpdateManagedResourceConditions(r.ctx, r.client, mr, conditionResourcesApplied); err != nil {
				return ctrl.Result{}, fmt.Errorf("could not update the ManagedResource status: %+v ", err)
			}

			return reconcile.Result{}, fmt.Errorf("could not read secret '%s': %+v", secret.Name, err)
		}

		for key, value := range secret.Data {
			var (
				decoder    = yaml.NewYAMLOrJSONDecoder(bytes.NewReader(value), 1024)
				decodedObj map[string]interface{}
			)

			for i := 0; true; i++ {
				err := decoder.Decode(&decodedObj)
				if err == io.EOF {
					break
				}
				if err != nil {
					decodingError := &decodingError{
						err:               err,
						secret:            fmt.Sprintf("%s/%s", secret.Namespace, secret.Name),
						secretKey:         key,
						objectIndexInFile: i,
					}
					decodingErrors = append(decodingErrors, decodingError)
					log.Error(decodingError.err, decodingError.StringShort())
					continue
				}

				if decodedObj == nil {
					continue
				}

				obj := &unstructured.Unstructured{Object: decodedObj}

				// look up scope of objects' kind to check, if we should default the namespace field
				mapping, err := r.targetRESTMapper.RESTMapping(obj.GroupVersionKind().GroupKind(), obj.GroupVersionKind().Version)
				if err != nil || mapping == nil {
					// Don't reset RESTMapper in case of cache misses. Most probably indicates, that the corresponding CRD is not yet applied.
					// CRD might be applied later as part of the ManagedResource reconciliation
					log.Info(fmt.Sprintf("could not get rest mapping for %s '%s/%s': %v", obj.GetKind(), obj.GetNamespace(), obj.GetName(), err),
						"secret", fmt.Sprintf("%s/%s", secret.Namespace, secret.Name), "secretKey", key, "objectIndexInFile", i)

					// default namespace on a best effort basis
					if obj.GetKind() != "Namespace" && obj.GetNamespace() == "" {
						obj.SetNamespace(metav1.NamespaceDefault)
					}
				} else {
					if mapping.Scope.Name() == meta.RESTScopeNameNamespace {
						// default namespace field to `default` in case of namespaced kinds
						if obj.GetNamespace() == "" {
							obj.SetNamespace(metav1.NamespaceDefault)
						}
					} else {
						// unset namespace field in case of non-namespaced kinds
						obj.SetNamespace("")
					}
				}

				var (
					newObj = object{
						obj:                       obj,
						forceOverwriteLabels:      forceOverwriteLabels,
						forceOverwriteAnnotations: forceOverwriteAnnotations,
					}
					objectReference = resourcesv1alpha1.ObjectReference{
						ObjectReference: corev1.ObjectReference{
							APIVersion: newObj.obj.GetAPIVersion(),
							Kind:       newObj.obj.GetKind(),
							Name:       newObj.obj.GetName(),
							Namespace:  newObj.obj.GetNamespace(),
						},
						Labels:      mergeMaps(newObj.obj.GetLabels(), mr.Spec.InjectLabels),
						Annotations: newObj.obj.GetAnnotations(),
					}
				)

				newObj.oldInformation, _ = existingResourcesIndex.Lookup(objectReference)
				decodedObj = nil

				newResourcesObjects = append(newResourcesObjects, newObj)
				newResourcesObjectReferences = append(newResourcesObjectReferences, objectReference)
			}
		}
	}

	// sort object references before updating status, to keep consistent ordering
	// (otherwise, the order will be different on each update)
	sortObjectReferences(newResourcesObjectReferences)

	// invalidate conditions, if resources have been added/removed from the managed resource
	if len(mr.Status.Resources) == 0 || !apiequality.Semantic.DeepEqual(mr.Status.Resources, newResourcesObjectReferences) {
		conditionResourcesHealthy := resourcesv1alpha1helper.GetOrInitCondition(mr.Status.Conditions, resourcesv1alpha1.ResourcesHealthy)
		conditionResourcesHealthy = resourcesv1alpha1helper.UpdatedCondition(conditionResourcesHealthy, resourcesv1alpha1.ConditionUnknown,
			resourcesv1alpha1.ConditionHealthChecksPending, "The health checks have not yet been executed for the current set of resources.")

		reason := resourcesv1alpha1.ConditionApplyProgressing
		msg := "The resources are currently being reconciled."
		switch conditionResourcesApplied.Reason {
		case resourcesv1alpha1.ConditionApplyFailed, resourcesv1alpha1.ConditionDeletionFailed, resourcesv1alpha1.ConditionDeletionPending:
			// keep condition reason and message if last reconciliation failed
			reason = conditionResourcesApplied.Reason
			msg = conditionResourcesApplied.Message
		}
		conditionResourcesApplied = resourcesv1alpha1helper.UpdatedCondition(conditionResourcesApplied, resourcesv1alpha1.ConditionProgressing, reason, msg)

		if err := tryUpdateManagedResourceConditions(r.ctx, r.client, mr, conditionResourcesHealthy, conditionResourcesApplied); err != nil {
			return ctrl.Result{}, fmt.Errorf("could not update the ManagedResource status: %+v", err)
		}
	}

	if deletionPending, err := r.cleanOldResources(existingResourcesIndex, mr); err != nil {
		var (
			reason string
			status resourcesv1alpha1.ConditionStatus
		)
		if deletionPending {
			reason = resourcesv1alpha1.ConditionDeletionPending
			status = resourcesv1alpha1.ConditionProgressing
			log.Info("Deletion is still pending", "err", err)
		} else {
			reason = resourcesv1alpha1.ConditionDeletionFailed
			status = resourcesv1alpha1.ConditionFalse
			log.Error(err, "Deletion of old resources failed")
		}

		conditionResourcesApplied = resourcesv1alpha1helper.UpdatedCondition(conditionResourcesApplied, status, reason, err.Error())
		if err := tryUpdateManagedResourceConditions(r.ctx, r.client, mr, conditionResourcesApplied); err != nil {
			return ctrl.Result{}, fmt.Errorf("could not update the ManagedResource status: %+v", err)
		}

		if deletionPending {
			return ctrl.Result{RequeueAfter: 5 * time.Second}, nil
		} else {
			return ctrl.Result{}, err
		}
	}

	if err := r.applyNewResources(origin, newResourcesObjects, mr.Spec.InjectLabels, equivalences); err != nil {
		conditionResourcesApplied = resourcesv1alpha1helper.UpdatedCondition(conditionResourcesApplied, resourcesv1alpha1.ConditionFalse, resourcesv1alpha1.ConditionApplyFailed, err.Error())
		if err := tryUpdateManagedResourceConditions(r.ctx, r.client, mr, conditionResourcesApplied); err != nil {
			return ctrl.Result{}, fmt.Errorf("could not update the ManagedResource status: %+v", err)
		}

		return ctrl.Result{}, fmt.Errorf("could not apply all new resources: %+v", err)
	}

	if len(decodingErrors) != 0 {
		conditionResourcesApplied = resourcesv1alpha1helper.UpdatedCondition(conditionResourcesApplied, resourcesv1alpha1.ConditionFalse, resourcesv1alpha1.ConditionDecodingFailed, fmt.Sprintf("Could not decode all new resources: %v", decodingErrors))
	} else {
		conditionResourcesApplied = resourcesv1alpha1helper.UpdatedCondition(conditionResourcesApplied, resourcesv1alpha1.ConditionTrue, resourcesv1alpha1.ConditionApplySucceeded, "All resources are applied.")
	}

	if err := tryUpdateManagedResourceStatus(r.ctx, r.client, mr, newResourcesObjectReferences, conditionResourcesApplied); err != nil {
		return ctrl.Result{}, fmt.Errorf("could not update the ManagedResource status: %+v", err)
	}

	log.Info("Finished to reconcile ManagedResource")
	return ctrl.Result{RequeueAfter: r.syncPeriod}, nil
}

func (r *Reconciler) delete(mr *resourcesv1alpha1.ManagedResource, log logr.Logger) (ctrl.Result, error) {
	log.Info("Starting to delete ManagedResource")

	conditionResourcesApplied := resourcesv1alpha1helper.GetOrInitCondition(mr.Status.Conditions, resourcesv1alpha1.ResourcesApplied)

	if keepObjects := mr.Spec.KeepObjects; keepObjects == nil || !*keepObjects {
		existingResourcesIndex := NewObjectIndex(mr.Status.Resources, nil)

		msg := "The resources are currently being deleted."
		switch conditionResourcesApplied.Reason {
		case resourcesv1alpha1.ConditionDeletionPending, resourcesv1alpha1.ConditionDeletionFailed:
			// keep condition message if deletion is pending / failed
			msg = conditionResourcesApplied.Message
		}
		conditionResourcesApplied = resourcesv1alpha1helper.UpdatedCondition(conditionResourcesApplied, resourcesv1alpha1.ConditionProgressing, resourcesv1alpha1.ConditionDeletionPending, msg)
		if err := tryUpdateManagedResourceConditions(r.ctx, r.client, mr, conditionResourcesApplied); err != nil {
			return ctrl.Result{}, fmt.Errorf("could not update the ManagedResource status: %+v", err)
		}

		if deletionPending, err := r.cleanOldResources(existingResourcesIndex, mr); err != nil {
			var (
				reason string
				status resourcesv1alpha1.ConditionStatus
			)
			if deletionPending {
				reason = resourcesv1alpha1.ConditionDeletionPending
				status = resourcesv1alpha1.ConditionProgressing
				log.Info("Deletion is still pending", "err", err)
			} else {
				reason = resourcesv1alpha1.ConditionDeletionFailed
				status = resourcesv1alpha1.ConditionFalse
				log.Error(err, "Deletion of all resources failed")
			}

			conditionResourcesApplied = resourcesv1alpha1helper.UpdatedCondition(conditionResourcesApplied, status, reason, err.Error())
			if err := tryUpdateManagedResourceConditions(r.ctx, r.client, mr, conditionResourcesApplied); err != nil {
				return ctrl.Result{}, fmt.Errorf("could not update the ManagedResource status: %+v", err)
			}

			if deletionPending {
				return ctrl.Result{RequeueAfter: 5 * time.Second}, nil
			} else {
				return ctrl.Result{}, err
			}
		}
	} else {
		log.Info(fmt.Sprintf("Do not delete any resources of %s because .spec.keepObjects=true", mr.Name))
	}

	log.Info("All resources have been deleted, removing finalizers from ManagedResource")

	if err := utils.DeleteFinalizer(r.ctx, r.client, r.class.FinalizerName(), mr); err != nil {
		return reconcile.Result{}, fmt.Errorf("error removing finalizer from ManagedResource: %+v", err)
	}

	log.Info("Finished to delete ManagedResource")
	return ctrl.Result{}, nil
}

func (r *Reconciler) applyNewResources(origin string, newResourcesObjects []object, labelsToInject map[string]string, equivalences Equivalences) error {
	var (
		results   = make(chan error)
		wg        sync.WaitGroup
		errorList = &multierror.Error{
			ErrorFormat: utils.NewErrorFormatFuncWithPrefix("Could not apply all new resources"),
		}
		encounteredNoMatchError = false
	)

	// get all HPA and HVPA targetRefs to check if we should prevent overwriting replicas and/or resource requirements.
	// VPAs don't have to be checked, as they don't update the spec directly and only mutate Pods via a MutatingWebhook
	// and therefore don't interfere with the resource manager.
	horizontallyScaledObjects, verticallyScaledObjects, err := computeAllScaledObjectKeys(r.ctx, r.targetClient)
	if err != nil {
		return fmt.Errorf("failed to compute all HPA and HVPA target ref object keys: %w", err)
	}

	for _, o := range newResourcesObjects {
		wg.Add(1)

		go func(obj object) {
			defer wg.Done()

			var (
				current            = obj.obj.DeepCopy()
				resource           = unstructuredToString(obj.obj)
				scaledHorizontally = isScaled(obj.obj, horizontallyScaledObjects, equivalences)
				scaledVertically   = isScaled(obj.obj, verticallyScaledObjects, equivalences)
			)

			r.log.Info("Applying", "resource", resource)

			results <- retry.RetryOnConflict(retry.DefaultBackoff, func() error {
				if operationResult, err := utils.TypedCreateOrUpdate(r.ctx, r.targetClient, r.targetScheme, current, r.alwaysUpdate, func() error {
					metadata, err := meta.Accessor(obj.obj)
					if err != nil {
						return fmt.Errorf("error getting metadata of object %q: %s", resource, err)
					}
					curmeta, err := meta.Accessor(current)
					if err != nil {
						return fmt.Errorf("error getting metadata of existing object %q: %s", resource, err)
					}

					// if the actual object shoud be ignored, do nothing (ignore the resource)
					if ignore(origin, metadata, curmeta) {
						annotations := current.GetAnnotations()
						delete(annotations, descriptionAnnotation)
						delete(annotations, originAnnotation)
						current.SetAnnotations(annotations)
						return nil
					}

					if err := injectLabels(obj.obj, labelsToInject); err != nil {
						return fmt.Errorf("error injecting labels into object %q: %s", resource, err)
					}

					return merge(origin, obj.obj, current, obj.forceOverwriteLabels, obj.oldInformation.Labels, obj.forceOverwriteAnnotations, obj.oldInformation.Annotations, scaledHorizontally, scaledVertically)
				}); err != nil {
					if meta.IsNoMatchError(err) {
						encounteredNoMatchError = true
					}

					if apierrors.IsConflict(err) {
						r.log.Info(fmt.Sprintf("conflict during apply of object %q: %s", resource, err))
						// return conflict error directly, so that the update will be retried
						return err
					}

					if apierrors.IsInvalid(err) && operationResult == controllerutil.OperationResultUpdated && deleteOnInvalidUpdate(current) {
						if deleteErr := r.targetClient.Delete(r.ctx, current); client.IgnoreNotFound(deleteErr) != nil {
							return fmt.Errorf("error deleting object %q after 'invalid' update error: %s", resource, deleteErr)
						}
						// return error directly, so that the create after delete will be retried
						return fmt.Errorf("deleted object %q because of 'invalid' update error and 'delete-on-invalid-update' annotation on object (%s)", resource, err)
					}

					return fmt.Errorf("error during apply of object %q: %s", resource, err)
				}
				return nil
			})
		}(o)
	}

	go func() {
		wg.Wait()
		close(results)
	}()

	for err := range results {
		if err != nil {
			errorList = multierror.Append(errorList, err)
		}
	}

	if encounteredNoMatchError {
		// Reset RESTMapper in case of cache misses (e.g. CRD not found),
		// but only reset once per reconcile, MR may contain multiple CRs of CRDs, that don't exist yet
		// and we don't want to issue too many discovery requests, that would anyways probably still not contain the CRD
		r.targetRESTMapper.Reset()
	}

	return errorList.ErrorOrNil()
}

// computeAllScaledObjectKeys returns two sets containing object keys (in the form `Group/Kind/Namespace/Name`).
// The first one contains keys to objects that are horizontally scaled by either an HPA or HVPA. And the
// second one contains keys to objects that are vertically scaled by an HVPA.
// VPAs are not checked, as they don't update the spec of Deployments/StatefulSets/... and only mutate resource
// requirements via a MutatingWebhook. This way VPAs don't interfere with the resource manager and must not be considered.
func computeAllScaledObjectKeys(ctx context.Context, c client.Client) (horizontallyScaledObjects, verticallyScaledObjects sets.String, err error) {
	horizontallyScaledObjects = sets.NewString()
	verticallyScaledObjects = sets.NewString()

	// get all HPAs' targets
	hpaList := &autoscalingv1.HorizontalPodAutoscalerList{}
	if err := c.List(ctx, hpaList); err != nil && !meta.IsNoMatchError(err) {
		return horizontallyScaledObjects, verticallyScaledObjects, fmt.Errorf("failed to list all HPAs: %w", err)
	}

	for _, hpa := range hpaList.Items {
		if key, err := targetObjectKeyFromHPA(hpa); err != nil {
			return horizontallyScaledObjects, verticallyScaledObjects, err
		} else {
			horizontallyScaledObjects.Insert(key)
		}
	}

	// get all HVPAs' targets
	hvpaList := &hvpav1alpha1.HvpaList{}
	if err := c.List(ctx, hvpaList); err != nil && !meta.IsNoMatchError(err) {
		return horizontallyScaledObjects, verticallyScaledObjects, fmt.Errorf("failed to list all HVPAs: %w", err)
	}

	for _, hvpa := range hvpaList.Items {
		if key, err := targetObjectKeyFromHVPA(hvpa); err != nil {
			return horizontallyScaledObjects, verticallyScaledObjects, err
		} else {
			if hvpa.Spec.Hpa.Deploy {
				horizontallyScaledObjects.Insert(key)
			}
			if hvpa.Spec.Vpa.Deploy {
				verticallyScaledObjects.Insert(key)
			}
		}
	}

	return horizontallyScaledObjects, verticallyScaledObjects, nil
}

func targetObjectKeyFromHPA(hpa autoscalingv1.HorizontalPodAutoscaler) (string, error) {
	targetGV, err := schema.ParseGroupVersion(hpa.Spec.ScaleTargetRef.APIVersion)
	if err != nil {
		return "", fmt.Errorf("invalid API version in scaleTargetReference of HorizontalPodAutoscaler '%s/%s': %w", hpa.Namespace, hpa.Name, err)
	}

	return objectKey(targetGV.Group, hpa.Spec.ScaleTargetRef.Kind, hpa.Namespace, hpa.Spec.ScaleTargetRef.Name), nil
}

func targetObjectKeyFromHVPA(hvpa hvpav1alpha1.Hvpa) (string, error) {
	targetGV, err := schema.ParseGroupVersion(hvpa.Spec.TargetRef.APIVersion)
	if err != nil {
		return "", fmt.Errorf("invalid API version in scaleTargetReference of HorizontalPodAutoscaler '%s/%s': %w", hvpa.Namespace, hvpa.Name, err)
	}

	return objectKey(targetGV.Group, hvpa.Spec.TargetRef.Kind, hvpa.Namespace, hvpa.Spec.TargetRef.Name), nil
}

func isScaled(obj *unstructured.Unstructured, scaledObjectKeys sets.String, equivalences Equivalences) bool {
	key := objectKeyFromUnstructured(obj)

	if scaledObjectKeys.Has(key) {
		return true
	}

	// check if a HPA/HVPA targets this object via an equivalent API Group
	gk := metav1.GroupKind{
		Group: obj.GroupVersionKind().Group,
		Kind:  obj.GetKind(),
	}
	for equivalentGroupKind := range equivalences.GetEquivalencesFor(gk) {
		if scaledObjectKeys.Has(objectKey(equivalentGroupKind.Group, equivalentGroupKind.Kind, obj.GetNamespace(), obj.GetName())) {
			return true
		}
	}

	return false
}

func objectKeyFromUnstructured(o *unstructured.Unstructured) string {
	return objectKey(o.GroupVersionKind().Group, o.GetKind(), o.GetNamespace(), o.GetName())
}

type AnnotationProvider interface {
	GetAnnotations() map[string]string
}

func ignoreFallback(origin string, meta AnnotationProvider, curmeta metav1.Object) bool {
	if curmeta != nil && curmeta.GetResourceVersion() != "" {
		// object already exists
		// check for marker annotations, if they are not present or they indicate another origin,
		// the object has been modified by another maintainer.
		// If the fallback option is given, this is accepted and the
		// object is ignored further on, otherwise the object will be reconciled.
		if annotationExistsAndValueTrue(meta, resourcesv1alpha1.Fallback) {
			if !annotationExistsAndMatchValue(curmeta, descriptionAnnotation, descriptionAnnotationText, EqualValueMatcher) ||
				(annotationExists(curmeta, originAnnotation) && !annotationExistsAndMatchValue(curmeta, originAnnotation, origin, MatchOrigin)) {
				return true
			}
		}
	}
	return false
}

func ignore(origin string, meta AnnotationProvider, curmeta metav1.Object) bool {
	if annotationExistsAndValueTrue(meta, resourcesv1alpha1.Ignore) {
		return true
	}
	return ignoreFallback(origin, meta, curmeta)
}

func deleteOnInvalidUpdate(meta metav1.Object) bool {
	return annotationExistsAndValueTrue(meta, resourcesv1alpha1.DeleteOnInvalidUpdate)
}

func keepObject(origin string, meta AnnotationProvider, curmeta metav1.Object) bool {
	return annotationExistsAndValueTrue(meta, resourcesv1alpha1.KeepObject) || ignoreFallback(origin, meta, curmeta)
}

func annotationExistsAndValueTrue(meta AnnotationProvider, key string) bool {
	annotations := meta.GetAnnotations()
	if annotations == nil {
		return false
	}
	val, annotationExists := annotations[key]
	valueTrue, _ := strconv.ParseBool(val)
	return annotationExists && valueTrue
}

type ValueMatcher func(check, actual string) bool

func EqualValueMatcher(check, actual string) bool {
	return check == actual
}

func annotationExistsAndMatchValue(meta AnnotationProvider, key string, value string, matcher ValueMatcher) bool {
	annotations := meta.GetAnnotations()
	if annotations == nil {
		return false
	}
	val, annotationExists := annotations[key]
	return annotationExists && matcher(value, val)
}

func annotationExists(meta AnnotationProvider, key string) bool {
	annotations := meta.GetAnnotations()
	if annotations == nil {
		return false
	}
	return true
}

func (r *Reconciler) cleanOldResources(index *ObjectIndex, mr *resourcesv1alpha1.ManagedResource) (bool, error) {
	type output struct {
		resource        string
		deletionPending bool
		err             error
	}

	var (
		origin          = r.origin(mr)
		results         = make(chan *output)
		wg              sync.WaitGroup
		deletePVCs      = mr.Spec.DeletePersistentVolumeClaims != nil && *mr.Spec.DeletePersistentVolumeClaims
		deletionPending = false
		errorList       = &multierror.Error{
			ErrorFormat: utils.NewErrorFormatFuncWithPrefix("Could not clean all old resources"),
		}
	)

	for _, oldResource := range index.Objects() {
		if !index.Found(oldResource) {
			wg.Add(1)
			go func(ref resourcesv1alpha1.ObjectReference) {
				defer wg.Done()

				obj := &unstructured.Unstructured{}
				obj.SetAPIVersion(ref.APIVersion)
				obj.SetKind(ref.Kind)
				obj.SetNamespace(ref.Namespace)
				obj.SetName(ref.Name)

				resource := unstructuredToString(obj)
				r.log.Info("Deleting", "resource", resource)

				// get object before deleting to be able to do cleanup work for it
				if err := r.targetClient.Get(r.ctx, client.ObjectKey{Namespace: ref.Namespace, Name: ref.Name}, obj); err != nil {
					if !apierrors.IsNotFound(err) && !meta.IsNoMatchError(err) {
						r.log.Error(err, "Error during deletion", "resource", resource)
						results <- &output{resource, true, err}
						return
					}

					// resource already deleted, nothing to do here
					results <- &output{resource, false, nil}
					return
				}
				if keepObject(origin, oldResource, obj) {
					r.log.Info("Keeping object in the system as "+resourcesv1alpha1.KeepObject+" annotation found", "resource", unstructuredToString(obj))
					results <- &output{resource, false, nil}
					return
				}

				if err := cleanup(r.ctx, r.targetClient, r.targetScheme, obj, deletePVCs); err != nil {
					r.log.Error(err, "Error during cleanup", "resource", resource)
					results <- &output{resource: resource, deletionPending: true, err: err}
					return
				}

				deleteOptions := &client.DeleteOptions{}

				// only delete resources in specific API groups with foreground deletion propagation
				// see https://github.com/kubernetes/kubernetes/issues/91621, https://github.com/kubernetes/kubernetes/issues/91287
				// and similar, because of which some objects (e.g `rbac/*` or `v1/Service`) cannot be deleted reliably
				// with foreground deletion propagation.
				if foregroundDeletionAPIGroups.Has(obj.GroupVersionKind().Group) {
					// delete with DeletePropagationForeground to be sure to cleanup all resources (e.g. batch/v1beta1.CronJob
					// defaults PropagationPolicy to Orphan for backwards compatibility, so it will orphan its Jobs)
					deleteOptions.PropagationPolicy = &deletePropagationForeground
				}

				if err := r.targetClient.Delete(r.ctx, obj, deleteOptions); err != nil {
					if !apierrors.IsNotFound(err) && !meta.IsNoMatchError(err) {
						r.log.Error(err, "Error during deletion", "resource", resource)
						results <- &output{resource, true, err}
						return
					}
					results <- &output{resource, false, nil}
					return
				}
				results <- &output{resource, true, nil}
			}(oldResource)
		}
	}

	go func() {
		wg.Wait()
		close(results)
	}()

	for out := range results {
		if out.deletionPending {
			deletionPending = true
			errMsg := fmt.Sprintf("deletion of old resource %q is still pending", out.resource)
			if out.err != nil {
				errMsg = fmt.Sprintf("%s: %v", errMsg, out.err)
			}
			errorList = multierror.Append(errorList, errors.New(errMsg))
			continue
		}

		if out.err != nil {
			errorList = multierror.Append(errorList, fmt.Errorf("error during deletion of old resource %q: %w", out.resource, out.err))
		}
	}

	return deletionPending, errorList.ErrorOrNil()
}

func tryUpdateManagedResourceStatus(
	ctx context.Context,
	c client.Client,
	mr *resourcesv1alpha1.ManagedResource,
	resources []resourcesv1alpha1.ObjectReference,
	updatedConditions ...resourcesv1alpha1.ManagedResourceCondition) error {
	return utils.TryUpdateStatus(ctx, retry.DefaultBackoff, c, mr, func() error {
		mr.Status.Conditions = resourcesv1alpha1helper.MergeConditions(mr.Status.Conditions, updatedConditions...)
		mr.Status.Resources = resources
		mr.Status.ObservedGeneration = mr.Generation
		return nil
	})
}

func tryUpdateManagedResourceConditions(ctx context.Context, c client.Client, mr *resourcesv1alpha1.ManagedResource, conditions ...resourcesv1alpha1.ManagedResourceCondition) error {
	return utils.TryUpdateStatus(ctx, retry.DefaultBackoff, c, mr, func() error {
		newConditions := resourcesv1alpha1helper.MergeConditions(mr.Status.Conditions, conditions...)
		mr.Status.Conditions = newConditions
		return nil
	})
}

func unstructuredToString(o *unstructured.Unstructured) string {
	// return no key, but an description including the version
	return objectKey(o.GetAPIVersion(), o.GetKind(), o.GetNamespace(), o.GetName())
}

// injectLabels injects the given labels into the given object's metadata and if present also into the
// pod template's and volume claims templates' metadata
func injectLabels(obj *unstructured.Unstructured, labels map[string]string) error {
	if len(labels) == 0 {
		return nil
	}
	obj.SetLabels(mergeMaps(obj.GetLabels(), labels))

	if err := injectLabelsIntoPodTemplate(obj, labels); err != nil {
		return err
	}

	return injectLabelsIntoVolumeClaimTemplate(obj, labels)
}

func injectLabelsIntoPodTemplate(obj *unstructured.Unstructured, labels map[string]string) error {
	_, found, err := unstructured.NestedMap(obj.Object, "spec", "template")
	if err != nil {
		return err
	}
	if !found {
		return nil
	}

	templateLabels, _, err := unstructured.NestedStringMap(obj.Object, "spec", "template", "metadata", "labels")
	if err != nil {
		return err
	}

	return unstructured.SetNestedField(obj.Object, mergeLabels(templateLabels, labels), "spec", "template", "metadata", "labels")
}

func injectLabelsIntoVolumeClaimTemplate(obj *unstructured.Unstructured, labels map[string]string) error {
	volumeClaimTemplates, templatesFound, err := unstructured.NestedSlice(obj.Object, "spec", "volumeClaimTemplates")
	if err != nil {
		return err
	}
	if !templatesFound {
		return nil
	}

	for i, t := range volumeClaimTemplates {
		template, ok := t.(map[string]interface{})
		if !ok {
			return fmt.Errorf("failed to inject labels into .spec.volumeClaimTemplates[%d], is not a map[string]interface{}", i)
		}

		templateLabels, _, err := unstructured.NestedStringMap(template, "metadata", "labels")
		if err != nil {
			return err
		}

		if err := unstructured.SetNestedField(template, mergeLabels(templateLabels, labels), "metadata", "labels"); err != nil {
			return err
		}
	}

	return unstructured.SetNestedSlice(obj.Object, volumeClaimTemplates, "spec", "volumeClaimTemplates")
}

func mergeLabels(existingLabels, newLabels map[string]string) map[string]interface{} {
	if existingLabels == nil {
		return stringMapToInterfaceMap(newLabels)
	}

	labels := make(map[string]interface{}, len(existingLabels)+len(newLabels))
	for k, v := range existingLabels {
		labels[k] = v
	}
	for k, v := range newLabels {
		labels[k] = v
	}
	return labels
}

func stringMapToInterfaceMap(in map[string]string) map[string]interface{} {
	m := make(map[string]interface{}, len(in))
	for k, v := range in {
		m[k] = v
	}
	return m
}

// mergeMaps merges the two string maps. If a key is present in both maps, the value in the second map takes precedence
func mergeMaps(one, two map[string]string) map[string]string {
	out := make(map[string]string, len(one)+len(two))
	for k, v := range one {
		out[k] = v
	}
	for k, v := range two {
		out[k] = v
	}
	return out
}

type object struct {
	obj                       *unstructured.Unstructured
	oldInformation            resourcesv1alpha1.ObjectReference
	forceOverwriteLabels      bool
	forceOverwriteAnnotations bool
}

type decodingError struct {
	err               error
	secret            string
	secretKey         string
	objectIndexInFile int
}

func (d *decodingError) StringShort() string {
	return fmt.Sprintf("Could not decode resource at index %d in '%s' in secret '%s'", d.objectIndexInFile, d.secretKey, d.secret)
}

func (d *decodingError) String() string {
	return fmt.Sprintf("%s: %s.", d.StringShort(), d.err)
}
