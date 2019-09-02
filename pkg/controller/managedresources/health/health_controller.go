package health

import (
	"context"
	"fmt"
	"time"

	resourcesv1alpha1 "github.com/gardener/gardener-resource-manager/pkg/apis/resources/v1alpha1"
	resourcesv1alpha1helper "github.com/gardener/gardener-resource-manager/pkg/apis/resources/v1alpha1/helper"
	"github.com/gardener/gardener-resource-manager/pkg/controller/managedresources"
	"github.com/gardener/gardener-resource-manager/pkg/controller/utils"

	"github.com/go-logr/logr"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/retry"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

type HealthReconciler struct {
	ctx          context.Context
	log          logr.Logger
	client       client.Client
	targetClient client.Client
	classFilter  *managedresources.ClassFilter
	syncPeriod   time.Duration
}

func NewHealthReconciler(ctx context.Context, log logr.Logger, client, targetClient client.Client, classFilter *managedresources.ClassFilter, syncPeriod time.Duration) *HealthReconciler {
	return &HealthReconciler{ctx, log, client, targetClient, classFilter, syncPeriod}
}

func (r *HealthReconciler) Reconcile(req ctrl.Request) (ctrl.Result, error) {
	log := r.log.WithValues("object", req)
	log.Info("Starting ManagedResource health checks")

	mr := &resourcesv1alpha1.ManagedResource{}
	if err := r.client.Get(r.ctx, req.NamespacedName, mr); err != nil {
		if apierrors.IsNotFound(err) {
			log.Info("Could not find Managedresource")
			return reconcile.Result{}, nil
		}
		log.Error(err, "Could not fetch Managedresource")
		return reconcile.Result{}, err
	}

	// Check responsability
	if _, responsible := r.classFilter.Active(mr); !responsible {
		log.Info("Stopping health checks as the responsibility changed")
		return ctrl.Result{}, nil // Do not requeue
	}

	// Initialize condition based on the current status.
	conditionResourcesHealthy := resourcesv1alpha1helper.GetOrInitCondition(mr.Status.Conditions, resourcesv1alpha1.ResourcesHealthy)

	resourcesObjectReferences := mr.Status.Resources
	for _, ref := range resourcesObjectReferences {
		obj := &unstructured.Unstructured{}
		obj.SetAPIVersion(ref.APIVersion)
		obj.SetKind(ref.Kind)

		key := types.NamespacedName{Namespace: ref.Namespace, Name: ref.Name}
		if err := r.targetClient.Get(r.ctx, key, obj); err != nil {
			if apierrors.IsNotFound(err) {
				log.Info("Could not get object", "name", ref.Name)

				var (
					reason  = obj.GetKind() + "Missing"
					message = fmt.Sprintf("Missing required %s with name %q.", ref.Kind, ref.Name)
				)

				conditionResourcesHealthy = resourcesv1alpha1helper.UpdatedCondition(conditionResourcesHealthy, resourcesv1alpha1.ConditionFalse, reason, message)
				if err := tryUpdateManagedResourceCondition(r.ctx, r.client, mr, conditionResourcesHealthy); err != nil {
					log.Error(err, "Could not update the ManagedResource status")
					return ctrl.Result{}, err
				}

				return ctrl.Result{RequeueAfter: r.syncPeriod}, nil // We do not want to run in the exponential backoff for the condition check.
			}

			return ctrl.Result{}, err
		}

		if err := CheckHealth(obj); err != nil {
			var (
				reason  = obj.GetKind() + "Unhealthy"
				message = fmt.Sprintf("%s %s is unhealthy: %v", obj.GetKind(), obj.GetName(), err.Error())
			)

			conditionResourcesHealthy = resourcesv1alpha1helper.UpdatedCondition(conditionResourcesHealthy, resourcesv1alpha1.ConditionFalse, reason, message)
			if err := tryUpdateManagedResourceCondition(r.ctx, r.client, mr, conditionResourcesHealthy); err != nil {
				log.Error(err, "Could not update the ManagedResource status")
				return ctrl.Result{}, err
			}

			return ctrl.Result{RequeueAfter: r.syncPeriod}, nil // We do not want to run in the exponential backoff for the condition check.
		}
	}

	conditionResourcesHealthy = resourcesv1alpha1helper.UpdatedCondition(conditionResourcesHealthy, resourcesv1alpha1.ConditionTrue, "ResourcesHealthy", "All resources are healthy.")
	if err := tryUpdateManagedResourceCondition(r.ctx, r.client, mr, conditionResourcesHealthy); err != nil {
		log.Error(err, "Could not update the ManagedResource status")
		return ctrl.Result{}, err
	}

	log.Info("Finished ManagedResource health checks")
	return ctrl.Result{RequeueAfter: r.syncPeriod}, nil
}

func tryUpdateManagedResourceCondition(ctx context.Context, c client.Client, mr *resourcesv1alpha1.ManagedResource, condition resourcesv1alpha1.ManagedResourceCondition) error {
	return utils.TryUpdateStatus(ctx, retry.DefaultBackoff, c, mr, func() error {
		newConditions := resourcesv1alpha1helper.MergeConditions(mr.Status.Conditions, condition)
		mr.Status.Conditions = newConditions
		return nil
	})
}
