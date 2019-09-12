// Copyright (c) 2018 SAP SE or an SAP affiliate company. All rights reserved. This file is licensed under the Apache Software License, v. 2 except as noted otherwise in the LICENSE file
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

package health_test

import (
	"testing"

	"github.com/gardener/gardener-resource-manager/pkg/apis/resources/v1alpha1"
	"github.com/gardener/gardener-resource-manager/pkg/health"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apiextensionsv1beta1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/ginkgo/extensions/table"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/types"
)

func TestHealth(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Health Suite")
}

var _ = Describe("health", func() {
	Context("CheckCustomResourceDefinition", func() {
		DescribeTable("crds",
			func(crd *apiextensionsv1beta1.CustomResourceDefinition, matcher types.GomegaMatcher) {
				err := health.CheckCustomResourceDefinition(crd)
				Expect(err).To(matcher)
			},
			Entry("terminating", &apiextensionsv1beta1.CustomResourceDefinition{
				Status: apiextensionsv1beta1.CustomResourceDefinitionStatus{
					Conditions: []apiextensionsv1beta1.CustomResourceDefinitionCondition{
						{
							Type:   apiextensionsv1beta1.NamesAccepted,
							Status: apiextensionsv1beta1.ConditionTrue,
						},
						{
							Type:   apiextensionsv1beta1.Established,
							Status: apiextensionsv1beta1.ConditionTrue,
						},
						{
							Type:   apiextensionsv1beta1.Terminating,
							Status: apiextensionsv1beta1.ConditionTrue,
						},
					},
				},
			}, HaveOccurred()),
			Entry("with conflicting name", &apiextensionsv1beta1.CustomResourceDefinition{
				Status: apiextensionsv1beta1.CustomResourceDefinitionStatus{
					Conditions: []apiextensionsv1beta1.CustomResourceDefinitionCondition{
						{
							Type:   apiextensionsv1beta1.NamesAccepted,
							Status: apiextensionsv1beta1.ConditionFalse,
						},
						{
							Type:   apiextensionsv1beta1.Established,
							Status: apiextensionsv1beta1.ConditionFalse,
						},
					},
				},
			}, HaveOccurred()),
			Entry("healthy", &apiextensionsv1beta1.CustomResourceDefinition{
				Status: apiextensionsv1beta1.CustomResourceDefinitionStatus{
					Conditions: []apiextensionsv1beta1.CustomResourceDefinitionCondition{
						{
							Type:   apiextensionsv1beta1.NamesAccepted,
							Status: apiextensionsv1beta1.ConditionTrue,
						},
						{
							Type:   apiextensionsv1beta1.Established,
							Status: apiextensionsv1beta1.ConditionTrue,
						},
					},
				},
			}, BeNil()),
		)
	})

	Context("CheckPod", func() {
		DescribeTable("pods",
			func(pod *corev1.Pod, matcher types.GomegaMatcher) {
				err := health.CheckPod(pod)
				Expect(err).To(matcher)
			},
			Entry("pending", &corev1.Pod{
				Status: corev1.PodStatus{
					Phase: corev1.PodPending,
				},
			}, HaveOccurred()),
			Entry("running", &corev1.Pod{
				Status: corev1.PodStatus{
					Phase: corev1.PodRunning,
				},
			}, BeNil()),
			Entry("succeeded", &corev1.Pod{
				Status: corev1.PodStatus{
					Phase: corev1.PodSucceeded,
				},
			}, BeNil()),
		)
	})

	Context("CheckReplicaSet", func() {
		DescribeTable("replicasets",
			func(rs *appsv1.ReplicaSet, matcher types.GomegaMatcher) {
				err := health.CheckReplicaSet(rs)
				Expect(err).To(matcher)
			},
			Entry("not observed at latest version", &appsv1.ReplicaSet{
				ObjectMeta: metav1.ObjectMeta{Generation: 1},
			}, HaveOccurred()),
			Entry("not enough ready replicas", &appsv1.ReplicaSet{
				Spec:   appsv1.ReplicaSetSpec{Replicas: replicas(2)},
				Status: appsv1.ReplicaSetStatus{ReadyReplicas: 1},
			}, HaveOccurred()),
			Entry("healthy", &appsv1.ReplicaSet{
				Spec:   appsv1.ReplicaSetSpec{Replicas: replicas(2)},
				Status: appsv1.ReplicaSetStatus{ReadyReplicas: 2},
			}, BeNil()),
		)
	})

	Context("CheckReplicationController", func() {
		DescribeTable("replicationcontroller",
			func(rc *corev1.ReplicationController, matcher types.GomegaMatcher) {
				err := health.CheckReplicationController(rc)
				Expect(err).To(matcher)
			},
			Entry("not observed at latest version", &corev1.ReplicationController{
				ObjectMeta: metav1.ObjectMeta{Generation: 1},
			}, HaveOccurred()),
			Entry("not enough ready replicas", &corev1.ReplicationController{
				Spec:   corev1.ReplicationControllerSpec{Replicas: replicas(2)},
				Status: corev1.ReplicationControllerStatus{ReadyReplicas: 1},
			}, HaveOccurred()),
			Entry("healthy", &corev1.ReplicationController{
				Spec:   corev1.ReplicationControllerSpec{Replicas: replicas(2)},
				Status: corev1.ReplicationControllerStatus{ReadyReplicas: 2},
			}, BeNil()),
		)
	})

	Context("CheckManagedResource", func() {
		DescribeTable("managedresource",
			func(mr v1alpha1.ManagedResource, matcher types.GomegaMatcher) {
				err := health.CheckManagedResource(mr)
				Expect(err).To(matcher)
			},
			Entry("applied condition not true", v1alpha1.ManagedResource{
				ObjectMeta: metav1.ObjectMeta{Generation: 1},
				Status: v1alpha1.ManagedResourceStatus{
					ObservedGeneration: 1,
					Conditions: []v1alpha1.ManagedResourceCondition{
						{
							Type:   v1alpha1.ResourcesApplied,
							Status: v1alpha1.ConditionFalse,
						},
						{
							Type:   v1alpha1.ResourcesHealthy,
							Status: v1alpha1.ConditionTrue,
						},
					},
				},
			}, HaveOccurred()),
			Entry("healthy condition not true", v1alpha1.ManagedResource{
				ObjectMeta: metav1.ObjectMeta{Generation: 1},
				Status: v1alpha1.ManagedResourceStatus{
					ObservedGeneration: 1,
					Conditions: []v1alpha1.ManagedResourceCondition{
						{
							Type:   v1alpha1.ResourcesApplied,
							Status: v1alpha1.ConditionTrue,
						},
						{
							Type:   v1alpha1.ResourcesHealthy,
							Status: v1alpha1.ConditionFalse,
						},
					},
				},
			}, HaveOccurred()),
			Entry("conditions true", v1alpha1.ManagedResource{
				ObjectMeta: metav1.ObjectMeta{Generation: 1},
				Status: v1alpha1.ManagedResourceStatus{
					ObservedGeneration: 1,
					Conditions: []v1alpha1.ManagedResourceCondition{
						{
							Type:   v1alpha1.ResourcesApplied,
							Status: v1alpha1.ConditionTrue,
						},
						{
							Type:   v1alpha1.ResourcesHealthy,
							Status: v1alpha1.ConditionTrue,
						},
					},
				},
			}, Not(HaveOccurred())),
			Entry("no applied condition", v1alpha1.ManagedResource{
				ObjectMeta: metav1.ObjectMeta{Generation: 1},
				Status: v1alpha1.ManagedResourceStatus{
					ObservedGeneration: 1,
					Conditions: []v1alpha1.ManagedResourceCondition{
						{
							Type:   v1alpha1.ResourcesHealthy,
							Status: v1alpha1.ConditionTrue,
						},
					},
				},
			}, HaveOccurred()),
			Entry("no conditions", v1alpha1.ManagedResource{
				ObjectMeta: metav1.ObjectMeta{Generation: 1},
				Status: v1alpha1.ManagedResourceStatus{
					ObservedGeneration: 1,
				},
			}, HaveOccurred()),
			Entry("outdated generation", v1alpha1.ManagedResource{
				ObjectMeta: metav1.ObjectMeta{Generation: 2},
				Status: v1alpha1.ManagedResourceStatus{
					ObservedGeneration: 1,
				},
			}, HaveOccurred()),
			Entry("no status", v1alpha1.ManagedResource{
				ObjectMeta: metav1.ObjectMeta{Generation: 2},
			}, HaveOccurred()),
		)
	})
})

func replicas(i int32) *int32 {
	return &i
}
