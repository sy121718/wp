package pagehttp

// page_jump.go — page 后台页写动作的**共享出口**：整页提示（shell.RenderJump，对应 ThinkPHP 的
// success() / error()）与「错误 → 一句结论」的取词原语。
//
// 取代原先的 302/303 + `?err=` / `?done=` / `?ok=` 回列表页：那条通道要求读侧再判一次
// 「这条提示是不是本仓给的」（pagePageErr / pagePageDone / pageNoticeTexts /
// redirectOkKey / redirectErrKeyFromCode / siteSlotQueryText 就是那套），而查询参数
// 不是可信边界。文案改走响应体之后，那套判定整批删除。
//
// 三条边界（同 shell.RenderJump 的注释）：
//   · 结论是**事实**（key + 中文兜底 + `%s` 参数），不是拼好的句子 —— 控制器不取词，
//     取词由模板注入的 tf 做（见 adminui.Prepare）。`pageMsg` 就是这条事实的形状；
//   · 错误事实的判定统一在 adminui.PageErrorFact（pkg/response.IsBusinessError），
//     本模块不再维护自己的白名单：原先的 page_err.go 已整个删除；
//   · 回跳地址由 shell.BackPath 从**表单 action 的 query** 按白名单读回（服务端自己拼，
//     不读隐藏域里的整串 URL）；结论不进 URL。
//
// **本文件只放被多个功能共用**的那几件：pageJump 原语、页面列表的回跳（语言面板 / 排定面板 /
// 翻译工作台写的都是列表页里的东西，回跳目标同一处）、以及错误取词。**每个功能自己的回跳地址、
// 回跳文字、回执文案放在各自的功能文件里**（page_redirect.go / page_site_slot_handle.go /
// page_translations_handle.go / pages_handle.go）—— 控制器按功能拆成多个文件
// 的意义就在这里：改重定向的回执不必翻一个装五个功能的杂物箱。

import (
	"strings"

	"github.com/gin-gonic/gin"

	"go_wp/internal/adminui"
	pageenums "go_wp/internal/module/page/enums"
	"go_wp/internal/shell"
	"go_wp/pkg/i18n"
	"go_wp/pkg/logger"
)

// —— 页面列表的回跳（列表页内的各个面板都回这里）——
//
// 回跳筛选键由**两条路径共用**：渲染时拼进表单 action 的 query、POST 回来时由
// shell.BackPath 读回。两处分叉的表现是「写完跳回去筛选静默丢了」—— 页面不报错、
// 日志也干净，所以键表必须只有一份（不要在两处各写一遍字面量）。

var pageListBackKeys = []string{"project"}

const pageListPath = "/admin/pages"

func pageListBack(c *gin.Context) string {
	return shell.BackPath(c, pageListPath, pageListBackKeys...)
}

// pageListBackMsg 回跳链接文字（复用页面列表页自己的标题词条，不新增全站词条）。
//
// 静态的 key + 中文原文，所以是包级变量：**不需要 gin.Context** ——
// 「这段文案属于哪个语言」由模板取词时决定，与控制器无关。
var pageListBackMsg = pageMsg{Key: pageenums.MsgPagesTitle, Fallback: "页面管理"}

// —— 提示页的一句结论 ——

// pageMsg 提示页的一句结论 —— 就是共享形状 adminui.Msg（key + 中文兜底 + `%s` 参数）。
//
// 这里用别名而不是新造 struct：六个模块各造一份同形类型就是六份契约，字段名与参数编码
// 迟早分叉，而症状是「某个页面的提示偶尔少一个数字」，既不报错也不记日志。
type pageMsg = adminui.Msg

// pageMsgText 把一句结论取成当前语言文本（片段槽位 / JSON 出口没有模板取词时用）。
func pageMsgText(c *gin.Context, m pageMsg) string {
	return adminui.MsgText(c, m)
}

// —— 错误事实 ——

// pageErrScene 日志场景名（与 page 模块其它 logger.Scene("page") 一致）。
const pageErrScene = "page"

// pageErrMsg 业务错误 → 一句结论（命中白名单的判定已收敛到 adminui.PageErrorFact）。
//
// 原先这里是 page_err.go 的 pageFacingText / pageErrPageText / pageFacingOrInternal 三件套：
// 模块自己维护一份 pageservice sentinel 白名单 + 受控前缀，再逐条 errors.Is 判定。
// 那是**第二份真相** —— service 换了措辞、或某个 key 改由别的模块提供，白名单就静默失配，
// 而两种症状都不报错（有时把内部错误当业务文案透出、有时把业务文案吞成归口）。
// 现在判据只有一条：**业务错误的取值本来就是 enums 常量，且 enums 的值即 i18n key**，
// 所以「认得出形态 = 可以直接给用户看」，认不出则记日志 + 归口。
func pageErrMsg(c *gin.Context, err error) pageMsg {
	return pageErrMsgOf(c, adminui.PageErrorFact(c, pageErrScene, err))
}

