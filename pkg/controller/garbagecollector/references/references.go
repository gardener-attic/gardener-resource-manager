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

package references

import (
	"strings"

	"github.com/gardener/gardener/pkg/utils"

	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	batchv1beta1 "k8s.io/api/batch/v1beta1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

const (
	// LabelKeyGarbageCollectable is a constant for a label key on a Secret or ConfigMap resource which makes
	// the GRM's garbage collector controller considering it for potential deletion in case it is unused by any
	// workload.
	LabelKeyGarbageCollectable = "resources.gardener.cloud/garbage-collectable-reference"
	// LabelValueGarbageCollectable is a constant for a label value on a Secret or ConfigMap resource which
	// makes the GRM's garbage collector controller considering it for potential deletion in case it is unused by any
	// workload.
	LabelValueGarbageCollectable = "true"

	delimiter = "_"
	// AnnotationKeyPrefix is a constant for the prefix used in annotations keys to indicate references to
	// other resources.
	AnnotationKeyPrefix = "reference/"
	// KindConfigMap is a constant for the 'configmap' kind used in reference annotations.
	KindConfigMap = "configmap"
	// KindSecret is a constant for the 'secret' kind used in reference annotations.
	KindSecret = "secret"
)

// AnnotationKey computes a reference annotation key based on the given object kind and object name.
func AnnotationKey(kind, name string) string {
	return AnnotationKeyPrefix + kind + delimiter + name
}

// KindAndNameFromAnnotationKey computes the object kind and object name based on the given reference annotation key. If
// the key is not valid then both return values will be empty.
func KindAndNameFromAnnotationKey(key string) (kind, name string) {
	if !strings.HasPrefix(key, AnnotationKeyPrefix) {
		return
	}

	var (
		withoutPrefix = strings.TrimPrefix(key, AnnotationKeyPrefix)
		split         = strings.Split(withoutPrefix, delimiter)
	)

	if len(split) != 2 {
		return
	}

	kind = split[0]
	name = split[1]
	return
}

// InjectAnnotations injects annotations into the annotation maps based on the referenced ConfigMaps/Secrets appearing
// in the pod template spec's `.volumes[]` or `.containers[].envFrom[]` lists. Additional reference annotations can be
// specified via the variadic parameter.
func InjectAnnotations(obj runtime.Object, additional ...string) {
	switch o := obj.(type) {
	case *corev1.Pod:
		referenceAnnotations := computeAnnotations(o.Spec, additional...)
		o.Annotations = utils.MergeStringMaps(o.Annotations, referenceAnnotations)

	case *appsv1.ReplicaSet:
		referenceAnnotations := computeAnnotations(o.Spec.Template.Spec, additional...)
		o.Annotations = utils.MergeStringMaps(o.Annotations, referenceAnnotations)
		o.Spec.Template.Annotations = utils.MergeStringMaps(o.Spec.Template.Annotations, referenceAnnotations)

	case *appsv1.Deployment:
		referenceAnnotations := computeAnnotations(o.Spec.Template.Spec, additional...)
		o.Annotations = utils.MergeStringMaps(o.Annotations, referenceAnnotations)
		o.Spec.Template.Annotations = utils.MergeStringMaps(o.Spec.Template.Annotations, referenceAnnotations)

	case *appsv1.StatefulSet:
		referenceAnnotations := computeAnnotations(o.Spec.Template.Spec, additional...)
		o.Annotations = utils.MergeStringMaps(o.Annotations, referenceAnnotations)
		o.Spec.Template.Annotations = utils.MergeStringMaps(o.Spec.Template.Annotations, referenceAnnotations)

	case *appsv1.DaemonSet:
		referenceAnnotations := computeAnnotations(o.Spec.Template.Spec, additional...)
		o.Annotations = utils.MergeStringMaps(o.Annotations, referenceAnnotations)
		o.Spec.Template.Annotations = utils.MergeStringMaps(o.Spec.Template.Annotations, referenceAnnotations)

	case *batchv1.Job:
		referenceAnnotations := computeAnnotations(o.Spec.Template.Spec, additional...)
		o.Annotations = utils.MergeStringMaps(o.Annotations, referenceAnnotations)
		o.Spec.Template.Annotations = utils.MergeStringMaps(o.Spec.Template.Annotations, referenceAnnotations)

	case *batchv1.CronJob:
		referenceAnnotations := computeAnnotations(o.Spec.JobTemplate.Spec.Template.Spec, additional...)
		o.Annotations = utils.MergeStringMaps(o.Annotations, referenceAnnotations)
		o.Spec.JobTemplate.Annotations = utils.MergeStringMaps(o.Spec.JobTemplate.Annotations, referenceAnnotations)
		o.Spec.JobTemplate.Spec.Template.Annotations = utils.MergeStringMaps(o.Spec.JobTemplate.Spec.Template.Annotations, referenceAnnotations)

	case *batchv1beta1.CronJob:
		referenceAnnotations := computeAnnotations(o.Spec.JobTemplate.Spec.Template.Spec, additional...)
		o.Annotations = utils.MergeStringMaps(o.Annotations, referenceAnnotations)
		o.Spec.JobTemplate.Annotations = utils.MergeStringMaps(o.Spec.JobTemplate.Annotations, referenceAnnotations)
		o.Spec.JobTemplate.Spec.Template.Annotations = utils.MergeStringMaps(o.Spec.JobTemplate.Spec.Template.Annotations, referenceAnnotations)
	}
}

func computeAnnotations(spec corev1.PodSpec, additional ...string) map[string]string {
	out := make(map[string]string)

	for _, container := range spec.Containers {
		for _, env := range container.EnvFrom {
			if env.SecretRef != nil {
				out[AnnotationKey(KindSecret, env.SecretRef.Name)] = ""
			}

			if env.ConfigMapRef != nil {
				out[AnnotationKey(KindConfigMap, env.ConfigMapRef.Name)] = ""
			}
		}
	}

	for _, volume := range spec.Volumes {
		if volume.Secret != nil {
			out[AnnotationKey(KindSecret, volume.Secret.SecretName)] = ""
		}

		if volume.ConfigMap != nil {
			out[AnnotationKey(KindConfigMap, volume.ConfigMap.Name)] = ""
		}
	}

	for _, v := range additional {
		out[v] = ""
	}

	return out
}
