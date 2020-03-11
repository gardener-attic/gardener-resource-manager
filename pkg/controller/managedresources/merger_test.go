// Copyright (c) 2020 SAP SE or an SAP affiliate company. All rights reserved. This file is licensed under the Apache Software License, v. 2 except as noted otherwise in the LICENSE file
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
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("merger", func() {

	Describe("#mergeJob", func() {
		var (
			old, new *batchv1.Job
			s        *runtime.Scheme
		)

		BeforeEach(func() {
			s = runtime.NewScheme()
			Expect(batchv1.AddToScheme(s)).ToNot(HaveOccurred(), "schema add should succeed")

			old = &batchv1.Job{
				ObjectMeta: metav1.ObjectMeta{},
				Spec: batchv1.JobSpec{
					Selector: &metav1.LabelSelector{
						MatchLabels: map[string]string{"controller-uid": "1a2b3c"},
					},
					Template: corev1.PodTemplateSpec{
						ObjectMeta: metav1.ObjectMeta{
							Labels: map[string]string{"controller-uid": "1a2b3c", "job-name": "pi"},
						},
					},
				},
			}

			new = old.DeepCopy()
		})

		It("should not overwrite old .spec.selector if the new one is nil", func() {
			new.Spec.Selector = nil

			expected := old.DeepCopy()

			Expect(mergeJob(s, old, new)).NotTo(HaveOccurred(), "merge should be successful")
			Expect(new).To(Equal(expected))
		})

		It("should not overwrite old .spec.template.labels if the new one is nil", func() {
			new.Spec.Template.Labels = nil

			expected := old.DeepCopy()

			Expect(mergeJob(s, old, new)).NotTo(HaveOccurred(), "merge should be successful")
			Expect(new).To(Equal(expected))
		})

		It("should be able to merge new .spec.template.labels with the old ones", func() {
			new.Spec.Template.Labels = map[string]string{"app": "myapp", "version": "v0.1.0"}

			expected := old.DeepCopy()
			expected.Spec.Template.Labels = map[string]string{
				"app":            "myapp",
				"controller-uid": "1a2b3c",
				"job-name":       "pi",
				"version":        "v0.1.0",
			}

			Expect(mergeJob(s, old, new)).NotTo(HaveOccurred(), "merge should be successful")
			Expect(new).To(Equal(expected))
		})
	})
})
