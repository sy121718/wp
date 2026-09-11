package productservice

// entity_source.go — 商品域实体类型注册与构建期字段解析（issue #6；issue #12 扩到分类/品牌/标签/属性）。
//
// 本模块是自身实体类型字段白名单的唯一来源；装配期把五个实体类型注册进注册表，
// 内容模板与发布实例据此校验类型与字段绑定，构建期由本文件的解析器把
// 「实体的数据」静态填入组件（发布产物零查库，不变量 1）。
//
// 语言（多语言，issue #12）：解析器按构建语言（core.BuildLang）对「作者填写的文本」
// 取内容译文，语境固定 "{实体类型}.{字段名}"（docs/06-D §7.5）：
//   - product.name / product.subtitle / product.description / product.imageAlts
//     —— 商品自身文本；
//   - product_attribute.values —— 属性值展示文本（只翻展示文本；
//     值 key 与规格组合、筛选参数、URL 段原样保留，验收 5）；
//   - product_category.name / product_brand.name / product_tag.name 等
//     —— 商品引用的分类 / 品牌 / 标签文本（随商品一起进产物）。
//
// 未设置语言或未注入译文端口时一律回退原文，绝不报错（决策 F8）。

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"go_wp/internal/builder/core"
	productcontract "go_wp/internal/module/product/contract"
	productdto "go_wp/internal/module/product/dto"
	productenums "go_wp/internal/module/product/enums"
	productmodel "go_wp/internal/module/product/model"
	"go_wp/pkg/i18n"

	"gorm.io/gorm"
)

// entityFieldSource 商品域实体类型的字段来源适配器。
type entityFieldSource struct {
	svc        *Service
	entityType string
}

// EntityType 实现 core.EntityFieldSource。
func (e *entityFieldSource) EntityType() string { return e.entityType }

// FieldWhitelist 实现 core.EntityFieldSource（白名单仍取自本模块契约的唯一来源）。
func (e *entityFieldSource) FieldWhitelist() []string {
	return productcontract.FieldWhitelist(e.entityType)
}

// ResolverFor 实现 core.EntityFieldSource。
func (e *entityFieldSource) ResolverFor(ctx context.Context, entityID string) (core.ContentResolver, error) {
	return e.svc.ResolverFor(ctx, e.entityType, entityID)
}

// RegisterEntityTypes 把本模块的实体类型注册进注册表（装配期调用）。
//
// 注册表为 nil 视为装配缺陷（fail-closed）：静默跳过会让构建层到运行期才发现
// 「类型非法」，比装配期直接报错更难排查（与 content 模块同一口径）。
func (s *Service) RegisterEntityTypes(reg core.EntitySourceRegistry) error {
	if reg == nil {
		return errors.New("实体类型注册表为空")
	}
	for _, t := range productcontract.EntityTypes() {
		if err := reg.Register(&entityFieldSource{svc: s, entityType: t}); err != nil {
			return err
		}
	}
	return nil
}

// ResolverFor 返回绑定单个实体的字段解析器（构建期注入）。
//
// 一次性把白名单字段值算好（含价格区间等派生值、分类/品牌/标签/属性值的译文）
// 并挂上译文，构建期只读内存；不存在的实体返回 ErrNotFound，类型不符返回 ErrInvalidType。
func (s *Service) ResolverFor(ctx context.Context, entityType, entityID string) (r core.ContentResolver, err error) {
	if !productcontract.IsValidType(entityType) {
		return nil, errors.New(productenums.ErrInvalidType)
	}
	lang := core.BuildLang(ctx)
	switch entityType {
	case productcontract.EntityTypeCategory:
		var row *productmodel.ProductCategoryEntity
		if row, err = s.m.GetCategory(ctx, entityID); err != nil {
			return nil, mapNotFound(err)
		}
		return &entityResolver{entityType: entityType, values: s.categoryValues(ctx, lang, row)}, nil
	case productcontract.EntityTypeBrand:
		var row *productmodel.ProductBrandEntity
		if row, err = s.m.GetBrand(ctx, entityID); err != nil {
			return nil, mapNotFound(err)
		}
		return &entityResolver{entityType: entityType, values: s.brandValues(ctx, lang, row)}, nil
	case productcontract.EntityTypeTag:
		var row *productmodel.ProductTagEntity
		if row, err = s.m.GetTag(ctx, entityID); err != nil {
			return nil, mapNotFound(err)
		}
		return &entityResolver{entityType: entityType, values: s.tagValues(ctx, lang, row)}, nil
	case productcontract.EntityTypeAttribute:
		var row *productmodel.ProductAttributeEntity
		if row, err = s.m.GetAttribute(ctx, entityID); err != nil {
			return nil, mapNotFound(err)
		}
		return &entityResolver{entityType: entityType, values: s.attributeValues(ctx, lang, row)}, nil
	}

	e, gerr := s.m.Get(ctx, entityID)
	if gerr != nil {
		return nil, mapNotFound(gerr)
	}
	variants, verr := s.m.ListVariants(ctx, e.ID)
	if verr != nil {
		return nil, verr
	}
	attrs, aerr := s.m.ListAttributesByIDs(ctx, decodeStrings(e.AttributeIDs))
	if aerr != nil {
		return nil, aerr
	}
	loc, lerr := s.localizeRelated(ctx, lang, e)
	if lerr != nil {
		return nil, lerr
	}
	values := productFieldValues(e, variants, attrs, loc)
	return &entityResolver{entityType: entityType, values: s.translateFields(ctx, lang, productcontract.EntityTypeProduct, values)}, nil
}

