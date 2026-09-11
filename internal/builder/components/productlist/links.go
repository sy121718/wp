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
)

// fragmentPath 列表片段端点（与 runtimefragment 的 capability 名一致）。
const fragmentPath = "/_fragments/productList"

// linkContext 拼链接需要的三样东西：实例配置、当前语义参数、节点 id。
type linkContext struct {
	instanceQuery string
	pushQuery     string
}

// newLinkContext 构建期/片段渲染期各调一次。
//
// pushQuery 只有片段渲染时才有值（构建期不知道访客选了哪些维度）；
// 构建期为空表示「默认态」，此时链接推的就是最朴素的语义参数。
func newLinkContext(nodeID string, p *Props, ctx *core.RenderContext) linkContext {
	return linkContext{
		instanceQuery: fragmentQuery(nodeID, p, ctx),
		pushQuery:     strings.TrimSpace(p.PushQuery),
	}
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
func fragmentQuery(nodeID string, p *Props, ctx *core.RenderContext) string {
	q := url.Values{}
	q.Set("nodeId", nodeID)
	if ctx != nil && strings.TrimSpace(ctx.ProjectID) != "" {
		q.Set("projectId", strings.TrimSpace(ctx.ProjectID))
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
	q.Set("optionKeys", strings.Join(optionFilterKeys(p), ","))
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
