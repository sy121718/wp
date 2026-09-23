package projecthttp

// project_err.go — 项目域（主题 / 站点设置）**页面** handler 的错误归口。
//
// 背景：本模块的页面 handler 有 20 处 `c.String(4xx, "…")` / `c.String(500, 归口 key)` 直写响应。
// 浏览器里没有页面，只有一块纯文本：侧边栏、页头、表单全部消失，用户既改不了也退不回
// （AGENTS.md「不许直出内部错误」的形态 ①）。归口 key 那一半更隐蔽 —— `c.String(500, "MsgInternalError")`
// 不经过任何翻译，页面上就是 `MsgInternalError` 这串英文，运营只会以为后台坏了。
//
// 形状与 internal/module/admin/inbound/http/admin_err.go 对齐，只有候选来源不同：
// admin 是手写白名单，本域**直接用 service 哨兵**——因为 project service 的哨兵消息
// 本来就是 enums 的 key（`errors.New(projectenums.ErrThemeNotFound)`），
// 于是「判定业务错误」这件事只需要一份表，JSON 出口与页面出口共用。
//
// 三件套在这里落地为：
//   · 白名单 —— service 哨兵（projectErrStatusText 的判定表）+ projectPageErrKeys；
//   · 归口文案 —— ErrThemeInternal（主题域）/ ErrProjectInternal（站点设置域）；
//   · 结构化日志 —— 未命中判定表时记原文（带 path），响应只出归口文案。

import (
	"errors"
	"net/http"
	"net/url"
	"strings"

	projectenums "go_wp/internal/module/project/enums"
	service "go_wp/internal/module/project/service"
	"go_wp/internal/web/shell"
	"go_wp/pkg/logger"
	r "go_wp/pkg/response"

	"github.com/gin-gonic/gin"
)

// projectErrStatusText 把 service 返回的错误判定成 (HTTP 状态码, i18n key)。
//
// fallbackKey 是本域的**归口文案**，由调用方按域给：主题域 ErrThemeInternal、
// 站点设置域 ErrProjectInternal。归口文案回答的是「哪一处的服务出了问题」——
// 把主题的故障说成「工程服务内部错误」，排障时日志场景与页面提示就对不上了。
//
// 判定用 **service 哨兵**（errors.Is）而不是字符串形态：pkg/response 的 businessErrKey
// 那类形态判定在这里不够用 —— 它只认「像 key 的字符串」，而本域要的是
// 「这句话确实出自本域 enums」。哨兵是编译期的，抄错名字会直接报错。
//
// 返回的 key 一定来自 projectenums（或是调用方给的 fallbackKey），
// 所以调用方可以直接把它交给 r.TranslateMessage —— 页面渲染的是译文，不是裸 key。
//
// 比原先 themeError 的判定集**多认了** ErrThemeProjectRequired 与站点工程域的四个哨兵：
// 它们本来就是业务错误（"需要显式工程作用域" / "站点工程名称不能为空" 等），
// 原先落 default → 500 + 「主题服务内部错误」，等于把用户能自己修的问题说成了系统故障。
func projectErrStatusText(err error, fallbackKey string) (status int, key string) {
	switch {
	case err == nil:
		return http.StatusInternalServerError, fallbackKey

	// —— 主题域 ——
	case errors.Is(err, service.ErrThemeNotFound):
		return http.StatusNotFound, projectenums.ErrThemeNotFound
	case errors.Is(err, service.ErrThemeIsActive):
		return http.StatusBadRequest, projectenums.ErrThemeIsActive
	case errors.Is(err, service.ErrThemeNameRequired):
		return http.StatusBadRequest, projectenums.ErrThemeNameRequired
	case errors.Is(err, service.ErrThemeDuplicateName):
		return http.StatusBadRequest, projectenums.ErrThemeDuplicateName
	case errors.Is(err, service.ErrThemeProjectIDEmpty):
		return http.StatusBadRequest, projectenums.ErrThemeProjectIDEmpty
	case errors.Is(err, service.ErrInvalidThemeSettings):
		return http.StatusBadRequest, projectenums.ErrInvalidThemeSettings
	case errors.Is(err, service.ErrThemeProjectRequired):
		return http.StatusBadRequest, projectenums.ErrProjectRequired

	// —— 站点工程域 ——
	case errors.Is(err, service.ErrProjectNotFound):
		return http.StatusNotFound, projectenums.ErrProjectNotFound
	case errors.Is(err, service.ErrInvalidName):
		return http.StatusBadRequest, projectenums.ErrInvalidName
	case errors.Is(err, service.ErrInvalidSettings):
		return http.StatusBadRequest, projectenums.ErrInvalidSettings
	case errors.Is(err, service.ErrInvalidParam):
		return http.StatusBadRequest, projectenums.ErrInvalidParam

	default:
		// 基础设施故障（连接池 / 约束冲突 / 驱动原文）：key 落归口文案，原文只进日志。
		return http.StatusInternalServerError, fallbackKey
	}
}

