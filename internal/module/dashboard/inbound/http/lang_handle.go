package dashboardhttp

// 后台语言切换（多语言 P1）：GET /admin/lang?lang=en-US&redirect=/admin/pages
//
// 语义：校验 lang（复用 pkg/response 的语言白名单）→ 写语言协商 Cookie → 302 回跳。
// 兜底原则（用户最高优先级要求）：任何非法输入都不报错、不 4xx、不 panic ——
// 非法 lang 忽略并回退默认语言，非法 redirect 回首页。

import (
	"net/http"
	"strings"

	"go_wp/pkg/response"

	"github.com/gin-gonic/gin"
)

// LangSwitch 处理 GET /admin/lang：写语言 Cookie 后 302 回跳。
//
// query 参数：
//   - lang：目标语言（zh / zh-CN / en / en-US 等变体，经 response.NormalizeLang 规范化）；
//     缺失或非法时忽略该值并回退配置默认语言（不报错）。
//   - redirect：回跳地址；仅接受站内路径（以 / 开头且不含 //、反斜杠、控制字符），
//     否则回首页，防开放重定向。
//
// 挂载位置：admin 页面路由组（SessionAuth + CSRF，无 Casbin）。GET 属安全方法，
// CSRF 直接放行；Cookie 为 SameSite=Lax + Path=/，同站 HTMX 请求会携带。
func LangSwitch(c *gin.Context) {
	lang, _ := response.NormalizeLang(c.Query("lang"))
	response.SetLangCookie(c, lang)

	c.Redirect(http.StatusFound, safeLangRedirect(c.Query("redirect")))
}

// safeLangRedirect 校验回跳地址：只允许站内绝对路径，其余一律回首页 "/"。
//
// 拒绝的形态（浏览器会把它们当协议相对 URL 跳外站）：
//   - 不以 "/" 开头（相对路径、绝对 URL、空值）
//   - 含 "//"（如 "//evil.com"、"http://evil.com"）
//   - 含反斜杠或控制字符（如 "/\evil.com"、"/a\r\nLocation: ..."）
func safeLangRedirect(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" || !strings.HasPrefix(raw, "/") {
		return "/"
	}
	if strings.Contains(raw, "//") {
		return "/"
	}
	if strings.ContainsAny(raw, "\\\r\n\t") {
		return "/"
	}
	return raw
}
