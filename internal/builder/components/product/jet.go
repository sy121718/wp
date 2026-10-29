// Package product — Jet 渲染路径辅助导出。
//
// props 解码 / 字段解析（经 ContentResolver） / CSS 生成保留在 Go，
// HTML 拼装交给 product.jet 模板；越界字段在这里显式报错，
// 不静默渲染空块（构建期必须失败，docs/02 §冻结边界）。
package product

import (
	"fmt"
	json "go_wp/pkg/jsonx"
	"math"
	"net/url"
	"strconv"
	"strings"

	"go_wp/internal/builder/core"
)

// CompileCSS 商品详情样式编译（媒体 + 信息区两栏，窄容器自动纵向堆叠）。
func CompileCSS(id string, p *Props, b *core.CSSBuckets) {
	compileCSS(id, p, b)
}

// Image 单张图片的渲染数据。
type Image struct {
	// URL 图片地址（模板输出时由 Jet 默认转义）。
	URL string
	// Alt 替代文本（空串合法 = 装饰性图片；模板始终输出 alt 属性）。
	Alt string
	// Eager 是否立即加载（首图优先，其余懒加载）。
	Eager bool
}

// View 商品详情渲染视图（供 product.jet 模板使用）。
type View struct {
	// HasInfo 是否绑定了任何**信息侧**槽位（标题 / 副标题 / 价格 / 规格 / 描述）。
	//
	// 用途：详情页要把「图集」与「摘要」拆成左右两栏（源站版式），
	// 于是同一个组件会被绑两次、各只填一侧。根容器的两栏栅格对单侧实例没有意义
	// —— 缺的一侧会留下一个空白栏，把内容挤到半边。模板据此加类名、CSS 收敛成单列。
	HasInfo bool
	// TitleTag 标题标签名（h1~h3）。
	TitleTag string
	// StockNote 无脚本 / 数据接口未接入时的库存兜底文案（审计 I18N-010）。
	StockNote string
	// Currency 货币符号（价格前缀）。
	Currency string
	// HasMedia 是否有主图。
	HasMedia bool
	// MediaURL 主图地址。
	MediaURL string
	// MediaAlt 主图替代文本（作者填写的 alt 取译文；空串时回退商品名）。
	MediaAlt string
	// HasGallery 是否有图集。
	HasGallery bool
	// Gallery 图集（多图，按商品图集顺序）。
	Gallery []Image
	// HasTitle / Title 商品名。
	HasTitle bool
	Title    string
	// HasSubtitle / Subtitle 卖点。
	HasSubtitle bool
	Subtitle    string
	// HasPrice / Price 价格（单变体为一口价，多变体为区间）。
	HasPrice bool
	Price    string
	// HasComparePrice / ComparePrice 划线价（有折扣时显示）。
	HasComparePrice bool
	ComparePrice    string
	// HasDiscount / DiscountPercent 折扣角标（如 "-13%"）。
	//
	// 只在「有划线价、且划线价 > 现价」时输出 —— 那是唯一能算出真实折扣的组合。
	// 源站的商品卡与详情图集都在图上压一个蓝色折扣胶囊，这是列表页最显眼的视觉元素。
	HasDiscount     bool
	DiscountPercent string
	// HasDescription / DescriptionHTML 描述（已富文本白名单清洗）。
	HasDescription  bool
	DescriptionHTML string
	// RelatedLink 分类 / 品牌链接项（来自 product.related 的展示文本）。
	// HasCategories / Categories 分类链接（源站放在标题上方）。
	HasCategories bool
	Categories    []RelatedLink
	// HasBrand / Brand 品牌链接（源站放在摘要底部「Brand: xxx」）。
	HasBrand bool
	Brand    RelatedLink
	// Labels 固定文案（多语言 P4）：分类区无障碍标签 / 评价数后缀 / 品牌行标签。
	Labels     Labels
	LiveLabels LiveLabels
	// HasRatingLine 是否输出评价行（源站「0 Reviews  Write a review」）。
	// 只有拿到评价数字段时才出 —— 没有数字的「评价」是空架子。
	HasRatingLine bool
	// RatingValue 平均分（0~5，可能为空：没人评过）。
	RatingValue string
	// RatingCount 评价数。
	RatingCount string

	// HasOptions 是否输出规格选择器：有规格维度、且可展示的规格组合 ≥2 才输出。
	// 单变体商品（含只有无规格占位变体的商品）在前台不输出选择器（issue #8）。
	HasOptions bool
	// OptionGroups 规格维度（颜色 / 尺寸），按商品引用属性组的顺序。
	OptionGroups []OptionGroup
	// VariantOptions 规格组合行（每个组合一行：规格标签 + 价格）。
	VariantOptions []VariantOption
}

