package productservice

// product_translation.go — 商品域翻译候选收集（issue #12）。
//
// 翻译入口在商品列表行内「多语言」按钮（/admin/products/translations）：页面按
// 「商品 + 该商品引用的分类/品牌/标签/属性」列出全部可翻译文本，译文写入
// sys_translation（语境 "实体类型.字段名"，docs/06-D §7.5/§7.8）。
//
// 与构建期同源（唯一来源，不另写一份白名单）：
//   - 哪些字段可翻译 → productcontract.TranslatableFields；
//   - 语境界定       → productcontract.FieldContext；
//   - 跳过规则       → i18n.ShouldTranslateContent（纯数字/纯符号/空白不进表）。
//
// 本文件只做「收集候选」，写入由调用方经 pkg/i18n.ContentWriter 完成（与页面翻译
// 工作台共用同一写入路径与 hash 校验）。

import (
	"context"
	"strings"

	productcontract "go_wp/internal/module/product/contract"
	productmodel "go_wp/internal/module/product/model"
	"go_wp/pkg/i18n"
)

// ProductTranslationCandidates 单个商品及其引用实体的全部可翻译文本。
//
// 返回顺序：商品自身 → 分类（商品引用顺序）→ 品牌 → 标签 → 属性组。
// 实体不存在 / 引用已失效 → 跳过该实体，不报错（与详情页读取口径一致）。
func (s *Service) ProductTranslationCandidates(ctx context.Context, productID string) (list []productcontract.TranslationCandidate, err error) {
	e, gerr := s.m.Get(ctx, productID, "")
	if gerr != nil {
		return nil, mapNotFound(gerr)
	}
	return s.translationCandidatesForProduct(ctx, e)
}

// ProjectTranslationCandidates 工程内全部商品域可翻译文本（按 (hash, context) 去重）。
//
// 用于「整个商品域都翻一遍」的场景；单商品入口用 ProductTranslationCandidates。
func (s *Service) ProjectTranslationCandidates(ctx context.Context, projectID string) (list []productcontract.TranslationCandidate, err error) {
	if strings.TrimSpace(projectID) == "" {
		return nil, nil
	}
	rows, lerr := s.m.List(ctx, projectID, "", "", 1000, 0)
	if lerr != nil {
		return nil, lerr
	}
	seen := map[string]bool{}
	for _, row := range rows {
		cands, cerr := s.translationCandidatesForProduct(ctx, row)
		if cerr != nil {
			return nil, cerr
		}
		list = appendUniqueCandidates(list, seen, cands)
	}
	// 未被任何商品引用的分类 / 品牌 / 标签 / 属性也要可翻译（先建后挂是常态）。
	extra, eerr := s.orphanTranslationCandidates(ctx, projectID, seen)
	if eerr != nil {
		return nil, eerr
	}
	return append(list, extra...), nil
}

// translationCandidatesForProduct 单个商品的候选（商品自身 + 引用实体）。
func (s *Service) translationCandidatesForProduct(ctx context.Context, e *productmodel.ProductEntity) (list []productcontract.TranslationCandidate, err error) {
	if e == nil {
		return nil, nil
	}
	list = append(list, productTextCandidates(e)...)

	categoryRows, cerr := s.m.ListCategoriesByIDs(ctx, decodeStrings(e.CategoryIDs))
	if cerr != nil {
		return nil, cerr
	}
	for _, row := range categoryRows {
		list = append(list, entityTextCandidates(productcontract.EntityTypeCategory, row.ID, row.Name,
			map[string]string{"name": row.Name, "description": row.Description, "seoTitle": row.SEOTitle})...)
	}
	if e.BrandID != nil && strings.TrimSpace(*e.BrandID) != "" {
		if row, berr := s.m.GetBrand(ctx, *e.BrandID); berr == nil {
			list = append(list, entityTextCandidates(productcontract.EntityTypeBrand, row.ID, row.Name,
				map[string]string{"name": row.Name, "description": row.Description, "seoTitle": row.SEOTitle})...)
		}
	}
	tagRows, terr := s.m.ListTagsByIDs(ctx, decodeStrings(e.TagIDs))
	if terr != nil {
		return nil, terr
	}
	for _, row := range tagRows {
		list = append(list, entityTextCandidates(productcontract.EntityTypeTag, row.ID, row.Name,
			map[string]string{"name": row.Name})...)
	}
	attrRows, aerr := s.m.ListAttributesByIDs(ctx, decodeStrings(e.AttributeIDs))
	if aerr != nil {
		return nil, aerr
	}
	for _, row := range attrRows {
		list = append(list, attributeTextCandidates(row)...)
	}
	return list, nil
}

