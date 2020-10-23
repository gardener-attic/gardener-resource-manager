module github.com/gardener/gardener-resource-manager

go 1.13

require (
	github.com/gardener/gardener v1.11.3
	github.com/gardener/hvpa-controller v0.3.1
	github.com/go-logr/logr v0.1.0
	github.com/golang/mock v1.4.4-0.20200731163441-8734ec565a4d
	github.com/hashicorp/go-multierror v1.0.0
	github.com/onsi/ginkgo v1.14.0
	github.com/onsi/gomega v1.10.1
	github.com/spf13/cobra v0.0.6
	github.com/spf13/pflag v1.0.5
	go.uber.org/zap v1.13.0
	k8s.io/api v0.18.10
	k8s.io/apiextensions-apiserver v0.18.10
	k8s.io/apimachinery v0.18.10
	k8s.io/autoscaler v0.0.0-20190805135949-100e91ba756e
	k8s.io/client-go v11.0.1-0.20190409021438-1a26190bd76a+incompatible
	k8s.io/code-generator v0.18.10
	k8s.io/kube-aggregator v0.18.10
	k8s.io/kube-openapi v0.0.0-20200410145947-bcb3869e6f29
	k8s.io/utils v0.0.0-20200619165400-6e3d28b6ed19
	sigs.k8s.io/controller-runtime v0.6.3
)

replace k8s.io/client-go => k8s.io/client-go v0.18.10
