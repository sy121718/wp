package core

import (
	"context"
	"strings"

	src "go_wp/pkg/builder/source"
)

type EntityFieldSource = src.EntityFieldSource

type FieldRef = src.FieldRef

type FieldBindingProvider interface {
	FieldBindings(node *Node) ([]FieldRef, error)
}

type entityLangKey struct{}

func WithBuildLang(ctx context.Context, lang string) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, entityLangKey{}, strings.TrimSpace(lang))
}

func BuildLang(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	if v, ok := ctx.Value(entityLangKey{}).(string); ok {
		return v
	}
	return ""
}

type EntitySourceRegistry = src.EntitySourceRegistry


// NewEntitySourceRegistry 构造实体来源注册表。
func NewEntitySourceRegistry() EntitySourceRegistry { return src.NewRegistry() }
