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
	"sync"

	resourcesv1alpha1 "github.com/gardener/gardener-resource-manager/pkg/apis/resources/v1alpha1"
	resourcesv1alpha1helper "github.com/gardener/gardener-resource-manager/pkg/apis/resources/v1alpha1/helper"
	"github.com/gardener/gardener-resource-manager/pkg/controller/utils"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/apimachinery/pkg/util/yaml"
	"k8s.io/client-go/restmapper"
	"k8s.io/client-go/util/retry"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

// FinalizerName is the finalizer base name that is injected into ManagedResources.
// The concrete finalizer is finally componed by this base name and the resource class.
const FinalizerName = "resources.gardener.cloud/gardener-resource-manager"

// Reconciler contains information in order to reconcile instances of ManagedResource.
type Reconciler struct {
	ctx context.Context
	log logr.Logger

	client           client.Client
	targetClient     client.Client
	targetRESTMapper *restmapper.DeferredDiscoveryRESTMapper

	class *ClassFilter
}

// NewReconciler creates a new reconciler with the given target client.
func NewReconciler(ctx context.Context, log logr.Logger, c, targetClient client.Client, targetRESTMapper *restmapper.DeferredDiscoveryRESTMapper, class *ClassFilter) *Reconciler {
	return &Reconciler{ctx, log, c, targetClient, targetRESTMapper, class}
}