// mapNotFound gorm 未命中 → 模块统一的「不存在」错误（其余原样上抛）。
func mapNotFound(err error) error {
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return errors.New(productenums.ErrNotFound)
	}
	return err
}

// —— 各实体类型的取值 ——

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
	if lang == "" || s.contentStore == nil || len(values) == 0 {
		return values
	}
	texts := map[string]string{}
	fields := map[string]string{}
	for _, f := range productcontract.TranslatableFields(entityType) {
		if f == "values" {
			continue
		}
		if v, ok := values[f]; ok {
			contextName := productcontract.FieldContext(entityType, f)
			texts[contextName] = v
			fields[contextName] = f
		}
	}
	if len(texts) == 0 {
		return values
	}
	i18n.TranslateValues(ctx, lang, s.contentStore, texts)
	for contextName, field := range fields {
		values[field] = texts[contextName]
	}
	return values
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
	tr := i18n.NewContentTranslatorWith(ctx, s.contentStore, lang, hashes)
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

// ResolveString 按字段白名单解析实体字段值。
//
// field 形如 "product.name"（entityType.field，与 heading 等组件的绑定格式一致）；
// 前缀类型不符或字段不在白名单内一律报错 —— 白名单是唯一来源，不做静默回退。
func (r *entityResolver) ResolveString(field string) (string, error) {
	entityType, name, ok := splitEntityField(field)
	if !ok {
		return "", fmt.Errorf("%s: %q（期望 entityType.field）", productenums.ErrInvalidField, field)
	}
	if entityType != r.entityType {
		return "", fmt.Errorf("绑定字段 %q 类型 %q 与当前实体 %q 不符", field, entityType, r.entityType)
	}
	if !productcontract.IsValidField(entityType, name) {
		return "", fmt.Errorf("%s: %q", productenums.ErrInvalidField, name)
	}
	return r.values[name], nil
}

// splitEntityField 拆 "entity.field"（两段，字段名不允许再带点）。
func splitEntityField(field string) (entityType, name string, ok bool) {
	parts := strings.SplitN(strings.TrimSpace(field), ".", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", false
	}
	if strings.Contains(parts[1], ".") {
		return "", "", false
	}
	return parts[0], parts[1], true
}

// —— 商品字段值 ——

