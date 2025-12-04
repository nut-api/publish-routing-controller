package github

import (
	"context"
	"fmt"
	"os"
	"strings"

	argoappv1 "github.com/argoproj/argo-cd/v3/pkg/apis/application/v1alpha1"
	"github.com/goccy/go-yaml"
	"github.com/nut-api/publish-routing-controller.git/pkg/webrenderer"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

var _ webrenderer.Webrenderer = (*WebrendererGithub)(nil)

type WebrendererGithub struct {
	Client             client.Client
	GithubClient       GithubClient
	WebrendererVersion string
	WebrendererPath    string
}

func (g *WebrendererGithub) NewWebrenderer(ctx context.Context, version string) webrenderer.Webrenderer {
	return &WebrendererGithub{
		Client:             g.Client,
		GithubClient:       g.GithubClient,
		WebrendererVersion: version,
		WebrendererPath:    "app-repo/webrenderer-" + version,
	}
}

func (g *WebrendererGithub) GetAndCreateIfNotExists(ctx context.Context) error {
	l := logf.FromContext(ctx)
	// Check if ArgoCD app YAML exists in WebrendererPath
	appPath := g.WebrendererPath + "/app.yaml"
	valuesPath := g.WebrendererPath + "/values.yaml"
	if _, err := os.Stat(appPath); err == nil {
		l.Info("ArgoCD app YAML already exists", "path", appPath)
		// File exists
		return nil
	} else if !os.IsNotExist(err) {
		// Other error
		return err
	}

	currentConfig := &corev1.ConfigMap{}
	err := g.Client.Get(ctx, client.ObjectKey{Name: "webrenderer-info", Namespace: "default"}, currentConfig)
	if err != nil {
		l.Error(err, "Failed to get current ConfigMap")
	}

	// Check CurrentConfig have data
	if currentConfig.Data == nil {
		l.Info("Current ConfigMap has no data, cannot create webrenderer")
		err := os.NewSyscallError("ConfigMap Data not found", nil)
		return err
	}

	// Get the ArgoCD app YAML from the template
	appYaml, err := GetArgoCDAppYAML(g.WebrendererVersion, currentConfig.Data["chartVersion"])
	if err != nil {
		return err
	}
	var valuesYaml map[string]interface{}
	if yaml.Unmarshal([]byte(currentConfig.Data["values"]), &valuesYaml) != nil {
		return err
	}
	var envYaml []interface{}
	if yaml.Unmarshal([]byte(currentConfig.Data["env"]), &envYaml) != nil {
		return err
	}

	// Configure webrenderer from current ConfigMap
	webrendererValues := valuesYaml["webrenderer"].(map[string]interface{})
	webrendererValues["overrideEnv"] = envYaml
	//change image.version to g.WebrendererVersion
	webrendererValues["image"].(map[string]interface{})["version"] = g.WebrendererVersion
	valuesYaml["webrenderer"] = webrendererValues

	// Spacial value for isolate webrenderer
	valuesYaml["global"].(map[string]interface{})["isBaseChart"] = false

	// Marshal valuesYaml back to YAML
	valuesBytes, err := yaml.Marshal(valuesYaml)
	if err != nil {
		return err
	}

	// Ensure directory exists
	if err := os.MkdirAll(g.WebrendererPath, 0755); err != nil {
		return err
	}
	l.Info("Creating ArgoCD app YAML", "path", appPath)
	// Write the YAML file
	if os.WriteFile(appPath, []byte(appYaml), 0644) != nil {
		return err
	}
	l.Info("Creating values YAML", "path", valuesPath)
	// Write the values.yaml file
	return os.WriteFile(valuesPath, []byte(valuesBytes), 0644)
}

func (g *WebrendererGithub) DeleteWebrenderer(context.Context) error {
	//Remove the directory
	return os.RemoveAll(g.WebrendererPath)
}

func (g *WebrendererGithub) UpdateWebrenderer(ctx context.Context) error {
	// TODO: Implement update logic if needed
	return nil
}

func (g *WebrendererGithub) IsReady(ctx context.Context) (bool, error) {
	app := &argoappv1.Application{}
	err := g.Client.Get(ctx, client.ObjectKey{
		Name:      "webrenderer-" + g.WebrendererVersion,
		Namespace: "argocd",
	}, app)
	if err != nil {
		// Application not found
		if client.IgnoreNotFound(err) != nil {
			return false, err
		}
		fmt.Println("Webrenderer verion ", g.WebrendererVersion, " not found")
		return false, nil
	}
	if app.Status.Sync.Status != "Synced" || app.Status.Health.Status != "Healthy" {
		return false, nil
	}
	return true, nil

	// // check if the app.yaml file exists
	// appPath := g.WebrendererPath + "/app.yaml"
	// if _, err := os.Stat(appPath); err == nil {
	// 	// File exists
	// 	return true, nil
	// } else if !os.IsNotExist(err) {
	// 	// Other error
	// 	return false, err
	// }
	// // File does not exist
	// return false, nil
}

func GetArgoCDAppYAML(version string, chartVersion string) (string, error) {
	// Get from file on template/app.yaml
	data, err := os.ReadFile("app-repo/template/app.yaml")
	if err != nil {
		return "", err
	}
	// Replace VERSION with version
	appYaml := string(data)
	appYaml = strings.ReplaceAll(appYaml, "CHART_VERSION", chartVersion)
	appYaml = strings.ReplaceAll(appYaml, "VERSION", version)
	return appYaml, nil
}
