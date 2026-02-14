package controller

import (
	"context"
	"encoding/json"
	"strconv"
	"time"

	"github.com/nut-api/publish-routing-controller.git/pkg/webrenderer"
	"github.com/nut-api/publish-routing-controller.git/pkg/webrenderer/github"
	"github.com/samber/lo"
	corev1 "k8s.io/api/core/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

func webrendererManagerController(ctx context.Context, r *ConfigMapReconciler, req ctrl.Request) (ctrl.Result, error) {
	l := logf.FromContext(ctx)
	l.Info("Reconciling ConfigMap")

	configMap := &corev1.ConfigMap{}
	err := r.Get(ctx, req.NamespacedName, configMap)
	if err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}
	// Return if no webrenderer-versions value in ConfigMap
	if _, ok := configMap.Data["requiredMajorVersions"]; !ok {
		l.Info("No requiredMajorVersions key in ConfigMap, nothing to do")
		return ctrl.Result{}, nil
	}

	// Parse requiredMajorVersions from ConfigMap
	requiredVersionJson := []int{}
	err = json.Unmarshal([]byte(configMap.Data["requiredMajorVersions"]), &requiredVersionJson)
	if err != nil {
		return ctrl.Result{}, err
	}
	// servingWebrenderersJson defined on global scope for multiple reconcile use
	err = json.Unmarshal([]byte(configMap.Data["servingWebrenderers"]), &servingWebrenderersJson)
	if err != nil {
		return ctrl.Result{}, err
	}
	pendingServingJson := []webrenderer.ServingWebrenderer{}
	err = json.Unmarshal([]byte(configMap.Data["pendingServingWebrenderers"]), &pendingServingJson)
	if err != nil {
		return ctrl.Result{}, err
	}

	// List contains webrenderers that need to be updated in ConfigMap
	updateConfigMapList := map[string][]webrenderer.ServingWebrenderer{}

	for _, version := range requiredVersionJson {
		l.Info("Processing required webrenderer version", "version", version)

		// Skip if already in servingWebrenderersJson
		if lo.ContainsBy(servingWebrenderersJson, func(sw webrenderer.ServingWebrenderer) bool {
			return sw.Version == version
		}) {
			continue
		}

		webrendererClient := (&github.WebrendererGithub{Client: r.Client, GithubClient: r.GithubClient}).NewWebrenderer(ctx, strconv.Itoa(version), r.Namespace)

		// Check if webrenderer is already being deployed (in pendingServingJson)
		focusWebrenderer := lo.FindOrElse(pendingServingJson, webrenderer.ServingWebrenderer{}, func(sw webrenderer.ServingWebrenderer) bool {
			return sw.Version == version
		})
		if focusWebrenderer == (webrenderer.ServingWebrenderer{}) {
			// Ensure the webrenderer exist
			focusWebrenderer, err = webrendererClient.GetAndCreateIfNotExists(ctx)
			if err != nil {
				l.Error(err, "Failed to create or ensure webrenderer exists", "version", version)
				return ctrl.Result{}, err
			}
			// Add to pendingServingJson
			pendingServingJson = append(pendingServingJson, focusWebrenderer)
			l.Info("Added version to pendingServingWebrenderers", "version", version)
			// Mark updateCheck to true
			updateConfigMapList["pendingServingWebrenderers"] = pendingServingJson
			continue
		}

		// Check if the webrenderer is ready
		if ready, err := webrendererClient.IsReady(ctx); err != nil {
			l.Error(err, "Failed to check webrenderer status", "version", version)
			return ctrl.Result{}, err
		} else if !ready {
			l.Info("Webrenderer is not ready", "version", version)
			continue
		}

		// Move ServingWebrenderer from pending to serving
		l.Info("Webrenderer is ready, moving from pending to serving", "version", version)

		servingWebrenderersJson = append(servingWebrenderersJson, focusWebrenderer)
		pendingServingJson = lo.Filter(pendingServingJson, func(v webrenderer.ServingWebrenderer, _ int) bool {
			return v.Version != version
		})

		// Mark updateConfigMapList
		updateConfigMapList["servingWebrenderers"] = servingWebrenderersJson
		updateConfigMapList["pendingServingWebrenderers"] = pendingServingJson
	}

	// Clean up unneeded serving webrenderers (no longer in required versions)
	if isClean, err := cleanupUnneededWebrenderers(ctx, r, &requiredVersionJson, &servingWebrenderersJson); err != nil {
		l.Error(err, "Failed to clean up unneeded serving webrenderers")
		return ctrl.Result{}, err
	} else if isClean {
		updateConfigMapList["servingWebrenderers"] = servingWebrenderersJson
	}

	// Clean up unneeded pending webrenderers (no longer in required versions)
	if isClean, err := cleanupUnneededWebrenderers(ctx, r, &requiredVersionJson, &pendingServingJson); err != nil {
		l.Error(err, "Failed to clean up unneeded pending webrenderers")
		return ctrl.Result{}, err
	} else if isClean {
		updateConfigMapList["pendingServingWebrenderers"] = pendingServingJson
	}
	updateConfigMapByData(ctx, r, configMap, updateConfigMapList)

	// Requeue if there are still pending webrenderers being deployed
	if len(pendingServingJson) > 0 {
		l.Info("There are still pending webrenderers being deployed, requeueing", "pendingVersions", pendingServingJson)
		return ctrl.Result{RequeueAfter: 1 * time.Minute}, nil
	}
	return ctrl.Result{}, nil
}

func cleanupUnneededWebrenderers(ctx context.Context, r *ConfigMapReconciler, requiredVersions *[]int, servingVersions *[]webrenderer.ServingWebrenderer) (bool, error) {
	l := logf.FromContext(ctx)
	isUpdate := false
	var err error
	// Clean up unneeded pending webrenderers (no longer in required versions)
	for _, sv := range *servingVersions {
		if lo.Contains(*requiredVersions, sv.Version) {
			continue
		}
		// No longer in requeired versions, delete it
		l.Info("Removing unneeded webrenderer", "version", sv.Version)
		webrendererClient := (&github.WebrendererGithub{Client: r.Client, GithubClient: r.GithubClient}).NewWebrenderer(ctx, strconv.Itoa(sv.Version), r.Namespace)
		err = webrendererClient.DeleteWebrenderer(ctx)
		if err != nil {
			l.Error(err, "Failed to delete unneeded webrenderer", "version", sv.Version)
			return isUpdate, err
		}
		// Remove from pendingServingJson
		*servingVersions = lo.Filter(*servingVersions, func(v webrenderer.ServingWebrenderer, _ int) bool {
			return v.Version != sv.Version
		})
		// Mark updateCheck to true
		isUpdate = true
	}
	return isUpdate, nil
}