// productFieldValues 计算商品白名单字段的展示值（纯函数，便于单测）。
//
// 价格全部落在变体上（商品主体不存价格，issue #5 已定语义），这里给出的是
// 由变体派生的只读值：price 取最低变体价，priceRange 在多价时输出 "最低 ~ 最高"。
//
// options / variants 同理是派生值（issue #8）：属性组 → 规格维度，变体行 → 规格组合；
// 维度显示名与值展示文本取 product_attribute 的译文（验收 5：只翻展示文本，key 原样保留）。
// 商品自身可翻译字段的取词由调用方（ResolverFor）用 translateFields 完成 ——
// 本函数保持无副作用的纯计算，便于单测与集合源复用。
func productFieldValues(p *productmodel.ProductEntity, variants []*productmodel.VariantEntity, attrs []*productmodel.ProductAttributeEntity, loc *relatedTexts) map[string]string {
	out := map[string]string{
		"name":         p.Name,
		"subtitle":     p.Subtitle,
		"description":  descriptionHTML(p.Description),
		"imageAlts":    imageAltsJSON(p, loc),
		"slug":         p.Slug,
		"unit":         p.Unit,
		"images":       imagesJSON(p),
		"defaultImage": p.DefaultImage,
		"options":      optionsJSON(p, attrs, loc),
		"variants":     variantsJSON(variants),
		"related":      relatedJSON(p, loc),
	}
	if len(variants) > 0 {
		out["sku"] = variants[0].SKUCode
		minPrice, maxPrice := variants[0].Price, variants[0].Price
		var compare float64
		hasCompare := false
		for _, v := range variants {
			if v.Price < minPrice {
				minPrice = v.Price
			}
			if v.Price > maxPrice {
				maxPrice = v.Price
			}
			if v.ComparePrice != nil && (!hasCompare || *v.ComparePrice > compare) {
				compare, hasCompare = *v.ComparePrice, true
			}
		}
		out["price"] = formatPrice(minPrice)
		out["minPrice"] = formatPrice(minPrice)
		out["maxPrice"] = formatPrice(maxPrice)
		if minPrice == maxPrice {
			out["priceRange"] = formatPrice(minPrice)
		} else {
			out["priceRange"] = formatPrice(minPrice) + " ~ " + formatPrice(maxPrice)
		}
		if hasCompare {
			out["comparePrice"] = formatPrice(compare)
		}
	}
	// imageAlt：图集首张的 alt 单值槽位（主图 alt 直接绑它；无 alt 时组件用商品名兜底）。
	if alts := decodeStrings(json.RawMessage(out["imageAlts"])); len(alts) > 0 {
		out["imageAlt"] = alts[0]
	}
	return out
}

// —— 关联实体（分类 / 品牌 / 标签 / 属性）的译文视图 ——

// relatedTexts 商品引用的分类 / 品牌 / 标签 / 属性组的译文视图。
//
// entities 存实体本身（slug / key 等稳定标识原样读取，永不进译文），
// fields 存译文（键 "实体类型" + NUL + id → 字段名 → 文本）。
type relatedTexts struct {
	categories map[string]*productmodel.ProductCategoryEntity
	brands     map[string]*productmodel.ProductBrandEntity
	tags       map[string]*productmodel.ProductTagEntity
	attributes map[string]*productmodel.ProductAttributeEntity
	fields     map[string]map[string]string
	// attrLabels 属性 id → 属性值 key → 展示文本译文。
	attrLabels map[string]map[string]string
	// imageAlts 商品图集 alt 的译文（原文 → 译文）。
	imageAlts map[string]string
}

// relatedFieldKey 译文映射键（实体类型 + NUL + id；NUL 不会出现在两者里，无歧义）。
func relatedFieldKey(entityType, id string) string {
	return entityType + "\x00" + id
}

// emptyRelated 空译文视图（无语言 / 无引用时使用，方法全部回退原文）。
func emptyRelated() *relatedTexts {
	return &relatedTexts{
		categories: map[string]*productmodel.ProductCategoryEntity{},
		brands:     map[string]*productmodel.ProductBrandEntity{},
		tags:       map[string]*productmodel.ProductTagEntity{},
		attributes: map[string]*productmodel.ProductAttributeEntity{},
		fields:     map[string]map[string]string{},
		attrLabels: map[string]map[string]string{},
		imageAlts:  map[string]string{},
	}
}

// name 取某实体某字段的译文（无译文 / 无该实体时回退 fallback）。
func (r *relatedTexts) name(entityType, id, field, fallback string) string {
	if r == nil {
		return fallback
	}
	if v, ok := r.fields[relatedFieldKey(entityType, id)][field]; ok && strings.TrimSpace(v) != "" {
		return v
	}
	return fallback
}

