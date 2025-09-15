package webrenderer

import "context"

type Webrenderer interface {
	GetAndCreateIfNotExists(context.Context) error
	DeleteWebrenderer(context.Context) error
}
