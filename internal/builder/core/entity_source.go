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
	"go_wp/internal/builder/source"
	"strings"
	"sync"
)

// EntityFieldSource — 定义已搬到 internal/builder/source（issue #35），
// 此处保留别名：既有引用（组件、构建管线、工作台）不必跟着改。
type EntityFieldSource = source.EntityFieldSource

// FieldRef — 定义已搬到 internal/builder/source（issue #35），
// 此处保留别名：既有引用（组件、构建管线、工作台）不必跟着改。
type FieldRef = source.FieldRef

// FieldBindingProvider 由「在节点 props 里声明实体字段绑定」的组件实现。
//
// 实现方只负责把自身声明的绑定原样报出（不做合法性判断）——校验统一由
// builder.ValidateFieldRefs 按注册表执行，避免「谁校验」出现第二个来源。
type FieldBindingProvider interface {
	// FieldBindings 返回本节点声明的实体字段绑定（无声明返回 nil）。
	FieldBindings(node *Node) ([]FieldRef, error)
}

// entityLangKey 构建语言在上下文里的键（私有类型，避免与其它包的 key 冲突）。
type entityLangKey struct{}

// WithBuildLang 把本次构建的目标语言放进上下文。
//
// 实体字段解析器（如商品名/描述这类作者填写文本）据此按语言取内容译文
// （sys_translation，语境 实体.字段名）；未设置时解析器返回原文，不做翻译。
func WithBuildLang(ctx context.Context, lang string) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, entityLangKey{}, strings.TrimSpace(lang))
}

// BuildLang 取上下文里的构建语言（未设置 / 为空返回空串 = 原文）。
func BuildLang(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	if v, ok := ctx.Value(entityLangKey{}).(string); ok {
		return v
	}
	return ""
}

// EntitySourceRegistry — 定义已搬到 internal/builder/source（issue #35），
// 此处保留别名：既有引用（组件、构建管线、工作台）不必跟着改。
type EntitySourceRegistry = source.EntitySourceRegistry

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
