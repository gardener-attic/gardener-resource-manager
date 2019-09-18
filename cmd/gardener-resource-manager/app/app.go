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
	"time"

	resourcesv1alpha1 "github.com/gardener/gardener-resource-manager/pkg/apis/resources/v1alpha1"
	"github.com/gardener/gardener-resource-manager/pkg/controller/managedresources"
	"github.com/gardener/gardener-resource-manager/pkg/controller/managedresources/health"

	"github.com/spf13/cobra"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/types"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/discovery"
	memcache "k8s.io/client-go/discovery/cached/memory"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/restmapper"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/util/workqueue"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/config"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	logf "sigs.k8s.io/controller-runtime/pkg/runtime/log"
	"sigs.k8s.io/controller-runtime/pkg/source"
)

var log = logf.Log.WithName("gardener-resource-manager")

// NewControllerManagerCommand creates a new command for running a gardener resource manager controllers.
func NewControllerManagerCommand(ctx context.Context) *cobra.Command {
	logf.SetLogger(logf.ZapLogger(false))
	entryLog := log.WithName("entrypoint")

	var (
		leaderElection             bool
		leaderElectionNamespace    string
		syncPeriod                 time.Duration
		maxConcurrentWorkers       int
		healthSyncPeriod           time.Duration
		healthMaxConcurrentWorkers int
		targetKubeconfigPath       string
		kubeconfigPath             string
		namespace                  string
		resourceClass              string
	)

	cmd := &cobra.Command{
		Use: "gardener-resource-manager",

		Run: func(cmd *cobra.Command, args []string) {
			var cfg *rest.Config
			var err error
			if kubeconfigPath != "" {
				cfg, err = clientcmd.BuildConfigFromFlags("", kubeconfigPath)
				if err != nil {
					entryLog.Error(err, "could not instantiate rest config")
					os.Exit(1)
				}
				entryLog.Info("using kubeconfig " + kubeconfigPath)
			} else {
				cfg = config.GetConfigOrDie()
			}
			mgr, err := manager.New(cfg, manager.Options{
				LeaderElection:          leaderElection,
				LeaderElectionID:        "gardener-resource-manager",
				LeaderElectionNamespace: leaderElectionNamespace,
				SyncPeriod:              &syncPeriod,
				Namespace:               namespace,
			})
			if err != nil {
				entryLog.Error(err, "could not instantiate manager")
				os.Exit(1)
			}

			utilruntime.Must(resourcesv1alpha1.AddToScheme(mgr.GetScheme()))

			targetConfig, err := getTargetConfig(targetKubeconfigPath)
			if err != nil {
				entryLog.Error(err, "unable to create REST config for target cluster")
				os.Exit(1)
			}

			targetRESTMapper, err := getTargetRESTMapper(targetConfig)
			if err != nil {
				entryLog.Error(err, "unable to create REST mapper for target cluster")
				os.Exit(1)
			}

			targetClient, err := getTargetClient(*targetConfig, targetRESTMapper)
			if err != nil {
				entryLog.Error(err, "unable to create client for target cluster")
				os.Exit(1)
			}

			if resourceClass == "" {
				resourceClass = managedresources.DefaultClass
			}
			filter := managedresources.NewClassFilter(resourceClass)
			c, err := controller.New("resource-controller", mgr, controller.Options{
				MaxConcurrentReconciles: maxConcurrentWorkers,
				Reconciler: managedresources.NewReconciler(
					ctx,
					log.WithName("reconciler"),
					mgr.GetClient(),
					targetClient,
					targetRESTMapper,
					filter,
				),
			})
			if err != nil {
				entryLog.Error(err, "unable to set up individual controller")
				os.Exit(1)
			}

			if err := c.Watch(
				&source.Kind{Type: &resourcesv1alpha1.ManagedResource{}},
				&handler.EnqueueRequestForObject{},
				filter, managedresources.GenerationChangedPredicate(),
			); err != nil {
				entryLog.Error(err, "unable to watch ManagedResources")
				os.Exit(1)
			}
			if err := c.Watch(
				&source.Kind{Type: &corev1.Secret{}},
				&handler.EnqueueRequestsFromMapFunc{ToRequests: managedresources.SecretToManagedResourceMapper(mgr.GetClient(), filter)},
			); err != nil {
				entryLog.Error(err, "unable to watch Secrets mapping to ManagedResources")
				os.Exit(1)
			}

			entryLog.Info("Managed namespace: " + namespace)
			entryLog.Info("Resource class: " + filter.ResourceClass())
			entryLog.Info("Managed resource controller", "syncPeriod", syncPeriod.String())
			entryLog.Info("Managed resource controller", "maxConcurrentWorkers", maxConcurrentWorkers)

			healthController, err := controller.New("health-controller", mgr, controller.Options{
				MaxConcurrentReconciles: healthMaxConcurrentWorkers,
				Reconciler: health.NewHealthReconciler(
					ctx,
					log.WithName("health-reconciler"),
					mgr.GetClient(),
					targetClient,
					filter,
					healthSyncPeriod,
				),
			})
			if err != nil {
				entryLog.Error(err, "unable to set up individual controller")
				os.Exit(1)
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
				filter, health.ClassChangedPredicate(),
			); err != nil {
				entryLog.Error(err, "unable to watch ManagedResources")
				os.Exit(1)
			}

			entryLog.Info("Managed resource health controller", "syncPeriod", healthSyncPeriod.String())
			entryLog.Info("Managed resource health controller", "maxConcurrentWorkers", healthMaxConcurrentWorkers)

			if err := mgr.Start(ctx.Done()); err != nil {
				entryLog.Error(err, "error running manager")
				os.Exit(1)
			}
		},
	}

	cmd.Flags().BoolVar(&leaderElection, "leader-election", true, "enable or disable leader election")
	cmd.Flags().StringVar(&leaderElectionNamespace, "leader-election-namespace", "", "namespace for leader election")
	cmd.Flags().DurationVar(&syncPeriod, "sync-period", time.Minute, "duration how often existing resources should be synced")
	cmd.Flags().IntVar(&maxConcurrentWorkers, "max-concurrent-workers", 10, "number of worker threads for concurrent reconciliation of resources")
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

func getTargetClient(config rest.Config, restMapper meta.RESTMapper) (client.Client, error) {
	config.QPS = 100.0
	config.Burst = 130

	return client.New(&config, client.Options{
		Mapper: restMapper,
	})
}
