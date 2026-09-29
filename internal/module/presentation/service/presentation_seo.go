// presentation_seo.go — 自动发布实例的 SEO 头注入（构建期，实体字段驱动）。
//
// 缺口：手工 Page 的构建在 builder.Compile 内注入 canonical / OG / Twitter / JSON-LD，
// 而自动发布实例此前只把模板 AST 编译成字节 —— 实体上的 seoTitle / seoDescription
// 没有任何构建期消费者，商品 / 文章详情页产物里既没有 canonical 也没有结构化数据，
// og:type 也永远只能是默认的 website。
//
// 这里在**唯一注入点**（renderHTML，发布与预览共用同一份渲染）把实体字段与实例线上
// 路径喂进 page.Settings.SEO，随后由 builder 既有的 BuildSEOHead 统一产出（不自己拼
// meta：canonical / OG / Twitter / JSON-LD 的转义、`og:type` 跟随 schemaType、面包屑
// 等规则只有一份）。
//
// 三条取舍：
//
//  1. **优先级：实体字段（非空）> 模板 settings.seo 已有值**。实体字段是「这一篇 /
//     这一个商品」的 SEO 事实，模板是这一**类**页面的默认值 —— 前者更具体。
//     但实体字段为空时**一律不写**：模板里人工填好的标题 / 描述不会因为实体没填
//     而被清空（与 internal/seo 的 ScoreArticle 取法同向：seoTitle 缺了才回落 title）。
//
//  2. **canonical 是唯一的例外：实例的线上路径（urlPath）覆盖模板值**。同一套模板
//     被多个实体复用，模板里手填的 canonical 必然只对其中一个正确；而实例绑定的
//     URL 是系统权威事实（与产物 Manifest.CanonicalPath 同源）。预览不激活 URL
//     （urlPath 传空）→ 保持模板原值不动，通常就是没有 canonical。
//
//  3. **确定性**：只取实体字段与实例路径，不引入时间戳等非确定值 ——
//     同一输入（模板 + 实体 + URL）产生相同字节（项目不变量）。
//
// 注意（产物字节变化）：这让**所有重新构建**的详情页产物多出 canonical / JSON-LD，
// 已发布但内容未变的实例不会自动重建 —— 要重新发布（或经依赖失效触发重建）
// 才会带上 SEO 头。
package presentationservice

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"

	"go_wp/internal/builder"
	"go_wp/internal/builder/core"
	productcontract "go_wp/internal/module/product/contract"
	seoutil "go_wp/internal/seo"
	"go_wp/pkg/logger"
	"go_wp/pkg/upload"
)

func seoCanonicalPath(path string) string {
	return seoutil.CanonicalPublicPath(path)
}

// builder 页面设置校验的字段上限（internal/builder/settings.go 的 validateSettings）：
// title ≤ 200 字节、description ≤ 500 字节，超限直接让 Compile 失败。
//
// 失败的代价很大而收益为零：上层只拿到 ErrBuildFailed，要知道"是 SEO 标题太长"
// 得翻到设置校验那一层；而当事人（填标题的运营）看到的是"发布失败"。
// 所以这里按上限**截断**：SEO 标题与描述本来就是会被搜索结果截断的东西
// （Google 大约 60 字符就开始视觉截断），少几个字可以接受，整页发不出去不可以接受。
// 截断不是静默的 —— 每次都会记一条 warn（带实体类型、字段、上限与实际字节数）。
const (
	seoTitleLimitBytes       = 200
	seoDescriptionLimitBytes = 500
)

// 自动发布实例的两种实体类型标识。
//
// 商品取商品模块契约的常量（类型标识的唯一来源）；文章没有导出常量（内容模块的
// 白名单以字面量 article 为键），这里按该键逐字取值。
const (
	entityTypeArticle = "article"
	entityTypeProduct = productcontract.EntityTypeProduct
)

// seoTitleCandidates / seoDescriptionCandidates 实体字段的候选链（顺序即回落顺序）。
//
// 覆盖两类详情页：
//   - 文章：seoTitle → title；seoDescription → excerpt（字段白名单见 content 模块契约）；
//   - 商品：白名单里没有 seoTitle / seoDescription（只有分类与品牌有），
//     标题回落 name、描述回落 description。
//
// 落在白名单外的候选会被直接跳过（见 pickSEOField）：解析器对白名单外的字段
// 是**报错**而不是返回空串，「文章没有 name 字段」不是错误，只是这个候选不适用。
var (
	seoTitleCandidates       = []string{"seoTitle", "title", "name"}
	seoDescriptionCandidates = []string{"seoDescription", "excerpt", "description"}
)

// schemaTypeOf 实体类型 → 结构化数据类型（builder schemaTypeMap 的取值域）。
//
// 只映射两类详情页；未知类型返回空串 = 不写这个键，模板已填的 schemaType 原样保留
// （builder 对空值回落 WebPage，与「没有这个功能」时的产物一致）。
func schemaTypeOf(entityType string) string {
	switch entityType {
	case entityTypeArticle:
		return entityTypeArticle
	case entityTypeProduct:
		return entityTypeProduct
	}
	return ""
}

