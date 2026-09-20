package productservice

// entity_source_translate.go — 多语言翻译与关联实体本地化（字段级批量取词、关联名称与属性值译文）。

import (
	"context"
	"strings"

	"go_wp/internal/builder/core"
	productcontract "go_wp/internal/module/product/contract"
	productmodel "go_wp/internal/module/product/model"
	"go_wp/pkg/i18n"
)

// categoryValues 分类的可绑定字段值（作者文本按 lang 取译文）。
func (s *Service) categoryValues(ctx context.Context, lang string, e *productmodel.ProductCategoryEntity) map[string]string {
	values := map[string]string{
		"name":           e.Name,
		"slug":           e.Slug,
		"description":    e.Description,
		"image":          e.Image,
		"seoTitle":       e.SEOTitle,
		"seoDescription": e.SEODescription,
	}
	return s.translateFields(ctx, lang, productcontract.EntityTypeCategory, values)
}

// brandValues 品牌的可绑定字段值。
func (s *Service) brandValues(ctx context.Context, lang string, e *productmodel.ProductBrandEntity) map[string]string {
	values := map[string]string{
		"name":           e.Name,
		"slug":           e.Slug,
		"logo":           e.Logo,
		"description":    e.Description,
		"seoTitle":       e.SEOTitle,
		"seoDescription": e.SEODescription,
	}
	return s.translateFields(ctx, lang, productcontract.EntityTypeBrand, values)
}

// tagValues 标签的可绑定字段值。
func (s *Service) tagValues(ctx context.Context, lang string, e *productmodel.ProductTagEntity) map[string]string {
	values := map[string]string{"name": e.Name, "slug": e.Slug}
	return s.translateFields(ctx, lang, productcontract.EntityTypeTag, values)
}

// attributeValues 属性组的可绑定字段值。
//
// values 字段是属性值展示文本的 JSON 数组（按组内定义顺序，含未启用值）；
// 逐元素取 product_attribute.values 的译文（与商品规格选择器同源），
// 值 key 不进数组 —— 它进筛选参数与规格组合，永不翻译（验收 5）。
func (s *Service) attributeValues(ctx context.Context, lang string, e *productmodel.ProductAttributeEntity) map[string]string {
	values := map[string]string{
		"name":   e.Name,
		"key":    e.Key,
		"values": valueLabelsJSON(normalizeValuesFromRaw(e.Values)),
	}
	values = s.translateFields(ctx, lang, productcontract.EntityTypeAttribute, values)
	values["values"] = s.translatedValueLabelsJSON(ctx, lang, e)
	return values
}

// translatedValueLabelsJSON 属性值展示文本数组（逐元素取译文，无译文回退原文）。
func (s *Service) translatedValueLabelsJSON(ctx context.Context, lang string, e *productmodel.ProductAttributeEntity) string {
	if e == nil {
		return "[]"
	}
	items := normalizeValuesFromRaw(e.Values)
	translated := s.attributeValueTranslations(ctx, lang, e)
	labels := make([]string, 0, len(items))
	for _, v := range items {
		if target, ok := translated[v.Key]; ok {
			labels = append(labels, target)
			continue
		}
		labels = append(labels, v.Label)
	}
	return stringListJSON(labels)
}

// translateFields 把某实体类型的标量可翻译字段整体替换为译文（就地改写 values）。
//
// 语境按 productcontract.FieldContext 一处拼装；跳过规则（纯数字/纯符号/空白）
// 与「无译文回退原文」由 pkg/i18n 负责，这里不做第二套判断。
// 数组字段（product_attribute.values）不在此处翻 —— 它逐元素取词，见
// attributeValueTranslations。
func (s *Service) translateFields(ctx context.Context, lang string, entityType string, values map[string]string) map[string]string {
	if len(values) == 0 {
		return values
	}
	s.translateFieldsBatch(ctx, lang, entityType, []map[string]string{values})
	return values
}

