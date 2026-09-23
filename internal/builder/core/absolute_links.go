package core

// absolute_links.go — 富文本字段里的站内地址绝对化（构建期）。
//
// 为什么只有这一处需要它：组件输出的链接在**自己的数据入口**就已是绝对地址 ——
//   · 作者手填的站内链接 → ctx.ResolveSiteLink（注入的实现补站点基址）
//   · 导航菜单 → LocalizeMenuURLWith
//   · 站点槽位地址 → 集合/槽位注入时补
//   · 商品 / 文章细节链接 → 集合项 url 注入时补
// 唯独**正文 HTML 是内容作者写的**（WordPress 导入的正文里就带着 /xxx/ 这类
// 站内链接），构建管线管不到它的产生，只能在它被清洗后、输出前做一次归一。
//
// 与「在产物出口重写整份 HTML」的区别（那条路已否决）：
//   · 作用域是**一个已清洗的富文本字段值**，不是整份文档 ——
//     碰不到组件输出、属性值、JSON-LD、<script> 里的字符串；
//   · 输入已过 SanitizeRichHTML 白名单，只剩受控标签与 href/src 属性。
//
// 未配置站点基址（WP_SITE_BASE_URL 空）时原样返回：宁可留相对路径，
// 也不能猜一个域名。

import (
	"regexp"
	"strings"

	"go_wp/internal/seo"
)

// richRootRelativeAttrRe 富文本里需要绝对化的地址属性。
//
// 只认双引号包裹的**站内根相对**值（单个 "/" 开头）：
// 协议相对（//host）与已绝对地址不会被这个模式匹配到。
var richRootRelativeAttrRe = regexp.MustCompile(`(?i)\b(href|src)="(/[^"]*)"`)

// AbsolutizeInternalLinks 把 HTML 片段里的站内根相对地址补成绝对 URL。
func AbsolutizeInternalLinks(html string) string {
	if html == "" || seo.SiteBaseURL() == "" || !strings.Contains(html, "=\"/") {
		return html
	}
	return richRootRelativeAttrRe.ReplaceAllStringFunc(html, func(m string) string {
		sub := richRootRelativeAttrRe.FindStringSubmatch(m)
		if len(sub) != 3 {
			return m
		}
		if strings.HasPrefix(sub[2], "//") {
			return m
		}
		return sub[1] + "=\"" + seo.AbsoluteSiteURL(sub[2]) + "\""
	})
}
