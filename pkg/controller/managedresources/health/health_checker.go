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

package health

import (
	health "github.com/gardener/gardener-resource-manager/pkg/health"

	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	apiextensionsv1beta1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1beta1"
	"k8s.io/apimachinery/pkg/runtime"
)

// CheckHealth checks whether the given `runtime.Unstructured` is healthy.
// `nil` is returned when the `runtime.Unstructured` has kind which is not supported by this function.
func CheckHealth(obj runtime.Unstructured) error {
	switch obj.GetObjectKind().GroupVersionKind().GroupKind() {
	case apiextensionsv1beta1.SchemeGroupVersion.WithKind("CustomResourceDefinition").GroupKind():
		crd := &apiextensionsv1beta1.CustomResourceDefinition{}
		if err := runtime.DefaultUnstructuredConverter.FromUnstructured(obj.UnstructuredContent(), crd); err != nil {
			return err
		}
		return health.CheckCustomResourceDefinition(crd)
	case appsv1.SchemeGroupVersion.WithKind("DaemonSet").GroupKind():
		ds := &appsv1.DaemonSet{}
		if err := runtime.DefaultUnstructuredConverter.FromUnstructured(obj.UnstructuredContent(), ds); err != nil {
			return err
		}
		return health.CheckDaemonSet(ds)
	case appsv1.SchemeGroupVersion.WithKind("Deployment").GroupKind():
		deploy := &appsv1.Deployment{}
		if err := runtime.DefaultUnstructuredConverter.FromUnstructured(obj.UnstructuredContent(), deploy); err != nil {
			return err
		}
		return health.CheckDeployment(deploy)
	case batchv1.SchemeGroupVersion.WithKind("Job").GroupKind():
		job := &batchv1.Job{}
		if err := runtime.DefaultUnstructuredConverter.FromUnstructured(obj.UnstructuredContent(), job); err != nil {
			return err
		}
		return health.CheckJob(job)
	case corev1.SchemeGroupVersion.WithKind("Pod").GroupKind():
		pod := &corev1.Pod{}
		if err := runtime.DefaultUnstructuredConverter.FromUnstructured(obj.UnstructuredContent(), pod); err != nil {
			return err
		}
		return health.CheckPod(pod)
	case appsv1.SchemeGroupVersion.WithKind("ReplicaSet").GroupKind():
		rs := &appsv1.ReplicaSet{}
		if err := runtime.DefaultUnstructuredConverter.FromUnstructured(obj.UnstructuredContent(), rs); err != nil {
			return err
		}
		return health.CheckReplicaSet(rs)
	case corev1.SchemeGroupVersion.WithKind("ReplicationController").GroupKind():
		rc := &corev1.ReplicationController{}
		if err := runtime.DefaultUnstructuredConverter.FromUnstructured(obj.UnstructuredContent(), rc); err != nil {
			return err
		}
		return health.CheckReplicationController(rc)
	case appsv1.SchemeGroupVersion.WithKind("StatefulSet").GroupKind():
		statefulSet := &appsv1.StatefulSet{}
		if err := runtime.DefaultUnstructuredConverter.FromUnstructured(obj.UnstructuredContent(), statefulSet); err != nil {
			return err
		}
		return health.CheckStatefulSet(statefulSet)
	}

	return nil
}
