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
	rows, err := s.m.List(ctx, entityType, 100, 0)
	if err != nil {
		return nil, err
	}
	items = make([]map[string]any, 0, len(rows))
	for _, r := range rows {
		data := map[string]any{}
		if uerr := json.Unmarshal(r.Data, &data); uerr != nil {
			return nil, fmt.Errorf("%s: %w", contentenums.ErrDataInvalid, uerr)
		}
		// 过滤（等值匹配；键已由组件声明白名单约束）。
		if !matchFilter(data, filter) {
			continue
		}
		// 注入系统字段（id/slug/revision 供模板展示）。
		out := map[string]any{"id": r.ID, "slug": r.Slug, "revision": r.Revision}
		for k, v := range data {
			out[k] = v
		}
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

// 编译期断言：Service 实现 core.CollectionResolver。
var _ core.CollectionResolver = (*Service)(nil)
