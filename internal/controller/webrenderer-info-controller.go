package controller

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/nut-api/publish-routing-controller.git/pkg/webrenderer"
	"github.com/nut-api/publish-routing-controller.git/pkg/webrenderer/github"
	"github.com/samber/lo"
	corev1 "k8s.io/api/core/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

func webrendererInfoController(ctx context.Context, r *ConfigMapReconciler, req ctrl.Request) (ctrl.Result, error) {
	l := logf.FromContext(ctx)
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
	// Is currently not serving this webrenderer version
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
	toCheckWebrenderer := (&github.WebrendererGithub{Client: r.Client, GithubClient: r.GithubClient}).NewWebrenderer(ctx, strconv.Itoa(servingWebrenderer.Version), r.Namespace)
	err = toCheckWebrenderer.UpdateWebrenderer(ctx, configMap)
	if err != nil {
		l.Error(err, "Failed to update webrenderer to new version", "version", webrendererVersion)
		return ctrl.Result{}, err
	}

	err = r.Get(ctx, client.ObjectKey{Name: "webrenderer-manager-config", Namespace: r.Namespace}, configMap)
	if err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}
	// Update ServingWebrenderer to ConfigMap
	servingWebrenderer.VersionFull = webrendererVersion
	// Update in servingWebrenderersJson
	servingWebrenderersJson = lo.Map(servingWebrenderersJson, func(sw webrenderer.ServingWebrenderer, _ int) webrenderer.ServingWebrenderer {
		if sw.Version == servingWebrenderer.Version {
			return servingWebrenderer
		}
		return sw
	})

	// PostReconcile for github webrenderer to commit and push changes if any
	err = r.PostReconcile(ctx)
	if err != nil {
		return ctrl.Result{}, err
	}
	updateConfigMapByData(ctx, r, configMap, map[string][]webrenderer.ServingWebrenderer{
		"servingWebrenderers": servingWebrenderersJson,
	})

	return ctrl.Result{}, nil
}
