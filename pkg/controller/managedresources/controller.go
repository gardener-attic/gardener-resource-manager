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

	extensionscontroller "github.com/gardener/gardener-extensions/pkg/controller"
	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/apimachinery/pkg/util/yaml"
	"k8s.io/client-go/util/retry"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

// FinalizerName is the finalizer name that is injected into ManagedResources.
const FinalizerName = "resources.gardener.cloud/gardener-resource-manager"

type reconciler struct {
	ctx context.Context
	log logr.Logger

	scheme *runtime.Scheme
	client client.Client

	targetClient client.Client
}

// NewReconciler creates a new reconciler with the given target client.
func NewReconciler(ctx context.Context, log logr.Logger, scheme *runtime.Scheme, c, targetClient client.Client) *reconciler {
	return &reconciler{ctx, log, scheme, c, targetClient}
}

func (r *reconciler) Reconcile(req ctrl.Request) (ctrl.Result, error) {
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

	if mr.DeletionTimestamp != nil {
		return r.delete(mr, log)
	}
	return r.reconcile(mr, log)
}

func (r *reconciler) reconcile(mr *resourcesv1alpha1.ManagedResource, log logr.Logger) (ctrl.Result, error) {
	log.Info("Starting to reconcile ManagedResource")

	if err := extensionscontroller.EnsureFinalizer(r.ctx, r.client, FinalizerName, mr); err != nil {
		return reconcile.Result{}, err
	}

	var (
		newResourcesObjects          []*unstructured.Unstructured
		newResourcesObjectReferences []corev1.ObjectReference
		newResourcesSet              = sets.NewString()
	)

	for _, ref := range mr.Spec.SecretRefs {
		secret := &corev1.Secret{}
		if err := r.client.Get(r.ctx, types.NamespacedName{Namespace: mr.Namespace, Name: ref.Name}, secret); err != nil {
			log.Error(err, "Could not read secret", "name", secret.Name)
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

				newObj := &unstructured.Unstructured{Object: decodedObj}
				decodedObj = nil

				objectReference := corev1.ObjectReference{
					APIVersion: newObj.GetAPIVersion(),
					Kind:       newObj.GetKind(),
					Name:       newObj.GetName(),
					Namespace:  newObj.GetNamespace(),
				}

				newResourcesObjects = append(newResourcesObjects, newObj)
				newResourcesObjectReferences = append(newResourcesObjectReferences, objectReference)
				newResourcesSet.Insert(objectReferenceToString(objectReference))
			}
		}
	}

	if deletionPending, err := r.cleanOldResources(mr, newResourcesSet); err != nil {
		if deletionPending {
			log.Error(err, "Deletion is still pending")
		} else {
			log.Error(err, "Deletion of old resources failed")
		}
		return ctrl.Result{}, err
	}

	if err := extensionscontroller.TryUpdateStatus(r.ctx, retry.DefaultBackoff, r.client, mr, func() error {
		mr.Status.ObservedGeneration = mr.Generation
		mr.Status.Resources = newResourcesObjectReferences
		return nil
	}); err != nil {
		log.Error(err, "Could not update the ManagedResource status")
		return ctrl.Result{}, err
	}

	if err := r.applyNewResources(newResourcesObjects, mr.Spec.InjectLabels); err != nil {
		log.Error(err, "Could not apply all new resources")
		return ctrl.Result{}, err
	}

	log.Info("Finished to reconcile ManagedResource")
	return ctrl.Result{}, nil
}

func (r *reconciler) delete(mr *resourcesv1alpha1.ManagedResource, log logr.Logger) (ctrl.Result, error) {
	log.Info("Starting to delete ManagedResource")

	if deletionPending, err := r.cleanOldResources(mr, sets.NewString()); err != nil {
		if deletionPending {
			log.Error(err, "Deletion is still pending")
		} else {
			log.Error(err, "Deletion of all resources failed")
		}
		return ctrl.Result{}, err
	}

	if err := extensionscontroller.DeleteFinalizer(r.ctx, r.client, FinalizerName, mr); err != nil {
		log.Error(err, "Error removing finalizer from ManagedResource")
		return reconcile.Result{}, err
	}

	log.Info("Finished to delete ManagedResource")
	return ctrl.Result{}, nil
}

func (r *reconciler) applyNewResources(newResourcesObjects []*unstructured.Unstructured, labelsToInject map[string]string) error {
	var (
		results   = make(chan error)
		wg        sync.WaitGroup
		errorList = []error{}
	)

	for _, desired := range newResourcesObjects {
		wg.Add(1)

		go func(desired *unstructured.Unstructured) {
			defer wg.Done()

			if desired.GetNamespace() == "" {
				desired.SetNamespace(metav1.NamespaceDefault)
			}

			var (
				current  = desired.DeepCopy()
				resource = unstructuredToString(desired)
			)

			r.log.Info("Applying", "resource", resource)

			results <- retry.RetryOnConflict(retry.DefaultBackoff, func() (err error) {
				if err := extensionscontroller.CreateOrUpdate(r.ctx, r.targetClient, current, func() error {
					if err := injectLabels(desired, labelsToInject); err != nil {
						return err
					}
					return merge(desired, current)
				}); err != nil {
					return fmt.Errorf("error during apply of object %q: %+v", resource, err)
				}
				return nil
			})
		}(desired)
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

func (r *reconciler) cleanOldResources(mr *resourcesv1alpha1.ManagedResource, newResourcesSet sets.String) (bool, error) {
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

			go func(ref corev1.ObjectReference) {
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
			return true, fmt.Errorf("deletion of old resource %s is still pending", out.resource)
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

func objectReferenceToString(o corev1.ObjectReference) string {
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