// entityImageCandidates 各实体类型的头图字段候选（顺序即回落顺序）。
//
// 只列**确实存在且语义就是头图**的字段：
//   - 文章：featuredImage（内容字段白名单里的封面）；
//   - 商品：defaultImage 是主图，images 是图集（取首张）。
//
// 候选外的字段一律不看 —— 猜一个字段名只会得到一张错的分享图。
var entityImageCandidates = map[string][]string{
	entityTypeArticle: {"featuredImage"},
	entityTypeProduct: {"defaultImage", "images"},
}

// pickEntityImage 取实体头图（取不到返回空串，不报错）。
//
// 图集字段（images）在实体里是 JSON 数组字符串，这里取首张 —— 与前台商品卡同口径
// （都取图集第一张当主图），避免「卡片与分享图不是同一张」。
func pickEntityImage(entityType string, writable []string, resolver core.ContentResolver) string {
	if resolver == nil {
		return ""
	}
	for _, field := range entityImageCandidates[entityType] {
		if !fieldWritable(writable, field) {
			continue
		}
		v, err := resolver.ResolveString(entityType + "." + field)
		if err != nil {
			continue
		}
		v = strings.TrimSpace(v)
		if v == "" {
			continue
		}
		if strings.HasPrefix(v, "[") {
			var list []string
			if jerr := json.Unmarshal([]byte(v), &list); jerr == nil && len(list) > 0 {
				v = strings.TrimSpace(list[0])
			}
		}
		if v != "" {
			return v
		}
	}
	return ""
}

// applyEntitySEO 把实体字段与实例线上路径写进页面 SEO 设置（Compile 之前调用）。
//
// writable 是该实体类型的字段白名单（取自实体类型注册表 —— 白名单的唯一来源，
// 这里不另维护一份）；resolver 是绑定该实体的构建期解析器，读到的值已按构建语言
// 取过译文（语境 实体类型.字段名），因此 SEO 头与页面正文的语言一致。
func applyEntitySEO(page *builder.Page, entityType, urlPath string,
	writable []string, resolver core.ContentResolver) error {
	title, err := pickSEOField(entityType, writable, resolver, seoTitleCandidates, seoPlainText)
	if err != nil {
		return err
	}
	description, err := pickSEOField(entityType, writable, resolver, seoDescriptionCandidates, seoDescriptionText)
	if err != nil {
		return err
	}
	// 空值不覆盖：只写取到的实体字段（取舍 1）。超限按上限截断（见 clampSEOField）。
	if title != "" {
		page.Settings.SEO.Title = clampSEOField(entityType, "seoTitle", title, seoTitleLimitBytes)
	}
	if description != "" {
		page.Settings.SEO.Description = clampSEOField(entityType, "seoDescription", description, seoDescriptionLimitBytes)
	}
	if st := schemaTypeOf(entityType); st != "" {
		page.Settings.SEO.SchemaType = st
	}
	if entityType == entityTypeProduct {
		if offer := productOfferLD(entityType, writable, resolver); offer != nil {
			page.Settings.SEO.ProductOffer = offer
		}
	}
	// 头图 → og:image（社交分享卡片与 JSON-LD 的 image）。
	//
	// 此前只填了 title / description / schemaType，**没填图** —— 详情页的产物里
	// 一条 og:image 都没有，分享到任何平台都是无图卡片（twitter:card 也退回 summary）。
	// 取不到就不写：宁可没有图，也不要一张猜出来的图。
	if img := pickEntityImage(entityType, writable, resolver); img != "" {
		// 先归一到媒体自己的对外地址（upload.StorageURL），再交给 BuildSEOHead。
		// 顺序要紧：实体里存的可能是相对路径 /storage/x.jpg，直接交给 BuildSEOHead 的
		// absoluteURL 会**套上站点基址的前缀** —— 基址是 /site 时拼成
		// /site/storage/x.jpg，而媒体其实在站点根的 /storage 下（实测踩过）。
		page.Settings.SEO.OGImage = upload.StorageURL(img)
	}
	// 线上路径覆盖模板 canonical（取舍 2）；预览（urlPath 空）不动模板原值。
	if path := strings.TrimSpace(urlPath); path != "" {
		page.Settings.SEO.Canonical = seoCanonicalPath(path)
	}
	return nil
}

