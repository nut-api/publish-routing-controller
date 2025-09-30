package webrenderer

import (
	"context"
)

type Webrenderer interface {
	NewWebrenderer(ctx context.Context, version string) Webrenderer
	GetAndCreateIfNotExists(context.Context) error
	DeleteWebrenderer(context.Context) error
	IsReady(context.Context) (bool, error)
}
