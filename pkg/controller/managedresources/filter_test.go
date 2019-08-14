/*
 * Copyright 2019 SAP SE or an SAP affiliate company. All rights reserved. This file is licensed under the Apache Software License, v. 2 except as noted otherwise in the LICENSE file
 *
 *  Licensed under the Apache License, Version 2.0 (the "License");
 *  you may not use this file except in compliance with the License.
 *  You may obtain a copy of the License at
 *
 *       http://www.apache.org/licenses/LICENSE-2.0
 *
 *  Unless required by applicable law or agreed to in writing, software
 *  distributed under the License is distributed on an "AS IS" BASIS,
 *  WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 *  See the License for the specific language governing permissions and
 *  limitations under the License.
 *
 */

package managedresources_test

import (
	"fmt"

	"github.com/gardener/gardener-resource-manager/pkg/apis/resources/v1alpha1"
	"github.com/gardener/gardener-resource-manager/pkg/controller/managedresources"
	"sigs.k8s.io/controller-runtime/pkg/event"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	. "github.com/onsi/ginkgo/extensions/table"
)

var _ = Describe("ClassFilter", func() {
	var (
		classOld     *string = nil
		finalizerOld         = managedresources.FinalizerName

		classNew     = "new"
		finalizerNew = fmt.Sprintf("%s-%s", managedresources.FinalizerName, classNew)

		mrOldClass = &v1alpha1.ManagedResource{
			ObjectMeta: metav1.ObjectMeta{
				Finalizers: []string{finalizerOld},
			},
			Spec: v1alpha1.ManagedResourceSpec{
				Class: classOld,
			},
		}

		mrNewClassOldFinalizer = &v1alpha1.ManagedResource{
			ObjectMeta: metav1.ObjectMeta{
				Finalizers: []string{finalizerOld},
			},
			Spec: v1alpha1.ManagedResourceSpec{
				Class: &classNew,
			},
		}

		mrNewClass = &v1alpha1.ManagedResource{
			ObjectMeta: metav1.ObjectMeta{
				Finalizers: []string{finalizerNew},
			},
			Spec: v1alpha1.ManagedResourceSpec{
				Class: &classNew,
			},
		}
	)

	DescribeTable("Active",
		func(mr *v1alpha1.ManagedResource, class string, action, responsible bool) {
			filter := managedresources.NewClassFilter(class)

			act, resp := filter.Active(mr)
			Expect(act).To(Equal(action))
			Expect(resp).To(Equal(responsible))
		},
		Entry("is responsible and take action", mrOldClass, "", true, true),
		Entry("is not responsible and take action", mrNewClassOldFinalizer, "", true, false),
		Entry("is responsible and don't take action", mrNewClassOldFinalizer, classNew, false, true),
		Entry("is not responsible and don't take action", mrNewClass, "", false, false),
	)

	DescribeTable("Generic",
		func(mr *v1alpha1.ManagedResource, class string, expectation bool) {
			filter := managedresources.NewClassFilter(class)

			result := filter.Generic(event.GenericEvent{
				Object: mr,
			})
			Expect(result).To(Equal(expectation))
		},
		Entry("Generic event true", mrOldClass, "", true),
		Entry("Generic event true", mrNewClassOldFinalizer, "", true),
		Entry("Generic event true", mrNewClassOldFinalizer, classNew, true),
		Entry("Generic event false", mrNewClass, "", false),
	)

	DescribeTable("Create",
		func(mr *v1alpha1.ManagedResource, class string, expectation bool) {
			filter := managedresources.NewClassFilter(class)

			result := filter.Create(event.CreateEvent{
				Object: mr,
			})
			Expect(result).To(Equal(expectation))
		},
		Entry("Create event true", mrOldClass, "", true),
		Entry("Create event true", mrNewClassOldFinalizer, "", true),
		Entry("Create event true", mrNewClassOldFinalizer, classNew, true),
		Entry("Create event false", mrNewClass, "", false),
	)

	DescribeTable("Delete",
		func(mr *v1alpha1.ManagedResource, class string, expectation bool) {
			filter := managedresources.NewClassFilter(class)

			result := filter.Delete(event.DeleteEvent{
				Object: mr,
			})
			Expect(result).To(Equal(expectation))
		},
		Entry("Delete event true", mrOldClass, "", true),
		Entry("Delete event true", mrNewClassOldFinalizer, "", true),
		Entry("Delete event true", mrNewClassOldFinalizer, classNew, true),
		Entry("Delete event false", mrNewClass, "", false),
	)

	DescribeTable("Update",
		func(mr *v1alpha1.ManagedResource, class string, expectation bool) {
			filter := managedresources.NewClassFilter(class)

			result := filter.Update(event.UpdateEvent{
				ObjectNew: mr,
			})
			Expect(result).To(Equal(expectation))
		},
		Entry("Update event true", mrOldClass, "", true),
		Entry("Update event true", mrNewClassOldFinalizer, "", true),
		Entry("Update event true", mrNewClassOldFinalizer, classNew, true),
		Entry("Update event false", mrNewClass, "", false),
	)
})