// RelatedLink 一个分类 / 品牌链接项。
//
// Slug 用来拼归档页地址（走站点 URL 规则，与商品详情同一份真源），
// Name 是展示文案（已按构建语言取过译文）。
// 两者都可为空：数据缺失时组件**不渲染这一项**，而不是渲染一个点不动的链接。
type RelatedLink struct {
	Slug string
	Name string
	// Href 归档页地址（前缀 + slug，已经过站内链接本地化）。
	// 前缀没配时为空 —— 那时只渲染展示名，不渲染点不动的链接。
	Href string
}

// relatedPayload product.related 字段的 JSON 形状（与商品集合源的 relatedJSON 同源）。
type relatedPayload struct {
	Categories []struct {
		Slug string `json:"slug"`
		Name string `json:"name"`
	} `json:"categories"`
	Brand *struct {
		Slug string `json:"slug"`
		Name string `json:"name"`
	} `json:"brand"`
}

// OptionValue 规格选择器里的一个可选值。
type OptionValue struct {
	// Key 值标识（稳定，与变体 option_values 中的值一致）。
	Key string
	// Label 值显示名。
	Label string
}

// OptionGroup 一个规格维度（如颜色）。
type OptionGroup struct {
	// Key 维度标识（属性组 key）。
	Key string
	// Name 维度显示名（属性组名）。
	Name string
	// Values 可选值（按属性组内定义顺序）。
	Values []OptionValue
}

// VariantOption 一个规格组合行（前台展示用）。
type VariantOption struct {
	// ID 变体 id（issue #24）：产物里烘进「实时可用量片段」的请求参数 ——
	// 库存是运行期真源，构建期只能把 id 写进产物，可用量每次请求现取。
	ID string
	// SKUCode 变体编码。
	SKUCode string
	// Price 价格（已带货币符号）。
	Price string
	// ComparePrice 划线价（已带货币符号；空串 = 不输出）。
	ComparePrice string
	// Labels 组合的展示文本（如「颜色 红 · 尺寸 S」）。
	Labels string
	// PriceCents 构建期价（**分**）：作为 data-price 烘进产物，供前端价格核对
	// 与库里的当前价比对（BIZ-2）。用分而不是元文本，是为了与跨模块的
	// VariantSnapshotPort 同一口径 —— 少一次浮点解析，就少一次「99.90 读成 99.9」的假提示。
	PriceCents int64
	// PriceFactsURL 与 StockFactsURL 共用只读数据入口；价格不可解析时不显示改价提示。
	PriceFactsURL string
	StockFactsURL string
}

// optionGroupJSON 商品解析器输出的规格维度结构（product.options）。
type optionGroupJSON struct {
	Key    string `json:"key"`
	Name   string `json:"name"`
	Values []struct {
		Key   string `json:"key"`
		Label string `json:"label"`
	} `json:"values"`
}

// variantJSON 商品解析器输出的规格组合结构（product.variants）。
type variantJSON struct {
	ID           string            `json:"id"`
	SKU          string            `json:"sku"`
	Price        string            `json:"price"`
	ComparePrice string            `json:"comparePrice"`
	Image        string            `json:"image"`
	Enabled      bool              `json:"enabled"`
	Options      map[string]string `json:"options"`
}

