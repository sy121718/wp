// Package productcard — Jet 渲染路径（视图组装）。
//
// props 解码 / 字段解析 / 转义边界保持在 Go，HTML 拼装交给 product_card.jet：
// 模板不做判断型业务逻辑，只按视图里已经填好的布尔量输出节点。
package productcard

import (
	"encoding/json"
	"fmt"
	"strings"

	"go_wp/internal/builder/core"
)

// CompileCSS 商品卡样式编译（jetview 经本入口调用，实现仍在 productcard.go）。
func CompileCSS(id string, p *Props, b *core.CSSBuckets) {
	compileCSS(id, p, b)
}

// View 商品卡渲染视图（供 product_card.jet 使用）。
//
// 全部布尔量（HasImage / HasTitle / HasPrice …）都是「解析后确实有值」的意思：
// 商品没填的字段不留空壳节点（空 <div>、空 <li>、alt 为空的装饰图都会污染产物）。
type View struct {
	TitleTag string
	Currency string

	HasImage bool
	ImageURL string
	// ImageAlt 主图替代文本（作者字段优先，缺失回退标题；模板始终输出 alt 属性）。
	ImageAlt string

	HasTitle bool
	Title    string

	// Href 卡片链接（空 = 不输出 <a>，卡片仍是纯展示）。
	Href string

	HasPrice bool
	Price    string

	HasComparePrice bool
	ComparePrice    string

	// Tags 标签展示名（空数组 = 不输出标签行）。
	Tags []string
}

// BuildView 生成商品卡视图：逐槽位经内容解析器取商品字段值。
//
// content 为构建期注入的解析器：集合里是集合项作用域（item.* 取当前商品），
// 集合外是页面级解析器（product.* 取当前实体）。未注入且声明了槽位即报错 ——
// 静默渲染成空卡比构建失败危险得多（页面上看不出少了什么）。
// siteLink 站内链接本地化器（审计 I18N-015，可空）：LinkPrefix 是作者填的站内逻辑路径
// （如 /products/），拼出的详情地址要按当前语言加前缀。
func BuildView(p *Props, content core.ContentResolver, siteLink func(string) string) (view View, err error) {
	if p == nil {
		return View{}, fmt.Errorf("商品卡属性为空")
	}
	slots := p.slotFields()
	declared := 0
	for _, s := range slots {
		if s.Field != "" {
			declared++
		}
	}
	if declared == 0 {
		return View{}, fmt.Errorf("至少需要声明一个商品字段（主图 / 标题 / 价格 / 划线价 / 标签 / 链接）")
	}
	if content == nil {
		return View{}, fmt.Errorf("编译上下文缺少内容解析器，无法解析商品字段")
	}

	view = View{TitleTag: effectiveTitleTag(p), Currency: effectiveCurrency(p)}
	var rawImageAlt, rawTitle string
	for _, s := range slots {
		if s.Field == "" {
			continue
		}
		value, rerr := content.ResolveString(s.Field)
		if rerr != nil {
			return View{}, fmt.Errorf("解析商品字段 %q 失败: %w", s.Field, rerr)
		}
		if strings.TrimSpace(value) == "" {
			continue // 空值 = 不输出该节点
		}
		switch s.Slot {
		case slotImage:
			if url := FirstImageURL(value); url != "" {
				view.HasImage, view.ImageURL = true, url
			}
		case slotImageAlt:
			rawImageAlt = strings.TrimSpace(value)
		case slotTitle:
			view.HasTitle, view.Title = true, value
			rawTitle = strings.TrimSpace(value)
		case slotPrice:
			view.HasPrice, view.Price = true, view.Currency+strings.TrimSpace(value)
		case slotComparePrice:
			view.HasComparePrice, view.ComparePrice = true, view.Currency+strings.TrimSpace(value)
		case slotTags:
			view.Tags = ParseTagNames(value)
		case slotLink:
			// 只本地化作者填的**前缀**，CMS 字段值原样参与拼接（见 LocalizePrefix）。
			view.Href = CardHref(LocalizePrefix(p.LinkPrefix, siteLink), value)
		}
	}
	// 主图 alt：作者填的 alt 字段优先，缺失回退标题（有图才有 alt 的意义）。
	if view.HasImage {
		view.ImageAlt = rawImageAlt
		if view.ImageAlt == "" {
			view.ImageAlt = rawTitle
		}
	}
	return view, nil
}

