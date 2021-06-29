module github.com/gardener/gardener-resource-manager

go 1.15

require (
	github.com/Masterminds/goutils v1.1.1 // indirect
	github.com/gardener/gardener v1.25.1
	github.com/gardener/gardener-resource-manager/api v0.0.0-00010101000000-000000000000
	github.com/gardener/hvpa-controller v0.3.1
	github.com/go-logr/logr v0.3.0
	github.com/golang/mock v1.6.0
	github.com/hashicorp/go-multierror v1.1.0
	github.com/kubernetes-csi/external-snapshotter/v2 v2.1.4
	github.com/mitchellh/copystructure v1.1.1 // indirect
	github.com/onsi/ginkgo v1.14.2
	github.com/onsi/gomega v1.10.5
	github.com/spf13/cobra v1.1.1
	github.com/spf13/pflag v1.0.5
	go.uber.org/zap v1.15.0
	golang.org/x/time v0.0.0-20200630173020-3af7569d3a1e
	k8s.io/api v0.20.7
	k8s.io/apiextensions-apiserver v0.20.7
	k8s.io/apimachinery v0.20.7
	k8s.io/autoscaler v0.0.0-20190805135949-100e91ba756e
	k8s.io/client-go v11.0.1-0.20190409021438-1a26190bd76a+incompatible
	k8s.io/code-generator v0.20.7
	k8s.io/kube-aggregator v0.20.7
	k8s.io/kube-openapi v0.0.0-20201113171705-d219536bb9fd
	k8s.io/utils v0.0.0-20210111153108-fddb29f9d009
	sigs.k8s.io/controller-runtime v0.8.3
)

replace (
	github.com/gardener/gardener-resource-manager/api => ./api
	k8s.io/client-go => k8s.io/client-go v0.20.7
)