// BuildView 生成商品详情渲染视图：逐槽位经内容解析器取商品字段值。
//
// content 为构建期注入的商品解析器（未注入时声明了槽位即报错，不静默出空块）；
// 解析失败（字段越界 / 类型不符）原样上抛，由编译链报错终止发布。
//
// projectID 显式写入公开商品数据 URL；缺工程时只保留构建期内容。
// lang 保留在请求 URL 中，展示文案由 ApplyI18n 在构建期冻结。
// siteLink 站内链接本地化器（可空）：把作者填的逻辑路径转成当前语言的访问地址。
//
// 与 core.articleList / core.productCard 同一约定 —— 组件包里不 import 本地化实现，
// 由 builder 的视图装配层注入（ctx.ResolveSiteLink）。
func BuildView(p *Props, content core.ContentResolver, projectID, lang string, siteLink func(string) string) (View, error) {
	source := effectiveSource(p)
	slots := p.slotFields()
	declared := 0
	for _, s := range slots {
		if s.Field != "" {
			declared++
		}
	}
	if declared == 0 {
		return View{}, fmt.Errorf("至少需要声明一个商品字段（主图/图集/标题/副标题/价格/划线价/描述/规格维度/变体组合）")
	}
	if content == nil {
		return View{}, fmt.Errorf("编译上下文缺少内容解析器，无法解析商品字段（数据源 %s）", source)
	}

	// StockNote 先落中文兜底：ApplyI18n 会在 BuildView 之后按语言覆盖；
	// 未接入 i18n 时它就是最终值（产物与抽 key 前逐字一致）。
	view := View{TitleTag: effectiveTitleTag(p), Currency: effectiveCurrency(p), StockNote: textFallbackStockNote}
	// 分类 / 品牌归档地址的前缀：**一次**本地化，循环里只管拼 slug。
	// 本地化器可能为空（单测 / 未装配的渲染路径）——那时前缀保持原样。
	categoryPrefix := localizePrefix(p.CategoryLinkPrefix, defaultCategoryLinkPrefix, siteLink)
	brandPrefix := localizePrefix(p.BrandLinkPrefix, defaultBrandLinkPrefix, siteLink)
	// 规格数据与 alt 先收原值，槽位循环结束后再统一解析（图集 alt 要按「第 i 张」
	// 对应，而 alt 槽位可能声明在图集槽位之前；组合行要按维度取标签）。
	var rawOptions, rawVariants, rawMediaAlt, rawGalleryAlt string
	// 折扣角标要的是**纯数值**，而 view.Price / view.ComparePrice 都拼了货币符号 ——
	// 拿它们去 ParseFloat 会失败、角标永远不显示（实测）。所以先收原始文本，
	// 槽位循环结束后再算（两个槽位的先后顺序也不保证）。
	var rawPrice, rawComparePrice string
	for _, s := range slots {
		if s.Field == "" {
			continue
		}
		value, err := content.ResolveString(s.Field)
		if err != nil {
			return View{}, fmt.Errorf("解析商品字段 %q 失败: %w", s.Field, err)
		}
		if strings.TrimSpace(value) == "" {
			continue
		}
		switch s.Slot {
		case slotMedia:
			view.HasMedia, view.MediaURL = true, strings.TrimSpace(value)
		case slotMediaAlt:
			rawMediaAlt = strings.TrimSpace(value)
		case slotGallery:
			images := parseImages(value, "")
			if len(images) > 0 {
				view.HasGallery, view.Gallery = true, images
			}
		case slotGalleryAlt:
			rawGalleryAlt = strings.TrimSpace(value)
		case slotTitle:
			view.HasTitle, view.Title = true, value
		case slotSubtitle:
			view.HasSubtitle, view.Subtitle = true, value
		case slotCategories:
			catOK, catItems := parseRelatedCategories(value)
			view.HasCategories, view.Categories = linkRelated(catOK, catItems, categoryPrefix)
		case slotBrand:
			brandOK, brandItem := parseRelatedBrand(value)
			view.HasBrand, view.Brand = linkRelatedOne(brandOK, brandItem, brandPrefix)
		case slotRating:
			view.RatingValue = strings.TrimSpace(value)
		case slotRatingCount:
			view.RatingCount = strings.TrimSpace(value)
		case slotPrice:
			rawPrice = strings.TrimSpace(value)
			view.HasPrice, view.Price = true, view.Currency+rawPrice
		case slotComparePrice:
			rawComparePrice = strings.TrimSpace(value)
			view.HasComparePrice, view.ComparePrice = true, view.Currency+rawComparePrice
		case slotDescription:
			view.HasDescription, view.DescriptionHTML = true, core.RichTextHTML(value)
		case slotOptions:
			rawOptions = value
		case slotVariants:
			rawVariants = value
		}
	}
	view.HasDiscount, view.DiscountPercent = discountBadge(rawPrice, rawComparePrice)
	// 评价行只认**评价数**：没有它就没有「几条评价」这个事实，
	// 单独渲染一个 0 分或一排空星星都是编出来的内容。
	view.HasRatingLine = view.RatingCount != ""
	view.HasInfo = view.HasTitle || view.HasSubtitle || view.HasPrice ||
		view.HasDescription || view.HasOptions || len(view.VariantOptions) > 0
	// 图集 alt（issue #12）：按位填入作者填写的 alt（逐元素已按构建语言取译文）；
	// 缺位 / 空串回退商品名（无商品名时留空 = 装饰性图片，模板仍输出 alt=""）。
	galleryAlts := parseAltList(rawGalleryAlt)
	for i := range view.Gallery {
		if i < len(galleryAlts) && galleryAlts[i] != "" {
			view.Gallery[i].Alt = galleryAlts[i]
			continue
		}
		if view.HasTitle {
			view.Gallery[i].Alt = view.Title
		}
	}
	// 主图 alt：作者填了就用它（取译文后），否则回退商品名。
	if view.HasMedia && rawMediaAlt != "" {
		view.MediaAlt = rawMediaAlt
	} else if view.HasMedia && view.HasTitle {
		view.MediaAlt = view.Title
	}
	// 规格选择器：有维度且可展示的组合 ≥2 才输出 —— 单变体商品不输出选择器。
	view.OptionGroups = ParseOptionGroups(rawOptions)
	view.VariantOptions = ParseVariantOptions(rawVariants, view.OptionGroups, view.Currency, projectID, lang)
	view.HasOptions = len(view.OptionGroups) > 0 && len(view.VariantOptions) > 1
	return view, nil
}

