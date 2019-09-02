module github.com/gardener/gardener-resource-manager

go 1.13

require (
	github.com/gardener/gardener v0.0.0-20190910145521-61b1a94eccba
	github.com/go-logr/logr v0.1.0
	github.com/go-logr/zapr v0.1.1
	github.com/golang/mock v1.3.1
	github.com/onsi/ginkgo v1.8.0
	github.com/onsi/gomega v1.5.0
	github.com/spf13/cobra v0.0.5
	go.uber.org/zap v1.10.0
	k8s.io/api v0.0.0-20190409021203-6e4e0e4f393b
	k8s.io/apiextensions-apiserver v0.0.0-20190409022649-727a075fdec8
	k8s.io/apimachinery v0.0.0-20190404173353-6a84e37a896d
	k8s.io/client-go v11.0.1-0.20190409021438-1a26190bd76a+incompatible
	k8s.io/code-generator v0.0.0-20190713022532-93d7507fc8ff
	k8s.io/gengo v0.0.0-20190826232639-a874a240740c // indirect
	k8s.io/kube-openapi v0.0.0-20190722073852-5e22f3d471e6
	sigs.k8s.io/controller-runtime v0.2.0-beta.2
)

replace (
	// TODO: Fix for https://github.com/Azure/go-autorest/issues/449.
	// Remove after revendoring of github.com/gardener/gardener.
	github.com/census-instrumentation/opencensus-proto v0.1.0-0.20181214143942-ba49f56771b8 => github.com/census-instrumentation/opencensus-proto v0.0.3-0.20181214143942-ba49f56771b8
	k8s.io/api => k8s.io/api v0.0.0-20190313235455-40a48860b5ab // kubernetes-1.14.0
	k8s.io/apimachinery => k8s.io/apimachinery v0.0.0-20190313205120-d7deff9243b1 // kubernetes-1.14.0
	k8s.io/client-go => k8s.io/client-go v11.0.0+incompatible // kubernetes-1.14.0
	k8s.io/code-generator => k8s.io/code-generator v0.0.0-20190311093542-50b561225d70 // kubernetes-1.14.0
	k8s.io/kube-openapi => k8s.io/kube-openapi v0.0.0-20190320154901-5e45bb682580
	sigs.k8s.io/controller-runtime => sigs.k8s.io/controller-runtime v0.2.0-beta.2
)
