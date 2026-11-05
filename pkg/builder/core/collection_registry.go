package core

import (
	"cmp"
	"context"
	"fmt"
	"slices"
	"strings"
	"sync"
)

type CollectionSourceProvider interface {
	CollectionResolver
	CollectionSchemaProvider
}

type CollectionRegistry interface {
	CollectionResolver
	CollectionSchemaProvider

	ResolveCollectionPage(ctx context.Context, source string, q CollectionQuery) (CollectionPage, error)

	CollectionFilterOptionsProvider

	Register(p CollectionSourceProvider) error

	Sources(ctx context.Context) ([]string, error)
}

type collectionRegistry struct {
	mu       sync.RWMutex
	ordered  []CollectionSourceProvider
	bySource map[string]CollectionSourceProvider
}

func NewCollectionRegistry() CollectionRegistry {
	return &collectionRegistry{bySource: map[string]CollectionSourceProvider{}}
}

func (r *collectionRegistry) Register(p CollectionSourceProvider) error {
	if p == nil {
		return fmt.Errorf("集合源提供方为空")
	}

	schemas, err := p.CollectionSchemas(context.Background())
	if err != nil {
		return fmt.Errorf("集合源元数据不可用: %w", err)
	}
	if len(schemas) == 0 {
		return fmt.Errorf("集合源提供方未声明任何集合源")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, s := range schemas {
		source := strings.TrimSpace(s.Source)
		if source == "" || source != s.Source {
			return fmt.Errorf("集合源标识非法：%q（不允许为空或含首尾空白）", s.Source)
		}
		if _, dup := r.bySource[source]; dup {
			return fmt.Errorf("集合源 %q 重复注册", source)
		}
		r.bySource[source] = p
	}
	r.ordered = append(r.ordered, p)
	return nil
}

func (r *collectionRegistry) ResolveCollection(ctx context.Context, source string, filter map[string]string) (items []map[string]any, err error) {
	p, err := r.providerFor(ctx, source)
	if err != nil {
		return nil, err
	}
	return p.ResolveCollection(ctx, source, filter)
}

func (r *collectionRegistry) ResolveCollectionPage(ctx context.Context, source string, q CollectionQuery) (CollectionPage, error) {
	p, err := r.providerFor(ctx, source)
	if err != nil {
		return CollectionPage{}, err
	}
	if pager, ok := p.(CollectionPager); ok {
		return pager.ResolveCollectionPage(ctx, source, q)
	}
	items, rerr := p.ResolveCollection(ctx, source, q.Filter)
	if rerr != nil {
		return CollectionPage{}, rerr
	}
	if q.Offset > 0 {
		if q.Offset >= len(items) {
			items = nil
		} else {
			items = items[q.Offset:]
		}
	}
	if q.Limit > 0 && len(items) > q.Limit {
		items = items[:q.Limit]
	}
	return CollectionPage{Items: items, Total: -1}, nil
}

func (r *collectionRegistry) CollectionFilterOptions(ctx context.Context, source, projectID string) (CollectionFilterOptions, error) {
	source = strings.TrimSpace(source)
	if source == "" {
		return CollectionFilterOptions{}, nil
	}
	p, ok := r.lookup(source)
	if !ok {
		return CollectionFilterOptions{}, nil
	}
	provider, ok := p.(CollectionFilterOptionsProvider)
	if !ok {
		return CollectionFilterOptions{}, nil
	}
	return provider.CollectionFilterOptions(ctx, source, projectID)
}

func (r *collectionRegistry) CollectionSchemas(ctx context.Context) ([]CollectionSchema, error) {
	r.mu.RLock()
	providers := make([]CollectionSourceProvider, len(r.ordered))
	copy(providers, r.ordered)
	r.mu.RUnlock()

	out := make([]CollectionSchema, 0, len(r.bySource))
	for _, p := range providers {
		schemas, err := p.CollectionSchemas(ctx)
		if err != nil {
			return nil, err
		}
		out = append(out, schemas...)
	}
	slices.SortStableFunc(out, func(a, b CollectionSchema) int { return cmp.Compare(a.Source, b.Source) })
	return out, nil
}

func (r *collectionRegistry) Sources(ctx context.Context) ([]string, error) {
	schemas, err := r.CollectionSchemas(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(schemas))
	for _, s := range schemas {
		out = append(out, s.Source)
	}
	return out, nil
}

func (r *collectionRegistry) providerFor(ctx context.Context, source string) (CollectionSourceProvider, error) {
	if p, ok := r.lookup(source); ok {
		return p, nil
	}
	available, err := r.Sources(ctx)
	if err != nil {
		return nil, err
	}
	if len(available) == 0 {
		return nil, fmt.Errorf("未知集合源 %q（当前没有已注册的集合源）", source)
	}
	return nil, fmt.Errorf("未知集合源 %q（可用：%s）", source, strings.Join(available, "、"))
}

func (r *collectionRegistry) lookup(source string) (CollectionSourceProvider, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	p, ok := r.bySource[source]
	return p, ok
}

var (
	_ CollectionResolver              = (*collectionRegistry)(nil)
	_ CollectionSchemaProvider        = (*collectionRegistry)(nil)
	_ CollectionFilterOptionsProvider = (*collectionRegistry)(nil)
	_ CollectionRegistry              = (*collectionRegistry)(nil)
)

type buildProjectKey struct{}

func WithBuildProjectID(ctx context.Context, projectID string) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, buildProjectKey{}, strings.TrimSpace(projectID))
}

func BuildProjectID(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	if v, ok := ctx.Value(buildProjectKey{}).(string); ok {
		return v
	}
	return ""
}