// ParseOptionGroups 规格维度 JSON → 视图结构。
//
// 结构对不上（空串 / 非法 JSON / 旧形态）时返回空：选择器不输出，
// 而不是让整个商品详情页构建失败（字段本身已由白名单校验过合法性）。
func ParseOptionGroups(raw string) []OptionGroup {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	var rows []optionGroupJSON
	if err := json.Unmarshal([]byte(raw), &rows); err != nil {
		return nil
	}
	out := make([]OptionGroup, 0, len(rows))
	for _, r := range rows {
		if r.Key == "" {
			continue
		}
		values := make([]OptionValue, 0, len(r.Values))
		for _, v := range r.Values {
			if v.Key == "" {
				continue
			}
			values = append(values, OptionValue{Key: v.Key, Label: v.Label})
		}
		if len(values) == 0 {
			continue
		}
		out = append(out, OptionGroup{Key: r.Key, Name: r.Name, Values: values})
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// ParseVariantOptions 规格组合 JSON → 视图行（只保留能对上全部维度的组合）。
//
// 两条过滤规则：
//   - 无规格组合（option_values 为空）不进规格清单 —— 它是商品的占位 / 手工变体，
//     不是规格选择器里的一格；
//   - 未启用的变体不上架，因而不出现在选择器里（组合计数也不含它）。
//
// projectID 只透传给实时价格核对片段的 URL（见 livePriceFragmentURL）：三个调用方
// （商品详情 / 独立选择器 / 加购）都必须给出自己那份构建上下文里的工程 id。
//
// lang 与 projectID 一样只透传给那两个片段 URL：片段语言只能从 lang 查询参数来（见 BuildView）。
// FirstEnabledVariant 取第一个**启用**的变体（简单商品：没有规格维度，唯一可买的那件）。
//
// 与 ParseVariantOptions 的分工：那个要求变体带完整的规格组合（选择器要按维度取标签），
// 简单商品没有 option_values，走那条路会被 `len(r.Options) == 0` 整条滤掉。
// 于是货架上**绝大多数**商品（一个口味一件 SKU、没有颜色尺寸可选）的加购按钮
// 渲染成「暂无可购买的规格」—— 页面上最该能点的按钮点不了，
// 而且它看起来像「这件缺货」，不像「组件不支持这种商品形态」。
//
// 判据只要求「启用」：价格可以为空（那时按钮照出，价格由商品详情区展示）。
func FirstEnabledVariant(raw, currency string) (VariantOption, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return VariantOption{}, false
	}
	var rows []variantJSON
	if err := json.Unmarshal([]byte(raw), &rows); err != nil {
		return VariantOption{}, false
	}
	for _, r := range rows {
		if !r.Enabled || strings.TrimSpace(r.ID) == "" {
			continue
		}
		out := VariantOption{ID: r.ID, SKUCode: r.SKU}
		if price := strings.TrimSpace(r.Price); price != "" {
			out.Price, out.PriceCents = currency+price, 0
		}
		if compare := strings.TrimSpace(r.ComparePrice); compare != "" {
			out.ComparePrice = currency + compare
		}
		return out, true
	}
	return VariantOption{}, false
}

func ParseVariantOptions(raw string, groups []OptionGroup, currency, projectID, lang string) []VariantOption {
	raw = strings.TrimSpace(raw)
	// **不因「没有规格维度」而返回空**：本系统没有「简单商品」概念，
	// 单 SKU 就是只有一个变体、没有规格维度的可变商品。早先这里写作
	// `raw == "" || len(groups) == 0`，于是单 SKU 商品在加购处被整条滤掉、
	// 渲染成「暂无可购买的规格」—— 页面上最该能点的按钮点不了。
	// 没有维度时下面 parts 循环不执行、complete 保持 true，出来的是一条无维度变体行。
	if raw == "" {
		return nil
	}
	var rows []variantJSON
	if err := json.Unmarshal([]byte(raw), &rows); err != nil {
		return nil
	}
	labels := make(map[string]map[string]string, len(groups))
	for _, g := range groups {
		m := make(map[string]string, len(g.Values))
		for _, v := range g.Values {
			m[v.Key] = v.Label
		}
		labels[g.Key] = m
	}
	out := make([]VariantOption, 0, len(rows))
	for _, r := range rows {
		// 只按「启用」过滤，**不按有没有规格维度过滤**。
		//
		// 本系统没有「简单商品」这个概念：单 SKU 就是只有一个变体的可变商品。
		// 早先这里还要求 `len(r.Options) > 0`，于是没有 option_values 的变体
		// 被整条滤掉 —— 单 SKU 商品的加购按钮渲染成「暂无可购买的规格」
		// （页面上最该能点的按钮点不了，且看起来像缺货而不是像组件不支持）。
		// 规格维度为空时下面 parts 循环不执行、complete 保持 true，
		// 出来的就是一条**无维度**的变体行（Labels 为空），这正是单 SKU 要的样子。
		if !r.Enabled {
			continue
		}
		parts := make([]string, 0, len(groups))
		complete := true
		for _, g := range groups {
			key, ok := r.Options[g.Key]
			if !ok {
				complete = false
				break
			}
			label, ok := labels[g.Key][key]
			if !ok {
				complete = false
				break
			}
			name := g.Name
			if name == "" {
				name = g.Key
			}
			parts = append(parts, name+" "+label)
		}
		if !complete {
			continue
		}
		row := VariantOption{
			ID:      r.ID,
			SKUCode: r.SKU, Labels: strings.Join(parts, " · "),
			Price: currency + r.Price,
		}
		// 实时可用量位（issue #24）：与价格核对位同形 —— 构建期只烘「变体 id + 工程 id」，
		// 可用量每次请求现取（烘进产物等于发布一份过期库存）。
		row.StockFactsURL = variantFactsURL(r.ID, projectID, lang)
		// 实时价格核对（BIZ-2）：价格形状可解析时才烘 URL —— 解析不出来的价拿去比对
		// 只会得到一句错话，不如不请求。
		if cents, ok := priceYuanCents(r.Price); ok {
			row.PriceCents = cents
			row.PriceFactsURL = row.StockFactsURL
		}
		if strings.TrimSpace(r.ComparePrice) != "" {
			row.ComparePrice = currency + r.ComparePrice
		}
		out = append(out, row)
	}
	return out
}

// PublicVariantsPath 只读 JSON 数据入口；价格比较和库存提示由前端处理。
const PublicVariantsPath = "/api/site/product/variants"
const paramProjectID = "projectId"

func variantFactsURL(variantID, projectID, lang string) string {
	if strings.TrimSpace(variantID) == "" || strings.TrimSpace(projectID) == "" {
		return ""
	}
	q := url.Values{}
	q.Set(paramProjectID, strings.TrimSpace(projectID))
	q.Set("variantIds", variantID)
	if lang != "" {
		q.Set("lang", lang)
	}
	return PublicVariantsPath + "?" + q.Encode()
}

// 归档页默认路径前缀（与内置的默认 URL 规则一致；作者可在组件属性里覆盖）。
const (
	defaultCategoryLinkPrefix = "/product_category"
	defaultBrandLinkPrefix    = "/product_brand"
)

// localizePrefix 取生效的链接前缀并本地化（空则回落默认值）。
func localizePrefix(configured, fallback string, siteLink func(string) string) string {
	prefix := strings.TrimSpace(configured)
	if prefix == "" {
		prefix = fallback
	}
	if siteLink == nil {
		return prefix
	}
	return siteLink(prefix)
}

// linkRelated 给分类项拼归档地址（前缀为空则整批不带链接）。
func linkRelated(ok bool, items []RelatedLink, prefix string) (bool, []RelatedLink) {
	if !ok {
		return false, nil
	}
	for i := range items {
		items[i].Href = joinArchiveHref(prefix, items[i].Slug)
	}
	return true, items
}

func linkRelatedOne(ok bool, item RelatedLink, prefix string) (bool, RelatedLink) {
	if !ok {
		return false, RelatedLink{}
	}
	item.Href = joinArchiveHref(prefix, item.Slug)
	return true, item
}

// joinArchiveHref 前缀 + slug。前缀已本地化（可能是绝对地址），所以这里只处理斜杠，
// 不重新拼域名 —— 再拼一次会把绝对地址变成 "http://host/http://host/..."。
func joinArchiveHref(prefix, slug string) string {
	p, s := strings.TrimSpace(prefix), strings.TrimSpace(slug)
	if p == "" || s == "" {
		return ""
	}
	if !strings.HasSuffix(p, "/") {
		p += "/"
	}
	return p + s
}

// parseRelatedCategories 从 product.related 里取分类链接（无 / 解析失败返回空）。
//
// 解析失败**静默返回空**而不是报错：related 是派生展示数据，缺了它该少一行链接，
// 不该让整个商品详情页构建失败（同一个商品在别的模板上仍然要能渲染）。
func parseRelatedCategories(raw string) (bool, []RelatedLink) {
	p, ok := parseRelated(raw)
	if !ok {
		return false, nil
	}
	out := make([]RelatedLink, 0, len(p.Categories))
	for _, c := range p.Categories {
		if strings.TrimSpace(c.Slug) == "" || strings.TrimSpace(c.Name) == "" {
			continue
		}
		out = append(out, RelatedLink{Slug: c.Slug, Name: c.Name})
	}
	if len(out) == 0 {
		return false, nil
	}
	return true, out
}

// parseRelatedBrand 从 product.related 里取品牌（无 / 解析失败返回空）。
func parseRelatedBrand(raw string) (bool, RelatedLink) {
	p, ok := parseRelated(raw)
	if !ok || p.Brand == nil {
		return false, RelatedLink{}
	}
	slug, name := strings.TrimSpace(p.Brand.Slug), strings.TrimSpace(p.Brand.Name)
	if slug == "" || name == "" {
		return false, RelatedLink{}
	}
	return true, RelatedLink{Slug: slug, Name: name}
}

func parseRelated(raw string) (relatedPayload, bool) {
	var p relatedPayload
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" || trimmed == "{}" {
		return p, false
	}
	if err := json.Unmarshal([]byte(trimmed), &p); err != nil {
		return p, false
	}
	return p, true
}

