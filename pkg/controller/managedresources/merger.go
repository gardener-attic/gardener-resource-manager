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
	extensionsv1beta1 "k8s.io/api/extensions/v1beta1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/scheme"
)

// merge merges the values of the `desired` object into the `current` object while preserving `current`'s important
// metadata (like resourceVersion and finalizers), status and selected spec fields of the respective kind (e.g.
// .spec.selector of a Job).
func merge(desired, current *unstructured.Unstructured, forceOverwriteLabels bool, existingLabels map[string]string, forceOverwriteAnnotations bool, existingAnnotations map[string]string, preserveReplicas, preserveResources bool) error {
	// save copy of current object before merging
	oldObject := current.DeepCopy()

	// copy desired state into new object
	newObject := current
	desired.DeepCopyInto(newObject)

	newObject.SetResourceVersion(oldObject.GetResourceVersion())
	newObject.SetFinalizers(oldObject.GetFinalizers())

	if forceOverwriteLabels {
		newObject.SetLabels(desired.GetLabels())
	} else {
		newObject.SetLabels(mergeMapsBasedOnOldMap(desired.GetLabels(), oldObject.GetLabels(), existingLabels))
	}
	if forceOverwriteAnnotations {
		newObject.SetAnnotations(desired.GetAnnotations())
	} else {
		newObject.SetAnnotations(mergeMapsBasedOnOldMap(desired.GetAnnotations(), oldObject.GetAnnotations(), existingAnnotations))
	}

	// keep status of old object if it is set and not empty
	var oldStatus map[string]interface{}
	if oldStatusInterface, containsStatus := oldObject.Object["status"]; containsStatus {
		// cast to map to be able to check if status is empty
		if oldStatusMap, ok := oldStatusInterface.(map[string]interface{}); ok {
			oldStatus = oldStatusMap
		}
	}

	if len(oldStatus) > 0 {
		newObject.Object["status"] = oldStatus
	} else {
		delete(newObject.Object, "status")
	}

	switch newObject.GroupVersionKind().GroupKind() {
	case appsv1.SchemeGroupVersion.WithKind("Deployment").GroupKind(), extensionsv1beta1.SchemeGroupVersion.WithKind("Deployment").GroupKind():
		return mergeDeployment(scheme.Scheme, oldObject, newObject, preserveReplicas, preserveResources)
	case batchv1.SchemeGroupVersion.WithKind("Job").GroupKind():
		return mergeJob(scheme.Scheme, oldObject, newObject)
	case appsv1.SchemeGroupVersion.WithKind("StatefulSet").GroupKind(), extensionsv1beta1.SchemeGroupVersion.WithKind("StatefulSet").GroupKind():
		return mergeStatefulSet(scheme.Scheme, oldObject, newObject, preserveReplicas, preserveResources)
	case corev1.SchemeGroupVersion.WithKind("Service").GroupKind():
		return mergeService(scheme.Scheme, oldObject, newObject)
	case corev1.SchemeGroupVersion.WithKind("ServiceAccount").GroupKind():
		return mergeServiceAccount(scheme.Scheme, oldObject, newObject)
	}

	return nil
}

func mergeDeployment(scheme *runtime.Scheme, oldObj, newObj runtime.Object, preserveReplicas, preserveResources bool) error {
	oldDeployment := &appsv1.Deployment{}
	if err := scheme.Convert(oldObj, oldDeployment, nil); err != nil {
		return err
	}

	newDeployment := &appsv1.Deployment{}
	if err := scheme.Convert(newObj, newDeployment, nil); err != nil {
		return err
	}

	// Do not overwrite a Deployment's '.spec.replicas' if the new Deployment's '.spec.replicas'
	// field is unset or the Deployment is scaled by either an HPA or HVPA.
	if newDeployment.Spec.Replicas == nil || preserveReplicas {
		newDeployment.Spec.Replicas = oldDeployment.Spec.Replicas
	}

	mergePodTemplate(&oldDeployment.Spec.Template, &newDeployment.Spec.Template, preserveResources)

	return scheme.Convert(newDeployment, newObj, nil)
}

func mergePodTemplate(oldPod, newPod *corev1.PodTemplateSpec, preserveResources bool) {
	if !preserveResources {
		return
	}

	// Do not overwrite a PodTemplate's resource requests / limits if it is scaled by an HVPA
	for _, newContainer := range newPod.Spec.Containers {
		newContainerName := newContainer.Name

		for _, oldContainer := range oldPod.Spec.Containers {
			oldContainerName := oldContainer.Name

			if newContainerName == oldContainerName {
				mergeContainer(&oldContainer, &newContainer, preserveResources)
			}
		}
	}
}

func mergeContainer(oldContainer, newContainer *corev1.Container, preserveResources bool) {
	if !preserveResources {
		return
	}

	for resourceName, oldRequests := range oldContainer.Resources.Requests {
		switch resourceName {
		case corev1.ResourceCPU, corev1.ResourceMemory:
			if newContainer.Resources.Requests == nil {
				newContainer.Resources.Requests = corev1.ResourceList{}
			}

			newContainer.Resources.Requests[resourceName] = oldRequests
		}
	}

	for resourceName, oldLimits := range oldContainer.Resources.Limits {
		switch resourceName {
		case corev1.ResourceCPU, corev1.ResourceMemory:
			if newContainer.Resources.Limits == nil {
				newContainer.Resources.Limits = corev1.ResourceList{}
			}

			newContainer.Resources.Limits[resourceName] = oldLimits
		}
	}
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

func mergeStatefulSet(scheme *runtime.Scheme, oldObj, newObj runtime.Object, preserveReplicas, preserveResources bool) error {
	oldStatefulSet := &appsv1.StatefulSet{}
	if err := scheme.Convert(oldObj, oldStatefulSet, nil); err != nil {
		return err
	}

	newStatefulSet := &appsv1.StatefulSet{}
	if err := scheme.Convert(newObj, newStatefulSet, nil); err != nil {
		return err
	}

	// Do not overwrite a StatefulSet's '.spec.replicas' if the new StatefulSet's `.spec.replicas'
	// field is unset or the Deployment is scaled by either an HPA or HVPA.
	if newStatefulSet.Spec.Replicas == nil || preserveReplicas {
		newStatefulSet.Spec.Replicas = oldStatefulSet.Spec.Replicas
	}

	mergePodTemplate(&oldStatefulSet.Spec.Template, &oldStatefulSet.Spec.Template, preserveResources)

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
		var ports []corev1.ServicePort

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