// label 取属性值展示文本的译文（无译文回退原文；key 永远不变）。
func (r *relatedTexts) label(attrID, valueKey, fallback string) string {
	if r == nil {
		return fallback
	}
	if v, ok := r.attrLabels[attrID][valueKey]; ok && strings.TrimSpace(v) != "" {
		return v
	}
	return fallback
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
		rows, cerr := s.m.ListCategoriesByIDs(ctx, ids)
		if cerr != nil {
			return nil, cerr
		}
		for _, row := range rows {
			loc.categories[row.ID] = row
		}
	}
	if e.BrandID != nil && strings.TrimSpace(*e.BrandID) != "" {
		if row, berr := s.m.GetBrand(ctx, *e.BrandID); berr == nil {
			loc.brands[row.ID] = row
		}
	}
	if ids := decodeStrings(e.TagIDs); len(ids) > 0 {
		rows, terr := s.m.ListTagsByIDs(ctx, ids)
		if terr != nil {
			return nil, terr
		}
		for _, row := range rows {
			loc.tags[row.ID] = row
		}
	}
	attrs, aerr := s.m.ListAttributesByIDs(ctx, decodeStrings(e.AttributeIDs))
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
type variantJSON struct {
	SKU          string            `json:"sku"`
	Price        string            `json:"price"`
	ComparePrice string            `json:"comparePrice,omitempty"`
	Image        string            `json:"image,omitempty"`
	Enabled      bool              `json:"enabled"`
	Options      map[string]string `json:"options"`
}

// relatedJSON 商品挂载关系的展示文本（分类 / 品牌 / 标签）。
//
// 形状：{"categories":[{"slug":…,"name":…}],"brand":{…},"tags":[…]}
// slug 原样保留（它进 URL 与筛选参数），只有展示名取译文；空引用输出空数组。
func relatedJSON(p *productmodel.ProductEntity, loc *relatedTexts) string {
	type named struct {
		Slug string `json:"slug"`
		Name string `json:"name"`
	}
	out := struct {
		Categories []named `json:"categories"`
		Brand      *named  `json:"brand,omitempty"`
		Tags       []named `json:"tags"`
	}{Categories: []named{}, Tags: []named{}}
	if p == nil {
		return "{}"
	}
	for _, id := range decodeStrings(p.CategoryIDs) {
		row, ok := loc.categories[id]
		if !ok {
			continue
		}
		out.Categories = append(out.Categories, named{
			Slug: row.Slug,
			Name: loc.name(productcontract.EntityTypeCategory, id, "name", row.Name),
		})
	}
	if p.BrandID != nil {
		if row, ok := loc.brands[*p.BrandID]; ok {
			out.Brand = &named{Slug: row.Slug, Name: loc.name(productcontract.EntityTypeBrand, row.ID, "name", row.Name)}
		}
	}
	for _, id := range decodeStrings(p.TagIDs) {
		row, ok := loc.tags[id]
		if !ok {
			continue
		}
		out.Tags = append(out.Tags, named{
			Slug: row.Slug,
			Name: loc.name(productcontract.EntityTypeTag, id, "name", row.Name),
		})
	}
	b, err := json.Marshal(out)
	if err != nil {
		return "{}"
	}
	return string(b)
}

// imageAltsJSON 图集 alt 文本数组（与 images 逐位对应，元素可为空串）。
//
// 原文来自 products.images_alt（issue #12，迁移 094）；命中译文用译文，
// 未命中逐字节回退原文（不改写入库）。
func imageAltsJSON(p *productmodel.ProductEntity, loc *relatedTexts) string {
	alts := decodeStrings(p.ImageAlts)
	out := make([]string, len(alts))
	for i, alt := range alts {
		out[i] = alt
		if loc == nil {
			continue
		}
		if target, ok := loc.imageAlts[alt]; ok && strings.TrimSpace(target) != "" {
			out[i] = target
		}
	}
	b, err := json.Marshal(out)
	if err != nil {
		return "[]"
	}
	return string(b)
}

