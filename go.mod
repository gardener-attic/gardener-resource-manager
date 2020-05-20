module github.com/gardener/gardener-resource-manager

go 1.13

require (
	github.com/gardener/gardener v1.4.1-0.20200519155656-a8ccc6cc779a
	github.com/gardener/hvpa-controller v0.2.5
	github.com/go-logr/logr v0.1.0
	github.com/golang/mock v1.4.3
	github.com/hashicorp/go-multierror v1.0.0
	github.com/onsi/ginkgo v1.10.1
	github.com/onsi/gomega v1.7.0
	github.com/spf13/cobra v0.0.6
	github.com/spf13/pflag v1.0.5
	go.uber.org/zap v1.13.0
	k8s.io/api v0.17.0 // kubernetes-1.16.0
	k8s.io/apiextensions-apiserver v0.17.0 // kubernetes-1.16.0
	k8s.io/apimachinery v0.17.0 // kubernetes-1.16.0
	k8s.io/autoscaler v0.0.0-20190805135949-100e91ba756e
	k8s.io/client-go v11.0.1-0.20190409021438-1a26190bd76a+incompatible // kubernetes-1.16.0
	k8s.io/code-generator v0.17.0 // kubernetes-1.16.0
	k8s.io/kube-openapi v0.0.0-20191107075043-30be4d16710a
	k8s.io/utils v0.0.0-20200327001022-6496210b90e8
	sigs.k8s.io/controller-runtime v0.4.0
)

replace (
	k8s.io/api => k8s.io/api v0.16.8 // 1.16.8
	k8s.io/apiextensions-apiserver => k8s.io/apiextensions-apiserver v0.16.8 // 1.16.8
	k8s.io/apimachinery => k8s.io/apimachinery v0.16.8 // 1.16.8
	k8s.io/client-go => k8s.io/client-go v0.16.8 // 1.16.8
	k8s.io/code-generator => k8s.io/code-generator v0.16.8 // 1.16.8
)
