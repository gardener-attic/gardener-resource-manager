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

package predicate_test

import (
	resourcesv1alpha1 "github.com/gardener/gardener-resource-manager/pkg/apis/resources/v1alpha1"
	resourcesv1alpha1helper "github.com/gardener/gardener-resource-manager/pkg/apis/resources/v1alpha1/helper"
	managerpredicate "github.com/gardener/gardener-resource-manager/pkg/predicate"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/utils/pointer"
	"sigs.k8s.io/controller-runtime/pkg/event"
)

var _ = Describe("#ConditionStatusChanged", func() {
	var (
		managedResource *resourcesv1alpha1.ManagedResource
		createEvent     event.CreateEvent
		updateEvent     event.UpdateEvent
		deleteEvent     event.DeleteEvent
		genericEvent    event.GenericEvent
		conditionType   resourcesv1alpha1.ConditionType
	)

	BeforeEach(func() {
		managedResource = &resourcesv1alpha1.ManagedResource{
			Spec: resourcesv1alpha1.ManagedResourceSpec{
				Class: pointer.StringPtr("shoot"),
			},
		}
		createEvent = event.CreateEvent{
			Object: managedResource,
		}
		updateEvent = event.UpdateEvent{
			ObjectOld: managedResource,
			ObjectNew: managedResource,
		}
		deleteEvent = event.DeleteEvent{
			Object: managedResource,
		}
		genericEvent = event.GenericEvent{
			Object: managedResource,
		}

		conditionType = resourcesv1alpha1.ResourcesApplied
	})

	It("should not match on update (no change)", func() {
		predicate := managerpredicate.ConditionStatusChanged(conditionType)

		Expect(predicate.Create(createEvent)).To(BeTrue())
		Expect(predicate.Update(updateEvent)).To(BeFalse())
		Expect(predicate.Delete(deleteEvent)).To(BeTrue())
		Expect(predicate.Generic(genericEvent)).To(BeTrue())
	})

	It("should not match on update (old not set)", func() {
		updateEvent.ObjectOld = nil

		predicate := managerpredicate.ConditionStatusChanged(conditionType)

		Expect(predicate.Create(createEvent)).To(BeTrue())
		Expect(predicate.Update(updateEvent)).To(BeFalse())
		Expect(predicate.Delete(deleteEvent)).To(BeTrue())
		Expect(predicate.Generic(genericEvent)).To(BeTrue())
	})

	It("should not match on update (old is not a ManagedResource)", func() {
		updateEvent.ObjectOld = &corev1.Pod{}

		predicate := managerpredicate.ConditionStatusChanged(conditionType)

		Expect(predicate.Create(createEvent)).To(BeTrue())
		Expect(predicate.Update(updateEvent)).To(BeFalse())
		Expect(predicate.Delete(deleteEvent)).To(BeTrue())
		Expect(predicate.Generic(genericEvent)).To(BeTrue())
	})

	It("should not match on update (new not set)", func() {
		updateEvent.ObjectNew = nil

		predicate := managerpredicate.ConditionStatusChanged(conditionType)

		Expect(predicate.Create(createEvent)).To(BeTrue())
		Expect(predicate.Update(updateEvent)).To(BeFalse())
		Expect(predicate.Delete(deleteEvent)).To(BeTrue())
		Expect(predicate.Generic(genericEvent)).To(BeTrue())
	})

	It("should not match on update (new is not a ManagedResource)", func() {
		updateEvent.ObjectNew = &corev1.Pod{}

		predicate := managerpredicate.ConditionStatusChanged(conditionType)

		Expect(predicate.Create(createEvent)).To(BeTrue())
		Expect(predicate.Update(updateEvent)).To(BeFalse())
		Expect(predicate.Delete(deleteEvent)).To(BeTrue())
		Expect(predicate.Generic(genericEvent)).To(BeTrue())
	})

	It("should match on update (condition added)", func() {
		condition := resourcesv1alpha1helper.InitCondition(conditionType)
		managedResourceNew := managedResource.DeepCopy()
		condition.Status = resourcesv1alpha1.ConditionTrue
		managedResourceNew.Status.Conditions = []resourcesv1alpha1.ManagedResourceCondition{condition}
		updateEvent.ObjectNew = managedResourceNew

		predicate := managerpredicate.ConditionStatusChanged(conditionType)

		Expect(predicate.Create(createEvent)).To(BeTrue())
		Expect(predicate.Update(updateEvent)).To(BeTrue())
		Expect(predicate.Delete(deleteEvent)).To(BeTrue())
		Expect(predicate.Generic(genericEvent)).To(BeTrue())
	})

	It("should match on update (condition removed)", func() {
		condition := resourcesv1alpha1helper.InitCondition(conditionType)
		condition.Status = resourcesv1alpha1.ConditionTrue
		managedResourceNew := managedResource.DeepCopy()
		managedResource.Status.Conditions = []resourcesv1alpha1.ManagedResourceCondition{condition}
		updateEvent.ObjectNew = managedResourceNew

		predicate := managerpredicate.ConditionStatusChanged(conditionType)

		Expect(predicate.Create(createEvent)).To(BeTrue())
		Expect(predicate.Update(updateEvent)).To(BeTrue())
		Expect(predicate.Delete(deleteEvent)).To(BeTrue())
		Expect(predicate.Generic(genericEvent)).To(BeTrue())
	})

	It("should match on update (condition status changed)", func() {
		condition := resourcesv1alpha1helper.InitCondition(conditionType)
		condition.Status = resourcesv1alpha1.ConditionProgressing
		managedResource.Status.Conditions = []resourcesv1alpha1.ManagedResourceCondition{condition}

		managedResourceNew := managedResource.DeepCopy()
		condition.Status = resourcesv1alpha1.ConditionTrue
		managedResourceNew.Status.Conditions = []resourcesv1alpha1.ManagedResourceCondition{condition}
		updateEvent.ObjectNew = managedResourceNew

		predicate := managerpredicate.ConditionStatusChanged(conditionType)

		Expect(predicate.Create(createEvent)).To(BeTrue())
		Expect(predicate.Update(updateEvent)).To(BeTrue())
		Expect(predicate.Delete(deleteEvent)).To(BeTrue())
		Expect(predicate.Generic(genericEvent)).To(BeTrue())
	})
})
