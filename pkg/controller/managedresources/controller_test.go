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
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/gardener/gardener-resource-manager/pkg/apis/resources/v1alpha1"
)

var _ = Describe("Controller", func() {
	Describe("#injectLabels", func() {
		var (
			obj, expected *unstructured.Unstructured
			labels        map[string]string
		)

		BeforeEach(func() {
			obj = &unstructured.Unstructured{Object: map[string]interface{}{}}
			expected = obj.DeepCopy()
		})

		It("do nothing as labels is nil", func() {
			labels = nil
			Expect(injectLabels(obj, labels)).To(Succeed())
			Expect(obj).To(Equal(expected))
		})

		It("do nothing as labels is empty", func() {
			labels = map[string]string{}
			Expect(injectLabels(obj, labels)).To(Succeed())
			Expect(obj).To(Equal(expected))
		})

		It("should correctly inject labels into the object's metadata", func() {
			labels = map[string]string{
				"inject": "me",
			}
			expected.SetLabels(labels)

			Expect(injectLabels(obj, labels)).To(Succeed())
			Expect(obj).To(Equal(expected))
		})

		It("should correctly inject labels into the object's pod template's metadata", func() {
			labels = map[string]string{
				"inject": "me",
			}

			// add .spec.template to object
			Expect(unstructured.SetNestedMap(obj.Object, map[string]interface{}{
				"template": map[string]interface{}{},
			}, "spec")).To(Succeed())

			expected = obj.DeepCopy()
			Expect(unstructured.SetNestedMap(expected.Object, map[string]interface{}{
				"inject": "me",
			}, "spec", "template", "metadata", "labels")).To(Succeed())
			expected.SetLabels(labels)

			Expect(injectLabels(obj, labels)).To(Succeed())
			Expect(obj).To(Equal(expected))
		})

		It("should correctly inject labels into the object's volumeClaimTemplates' metadata", func() {
			labels = map[string]string{
				"inject": "me",
			}

			// add .spec.volumeClaimTemplates to object
			Expect(unstructured.SetNestedMap(obj.Object, map[string]interface{}{
				"volumeClaimTemplates": []interface{}{
					map[string]interface{}{
						"metadata": map[string]interface{}{
							"name": "volume-claim-name",
						},
					},
				},
			}, "spec")).To(Succeed())

			expected = obj.DeepCopy()
			Expect(unstructured.SetNestedMap(expected.Object, map[string]interface{}{
				"volumeClaimTemplates": []interface{}{
					map[string]interface{}{
						"metadata": map[string]interface{}{
							"name": "volume-claim-name",
							"labels": map[string]interface{}{
								"inject": "me",
							},
						},
					},
				},
			}, "spec")).To(Succeed())
			expected.SetLabels(labels)

			Expect(injectLabels(obj, labels)).To(Succeed())
			Expect(obj).To(Equal(expected))
		})
	})
	Describe("#ignore", func() {
		origin := "origin"
		newmeta := &metav1.ObjectMeta{}
		newmeta.SetAnnotations(map[string]string{
			descriptionAnnotation: descriptionAnnotationText,
			originAnnotation:      origin,
		})
		actual := newmeta.DeepCopy()
		actual.SetResourceVersion("0815")

		Context("non-fallback", func() {
			It("accept managed", func() {
				Expect(ignore(origin, newmeta, actual)).To(BeFalse())
			})
			It("accept new", func() {
				actual := &metav1.ObjectMeta{}
				Expect(ignore(origin, newmeta, actual)).To(BeFalse())
			})
			It("ignore unmanaged", func() {
				actual := actual.DeepCopy()
				delete(actual.Annotations, descriptionAnnotation)
				Expect(ignore(origin, newmeta, actual)).To(BeFalse())
			})
			It("don't ignore foreign managed", func() {
				actual := actual.DeepCopy()
				actual.Annotations[originAnnotation] = "other"
				Expect(ignore(origin, newmeta, actual)).To(BeFalse())
			})
			It("don't ignore resource marked as ignore", func() {
				newmeta := &metav1.ObjectMeta{}
				newmeta.SetAnnotations(map[string]string{v1alpha1.Ignore: "true"})
				Expect(ignore(origin, newmeta, actual)).To(BeTrue())
			})
		})
		Context("fallback", func() {
			newmeta := newmeta.DeepCopy()
			newmeta.Annotations[v1alpha1.Fallback] = "true"

			It("accept managed", func() {
				Expect(ignore(origin, newmeta, actual)).To(BeFalse())
			})
			It("accept new", func() {
				actual := &metav1.ObjectMeta{}
				Expect(ignore(origin, newmeta, actual)).To(BeFalse())
			})
			It("ignore unmanaged", func() {
				actual := actual.DeepCopy()
				delete(actual.Annotations, descriptionAnnotation)
				Expect(ignore(origin, newmeta, actual)).To(BeTrue())
			})
			It("ignore foreign managed", func() {
				actual := actual.DeepCopy()
				actual.Annotations[originAnnotation] = "other"
				Expect(ignore(origin, newmeta, actual)).To(BeTrue())
			})
			It("ignore resource marked as ignore", func() {
				newmeta := &metav1.ObjectMeta{}
				newmeta.SetAnnotations(map[string]string{v1alpha1.Ignore: "true"})
				Expect(ignore(origin, newmeta, actual)).To(BeTrue())
			})
		})

	})
})
