package github

import (
	"context"
	"os"
	"strconv"
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
	Client               client.Client
	GithubClient         GithubClient
	WebrendererVersion   string
	WebrendererPath      string
	WebrendererNamespace string
}

func (g *WebrendererGithub) NewWebrenderer(ctx context.Context, version string, namespace string) webrenderer.Webrenderer {
	return &WebrendererGithub{
		Client:               g.Client,
		GithubClient:         g.GithubClient,
		WebrendererVersion:   version,
		WebrendererPath:      "app-repo/webrenderer-" + version,
		WebrendererNamespace: namespace,
	}
}

func (g *WebrendererGithub) GetAndCreateIfNotExists(ctx context.Context) (webrenderer.ServingWebrenderer, error) {
	l := logf.FromContext(ctx)
	// Check if ArgoCD app YAML exists in WebrendererPath
	if _, err := os.Stat(g.WebrendererPath); err == nil {
		l.Info("ArgoCD app YAML already exists", "path", g.WebrendererPath)
		// File exists
		return webrenderer.ServingWebrenderer{
			Version: func() int {
				v, _ := strconv.Atoi(g.WebrendererVersion)
				return v
			}(),
			VersionFull: "",
			Host:        "webrenderer-" + g.WebrendererVersion,
		}, nil
	} else if !os.IsNotExist(err) {
		// Other error
		return webrenderer.ServingWebrenderer{}, err
	}

	currentConfig := &corev1.ConfigMap{}
	err := g.Client.Get(ctx, client.ObjectKey{Name: "webrenderer-info", Namespace: g.WebrendererNamespace}, currentConfig)
	if err != nil {
		l.Error(err, "Failed to get current ConfigMap")
	}

	// Check CurrentConfig have data
	if currentConfig.Data == nil {
		l.Info("Current ConfigMap has no data, cannot create webrenderer")
		err := os.NewSyscallError("ConfigMap Data not found", nil)
		return webrenderer.ServingWebrenderer{}, err
	}

	return CreateWebrenderer(ctx, g, currentConfig)

}

func (g *WebrendererGithub) DeleteWebrenderer(context.Context) error {
	//Remove the directory
	return os.RemoveAll(g.WebrendererPath)
}

func (g *WebrendererGithub) UpdateWebrenderer(ctx context.Context, currentConfig *corev1.ConfigMap) error {
	g.DeleteWebrenderer(ctx)
	if _, err := CreateWebrenderer(ctx, g, currentConfig); err != nil {
		return err
	}
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
		return false, nil
	}
	if app.Status.Sync.Status != "Synced" || app.Status.Health.Status != "Healthy" {
		return false, nil
	}
	return true, nil
}

func CreateWebrenderer(ctx context.Context, g *WebrendererGithub, currentConfig *corev1.ConfigMap) (webrenderer.ServingWebrenderer, error) {
	l := logf.FromContext(ctx)
	appPath := g.WebrendererPath + "/app.yaml"
	valuesPath := g.WebrendererPath + "/values.yaml"

	// Get the ArgoCD app YAML from the template
	appYaml, err := GetArgoCDAppYAML(g.WebrendererVersion, currentConfig.Data["chartVersion"], g.GithubClient.RepoURL, g.WebrendererNamespace)
	if err != nil {
		return webrenderer.ServingWebrenderer{}, err
	}
	// Get the values.yaml content
	valuesBytes, err := GetWebrendererValues(currentConfig, g)
	if err != nil {
		return webrenderer.ServingWebrenderer{}, err
	}

	// Ensure directory exists
	if err := os.MkdirAll(g.WebrendererPath, 0755); err != nil {
		return webrenderer.ServingWebrenderer{}, err
	}
	l.Info("Creating ArgoCD app YAML", "path", appPath)
	// Write the YAML file
	if os.WriteFile(appPath, []byte(appYaml), 0644) != nil {
		return webrenderer.ServingWebrenderer{}, err
	}
	l.Info("Creating values YAML", "path", valuesPath)
	// Write the values.yaml file
	return webrenderer.ServingWebrenderer{
		Version: func() int {
			v, _ := strconv.Atoi(g.WebrendererVersion)
			return v
		}(),
		VersionFull: currentConfig.Data["webrendererVersion"],
		Host:        "webrenderer-" + g.WebrendererVersion,
	}, os.WriteFile(valuesPath, valuesBytes, 0644)
}

func GetArgoCDAppYAML(version string, chartVersion string, repoUrl string, namespace string) ([]byte, error) {
	// Get from file on template/app.yaml
	data, err := os.ReadFile("app-repo/template/app.yaml")
	if err != nil {
		return []byte{}, err
	}
	// Replace VERSION with version
	appYaml := string(data)
	appYaml = strings.ReplaceAll(appYaml, "CHART_VERSION", chartVersion)
	appYaml = strings.ReplaceAll(appYaml, "VERSION", version)
	appYaml = strings.ReplaceAll(appYaml, "REPO_URL", repoUrl)
	appYaml = strings.ReplaceAll(appYaml, "NAMESPACE", namespace)
	return []byte(appYaml), nil
}

func GetWebrendererValues(currentConfig *corev1.ConfigMap, g *WebrendererGithub) ([]byte, error) {
	var valuesYaml map[string]interface{}
	if err := yaml.Unmarshal([]byte(currentConfig.Data["values"]), &valuesYaml); err != nil {
		return nil, err
	}
	var envYaml []interface{}
	if err := yaml.Unmarshal([]byte(currentConfig.Data["env"]), &envYaml); err != nil {
		return nil, err
	}
	var currentFullVersion string
	if err := yaml.Unmarshal([]byte(currentConfig.Data["webrendererVersion"]), &currentFullVersion); err != nil {
		return nil, err
	}

	// Configure webrenderer from current ConfigMap
	// Set old values
	webrendererValues := valuesYaml["webrenderer"].(map[string]interface{})

	// replace values
	webrendererValues["overrideEnv"] = envYaml

	// If major version is different from currentConfig(deploy non current version), change currentFullVersion to g.WebrendererVersion
	if g.WebrendererVersion != strings.Split(strings.TrimPrefix(currentFullVersion, "v"), ".")[0] {
		currentFullVersion = g.WebrendererVersion
	}
	webrendererValues["image"].(map[string]interface{})["version"] = currentFullVersion
	valuesYaml["webrenderer"] = webrendererValues

	valuesYaml["nameOverride"] = "webrenderer-" + g.WebrendererVersion

	// Spacial value for isolate webrenderer
	valuesYaml["global"].(map[string]interface{})["isBaseChart"] = false

	return yaml.Marshal(valuesYaml)
}
