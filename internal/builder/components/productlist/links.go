// links.go — 列表交互控件的 URL 拼装（issue #27）。
//
// 两类 URL 分工明确，这是整套「局部刷新 + URL 同步」的关键：
//
//	FragmentGet  片段请求的完整 URL —— 带**实例配置**（nodeId / projectId / 字段槽位 / 布局 / 条数）
//	             加上当前语义参数与本次变化；它不进地址栏。
//	PushURL      浏览器地址栏要变成的查询串 —— 只带**语义参数**（筛选 / 排序 / 分页），
//	             保持可分享、可刷新、可前进后退的形状。
//
// 拼装全部在构建期/片段渲染期完成（Go 侧），模板只管把字符串放进 hx-get / hx-push-url / href：
// 模板里拼 URL 很容易漏掉转义与参数顺序，而且没法写测试。
package productlist

import (
	"net/url"
	"sort"
	"strconv"
	"strings"

	"go_wp/internal/builder/core"
	"go_wp/internal/seo"
)

// fragmentPath 列表片段端点（与 runtimefragment 的 capability 名一致）。
const fragmentPath = "/_fragments/productList"

// linkContext 拼链接需要的三样东西：实例配置、当前语义参数、节点 id。
type linkContext struct {
	instanceQuery string
	pushQuery     string
	// pageURL 当前页的对外**绝对地址**（ctx.CurrentPath 补站点基址，可空）。
	//
	// 只用于**降级链接**（href，无 JS 时整页跳转）：htmx 路径走 pushURL
	// （地址栏查询串，必须保持相对，htmx 才能按当前路径推入历史）。
	//
	// 用「当前页」而不是「列表页槽位」：组件可能挂在归档页
	// （/product_category/kuz）上，那时槽位解析为空、而指向 /shop 在语义上
	// 也是错的（会把访客从归档页带走）。当前页对两种宿主都成立。
	pageURL string
}

// newLinkContext 构建期/片段渲染期各调一次。
//
// pushQuery 只有片段渲染时才有值（构建期不知道访客选了哪些维度）；
// 构建期为空表示「默认态」，此时链接推的就是最朴素的语义参数。
func newLinkContext(nodeID string, p *Props, ctx *core.RenderContext) linkContext {
	lc := linkContext{
		instanceQuery: fragmentQuery(nodeID, p, ctx),
		pushQuery:     strings.TrimSpace(p.PushQuery),
	}
	if ctx != nil {
		lc.pageURL = seo.AbsoluteSiteURL(strings.TrimSpace(ctx.CurrentPath))
	}
	return lc
}

// fragmentGet 片段请求 URL：实例配置 + 当前语义参数 + 本次覆盖。
func (c linkContext) fragmentGet(override url.Values) string {
	q := c.merged(override)
	return fragmentPath + "?" + q.Encode()
}

// pushURL 地址栏 URL：只含语义参数（不含实例配置）。
func (c linkContext) pushURL(override url.Values) string {
	values := url.Values{}
	for k, v := range parseQuery(c.pushQuery) {
		values[k] = v
	}
	for k, v := range override {
		if len(v) == 0 || strings.TrimSpace(v[0]) == "" {
			values.Del(k)
			continue
		}
		values[k] = v
	}
	if encoded := values.Encode(); encoded != "" {
		return "?" + encoded
	}
	return "?"
}

// fallbackHref 降级链接（无 JS 时的整页跳转地址）。
//
// 有列表页地址就拼成绝对地址（约定：站内地址一律绝对）；
// 槽位没绑（列表挂在非列表页上）时退回查询相对形式 —— 那种情况下没有
// 「列表页」可指，查询串按当前文档解析才是对的语义。
func (c linkContext) fallbackHref(override url.Values) string {
	q := c.pushURL(override)
	if c.pageURL == "" {
		return q
	}
	if q == "?" {
		return c.pageURL
	}
	return c.pageURL + q
}

// merged 实例配置 + 语义参数 + 覆盖，合成片段请求的查询参数。
func (c linkContext) merged(override url.Values) url.Values {
	q := url.Values{}
	for k, v := range parseQuery(c.instanceQuery) {
		q[k] = v
	}
	for k, v := range parseQuery(c.pushQuery) {
		q[k] = v
	}
	for k, v := range override {
		if len(v) == 0 || strings.TrimSpace(v[0]) == "" {
			q.Del(k)
			continue
		}
		q[k] = v
	}
	return q
}

// parseQuery 解析查询串（非法编码返回空表，不 panic —— URL 来自作者配置与访客地址栏）。
func parseQuery(raw string) url.Values {
	raw = strings.Trim(strings.TrimSpace(raw), "&")
	if raw == "" {
		return url.Values{}
	}
	values, err := url.ParseQuery(raw)
	if err != nil {
		return url.Values{}
	}
	return values
}

