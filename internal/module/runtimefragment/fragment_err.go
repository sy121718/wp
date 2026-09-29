package runtimefragment

// fragment_err.go — 访问面片段端点的错误出口归口。
//
// 背景：endpoint.go 曾有两处把 Go 的 error 原文直接写进响应：
//
//	c.String(http.StatusBadRequest, perr.Error())  // collectFragmentParams 的白名单拒绝
//	c.String(http.StatusBadRequest, err.Error())   // validateContext 的白名单拒绝
//
// 本项目把「响应写入里出现 .Error()」列为必须堵的形态①（AGENTS.md「响应与错误处理」）。
// 实测这两处的**值域是封闭的**（见下面的哨兵），当前不会带表名 / SQL / 路径 —— 但形状仍是
// 缺陷源，理由有三条，都不是「今天没漏所以不用管」：
//
//  1. validateContext 原来的错误串是 `fmt.Errorf("非法的片段上下文: %q", ctx)`：
//     **请求方可控的串被原样回显进响应**。/_fragments/loginPanel?context=<任意串> 的输出
//     由请求方决定。%q 会让它转义、不成 XSS，但「响应内容由请求方裁」这件事本身就是
//     响应不是可信边界的反面教材；日志里需要这个值，响应里不需要。
//  2. 出口没有日志：参数被拒是 400 静默返回，运维在日志里看不到任何痕迹，
//     线上出现「某能力恒 400」时只能靠复现。
//  3. 形状收口的价值在将来：这个位置最自然的演化是「换一个更严格的参数校验」，
//     那一刻 err 就可能带上 schema / 表名 / 驱动原文 —— 判据按**形状**立，才不会等到那天。
//
// 三件套在这里落地为：
//   · 白名单 —— 判定表（哨兵 error → 受控文案），值域由本包的哨兵定义，errors.Is 判定；
//   · 归口文案 —— site.fragment.err.* 词条（迁移 409），缺词条回退包内中文原文；
//   · 结构化日志 —— 原文只进日志（参数类 Warn、未知来源 Error），带能力类型与上下文。
//
// 判定用**哨兵**而不是 err.Error() 的字符串形态：哨兵是编译期的，改名会直接编译失败；
// 字符串比对在措辞变化时静默失配 —— 表现为「用户从「参数过多」变成一句通用提示」，
// 既不报错也不记日志（同 internal/module/project/inbound/http/project_err.go 的取舍）。
//
// 为什么不是 internal/module/*/enums：本包被 AGENTS.md 明确列为「无 contract、直挂访问面
// 路由」，面向访客的文案走 site.fragment.* 词条（fragment_i18n.go / fragment_i18n_*.go 的
// 既有惯例）。访问面必须多语言 —— 293 修的正是「loginPanel 两句写死在 Go 里，
// 英文站点一直显示中文」，这里不能重蹈。
//
// 补记（取词 key 收进常量之后）：本包**取词调用点的 key** 现已收进 enums/enums.go
// （包 runtimefragmentenums），但**错误出口这 9 个 key 常量刻意留在本文件**，不是漏迁 ——
// site.fragment.* 是四段 key，以 `Err` 开头命名会让 pkg/response 的 enums 对账测试变红
// （详见 enums/enums.go 头部「与错误消息 enums 的区别」）。两类 key 因此分居两处，各有唯一真源。

import (
	"errors"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"go_wp/pkg/i18n"
	"go_wp/pkg/logger"
)

// fragmentErrScene 日志场景名（与 registry.go 的 logger.Scene("fragment") 一致）。
const fragmentErrScene = "fragment"

// —— 值域：本包全部「请求被白名单拒绝」的哨兵 ——
//
// 四个哨兵各自对应 endpoint.go 的一个拒绝点，值域**封闭**：除了这几个哨兵与下游渲染错误，
// 没有别的 error 会走到 fragmentFail。哨兵的文本只进日志，不参与任何判定与响应。
var (
	// errFragmentFormParse POST 表单解析失败（客户端请求体格式问题）。
	errFragmentFormParse = errors.New("片段请求表单无法解析")
	// errFragmentParamCount 参数名数量超过 GET / POST 的上限。
	errFragmentParamCount = errors.New("片段请求参数过多")
	// errFragmentParamIllegal 参数名超过 64 字节，或参数值超过 maxParamLen。
	errFragmentParamIllegal = errors.New("片段请求参数不合法")
	// errFragmentContext 语义上下文不在白名单枚举内（registry.go 的 validateContext）。
	//
	// 哨兵刻意**不带非法值**：那个值只进日志（见 endpoint.go 的调用点把 req.Context
	// 作为日志字段传进来）。判定只需要「是这一类」这个事实，回显是原实现的缺陷。
	errFragmentContext = errors.New("片段语义上下文不合法")
)

