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

package managedresources

import (
	"strings"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
)

// AnnotationClass is used to annotate a resource class
const AnnotationClass = "resources.gardener.cloud/class"

// DefaultClass is used a resource class is no class is specified on the command line
const DefaultClass = "resources"

// NoClass indicates no specfied class for the recover mode
const NoResourceClass = "-"

// ClassFilter keeps the resource class for the actual controller instance
// and is used as Filter predicate for events finally passed to the controller
type ClassFilter struct {
	resourceClass string

	finalizer string
}

var _ predicate.Predicate = &ClassFilter{}

func NewClassFilter(class string) *ClassFilter {
	if class == "" {
		class = DefaultClass
	}

	finalizer := FinalizerName + "-" + class
	if class == DefaultClass {
		finalizer = FinalizerName
	}

	return &ClassFilter{
		resourceClass: class,
		finalizer:     finalizer,
	}
}

// ResourceClass returns the actually configured resource class
func (f *ClassFilter) ResourceClass() string {
	return f.resourceClass
}

// FinalizerName determines the finalizer name to be used for the actual resource class
func (f *ClassFilter) FinalizerName() string {
	return f.finalizer
}

// Responsible checks whether an object should be managed by the actual controller instance
func (f *ClassFilter) Responsible(o metav1.Object) bool {
	c := ""
	annos := o.GetAnnotations()
	if annos != nil {
		c = annos[AnnotationClass]
	}
	return c == f.resourceClass || (c == "" && f.resourceClass == DefaultClass)
}

// Active checks whether a dedicated object must be handled by the actual controller
// instance. This is split into two conditions. An object must be handled
// if it has already been handled, indicated by the actual finalizer, or
// if the actual controller is responsible for the object.
func (f *ClassFilter) Active(o metav1.Object) (action bool, responsible bool) {
	busy := false
	responsible = f.Responsible(o)

	for _, finalizer := range o.GetFinalizers() {
		if strings.HasPrefix(finalizer, FinalizerName) {
			busy = true
			if finalizer == f.finalizer {
				action = true
				return
			}
		}
	}
	action = (!busy && responsible)
	return
}

func (f *ClassFilter) Create(e event.CreateEvent) bool {
	a, r := f.Active(e.Meta)
	return a || r
}

func (f *ClassFilter) Delete(e event.DeleteEvent) bool {
	a, r := f.Active(e.Meta)
	return a || r
}

func (f *ClassFilter) Update(e event.UpdateEvent) bool {
	a, r := f.Active(e.MetaNew)
	return a || r
}

func (f *ClassFilter) Generic(e event.GenericEvent) bool {
	a, r := f.Active(e.Meta)
	return a || r
}
