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

package app

import (
	"context"
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"sync"
	"time"

	resourcesv1alpha1 "github.com/gardener/gardener-resource-manager/pkg/apis/resources/v1alpha1"
	"github.com/gardener/gardener-resource-manager/pkg/controller/managedresources"
	"github.com/gardener/gardener-resource-manager/pkg/controller/managedresources/health"
	logpkg "github.com/gardener/gardener-resource-manager/pkg/log"
	"github.com/gardener/gardener-resource-manager/pkg/mapper"
	managerpredicate "github.com/gardener/gardener-resource-manager/pkg/predicate"

	hvpav1alpha1 "github.com/gardener/hvpa-controller/api/v1alpha1"
	extensionspredicate "github.com/gardener/gardener/extensions/pkg/predicate"
	extensionscontroller "github.com/gardener/gardener/extensions/pkg/controller"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	corev1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apiextensionsv1beta1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1beta1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/discovery"
	memcache "k8s.io/client-go/discovery/cached/memory"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/restmapper"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/util/workqueue"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/config"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	runtimelog "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"
)

var log = runtimelog.Log.WithName("gardener-resource-manager")

// NewControllerManagerCommand creates a new command for running a gardener resource manager controllers.
func NewControllerManagerCommand(parentCtx context.Context) *cobra.Command {
	runtimelog.SetLogger(logpkg.ZapLogger(false))
	entryLog := log.WithName("entrypoint")

	var (
		leaderElection             bool
		leaderElectionNamespace    string
		cacheResyncPeriod          time.Duration
		targetCacheResyncPeriod    time.Duration
		syncPeriod                 time.Duration
		maxConcurrentWorkers       int
		secretMaxConcurrentWorkers int
		healthSyncPeriod           time.Duration
		healthMaxConcurrentWorkers int
		targetKubeconfigPath       string
		kubeconfigPath             string
		namespace                  string
		resourceClass              string
	)

	cmd := &cobra.Command{
		Use: "gardener-resource-manager",

		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, cancel := context.WithCancel(parentCtx)
			defer cancel()

			entryLog.Info("Starting gardener-resource-manager...")
			cmd.Flags().VisitAll(func(flag *pflag.Flag) {
				entryLog.Info(fmt.Sprintf("FLAG: --%s=%s", flag.Name, flag.Value))
			})

			var cfg *rest.Config
			var err error
			if kubeconfigPath != "" {
				cfg, err = clientcmd.BuildConfigFromFlags("", kubeconfigPath)
				if err != nil {
					return fmt.Errorf("could not instantiate rest config: %+v", err)
				}
				entryLog.Info("using kubeconfig " + kubeconfigPath)
			} else {
				cfg = config.GetConfigOrDie()
			}

			mgr, err := manager.New(cfg, manager.Options{
				LeaderElection:          leaderElection,
				LeaderElectionID:        "gardener-resource-manager",
				LeaderElectionNamespace: leaderElectionNamespace,
				SyncPeriod:              &cacheResyncPeriod,
				Namespace:               namespace,
			})
			if err != nil {
				return fmt.Errorf("could not instantiate manager: %+v", err)
			}

			utilruntime.Must(resourcesv1alpha1.AddToScheme(mgr.GetScheme()))

			targetScheme := runtime.NewScheme()
			utilruntime.Must(scheme.AddToScheme(targetScheme)) // add most of the standard k8s APIs
			utilruntime.Must(apiextensionsv1beta1.AddToScheme(targetScheme))
			utilruntime.Must(apiextensionsv1.AddToScheme(targetScheme))
			utilruntime.Must(hvpav1alpha1.AddToScheme(targetScheme))

			targetConfig, err := getTargetConfig(targetKubeconfigPath)
			if err != nil {
				return fmt.Errorf("unable to create REST config for target cluster: %+v", err)
			}

			targetRESTMapper, err := getTargetRESTMapper(targetConfig)
			if err != nil {
				return fmt.Errorf("unable to create REST mapper for target cluster: %+v", err)
			}

			targetCache, err := getTargetCache(targetConfig, cache.Options{
				Mapper: targetRESTMapper,
				Resync: &targetCacheResyncPeriod,
				Scheme: targetScheme,
			})
			if err != nil {
				return fmt.Errorf("unable to create client cache for target cluster: %+v", err)
			}

			targetClient, err := getTargetClient(targetCache, *targetConfig, client.Options{
				Mapper: targetRESTMapper,
				Scheme: targetScheme,
			})
			if err != nil {
				return fmt.Errorf("unable to create client for target cluster: %+v", err)
			}

			if resourceClass == "" {
				resourceClass = managedresources.DefaultClass
			}
			filter := managedresources.NewClassFilter(resourceClass)

			entryLog.Info("Managed namespace: " + namespace)
			entryLog.Info("Resource class: " + filter.ResourceClass())
			entryLog.Info("Cache resync period " + cacheResyncPeriod.String())

			c, err := controller.New("resource-controller", mgr, controller.Options{
				MaxConcurrentReconciles: maxConcurrentWorkers,
				Reconciler: extensionscontroller.OperationAnnotationWrapper(
					&resourcesv1alpha1.ManagedResource{},
					managedresources.NewReconciler(
					ctx,
					log.WithName("reconciler"),
					mgr.GetClient(),
					targetClient,
					targetRESTMapper,
					targetScheme,
					filter,
					syncPeriod,
					),
				),
			})
			if err != nil {
				return fmt.Errorf("unable to set up individual controller: %+v", err)
			}

			if err := c.Watch(
				&source.Kind{Type: &resourcesv1alpha1.ManagedResource{}},
				&handler.EnqueueRequestForObject{},
				filter, extensionspredicate.Or(predicate.GenerationChangedPredicate{}, extensionspredicate.HasOperationAnnotation()),
			); err != nil {
				return fmt.Errorf("unable to watch ManagedResources: %+v", err)
			}
			if err := c.Watch(
				&source.Kind{Type: &corev1.Secret{}},
				&handler.EnqueueRequestsFromMapFunc{ToRequests: mapper.SecretToManagedResourceMapper(filter)},
			); err != nil {
				return fmt.Errorf("unable to watch Secrets mapping to ManagedResources: %+v", err)
			}

			entryLog.Info("Managed resource controller", "syncPeriod", syncPeriod.String())
			entryLog.Info("Managed resource controller", "maxConcurrentWorkers", maxConcurrentWorkers)

			secretController, err := controller.New("secret-controller", mgr, controller.Options{
				MaxConcurrentReconciles: secretMaxConcurrentWorkers,
				Reconciler: managedresources.NewSecretReconciler(
					log.WithName("secret-reconciler"),
					filter,
				),
			})
			if err != nil {
				return fmt.Errorf("unable to set up secret controller: %+v", err)
			}

			if err := secretController.Watch(
				&source.Kind{Type: &resourcesv1alpha1.ManagedResource{}},
				&handler.EnqueueRequestsFromMapFunc{ToRequests: mapper.ManagedResourceToSecretsMapper()},
				predicate.GenerationChangedPredicate{},
			); err != nil {
				return fmt.Errorf("unable to watch ManagedResources: %+v", err)
			}

			// Also watch secrets to ensure, that we properly remove the finalizer in case we missed an important
			// update event for a ManagedResource during downtime.
			if err := secretController.Watch(
				&source.Kind{Type: &corev1.Secret{}},
				&handler.EnqueueRequestForObject{},
				// Only requeue secrets from create/update events with the controller's finalizer to not flood the controller
				// with too many unnecessary requests for all secrets in cluster/namespace.
				managerpredicate.HasFinalizer(filter.FinalizerName()),
			); err != nil {
				return fmt.Errorf("unable to watch Secrets: %+v", err)
			}

			healthController, err := controller.New("health-controller", mgr, controller.Options{
				MaxConcurrentReconciles: healthMaxConcurrentWorkers,
				Reconciler: health.NewHealthReconciler(
					ctx,
					log.WithName("health-reconciler"),
					mgr.GetClient(),
					targetClient,
					targetScheme,
					filter,
					healthSyncPeriod,
				),
			})
			if err != nil {
				return fmt.Errorf("unable to set up individual controller: %+v", err)
			}

			if err := healthController.Watch(
				&source.Kind{Type: &resourcesv1alpha1.ManagedResource{}},
				&handler.Funcs{
					CreateFunc: func(e event.CreateEvent, q workqueue.RateLimitingInterface) {
						q.Add(reconcile.Request{NamespacedName: types.NamespacedName{
							Name:      e.Meta.GetName(),
							Namespace: e.Meta.GetNamespace(),
						}})
					},
					UpdateFunc: func(e event.UpdateEvent, q workqueue.RateLimitingInterface) {
						q.Add(reconcile.Request{NamespacedName: types.NamespacedName{
							Name:      e.MetaNew.GetName(),
							Namespace: e.MetaNew.GetNamespace(),
						}})
					},
				},
				filter, managerpredicate.Or(
					managerpredicate.ClassChangedPredicate(),
					// start health checks immediately after MR has been reconciled
					managerpredicate.ConditionStatusChanged(resourcesv1alpha1.ResourcesApplied),
				),
			); err != nil {
				return fmt.Errorf("unable to watch ManagedResources: %+v", err)
			}

			entryLog.Info("Managed resource health controller", "syncPeriod", healthSyncPeriod.String())
			entryLog.Info("Managed resource health controller", "maxConcurrentWorkers", healthMaxConcurrentWorkers)

			var wg sync.WaitGroup
			errChan := make(chan error)

			// start the target cache and exit if there was an error
			go func() {
				defer wg.Done()

				wg.Add(1)
				if err := targetCache.Start(ctx.Done()); err != nil {
					errChan <- fmt.Errorf("error syncing target cache: %+v", err)
				}
			}()

			targetCache.WaitForCacheSync(ctx.Done())

			go func() {
				defer wg.Done()

				wg.Add(1)
				if err := mgr.Start(ctx.Done()); err != nil {
					errChan <- fmt.Errorf("error running manager: %+v", err)
				}
			}()

			select {
			case err := <-errChan:
				cancel()
				wg.Wait()
				return err

			case <-parentCtx.Done():
				wg.Wait()
				entryLog.Info("Stop signal received, shutting down.")
				return nil
			}
		},
	}

	cmd.Flags().BoolVar(&leaderElection, "leader-election", true, "enable or disable leader election")
	cmd.Flags().StringVar(&leaderElectionNamespace, "leader-election-namespace", "", "namespace for leader election")
	cmd.Flags().DurationVar(&cacheResyncPeriod, "cache-resync-period", 24*time.Hour, "duration how often the controller's cache is resynced")
	cmd.Flags().DurationVar(&syncPeriod, "sync-period", time.Minute, "duration how often existing resources should be synced")
	cmd.Flags().DurationVar(&targetCacheResyncPeriod, "target-cache-resync-period", 24*time.Hour, "duration how often the controller's cache for the target cluster is resynced")
	cmd.Flags().IntVar(&maxConcurrentWorkers, "max-concurrent-workers", 10, "number of worker threads for concurrent reconciliation of resources")
	cmd.Flags().IntVar(&secretMaxConcurrentWorkers, "secret-max-concurrent-workers", 5, "number of worker threads for concurrent secret reconciliation of resources")
	cmd.Flags().DurationVar(&healthSyncPeriod, "health-sync-period", time.Minute, "duration how often the health of existing resources should be synced")
	cmd.Flags().IntVar(&healthMaxConcurrentWorkers, "health-max-concurrent-workers", 10, "number of worker threads for concurrent health reconciliation of resources")
	cmd.Flags().StringVar(&kubeconfigPath, "kubeconfig", "", "path to the kubeconfig for the source cluster")
	cmd.Flags().StringVar(&targetKubeconfigPath, "target-kubeconfig", "", "path to the kubeconfig for the target cluster")
	cmd.Flags().StringVar(&namespace, "namespace", "", "namespace in which the ManagedResources should be observed (defaults to all namespaces)")
	cmd.Flags().StringVar(&resourceClass, "resource-class", managedresources.DefaultClass, "resource class used to filter resource resources")

	return cmd
}

