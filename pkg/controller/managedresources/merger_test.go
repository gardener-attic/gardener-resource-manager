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

package managedresources

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/intstr"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/ginkgo/extensions/table"
	. "github.com/onsi/gomega"
)

var _ = Describe("#MergeService", func() {
	var (
		old, new, expected *corev1.Service
		s                  *runtime.Scheme
	)

	BeforeEach(func() {
		s = runtime.NewScheme()
		Expect(corev1.AddToScheme(s)).ToNot(HaveOccurred(), "schema add should succeed")

		old = &corev1.Service{
			ObjectMeta: metav1.ObjectMeta{},
			Spec: corev1.ServiceSpec{
				ClusterIP: "1.2.3.4",
				Ports: []corev1.ServicePort{
					{
						Name:       "foo",
						Protocol:   corev1.ProtocolTCP,
						Port:       123,
						TargetPort: intstr.FromInt(919),
					},
				},
				Type:            corev1.ServiceTypeClusterIP,
				SessionAffinity: corev1.ServiceAffinityNone,
				Selector:        map[string]string{"foo": "bar"},
			},
		}

		new = old.DeepCopy()
		expected = old.DeepCopy()
	})

	DescribeTable("ClusterIP to", func(mutator func()) {
		mutator()
		Expect(mergeService(s, old, new)).NotTo(HaveOccurred(), "merge should be successful")
		Expect(new).To(Equal(expected))
	},
		Entry("ClusterIP with changed ports", func() {
			new.Spec.Ports[0].Port = 1234
			new.Spec.Ports[0].Protocol = corev1.ProtocolUDP
			new.Spec.Ports[0].TargetPort = intstr.FromInt(989)

			expected = new.DeepCopy()
			new.Spec.ClusterIP = ""
		}),
		Entry("ClusterIP with changed ClusterIP, should not update it", func() {
			new.Spec.ClusterIP = "5.6.7.8"
		}),
		Entry("Headless ClusterIP", func() {
			new.Spec.ClusterIP = "None"
			expected.Spec.ClusterIP = "None"
		}),
		Entry("ClusterIP without passing any type", func() {
			new.Spec.Ports[0].Protocol = corev1.ProtocolUDP
			new.Spec.Ports[0].Port = 999
			new.Spec.Ports[0].TargetPort = intstr.FromInt(888)

			expected = new.DeepCopy()
			new.Spec.ClusterIP = "5.6.7.8"
			new.Spec.Type = ""
		}),
		Entry("NodePort with changed ports", func() {
			new.Spec.Type = corev1.ServiceTypeNodePort
			new.Spec.Ports[0].Protocol = corev1.ProtocolUDP
			new.Spec.Ports[0].Port = 999
			new.Spec.Ports[0].TargetPort = intstr.FromInt(888)
			new.Spec.Ports[0].NodePort = 444

			expected = new.DeepCopy()
		}),

		Entry("ExternalName removes ClusterIP", func() {
			new.Spec.Type = corev1.ServiceTypeExternalName
			new.Spec.Selector = nil
			new.Spec.Ports[0].Protocol = corev1.ProtocolUDP
			new.Spec.Ports[0].Port = 999
			new.Spec.Ports[0].TargetPort = intstr.FromInt(888)
			new.Spec.Ports[0].NodePort = 0
			new.Spec.ClusterIP = ""
			new.Spec.ExternalName = "foo.com"
			new.Spec.HealthCheckNodePort = 0

			expected = new.DeepCopy()
		}),
	)

	DescribeTable("NodePort to",
		func(mutator func()) {
			old.Spec.Ports[0].NodePort = 3333
			old.Spec.Type = corev1.ServiceTypeNodePort

			new = old.DeepCopy()
			expected = old.DeepCopy()

			mutator()

			Expect(mergeService(s, old, new)).NotTo(HaveOccurred(), "merge should be successful")
			Expect(new).To(Equal(expected))
		},
		Entry("ClusterIP with changed ports", func() {
			new.Spec.Type = corev1.ServiceTypeClusterIP
			new.Spec.Ports[0].Protocol = corev1.ProtocolUDP
			new.Spec.Ports[0].Port = 999
			new.Spec.Ports[0].TargetPort = intstr.FromInt(888)
			new.Spec.Ports[0].NodePort = 0

			expected = new.DeepCopy()
		}),
		Entry("ClusterIP with changed ClusterIP", func() {
			new.Spec.ClusterIP = "5.6.7.8"
		}),
		Entry("Headless ClusterIP type service", func() {
			new.Spec.Type = corev1.ServiceTypeClusterIP
			new.Spec.ClusterIP = "None"

			expected = new.DeepCopy()
		}),

		Entry("NodePort with changed ports", func() {
			new.Spec.Ports[0].Protocol = corev1.ProtocolUDP
			new.Spec.Ports[0].Port = 999
			new.Spec.Ports[0].TargetPort = intstr.FromInt(888)
			new.Spec.Ports[0].NodePort = 444

			expected = new.DeepCopy()
		}),
		Entry("NodePort with changed ports and without nodePort", func() {
			new.Spec.Ports[0].Protocol = corev1.ProtocolUDP
			new.Spec.Ports[0].Port = 999
			new.Spec.Ports[0].TargetPort = intstr.FromInt(888)

			expected = new.DeepCopy()
			new.Spec.Ports[0].NodePort = 0
		}),
		Entry("ExternalName removes ClusterIP", func() {
			new.Spec.Type = corev1.ServiceTypeExternalName
			new.Spec.Selector = nil
			new.Spec.Ports[0].Protocol = corev1.ProtocolUDP
			new.Spec.Ports[0].Port = 999
			new.Spec.Ports[0].TargetPort = intstr.FromInt(888)
			new.Spec.Ports[0].NodePort = 0
			new.Spec.ClusterIP = ""
			new.Spec.ExternalName = "foo.com"
			new.Spec.HealthCheckNodePort = 0

			expected = new.DeepCopy()
		}),
	)

	DescribeTable("LoadBalancer to", func(mutator func()) {
		old.Spec.Ports[0].NodePort = 3333
		old.Spec.Type = corev1.ServiceTypeLoadBalancer

		new = old.DeepCopy()
		expected = old.DeepCopy()

		mutator()

		Expect(mergeService(s, old, new)).NotTo(HaveOccurred(), "merge should be successful")
		Expect(new).To(Equal(expected))
	},
		Entry("ClusterIP with changed ports", func() {
			new.Spec.Type = corev1.ServiceTypeClusterIP
			new.Spec.Ports[0].Protocol = corev1.ProtocolUDP
			new.Spec.Ports[0].Port = 999
			new.Spec.Ports[0].TargetPort = intstr.FromInt(888)
			new.Spec.Ports[0].NodePort = 0

			expected = new.DeepCopy()
		}),
		Entry("Cluster with ClusterIP changed", func() {
			new.Spec.ClusterIP = "5.6.7.8"
		}),
		Entry("Headless ClusterIP type service", func() {
			new.Spec.Type = corev1.ServiceTypeClusterIP
			new.Spec.ClusterIP = "None"

			expected = new.DeepCopy()
		}),
		Entry("NodePort with changed ports", func() {
			new.Spec.Ports[0].Protocol = corev1.ProtocolUDP
			new.Spec.Ports[0].Port = 999
			new.Spec.Ports[0].TargetPort = intstr.FromInt(888)
			new.Spec.Ports[0].NodePort = 444

			expected = new.DeepCopy()
		}),
		Entry("NodePort with changed ports and without nodePort", func() {
			new.Spec.Ports[0].Protocol = corev1.ProtocolUDP
			new.Spec.Ports[0].Port = 999
			new.Spec.Ports[0].TargetPort = intstr.FromInt(888)

			expected = new.DeepCopy()
			new.Spec.Ports[0].NodePort = 0
		}),
		Entry("ExternalName removes ClusterIP", func() {
			new.Spec.Type = corev1.ServiceTypeExternalName
			new.Spec.Selector = nil
			new.Spec.Ports[0].Protocol = corev1.ProtocolUDP
			new.Spec.Ports[0].Port = 999
			new.Spec.Ports[0].TargetPort = intstr.FromInt(888)
			new.Spec.Ports[0].NodePort = 0
			new.Spec.ClusterIP = ""
			new.Spec.ExternalName = "foo.com"
			new.Spec.HealthCheckNodePort = 0

			expected = new.DeepCopy()
		}),
		Entry("LoadBalancer with ExternalTrafficPolicy=Local and HealthCheckNodePort", func() {
			new.Spec.HealthCheckNodePort = 123
			new.Spec.ExternalTrafficPolicy = corev1.ServiceExternalTrafficPolicyTypeLocal

			expected = new.DeepCopy()
		}),
		Entry("LoadBalancer with ExternalTrafficPolicy=Local and no HealthCheckNodePort", func() {
			old.Spec.ExternalTrafficPolicy = corev1.ServiceExternalTrafficPolicyTypeLocal
			old.Spec.HealthCheckNodePort = 3333

			new.Spec.HealthCheckNodePort = 0
			new.Spec.ExternalTrafficPolicy = corev1.ServiceExternalTrafficPolicyTypeLocal

			expected = old.DeepCopy()
		}),
	)

	DescribeTable("ExternalName to", func(mutator func()) {
		old.Spec.Ports[0].NodePort = 0
		old.Spec.Type = corev1.ServiceTypeExternalName
		old.Spec.HealthCheckNodePort = 0
		old.Spec.ClusterIP = ""
		old.Spec.ExternalName = "baz.bar"
		old.Spec.Selector = nil

		new = old.DeepCopy()
		expected = old.DeepCopy()

		mutator()

		Expect(mergeService(s, old, new)).NotTo(HaveOccurred(), "merge should be successful")
		Expect(new).To(Equal(expected))
	},
		Entry("ClusterIP with changed ports", func() {
			new.Spec.Type = corev1.ServiceTypeClusterIP
			new.Spec.Ports[0].Protocol = corev1.ProtocolUDP
			new.Spec.Ports[0].Port = 999
			new.Spec.Ports[0].TargetPort = intstr.FromInt(888)
			new.Spec.Ports[0].NodePort = 0
			new.Spec.ExternalName = ""
			new.Spec.ClusterIP = "3.4.5.6"

			expected = new.DeepCopy()
		}),
		Entry("NodePort with changed ports", func() {
			new.Spec.Type = corev1.ServiceTypeNodePort
			new.Spec.Ports[0].Protocol = corev1.ProtocolUDP
			new.Spec.Ports[0].Port = 999
			new.Spec.Ports[0].TargetPort = intstr.FromInt(888)
			new.Spec.Ports[0].NodePort = 444
			new.Spec.ExternalName = ""
			new.Spec.ClusterIP = "3.4.5.6"

			expected = new.DeepCopy()
		}),
		Entry("LoadBalancer with ExternalTrafficPolicy=Local and HealthCheckNodePort", func() {
			new.Spec.Type = corev1.ServiceTypeLoadBalancer
			new.Spec.HealthCheckNodePort = 123
			new.Spec.ExternalTrafficPolicy = corev1.ServiceExternalTrafficPolicyTypeLocal
			new.Spec.ExternalName = ""
			new.Spec.ClusterIP = "3.4.5.6"

			expected = new.DeepCopy()
		}),
	)

})
