package dashboardhttp

// 后台页面多语言数据注入（多语言 P1 第二步）。
//
// 取翻译路径：handler 按请求注入 t 函数（数据 map 里的函数值），模板用
// {{ .["t"]("shell.brand", "管理后台") }} 取译文。实测依据与放弃另两条路径的原因见
// internal/templates/i18n_jet_test.go；语言协商（Cookie > query > Accept-Language > 默认）
// 复用第一步的 pkg/response，不在此重复实现。

import (
	"go_wp/internal/templates"
	"go_wp/pkg/response"

	"github.com/gin-gonic/gin"
)

// withI18n 向模板数据注入多语言键，所有后台页面渲染入口统一调用（withCSRF 已内置）：
//
//   - lang：本次请求语言（规范化后的 zh-CN / en-US）；
//   - t：翻译函数，签名 func(key, fallback string) string，模板用 {{ .["t"]("key","中文原文") }}；
//   - langs：语言下拉选项（zh-CN / en-US，当前语言标记 Active）；
//   - lang_redirect：当前页 URI，语言切换回跳用（仅站内路径，服务端 safeLangRedirect 再校验）；
//   - title：若标题是 sys_i18n key（如 MsgPagesTitle）则翻译为当前语言，否则原样保留。
//
// 兜底：i18n 缓存未初始化 / key 缺失时 t 返回模板内原文，title 保持原值 —— 不报错、不 panic。
func withI18n(c *gin.Context, data gin.H) gin.H {
	if data == nil {
		data = gin.H{}
	}

	lang := response.RequestLanguage(c)
	t := templates.TranslateFunc(lang)

	data["lang"] = lang
	data["t"] = t
	data["langs"] = templates.LanguageOptions(lang)
	data["lang_redirect"] = requestURI(c)

	// 页面标题：enums 常量已是 key（如 MsgPagesTitle），此处按当前语言翻译；
	// 非 key 的字面量标题（如登录页）走 fallback 原样返回，不改变行为。
	if title, ok := data["title"].(string); ok && title != "" {
		data["title"] = t(title, title)
	}
	return data
}

// translateFor 返回 Go 侧文案翻译函数（分页条等由 Go 拼接、不经模板的文案）。
// c 为 nil 时按默认语言返回，绝不 panic。
func translateFor(c *gin.Context) func(key, fallback string) string {
	return templates.TranslateFunc(response.RequestLanguage(c))
}

// requestURI 返回当前请求的站内 URI（含 query）；异常时回首页 "/"。
func requestURI(c *gin.Context) string {
	if c == nil || c.Request == nil || c.Request.URL == nil {
		return "/"
	}
	uri := c.Request.URL.RequestURI()
	if uri == "" {
		return "/"
	}
	return uri
}