// orphanTranslationCandidates 工程内未被商品引用的分类 / 品牌 / 标签 / 属性的候选。
func (s *Service) orphanTranslationCandidates(ctx context.Context, projectID string, seen map[string]bool) (list []productcontract.TranslationCandidate, err error) {
	categories, cerr := s.m.ListCategories(ctx, projectID, "")
	if cerr != nil {
		return nil, cerr
	}
	for _, row := range categories {
		list = appendUniqueCandidates(list, seen, entityTextCandidates(productcontract.EntityTypeCategory, row.ID, row.Name,
			map[string]string{"name": row.Name, "description": row.Description, "seoTitle": row.SEOTitle}))
	}
	brands, berr := s.m.ListBrands(ctx, projectID, "")
	if berr != nil {
		return nil, berr
	}
	for _, row := range brands {
		list = appendUniqueCandidates(list, seen, entityTextCandidates(productcontract.EntityTypeBrand, row.ID, row.Name,
			map[string]string{"name": row.Name, "description": row.Description, "seoTitle": row.SEOTitle}))
	}
	tags, terr := s.m.ListTags(ctx, projectID, "", "")
	if terr != nil {
		return nil, terr
	}
	for _, row := range tags {
		list = appendUniqueCandidates(list, seen, entityTextCandidates(productcontract.EntityTypeTag, row.ID, row.Name,
			map[string]string{"name": row.Name}))
	}
	attrs, aerr := s.m.ListAttributesByProject(ctx, projectID)
	if aerr != nil {
		return nil, aerr
	}
	for _, row := range attrs {
		list = appendUniqueCandidates(list, seen, attributeTextCandidates(row))
	}
	return list, nil
}

// appendUniqueCandidates 按 (hash, context) 去重追加。
func appendUniqueCandidates(dst []productcontract.TranslationCandidate, seen map[string]bool, add []productcontract.TranslationCandidate) []productcontract.TranslationCandidate {
	for _, c := range add {
		key := i18n.ContentIndexKey(c.SourceHash, c.Context)
		if seen[key] {
			continue
		}
		seen[key] = true
		dst = append(dst, c)
	}
	return dst
}

// productTextCandidates 商品自身可翻译字段的候选（图集 alt 逐元素展开）。
func productTextCandidates(e *productmodel.ProductEntity) []productcontract.TranslationCandidate {
	values := map[string]string{
		"name":        e.Name,
		"subtitle":    e.Subtitle,
		"description": descriptionHTML(e.Description),
	}
	list := make([]productcontract.TranslationCandidate, 0, 8)
	for _, field := range productcontract.TranslatableFields(productcontract.EntityTypeProduct) {
		if field == "imageAlts" {
			for _, alt := range decodeStrings(e.ImageAlts) {
				if c, ok := makeCandidate(productcontract.EntityTypeProduct, e.ID, e.Name, field, alt); ok {
					list = append(list, c)
				}
			}
			continue
		}
		if c, ok := makeCandidate(productcontract.EntityTypeProduct, e.ID, e.Name, field, values[field]); ok {
			list = append(list, c)
		}
	}
	return list
}

// entityTextCandidates 一个标量文本实体（分类 / 品牌 / 标签）的候选。
//
// values 是「字段名 → 原文」映射（原样传入，不做翻译）；
// 字段集合仍取自 contract 的唯一来源，不在此另列。
func entityTextCandidates(entityType, id, name string, values map[string]string) []productcontract.TranslationCandidate {
	list := make([]productcontract.TranslationCandidate, 0, len(values))
	for _, field := range productcontract.TranslatableFields(entityType) {
		if c, ok := makeCandidate(entityType, id, name, field, values[field]); ok {
			list = append(list, c)
		}
	}
	return list
}

// attributeTextCandidates 属性组的候选：组名 + 属性值展示文本逐元素。
func attributeTextCandidates(e *productmodel.ProductAttributeEntity) []productcontract.TranslationCandidate {
	if e == nil {
		return nil
	}
	list := make([]productcontract.TranslationCandidate, 0, 4)
	if c, ok := makeCandidate(productcontract.EntityTypeAttribute, e.ID, e.Name, "name", e.Name); ok {
		list = append(list, c)
	}
	for _, v := range normalizeValuesFromRaw(e.Values) {
		if c, ok := makeCandidate(productcontract.EntityTypeAttribute, e.ID, e.Name, "values", v.Label); ok {
			list = append(list, c)
		}
	}
	return list
}

// makeCandidate 组装一条候选（跳过规则命中 / 不在白名单内 → false）。
func makeCandidate(entityType, id, name, field, source string) (productcontract.TranslationCandidate, bool) {
	if !productcontract.IsTranslatableField(entityType, field) {
		return productcontract.TranslationCandidate{}, false
	}
	if !i18n.ShouldTranslateContent(source) {
		return productcontract.TranslationCandidate{}, false
	}
	return productcontract.TranslationCandidate{
		EntityType: entityType, EntityID: id, EntityName: name,
		Field: field, FieldLabel: productcontract.FieldLabel(entityType, field),
		Context:    productcontract.FieldContext(entityType, field),
		SourceText: source, SourceHash: i18n.ContentHash(source),
	}, true
}