// productOfferLD 商品 JSON-LD 扩展：构建期静态 Offer/评分（不含实时库存，SEO-005）。
func productOfferLD(entityType string, writable []string, resolver core.ContentResolver) *builder.ProductOfferLD {
	read := func(field string) string {
		if !fieldWritable(writable, field) {
			return ""
		}
		v, err := resolver.ResolveString(entityType + "." + field)
		if err != nil {
			return ""
		}
		return strings.TrimSpace(v)
	}
	price := read("price")
	if price == "" {
		return nil
	}
	offer := &builder.ProductOfferLD{
		SKU:           read("sku"),
		Price:         price,
		PriceCurrency: "CNY",
		Availability:  "InStock",
	}
	if variants := read("variants"); variants == "" || variants == "[]" {
		offer.Availability = "OutOfStock"
	}
	if rc := read("ratingCount"); rc != "" {
		if n, err := strconv.Atoi(rc); err == nil && n > 0 {
			offer.RatingCount = n
			if rv := read("rating"); rv != "" {
				if f, err := strconv.ParseFloat(rv, 64); err == nil && f > 0 {
					offer.RatingValue = f
				}
			}
		}
	}
	return offer
}

// clampSEOField 按字节上限截断，并回退到完整 UTF-8 边界（不切碎多字节字符）。
//
// 回退后可能变空（只有超长多字节串才会，实际到不了）—— 那时返回空串，
// 调用方据此不写这个键、保留模板原值：半个字的 SEO 标题比没有更糟。
func clampSEOField(entityType, field, value string, limit int) string {
	if len(value) <= limit {
		return value
	}
	cut := value[:limit]
	for len(cut) > 0 && !utf8.ValidString(cut) {
		cut = cut[:len(cut)-1]
	}
	logger.Scene("build").
		With("entityType", entityType).
		With("field", field).
		With("limitBytes", limit).
		With("actualBytes", len(value)).
		Warn("实体 SEO 字段超过页面设置上限，已按上限截断")
	return strings.TrimSpace(cut)
}

// pickSEOField 按候选链取第一个非空字段值（全空返回空串）。
//
// normalize 既决定判空口径，也决定最终写进 SEO 的字节 —— 「富文本里只剩标签」
// 这种值因此会被当成空（不把 <p></p> 写进 meta description）。
func pickSEOField(entityType string, writable []string, resolver core.ContentResolver,
	candidates []string, normalize func(string) string) (string, error) {
	for _, name := range candidates {
		if !fieldWritable(writable, name) {
			continue
		}
		raw, err := resolver.ResolveString(entityType + "." + name)
		if err != nil {
			// 白名单内的字段读不到是真实故障（注册表与解析器口径不一致 / 实体数据损坏）：
			// 返回错误让构建显式失败，而不是静默产出一个没有 SEO 的详情页。
			return "", fmt.Errorf("读取实体 SEO 字段 %s.%s 失败: %w", entityType, name, err)
		}
		if v := normalize(raw); v != "" {
			return v, nil
		}
	}
	return "", nil
}

// fieldWritable 字段是否在该实体类型的白名单内。
func fieldWritable(writable []string, name string) bool {
	for _, f := range writable {
		if f == name {
			return true
		}
	}
	return false
}

// blockBoundaryRe 块级标签的边界（闭合标签与换行标签）。
//
// core.StripRichTags 只取文本节点、不在块之间插分隔，段落会粘成「透气四季可穿」；
// meta 描述与 og:title 要的是可读的单行文本，所以先把边界换成空格再交给它清洗
// （仍然只有一份清洗口径，不另写一套去标签逻辑）。
// 注意 br 的写法要容得下 <br> / <br/> / <br />：漏了带空格那种（HTML 里很常见），
// 换行就会被当成"没有边界"，前后两段文字直接粘在一起。
// 块边界集合要跟着富文本白名单走：白名单新增块级元素（summary/details/thead/tbody/tfoot/
// caption，见 core/richtext.go）而这里不补，去标签后「标题」与「正文」会**粘成一串**
// （<summary>标题</summary><p>正文</p> → 标题正文），meta 描述与 JSON-LD 都会带上这种噪声。
var blockBoundaryRe = regexp.MustCompile(`(?i)</(p|div|li|h[1-6]|tr|td|th|blockquote|summary|details|thead|tbody|tfoot|caption|dt|dd)>|<br\s*/?>`)

// seoPlainText 实体字段值 → 可进 <head> 的纯文本。
//
// 商品描述（product.description）是富文本 HTML（见商品模块的 descriptionHTML），
// 直接写进 meta 会被转义成 "&lt;p&gt;…" 这样的可见噪声，所以含标记时先去标签。
// 纯文本字段（标题 / 摘要）不含标记，原样返回。
func seoPlainText(v string) string {
	v = strings.TrimSpace(v)
	if v == "" || !core.HasRichMarkup(v) {
		return v
	}
	v = blockBoundaryRe.ReplaceAllString(v, " ")
	return strings.TrimSpace(core.StripRichTags(v))
}

// seoDescriptionText 描述的进一步归一：富文本去标签后会留下换行与连续空格，
// 折叠成单空格（meta 描述与 JSON-LD 都是单行文本，产物字节也因此稳定）。
func seoDescriptionText(v string) string {
	return strings.Join(strings.Fields(seoPlainText(v)), " ")
}