// pageErrMsgQuiet 同 pageErrMsg，但不记日志 —— 给「调用点已经记过一条更具体的日志
// （带 pageId / lang / name）」的地方用，免得同一个错误在日志里出现两遍。
func pageErrMsgQuiet(c *gin.Context, err error) pageMsg {
	return pageErrMsgOf(c, adminui.PageErrorFactQuiet(c, pageErrScene, err))
}

func pageErrMsgOf(c *gin.Context, f adminui.ErrFact) pageMsg {
	if f.Key == "" {
		return pageMsg{}
	}
	if f.Tail != "" {
		if detail := pageDetailText(shell.TranslateFor(c), f.Tail); detail != "" {
			// 明细是「词条 + 具名参数」的多段结构，一段 key 表达不了它 ——
			// 这一档给成品串（与迁入前逐字一致；明细本身仍随语言变化）。
			return pageMsg{Fallback: adminui.TranslateArgs(c, f.Key, f.Fallback, nil) + "：" + detail}
		}
	}
	return pageMsg{Key: f.Key, Fallback: f.Fallback}
}

// pageErrText 错误结论的**文本**形态 —— 只给 JSON 出口用。
//
// JSON 响应里没有模板取词这一环（前端拿到的是字节，不是模板），成品文案必须在服务端拼；
// 页面出口一律交 pageMsg 事实 + 模板里的 tr / tf，不要调本函数。
func pageErrText(c *gin.Context, err error) string {
	return pageMsgText(c, pageErrMsg(c, err))
}

// pageDetailText 业务错误的**补充说明** → 当前语言文案（不是词条时返回空串）。
//
// 接受的形态只有一种：i18n.ErrorDetail 的产物（控制字符开头的「明细词条 key + 具名参数」，
// 可多段）。其余一律**丢弃并落日志** —— 这与 product / inventory 的读侧口径有意不同：
// 那两处的 `return tail` 是为了兼容改造前就已存在的纯文本明细，而 page 这一族从本批起
// 全部改走 ErrorDetail（写侧见 page/service/page_document_detail.go），所以不再保留
// 「原样透出」这条无界通道 —— 它正是英文界面上中文混排的来源。
//
// 未登记的明细 key（拼错 / 新加漏登记）跳过该段并记一条日志：少一句补充说明，
// 好过把编码串或半截占位符摆到页面上。
//
// 留在本模块而不是 adminui：明细词条的回落表是 **page 自己的 enums**
// （pageenums.ErrDetailFallbacks），共享层不替它保管。
func pageDetailText(tr func(key, fallback string) string, tail string) string {
	parts, ok := i18n.ParseErrorDetails(tail)
	if !ok {
		logger.Scene(pageErrScene).With("tail", i18n.DetailTailForLog(tail)).
			Warn("业务错误的补充说明不是登记的词条形态，已丢弃（不再原样透出）")
		return ""
	}
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		fallback, registered := pageenums.ErrDetailFallbacks[p.Key]
		if !registered || fallback == "" {
			logger.Scene(pageErrScene).With("detail_key", p.Key).
				Warn("业务错误的补充说明词条未登记，已省略该段")
			continue
		}
		out = append(out, i18n.FillTranslate(tr, p.Key, fallback, p.Args))
	}
	return strings.Join(out, "；")
}

// —— 出口 ——

// pageJump 渲染整页提示：成功 1 秒后自动回跳，失败不自动跳（运营要看清楚原因）。
func pageJump(c *gin.Context, ok bool, msg pageMsg, back string, backText pageMsg) {
	seconds := 0
	if ok {
		seconds = 1
	}
	shell.RenderJump(c, shell.Jump{
		OK:          ok,
		MsgKey:      msg.Key,
		Msg:         msg.Fallback,
		MsgArgs:     msg.Args,
		Back:        back,
		BackTextKey: backText.Key,
		BackText:    backText.Fallback,
		Seconds:     seconds,
	})
}

// pageListJump 回页面列表的提示页出口 —— 语言面板 / 排定面板 / 翻译工作台写的都是列表页里的
// 东西，回跳目标同一处，所以这几条共用它（各功能自己的回跳在各自文件里）。
func pageListJump(c *gin.Context, ok bool, msg pageMsg) {
	pageJump(c, ok, msg, pageListBack(c), pageListBackMsg)
}

// outcomeMsg 把 enums 的**结果事实**（Outcome：key + 兜底 + 占位符实参）转成槽位事实。
//
// 唯一转换点：控制器不选文案（选哪一条 + 带哪几个计数是 enums 的判据），也不自己 strconv。
func outcomeMsg(o pageenums.Outcome) pageMsg {
	return pageMsg{Key: o.Key, Fallback: o.Fallback, Args: o.Args}
}

// pageInternalMsg 归口提示（拿不到具体 err 的失败分支用）：提示页不能显示空串。
func pageInternalMsg() pageMsg {
	f := adminui.InternalErrorFact()
	return pageMsg{Key: f.Key, Fallback: f.Fallback}
}
