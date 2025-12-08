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
	"time"

	"github.com/nut-api/publish-routing-controller.git/pkg/webrenderer/github"
	"github.com/samber/lo"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

// ConfigMapReconciler reconciles a ConfigMap object
type ConfigMapReconciler struct {
	client.Client
	Scheme       *runtime.Scheme
	GithubClient github.GithubClient
	Namespace    string
}

type ServingWebrenderer struct {
	Version int    `json:"version"`
	Host    string `json:"host"`
}

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

	// Check if the ConfigMap is "webrenderer-version-config"
	if req.Name != "webrenderer-manager-config" || req.Namespace != r.Namespace {
		// Not the ConfigMap we are interested in, ignore it
		return ctrl.Result{}, nil
	}
	fmt.Println("Processing webrenderer-manager-config ConfigMap")

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
	servingWebrenderersJson := []ServingWebrenderer{}
	err = json.Unmarshal([]byte(servingWebrenderersData), &servingWebrenderersJson)
	if err != nil {
		return ctrl.Result{}, err
	}

	pendingServingData := configMap.Data["pendingServingWebrenderers"]
	// If empty, initialize to empty array
	if pendingServingData == "" {
		pendingServingData = "[]"
	}
	pendingServingJson := []int{}
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
		// print version
		fmt.Println("This is version: ", version)
		versionInt, _ := strconv.Atoi(version)

		// Skip if already in servingWebrenderersJson
		if lo.ContainsBy(servingWebrenderersJson, func(sw ServingWebrenderer) bool {
			return sw.Version == versionInt
		}) {
			continue
		}

		webrenderer := (&github.WebrendererGithub{Client: r.Client, GithubClient: r.GithubClient}).NewWebrenderer(ctx, version, r.Namespace)

		// Check if webrenderer is already being deployed (in pendingServingJson)
		if !lo.Contains(pendingServingJson, versionInt) {
			// Ensure the webrenderer exist
			err = webrenderer.GetAndCreateIfNotExists(ctx)
			if err != nil {
				l.Error(err, "Failed to create or ensure webrenderer exists", "version", version)
				// webrenderer.DeleteWebrenderer(ctx)
				return ctrl.Result{}, err
			}
			// Add to pendingServingJson
			pendingServingJson = append(pendingServingJson, versionInt)
			l.Info("Added version to pendingServingWebrenderers", "version", version)
			// Mark updateCheck to true
			updateCheck = true
			continue
		}

		// if webrenderer.UpdateWebrenderer(ctx) != nil {
		// 	l.Error(err, "Failed to update webrenderer", "version", version)
		// 	return ctrl.Result{}, err
		// }

		if ready, err := webrenderer.IsReady(ctx); err != nil {
			l.Error(err, "Failed to check webrenderer status", "version", version)
			return ctrl.Result{}, err
		} else if !ready {
			// If not ready, requeue after 1 minute
			l.Info("Webrenderer is not ready", "version", version)
			// return ctrl.Result{RequeueAfter: 10 * time.Second}, nil
			continue
		}

		// Add ready webrenderer to servingWebrenderers list
		webrendererHost := fmt.Sprintf("webrenderer-%s", version)
		l.Info("Adding new ServingWebrenderer", "version", version, "host", webrendererHost)
		servingWebrenderersJson = append(servingWebrenderersJson, ServingWebrenderer{
			Version: versionInt,
			Host:    webrendererHost,
		})
		// Remove from pendingServingJson
		pendingServingJson = lo.Filter(pendingServingJson, func(v int, _ int) bool {
			return v != versionInt
		})
		// Mark updateCheck to true
		updateCheck = true
	}

	// Clean up unneeded webrenderers
	for _, sw := range servingWebrenderersJson {
		if !lo.Contains(versionIntJson, sw.Version) {
			l.Info("Removing unneeded webrenderer", "version", sw.Version)
			webrenderer := (&github.WebrendererGithub{Client: r.Client, GithubClient: r.GithubClient}).NewWebrenderer(ctx, strconv.Itoa(sw.Version), r.Namespace)
			err = webrenderer.DeleteWebrenderer(ctx)
			if err != nil {
				l.Error(err, "Failed to delete unneeded webrenderer", "version", sw.Version)
				return ctrl.Result{}, err
			}
			// Remove from servingWebrenderersJson
			servingWebrenderersJson = lo.Filter(servingWebrenderersJson, func(v ServingWebrenderer, _ int) bool {
				return v.Version != sw.Version
			})
			// Mark updateCheck to true
			updateCheck = true
		}
	}
	// Clean up unneeded pending webrenderers
	for _, pv := range pendingServingJson {
		if !lo.Contains(versionIntJson, pv) {
			l.Info("Removing unneeded pending webrenderer", "version", pv)
			webrenderer := (&github.WebrendererGithub{Client: r.Client, GithubClient: r.GithubClient}).NewWebrenderer(ctx, strconv.Itoa(pv), r.Namespace)
			err = webrenderer.DeleteWebrenderer(ctx)
			if err != nil {
				l.Error(err, "Failed to delete unneeded pending webrenderer", "version", pv)
				return ctrl.Result{}, err
			}
			// Remove from pendingServingJson
			pendingServingJson = lo.Filter(pendingServingJson, func(v int, _ int) bool {
				return v != pv
			})
			// Mark updateCheck to true
			updateCheck = true
		}
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

// SetupWithManager sets up the controller with the Manager.
func (r *ConfigMapReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		// Watch only webrenderer-version-config ConfigMap
		For(&corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "webrenderer-manager-config",
				Namespace: r.Namespace,
			},
		}).
		// Named("webrenderer-manager-configmap").
		Complete(r)
}