// translateFieldsBatch 对多组字段值一次性批量取词（PERF-009：集合解析不再逐商品查库）。
func (s *Service) translateFieldsBatch(ctx context.Context, lang, entityType string, batches []map[string]string) {
	if lang == "" || s.contentStore == nil || len(batches) == 0 {
		return
	}
	type workItem struct {
		batchIdx int
		field    string
		context  string
		source   string
	}
	work := make([]workItem, 0, len(batches)*4)
	hashes := make([]string, 0, len(batches)*4)
	seenHash := map[string]bool{}
	for i, values := range batches {
		if len(values) == 0 {
			continue
		}
		for _, f := range productcontract.TranslatableFields(entityType) {
			if f == "values" {
				continue
			}
			v, ok := values[f]
			if !ok || !i18n.ShouldTranslateContent(v) {
				continue
			}
			h := i18n.ContentHash(v)
			if !seenHash[h] {
				seenHash[h] = true
				hashes = append(hashes, h)
			}
			work = append(work, workItem{
				batchIdx: i,
				field:    f,
				context:  productcontract.FieldContext(entityType, f),
				source:   v,
			})
		}
	}
	if len(hashes) == 0 {
		return
	}
	// 工程作用域（审计 I18N-009）：工程 id 取自构建上下文（core.WithBuildProjectID
	// 由 builder.Compile 注入，与 BuildLang 同一约定；非构建调用退化为全局视图）。
	tr := i18n.NewContentTranslatorScoped(ctx, core.BuildProjectID(ctx), s.contentStore, lang, hashes)
	for _, item := range work {
		batches[item.batchIdx][item.field] = tr.TranslateContent(item.source, item.context)
	}
}

// translateTexts 批量取一组文本在指定语境下的译文（同一语境，逐元素）。
//
// 返回 map[原文]译文；无译文 / 跳过规则命中的原文不在返回值里（调用方回退原文）。
// 一次批量 SQL（取词器的唯一查询形态），不逐条查库。
func (s *Service) translateTexts(ctx context.Context, lang, contextName string, sources []string) map[string]string {
	out := map[string]string{}
	if lang == "" || s.contentStore == nil || contextName == "" || len(sources) == 0 {
		return out
	}
	hashes := make([]string, 0, len(sources))
	uniq := make([]string, 0, len(sources))
	seen := map[string]bool{}
	for _, src := range sources {
		if !i18n.ShouldTranslateContent(src) || seen[src] {
			continue
		}
		seen[src] = true
		uniq = append(uniq, src)
		hashes = append(hashes, i18n.ContentHash(src))
	}
	if len(hashes) == 0 {
		return out
	}
	// 工程作用域（审计 I18N-009）：工程 id 取自构建上下文（core.WithBuildProjectID
	// 由 builder.Compile 注入，与 BuildLang 同一约定；非构建调用退化为全局视图）。
	tr := i18n.NewContentTranslatorScoped(ctx, core.BuildProjectID(ctx), s.contentStore, lang, hashes)
	for _, src := range uniq {
		if target := tr.TranslateContent(src, contextName); target != src {
			out[src] = target
		}
	}
	return out
}

// attributeValueTranslations 属性组内每个属性值展示文本的译文（值 key → 译文）。
//
// 语境固定 product_attribute.values：同一属性组内「同一段文本 → 同一行译文」，
// 值 key 永不进译文（验收 5：筛选参数与 URL 段保持不变）。
func (s *Service) attributeValueTranslations(ctx context.Context, lang string, e *productmodel.ProductAttributeEntity) map[string]string {
	out := map[string]string{}
	if e == nil {
		return out
	}
	values := normalizeValuesFromRaw(e.Values)
	sources := make([]string, 0, len(values))
	for _, v := range values {
		sources = append(sources, v.Label)
	}
	translated := s.translateTexts(ctx, lang, productcontract.FieldContext(productcontract.EntityTypeAttribute, "values"), sources)
	if len(translated) == 0 {
		return out
	}
	for _, v := range values {
		if target, ok := translated[v.Label]; ok {
			out[v.Key] = target
		}
	}
	return out
}

// imageAltTranslations 商品图集 alt 文本的译文（原文 → 译文）。
//
// 语境固定 product.imageAlts（与工作台写入一致），逐元素取词；URL 永不翻译。
func (s *Service) imageAltTranslations(ctx context.Context, lang string, p *productmodel.ProductEntity) map[string]string {
	if p == nil {
		return map[string]string{}
	}
	alts := decodeStrings(p.ImageAlts)
	if len(alts) == 0 {
		return map[string]string{}
	}
	return s.translateTexts(ctx, lang, productcontract.FieldContext(productcontract.EntityTypeProduct, "imageAlts"), alts)
}

// entityResolver 绑定单个实体的字段解析器（值已预计算并翻译）。
type entityResolver struct {
	entityType string
	values     map[string]string
}

