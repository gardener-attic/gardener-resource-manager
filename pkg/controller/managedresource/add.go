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

package managedresource

import (
	"fmt"
	"time"

	"sigs.k8s.io/controller-runtime/pkg/client"

	extensionscontroller "github.com/gardener/gardener/extensions/pkg/controller"
	extensionspredicate "github.com/gardener/gardener/extensions/pkg/predicate"
	"github.com/spf13/pflag"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/source"

	resourcesv1alpha1 "github.com/gardener/gardener-resource-manager/api/resources/v1alpha1"
	resourcemanagercmd "github.com/gardener/gardener-resource-manager/pkg/cmd"
	"github.com/gardener/gardener-resource-manager/pkg/filter"
	"github.com/gardener/gardener-resource-manager/pkg/mapper"
	managerpredicate "github.com/gardener/gardener-resource-manager/pkg/predicate"
	extensionshandler "github.com/gardener/gardener/extensions/pkg/handler"
)

// ControllerName is the name of the managedresource controller.
const ControllerName = "resource-controller"

// defaultControllerConfig is the default config for the controller.
var defaultControllerConfig ControllerConfig

// ControllerOptions are options for adding the controller to a Manager.
type ControllerOptions struct {
	maxConcurrentWorkers int
	syncPeriod           time.Duration
	resourceClass        string
	alwaysUpdate         bool
}

// ControllerConfig is the completed configuration for the controller.
type ControllerConfig struct {
	MaxConcurrentWorkers int
	SyncPeriod           time.Duration
	ClassFilter          *filter.ClassFilter
	AlwaysUpdate         bool

	TargetClientConfig resourcemanagercmd.TargetClientConfig
}

// AddToManagerWithOptions adds the controller to a Manager with the given config.
func AddToManagerWithOptions(mgr manager.Manager, conf ControllerConfig) error {
	c, err := controller.New(ControllerName, mgr, controller.Options{
		MaxConcurrentReconciles: conf.MaxConcurrentWorkers,
		Reconciler: extensionscontroller.OperationAnnotationWrapper(
			func() client.Object { return &resourcesv1alpha1.ManagedResource{} },
			&Reconciler{
				targetClient:     conf.TargetClientConfig.Client,
				targetRESTMapper: conf.TargetClientConfig.RESTMapper,
				targetScheme:     conf.TargetClientConfig.Scheme,
				class:            conf.ClassFilter,
				alwaysUpdate:     conf.AlwaysUpdate,
				syncPeriod:       conf.SyncPeriod,
			},
		),
	})
	if err != nil {
		return fmt.Errorf("unable to set up managedresource controller: %w", err)
	}

	if err := c.Watch(
		&source.Kind{Type: &resourcesv1alpha1.ManagedResource{}},
		&handler.EnqueueRequestForObject{},
		conf.ClassFilter, predicate.Or(
			predicate.GenerationChangedPredicate{},
			extensionspredicate.HasOperationAnnotation(),
			managerpredicate.ConditionStatusChanged(resourcesv1alpha1.ResourcesHealthy, managerpredicate.ConditionChangedToUnhealthy),
		),
	); err != nil {
		return fmt.Errorf("unable to watch ManagedResources: %w", err)
	}

	if err := c.Watch(
		&source.Kind{Type: &corev1.Secret{}},
		extensionshandler.EnqueueRequestsFromMapper(mapper.SecretToManagedResourceMapper(conf.ClassFilter), extensionshandler.UpdateWithOldAndNew),
	); err != nil {
		return fmt.Errorf("unable to watch Secrets mapping to ManagedResources: %w", err)
	}
	return nil
}

// AddToManagerWithOptions adds the controller to a Manager using the default config.
func AddToManager(mgr manager.Manager) error {
	return AddToManagerWithOptions(mgr, defaultControllerConfig)
}

// AddFlags adds the needed command line flags to the given FlagSet.
func (o *ControllerOptions) AddFlags(fs *pflag.FlagSet) {
	fs.IntVar(&o.maxConcurrentWorkers, "max-concurrent-workers", 10, "number of worker threads for concurrent reconciliation of resources")
	fs.DurationVar(&o.syncPeriod, "sync-period", time.Minute, "duration how often existing resources should be synced")
	fs.StringVar(&o.resourceClass, "resource-class", filter.DefaultClass, "resource class used to filter resource resources")
	fs.BoolVar(&o.alwaysUpdate, "always-update", false, "if set to false then a resource will only be updated if its desired state differs from the actual state. otherwise, an update request will be always sent.")
}

// Complete completes the given command line flags and set the defaultControllerConfig accordingly.
func (o *ControllerOptions) Complete() error {
	if o.resourceClass == "" {
		o.resourceClass = filter.DefaultClass
	}

	defaultControllerConfig = ControllerConfig{
		MaxConcurrentWorkers: o.maxConcurrentWorkers,
		SyncPeriod:           o.syncPeriod,
		ClassFilter:          filter.NewClassFilter(o.resourceClass),
		AlwaysUpdate:         o.alwaysUpdate,
	}
	return nil
}

// Completed returns the completed ControllerConfig.
func (o *ControllerOptions) Completed() *ControllerConfig {
	return &defaultControllerConfig
}

// ApplyClassFilter sets filter to the ClassFilter of this config.
func (c *ControllerConfig) ApplyClassFilter(filter *filter.ClassFilter) {
	*filter = *c.ClassFilter
}
