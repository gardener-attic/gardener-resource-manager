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
	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/scheme"
)

func merge(desired, current *unstructured.Unstructured, forceOverwriteLabels bool, existingLabels map[string]string, forceOverwriteAnnotations bool, existingAnnotations map[string]string) error {
	currentCopy := current.DeepCopy()

	desired.DeepCopyInto(current)
	current.SetResourceVersion(currentCopy.GetResourceVersion())
	current.SetFinalizers(currentCopy.GetFinalizers())
	if forceOverwriteLabels {
		current.SetLabels(desired.GetLabels())
	} else {
		current.SetLabels(mergeMapsBasedOnOldMap(desired.GetLabels(), currentCopy.GetLabels(), existingLabels))
	}
	if forceOverwriteAnnotations {
		current.SetAnnotations(desired.GetAnnotations())
	} else {
		current.SetAnnotations(mergeMapsBasedOnOldMap(desired.GetAnnotations(), currentCopy.GetAnnotations(), existingAnnotations))
	}

	switch current.GroupVersionKind().GroupKind() {
	case appsv1.SchemeGroupVersion.WithKind("Deployment").GroupKind():
		return mergeDeployment(scheme.Scheme, currentCopy, current)
	case batchv1.SchemeGroupVersion.WithKind("Job").GroupKind():
		return mergeJob(scheme.Scheme, currentCopy, current)
	case appsv1.SchemeGroupVersion.WithKind("StatefulSet").GroupKind():
		return mergeStatefulSet(scheme.Scheme, currentCopy, current)
	case corev1.SchemeGroupVersion.WithKind("Service").GroupKind():
		return mergeService(scheme.Scheme, currentCopy, current)
	case corev1.SchemeGroupVersion.WithKind("ServiceAccount").GroupKind():
		return mergeServiceAccount(scheme.Scheme, currentCopy, current)
	}

	return nil
}

func mergeDeployment(scheme *runtime.Scheme, oldObj, newObj runtime.Object) error {
	oldDeployment := &appsv1.Deployment{}
	if err := scheme.Convert(oldObj, oldDeployment, nil); err != nil {
		return err
	}

	newDeployment := &appsv1.Deployment{}
	if err := scheme.Convert(newObj, newDeployment, nil); err != nil {
		return err
	}

	// Do not overwrite a Deployment's '.spec.replicas' if the new Deployment's '.spec.replicas'
	// field is unset.
	if newDeployment.Spec.Replicas == nil {
		newDeployment.Spec.Replicas = oldDeployment.Spec.Replicas
	}

	return scheme.Convert(newDeployment, newObj, nil)
}

func mergeJob(scheme *runtime.Scheme, oldObj, newObj runtime.Object) error {
	oldJob := &batchv1.Job{}
	if err := scheme.Convert(oldObj, oldJob, nil); err != nil {
		return err
	}

	newJob := &batchv1.Job{}
	if err := scheme.Convert(newObj, newJob, nil); err != nil {
		return err
	}

	// Do not overwrite a Job's '.spec.selector' if the new Jobs's '.spec.selector'
	// field is unset.
	if newJob.Spec.Selector == nil && oldJob.Spec.Selector != nil {
		newJob.Spec.Selector = oldJob.Spec.Selector
	}

	// Do not overwrite Job managed labels as 'controller-uid' and 'job-name'. '.spec.template' is immutable.
	newJob.Spec.Template.Labels = labels.Merge(oldJob.Spec.Template.Labels, newJob.Spec.Template.Labels)

	return scheme.Convert(newJob, newObj, nil)
}

