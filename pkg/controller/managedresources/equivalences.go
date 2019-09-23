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
	resourcesv1alpha1 "github.com/gardener/gardener-resource-manager/pkg/apis/resources/v1alpha1"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/sets"
)

type equivalences [][]metav1.GroupKind

var defaultEquivalences = equivalences{
	EquiSetForKind("Deployment", "extensions", "apps"),
	EquiSetForKind("DaemonSet", "extensions", "apps"),
	EquiSetForKind("ReplicaSet", "extensions", "apps"),
	EquiSetForKind("StatefulSet", "extensions", "apps"),
	EquiSetForKind("Ingress", "extensions", "networking.k8s.io"),
	EquiSetForKind("NetworkPolicy", "extensions", "networking.k8s.io"),
	EquiSetForKind("PodSecurityPolicy", "extensions", "policy"),
}

type GroupKindSet map[metav1.GroupKind]struct{}

func (s GroupKindSet) Insert(gks ...metav1.GroupKind) GroupKindSet {
	for _, gk := range gks {
		s[gk] = struct{}{}
	}
	return s

}
func (s GroupKindSet) Delete(gks ...metav1.GroupKind) GroupKindSet {
	for _, gk := range gks {
		delete(s, gk)
	}
	return s
}

type ObjectIndex struct {
	index        map[string]resourcesv1alpha1.ObjectReference
	found        sets.String
	equivalences map[metav1.GroupKind]GroupKindSet
}

func NewObjectIndex(resources []resourcesv1alpha1.ObjectReference, equis equivalences) *ObjectIndex {
	index := &ObjectIndex{
		make(map[string]resourcesv1alpha1.ObjectReference, len(resources)),
		sets.String{},
		map[metav1.GroupKind]GroupKindSet{},
	}

	for _, r := range resources {
		index.index[objectKeyByReference(r)] = r
	}
	index.addEquivalences(defaultEquivalences)
	index.addEquivalences(equis)
	return index
}

func (i *ObjectIndex) Objects() map[string]resourcesv1alpha1.ObjectReference {
	return i.index
}

func (i *ObjectIndex) addEquivalences(equis equivalences) {
	for _, equi := range equis {
		var m GroupKindSet
		for _, e := range equi {
			if f, ok := i.equivalences[e]; ok {
				m = f
				break
			}
		}
		if m == nil {
			m = map[metav1.GroupKind]struct{}{}
		}
		for _, e := range equi {
			m.Insert(e)
			i.equivalences[e] = m
		}
	}
}

func (i ObjectIndex) GetEquivalencesFor(gk metav1.GroupKind) GroupKindSet {
	return i.equivalences[gk]
}

func (i ObjectIndex) Found(ref resourcesv1alpha1.ObjectReference) bool {
	return i.found.Has(objectKeyByReference(ref))
}

func (i ObjectIndex) Lookup(ref resourcesv1alpha1.ObjectReference) (resourcesv1alpha1.ObjectReference, bool) {
	key := objectKeyByReference(ref)
	if found, ok := i.index[key]; ok {
		i.found.Insert(key)
		return found, ok
	}
	gk := metav1.GroupKind{
		Group: ref.GroupVersionKind().Group,
		Kind:  ref.Kind,
	}
	equis, ok := i.equivalences[gk]
	if ok {
		for e := range equis {
			key = objectKey(e.Group, e.Kind, ref.Namespace, ref.Name)
			if found, ok := i.index[key]; ok {
				i.found.Insert(key)
				return found, ok
			}
		}
	}
	return resourcesv1alpha1.ObjectReference{}, false
}

////////////////////////////////////////////////////////////////////////////////

func EquiSetForKind(kind string, groups ...string) []metav1.GroupKind {
	r := []metav1.GroupKind{}

	for _, g := range groups {
		r = append(r, metav1.GroupKind{
			Group: g,
			Kind:  kind,
		})
	}
	return r
}
