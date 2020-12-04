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
	"flag"
	"fmt"
	"sync"

	"github.com/go-logr/logr"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	runtimelog "sigs.k8s.io/controller-runtime/pkg/log"
	logzap "sigs.k8s.io/controller-runtime/pkg/log/zap"
	"sigs.k8s.io/controller-runtime/pkg/manager"

	resourcemanagercmd "github.com/gardener/gardener-resource-manager/pkg/cmd"
	healthcontroller "github.com/gardener/gardener-resource-manager/pkg/controller/health"
	resourcecontroller "github.com/gardener/gardener-resource-manager/pkg/controller/managedresource"
	secretcontroller "github.com/gardener/gardener-resource-manager/pkg/controller/secret"
	"github.com/gardener/gardener-resource-manager/pkg/healthz"
	"github.com/gardener/gardener-resource-manager/pkg/version"
)

// NewResourceManagerCommand creates a new command for running a gardener resource manager controllers.
func NewResourceManagerCommand() *cobra.Command {
	var (
		zapOpts = &logzap.Options{}

		managerOpts      = &resourcemanagercmd.ManagerOptions{}
		sourceClientOpts = &resourcemanagercmd.SourceClientOptions{}
		targetClientOpts = &resourcemanagercmd.TargetClientOptions{}

		resourceControllerOpts = &resourcecontroller.ControllerOptions{}
		secretControllerOpts   = &secretcontroller.ControllerOptions{}
		healthControllerOpts   = &healthcontroller.ControllerOptions{}

		log logr.Logger
	)

	cmd := &cobra.Command{
		Use:     "gardener-resource-manager",
		Version: version.Get().GitVersion,

		PersistentPreRun: func(cmd *cobra.Command, args []string) {
			runtimelog.SetLogger(logzap.New(
				// use configuration passed via flags
				logzap.UseFlagOptions(zapOpts),
				// and overwrite some stuff
				func(o *logzap.Options) {
					if !o.Development {
						encCfg := zap.NewProductionEncoderConfig()
						// overwrite time encoding to human readable format for production logs
						encCfg.EncodeTime = zapcore.ISO8601TimeEncoder
						o.Encoder = zapcore.NewJSONEncoder(encCfg)
					}

					// don't print stacktrace for warning level logs
					o.StacktraceLevel = zapcore.ErrorLevel
				},
			))

			log = runtimelog.Log
			log.Info("Starting gardener-resource-manager...", "version", version.Get().GitVersion)
			cmd.Flags().VisitAll(func(flag *pflag.Flag) {
				log.Info(fmt.Sprintf("FLAG: --%s=%s", flag.Name, flag.Value))
			})

			cmd.SilenceUsage = true
			cmd.SilenceErrors = true
		},

		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, cancel := context.WithCancel(cmd.Context())
			defer cancel()

			if err := resourcemanagercmd.CompleteAll(
				managerOpts,
				sourceClientOpts,
				targetClientOpts,
				resourceControllerOpts,
				secretControllerOpts,
				healthControllerOpts,
			); err != nil {
				return err
			}

			var managerOptions manager.Options
			healthz.DefaultAddOptions.Ctx = ctx

			managerOpts.Completed().Apply(&managerOptions)
			sourceClientOpts.Completed().ApplyManagerOptions(&managerOptions)
			sourceClientOpts.Completed().ApplyClientSet(&healthz.DefaultAddOptions.ClientSet)
			targetClientOpts.Completed().Apply(&resourceControllerOpts.Completed().TargetClientConfig)
			targetClientOpts.Completed().Apply(&healthControllerOpts.Completed().TargetClientConfig)
			resourceControllerOpts.Completed().ApplyClassFilter(&secretControllerOpts.Completed().ClassFilter)
			resourceControllerOpts.Completed().ApplyClassFilter(&healthControllerOpts.Completed().ClassFilter)

			// setup manager
			mgr, err := manager.New(sourceClientOpts.Completed().RESTConfig, managerOptions)
			if err != nil {
				return fmt.Errorf("could not instantiate manager: %w", err)
			}

			// add controllers and health endpoint to manager
			if err := resourcemanagercmd.AddAllToManager(
				mgr,
				resourcecontroller.AddToManager,
				secretcontroller.AddToManager,
				healthcontroller.AddToManager,
				healthz.AddToManager,
			); err != nil {
				return err
			}

			// start the target cache and exit if there was an error
			var wg sync.WaitGroup
			errChan := make(chan error)

			go func() {
				defer wg.Done()

				wg.Add(1)
				if err := targetClientOpts.Completed().Start(ctx.Done()); err != nil {
					errChan <- fmt.Errorf("error syncing target cache: %w", err)
				}
			}()

			if !targetClientOpts.Completed().WaitForCacheSync(ctx.Done()) {
				return fmt.Errorf("timed out waiting for target cache to sync")
			}

			go func() {
				defer wg.Done()

				wg.Add(1)
				if err := mgr.Start(ctx.Done()); err != nil {
					errChan <- fmt.Errorf("error running manager: %w", err)
				}
			}()

			select {
			case err := <-errChan:
				cancel()
				wg.Wait()
				return err

			case <-cmd.Context().Done():
				log.Info("Stop signal received, shutting down.")
				wg.Wait()
				return nil
			}
		},
	}

	resourcemanagercmd.AddAllFlags(
		cmd.Flags(),
		managerOpts,
		targetClientOpts,
		sourceClientOpts,
		resourceControllerOpts,
		secretControllerOpts,
		healthControllerOpts,
	)

	zapOpts.BindFlags(flag.CommandLine)
	cmd.PersistentFlags().AddGoFlagSet(flag.CommandLine)

	return cmd
}