func getTargetRESTMapper(config *rest.Config) (*restmapper.DeferredDiscoveryRESTMapper, error) {
	targetDiscoveryClient, err := discovery.NewDiscoveryClientForConfig(config)
	if err != nil {
		return nil, err
	}
	return restmapper.NewDeferredDiscoveryRESTMapper(memcache.NewMemCacheClient(targetDiscoveryClient)), nil
}

func getTargetConfig(kubeconfigPath string) (*rest.Config, error) {
	if len(kubeconfigPath) > 0 {
		return clientcmd.BuildConfigFromFlags("", kubeconfigPath)
	}
	if kubeconfig := os.Getenv("KUBECONFIG"); len(kubeconfig) > 0 {
		return clientcmd.BuildConfigFromFlags("", kubeconfig)
	}
	if c, err := rest.InClusterConfig(); err == nil {
		return c, nil
	}
	if usr, err := user.Current(); err == nil {
		if c, err := clientcmd.BuildConfigFromFlags("", filepath.Join(usr.HomeDir, ".kube", "config")); err == nil {
			return c, nil
		}
	}
	return nil, fmt.Errorf("could not create config for cluster")
}

func getTargetCache(config *rest.Config, options cache.Options) (cache.Cache, error) {
	return cache.New(config, options)
}

func getTargetClient(cache cache.Cache, config rest.Config, options client.Options) (client.Client, error) {
	config.QPS = 100.0
	config.Burst = 130

	// Create the Client for Write operations.
	c, err := client.New(&config, options)
	if err != nil {
		return nil, err
	}

	return &client.DelegatingClient{
		Reader: &client.DelegatingReader{
			CacheReader:  cache,
			ClientReader: c,
		},
		Writer:       c,
		StatusClient: c,
	}, nil
}