// optionsJSON 规格维度：商品引用且「参与变体」的属性组 × 其启用值。
//
// 顺序 = 商品 attribute_ids 的引用顺序（作者在后台勾选的顺序），保证同一份数据
// 每次构建输出同样的字节（不变量 5）。非参与变体 / 无启用值的组不进规格维度。
// 维度名取 product_attribute.name 的译文，值展示文本取 product_attribute.values
// 的译文；值 key 原样保留（验收 5：筛选参数与 URL 段保持不变）。
func optionsJSON(p *productmodel.ProductEntity, attrs []*productmodel.ProductAttributeEntity, loc *relatedTexts) string {
	byID := make(map[string]*productmodel.ProductAttributeEntity, len(attrs))
	for _, a := range attrs {
		byID[a.ID] = a
	}
	groups := []optionGroupJSON{}
	for _, id := range decodeStrings(p.AttributeIDs) {
		a, ok := byID[id]
		if !ok || !a.IsVariation {
			continue
		}
		values := []optionValueJSON{}
		for _, v := range normalizeValuesFromRaw(a.Values) {
			if !v.Enabled {
				continue
			}
			values = append(values, optionValueJSON{Key: v.Key, Label: loc.label(id, v.Key, v.Label)})
		}
		if len(values) == 0 {
			continue
		}
		groups = append(groups, optionGroupJSON{
			Key:    a.Key,
			Name:   loc.name(productcontract.EntityTypeAttribute, id, "name", a.Name),
			Values: values,
		})
	}
	b, err := json.Marshal(groups)
	if err != nil {
		return "[]"
	}
	return string(b)
}

// variantsJSON 规格组合行：每个变体的编码 / 价格 / 组合（属性组 key → 属性值 key）。
//
// 价格在这里就带上货币无关的纯数字（货币符号由组件按 Props 前缀），
// 与 price / priceRange 的取值口径一致。组合里的 key 是稳定标识，永不进译文。
func variantsJSON(variants []*productmodel.VariantEntity) string {
	rows := []variantJSON{}
	for _, v := range variants {
		row := variantJSON{
			SKU: v.SKUCode, Price: formatPrice(v.Price), Image: v.Image,
			Enabled: v.Enabled, Options: map[string]string{},
		}
		if v.ComparePrice != nil {
			row.ComparePrice = formatPrice(*v.ComparePrice)
		}
		if len(v.OptionValues) > 0 {
			_ = json.Unmarshal(v.OptionValues, &row.Options)
		}
		if row.Options == nil {
			row.Options = map[string]string{}
		}
		rows = append(rows, row)
	}
	b, err := json.Marshal(rows)
	if err != nil {
		return "[]"
	}
	return string(b)
}

// valueLabelsJSON 属性值展示文本数组（按组内定义顺序，含未启用值）。
func valueLabelsJSON(values []productdto.AttributeValueResp) string {
	labels := make([]string, 0, len(values))
	for _, v := range values {
		labels = append(labels, v.Label)
	}
	return stringListJSON(labels)
}

// stringListJSON 字符串数组 → JSON（nil 当空数组；编码失败退化为空数组，不让构建失败）。
func stringListJSON(items []string) string {
	if items == nil {
		items = []string{}
	}
	b, err := json.Marshal(items)
	if err != nil {
		return "[]"
	}
	return string(b)
}

// formatPrice 数值 → 展示字符串：整数不带小数尾巴，其余按最短表示。
func formatPrice(v float64) string {
	return strconv.FormatFloat(v, 'f', -1, 64)
}

// imagesJSON 图集 JSON 数组字符串（core.gallery 等组件按 JSON 数组解析绑定值）；
// 商品未配图集但有主图时退化为单元素数组，避免详情页出现空图区。
func imagesJSON(p *productmodel.ProductEntity) string {
	urls := imageURLs(p)
	b, err := json.Marshal(urls)
	if err != nil {
		return "[]"
	}
	return string(b)
}

// imageURLs 图集 URL：未配图集但有主图时退化为单元素数组（与 core.product 同口径）。
func imageURLs(p *productmodel.ProductEntity) []string {
	urls := []string{}
	if len(p.Images) > 0 {
		_ = json.Unmarshal(p.Images, &urls)
	}
	if len(urls) == 0 && p.DefaultImage != "" {
		urls = []string{p.DefaultImage}
	}
	return urls
}

// descriptionHTML 商品描述（jsonb）→ HTML 片段。
//
// 约定两种形态：{"html": "..."}（富文本）与 JSON 字符串（纯文本）；
// 其余（{} 或未知结构）返回空串，由组件侧决定是否输出占位。
func descriptionHTML(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var obj struct {
		HTML string `json:"html"`
	}
	if err := json.Unmarshal(raw, &obj); err == nil && obj.HTML != "" {
		return obj.HTML
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return s
	}
	return ""
}

// 编译期断言：适配器实现 core.EntityFieldSource，解析器实现 core.ContentResolver。
var (
	_ core.EntityFieldSource = (*entityFieldSource)(nil)
	_ core.ContentResolver   = (*entityResolver)(nil)
)