// projectErrParam 页面路径的错误文案归口：返回**当前语言的译文**，供 ?err= / 模板错误槽渲染。
//
// 为什么必须翻译：页面上的提示是直接渲染的文本（模板 {{.Err}} 不经过 response 的 translate）。
// 不翻的话运营看到的是裸 key（ErrThemeInternal）而不是「主题服务内部错误」。
//
// 为什么不在这里写响应：页面路径靠 303 回列表 / 回表单表达失败，没有地方放状态码；
// 需要如实区分 400 / 500 的是 JSON 出口（themeError），那里才有机器可读的消费方。
//
// 判据与 themeError 的 default 分支同源：**只有落到 500 才是「预期外的故障」**，
// 业务错误（缺参 / 不存在 / 被引用拒绝）是预期内的用户输入问题，不污染错误日志。
func projectErrParam(c *gin.Context, scene, fallbackKey string, err error) string {
	status, key := projectErrStatusText(err, fallbackKey)
	if err != nil && status == http.StatusInternalServerError {
		logger.Scene(scene).With("path", c.Request.URL.Path).Error(err, "项目域页面操作失败")
	}
	return r.TranslateMessage(c, key)
}

// projectPageErrKeys ?err= 通道上**可能出现**的 i18n key（读侧候选的来源）。
//
// 写侧每新增一条会进 ?err= 的文案，必须在这里补一行 —— 漏登记的症状是
// 「写侧发了提示、页面上什么都不显示」（?err= 整体匹配不上候选，被判成伪造而落空串），
// 有 project_err_texts_test.go 的写侧对账用例钉住。
//
// 列的是**文案 key** 而不是「哪一处调用」：同一个 key 可能被多处写侧用到
// （ErrThemeNotFound 在「GET 缺主题」与「POST 主题不存在」两条路上各出现一次）。
var projectPageErrKeys = []string{
	// service 哨兵判定表的全部产物（projectErrStatusText 的返回值集合）
	projectenums.ErrThemeNotFound,
	projectenums.ErrThemeIsActive,
	projectenums.ErrThemeNameRequired,
	projectenums.ErrThemeDuplicateName,
	projectenums.ErrThemeProjectIDEmpty,
	projectenums.ErrInvalidThemeSettings,
	projectenums.ErrProjectRequired,
	projectenums.ErrProjectNotFound,
	projectenums.ErrInvalidName,
	projectenums.ErrInvalidSettings,
	projectenums.ErrInvalidParam,
	// 两个域的归口文案（default 分支落到哪一个，取决于调用方给的 fallbackKey）
	projectenums.ErrThemeInternal,
	projectenums.ErrProjectInternal,
	// handler 自己的校验文案（不经 service、没有哨兵）
	projectenums.ErrThemeIDRequired,
	projectenums.MsgThemeSettingsInvalid,
	projectenums.MsgThemeSettingsRefreshFailed,
	projectenums.ErrSiteSettingsNameRequired,
	projectenums.ErrGA4IDInvalid,
	projectenums.ErrGSCVerificationInvalid,
	projectenums.ErrNotFoundHTMLTooLong,
	projectenums.ErrLangURLModeInvalid,
}

// projectErrTexts ?err= 可以原样渲染的受控文案（**当前语言**的译文）。
//
// 与写侧同源：写侧用 r.TranslateMessage(c, key) 产出提示，读侧用同一个函数、
// 同一份 key 列表取得候选 —— 两处若各自硬编码中文，词条一改措辞候选就静默失配，
// 表现为「写侧提示可见、页面上却什么都没有」（不报错、不记日志）。
func projectErrTexts(c *gin.Context) []string {
	out := make([]string, 0, len(projectPageErrKeys))
	for _, key := range projectPageErrKeys {
		out = append(out, r.TranslateMessage(c, key))
	}
	return out
}

