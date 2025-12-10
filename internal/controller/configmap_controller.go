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
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/nut-api/publish-routing-controller.git/pkg/webrenderer"
	"github.com/nut-api/publish-routing-controller.git/pkg/webrenderer/github"
	"github.com/samber/lo"
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
	l := logf.FromContext(ctx)

	fmt.Println("Reconciling ConfigMap:", req.NamespacedName)

	// Print old servingWebrenderersJson
	l.Info("Current servingWebrenderersJson", "servingWebrenderers", servingWebrenderersJson)

	// Check if the ConfigMap is "webrenderer-info"
	if req.Name == "webrenderer-info" {
		fmt.Println("Processing webrenderer-info ConfigMap")
		configMap := &corev1.ConfigMap{}
		err := r.Get(ctx, req.NamespacedName, configMap)
		if err != nil {
			return ctrl.Result{}, client.IgnoreNotFound(err)
		}

		// Update current webrenderer versions with the new configMap
		webrendererVersion := configMap.Data["webrendererVersion"]

		// Check outdated webrenderer versions and update if needed
		servingWebrenderer := lo.FindOrElse(servingWebrenderersJson, webrenderer.ServingWebrenderer{}, func(sw webrenderer.ServingWebrenderer) bool {
			return strconv.Itoa(sw.Version) == strings.Split(strings.TrimPrefix(webrendererVersion, "v"), ".")[0]
		})
		// Return if empty
		if (webrenderer.ServingWebrenderer{}) == servingWebrenderer {
			return ctrl.Result{}, nil
		}
		// Check full version
		if webrendererVersion == servingWebrenderer.VersionFull {
			// Same version, do nothing
			return ctrl.Result{}, nil
		}
		// Update webrenderer to new version
		l.Info("Updating webrenderer to new version", "oldVersion", servingWebrenderer.VersionFull, "newVersion", webrendererVersion)
		webrenderer := (&github.WebrendererGithub{Client: r.Client, GithubClient: r.GithubClient}).NewWebrenderer(ctx, strconv.Itoa(servingWebrenderer.Version), r.Namespace)
		err = webrenderer.UpdateWebrenderer(ctx, configMap)
		if err != nil {
			l.Error(err, "Failed to update webrenderer to new version", "version", webrendererVersion)
			return ctrl.Result{}, err
		}

		return ctrl.Result{}, nil
	}

	// // Check if the ConfigMap is "webrenderer-version-config"
	// if req.Name != "webrenderer-manager-config" {
	// 	// Not the ConfigMap we are interested in, ignore it
	// 	return ctrl.Result{}, nil
	// }
	// fmt.Println("Processing webrenderer-manager-config ConfigMap")

	configMap := &corev1.ConfigMap{}
	err := r.Get(ctx, req.NamespacedName, configMap)
	if err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	// Example: Log the ConfigMap data
	l.Info("ConfigMap data", "data", configMap.Data)

	// Return if no webrenderer-versions value in ConfigMap
	if _, ok := configMap.Data["requiredMajorVersions"]; !ok {
		l.Info("No requiredMajorVersions key in ConfigMap, nothing to do")
		return ctrl.Result{}, nil
	}

	// Construct json version from configmap data
	requiredMajorVersions := configMap.Data["requiredMajorVersions"]
	versionIntJson := []int{}
	err = json.Unmarshal([]byte(requiredMajorVersions), &versionIntJson)
	if err != nil {
		return ctrl.Result{}, err
	}
	versionJson := lo.Map(versionIntJson, func(v int, _ int) string {
		return strconv.Itoa(v)
	})

	servingWebrenderersData := configMap.Data["servingWebrenderers"]
	err = json.Unmarshal([]byte(servingWebrenderersData), &servingWebrenderersJson)
	if err != nil {
		return ctrl.Result{}, err
	}

	pendingServingData := configMap.Data["pendingServingWebrenderers"]
	pendingServingJson := []webrenderer.ServingWebrenderer{}
	err = json.Unmarshal([]byte(pendingServingData), &pendingServingJson)
	if err != nil {
		return ctrl.Result{}, err
	}

	// PreReconcile for github webrenderer to pull latest repo
	err = r.PreReconcile(ctx)
	if err != nil {
		return ctrl.Result{}, err
	}

	updateCheck := false
	for _, version := range versionJson {

		l.Info("Processing required webrenderer version", "version", version)
		versionInt, _ := strconv.Atoi(version)

		// Skip if already in servingWebrenderersJson
		if lo.ContainsBy(servingWebrenderersJson, func(sw webrenderer.ServingWebrenderer) bool {
			return sw.Version == versionInt
		}) {
			continue
		}

		toCheckWebrenderer := (&github.WebrendererGithub{Client: r.Client, GithubClient: r.GithubClient}).NewWebrenderer(ctx, version, r.Namespace)

		// Check if webrenderer is already being deployed (in pendingServingJson)
		servingWebrenderer := lo.FindOrElse(pendingServingJson, webrenderer.ServingWebrenderer{}, func(sw webrenderer.ServingWebrenderer) bool {
			return sw.Version == versionInt
		})
		if servingWebrenderer == (webrenderer.ServingWebrenderer{}) {
			// Ensure the webrenderer exist
			servingWebrenderer, err := toCheckWebrenderer.GetAndCreateIfNotExists(ctx)
			if err != nil {
				l.Error(err, "Failed to create or ensure webrenderer exists", "version", version)
				return ctrl.Result{}, err
			}
			// Add to pendingServingJson
			pendingServingJson = append(pendingServingJson, servingWebrenderer)
			l.Info("Added version to pendingServingWebrenderers", "version", version)
			// Mark updateCheck to true
			updateCheck = true
			continue
		}

		// Check if the webrenderer is ready
		if ready, err := toCheckWebrenderer.IsReady(ctx); err != nil {
			l.Error(err, "Failed to check webrenderer status", "version", version)
			return ctrl.Result{}, err
		} else if !ready {
			l.Info("Webrenderer is not ready", "version", version)
			continue
		}

		// Move ServingWebrenderer from pending to serving
		l.Info("Webrenderer is ready, moving from pending to serving", "version", version)

		servingWebrenderersJson = append(servingWebrenderersJson, servingWebrenderer)
		pendingServingJson = lo.Filter(pendingServingJson, func(v webrenderer.ServingWebrenderer, _ int) bool {
			return v.Version != versionInt
		})

		// Mark updateCheck to true
		updateCheck = true
	}

	// Clean up unneeded serving webrenderers (no longer in required versions)
	if cleanupUnneededWebrenderers(ctx, r, &versionIntJson, &servingWebrenderersJson, &updateCheck) != nil {
		l.Error(err, "Failed to clean up unneeded serving webrenderers")
		return ctrl.Result{}, err
	}

	// Clean up unneeded pending webrenderers (no longer in required versions)
	if cleanupUnneededWebrenderers(ctx, r, &versionIntJson, &pendingServingJson, &updateCheck) != nil {
		l.Error(err, "Failed to clean up unneeded pending webrenderers")
		return ctrl.Result{}, err
	}

	// PostReconcile for github webrenderer to commit and push changes if any
	err = r.PostReconcile(ctx)
	if err != nil {
		return ctrl.Result{}, err
	}

	if updateCheck {
		// Update ConfigMap with new servingWebrenderersJson and pendingServingJson
		servingWebrenderersJsonBytes, _ := json.Marshal(servingWebrenderersJson)
		configMap.Data["servingWebrenderers"] = string(servingWebrenderersJsonBytes)
		pendingServingJsonBytes, _ := json.Marshal(pendingServingJson)
		configMap.Data["pendingServingWebrenderers"] = string(pendingServingJsonBytes)
		err = r.Update(ctx, configMap)
		if err != nil {
			return ctrl.Result{}, err
		}
		l.Info("Updated ConfigMap with new serving and pending webrenderers", "servingWebrenderers", configMap.Data["servingWebrenderers"], "pendingServingWebrenderers", configMap.Data["pendingServingWebrenderers"])
	}

	// Requeue if there are still pending webrenderers being deployed
	if len(pendingServingJson) > 0 {
		l.Info("There are still pending webrenderers being deployed, requeueing", "pendingVersions", pendingServingJson)
		return ctrl.Result{RequeueAfter: 30 * time.Second}, nil
	}

	// Everything is fine, requeue after 10 minutes to ensure the deployment is up-to-date
	return ctrl.Result{RequeueAfter: 10 * time.Minute}, nil
}

