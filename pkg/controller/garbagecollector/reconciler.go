// Copyright (c) 2021 SAP SE or an SAP affiliate company. All rights reserved. This file is licensed under the Apache Software License, v. 2 except as noted otherwise in the LICENSE file
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

package garbagecollector

import (
	"context"
	"time"

	"github.com/gardener/gardener-resource-manager/pkg/controller/garbagecollector/references"
	"github.com/gardener/gardener-resource-manager/pkg/controller/utils"

	"github.com/go-logr/logr"
	"github.com/hashicorp/go-multierror"
	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	batchv1beta1 "k8s.io/api/batch/v1beta1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/wait"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

type reconciler struct {
	log          logr.Logger
	syncPeriod   time.Duration
	targetClient client.Client
}

func (r *reconciler) InjectLogger(l logr.Logger) error {
	r.log = l.WithName(ControllerName)
	return nil
}

func (r *reconciler) Reconcile(reconcileCtx context.Context, _ reconcile.Request) (reconcile.Result, error) {
	ctx, cancel := context.WithTimeout(reconcileCtx, time.Minute)
	defer cancel()

	r.log.Info("Starting garbage collection")
	defer r.log.Info("Garbage collection finished")

	var (
		labels                  = client.MatchingLabels{references.LabelKeyGarbageCollectable: references.LabelValueGarbageCollectable}
		objectsToGarbageCollect = map[objectId]struct{}{}
	)

	for _, resource := range []struct {
		kind     string
		listKind string
	}{
		{references.KindSecret, "SecretList"},
		{references.KindConfigMap, "ConfigMapList"},
	} {
		objList := &metav1.PartialObjectMetadataList{}
		objList.SetGroupVersionKind(corev1.SchemeGroupVersion.WithKind(resource.listKind))
		if err := r.targetClient.List(ctx, objList, labels); err != nil {
			return reconcile.Result{}, err
		}

		for _, obj := range objList.Items {
			objectsToGarbageCollect[objectId{resource.kind, obj.Namespace, obj.Name}] = struct{}{}
		}
	}

	for _, gvk := range []schema.GroupVersionKind{
		appsv1.SchemeGroupVersion.WithKind("DeploymentList"),
		appsv1.SchemeGroupVersion.WithKind("StatefulSetList"),
		appsv1.SchemeGroupVersion.WithKind("DaemonSetList"),
		batchv1.SchemeGroupVersion.WithKind("JobList"),
		batchv1beta1.SchemeGroupVersion.WithKind("CronJobList"),
		corev1.SchemeGroupVersion.WithKind("PodList"),
	} {
		objList := &metav1.PartialObjectMetadataList{}
		objList.SetGroupVersionKind(gvk)
		if err := r.targetClient.List(ctx, objList); err != nil {
			return reconcile.Result{}, err
		}

		for _, objectMeta := range objList.Items {
			for key, objectName := range objectMeta.Annotations {
				objectKind := references.KindFromAnnotationKey(key)
				if objectKind == "" || objectName == "" {
					continue
				}

				delete(objectsToGarbageCollect, objectId{objectKind, objectMeta.Namespace, objectName})
			}
		}
	}

	var (
		results   = make(chan error, 1)
		wg        wait.Group
		errorList = &multierror.Error{ErrorFormat: utils.NewErrorFormatFuncWithPrefix("Could not delete all unused resources")}
	)

	for id := range objectsToGarbageCollect {
		objId := id
		wg.StartWithContext(ctx, func(ctx context.Context) {
			var (
				meta = metav1.ObjectMeta{Namespace: objId.namespace, Name: objId.name}
				obj  client.Object
			)

			switch objId.kind {
			case references.KindSecret:
				obj = &corev1.Secret{ObjectMeta: meta}
			case references.KindConfigMap:
				obj = &corev1.ConfigMap{ObjectMeta: meta}
			default:
				return
			}

			r.log.Info("Delete resource",
				"kind", objId.kind,
				"namespace", objId.namespace,
				"name", objId.name,
			)

			if err := r.targetClient.Delete(ctx, obj); client.IgnoreNotFound(err) != nil {
				results <- err
			}
		})
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

	return reconcile.Result{Requeue: true, RequeueAfter: r.syncPeriod}, errorList.ErrorOrNil()
}

type objectId struct {
	kind      string
	namespace string
	name      string
}
