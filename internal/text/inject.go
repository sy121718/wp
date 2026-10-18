package text

// inject.go — 模板数据的取词键注入（lang / t / tf / langs / lang_redirect）。
//
// 为什么由 Go 侧注入而不是给模板注册全局函数：语言是**请求级**的，Set.AddGlobal 的全局
// 函数拿不到当前请求的语言，按请求切语言就得改共享状态（并发下 -race 报 DATA RACE）。
//
// 漏注入的后果是**静默**的：模板里 tr := .["t"] 拿到 nil，该模板所有取词一起变空串 ——
// 页面结构完整、状态码 200、日志干净（见 internal/templates/CLAUDE.md 的实测记录）。
// 所以整页（adminui.Prepare）与片段（本函数）都必须走这里，不能各写一份。

import (
	"fmt"

	"github.com/gin-gonic/gin"

	"go_wp/internal/templates"
	"go_wp/pkg/i18n"
	"go_wp/pkg/response"
	"go_wp/pkg/safepath"
)

// AdminHomePath 控制面首页：语言回跳与拒绝页的兜底落点。
const AdminHomePath = "/admin"

// Inject 为模板数据补上取词键，返回同一个 map（便于链式写）。
//
// 注入：lang / t（func(key, fallback)）/ tf（func(key, fallback, args []string)）/
// langs（语言切换候选）/ lang_redirect（语言切换表单的 redirect 隐藏域）；
// data["title"] 是 i18n key 时顺带翻译。
//
// data 为 nil 时新建一个 —— 调用方常常是「先建 map 再逐键填」的形态。
func Inject(c *gin.Context, data map[string]any) map[string]any {
	if data == nil {
		data = map[string]any{}
	}

	lang := Lang(c)
	t := templates.TranslateFunc(lang)

	data["lang"] = lang
	data["t"] = t
	// tf 是「取词 + %s 占位符填充」：签名 func(key, fallback string, args []string) string。
	// 需要它是因为结论文案带计数（「已删除 %s 个页面。」），而 Go 侧不再拼整句。
	data["tf"] = func(key, fallback string, args []string) string {
		return response.ArgsText(c, key, fallback, args)
	}
	// tn 是「取词 + {name} 具名占位符填充」：签名 func(key, fallback string, args map[string]any) string。
	// 英文与中文的语序不同（「{n} 个」/「{n} tags」），具名占位符让词条自己决定位置。
	data["tn"] = func(key, fallback string, args map[string]any) string {
		params := make(map[string]string, len(args))
		for k, v := range args {
			params[k] = fmt.Sprint(v)
		}
		return i18n.FillTranslate(t, key, fallback, params)
	}
	data["langs"] = templates.LanguageOptions(lang)
	// req_path：当前请求的路径 + 查询串（不含 scheme/host）。分页条据此拼翻页链接 ——
	// 链接基址是**请求事实**，不是页面装配的产物，所以由注入层给，控制器不再算。
	data["req_path"] = RequestURI(c)
	data["lang_redirect"] = LangRedirect(c)

	// 页面标题：enums 常量已是 key（如 MsgPagesTitle），此处按当前语言翻译；
	// 非 key 的字面量标题（如登录页）走 fallback 原样返回，不改变行为。
	if title, ok := data["title"].(string); ok && title != "" {
		data["title"] = t(title, title)
	}
	return data
}

// LangRedirect 语言切换表单 redirect 隐藏域的取值。
//
// 判据只有一份，在 pkg/safepath.LangRedirectPath：站内相对路径且不超过 MaxReturnPathBytes
// 才通过。本函数只补渲染侧的两级兜底 —— 丢弃整个 URI（含超长 query）回落到当前请求的
// **路径**；路径本身也异常时回控制面首页。
func LangRedirect(c *gin.Context) string {
	if c == nil {
		return AdminHomePath
	}
	if p := safepath.LangRedirectPath(RequestURI(c)); p != "" {
		return p
	}
	return requestPathOrAdminHome(c)
}

// requestPathOrAdminHome 回落到当前请求的路径（丢掉 query）：回跳仍回到发起页；
// 路径本身异常（空 / 非同源 / 超长）时回控制面首页。
func requestPathOrAdminHome(c *gin.Context) string {
	if c != nil && c.Request != nil && c.Request.URL != nil {
		if p := c.Request.URL.Path; safepath.IsSameOriginPath(p) && len(p) <= safepath.MaxReturnPathBytes {
			return p
		}
	}
	return AdminHomePath
}

// RequestURI 当前请求的站内 URI（含 query）；异常时回控制面首页。
//
// 日志与语言回跳都要它：日志要真实 URI，语言回跳要经 LangRedirect 收敛后的值 ——
// 两者不能共用一份（拿日志值当回跳目标就是开放重定向）。
func RequestURI(c *gin.Context) string {
	if c == nil || c.Request == nil || c.Request.URL == nil {
		return AdminHomePath
	}
	uri := c.Request.URL.RequestURI()
	if uri == "" {
		return AdminHomePath
	}
	return uri
}