// PreReconcile clones or pulls the GitHub repo to ensure we have the latest version
func (r *ConfigMapReconciler) PreReconcile(ctx context.Context) error {
	return github.CloneOrPullRepo(ctx, r.GithubClient)
}

// Commit and push if changed
func (r *ConfigMapReconciler) PostReconcile(ctx context.Context) error {
	commitMsg := "Update webrenderer ArgoCD app " + time.Now().Format(time.RFC3339)
	return github.CommitAndPushChanges(ctx, r.GithubClient, commitMsg)
}

func cleanupUnneededWebrenderers(ctx context.Context, r *ConfigMapReconciler, requiredVersions *[]int, servingVersions *[]webrenderer.ServingWebrenderer, isUpdate *bool) error {
	l := logf.FromContext(ctx)
	var err error
	// Clean up unneeded pending webrenderers (no longer in required versions)
	for _, sv := range *servingVersions {
		if !lo.Contains(*requiredVersions, sv.Version) {
			l.Info("Removing unneeded webrenderer", "version", sv.Version)
			toCheckWebrenderer := (&github.WebrendererGithub{Client: r.Client, GithubClient: r.GithubClient}).NewWebrenderer(ctx, strconv.Itoa(sv.Version), r.Namespace)
			err = toCheckWebrenderer.DeleteWebrenderer(ctx)
			if err != nil {
				l.Error(err, "Failed to delete unneeded webrenderer", "version", sv.Version)
				return err
			}
			// Remove from pendingServingJson
			*servingVersions = lo.Filter(*servingVersions, func(v webrenderer.ServingWebrenderer, _ int) bool {
				return v.Version != sv.Version
			})
			// Mark updateCheck to true
			*isUpdate = true
		}
	}
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
				if obj.GetName() != "webrenderer-manager-config" || obj.GetNamespace() != r.Namespace {
					// If not, don't trigger reconciliation
					return []reconcile.Request{}
				}
				return []reconcile.Request{
					{
						NamespacedName: types.NamespacedName{
							Name:      obj.GetName(),
							Namespace: obj.GetNamespace(),
						},
					},
				}
			}),
		).
		Named("Webrenderer-manager-configmap").
		Complete(r)
}
