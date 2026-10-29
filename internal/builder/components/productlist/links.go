// links.go — 构建期生成白名单数据请求与可分享的语义参数。
// 作者模板与布局配置留在产物中；JSON 接口仅接收工程、语言、筛选、排序和分页事实。
package productlist

import (
	json "go_wp/pkg/jsonx"
	"net/url"
	"sort"
	"strconv"
	"strings"

	"go_wp/internal/builder/core"
	"go_wp/internal/seo"
)

// catalogPath 公开商品事实接口。
const catalogPath = "/api/site/product/catalog"

// linkContext 保存数据查询、默认交互语义与当前页地址。
type linkContext struct {
	instanceQuery string
	pushQuery     string
	// pageURL 当前页的对外**绝对地址**（ctx.CurrentPath 补站点基址，可空）。
	//
	// 只用于原生链接；前端交互用查询串更新当前地址的历史记录。
	//
	// 用「当前页」而不是「列表页槽位」：组件可能挂在归档页
	// （/product_category/kuz）上，那时槽位解析为空、而指向 /shop 在语义上
	// 也是错的（会把访客从归档页带走）。当前页对两种宿主都成立。
	pageURL string
}

// newLinkContext 将作者默认筛选与当前语义参数固定在静态外壳中。
func newLinkContext(nodeID string, p *Props, ctx *core.RenderContext) linkContext {
	semantic := url.Values{}
	for key, value := range collectionFilter(p) {
		if key != filterKeyStatus {
			semantic.Set(key, value)
		}
	}
	semantic.Set("orderBy", effectiveOrder(p))
	semantic.Set("pageSize", strconv.Itoa(EffectivePageSize(p)))
	semantic.Set("layout", effectiveLayout(p))
	semantic.Set("columns", effectiveColumns(p))
	for key, value := range parseQuery(p.PushQuery) {
		semantic[key] = value
	}
	lc := linkContext{instanceQuery: catalogQuery(nodeID, p, ctx), pushQuery: semantic.Encode()}
	if ctx != nil {
		lc.pageURL = seo.AbsoluteSiteURL(strings.TrimSpace(ctx.CurrentPath))
	}
	return lc
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

// catalogQuery 只输出公开 JSON 接口接受的事实参数，不发送组件属性或模板。
func catalogQuery(_ string, p *Props, ctx *core.RenderContext) string {
	q := url.Values{}
	if ctx != nil {
		q.Set("projectId", ctx.ProjectID)
		if ctx.Lang != "" {
			q.Set("lang", ctx.Lang)
		}
	}
	filter := collectionFilter(p)
	encoded, _ := json.Marshal(filter)
	q.Set("filter", string(encoded))
	q.Set("sort", effectiveOrder(p))
	q.Set("page", strconv.Itoa(EffectivePage(p)))
	size := EffectivePageSize(p)
	if size == 0 {
		size = effectiveLimit(p)
		if size > MaxPageSize {
			size = MaxPageSize
		}
	}
	q.Set("pageSize", strconv.Itoa(size))
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