// discountBadge 折扣角标文案（如 "-13%"），算不出真实折扣时返回空串。
//
// 只在两个价格都是**正数**且划线价严格大于现价时才算：
//
//	· 相等 → 没有折扣，画一个 "-0%" 是噪声；
//	· 现价更高 → 数据异常，此时显示正数百分号会误导（看起来像加价）；
//	· 任一解析失败 → 不显示（宁可没有角标，也不要一个错的百分比）。
//
// 取整用向下（floor）：向上取整会把 12.5% 写成 13%，而电商标价惯例是**不虚增**折扣力度。
func discountBadge(priceText, compareText string) (bool, string) {
	price, perr := strconv.ParseFloat(strings.TrimSpace(priceText), 64)
	compare, cerr := strconv.ParseFloat(strings.TrimSpace(compareText), 64)
	if perr != nil || cerr != nil || price <= 0 || compare <= price {
		return false, ""
	}
	percent := int(math.Floor((compare - price) / compare * 100))
	if percent <= 0 {
		return false, ""
	}
	return true, "-" + strconv.Itoa(percent) + "%"
}

// priceYuanCents 元文本（商品字段解析器 formatPrice 的输出形态，如 "99" / "99.5"）→ 分。
//
// 解析失败或负值返回 false：调用方据此不请求片段（而不是按 0 元比对，
// 那会把每个变体都判成「价格已更新」）。
func priceYuanCents(text string) (int64, bool) {
	v, err := strconv.ParseFloat(strings.TrimSpace(text), 64)
	if err != nil || math.IsNaN(v) || math.IsInf(v, 0) || v < 0 {
		return 0, false
	}
	return int64(math.Round(v * 100)), true
}