// FirstImageURL 主图取值：解析器给的可能是单个 URL，也可能是 JSON 数组文本 —— 两种都吃下。
//
// 集合项里的 images 按既有约定已被解析器取成首元素 URL；实体绑定（product.images）
// 给的是 JSON 文本。组件不假设调用方是哪一个，否则两种场景得写两套 props。
func FirstImageURL(value string) string {
	trimmed := strings.TrimSpace(value)
	if strings.HasPrefix(trimmed, "[") {
		var urls []string
		if uerr := json.Unmarshal([]byte(trimmed), &urls); uerr != nil {
			return ""
		}
		for _, u := range urls {
			if s := strings.TrimSpace(u); s != "" {
				return s
			}
		}
		return ""
	}
	if strings.HasPrefix(trimmed, "{") {
		// 对象形状（如 related / variants）不是图片，取不到 URL 就不输出图。
		return ""
	}
	return trimmed
}

// ParseTagNames 标签字段：JSON 名称数组 → 字符串数组。
//
// 非数组形状按「单个标签名」处理：作者把 tagsField 指到一个字符串字段时，
// 卡片显示那一个标签，而不是整块消失。
func ParseTagNames(value string) []string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return nil
	}
	if strings.HasPrefix(trimmed, "[") {
		var names []string
		if uerr := json.Unmarshal([]byte(trimmed), &names); uerr != nil {
			return nil
		}
		out := make([]string, 0, len(names))
		for _, n := range names {
			if s := strings.TrimSpace(n); s != "" {
				out = append(out, s)
			}
		}
		return out
	}
	return []string{trimmed}
}

// CardHref 卡片链接：前缀 + 字段值，三种写法都不会拼出坏链接。
//
// 字段值分三类：
//
//  1. 站内相对片段（如 slug "summer-shirt"，不含 scheme 也不以 / 开头）→ 拼前缀；
//  2. 已是绝对地址（/ 开头的站内路径、http(s):// 外链、# 锚点）→ 原样输出，
//     否则 /products/ + /products/x 会变成 /products//products/x；
//  3. 其它（javascript: / data: 等危险 scheme，或含非法字符）→ 空串：不输出链接。
//
// 注意不能拿 IsSafeURL 直接判第 1 类：相对片段（无前缀、不以 / 开头）在它眼里不是合法 URL，
// 而这里恰恰要允许它 —— 校验走「拼好之后整串」或者「补一个 / 前缀后的片段」。
func CardHref(prefix, value string) string {
	v := strings.TrimSpace(value)
	if v == "" {
		return ""
	}
	if !strings.HasPrefix(v, "/") && !strings.HasPrefix(v, "#") && !hasScheme(v) {
		p := strings.TrimSpace(prefix)
		if p == "" {
			// 作者没给前缀：片段本身就是相对地址，用同一套字符集白名单校验它。
			if !core.IsSafeURL("/" + v) {
				return ""
			}
			return v
		}
		out := strings.TrimSuffix(p, "/") + "/" + strings.TrimPrefix(v, "/")
		if !core.IsSafeURL(out) {
			return ""
		}
		return out
	}
	if !core.IsSafeURL(v) {
		return ""
	}
	return v
}

// LocalizePrefix 作者填的链接前缀 → 当前语言的访问路径（审计 I18N-015，可空解析器）。
//
// 只本地化**前缀**，不碰字段值：前缀是作者在组件上填的站内逻辑路径（/products/），
// 而字段值是 CMS 数据，它可能是纯 slug（"shirt"），也可能是完整路径（"/products/shirt"）
// 或外链 —— 后两种由 CardHref 原样返回，再前缀一次就会指到不存在的地址
// （与 button 的 ActionLink「CMS 绑定值不本地化」同一条口径）。
func LocalizePrefix(prefix string, siteLink func(string) string) string {
	if strings.TrimSpace(prefix) == "" {
		return prefix
	}
	return core.SiteLinkOrSame(siteLink, prefix)
}

// hasScheme 判断字符串是否带协议前缀（"javascript:alert(1)" / "https://…" / "mailto:…"）。
//
// 判据用 RFC 3986 的 scheme 形状（字母开头，后跟字母 / 数字 / + - .），
// 比 strings.Contains(s, ":") 准：含冒号但不像 scheme 的值（如 "9:30 开卖"）不该被误判。
func hasScheme(s string) bool {
	idx := strings.Index(s, ":")
	if idx <= 0 {
		return false
	}
	for i := 0; i < idx; i++ {
		c := s[i]
		ok := (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') ||
			(c >= '0' && c <= '9') || c == '+' || c == '-' || c == '.'
		if !ok {
			return false
		}
	}
	return true
}
