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

package resources

import (
	gardencore "github.com/gardener/gardener/pkg/apis/core"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// ManagedResource describes a list of managed resources.
type ManagedResource struct {
	metav1.TypeMeta
	// Standard object metadata.
	metav1.ObjectMeta
	// Spec contains the specification of this managed resource.
	Spec ManagedResourceSpec
	// Status contains the status of this managed resource.
	Status ManagedResourceStatus
}

type ManagedResourceSpec struct {
	// SecretRefs is a list of secret references.
	SecretRefs []corev1.LocalObjectReference
}

// ManagedResourceStatus is the status of a managed resource.
type ManagedResourceStatus struct {
	// Conditions represents the latest available observations of a ControllerInstallations's current state.
	Conditions []gardencore.Condition
}
