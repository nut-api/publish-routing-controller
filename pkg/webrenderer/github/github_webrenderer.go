package github

import (
	"context"
	"os"
	"strings"

	"github.com/nut-api/publish-routing-controller.git/pkg/webrenderer"
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

func (g *WebrendererGithub) NewWebrenderer(version string) webrenderer.Webrenderer {
	return &WebrendererGithub{
		Client:             g.Client,
		GithubClient:       g.GithubClient,
		WebrendererVersion: version,
		WebrendererPath:    "argo-app/webrenderer-" + version,
	}
}

func (g *WebrendererGithub) GetAndCreateIfNotExists(ctx context.Context) error {
	l := logf.FromContext(ctx)
	// Check if ArgoCD app YAML exists in WebrendererPath
	appPath := g.WebrendererPath + "/app.yaml"
	if _, err := os.Stat(appPath); err == nil {
		l.Info("ArgoCD app YAML already exists", "path", appPath)
		// File exists
		return nil
	} else if !os.IsNotExist(err) {
		// Other error
		return err
	}

	// Get the ArgoCD app YAML from the template
	appYaml, err := GetArgoCDAppYAML(g.WebrendererVersion)
	if err != nil {
		return err
	}

	// Ensure directory exists
	if err := os.MkdirAll(g.WebrendererPath, 0755); err != nil {
		return err
	}
	l.Info("Creating ArgoCD app YAML", "path", appPath)
	// Write the YAML file
	return os.WriteFile(appPath, []byte(appYaml), 0644)
}

func (g *WebrendererGithub) DeleteWebrenderer(context.Context) error {
	//Remove the directory
	return os.RemoveAll(g.WebrendererPath)
}

func (g *WebrendererGithub) IsReady(ctx context.Context) (bool, error) {
	// app := &argocdapp.Application{}
	// err := g.Client.Get(ctx, client.ObjectKey{Name: "webrenderer-" + g.WebrendererVersion, Namespace: "argocd"}, app)
	// if err != nil {
	// 	return false, err
	// }
	// if app.Status.Sync.Status != "Synced" || app.Status.Health.Status != "Healthy" {
	// 	return false, nil
	// }
	// return true, nil

	// check if the app.yaml file exists
	appPath := g.WebrendererPath + "/app.yaml"
	if _, err := os.Stat(appPath); err == nil {
		// File exists
		return true, nil
	} else if !os.IsNotExist(err) {
		// Other error
		return false, err
	}
	// File does not exist
	return false, nil
}

func GetArgoCDAppYAML(version string) (string, error) {
	// Get from file on template/app.yaml
	data, err := os.ReadFile("argo-app/template/app.yaml")
	if err != nil {
		return "", err
	}
	// Replace VERSION with version
	appYaml := string(data)
	appYaml = strings.ReplaceAll(appYaml, "VERSION", version)
	return appYaml, nil
}
