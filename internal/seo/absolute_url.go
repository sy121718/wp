package seo

// absolute_url.go — 站内地址绝对化的**唯一原语**。
//
// 约定：产物里所有站内地址（链接与资源）都写成绝对 URL（http:// 或 https://），
// 不留站内根相对路径（以单个 "/" 开头）。
//
// 为什么放在 seo 包：它是**唯一能同时被构建器（internal/builder）、发布管线
// （internal/pipeline）与业务模块（internal/module/*）引用**的层 ——
// 放 builder 会被业务模块反向依赖（架构上禁止），放 pipeline 同理。
// 地址规范化本来就属于 SEO 的关注点（canonical / sitemap / hreflang 都在这个包），
// 站内链接与它们是同一条规则的不同出口。
//
// 判据收敛在这里，调用点分散在**数据入口**（集合项 url、站点槽位地址、
// 菜单本地化、组件手填链接）—— 不能反过来靠出口重写：
//   · 出口重写改的是 HTML 文本而不是「链接」，CMS 富文本正文里的 href 也会被改，
//     而那块内容不归构建器管；
//   · 绝对与否在组件侧看不见，组件作者无法判断自己产出的地址对不对。

import (
	"os"
	"strings"
)

// SiteBaseURLEnv 站点对外基址的环境变量名。
//
// 与 sitemap / robots / hreflang / canonical 同一个真源；允许带路径前缀
// （站点部署在子路径时形如 https://example.com/shop）。
const SiteBaseURLEnv = "WP_SITE_BASE_URL"

// SiteBaseURL 站点对外基址（未配置时返回空串）。
func SiteBaseURL() string {
	return strings.TrimRight(strings.TrimSpace(os.Getenv(SiteBaseURLEnv)), "/")
}

// AbsoluteSiteURL 站内路径 → 对外**绝对地址**（站点基址 + 规范化路径）。
//
// 为什么站内链接一律给绝对：产物是站点自己的对外表达 —— 站点与构建期 CMS
// 不同域、被镜像/被抓取到别处渲染、或进邮件模板时，根相对路径会指回**承载页面**
// 的域而不是站点域。相对地址在「站点独占域名根」的部署下看不出区别，
// 所以这类不一致只会在换环境时炸。
//
// 原样返回的情形（都不是「站内根相对路径」）：
//
//	· 空值；
//	· 已绝对（http:// / https://）与协议相对（//host/x）—— 后者是合法外部地址，
//	  补基址会把它改成错的地址；
//	· 不以 "/" 开头的一切（"#anchor"、"?query"、"./x"、"x"、"mailto:…"）；
//	· 未配置站点基址 —— 宁可留相对路径，也不能猜一个域名：猜错会把全部站内链接
//	  指向别人的站点，比相对路径危险得多。
func AbsoluteSiteURL(pathOrURL string) string {
	v := strings.TrimSpace(pathOrURL)
	if v == "" {
		return pathOrURL
	}
	if !strings.HasPrefix(v, "/") || strings.HasPrefix(v, "//") {
		return pathOrURL
	}
	base := SiteBaseURL()
	if base == "" {
		return pathOrURL
	}
	return JoinURL(base, v)
}