// Reconcile implements `reconcile.Reconciler`.
func (r *Reconciler) Reconcile(req ctrl.Request) (ctrl.Result, error) {
	log := r.log.WithValues("object", req)

	mr := &resourcesv1alpha1.ManagedResource{}
	if err := r.client.Get(r.ctx, req.NamespacedName, mr); err != nil {
		if apierrors.IsNotFound(err) {
			log.Error(nil, "Could not find Managedresource")
			return reconcile.Result{}, nil
		}
		log.Error(err, "Could not fetch Managedresource")
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

		existingResourcesIndex = indexResources(mr.Status.Resources)

		forceOverwriteLabels      bool
		forceOverwriteAnnotations bool
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
		if err := r.client.Get(r.ctx, types.NamespacedName{Namespace: mr.Namespace, Name: ref.Name}, secret); err != nil {
			log.Error(err, "Could not read secret", "name", secret.Name)

			conditionResourcesApplied = resourcesv1alpha1helper.UpdatedCondition(conditionResourcesApplied, resourcesv1alpha1.ConditionFalse, "CannotReadSecret", err.Error())
			newConditions := resourcesv1alpha1helper.MergeConditions(mr.Status.Conditions, conditionResourcesApplied)
			if err := tryUpdateManagedResourceStatus(r.ctx, r.client, mr, newConditions, mr.Status.Resources); err != nil {
				log.Error(err, "Could not update the ManagedResource status")
				return ctrl.Result{}, err
			}

			return reconcile.Result{}, err
		}

		for _, value := range secret.Data {
			var (
				decoder    = yaml.NewYAMLOrJSONDecoder(bytes.NewReader(value), 1024)
				decodedObj map[string]interface{}
			)

			for err := decoder.Decode(&decodedObj); err == nil; err = decoder.Decode(&decodedObj) {
				if decodedObj == nil {
					continue
				}

				obj := &unstructured.Unstructured{Object: decodedObj}
				if obj.GetKind() != "Namespace" && obj.GetNamespace() == "" {
					obj.SetNamespace(metav1.NamespaceDefault)
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

				newObj.oldInformation = existingResourcesIndex[objectReferenceToString(objectReference)]
				decodedObj = nil

				newResourcesObjects = append(newResourcesObjects, newObj)
				newResourcesObjectReferences = append(newResourcesObjectReferences, objectReference)
				newResourcesSet.Insert(objectReferenceToString(objectReference))
			}
		}
	}

	if deletionPending, err := r.cleanOldResources(mr, newResourcesSet); err != nil {
		var reason string
		var message string
		if deletionPending {
			reason = "DeletionPending"
			message = "Deletion is still pending"
			log.Error(err, "Deletion is still pending")
		} else {
			reason = "DeletionFailed"
			message = "Deletion of old resources failed"
			log.Error(err, "Deletion of old resources failed")
		}

		conditionResourcesApplied = resourcesv1alpha1helper.UpdatedCondition(conditionResourcesApplied, resourcesv1alpha1.ConditionFalse, reason, message)
		newConditions := resourcesv1alpha1helper.MergeConditions(mr.Status.Conditions, conditionResourcesApplied)
		if err := tryUpdateManagedResourceStatus(r.ctx, r.client, mr, newConditions, mr.Status.Resources); err != nil {
			log.Error(err, "Could not update the ManagedResource status")
			return ctrl.Result{}, err
		}

		return ctrl.Result{}, err
	}

	if err := r.applyNewResources(newResourcesObjects, mr.Spec.InjectLabels); err != nil {
		log.Error(err, "Could not apply all new resources")

		conditionResourcesApplied = resourcesv1alpha1helper.UpdatedCondition(conditionResourcesApplied, resourcesv1alpha1.ConditionFalse, "ApplyFailed", "Could not apply all new resources")
		newConditions := resourcesv1alpha1helper.MergeConditions(mr.Status.Conditions, conditionResourcesApplied)
		if err := tryUpdateManagedResourceStatus(r.ctx, r.client, mr, newConditions, mr.Status.Resources); err != nil {
			log.Error(err, "Could not update the ManagedResource status")
			return ctrl.Result{}, err
		}

		return ctrl.Result{}, err
	}

	conditionResourcesApplied = resourcesv1alpha1helper.UpdatedCondition(conditionResourcesApplied, resourcesv1alpha1.ConditionTrue, "ApplySucceeded", "All resources are applied.")
	newConditions := resourcesv1alpha1helper.MergeConditions(mr.Status.Conditions, conditionResourcesApplied)
	if err := tryUpdateManagedResourceStatus(r.ctx, r.client, mr, newConditions, newResourcesObjectReferences); err != nil {
		log.Error(err, "Could not update the ManagedResource status")
		return ctrl.Result{}, err
	}

	log.Info("Finished to reconcile ManagedResource")
	return ctrl.Result{}, nil
}

func (r *Reconciler) delete(mr *resourcesv1alpha1.ManagedResource, log logr.Logger) (ctrl.Result, error) {
	log.Info("Starting to delete ManagedResource")

	if keepObjects := mr.Spec.KeepObjects; keepObjects == nil || !*keepObjects {
		if deletionPending, err := r.cleanOldResources(mr, sets.NewString()); err != nil {
			if deletionPending {
				log.Error(err, "Deletion is still pending")
			} else {
				log.Error(err, "Deletion of all resources failed")
			}
			return ctrl.Result{}, err
		}
	} else {
		log.Info(fmt.Sprintf("Do not delete any resources of %s because .spec.keepObjects=true", mr.Name))
	}

	if err := utils.DeleteFinalizer(r.ctx, r.client, r.class.FinalizerName(), mr); err != nil {
		log.Error(err, "Error removing finalizer from ManagedResource")
		return reconcile.Result{}, err
	}

	log.Info("Finished to delete ManagedResource")
	return ctrl.Result{}, nil
}

func (r *Reconciler) applyNewResources(newResourcesObjects []object, labelsToInject map[string]string) error {
	var (
		results   = make(chan error)
		wg        sync.WaitGroup
		errorList = []error{}
	)

	for _, o := range newResourcesObjects {
		wg.Add(1)

		go func(obj object) {
			defer wg.Done()

			var (
				current  = obj.obj.DeepCopy()
				resource = unstructuredToString(obj.obj)
			)

			r.log.Info("Applying", "resource", resource)

			results <- retry.RetryOnConflict(retry.DefaultBackoff, func() (err error) {
				if _, err := controllerutil.CreateOrUpdate(r.ctx, r.targetClient, current, func() error {
					if err := injectLabels(obj.obj, labelsToInject); err != nil {
						return err
					}
					return merge(obj.obj, current, obj.forceOverwriteLabels, obj.oldInformation.Labels, obj.forceOverwriteAnnotations, obj.oldInformation.Annotations)
				}); err != nil {
					if meta.IsNoMatchError(err) {
						// Reset RESTMapper in case of cache misses.
						// TODO: Remove this as soon as https://github.com/kubernetes-sigs/controller-runtime/pull/554
						// has been merged and released.
						r.targetRESTMapper.Reset()
					}
					return fmt.Errorf("error during apply of object %q: %w", resource, err)
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
			errorList = append(errorList, err)
		}
	}

	if len(errorList) > 0 {
		return fmt.Errorf("Errors occurred during applying: %+v", errorList)
	}

	return nil
}

func (r *Reconciler) cleanOldResources(mr *resourcesv1alpha1.ManagedResource, newResourcesSet sets.String) (bool, error) {
	type output struct {
		resource        string
		deletionPending bool
		err             error
	}

	var (
		results   = make(chan *output)
		wg        sync.WaitGroup
		errorList = []error{}
	)

	for _, oldResource := range mr.Status.Resources {
		resource := objectReferenceToString(oldResource)

		if !newResourcesSet.Has(resource) {
			wg.Add(1)

			go func(ref resourcesv1alpha1.ObjectReference) {
				defer wg.Done()

				obj := &unstructured.Unstructured{}
				obj.SetAPIVersion(ref.APIVersion)
				obj.SetKind(ref.Kind)
				obj.SetNamespace(ref.Namespace)
				obj.SetName(ref.Name)

				r.log.Info("Deleting", "resource", resource)
				if err := r.targetClient.Delete(r.ctx, obj); err != nil {
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
			return true, fmt.Errorf("Deletion of old resource %s is still pending", out.resource)
		}

		if out.err != nil {
			errorList = append(errorList, out.err)
		}
	}

	if len(errorList) > 0 {
		return true, fmt.Errorf("Errors occurred during deletion: %+v", errorList)
	}
	return false, nil
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

func objectReferenceToString(o resourcesv1alpha1.ObjectReference) string {
	return fmt.Sprintf("%s/%s/%s/%s", o.APIVersion, o.Kind, o.Namespace, o.Name)
}

func unstructuredToString(o *unstructured.Unstructured) string {
	return fmt.Sprintf("%s/%s/%s/%s", o.GetAPIVersion(), o.GetKind(), o.GetNamespace(), o.GetName())
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

func indexResources(resources []resourcesv1alpha1.ObjectReference) map[string]resourcesv1alpha1.ObjectReference {
	out := make(map[string]resourcesv1alpha1.ObjectReference, len(resources))

	for _, r := range resources {
		out[objectReferenceToString(r)] = r
	}

	return out
}

type object struct {
	obj                       *unstructured.Unstructured
	oldInformation            resourcesv1alpha1.ObjectReference
	forceOverwriteLabels      bool
	forceOverwriteAnnotations bool
}
