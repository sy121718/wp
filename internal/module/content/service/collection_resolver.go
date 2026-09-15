package contentservice

// collection_resolver.go — core.CollectionResolver 实现（0-A2 + L2）。
// source="content:{entityType}" → 查 contents 表该类型实体列表 → 展开 data
// 为字段值列表（构建期静态填入，供插件组件集合渲染）。

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"go_wp/internal/builder/core"
	contentcontract "go_wp/internal/module/content/contract"
	contentenums "go_wp/internal/module/content/enums"
)

// collectionSourcePrefix 内容实体集合源前缀。
const collectionSourcePrefix = "content:"

// ResolveCollection 实现 core.CollectionResolver：按集合源查实体列表。
// filter 为白名单等值过滤（MVP：仅支持空 filter 或按 entity_type 外的
// 数据字段等值匹配；字段值白名单由调用方组件声明控制）。
func (s *Service) ResolveCollection(ctx context.Context, source string, filter map[string]string) (items []map[string]any, err error) {
	entityType, ok := strings.CutPrefix(source, collectionSourcePrefix)
	if !ok || !contentcontract.IsValidType(entityType) {
		return nil, fmt.Errorf("%s: %q（期望 content:{product|article|category}）", contentenums.ErrInvalidType, source)
	}
	// 列投影 + 筛选下推（审计 PERF-008）：此前先 List 取回 100 行**整行**（data 里含正文
	// 全文）再在 Go 里过滤，等于为了渲染几张卡片把正文都读了一遍。
	// 投影字段取集合白名单（自动排除 body / focusKeyword），筛选条件下沉成 SQL。
	fields := contentcontract.CollectionFieldWhitelist(entityType)
	rows, err := s.m.ListForCollection(ctx, entityType, fields, filter, 0)
	if err != nil {
		return nil, err
	}
	items = make([]map[string]any, 0, len(rows))
	for _, r := range rows {
		out := map[string]any{}
		if len(r.Fields) > 0 {
			if uerr := json.Unmarshal(r.Fields, &out); uerr != nil {
				return nil, fmt.Errorf("%s: %w", contentenums.ErrDataInvalid, uerr)
			}
		}
		// 复查（防御，不是主路径）：筛选已下推到 SQL，这里再比一次是为了让
		// 「SQL 表达得对不对」永远不改变最终结果 —— 一旦下推的条件与内存语义有偏差，
		// 表现是少渲染几张卡片（复查会拦下），而不是渲染出不该出现的条目。
		if !matchFilter(out, filter) {
			continue
		}
		// 注入系统字段（id/slug/revision 供模板展示）。
		out["id"], out["slug"], out["revision"] = r.ID, r.Slug, r.Revision
		items = append(items, out)
	}
	return items, nil
}

// matchFilter 等值过滤（全部键匹配才保留）。
func matchFilter(data map[string]any, filter map[string]string) bool {
	for k, want := range filter {
		v, ok := data[k]
		if !ok || scalarString(v) != want {
			return false
		}
	}
	return true
}

// CollectionSchemas 实现 core.CollectionSchemaProvider：暴露内容集合源与字段白名单。
// 内置组件（如 cardstack）用它在构建期校验字段映射，工作台用它渲染字段下拉。
func (s *Service) CollectionSchemas(_ context.Context) ([]core.CollectionSchema, error) {
	types := contentcontract.EntityTypes()
	out := make([]core.CollectionSchema, 0, len(types))
	for _, t := range types {
		out = append(out, core.CollectionSchema{
			Source: collectionSourcePrefix + t,
			Label:  entityTypeLabel(t),
			// 集合项字段用**集合白名单**：下拉里不该出现正文与主关键词，
			// 它们既不在查询投影里、也不会被渲染。三处（下拉 / 构建期校验 / SQL 投影）
			// 共用这一个定义，才不会出现「选得到、构建出来是空」的静默失败。
			Fields: contentcontract.CollectionFieldWhitelist(t),
		})
	}
	return out, nil
}

// entityTypeLabel 内容类型的展示名（工作台集合源/字段下拉）。
func entityTypeLabel(entityType string) string {
	switch entityType {
	case "article":
		return "文章列表"
	case "product":
		return "商品列表"
	case "category":
		return "分类列表"
	default:
		return entityType
	}
}

// 编译期断言：Service 实现集合解析与集合元数据两个契约。
var (
	_ core.CollectionResolver       = (*Service)(nil)
	_ core.CollectionSchemaProvider = (*Service)(nil)
)