// projectPageErrText ?err= 的受控出口：整体命中受控文案返回原文，未命中落空串。
//
// 为什么不在这里落归口文案：那会给手拼的 `?err=任意文本` 凭空造出一条顶着「系统提示」
// 样式的伪造消息（Jet 已做 HTML 转义，所以不是 XSS —— 问题是「看起来像系统说的话」）。
// 与 admin_err.go 的 adminPageErrText、shell 的 ?done= / ?saved= 同一水准。
//
// 匹配用 shell.FacingNotice 的**整体**判定（逐字 / 数字归一 / 「候选 + ：」），
// 不是 strings.Contains —— 后者只要夹带一段已知文案就能塞任意前缀后缀。
// 超长参数（> shell.NoticeMaxBytes）一并判不命中，因此不需要另外截断。
func projectPageErrText(c *gin.Context, raw string) string {
	return shell.FacingQueryText(raw, "", func(msg string) string {
		return shell.FacingNotice(msg, projectErrTexts(c))
	})
}

// projectErrRedirect 页面写操作的统一失败出口：303 回来源页，并回带 ?err=<当前语言文案>。
//
// 为什么不是 c.String(400, …)：浏览器里只剩一块纯文本 —— 侧边栏、页头、列表全没了，
// 用户既看不到自己填了什么，除了「后退」也没有动作可做。失败的写操作**必须**把人送回原来的
// 位置改一处再试，这正是 303 + 提示条要解决的问题（同类收口见 admin 域的 35 处写失败出口）。
//
// 为什么不用 shell.AdminWriteFailedText（400 + 文案）：那是给 **HTMX 片段**请求准备的
// （响应体会被 htmx 就地替换进页面）。本域这些写操作是原生 `<form method="post">`，
// 400 会把用户直接导航到一块 JSON 上，表单内容同时丢失 —— 比纯文本更糟。
//
// text 为空时回落归口文案：页面提示不能是空白（用户会以为操作成功了）。
// 注意回跳 URL 可能自带 query（如 /admin/themes?project=xxx），所以要自己挑分隔符，
// 不能直接用 shell.FilterBaseURL（它固定拼 "?"）。
func projectErrRedirect(c *gin.Context, backURL, text string) {
	if strings.TrimSpace(text) == "" {
		text = shell.PageInternalText(c)
	}
	backURL = strings.TrimSpace(backURL)
	if backURL == "" {
		backURL = requestPathOrAdminRoot(c)
	}
	c.Redirect(http.StatusSeeOther, buildErrRedirectURL(backURL, text))
}

// buildErrRedirectURL 拼「回跳 URL + ?err=<转义后的文案>」（纯函数，便于单测）。
//
// 两件事都必须对，而错了的症状都不明显：
//   - **转义**：文案里的 `&` / `#` 不转义会把后面的参数截断，表现是「提示静默消失」；
//   - **分隔符**：回跳 URL 自带 query（/admin/themes?project=xxx）时再用 "?" 会拼出
//     `/admin/themes?project=xxx?err=…` —— err 成了 project 值的一部分，提示永远不显示。
func buildErrRedirectURL(backURL, text string) string {
	sep := "?"
	if strings.Contains(backURL, "?") {
		sep = "&"
	}
	return backURL + sep + "err=" + url.QueryEscape(text)
}

// requestPathOrAdminRoot 取当前请求路径作为回跳目标（回跳 URL 缺失时的兜底）。
//
// 只取 Path（忽略 query 与 fragment）并强制以 /admin 开头：回跳目标来自服务端拼装，
// 但这个兜底分支是「调用方没给」时才走 —— 万一将来有人把请求里的值透传到这里，
// 它也不会变成开放重定向（同 shell 的 sameOriginPath 判据）。
func requestPathOrAdminRoot(c *gin.Context) string {
	p := c.Request.URL.Path
	if strings.HasPrefix(p, "/admin") {
		return p
	}
	return "/admin"
}
