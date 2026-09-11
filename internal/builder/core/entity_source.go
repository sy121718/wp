package core

// entity_source.go — 实体类型注册表（跨模块共用的「实体类型 → 字段白名单 + 解析器」）。
//
// 由来：内容模板与发布实例需要「按实体类型校验字段白名单」与「按实体取字段解析器」，
// 而类型白名单此前是内容模块的编译期常量，迫使这两个模块直接依赖内容模块。
// 新领域（如商品）接入时，这种硬依赖会把发布链锁死在内容模块上。
//
// 注册表把「类型清单 + 白名单 + 解析器」从编译期常量变成装配期注册：
// 每个领域模块注册自己的实体类型，构建层只认注册表，不认识具体领域。
// 字段白名单仍由各领域模块自己维护（不变量 4 的唯一来源不变）。

import (
	"context"
	"fmt"
	"strings"
	"sync"
)

// EntityFieldSource 一个实体类型的字段来源。
//
// 一个来源对应一个实体类型；同一模块支持多个类型时注册多个来源。
type EntityFieldSource interface {
	// EntityType 类型标识（如 article / product）。
	EntityType() string
	// FieldWhitelist 该类型允许绑定的字段路径（不变量 4 的唯一来源，只读拷贝）。
	FieldWhitelist() []string
	// ResolverFor 返回绑定单个实体的字段解析器。
	ResolverFor(ctx context.Context, entityID string) (ContentResolver, error)
}

// EntitySourceRegistry 实体类型注册表（进程级；装配期注册，运行期只读）。
type EntitySourceRegistry interface {
	// Register 注册一个实体类型来源；重复类型或非法来源返回错误。
	Register(src EntityFieldSource) error
	// Lookup 按类型取来源。
	Lookup(entityType string) (EntityFieldSource, bool)
	// IsValidType 类型是否已注册。
	IsValidType(entityType string) bool
	// FieldWhitelist 该类型字段白名单（只读拷贝；未知类型返回 nil）。
	FieldWhitelist(entityType string) []string
	// ResolverFor 取该类型某实体的字段解析器。
	ResolverFor(ctx context.Context, entityType, entityID string) (ContentResolver, error)
}

// entitySourceRegistry EntitySourceRegistry 的默认实现。
type entitySourceRegistry struct {
	mu      sync.RWMutex
	sources map[string]EntityFieldSource
}

// NewEntitySourceRegistry 新建空注册表。
func NewEntitySourceRegistry() EntitySourceRegistry {
	return &entitySourceRegistry{sources: map[string]EntityFieldSource{}}
}

// Register 实现 EntitySourceRegistry。
func (r *entitySourceRegistry) Register(src EntityFieldSource) error {
	if src == nil {
		return fmt.Errorf("实体来源为空")
	}
	// 标识不做归一化：存什么查什么，避免「注册时 trim、查询时不 trim」的不对称。
	// 首尾空白直接拒绝（标识本身必须规范），而不是悄悄改写。
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

// Lookup 实现 EntitySourceRegistry。
func (r *entitySourceRegistry) Lookup(entityType string) (EntityFieldSource, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	src, ok := r.sources[entityType]
	return src, ok
}

// IsValidType 实现 EntitySourceRegistry。
func (r *entitySourceRegistry) IsValidType(entityType string) bool {
	_, ok := r.Lookup(entityType)
	return ok
}

// FieldWhitelist 实现 EntitySourceRegistry。
func (r *entitySourceRegistry) FieldWhitelist(entityType string) []string {
	src, ok := r.Lookup(entityType)
	if !ok {
		return nil
	}
	return src.FieldWhitelist()
}

// ResolverFor 实现 EntitySourceRegistry。
func (r *entitySourceRegistry) ResolverFor(ctx context.Context, entityType, entityID string) (ContentResolver, error) {
	src, ok := r.Lookup(entityType)
	if !ok {
		return nil, fmt.Errorf("未注册的实体类型 %q", entityType)
	}
	return src.ResolverFor(ctx, entityID)
}