// fragmentQuery 构建期拼好的**实例配置**（片段请求的固定部分）。
//
// 只放白名单参数：片段端会再校验一遍（字段槽位过商品字段白名单），
// 这里拼错也只是被拒，不会变成任意配置注入。
//
// lang 也在这份实例配置里（I18N-011）：片段语言只从 ?lang= 来（端点不读
// Accept-Language、不读语言 cookie，见 runtimefragment.resolveRequestLang）——
// 不带 lang 的请求恒回落**工程默认语言**，于是英文站刷新列表就出中文。
// 只在非空时带上：单语言站点多一个空 lang 参数只会让片段多做一次无用判断。
func fragmentQuery(nodeID string, p *Props, ctx *core.RenderContext) string {
	q := url.Values{}
	q.Set("nodeId", nodeID)
	if ctx != nil {
		if projectID := strings.TrimSpace(ctx.ProjectID); projectID != "" {
			q.Set("projectId", projectID)
		}
		if lang := strings.TrimSpace(ctx.Lang); lang != "" {
			q.Set("lang", lang)
		}
	}
	for param, value := range map[string]string{
		"layout":            effectiveLayout(p),
		"columns":           effectiveColumns(p),
		"currency":          effectiveCurrency(p),
		"titleTag":          effectiveTitleTag(p),
		"emptyText":         effectiveEmptyText(p),
		"linkPrefix":        propOrEmpty(p, func(pp *Props) string { return pp.LinkPrefix }),
		"imageField":        propOrEmpty(p, func(pp *Props) string { return pp.ImageField }),
		"imageAltField":     propOrEmpty(p, func(pp *Props) string { return pp.ImageAltField }),
		"titleField":        propOrEmpty(p, func(pp *Props) string { return pp.TitleField }),
		"priceField":        propOrEmpty(p, func(pp *Props) string { return pp.PriceField }),
		"comparePriceField": propOrEmpty(p, func(pp *Props) string { return pp.ComparePriceField }),
		"tagsField":         propOrEmpty(p, func(pp *Props) string { return pp.TagsField }),
		"linkField":         propOrEmpty(p, func(pp *Props) string { return pp.LinkField }),
		// 决定**筛选栏 / 工具栏长什么样**的开关与数据。
		//
		// 缺了它们，片段重渲染出来的列表**没有筛选栏**（实例配置里没这个开关，
		// 组件按默认值渲染），而产物里的初始列表有 —— 表现是
		// 「点一下筛选，筛选栏整块消失」，且此后再也回不来。
		// 这些是「这个实例怎么渲染」而不是「访客要哪批数据」，属于实例配置。
		"filters":       propOrEmpty(p, func(pp *Props) string { return pp.Filters }),
		"caretIcon":     propOrEmpty(p, func(pp *Props) string { return pp.CaretIcon }),
		"categoryMulti": propOrEmpty(p, func(pp *Props) string { return pp.CategoryMulti }),
		"toolbar":       propOrEmpty(p, func(pp *Props) string { return pp.Toolbar }),
		"priceRanges":   propOrEmpty(p, func(pp *Props) string { return pp.PriceRanges }),
		"priceSlider":   propOrEmpty(p, func(pp *Props) string { return pp.PriceSlider }),
		"priceBounds":   propOrEmpty(p, func(pp *Props) string { return pp.PriceBounds }),
		"ratingOptions": propOrEmpty(p, func(pp *Props) string { return pp.RatingOptions }),
	} {
		if value != "" {
			q.Set(param, value)
		}
	}
	if p != nil {
		if p.CollectionLimit > 0 {
			q.Set("limit", strconv.Itoa(p.CollectionLimit))
		}
		if EffectivePageSize(p) > 0 {
			q.Set("pageSize", strconv.Itoa(EffectivePageSize(p)))
		}
	}
	// 属性维度的键列出来：片段端据此知道要保留哪些 option.<key> 参数（否则筛选栏态会丢）。
	//
	// 空则不写：空值参数一样占用 GET 的参数名预算（maxParamCount），而实例配置本就有十几个键。
	if keys := optionFilterKeys(p); len(keys) > 0 {
		q.Set("optionKeys", strings.Join(keys, ","))
	}
	return q.Encode()
}

// propOrEmpty 取 props 字段值（nil 安全）。
func propOrEmpty(p *Props, pick func(*Props) string) string {
	if p == nil {
		return ""
	}
	return strings.TrimSpace(pick(p))
}

// optionFilterKeys 属性筛选的键列表（排序后，保证同输入同 URL）。
func optionFilterKeys(p *Props) []string {
	if p == nil {
		return nil
	}
	keys := make([]string, 0, 4)
	for _, pair := range strings.Split(p.FilterOptions, ",") {
		key, _, ok := strings.Cut(strings.TrimSpace(pair), ":")
		if !ok {
			continue
		}
		if key = strings.TrimSpace(key); key != "" {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	return keys
}