// —— 受控文案：哨兵 → (i18n key, 包内中文原文) ——
//
// 中文原文同时是 fallback：词条缺失（未跑迁移 / i18n 未初始化）时原样返回，
// 绝不输出裸 key、绝不输出空串（i18n 的兜底链保证，见 pkg/i18n/snapshot.go）。
const (
	fragmentErrKeyFormParse    = "site.fragment.err.form_parse"
	fragmentErrKeyTooManyParam = "site.fragment.err.too_many_params"
	fragmentErrKeyParamInvalid = "site.fragment.err.param_invalid"
	fragmentErrKeyContext      = "site.fragment.err.context"
	fragmentErrKeyInternal     = "site.fragment.err.internal"

	// —— endpoint.go 里「固定受控短句」形态的 4 条出口（本批收口）——
	//
	// 它们与上面的判定表**不同源**：不是「哨兵 → 文案」，而是直接在端点里按状态码分支写的短句。
	// 收口前它们是 Go 侧硬编码中文，英文站点恒显示中文（与 293 修过的 loginPanel 同一类缺陷）。
	fragmentErrKeyCapabilityMissing = "site.fragment.err.capability_missing"
	fragmentErrKeyMethodNotAllowed  = "site.fragment.err.method_not_allowed"
	fragmentErrKeyLoginRequired     = "site.fragment.err.login_required"
	fragmentErrKeyRenderFailed      = "site.fragment.err.render_failed"
)

// fragmentErrText 一条受控文案（i18n key + 中文原文）。
type fragmentErrText struct{ key, fallback string }

// 四条「请求被拒」的文案 + 一条归口文案。
//
// 为什么四条分开而不是合并成一句「请求不合法」：**消费方据此要做的事不同**——
// 参数过多要减少参数名、参数非法要缩短某个名或值、表单解析失败要修请求体、
// 上下文非法要改用枚举内的值。同样的判据也支撑 project_err.go 用 11 个 key 映射 service 哨兵。
//
// 为什么这四条不是 i18n 之外的「第二份真相」：文案只有一处定义（本表），
// 迁移 409 登记同一批 key，读侧与写侧同源。
var (
	fragmentErrTextFormParse = fragmentErrText{fragmentErrKeyFormParse, "片段请求表单无法解析"}
	fragmentErrTextTooMany   = fragmentErrText{fragmentErrKeyTooManyParam, "片段请求参数过多"}
	fragmentErrTextParam     = fragmentErrText{fragmentErrKeyParamInvalid, "片段请求参数不合法"}
	fragmentErrTextContext   = fragmentErrText{fragmentErrKeyContext, "片段语义上下文不合法"}

	// —— endpoint.go 的 4 条固定短句（中文原文逐字沿用收口前的写法）——
	//
	// 同一张表的好处与上面四条一致：文案只有一处定义，读侧（Go 兜底）与写侧（迁移登记）
	// 指向同一批 key。**「需要登录」是一条 key 而不是三条** —— 三处 401 出口说的是同一件事，
	// 拆分会让将来改一处措辞必须记得改三处。
	fragmentErrTextCapabilityMissing = fragmentErrText{fragmentErrKeyCapabilityMissing, "片段能力不存在"}
	fragmentErrTextMethodNotAllowed  = fragmentErrText{fragmentErrKeyMethodNotAllowed, "片段能力不支持该请求方法"}
	fragmentErrTextLoginRequired     = fragmentErrText{fragmentErrKeyLoginRequired, "需要登录"}
	fragmentErrTextRenderFailed      = fragmentErrText{fragmentErrKeyRenderFailed, "片段渲染失败"}

	// fragmentErrTextInternal 归口文案：判定表**未命中**时用。
	//
	// 当前值域封闭，正常走不到它；留着是因为 fragmentFail 是通用出口 ——
	// 将来有人把渲染 / 数据源错误接到这里，也会落到归口文案而不是把原文铺在响应上。
	fragmentErrTextInternal = fragmentErrText{fragmentErrKeyInternal, "片段请求处理失败"}
)

// fragmentErrResponse 把错误判定成 (HTTP 状态码, 受控文案, 是否命中白名单)。
//
// 纯函数（取词函数作为参数传入），便于单测直接钉住四条映射与归口分支。
//
// 状态码：命中判定表 → 400（请求不合法）；未命中 → 500（内部故障）。
// **内部故障不能谎报成 400** —— 客户端与监控都会以为「是我发错了」（同 admin_err.go 的判据）。
func fragmentErrResponse(err error, t func(key, fallback string) string) (status int, text string, known bool) {
	switch {
	case errors.Is(err, errFragmentFormParse):
		return http.StatusBadRequest, fragmentErrTextOf(t, fragmentErrTextFormParse), true
	case errors.Is(err, errFragmentParamCount):
		return http.StatusBadRequest, fragmentErrTextOf(t, fragmentErrTextTooMany), true
	case errors.Is(err, errFragmentParamIllegal):
		return http.StatusBadRequest, fragmentErrTextOf(t, fragmentErrTextParam), true
	case errors.Is(err, errFragmentContext):
		return http.StatusBadRequest, fragmentErrTextOf(t, fragmentErrTextContext), true
	default:
		return http.StatusInternalServerError, fragmentErrTextOf(t, fragmentErrTextInternal), false
	}
}

