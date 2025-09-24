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
	"slices"
	"strings"
	"time"

	"github.com/nut-api/publish-routing-controller.git/pkg/webrenderer/deployment"
	networkingv1 "istio.io/client-go/pkg/apis/networking/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

// VirtualServiceReconciler reconciles a VirtualService object
type VirtualServiceReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=networking.istio.io,resources=virtualservices,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=networking.istio.io,resources=virtualservices/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=networking.istio.io,resources=virtualservices/finalizers,verbs=update
// +kubebuilder:rbac:groups="",resources=configmaps,verbs=get;list;watch;create;update;patch;delete

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the VirtualService object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.21.0/pkg/reconcile
func (r *VirtualServiceReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	l := logf.FromContext(ctx)

	// TODO(user): your logic here
	virtualservice := &networkingv1.VirtualService{}
	err := r.Get(ctx, req.NamespacedName, virtualservice)
	if err != nil {
		// handle the error here
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}
	fmt.Println("Reconciling VirtualService:", virtualservice.Name)

	// Get virtualservice labels
	labels := virtualservice.GetLabels()
	if labels == nil {
		labels = map[string]string{}
	}

	// l.Info("VirtualService labels", "labels", labels)

	// Check if the label "webrenderer-version" exists
	if _, ok := labels["webrenderer-version"]; !ok {
		l.Info("VirtualService does not have 'webrenderer-version' label, skipping", "namespace", virtualservice.Namespace, "name", virtualservice.Name)
		return ctrl.Result{}, nil
	}
	version := labels["webrenderer-version"]

	l.Info("Reconciling VirtualService", "namespace", virtualservice.Namespace, "name", virtualservice.Name)

	// Add ConfigMap if not exists
	configMap, err := r.addConfigmapsIfNotExists(ctx, virtualservice, "webrenderer-version-config")
	if err != nil {
		l.Error(err, "Failed to ensure ConfigMap exists")
		return ctrl.Result{}, err
	}

	// Check version in ConfigMap
	err = r.checkVersionInConfigMap(ctx, virtualservice, configMap)
	if err != nil {
		l.Error(err, "Failed to check version in ConfigMap")
		return ctrl.Result{}, err
	}

	// Check webrenderer is ready
	webrenderer := (&deployment.WebrendererDeployment{Client: r.Client}).NewWebrenderer(version)
	if ready, err := webrenderer.IsReady(ctx); err != nil {
		l.Error(err, "Failed to check webrenderer deployment status", "version", version)
		return ctrl.Result{}, err
	} else if !ready {
		// If not ready, requeue after 1 minute
		l.Info("Webrenderer deployment is not ready", "version", version)
		// Requeue after 10 second to check again
		return ctrl.Result{RequeueAfter: 10 * time.Second}, nil
	}

	// Edit virtualservice destination to use the correct webrenderer service
	err = r.editVirtualServiceDestination(ctx, virtualservice)
	if err != nil {
		l.Error(err, "Failed to edit VirtualService destination")
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

func (r *VirtualServiceReconciler) addConfigmapsIfNotExists(ctx context.Context, v *networkingv1.VirtualService, name string) (*corev1.ConfigMap, error) {
	// Implement the logic to check if the ConfigMap exists and create it if not
	l := logf.FromContext(ctx)

	configMap := &corev1.ConfigMap{}
	err := r.Get(ctx, types.NamespacedName{Name: name, Namespace: v.Namespace}, configMap)
	if err != nil && errors.IsNotFound(err) {
		// Create the ConfigMap
		l.Info("Creating ConfigMap", "name", name)
		newConfigMap := &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      name,
				Namespace: v.Namespace,
			},
			Data: map[string]string{
				"webrenderer-versions": "",
			},
		}
		// if err := ctrl.SetControllerReference(v, newConfigMap, r.Scheme); err != nil {
		// 	return *configMap, err
		// }
		if err = r.Create(ctx, newConfigMap); err != nil {
			l.Error(err, "Failed to create ConfigMap", "name", name)
			return configMap, err
		}
		return newConfigMap, nil
	} else if err != nil {
		l.Error(err, "Failed to get ConfigMap", "name", name)
		return configMap, err
	}
	return configMap, nil
}

// Function check version in virtualservice labels is in configmap data
func (r *VirtualServiceReconciler) checkVersionInConfigMap(ctx context.Context, v *networkingv1.VirtualService, c *corev1.ConfigMap) error {
	l := logf.FromContext(ctx)

	// Get the version from the virtualservice labels
	version := v.Labels["webrenderer-version"]
	if version == "" {
		l.Info("VirtualService does not have 'webrenderer-version' label")
		return nil
	}

	// Check if the version is in the configmap data
	if versions, ok := c.Data["webrenderer-versions"]; ok {
		// Split the versions by comma and check if the version exists
		if slices.Contains(strings.Split(versions, ","), version) {
			// l.Info("Version found in ConfigMap", "version", version)
			return nil
		}
	}

	// If not found, append the version to the configmap data
	if c.Data == nil {
		c.Data = map[string]string{}
	}
	if existingVersions, ok := c.Data["webrenderer-versions"]; ok && existingVersions != "" {
		c.Data["webrenderer-versions"] = existingVersions + "," + version
	} else {
		c.Data["webrenderer-versions"] = version
	}

	l.Info("Adding version to ConfigMap", "version", version)
	if err := r.Update(ctx, c); err != nil {
		return err
	}

	return nil
}

// Function to edit virtualservice destination to use the correct webrenderer service
func (r *VirtualServiceReconciler) editVirtualServiceDestination(ctx context.Context, v *networkingv1.VirtualService) error {
	l := logf.FromContext(ctx)

	// Get the version from the virtualservice labels
	version := v.Labels["webrenderer-version"]
	if version == "" {
		l.Info("VirtualService does not have 'webrenderer-version' label")
		return nil
	}

	// Deepcopy the old virtualservice for comparison
	oldVirtualservice := v.DeepCopy()

	// Edit the virtualservice destination to use the correct webrenderer service
	if len(v.Spec.Http) > 0 && len(v.Spec.Http[0].Route) > 0 {
		for i := range v.Spec.Http[0].Route {
			if v.Spec.Http[0].Route[i].Destination != nil {
				v.Spec.Http[0].Route[i].Destination.Host = "webrenderer-" + version + ".default.svc.cluster.local"
			}
		}
	}

	// If old and new virtualservice are the same, do nothing
	if oldVirtualservice.Spec.String() == v.Spec.String() {
		l.Info("VirtualService destination already set correctly", "name", v.Name)
		return nil
	}
	// Log the change
	l.Info("Updating VirtualService destination")

	// Add currently use version to label

	v.Labels["current-webrenderer-version"] = version

	// Update the virtualservice
	if err := r.Update(ctx, v); err != nil {
		l.Error(err, "Failed to update VirtualService", "name", v.Name)
	}
	return nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *VirtualServiceReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		// Uncomment the following line adding a pointer to an instance of the controlled resource as an argument
		For(&networkingv1.VirtualService{}).
		Named("virtualservice").
		Complete(r)
}
