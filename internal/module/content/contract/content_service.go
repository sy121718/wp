// Package contentcontract content 模块对外契约（0-A2）。
package contentcontract

import (
	"context"
	"sort"

	"go_wp/internal/builder/source"
	"go_wp/internal/module/content/dto"
)

// 内容实体字段白名单（docs/02-domain.md §2.3，不变量 4 的唯一入口）。
// 键 = entity_type，值 = 允许的字段路径（ContentResolver.ResolveString 与
// contenttemplate 的 Binding 校验共用同一白名单，禁止两处各维护一份）。
var fieldWhitelist = map[string][]string{
	"article": {"title", "body", "excerpt", "featuredImage", "seoTitle", "seoDescription"},
}

// EntityTypes 全部支持的内容类型（字典序）。
func EntityTypes() []string {
	out := make([]string, 0, len(fieldWhitelist))
	for t := range fieldWhitelist {
		out = append(out, t)
	}
	sort.Strings(out)
	return out
}

// IsValidType 内容类型是否合法。
func IsValidType(t string) bool {
	_, ok := fieldWhitelist[t]
	return ok
}

// IsValidField 字段是否在该类型白名单内（不变量 4 校验）。
func IsValidField(entityType, field string) bool {
	for _, f := range fieldWhitelist[entityType] {
		if f == field {
			return true
		}
	}
	return false
}

// FieldWhitelist 返回该类型的字段白名单（只读拷贝，防调用方篡改）。
func FieldWhitelist(entityType string) []string {
	fields := fieldWhitelist[entityType]
	out := make([]string, len(fields))
	copy(out, fields)
	return out
}

// ContentService CMS 内容管理契约 + 构建期内容解析器工厂。
// ContentService 内容完整契约；issue #35 起嵌入 ContentDataSource（构建期只给受限的一半）。
type ContentService interface {
	// ContentDataSource 构建期数据源（只读）。
	ContentDataSource

	// Create 新建内容实体（revision=1）。
	Create(ctx context.Context, req *contentdto.CreateReq) (res *contentdto.ContentResp, err error)
	// Update 更新内容（revision 递增）。
	Update(ctx context.Context, req *contentdto.UpdateReq) (res *contentdto.ContentResp, err error)
	// Get 按 ID 查询。
	Get(ctx context.Context, req *contentdto.GetReq) (res *contentdto.ContentResp, err error)
	// List 按类型分页列表。
	List(ctx context.Context, req *contentdto.ListReq) (list []*contentdto.ContentResp, err error)
	// Delete 删除实体。
	Delete(ctx context.Context, req *contentdto.DeleteReq) (err error)
	// ResolverFor 返回绑定单个实体的内容解析器（构建期注入：presentation
	// 模块构建 DocumentSnapshot 时按 entityType+entityID 取实体解析 Binding）。
	ResolverFor(ctx context.Context, entityType, entityID string) (r source.ContentResolver, err error)
	// RegisterEntityTypes 把本模块支持的实体类型注册进实体类型注册表（装配期调用）。
	// 注册后，构建层（内容模板 / 发布实例）不再直接依赖本模块的类型判断函数。
	RegisterEntityTypes(reg source.EntitySourceRegistry) error
	// SetDependencyInvalidator 注入依赖失效扇出端口（编排层装配，可空）。
	// 内容实体变更后由本模块推导依赖源键（实体自身 + 所属集合）并交给端口，
	// 端口负责按依赖表反查受影响产物（PIPE-3）。
	SetDependencyInvalidator(inv DependencyInvalidator)
}

// DependencyInvalidator 依赖失效扇出入口（由编排层注入实现，通常是 pipeline.Fanout）。
//
// 契约刻意保持极简（kind + key）：content 模块只负责声明「我是哪个依赖源」，
// 具体反查与重建由发布来源模块完成，避免 content 反向依赖 page/presentation。
// 实现必须容错——失效失败不能影响已经成功的内容写入。
type DependencyInvalidator interface {
	// Invalidate 标记依赖源 (kind,key) 变更；kind/key 语义见
	// docs/03-pipeline.md §8.1 与 pipeline.DepKind* 常量。
	Invalidate(ctx context.Context, kind, key string)
}
