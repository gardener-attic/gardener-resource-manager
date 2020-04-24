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
	"fmt"
	"io"
	"strconv"
	"sync"
	"time"

	hvpav1alpha1 "github.com/gardener/hvpa-controller/api/v1alpha1"
	autoscalingv1 "k8s.io/api/autoscaling/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"

	resourcesv1alpha1 "github.com/gardener/gardener-resource-manager/pkg/apis/resources/v1alpha1"
	resourcesv1alpha1helper "github.com/gardener/gardener-resource-manager/pkg/apis/resources/v1alpha1/helper"
	"github.com/gardener/gardener-resource-manager/pkg/controller/utils"

	"github.com/go-logr/logr"
	"github.com/hashicorp/go-multierror"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
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
	// The concrete finalizer is finally componed by this base name and the resource class.
	FinalizerName = "resources.gardener.cloud/gardener-resource-manager"
)

var (
	deletePropagationForeground = metav1.DeletePropagationForeground
)

// Reconciler contains information in order to reconcile instances of ManagedResource.
type Reconciler struct {
	ctx context.Context
	log logr.Logger

	client           client.Client
	targetClient     client.Client
	targetRESTMapper *restmapper.DeferredDiscoveryRESTMapper

	class      *ClassFilter
	syncPeriod time.Duration
}

// NewReconciler creates a new reconciler with the given target client.
func NewReconciler(ctx context.Context, log logr.Logger, c, targetClient client.Client, targetRESTMapper *restmapper.DeferredDiscoveryRESTMapper, class *ClassFilter, syncPeriod time.Duration) *Reconciler {
	return &Reconciler{ctx, log, c, targetClient, targetRESTMapper, class, syncPeriod}
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
		log.Error(err, "Could not fetch ManagedResource")
		return reconcile.Result{}, err
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

func (r *Reconciler) reconcile(mr *resourcesv1alpha1.ManagedResource, log logr.Logger) (ctrl.Result, error) {
	log.Info("Starting to reconcile ManagedResource")

	if err := utils.EnsureFinalizer(r.ctx, r.client, r.class.FinalizerName(), mr); err != nil {
		return reconcile.Result{}, err
	}

	var (
		newResourcesObjects          []object
		newResourcesObjectReferences []resourcesv1alpha1.ObjectReference
		newResourcesSet              = sets.NewString()

		equivalences           = NewEquivalences(mr.Spec.Equivalences...)
		existingResourcesIndex = NewObjectIndex(mr.Status.Resources, equivalences)

		forceOverwriteLabels      bool
		forceOverwriteAnnotations bool

		decodingErrors []*decodingError
	)

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
			log.Error(err, "Could not read secret", "name", secret.Name)

			conditionResourcesApplied = resourcesv1alpha1helper.UpdatedCondition(conditionResourcesApplied, resourcesv1alpha1.ConditionFalse, "CannotReadSecret", err.Error())
			newConditions := resourcesv1alpha1helper.MergeConditions(mr.Status.Conditions, conditionResourcesApplied)
			if err := tryUpdateManagedResourceStatus(r.ctx, r.client, mr, newConditions, mr.Status.Resources); err != nil {
				log.Error(err, "Could not update the ManagedResource status")
				return ctrl.Result{}, err
			}

			return reconcile.Result{}, err
		}

		if err := utils.EnsureFinalizer(r.ctx, r.client, r.class.FinalizerName(), secret); err != nil {
			log.Error(err, "Failed to ensure finalizer on secret %q referenced by managed resource", "name", ref.Name)
			return reconcile.Result{}, err
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
					if meta.IsNoMatchError(err) {
						// Reset RESTMapper in case of cache misses.
						r.targetRESTMapper.Reset()
					}
					log.Error(err, fmt.Sprintf("could not get rest mapping for %s '%s/%s'", obj.GetKind(), obj.GetNamespace(), obj.GetName()),
						"secret", fmt.Sprintf("%s/%s", secret.Namespace, secret.Name), "secretKey", key, "objectIndexInFile", i)
					return reconcile.Result{}, err
				}

				if mapping.Scope.Name() == meta.RESTScopeNameNamespace {
					// default namespace field to `default` in case of namespaced kinds
					if obj.GetNamespace() == "" {
						obj.SetNamespace(metav1.NamespaceDefault)
					}
				} else {
					// unset namespace field in case of non-namespaced kinds
					obj.SetNamespace("")
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
						Labels: mergeMaps(newObj.obj.GetLabels(), mr.Spec.InjectLabels),
					}
				)

				newObj.oldInformation, _ = existingResourcesIndex.Lookup(objectReference)
				decodedObj = nil

				newResourcesObjects = append(newResourcesObjects, newObj)
				newResourcesObjectReferences = append(newResourcesObjectReferences, objectReference)
				newResourcesSet.Insert(objectKeyByReference(objectReference))
			}
		}
	}

	if deletionPending, err := r.cleanOldResources(existingResourcesIndex); err != nil {
		var reason string
		if deletionPending {
			reason = "DeletionPending"
			log.Error(err, "Deletion is still pending")
		} else {
			reason = "DeletionFailed"
			log.Error(err, "Deletion of old resources failed")
		}

		conditionResourcesApplied = resourcesv1alpha1helper.UpdatedCondition(conditionResourcesApplied, resourcesv1alpha1.ConditionFalse, reason, err.Error())
		newConditions := resourcesv1alpha1helper.MergeConditions(mr.Status.Conditions, conditionResourcesApplied)
		if err := tryUpdateManagedResourceStatus(r.ctx, r.client, mr, newConditions, mr.Status.Resources); err != nil {
			log.Error(err, "Could not update the ManagedResource status")
			return ctrl.Result{}, err
		}

		return ctrl.Result{}, err
	}

	if err := r.applyNewResources(newResourcesObjects, mr.Spec.InjectLabels, equivalences); err != nil {
		log.Error(err, "Could not apply all new resources")

		conditionResourcesApplied = resourcesv1alpha1helper.UpdatedCondition(conditionResourcesApplied, resourcesv1alpha1.ConditionFalse, "ApplyFailed", err.Error())
		newConditions := resourcesv1alpha1helper.MergeConditions(mr.Status.Conditions, conditionResourcesApplied)
		if err := tryUpdateManagedResourceStatus(r.ctx, r.client, mr, newConditions, mr.Status.Resources); err != nil {
			log.Error(err, "Could not update the ManagedResource status")
			return ctrl.Result{}, err
		}

		return ctrl.Result{}, err
	}

	if len(decodingErrors) != 0 {
		conditionResourcesApplied = resourcesv1alpha1helper.UpdatedCondition(conditionResourcesApplied, resourcesv1alpha1.ConditionFalse, "DecodingFailed", fmt.Sprintf("Could not decode all new resources: %v", decodingErrors))
	} else {
		conditionResourcesApplied = resourcesv1alpha1helper.UpdatedCondition(conditionResourcesApplied, resourcesv1alpha1.ConditionTrue, "ApplySucceeded", "All resources are applied.")
	}
	newConditions := resourcesv1alpha1helper.MergeConditions(mr.Status.Conditions, conditionResourcesApplied)
	if err := tryUpdateManagedResourceStatus(r.ctx, r.client, mr, newConditions, newResourcesObjectReferences); err != nil {
		log.Error(err, "Could not update the ManagedResource status")
		return ctrl.Result{}, err
	}

	log.Info("Finished to reconcile ManagedResource")
	return ctrl.Result{RequeueAfter: r.syncPeriod}, nil
}

