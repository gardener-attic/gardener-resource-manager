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

package managedresources

import (
	"github.com/gardener/gardener-resource-manager/pkg/apis/resources/v1alpha1"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	v1 "k8s.io/api/core/v1"
	schema "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var _ = Describe("Resource Equivalences", func() {

	Describe("Setup", func() {
		It("single set ", func() {
			equis := equivalences{
				[]schema.GroupKind{
					{Group: "groupA", Kind: "kindA"},
					{Group: "groupB", Kind: "kindB"},
					{Group: "groupC", Kind: "kindC"},
				},
			}

			set := GroupKindSet{}.Insert(equis[0]...)
			index := NewObjectIndex(nil, equis)

			Expect(index.GetEquivalencesFor(equis[0][0])).To(Equal(set))
			Expect(index.GetEquivalencesFor(equis[0][1])).To(Equal(set))
			Expect(index.GetEquivalencesFor(equis[0][2])).To(Equal(set))
		})
		It("multiple sets", func() {
			equis := equivalences{
				[]schema.GroupKind{
					{Group: "groupA", Kind: "kindA"},
					{Group: "groupB", Kind: "kindB"},
					{Group: "groupC", Kind: "kindC"},
				},
				[]schema.GroupKind{
					{Group: "groupA1", Kind: "kindA1"},
					{Group: "groupB1", Kind: "kindB1"},
					{Group: "groupC1", Kind: "kindC1"},
				},
			}

			set0 := GroupKindSet{}.Insert(equis[0]...)
			set1 := GroupKindSet{}.Insert(equis[1]...)
			index := NewObjectIndex(nil, equis)

			Expect(index.GetEquivalencesFor(equis[0][0])).To(Equal(set0))
			Expect(index.GetEquivalencesFor(equis[0][1])).To(Equal(set0))
			Expect(index.GetEquivalencesFor(equis[0][2])).To(Equal(set0))

			Expect(index.GetEquivalencesFor(equis[1][0])).To(Equal(set1))
			Expect(index.GetEquivalencesFor(equis[1][1])).To(Equal(set1))
			Expect(index.GetEquivalencesFor(equis[1][2])).To(Equal(set1))
		})

		It("mixed sets", func() {
			equis := equivalences{
				[]schema.GroupKind{
					{Group: "groupA", Kind: "kindA"},
					{Group: "groupB", Kind: "kindB"},
				},
				[]schema.GroupKind{
					{Group: "groupB", Kind: "kindB"},
					{Group: "groupC", Kind: "kindC"},
				},
			}

			set := GroupKindSet{}.Insert(equis[0]...).Insert(equis[1]...)
			index := NewObjectIndex(nil, equis)

			Expect(index.GetEquivalencesFor(equis[0][0])).To(Equal(set))
			Expect(index.GetEquivalencesFor(equis[0][1])).To(Equal(set))

			Expect(index.GetEquivalencesFor(equis[1][0])).To(Equal(set))
			Expect(index.GetEquivalencesFor(equis[1][1])).To(Equal(set))
		})
	})
	Describe("Lookup", func() {
		It("single", func() {
			equis := equivalences{
				[]schema.GroupKind{
					{Group: "groupA", Kind: "kindA"},
					{Group: "groupB", Kind: "kindB"},
					{Group: "groupC", Kind: "kindC"},
				},
			}

			refold := v1alpha1.ObjectReference{
				ObjectReference: v1.ObjectReference{Name: "name", Namespace: "ns", Kind: "kindA", APIVersion: "groupA/v2"},
			}

			refunused := v1alpha1.ObjectReference{
				ObjectReference: v1.ObjectReference{Name: "foo", Namespace: "bar", Kind: "kind", APIVersion: "group/v1"},
			}
			old := []v1alpha1.ObjectReference{
				refold,
				refunused,
			}

			index := NewObjectIndex(old, equis)

			refnew := v1alpha1.ObjectReference{
				ObjectReference: v1.ObjectReference{Name: "name", Namespace: "ns", Kind: "kindB", APIVersion: "groupB/v1"},
			}

			found, _ := index.Lookup(refnew)
			Expect(found).To(Equal(refold))
			Expect(index.Found(refold)).To(BeTrue())
			Expect(index.Found(refunused)).To(BeFalse())
		})
	})
})
