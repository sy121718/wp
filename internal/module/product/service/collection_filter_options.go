// collection_filter_options.go — 集合源筛选选项的实现（issue #27）。
//
// 筛选栏的选项来自**本模块的表**（分类 / 品牌 / 标签 / 属性组），构建期一次取回，
// 展示名按构建语言取译文（与集合项字段同一套 helper，不另写一份取词逻辑）。
package productservice

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"go_wp/internal/builder/core"
	productcontract "go_wp/internal/module/product/contract"
)

// 编译期断言：集合源筛选选项能力（issue #27）。
var _ core.CollectionFilterOptionsProvider = (*Service)(nil)

// CollectionFilterOptions 实现 core.CollectionFilterOptionsProvider。
//
// source 由集合源注册表按「注册时各提供方自报的集合源」派发进来：本模块只拥有
// 商品集合源一个，其它源（哪怕也落在本模块的元数据聚合里）不是本实现在答 ——
// 不猜、不报错，返回空选项（与注册表「无该能力/无该源 → 空选项」的规则一致）。
func (s *Service) CollectionFilterOptions(ctx context.Context, source, projectID string) (out core.CollectionFilterOptions, err error) {
	if strings.TrimSpace(source) != productcontract.CollectionSourceProduct {
		return out, nil
	}
	projectID = strings.TrimSpace(projectID)
	if projectID == "" {
		return out, fmt.Errorf("缺少工程 ID，无法给出筛选选项")
	}
	lang := core.BuildLang(ctx)

	categories, cerr := s.m.ListCategories(ctx, projectID, "")
	if cerr != nil {
		return out, cerr
	}
	for _, row := range categories {
		parent := ""
		if row.ParentID != nil {
			parent = *row.ParentID
		}
		out.Categories = append(out.Categories, core.CollectionFilterChoice{
			ID: row.ID, Name: filterOptionName(s.categoryValues(ctx, lang, row), row.Name), ParentID: parent,
		})
	}

	// 全量取（0, 0）：筛选选项要列出工程里所有品牌 / 标签，不是第一页。
	brands, berr := s.m.ListBrands(ctx, projectID, "", 0, 0)
	if berr != nil {
		return out, berr
	}
	for _, row := range brands {
		out.Brands = append(out.Brands, core.CollectionFilterChoice{
			ID: row.ID, Name: filterOptionName(s.brandValues(ctx, lang, row), row.Name),
		})
	}

	tags, terr := s.m.ListTags(ctx, projectID, "", "", 0, 0)
	if terr != nil {
		return out, terr
	}
	for _, row := range tags {
		out.Tags = append(out.Tags, core.CollectionFilterChoice{
			ID: row.ID, Name: filterOptionName(s.tagValues(ctx, lang, row), row.Name),
		})
	}

	attrs, aerr := s.m.ListAttributesByProject(ctx, projectID)
	if aerr != nil {
		return out, aerr
	}
	for _, row := range attrs {
		// 只有**参与变体**的属性组可筛：不参与变体的属性在商品侧只是一个标记，
		// 变体的 option_values 里没有它，筛了必然恒空 —— 与其给一个永远筛不出东西的选项，
		// 不如不显示（参考站上「价格」这类维度也不是靠属性表达的）。
		if !row.IsVariation {
			continue
		}
		labels := s.attributeValueTranslations(ctx, lang, row)
		attr := core.CollectionFilterAttributeGroup{
			Key: row.Key, Name: filterOptionName(s.attributeValues(ctx, lang, row), row.Name),
		}
		for _, value := range decodeAttributeValueOptions(row.Values) {
			name := strings.TrimSpace(labels[value.Key])
			if name == "" {
				name = value.Label
			}
			attr.Values = append(attr.Values, core.CollectionFilterChoice{
				Key: value.Key, Name: name,
			})
		}
		if len(attr.Values) > 0 {
			out.Attributes = append(out.Attributes, attr)
		}
	}
	return out, nil
}

// attributeValueOption 属性值定义的最小结构（只要渲染筛选项需要的两个字段）。
type attributeValueOption struct {
	Key   string `json:"key"`
	Label string `json:"label"`
}

// decodeAttributeValueOptions 属性值 JSONB → 选项列表（形状不对返回空，不 panic）。
func decodeAttributeValueOptions(raw json.RawMessage) []attributeValueOption {
	if len(raw) == 0 {
		return nil
	}
	var rows []attributeValueOption
	if err := json.Unmarshal(raw, &rows); err != nil {
		return nil
	}
	out := make([]attributeValueOption, 0, len(rows))
	for _, row := range rows {
		if strings.TrimSpace(row.Key) == "" {
			continue
		}
		out = append(out, row)
	}
	return out
}

// filterOptionName 取译文里的展示名，无译文逐字节回退原文（与集合项字段同一口径）。
func filterOptionName(values map[string]string, fallback string) string {
	if name := strings.TrimSpace(values["name"]); name != "" {
		return name
	}
	return fallback
}
