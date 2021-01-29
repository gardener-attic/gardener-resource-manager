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

package mapper

import (
	resourcesv1alpha1 "github.com/gardener/gardener-resource-manager/api/resources/v1alpha1"
	extensionshandler "github.com/gardener/gardener/extensions/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

type managedResourceToSecretsMapper struct{}

func (m *managedResourceToSecretsMapper) Map(obj client.Object) []reconcile.Request {
	if obj == nil {
		return nil
	}

	resource, ok := obj.(*resourcesv1alpha1.ManagedResource)
	if !ok {
		return nil
	}

	var requests []reconcile.Request

	for _, ref := range resource.Spec.SecretRefs {
		if ref.Name != "" {
			requests = append(requests, reconcile.Request{
				NamespacedName: types.NamespacedName{
					Name:      ref.Name,
					Namespace: resource.Namespace,
				},
			})
		}
	}

	return requests
}

// ManagedResourceToSecretsMapper returns a mapper that maps events for ManagedResources to their referenced secrets.
func ManagedResourceToSecretsMapper() extensionshandler.Mapper {
	return &managedResourceToSecretsMapper{}
}
