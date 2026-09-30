package productservice

// entity_source_fields.go — 实体字段取值与 JSON 投影（商品字段、变体、选项、图片、相关实体）。

import (
	"encoding/json"
	"strconv"
	"strings"

	"go_wp/internal/builder/core"
	productcontract "go_wp/internal/module/product/contract"
	productdto "go_wp/internal/module/product/dto"
	productmodel "go_wp/internal/module/product/model"
	"go_wp/pkg/money"
)

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
		"name":        p.Name,
		"subtitle":    p.Subtitle,
		"description": descriptionHTML(p.Description),
		// SEO 字段原样透出（不套 descriptionHTML）：它们是给 <title> 与 meta description
		// 用的纯文本，套上富文本包装反而要在消费侧再去标签
		//（presentation 侧仍会做一次归一，兜住历史数据里混进的标记）。
		// SEO 标题与商品名**就是同一个东西**（2026-09-30 合并）：商品名即网页标题。
		// 原来分开两个字段，是从 WordPress 那套抄来的形状 —— 它分开是因为原生没有独立标题概念，
		// 而这里「商品叫什么」本来就是我们要的 <title>，多一个框只会逼编辑者抄一遍。
		// seoTitle 这个**键保留**：模板里绑 {{product.seoTitle}} 的地方不用改，值就是商品名。
		// 副标题同理充当 meta description（按他说的「网页悬浮显示的那行」）。
		//
		// products.seo_title / seo_description 两列**保留但不再暴露**：里面可能有编辑者
		// 认真写过的历史值，删列会丢数据。它们不再是任何 UI 的来源。
		"seoTitle":       p.Name,
		"seoDescription": p.Subtitle,
		"imageAlts":      imageAltsJSON(p, loc),
		"slug":           p.Slug,
		"unit":           p.Unit,
		"images":         imagesJSON(p),
		"defaultImage":   mediaURL(p.DefaultImage),
		"options":        optionsJSON(p, variants, attrs, loc),
		"variants":       variantsJSON(variants),
		"related":        relatedJSON(p, loc),
		"tags":           tagsJSON(p, loc),
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
	// 评分（issue #30）：由评分明细投影算出（明细已 Preload），商品表上没有评分列。
	//
	// **一条评分都没有时不写这两个键**：与「评分 0」严格区分 —— 集合组件据此把它排到最后，
	// 而不是当成 0 分。rating 给两位小数的字符串（展示用），数值另见集合项的 ratingValue。
	if avg, count, ok := p.RatingSummaryOf(); ok {
		out["rating"] = strconv.FormatFloat(avg, 'f', 2, 64)
		out["ratingCount"] = strconv.Itoa(count)
	}
	// imageAlt：图集首张的 alt 单值槽位（主图 alt 直接绑它；无 alt 时组件用商品名兜底）。
	if alts := decodeStrings(json.RawMessage(out["imageAlts"])); len(alts) > 0 {
		out["imageAlt"] = alts[0]
	}
	return out
}

// tagsJSON 商品标签的展示名数组（issue #22：商品卡要显示标签）。
//
// 形状是**字符串数组**而不是对象数组：卡片只需要展示名（slug 进筛选参数，
// 不进卡片；要链到标签页的完整结构走 product.related）。展示名取译文（标签名
// 可翻译，issue #12），无引用输出空数组 —— 组件据此不输出空壳节点。
func tagsJSON(p *productmodel.ProductEntity, loc *relatedTexts) string {
	names := make([]string, 0, 4)
	if p == nil {
		return "[]"
	}
	for _, id := range decodeStrings(p.TagIDs) {
		row, ok := loc.tags[id]
		if !ok {
			continue
		}
		name := loc.name(productcontract.EntityTypeTag, id, "name", row.Name)
		if strings.TrimSpace(name) == "" {
			continue
		}
		names = append(names, name)
	}
	b, err := json.Marshal(names)
	if err != nil {
		return "[]"
	}
	return string(b)
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
// variantOptionKeys 变体实际用到的规格维度 key 集合。
//
// 变体的 option_values 是 {"<属性 key>": "<值 key>"} 形状（空对象表示无规格）。
// 解析失败按「没用到」处理：宁可少出一个选择器，也不要因为一条脏数据让整页报错。
func variantOptionKeys(variants []*productmodel.VariantEntity) map[string]bool {
	out := map[string]bool{}
	for _, v := range variants {
		if v == nil || len(v.OptionValues) == 0 {
			continue
		}
		var values map[string]string
		if err := json.Unmarshal(v.OptionValues, &values); err != nil {
			continue
		}
		for key := range values {
			if key != "" {
				out[key] = true
			}
		}
	}
	return out
}

// optionsJSON 商品规格维度（选择器用）。
//
// 一个属性要成为**规格维度**，必须同时满足两条：
//  1. 属性自身标记为变化属性（is_variation）；
//  2. **至少一个变体真的用了它**（option_values 里有这个 key）。
//
// 第 2 条是后补的，它挡住的是一类真实故障：单 SKU 商品也带属性引用
// （那些属性是拿来做**筛选**的，不是拿来选规格的），只看 is_variation 会把它们
// 全当成规格维度 → 组件认为「这件商品有规格，但没有任何可买的组合」→
// 加购按钮渲染成「暂无可购买的规格」。而本系统没有「简单商品」这个概念，
// 单 SKU 就是只有一个变体、没有规格维度的可变商品，它的加购必须照常可用。
func optionsJSON(p *productmodel.ProductEntity, variants []*productmodel.VariantEntity, attrs []*productmodel.ProductAttributeEntity, loc *relatedTexts) string {
	byID := make(map[string]*productmodel.ProductAttributeEntity, len(attrs))
	for _, a := range attrs {
		byID[a.ID] = a
	}
	used := variantOptionKeys(variants)
	groups := []optionGroupJSON{}
	for _, id := range decodeStrings(p.AttributeIDs) {
		a, ok := byID[id]
		if !ok || !a.IsVariation {
			continue
		}
		// 没有任何变体用到 → 它不是这个商品的规格维度（见函数注释第 2 条）。
		if !used[a.Key] {
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
			ID:  v.ID,
			SKU: v.SKUCode, Price: formatPrice(v.Price), Image: mediaURL(v.Image),
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
//
// 唯一实现在 pkg/money.FormatYuan（审计 CQ-013：此前与 dashboard 的 formatAmount
// 逐字节重复 —— 两处都是展示口径）。保留本名字是因为在构建期字段投影里「价格」
// 比「金额格式化」更贴调用点语义，函数体只是转发。
func formatPrice(v float64) string {
	return money.FormatYuan(v)
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
//
// 出口归一成完整链接：产物里的图片地址直接给访客用，相对路径在「站点与 CMS 不同域」
// 的部署下会指回 CMS 自己（cdn / 独立域名场景）。存量行入库时是相对路径，所以这一层
// 必须归一，不能只靠写入口。
func imageURLs(p *productmodel.ProductEntity) []string {
	urls := []string{}
	if len(p.Images) > 0 {
		_ = json.Unmarshal(p.Images, &urls)
	}
	if len(urls) == 0 && p.DefaultImage != "" {
		urls = []string{p.DefaultImage}
	}
	return mediaURLs(urls)
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
