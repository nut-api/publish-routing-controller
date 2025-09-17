package webrenderer

import (
	"context"

	"sigs.k8s.io/controller-runtime/pkg/client"
)

type Webrenderer interface {
	NewWebrenderer(client client.Client, version string) Webrenderer
	GetAndCreateIfNotExists(context.Context) error
	DeleteWebrenderer(context.Context) error
	IsReady(context.Context) (bool, error)
}