// parseAltList 图集 alt 字段值（JSON 字符串数组）→ alt 列表。
//
// 结构对不上（空串 / 非法 JSON）返回空列表：调用方逐位回退商品名，
// 不让整个商品详情页构建失败。元素中的纯空白按「未填写」处理。
func parseAltList(raw string) []string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	var alts []string
	if err := json.Unmarshal([]byte(raw), &alts); err != nil {
		return nil
	}
	out := make([]string, 0, len(alts))
	for _, a := range alts {
		out = append(out, strings.TrimSpace(a))
	}
	return out
}

// parseImages 图集字段值 → 图片列表。
//
// 商品图集字段是 JSON 数组字符串（商品解析器按 JSON 数组输出，core.gallery 亦按此解析）；
// 兼容逗号分隔与单个 URL 两种退化形态。
func parseImages(raw, alt string) (images []Image) {
	var urls []string
	if err := json.Unmarshal([]byte(raw), &urls); err != nil {
		for _, part := range strings.Split(raw, ",") {
			if part = strings.TrimSpace(part); part != "" {
				urls = append(urls, part)
			}
		}
	}
	for _, u := range urls {
		if u = strings.TrimSpace(u); u == "" {
			continue
		}
		images = append(images, Image{URL: u, Alt: alt, Eager: len(images) == 0})
	}
	return images
}

// DeclareFeatures 只在实际输出实时数据位时注入读取脚本。
func (v View) DeclareFeatures() (attrs, classes []string) {
	if !v.HasOptions || len(v.VariantOptions) == 0 {
		return nil, nil
	}
	// 按**实际烘出的 URL** 判断，而不是按「有规格组合」：缺工程 id 时两个片段位都不输出，
	// 此时登记 hx-* 会让产物白白多注入一份 htmx 脚本（PERF-014 的登记就是为这件事设的）。
	for _, vo := range v.VariantOptions {
		if vo.PriceFactsURL != "" || vo.StockFactsURL != "" {
			return []string{"data-site-read", "data-site-product"}, nil
		}
	}
	return nil, nil
}
