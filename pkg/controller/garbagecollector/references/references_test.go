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

package references_test

import (
	"fmt"

	. "github.com/gardener/gardener-resource-manager/pkg/controller/garbagecollector/references"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/ginkgo/extensions/table"
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	batchv1beta1 "k8s.io/api/batch/v1beta1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

var _ = Describe("References", func() {
	var (
		kind = "some-kind"
		name = "some-name"
		key  = fmt.Sprintf("reference/%s_%s", kind, name)
	)

	Describe("#AnnotationKey", func() {
		It("should compute the expected key", func() {
			Expect(AnnotationKey(kind, name)).To(Equal(key))
		})
	})

	Describe("#KindAndNameFromAnnotationKey", func() {
		It("should return the expected kind and name", func() {
			returnedKind, returnedName := KindAndNameFromAnnotationKey(key)
			Expect(returnedKind).To(Equal(kind))
			Expect(returnedName).To(Equal(name))
		})

		It("should return empty values because key doesn't start as expected", func() {
			returnedKind, returnedName := KindAndNameFromAnnotationKey("foobar/secret/name")
			Expect(returnedKind).To(BeEmpty())
			Expect(returnedName).To(BeEmpty())
		})

		It("should return empty values because key doesn't match expected format", func() {
			returnedKind, returnedName := KindAndNameFromAnnotationKey("reference/secret/name/foo")
			Expect(returnedKind).To(BeEmpty())
			Expect(returnedName).To(BeEmpty())
		})
	})

	Describe("#InjectAnnotations", func() {
		var (
			configMap1            = "cm1"
			configMap2            = "cm2"
			configMap3            = "cm3"
			configMap4            = "cm4"
			secret1               = "secret1"
			secret2               = "secret2"
			secret3               = "secret3"
			secret4               = "secret4"
			additionalAnnotation1 = "foo"
			additionalAnnotation2 = "bar"

			annotations = map[string]string{
				"some-existing": "annotation",
			}
			podSpec = corev1.PodSpec{
				Containers: []corev1.Container{
					{
						EnvFrom: []corev1.EnvFromSource{
							{
								ConfigMapRef: &corev1.ConfigMapEnvSource{
									LocalObjectReference: corev1.LocalObjectReference{
										Name: configMap3,
									},
								},
							},
						},
					},
					{
						EnvFrom: []corev1.EnvFromSource{
							{
								SecretRef: &corev1.SecretEnvSource{
									LocalObjectReference: corev1.LocalObjectReference{
										Name: secret3,
									},
								},
							},
							{
								SecretRef: &corev1.SecretEnvSource{
									LocalObjectReference: corev1.LocalObjectReference{
										Name: secret4,
									},
								},
							},
							{
								ConfigMapRef: &corev1.ConfigMapEnvSource{
									LocalObjectReference: corev1.LocalObjectReference{
										Name: configMap4,
									},
								},
							},
						},
					},
				},
				Volumes: []corev1.Volume{
					{
						VolumeSource: corev1.VolumeSource{
							ConfigMap: &corev1.ConfigMapVolumeSource{
								LocalObjectReference: corev1.LocalObjectReference{Name: configMap1},
							},
						},
					},
					{
						VolumeSource: corev1.VolumeSource{
							Secret: &corev1.SecretVolumeSource{
								SecretName: secret1,
							},
						},
					},
					{
						VolumeSource: corev1.VolumeSource{
							ConfigMap: &corev1.ConfigMapVolumeSource{
								LocalObjectReference: corev1.LocalObjectReference{Name: configMap2},
							},
						},
					},
					{
						VolumeSource: corev1.VolumeSource{
							Secret: &corev1.SecretVolumeSource{
								SecretName: secret2,
							},
						},
					},
				},
			}
			expectedAnnotationsWithExisting = map[string]string{
				"some-existing":            "annotation",
				"reference/configmap_cm1":  "",
				"reference/configmap_cm2":  "",
				"reference/configmap_cm3":  "",
				"reference/configmap_cm4":  "",
				"reference/secret_secret1": "",
				"reference/secret_secret2": "",
				"reference/secret_secret3": "",
				"reference/secret_secret4": "",
				additionalAnnotation1:      "",
				additionalAnnotation2:      "",
			}
			expectedAnnotationsWithoutExisting = map[string]string{
				"reference/configmap_cm1":  "",
				"reference/configmap_cm2":  "",
				"reference/configmap_cm3":  "",
				"reference/configmap_cm4":  "",
				"reference/secret_secret1": "",
				"reference/secret_secret2": "",
				"reference/secret_secret3": "",
				"reference/secret_secret4": "",
				additionalAnnotation1:      "",
				additionalAnnotation2:      "",
			}

			pod = &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: annotations,
				},
				Spec: podSpec,
			}
			replicaSet = &appsv1.ReplicaSet{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: annotations,
				},
				Spec: appsv1.ReplicaSetSpec{
					Template: corev1.PodTemplateSpec{
						Spec: podSpec,
					},
				},
			}
			deployment = &appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: annotations,
				},
				Spec: appsv1.DeploymentSpec{
					Template: corev1.PodTemplateSpec{
						Spec: podSpec,
					},
				},
			}
			statefulSet = &appsv1.StatefulSet{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: annotations,
				},
				Spec: appsv1.StatefulSetSpec{
					Template: corev1.PodTemplateSpec{
						Spec: podSpec,
					},
				},
			}
			daemonSet = &appsv1.DaemonSet{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: annotations,
				},
				Spec: appsv1.DaemonSetSpec{
					Template: corev1.PodTemplateSpec{
						Spec: podSpec,
					},
				},
			}
			job = &batchv1.Job{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: annotations,
				},
				Spec: batchv1.JobSpec{
					Template: corev1.PodTemplateSpec{
						Spec: podSpec,
					},
				},
			}
			cronJob = &batchv1.CronJob{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: annotations,
				},
				Spec: batchv1.CronJobSpec{
					JobTemplate: batchv1.JobTemplateSpec{
						Spec: batchv1.JobSpec{
							Template: corev1.PodTemplateSpec{
								Spec: podSpec,
							},
						},
					},
				},
			}
			cronJobV1beta1 = &batchv1beta1.CronJob{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: annotations,
				},
				Spec: batchv1beta1.CronJobSpec{
					JobTemplate: batchv1beta1.JobTemplateSpec{
						Spec: batchv1.JobSpec{
							Template: corev1.PodTemplateSpec{
								Spec: podSpec,
							},
						},
					},
				},
			}
		)

		It("should do nothing because object is not handled", func() {
			var (
				obj            = &corev1.Service{}
				expectedObject = obj.DeepCopy()
			)

			InjectAnnotations(obj, "foo")

			Expect(obj).To(Equal(expectedObject))
		})

		DescribeTable("should behave properly",
			func(obj runtime.Object, matchers ...func()) {
				InjectAnnotations(obj, additionalAnnotation1, additionalAnnotation2)

				for _, matcher := range matchers {
					matcher()
				}
			},

			Entry("corev1.Pod",
				pod,
				func() {
					Expect(pod.Annotations).To(Equal(expectedAnnotationsWithExisting))
				},
			),
			Entry("appsv1.ReplicaSet",
				replicaSet,
				func() {
					Expect(replicaSet.Annotations).To(Equal(expectedAnnotationsWithExisting))
					Expect(replicaSet.Spec.Template.Annotations).To(Equal(expectedAnnotationsWithoutExisting))
				},
			),
			Entry("appsv1.Deployment",
				deployment,
				func() {
					Expect(deployment.Annotations).To(Equal(expectedAnnotationsWithExisting))
					Expect(deployment.Spec.Template.Annotations).To(Equal(expectedAnnotationsWithoutExisting))
				},
			),
			Entry("appsv1.StatefulSet",
				statefulSet,
				func() {
					Expect(statefulSet.Annotations).To(Equal(expectedAnnotationsWithExisting))
					Expect(statefulSet.Spec.Template.Annotations).To(Equal(expectedAnnotationsWithoutExisting))
				},
			),
			Entry("appsv1.DaemonSet",
				daemonSet,
				func() {
					Expect(daemonSet.Annotations).To(Equal(expectedAnnotationsWithExisting))
					Expect(daemonSet.Spec.Template.Annotations).To(Equal(expectedAnnotationsWithoutExisting))
				},
			),
			Entry("batchv1.Job",
				job,
				func() {
					Expect(job.Annotations).To(Equal(expectedAnnotationsWithExisting))
					Expect(job.Spec.Template.Annotations).To(Equal(expectedAnnotationsWithoutExisting))
				},
			),
			Entry("batchv1.CronJob",
				cronJob,
				func() {
					Expect(cronJob.Annotations).To(Equal(expectedAnnotationsWithExisting))
					Expect(cronJob.Spec.JobTemplate.Annotations).To(Equal(expectedAnnotationsWithoutExisting))
					Expect(cronJob.Spec.JobTemplate.Spec.Template.Annotations).To(Equal(expectedAnnotationsWithoutExisting))
				},
			),
			Entry("batchv1beta1.CronJob",
				cronJobV1beta1,
				func() {
					Expect(cronJob.Annotations).To(Equal(expectedAnnotationsWithExisting))
					Expect(cronJob.Spec.JobTemplate.Annotations).To(Equal(expectedAnnotationsWithoutExisting))
					Expect(cronJob.Spec.JobTemplate.Spec.Template.Annotations).To(Equal(expectedAnnotationsWithoutExisting))
				},
			),
		)
	})
})