func mergeStatefulSet(scheme *runtime.Scheme, oldObj, newObj runtime.Object) error {
	oldStatefulSet := &appsv1.StatefulSet{}
	if err := scheme.Convert(oldObj, oldStatefulSet, nil); err != nil {
		return err
	}

	newStatefulSet := &appsv1.StatefulSet{}
	if err := scheme.Convert(newObj, newStatefulSet, nil); err != nil {
		return err
	}

	// Do not overwrite a StatefulSet's '.spec.replicas' if the new StatefulSet's `.spec.replicas'
	// field is unset.
	if newStatefulSet.Spec.Replicas == nil {
		newStatefulSet.Spec.Replicas = oldStatefulSet.Spec.Replicas
	}

	return scheme.Convert(newStatefulSet, newObj, nil)
}

// mergeService merges new service into old service
func mergeService(scheme *runtime.Scheme, oldObj, newObj runtime.Object) error {
	oldService := &corev1.Service{}
	if err := scheme.Convert(oldObj, oldService, nil); err != nil {
		return err
	}

	if oldService.Spec.Type == "" {
		oldService.Spec.Type = corev1.ServiceTypeClusterIP
	}

	newService := &corev1.Service{}
	if err := scheme.Convert(newObj, newService, nil); err != nil {
		return err
	}

	if newService.Spec.Type == "" {
		newService.Spec.Type = corev1.ServiceTypeClusterIP
	}

	switch newService.Spec.Type {
	case corev1.ServiceTypeLoadBalancer, corev1.ServiceTypeNodePort:
		ports := []corev1.ServicePort{}

		for _, np := range newService.Spec.Ports {
			p := np

			for _, op := range oldService.Spec.Ports {
				if (np.Port == op.Port || np.Name == op.Name) && np.NodePort == 0 {
					p.NodePort = op.NodePort
				}
			}

			ports = append(ports, p)
		}
		newService.Spec.Ports = ports

	case corev1.ServiceTypeExternalName:
		// there is no ClusterIP in this case
		return scheme.Convert(newService, newObj, nil)
	}

	// ClusterIP is immutable unless we want to transform the service into headless
	// where ClusterIP = None or if the previous type of the service was ExternalName
	// and the user wants to explicitly set an ClusterIP.
	if newService.Spec.ClusterIP != corev1.ClusterIPNone &&
		oldService.Spec.Type != corev1.ServiceTypeExternalName {
		newService.Spec.ClusterIP = oldService.Spec.ClusterIP
	}

	if oldService.Spec.Type == corev1.ServiceTypeLoadBalancer &&
		newService.Spec.Type == corev1.ServiceTypeLoadBalancer &&
		newService.Spec.ExternalTrafficPolicy == corev1.ServiceExternalTrafficPolicyTypeLocal &&
		oldService.Spec.ExternalTrafficPolicy == corev1.ServiceExternalTrafficPolicyTypeLocal &&
		newService.Spec.HealthCheckNodePort == 0 {
		newService.Spec.HealthCheckNodePort = oldService.Spec.HealthCheckNodePort
	}

	return scheme.Convert(newService, newObj, nil)
}

func mergeServiceAccount(scheme *runtime.Scheme, oldObj, newObj runtime.Object) error {
	oldServiceAccount := &corev1.ServiceAccount{}
	if err := scheme.Convert(oldObj, oldServiceAccount, nil); err != nil {
		return err
	}

	newServiceAccount := &corev1.ServiceAccount{}
	if err := scheme.Convert(newObj, newServiceAccount, nil); err != nil {
		return err
	}

	// Do not overwrite a ServiceAccount's '.secrets[]' list or '.imagePullSecrets[]'.
	newServiceAccount.Secrets = oldServiceAccount.Secrets
	newServiceAccount.ImagePullSecrets = oldServiceAccount.ImagePullSecrets

	return scheme.Convert(newServiceAccount, newObj, nil)
}

func mergeMapsBasedOnOldMap(desired, current, old map[string]string) map[string]string {
	out := map[string]string{}
	for k, v := range desired {
		out[k] = v
	}

	for k, v := range current {
		oldValue, ok := old[k]
		desiredValue, ok2 := desired[k]

		if ok && oldValue == v && (!ok2 || desiredValue != v) {
			continue
		}

		out[k] = v
	}

	return out
}
