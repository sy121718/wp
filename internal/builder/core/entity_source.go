package core

import (
	"context"
	"fmt"
	"go_wp/internal/builder/source"
	"strings"
	"sync"
)

type EntityFieldSource = source.EntityFieldSource

type FieldRef = source.FieldRef

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

type EntitySourceRegistry = source.EntitySourceRegistry

type entitySourceRegistry struct {
	mu      sync.RWMutex
	sources map[string]EntityFieldSource
}

func NewEntitySourceRegistry() EntitySourceRegistry {
	return &entitySourceRegistry{sources: map[string]EntityFieldSource{}}
}

func (r *entitySourceRegistry) Register(src EntityFieldSource) error {
	if src == nil {
		return fmt.Errorf("实体来源为空")
	}

	t := src.EntityType()
	if t == "" || strings.TrimSpace(t) != t {
		return fmt.Errorf("实体类型标识非法：%q（不允许为空或含首尾空白）", t)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, dup := r.sources[t]; dup {
		return fmt.Errorf("实体类型 %q 重复注册", t)
	}
	r.sources[t] = src
	return nil
}

func (r *entitySourceRegistry) Lookup(entityType string) (EntityFieldSource, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	src, ok := r.sources[entityType]
	return src, ok
}

func (r *entitySourceRegistry) IsValidType(entityType string) bool {
	_, ok := r.Lookup(entityType)
	return ok
}

func (r *entitySourceRegistry) FieldWhitelist(entityType string) []string {
	src, ok := r.Lookup(entityType)
	if !ok {
		return nil
	}
	return src.FieldWhitelist()
}

func (r *entitySourceRegistry) ResolverFor(ctx context.Context, entityType, entityID string) (ContentResolver, error) {
	src, ok := r.Lookup(entityType)
	if !ok {
		return nil, fmt.Errorf("未注册的实体类型 %q", entityType)
	}
	return src.ResolverFor(ctx, entityID)
}
