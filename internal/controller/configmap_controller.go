/*
Copyright 2025.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package controller

import (
	"context"
	"encoding/json"
	"time"

	"github.com/nut-api/publish-routing-controller.git/pkg/webrenderer"
	"github.com/nut-api/publish-routing-controller.git/pkg/webrenderer/github"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

// ConfigMapReconciler reconciles a ConfigMap object
type ConfigMapReconciler struct {
	client.Client
	Scheme       *runtime.Scheme
	GithubClient github.GithubClient
	Namespace    string
}

// In-memory serving webrenderers list
var servingWebrenderersJson []webrenderer.ServingWebrenderer

// +kubebuilder:rbac:groups=core,resources=configmaps,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=core,resources=configmaps/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=core,resources=configmaps/finalizers,verbs=update
// +kubebuilder:rbac:groups=core,resources=services,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=apps,resources=deployments,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=argoproj.io,resources=applications,verbs=get;list;watch

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// the ConfigMap object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.21.0/pkg/reconcile
func (r *ConfigMapReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	// PreReconcile for github webrenderer to pull latest repo
	err := r.PreReconcile(ctx)
	if err != nil {
		return ctrl.Result{}, err
	}

	// Dispatch to specific controller based on ConfigMap name
	result := ctrl.Result{}
	switch req.Name {
	case "webrenderer-manager-config":
		result, err = webrendererManagerController(ctx, r, req)
	case "webrenderer-info":
		result, err = webrendererInfoController(ctx, r, req)
	}
	if err != nil {
		return ctrl.Result{}, err
	}

	// PostReconcile for github webrenderer to commit and push changes if any
	err = r.PostReconcile(ctx)
	if err != nil {
		return ctrl.Result{}, err
	}

	// Everything is fine
	return result, nil
}

// PreReconcile clones or pulls the GitHub repo to ensure we have the latest version
func (r *ConfigMapReconciler) PreReconcile(ctx context.Context) error {
	return github.CloneOrPullRepo(ctx, r.GithubClient)
}

// PostReconcile commit and push if changed
func (r *ConfigMapReconciler) PostReconcile(ctx context.Context) error {
	commitMsg := "Update webrenderer ArgoCD app " + time.Now().Format(time.RFC3339)
	return github.CommitAndPushChanges(ctx, r.GithubClient, commitMsg)
}

func updateConfigMapByData(ctx context.Context, r *ConfigMapReconciler, configMap *corev1.ConfigMap, data map[string][]webrenderer.ServingWebrenderer) error {
	l := logf.FromContext(ctx)
	// If data empty, do nothing
	if len(data) == 0 {
		return nil
	}
	// Update ConfigMap data
	for key, values := range data {
		dataBytes, _ := json.Marshal(values)
		configMap.Data[key] = string(dataBytes)
	}
	err := r.Update(ctx, configMap)
	if err != nil {
		l.Error(err, "Failed to update ConfigMap with new servingWebrenderers")
		return err
	}
	l.Info("Updated ConfigMap with new data", "dataKeys", data)
	return nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *ConfigMapReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		// Watch only webrenderer-version-config ConfigMap
		Watches(
			&corev1.ConfigMap{},
			handler.EnqueueRequestsFromMapFunc(func(ctx context.Context, obj client.Object) []reconcile.Request {
				// Check if the configmap resource name and namespace match
				if (obj.GetName() == "webrenderer-manager-config" || obj.GetName() == "webrenderer-info") && obj.GetNamespace() == r.Namespace {
					// If not, don't trigger reconciliation
					return []reconcile.Request{
						{
							NamespacedName: types.NamespacedName{
								Name:      obj.GetName(),
								Namespace: obj.GetNamespace(),
							},
						},
					}
				}
				return []reconcile.Request{}
			}),
		).
		Named("Webrenderer-manager-configmap").
		Complete(r)
}
