// Package contentcontract content 模块对外契约（0-A2）。
package contentcontract

import (
	"context"
	"sort"

	"go_wp/internal/builder/core"
	"go_wp/internal/module/content/dto"
)

// 内容实体字段白名单（docs/02-domain.md §2.3，不变量 4 的唯一入口）。
// 键 = entity_type，值 = 允许的字段路径（ContentResolver.ResolveString 与
// contenttemplate 的 Binding 校验共用同一白名单，禁止两处各维护一份）。
var fieldWhitelist = map[string][]string{
	"product":  {"name", "description", "price", "images", "seoTitle", "seoDescription"},
	"article":  {"title", "body", "excerpt", "featuredImage", "seoTitle", "seoDescription"},
	"category": {"name", "description", "image"},
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
type ContentService interface {
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
	ResolverFor(ctx context.Context, entityType, entityID string) (r core.ContentResolver, err error)
}
