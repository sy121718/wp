package source

import (
	"context"
	"fmt"
	"strings"
	"sync"
)

type ContentResolver interface {
	ResolveString(field string) (string, error)
}

type EntityFieldSource interface {
	EntityType() string

	FieldWhitelist() []string

	ResolverFor(ctx context.Context, entityID string) (ContentResolver, error)
}

type FieldRef struct {
	EntityType string

	Field string

	CollectionSource string
}

type EntitySourceRegistry interface {
	Register(src EntityFieldSource) error

	Lookup(entityType string) (EntityFieldSource, bool)

	IsValidType(entityType string) bool

	FieldWhitelist(entityType string) []string

	ResolverFor(ctx context.Context, entityType, entityID string) (ContentResolver, error)
}

// Registry 是 EntitySourceRegistry 的默认实现。
type Registry struct {
	mu      sync.RWMutex
	sources map[string]EntityFieldSource
}

// NewRegistry 构造实体来源注册表。
func NewRegistry() *Registry {
	return &Registry{sources: map[string]EntityFieldSource{}}
}

func (r *Registry) Register(src EntityFieldSource) error {
	if src == nil {
		return fmt.Errorf("实体来源为空")
	}
	t := src.EntityType()
	if t == "" || strings.TrimSpace(t) != t {
		return fmt.Errorf("实体类型名不合法: %q", t)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.sources == nil {
		r.sources = map[string]EntityFieldSource{}
	}
	if _, exists := r.sources[t]; exists {
		return fmt.Errorf("实体类型 %q 重复注册", t)
	}
	r.sources[t] = src
	return nil
}

func (r *Registry) Lookup(entityType string) (EntityFieldSource, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	s, ok := r.sources[entityType]
	return s, ok
}

func (r *Registry) IsValidType(entityType string) bool {
	_, ok := r.Lookup(entityType)
	return ok
}

func (r *Registry) FieldWhitelist(entityType string) []string {
	s, ok := r.Lookup(entityType)
	if !ok {
		return nil
	}
	return s.FieldWhitelist()
}

func (r *Registry) ResolverFor(ctx context.Context, entityType, entityID string) (ContentResolver, error) {
	s, ok := r.Lookup(entityType)
	if !ok {
		return nil, fmt.Errorf("未注册的实体类型 %q", entityType)
	}
	return s.ResolverFor(ctx, entityID)
}
