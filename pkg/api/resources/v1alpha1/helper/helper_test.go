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

package helper_test

import (
	"testing"
	"time"

	resourcesv1alpha1 "github.com/gardener/gardener-resource-manager/api/resources/v1alpha1"
	"github.com/gardener/gardener-resource-manager/api/resources/v1alpha1/helper"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/ginkgo/extensions/table"
	. "github.com/onsi/gomega"
	. "github.com/onsi/gomega/gstruct"
	. "github.com/onsi/gomega/types"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestHelper(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "API v1alpha1 Helper Suite")
}

var zeroTime metav1.Time

var _ = Describe("helper", func() {
	DescribeTable("#UpdatedCondition",
		func(condition resourcesv1alpha1.ManagedResourceCondition, status resourcesv1alpha1.ConditionStatus, reason, message string, matcher GomegaMatcher) {
			updated := helper.UpdatedCondition(condition, status, reason, message)

			Expect(updated).To(matcher)
		},
		Entry("no update",
			resourcesv1alpha1.ManagedResourceCondition{
				Status:  resourcesv1alpha1.ConditionTrue,
				Reason:  "reason",
				Message: "message",
			},
			resourcesv1alpha1.ConditionTrue,
			"reason",
			"message",
			MatchFields(IgnoreExtras, Fields{
				"Status":             Equal(resourcesv1alpha1.ConditionTrue),
				"Reason":             Equal("reason"),
				"Message":            Equal("message"),
				"LastTransitionTime": Equal(zeroTime),
				"LastUpdateTime":     Equal(zeroTime),
			}),
		),
		Entry("update reason",
			resourcesv1alpha1.ManagedResourceCondition{
				Status:  resourcesv1alpha1.ConditionTrue,
				Reason:  "reason",
				Message: "message",
			},
			resourcesv1alpha1.ConditionTrue,
			"OtherReason",
			"message",
			MatchFields(IgnoreExtras, Fields{
				"Status":             Equal(resourcesv1alpha1.ConditionTrue),
				"Reason":             Equal("OtherReason"),
				"Message":            Equal("message"),
				"LastTransitionTime": Equal(zeroTime),
				"LastUpdateTime":     Not(Equal(zeroTime)),
			}),
		),
		Entry("update message",
			resourcesv1alpha1.ManagedResourceCondition{
				Status:  resourcesv1alpha1.ConditionTrue,
				Reason:  "reason",
				Message: "message",
			},
			resourcesv1alpha1.ConditionTrue,
			"reason",
			"OtherMessage",
			MatchFields(IgnoreExtras, Fields{
				"Status":             Equal(resourcesv1alpha1.ConditionTrue),
				"Reason":             Equal("reason"),
				"Message":            Equal("OtherMessage"),
				"LastTransitionTime": Equal(zeroTime),
				"LastUpdateTime":     Not(Equal(zeroTime)),
			}),
		),
		Entry("update status",
			resourcesv1alpha1.ManagedResourceCondition{
				Status:  resourcesv1alpha1.ConditionTrue,
				Reason:  "reason",
				Message: "message",
			},
			resourcesv1alpha1.ConditionFalse,
			"OtherReason",
			"message",
			MatchFields(IgnoreExtras, Fields{
				"Status":             Equal(resourcesv1alpha1.ConditionFalse),
				"Reason":             Equal("OtherReason"),
				"Message":            Equal("message"),
				"LastTransitionTime": Not(Equal(zeroTime)),
				"LastUpdateTime":     Not(Equal(zeroTime)),
			}),
		),
	)

	Describe("#MergeConditions", func() {
		It("should merge the conditions", func() {
			var (
				typeFoo resourcesv1alpha1.ConditionType = "foo"
				typeBar resourcesv1alpha1.ConditionType = "bar"
			)

			oldConditions := []resourcesv1alpha1.ManagedResourceCondition{
				{
					Type:   typeFoo,
					Reason: "hugo",
				},
			}

			result := helper.MergeConditions(oldConditions, resourcesv1alpha1.ManagedResourceCondition{Type: typeFoo}, resourcesv1alpha1.ManagedResourceCondition{Type: typeBar})

			Expect(result).To(Equal([]resourcesv1alpha1.ManagedResourceCondition{{Type: typeFoo}, {Type: typeBar}}))
		})
	})

	Describe("#GetCondition", func() {
		It("should return the found condition", func() {
			var (
				conditionType resourcesv1alpha1.ConditionType = "test-1"
				condition                                     = resourcesv1alpha1.ManagedResourceCondition{
					Type: conditionType,
				}
				conditions = []resourcesv1alpha1.ManagedResourceCondition{condition}
			)

			cond := helper.GetCondition(conditions, conditionType)

			Expect(cond).NotTo(BeNil())
			Expect(*cond).To(Equal(condition))
		})

		It("should return nil because the required condition could not be found", func() {
			var (
				conditionType resourcesv1alpha1.ConditionType = "test-1"
				conditions                                    = []resourcesv1alpha1.ManagedResourceCondition{}
			)

			cond := helper.GetCondition(conditions, conditionType)

			Expect(cond).To(BeNil())
		})
	})

	Describe("#GetOrInitCondition", func() {
		It("should get the existing condition", func() {
			var (
				c          = resourcesv1alpha1.ManagedResourceCondition{Type: "foo"}
				conditions = []resourcesv1alpha1.ManagedResourceCondition{c}
			)

			Expect(helper.GetOrInitCondition(conditions, "foo")).To(Equal(c))
		})

		It("should return a new, initialized condition", func() {
			tmp := helper.Now
			helper.Now = func() metav1.Time {
				return metav1.NewTime(time.Unix(0, 0))
			}
			defer func() { helper.Now = tmp }()

			Expect(helper.GetOrInitCondition(nil, "foo")).To(Equal(helper.InitCondition("foo")))
		})
	})
})