// localizeRelated 加载商品引用的分类 / 品牌 / 标签 / 属性组并按构建语言取译文。
//
// 每个实体类型一次批量取词（不逐条查库）；语言为空时直接返回空视图（零查库）。
func (s *Service) localizeRelated(ctx context.Context, lang string, e *productmodel.ProductEntity) (loc *relatedTexts, err error) {
	loc = emptyRelated()
	if e == nil {
		return loc, nil
	}
	if lang == "" || s.contentStore == nil {
		return loc, nil
	}

	if ids := decodeStrings(e.CategoryIDs); len(ids) > 0 {
		rows, cerr := s.m.ListCategoriesByIDs(ctx, ids, e.ProjectID)
		if cerr != nil {
			return nil, cerr
		}
		for _, row := range rows {
			loc.categories[row.ID] = row
		}
	}
	if e.BrandID != nil && strings.TrimSpace(*e.BrandID) != "" {
		if row, berr := s.m.GetBrand(ctx, *e.BrandID, e.ProjectID); berr == nil {
			loc.brands[row.ID] = row
		}
	}
	if ids := decodeStrings(e.TagIDs); len(ids) > 0 {
		rows, terr := s.m.ListTagsByIDs(ctx, ids, e.ProjectID)
		if terr != nil {
			return nil, terr
		}
		for _, row := range rows {
			loc.tags[row.ID] = row
		}
	}
	attrs, aerr := s.m.ListAttributesByIDs(ctx, decodeStrings(e.AttributeIDs), e.ProjectID)
	if aerr != nil {
		return nil, aerr
	}
	return s.fillRelatedTexts(ctx, lang, e, loc, attrs), nil
}

// localizeRelatedFrom 用已预载的分类 / 品牌 / 标签索引取译文（集合源列表用，零 N+1）。
func (s *Service) localizeRelatedFrom(ctx context.Context, lang string, e *productmodel.ProductEntity,
	categories map[string]*productmodel.ProductCategoryEntity,
	brands map[string]*productmodel.ProductBrandEntity,
	tags map[string]*productmodel.ProductTagEntity,
	attrs []*productmodel.ProductAttributeEntity) (loc *relatedTexts, err error) {
	loc = emptyRelated()
	if e == nil {
		return loc, nil
	}
	for id, row := range categories {
		loc.categories[id] = row
	}
	for id, row := range brands {
		loc.brands[id] = row
	}
	for id, row := range tags {
		loc.tags[id] = row
	}
	return s.fillRelatedTexts(ctx, lang, e, loc, attrs), nil
}

// fillRelatedTexts 按 lang 为已加载的关联实体取译文（每个实体类型一次批量取词）。
func (s *Service) fillRelatedTexts(ctx context.Context, lang string, e *productmodel.ProductEntity, loc *relatedTexts, attrs []*productmodel.ProductAttributeEntity) *relatedTexts {
	if lang == "" || s.contentStore == nil {
		return loc
	}
	for id, row := range loc.categories {
		loc.fields[relatedFieldKey(productcontract.EntityTypeCategory, id)] = s.categoryValues(ctx, lang, row)
	}
	for id, row := range loc.brands {
		loc.fields[relatedFieldKey(productcontract.EntityTypeBrand, id)] = s.brandValues(ctx, lang, row)
	}
	for id, row := range loc.tags {
		loc.fields[relatedFieldKey(productcontract.EntityTypeTag, id)] = s.tagValues(ctx, lang, row)
	}
	for _, row := range attrs {
		loc.attributes[row.ID] = row
		loc.fields[relatedFieldKey(productcontract.EntityTypeAttribute, row.ID)] = s.attributeValues(ctx, lang, row)
		loc.attrLabels[row.ID] = s.attributeValueTranslations(ctx, lang, row)
	}
	if e != nil {
		loc.imageAlts = s.imageAltTranslations(ctx, lang, e)
	}
	return loc
}

// —— JSON 派生值 ——

// optionGroupJSON 规格维度（构建期输出的 JSON 结构，与 core.product 的解析约定一致）。
type optionGroupJSON struct {
	Key    string            `json:"key"`
	Name   string            `json:"name"`
	Values []optionValueJSON `json:"values"`
}

// optionValueJSON 规格维度下的一个可选值（key 是稳定标识，不进译文）。
type optionValueJSON struct {
	Key   string `json:"key"`
	Label string `json:"label"`
}

// variantJSON 规格组合行（构建期输出的 JSON 结构）。
//
// ID 是变体 id（issue #24）：产物里要用它构造「实时可用量片段」的请求参数。
type variantJSON struct {
	ID           string            `json:"id"`
	SKU          string            `json:"sku"`
	Price        string            `json:"price"`
	ComparePrice string            `json:"comparePrice,omitempty"`
	Image        string            `json:"image,omitempty"`
	Enabled      bool              `json:"enabled"`
	Options      map[string]string `json:"options"`
}
