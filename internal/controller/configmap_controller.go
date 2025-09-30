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
	"fmt"
	"strings"
	"time"

	"github.com/nut-api/publish-routing-controller.git/pkg/webrenderer/github"
	networkingv1 "istio.io/client-go/pkg/apis/networking/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
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
}

// +kubebuilder:rbac:groups=core,resources=configmaps,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=core,resources=configmaps/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=core,resources=configmaps/finalizers,verbs=update
// +kubebuilder:rbac:groups=core,resources=services,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=apps,resources=deployments,verbs=get;list;watch;create;update;patch;delete

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the ConfigMap object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.21.0/pkg/reconcile
func (r *ConfigMapReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	l := logf.FromContext(ctx)

	fmt.Println("Reconciling ConfigMap:", req.NamespacedName)

	// TODO(user): your logic here
	configMap := &corev1.ConfigMap{}
	err := r.Get(ctx, req.NamespacedName, configMap)
	if err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}
	// Check if the ConfigMap is "webrenderer-version-config"
	if configMap.Name != "webrenderer-version-config" || configMap.Namespace != "default" {
		// Not the ConfigMap we are interested in, ignore it
		return ctrl.Result{}, nil
	}

	// Example: Log the ConfigMap data
	l.Info("ConfigMap data", "data", configMap.Data)

	// Return if no webrenderer-versions value in ConfigMap
	if _, ok := configMap.Data["webrenderer-versions"]; !ok {
		l.Info("No webrenderer-versions key in ConfigMap, nothing to do")
		return ctrl.Result{}, nil
	}

	versions := strings.SplitSeq(configMap.Data["webrenderer-versions"], ",")
	if versions == nil {
		return ctrl.Result{}, nil
	}

	//PreReconcile for github webrenderer to pull latest repo
	err = r.PreReconcile(ctx)
	if err != nil {
		return ctrl.Result{}, err
	}

	updateVersions := []string{}
	for version := range versions {

		webrenderer := (&github.WebrendererGithub{Client: r.Client, GithubClient: r.GithubClient}).NewWebrenderer(ctx, version)

		// Check any VirtualService is using this version by label "webrenderer-version"
		labels := []string{"webrenderer-version", "current-webrenderer-version"}
		used := false
		// Check all labels
		for _, label := range labels {
			l.Info("Check VirtualService using webrenderer version", "label", label, "version", version)
			used, err = r.isWebrendererUsedByLabel(ctx, label, version)
			if err != nil {
				return ctrl.Result{}, err
			}
			if used {
				break
			}
		}
		if !used {
			// No VirtualService is using this version, delete the webrenderer
			l.Info("No VirtualService is using this version, Delete webrenderer", "version", version)
			webrenderer.DeleteWebrenderer(ctx)
			if err != nil {
				return ctrl.Result{}, err
			}
			continue
		}
		l.Info("Found VirtualService is using this version, ensure webrenderer exists", "version", version)
		// Add used version to updateVersions list
		updateVersions = append(updateVersions, version)

		// Ensure the webrenderer exist
		err = webrenderer.GetAndCreateIfNotExists(ctx)
		if err != nil {
			l.Error(err, "Failed to create or ensure webrenderer exists", "version", version)
			webrenderer.DeleteWebrenderer(ctx)
			return ctrl.Result{}, err
		}
	}

	// PostReconcile for github webrenderer to commit and push changes if any
	err = r.PostReconcile(ctx)
	if err != nil {
		return ctrl.Result{}, err
	}

	// if strings.Join(versions, ",") != strings.Join(updateVersions, ",") {
	if configMap.Data["webrenderer-versions"] != strings.Join(updateVersions, ",") {
		l.Info("ConfigMap webrenderer-versions changed, updating", "old", configMap.Data["webrenderer-versions"], "new", strings.Join(updateVersions, ","))
		// Update the ConfigMap with the current versions in use
		configMap.Data["webrenderer-versions"] = strings.Join(updateVersions, ",")
		err = r.Update(ctx, configMap)
		if err != nil {
			return ctrl.Result{}, err
		}
		l.Info("Updated ConfigMap with current webrenderer versions", "versions", configMap.Data["webrenderer-versions"])
	} else {
		l.Info("ConfigMap webrenderer-versions is up-to-date", "versions", configMap.Data["webrenderer-versions"])
	}

	// Everything is fine, requeue after 10 minutes to ensure the deployment is up-to-date
	return ctrl.Result{RequeueAfter: 10 * time.Minute}, nil
}

// Check webrenderer is used by virtualserice label
func (r *ConfigMapReconciler) isWebrendererUsedByLabel(ctx context.Context, key string, value string) (bool, error) {
	vss := &networkingv1.VirtualServiceList{}
	err := r.List(ctx, vss, &client.ListOptions{
		Namespace:     "default",
		LabelSelector: labels.SelectorFromSet(labels.Set{key: value}),
		Limit:         1,
	})
	if err != nil {
		return false, err
	}
	return len(vss.Items) != 0, nil
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
				Name:      "webrenderer-version-config",
				Namespace: "default",
			},
		}).
		Named("webrenderer-version-configmap").
		Complete(r)
}
