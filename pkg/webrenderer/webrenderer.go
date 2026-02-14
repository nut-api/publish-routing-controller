package webrenderer

import (
	"context"

	corev1 "k8s.io/api/core/v1"
)

type Webrenderer interface {
	NewWebrenderer(ctx context.Context, version string, namespace string) Webrenderer
	GetAndCreateIfNotExists(context.Context) (ServingWebrenderer, error)
	DeleteWebrenderer(context.Context) error
	UpdateWebrenderer(ctx context.Context, config *corev1.ConfigMap) error
	IsReady(context.Context) (bool, error)
}

// ServingWebrenderer defines the structure of a webrenderer being served
type ServingWebrenderer struct {
	Version     int    `json:"version"`
	VersionFull string `json:"versionFull"`
	Host        string `json:"host"`
}