func (r *Reconciler) delete(mr *resourcesv1alpha1.ManagedResource, log logr.Logger) (ctrl.Result, error) {
	log.Info("Starting to delete ManagedResource")

	conditionResourcesApplied := resourcesv1alpha1helper.GetOrInitCondition(mr.Status.Conditions, resourcesv1alpha1.ResourcesApplied)

	if keepObjects := mr.Spec.KeepObjects; keepObjects == nil || !*keepObjects {
		existingResourcesIndex := NewObjectIndex(mr.Status.Resources, nil)
		if deletionPending, err := r.cleanOldResources(existingResourcesIndex); err != nil {
			var reason string
			if deletionPending {
				reason = "DeletionPending"
				log.Info("Deletion is still pending", "err", err)
			} else {
				reason = "DeletionFailed"
				log.Error(err, "Deletion of all resources failed")
			}

			conditionResourcesApplied = resourcesv1alpha1helper.UpdatedCondition(conditionResourcesApplied, resourcesv1alpha1.ConditionFalse, reason, err.Error())
			newConditions := resourcesv1alpha1helper.MergeConditions(mr.Status.Conditions, conditionResourcesApplied)
			if err := tryUpdateManagedResourceStatus(r.ctx, r.client, mr, newConditions, mr.Status.Resources); err != nil {
				log.Error(err, "Could not update the ManagedResource status")
				return ctrl.Result{}, err
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

	log.Info("All resources have been deleted, removing finalizers from ManagedResource and referenced Secrets")

	for _, ref := range mr.Spec.SecretRefs {
		secret := &corev1.Secret{}
		if err := r.client.Get(r.ctx, client.ObjectKey{Namespace: mr.Namespace, Name: ref.Name}, secret); err != nil {
			log.Error(err, "Could not read secret", "name", secret.Name)
			return reconcile.Result{}, err
		}
		if err := utils.DeleteFinalizer(r.ctx, r.client, r.class.FinalizerName(), secret); err != nil {
			log.Error(err, "Failed to remove finalizer from secret referenced by managed resource", "name", ref.Name)
			return reconcile.Result{}, err
		}
	}

	if err := utils.DeleteFinalizer(r.ctx, r.client, r.class.FinalizerName(), mr); err != nil {
		log.Error(err, "Error removing finalizer from ManagedResource")
		return reconcile.Result{}, err
	}

	log.Info("Finished to delete ManagedResource")
	return ctrl.Result{}, nil
}

func (r *Reconciler) applyNewResources(newResourcesObjects []object, labelsToInject map[string]string, equivalences Equivalences) error {
	var (
		results   = make(chan error)
		wg        sync.WaitGroup
		errorList = &multierror.Error{
			ErrorFormat: utils.NewErrorFormatFuncWithPrefix("Could not apply all new resources"),
		}
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

			// check if this object is scaled by

			results <- retry.RetryOnConflict(retry.DefaultBackoff, func() (err error) {
				if _, err := controllerutil.CreateOrUpdate(r.ctx, r.targetClient, current, func() error {
					metadata, err := meta.Accessor(obj.obj)
					if err != nil {
						return err
					}
					// if the reconcile annotation is set to false, do nothing (ignore the resource)
					if ignore(metadata) {
						return nil
					}

					if err := injectLabels(obj.obj, labelsToInject); err != nil {
						return err
					}
					return merge(obj.obj, current, obj.forceOverwriteLabels, obj.oldInformation.Labels, obj.forceOverwriteAnnotations, obj.oldInformation.Annotations, scaledHorizontally, scaledVertically)
				}); err != nil {
					if meta.IsNoMatchError(err) {
						// Reset RESTMapper in case of cache misses.
						// TODO: Remove this as soon as https://github.com/kubernetes-sigs/controller-runtime/pull/554
						// has been merged and released.
						r.targetRESTMapper.Reset()
					}
					if apierrors.IsConflict(err) {
						r.log.Info(fmt.Sprintf("conflict during apply of object %q: %s", resource, err))
						// return conflict error directly, so that the update will be retried
						return err
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

func ignore(meta metav1.Object) bool {
	val, annotationExists := meta.GetAnnotations()[resourcesv1alpha1.ResourceManagerIgnoreAnnotation]
	ignoreSet, _ := strconv.ParseBool(val)

	return annotationExists && ignoreSet
}

func (r *Reconciler) cleanOldResources(index *ObjectIndex) (bool, error) {
	type output struct {
		resource        string
		deletionPending bool
		err             error
	}

	var (
		results         = make(chan *output)
		wg              sync.WaitGroup
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

				// delete with DeletePropagationForeground to be sure to cleanup all resources (e.g. batch/v1beta1.CronJob
				// defaults PropagationPolicy to Orphan for backwards compatibility, so it will orphan its Jobs)
				if err := r.targetClient.Delete(r.ctx, obj, &client.DeleteOptions{PropagationPolicy: &deletePropagationForeground}); err != nil {
					if !apierrors.IsNotFound(err) {
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
			errorList = multierror.Append(errorList, fmt.Errorf("deletion of old resource %q is still pending", out.resource))
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
	conditions []resourcesv1alpha1.ManagedResourceCondition,
	resources []resourcesv1alpha1.ObjectReference) error {
	return utils.TryUpdateStatus(ctx, retry.DefaultBackoff, c, mr, func() error {
		mr.Status.Conditions = conditions
		mr.Status.Resources = resources
		mr.Status.ObservedGeneration = mr.Generation
		return nil
	})
}

func unstructuredToString(o *unstructured.Unstructured) string {
	// return no key, but an description including the version
	return objectKey(o.GetAPIVersion(), o.GetKind(), o.GetNamespace(), o.GetName())
}

func injectLabels(obj *unstructured.Unstructured, labels map[string]string) error {
	if labels == nil {
		return nil
	}
	if err := unstructured.SetNestedField(obj.Object, mergeLabels(obj.GetLabels(), labels), "metadata", "labels"); err != nil {
		return err
	}

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