// fragmentErrTextOf 按请求语言取一条受控文案（t 为空时直接用 fallback）。
func fragmentErrTextOf(t func(key, fallback string) string, e fragmentErrText) string {
	if t == nil {
		return e.fallback
	}
	return t(e.key, e.fallback)
}

// fragmentFail 片段端点的统一失败出口：受控文案进响应，原文只进日志。
//
// 本包写 4xx / 5xx 响应体的地方**只有两处形状**：
//
//   - 带 error 的拒绝（参数白名单、语义上下文）→ 本函数（判定表 + 结构化日志）；
//   - 无 error 的固定短句（404 能力不存在 / 405 方法不符 / 401 需要登录 / 500 渲染失败）
//     → endpoint.go 内直接 c.String，但文案同样取自本文件的 fragmentErrText* 表、
//     经 fragmentErrTextOf 取词（收口前它们是写死的中文，英文站点恒显示中文）。
//
// （204「无需替换」不写响应体，不在其中。）
//
// 取词来源按「语言此刻能不能解析」分：404/405/401 都在参数校验与 lang 解析**之前**，
// 只能走 fragmentErrorT（只认请求明确声明的 ?lang）；500 渲染失败发生在 req 构造之后，
// 用 req.T —— 与成功路径共用同一份取词结果。
//
// 收成一处定义的意义：以后新增拒绝点时，写响应这件事不再需要每次重新判断
// 「这句话能不能给出去」。
//
// fields 是附加的日志字段（如非法的 context 值）—— 它们**只进日志**，不进响应。
func fragmentFail(c *gin.Context, err error, t func(key, fallback string) string, fields map[string]any) {
	status, text, known := fragmentErrResponse(err, t)
	entry := logger.Scene(fragmentErrScene).
		With("type", c.Param("type")).
		With("path", requestURLPath(c)).
		With("detail", fragmentErrDetail(err))
	for k, v := range fields {
		entry = entry.With(k, v)
	}
	if known {
		// 请求被白名单拒绝是**客户端**问题（同 adminBindFail 用 Warn 不污染错误日志）。
		entry.Warn("片段请求被白名单拒绝")
	} else {
		entry.Error(err, "片段端点内部错误")
	}
	c.String(status, text)
}

// fragmentErrorT 错误出口的取词函数：语言取请求明确声明的 ?lang（GET query / POST 表单原值）。
//
// 为什么与成功路径同源：片段语言只有 ?lang 这一个来源（lang_resolve.go）——
// 错误响应的文案与成功响应的片段必须是同一种语言，否则访客看到的中文提示
// 出现在英文站点上，看起来像另一个系统说的话。
//
// 为什么**不查 project_locales**：走到这里时参数校验已经失败，projectId 可能根本不存在，
// 没有任何可信的工程上下文可查。值域由 i18n 的兜底链兜住（当前语言 → 默认语言 →
// 包内中文原文）：任意垃圾值都只是「未命中 → 回落」，不输出裸 key、不输出空串。
//
// 只在参数校验失败的出口用它：上下文校验失败时语言已经解析完毕（req.Lang / req.T），
// 那里直接用 req.T —— 与成功路径共用同一份取词结果。
func fragmentErrorT(c *gin.Context) func(key, fallback string) string {
	return i18n.Snapshot(fragmentErrorLang(c))
}

// fragmentErrorLang 尽力取请求声明的语言（不做任何校验，失败即空串 → 默认语言）。
func fragmentErrorLang(c *gin.Context) string {
	if c == nil || c.Request == nil {
		return ""
	}
	if lang := strings.TrimSpace(c.Query(fragmentLangParam)); lang != "" {
		return lang
	}
	if c.Request.Method == http.MethodPost {
		// ParseForm 幂等（结果缓存在 Request.PostForm），不会与 collectFragmentParams
		// 的解析互相干扰；表单解析失败时返回空串 → 回落默认语言（那正是需要它的场景）。
		return strings.TrimSpace(c.PostForm(fragmentLangParam))
	}
	return ""
}

// fragmentErrDetail 取日志用的错误原文（nil 安全）。
func fragmentErrDetail(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

// requestURLPath 请求路径（日志用；c 异常时给空串，绝不 panic）。
func requestURLPath(c *gin.Context) string {
	if c == nil || c.Request == nil || c.Request.URL == nil {
		return ""
	}
	return c.Request.URL.Path
}
