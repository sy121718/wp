package shell

// jump.go — 写操作完成后的**整页提示**（对应 ThinkPHP 的 $this->success() / $this->error()）。
//
// 要替换掉的现状：写动作的结论靠 302 + `?err=` / `?ok=` / `?done=` 带回列表页。那条通道
// 的代价是**整套读侧判定**：查询参数不是可信边界（读侧回执协议已退役），所以每个模块都要
// 维护一份「受控文案 + 数字归一模板」来证明这条提示确实出自本仓 —— order 一个模块就为此
// 长出 orderFacingText / orderDoneTexts / orderBulkTextOf / orderPageDone 与 16 个回跳函数。
//
// 改成由本入口**直接渲染**提示页之后：文案走响应体、不进 URL，不可信输入不在这条链上，
// 读侧判定与回跳通道随之可以整批删除。
//
// 四条边界（不是偏好，各自对应一个已经踩过的坑）：
//   - **内部错误仍走 JSON 出口**（原 shell.PageError，task-114 起是 adminui.PageErrorInternal）：
//     这里只呈现**已经被判定可以给用户看**的文案，原文只进日志 —— 换个页面呈现不等于可以
//     把 err.Error() 铺在页面上。判据已收敛到 adminui.PageErrorFact（pkg/response.IsBusinessError），
//     模块不再各写一份白名单；
//   - **提示正文是「事实」不是成品串**：调用点给 key + 中文兜底 + 具名参数，模板用注入的 tf 取词
//     （见 internal/adminui/prepare.go）。控制器拼整句的旧形态正在按模块退役 —— 拼在 Go 里意味着
//     「这段文案属于哪个语言」要在 controller 里再想一遍，而它本来只是页面上的一句话；
//   - **htmx 请求走 HX-Redirect**（与既有 couponRedirectWhere 的分档一致）：htmx 会把响应
//     换进目标节点，整页 HTML 塞进去是坏页面。代价是 htmx 那一档**看不到提示文案** ——
//     与现状一致，不引入回退；要保住文案需要一套闪存（session）机制，不在本批；
//   - **回跳地址必须站内**：复用 pkg/safepath 的判据（与语言切换回跳同一份），
//     否则一个提示页就成了开放重定向的跳板。**长度上限不在这里施加** ——
//     提示页的目标是服务端自己给的跳转目标，与语言切换隐藏域/回跳地址的 512 字节口径不同；
//     迁移时按现状保留（判据函数本身也不含长度，见 pkg/safepath 包注释）。

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"go_wp/internal/adminui"
	"go_wp/pkg/reqparams"
	"go_wp/pkg/safepath"
)

// Jump 是一次提示页的全部输入。
type Jump struct {
	// OK 决定外观（成功 / 失败）。
	OK bool
	// MsgKey 提示正文的 i18n key（可空：为空时 Msg 原样输出）。
	MsgKey string
	// Msg 提示正文的中文兜底。MsgKey 非空时它是「缺词条时显示什么」，
	// 为空时它就是正文本身（迁移中的旧形态）。
	//
	// 两者都为空时回落归口事实（InternalErrorFact）—— 页面不能显示空串。
	Msg string
	// MsgArgs MsgKey 的 `%s` 位置参数（**已 strconv 成字符串**，见 internal/adminui.TranslateArgs）。
	// 提示正文没有计数时传 nil。
	MsgArgs []string
	// Back 自动跳转目标：**站内相对路径**。非法值丢弃并回控制面首页。
	Back string
	// BackTextKey 「立即前往」链接文字的 i18n key（可空）。
	BackTextKey string
	// BackText 链接文字的中文兜底 / 原文；两者都空则不渲染链接（仍会自动跳转）。
	//
	// 刻意不给默认文案：默认值要么新增一条全站词条（本次不引入），要么借别的模块的词条
	// （跨模块借词条会随对方改动漂移）。调用点本来就有一句现成的话（「返回优惠码列表」）。
	BackText string
	// Seconds 停留秒数：>0 时按 meta refresh 自动跳转；<=0 只给链接，不自动跳。
	Seconds int
}

// RenderJump 渲染整页提示（HTTP 200）。
func RenderJump(c *gin.Context, j Jump) {
	if c == nil {
		return
	}

	back := strings.TrimSpace(j.Back)
	if !safepath.IsSameOriginPath(back) {
		back = adminHomePath
	}

	// htmx：换 HX-Redirect 整页跳转，不把整页 HTML 塞进片段位置。
	if reqparams.IsHXRequest(c) {
		c.Header("HX-Redirect", back)
		c.Status(http.StatusOK)
		return
	}

	// 视图与渲染入口归 adminui（与页面拒绝出口共用同一份模板契约，见 adminui/jump_page.go）。
	// 原文只进日志：换个页面呈现不等于可以把 err.Error() 铺在页面上。
	adminui.RenderJumpPage(c, http.StatusOK, adminui.JumpView{
		OK:          j.OK,
		MsgKey:      strings.TrimSpace(j.MsgKey),
		Msg:         strings.TrimSpace(j.Msg),
		MsgArgs:     j.MsgArgs,
		Back:        back,
		BackTextKey: strings.TrimSpace(j.BackTextKey),
		BackText:    strings.TrimSpace(j.BackText),
		Seconds:     j.Seconds,
	})
}
