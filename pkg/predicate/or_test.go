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
	managerpredicate "github.com/gardener/gardener-resource-manager/pkg/predicate"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"k8s.io/utils/pointer"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
)

var _ = Describe("#Or", func() {
	var (
		managedResource               *resourcesv1alpha1.ManagedResource
		createEvent                   event.CreateEvent
		updateEvent                   event.UpdateEvent
		deleteEvent                   event.DeleteEvent
		genericEvent                  event.GenericEvent
		falsePredicate, truePredicate predicate.Predicate
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

		falsePredicate = predicate.Funcs{
			CreateFunc:  func(_ event.CreateEvent) bool { return false },
			DeleteFunc:  func(_ event.DeleteEvent) bool { return false },
			UpdateFunc:  func(_ event.UpdateEvent) bool { return false },
			GenericFunc: func(_ event.GenericEvent) bool { return false },
		}
		truePredicate = predicate.Funcs{
			CreateFunc:  func(_ event.CreateEvent) bool { return true },
			DeleteFunc:  func(_ event.DeleteEvent) bool { return true },
			UpdateFunc:  func(_ event.UpdateEvent) bool { return true },
			GenericFunc: func(_ event.GenericEvent) bool { return true },
		}
	})

	It("should not match if none matches", func() {
		predicate := managerpredicate.Or(falsePredicate, falsePredicate)

		Expect(predicate.Create(createEvent)).To(BeFalse())
		Expect(predicate.Update(updateEvent)).To(BeFalse())
		Expect(predicate.Delete(deleteEvent)).To(BeFalse())
		Expect(predicate.Generic(genericEvent)).To(BeFalse())
	})

	It("should match if one matches", func() {
		predicate := managerpredicate.Or(falsePredicate, truePredicate)

		Expect(predicate.Create(createEvent)).To(BeTrue())
		Expect(predicate.Update(updateEvent)).To(BeTrue())
		Expect(predicate.Delete(deleteEvent)).To(BeTrue())
		Expect(predicate.Generic(genericEvent)).To(BeTrue())
	})

	It("should match if all match", func() {
		predicate := managerpredicate.Or(truePredicate, truePredicate)

		Expect(predicate.Create(createEvent)).To(BeTrue())
		Expect(predicate.Update(updateEvent)).To(BeTrue())
		Expect(predicate.Delete(deleteEvent)).To(BeTrue())
		Expect(predicate.Generic(genericEvent)).To(BeTrue())
	})
})
