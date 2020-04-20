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

package utils

import (
	"context"
	"reflect"
	"time"

	apiequality "k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/util/retry"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
)

// EvalGenericPredicate returns true if all predicates match for the given object.
func EvalGenericPredicate(obj runtime.Object, predicates ...predicate.Predicate) bool {
	e := NewGenericEventFromObject(obj)

	for _, p := range predicates {
		if !p.Generic(e) {
			return false
		}
	}

	return true
}

// NewGenericEventFromObject creates a new GenericEvent from the given runtime.Object.
//
// It tries to extract a metav1.Object from the given Object. If it fails, the Meta
// of the resulting GenericEvent will be `nil`.
func NewGenericEventFromObject(obj runtime.Object) event.GenericEvent {
	accessor, err := meta.Accessor(obj)
	if err != nil {
		return NewGenericEvent(nil, obj)
	}

	return NewGenericEvent(accessor, obj)
}

// NewGenericEvent creates a new GenericEvent from the given metav1.Object and runtime.Object.
func NewGenericEvent(meta metav1.Object, obj runtime.Object) event.GenericEvent {
	return event.GenericEvent{
		Meta:   meta,
		Object: obj,
	}
}

// EnsureFinalizer ensures that a finalizer of the given name is set on the given object.
// If the finalizer is not set, it adds it to the list of finalizers and updates the remote object.
func EnsureFinalizer(ctx context.Context, c client.Client, finalizerName string, obj runtime.Object) error {
	return retry.RetryOnConflict(retry.DefaultRetry, func() error {
		key, err := client.ObjectKeyFromObject(obj)
		if err != nil {
			return err
		}

		if err := c.Get(ctx, key, obj); err != nil {
			return err
		}

		finalizers, accessor, err := finalizersAndAccessorOf(obj)
		if err != nil {
			return err
		}

		if finalizers.Has(finalizerName) {
			return nil
		}

		finalizers.Insert(finalizerName)
		accessor.SetFinalizers(finalizers.UnsortedList())

		return c.Update(ctx, obj)
	})
}

// DeleteFinalizer ensures that the given finalizer is not present anymore in the given object.
// If it is set, it removes it and issues an update.
func DeleteFinalizer(ctx context.Context, c client.Client, finalizerName string, obj runtime.Object) error {
	return retry.RetryOnConflict(retry.DefaultRetry, func() error {
		key, err := client.ObjectKeyFromObject(obj)
		if err != nil {
			return err
		}

		if err := c.Get(ctx, key, obj); client.IgnoreNotFound(err) != nil {
			return err
		}

		finalizers, accessor, err := finalizersAndAccessorOf(obj)
		if err != nil {
			return err
		}

		if !finalizers.Has(finalizerName) {
			return nil
		}

		finalizers.Delete(finalizerName)
		accessor.SetFinalizers(finalizers.UnsortedList())

		return c.Update(ctx, obj)
	})
}

func finalizersAndAccessorOf(obj runtime.Object) (sets.String, metav1.Object, error) {
	accessor, err := meta.Accessor(obj)
	if err != nil {
		return nil, nil, err
	}

	return sets.NewString(accessor.GetFinalizers()...), accessor, nil
}

// TryUpdate tries to apply the given transformation function onto the given object, and to update it afterwards.
// It retries the update with an exponential backoff.
func TryUpdate(ctx context.Context, backoff wait.Backoff, c client.Client, obj runtime.Object, transform func() error) error {
	return tryUpdate(ctx, backoff, c, obj, c.Update, transform)
}

// TypedCreateOrUpdate is like controllerutil.CreateOrUpdate, it retrieves the current state of the object from the
// API server, applies the given mutate func and creates or updates it afterwards. In contrast to
// controllerutil.CreateOrUpdate it tries to create a new typed object of obj's kind (using the provided scheme)
// to make typed Get requests in order to leverage the client's cache.
func TypedCreateOrUpdate(ctx context.Context, c client.Client, scheme *runtime.Scheme, obj *unstructured.Unstructured, mutate func() error) error {
	key, err := client.ObjectKeyFromObject(obj)
	if err != nil {
		return err
	}

	// client.DelegatingReader does not use its cache for unstructured.Unstructured objects, so we
	// create a new typed object of the object's type to use the cache for get calls before applying changes
	var current runtime.Object
	if typed, err := scheme.New(obj.GetObjectKind().GroupVersionKind()); err == nil {
		current = typed
	} else {
		// fallback to unstructured request (type might not be registered in scheme)
		current = obj
	}

	if err := c.Get(ctx, key, current); err != nil {
		if !apierrors.IsNotFound(err) {
			return err
		}
		if err := mutate(); err != nil {
			return err
		}
		if err := c.Create(ctx, obj); err != nil {
			return err
		}
		return nil
	}

	var existing *unstructured.Unstructured

	// convert object back to unstructured for easy mutating/merging
	if _, isUnstructured := current.(*unstructured.Unstructured); !isUnstructured {
		u := &unstructured.Unstructured{}
		if err := scheme.Convert(current, u, nil); err != nil {
			return err
		}
		u.DeepCopyInto(obj)
		existing = u
	} else {
		existing = obj.DeepCopy()
	}

	if err := mutate(); err != nil {
		return err
	}

	if apiequality.Semantic.DeepEqual(existing, obj) {
		return nil
	}

	if err := c.Update(ctx, obj); err != nil {
		return err
	}
	return nil
}

// TryUpdateStatus tries to apply the given transformation function onto the given object, and to update its
// status afterwards. It retries the status update with an exponential backoff.
func TryUpdateStatus(ctx context.Context, backoff wait.Backoff, c client.Client, obj runtime.Object, transform func() error) error {
	return tryUpdate(ctx, backoff, c, obj, c.Status().Update, transform)
}

func tryUpdate(ctx context.Context, backoff wait.Backoff, c client.Client, obj runtime.Object, updateFunc func(context.Context, runtime.Object, ...client.UpdateOption) error, transform func() error) error {
	key, err := client.ObjectKeyFromObject(obj)
	if err != nil {
		return err
	}

	return exponentialBackoff(ctx, backoff, func() (bool, error) {
		if err := c.Get(ctx, key, obj); err != nil {
			return false, err
		}

		beforeTransform := obj.DeepCopyObject()
		if err := transform(); err != nil {
			return false, err
		}

		if reflect.DeepEqual(obj, beforeTransform) {
			return true, nil
		}

		if err := updateFunc(ctx, obj); err != nil {
			if apierrors.IsConflict(err) {
				return false, nil
			}
			return false, err
		}
		return true, nil
	})
}

func exponentialBackoff(ctx context.Context, backoff wait.Backoff, condition wait.ConditionFunc) error {
	duration := backoff.Duration

	for i := 0; i < backoff.Steps; i++ {
		if ok, err := condition(); err != nil || ok {
			return err
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
			adjusted := duration
			if backoff.Jitter > 0.0 {
				adjusted = wait.Jitter(duration, backoff.Jitter)
			}
			time.Sleep(adjusted)
			duration = time.Duration(float64(duration) * backoff.Factor)
		}

		i++
	}

	return wait.ErrWaitTimeout
}
