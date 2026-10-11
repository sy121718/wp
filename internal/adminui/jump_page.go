package adminui

// jump_page.go — **提示页**（admin/jump.html）的渲染数据与渲染入口，全站只有这一份契约。
//
// 为什么单独成文件、且归本包：提示页有两个渲染入口 —— 写动作的 shell.RenderJump（HTTP 200）
// 与页面拒绝的 adminui.Reject（真实 403/500）。两者曾经各持一个同名 struct（「过渡期极小复制」），
// 字段名就是模板契约，漂移一次整页正文/链接就**静默变空**（模板对缺字段是运行时报错，
// 但那种失败只在真实渲染时才出现，而单测常常只断言 HTTP 状态码）。
// 现在视图与渲染入口都只此一处，两边都调它。
//
// 依赖方向：shell → adminui（单向），所以 shell 可以引用本文件；adminui 不反向引用 shell。

import (
	"github.com/gin-gonic/gin"
)

// JumpView 提示页的渲染数据（字段名是模板契约，admin/jump.html 按名取）。
//
// 正文是**事实**（Key + 中文兜底 + `%s` 参数）而不是成品串：取词由模板注入的 tf 完成
// （见 prepare.go），所以提示页的语言跟着当前请求走，而调用点不需要知道当前是什么语言。
type JumpView struct {
	// OK 决定外观（成功 / 失败）。
	OK bool
	// MsgKey 正文的 i18n key（可空：为空时 Msg 原样输出）。
	MsgKey string
	// Msg 正文的中文兜底。MsgKey 为空时它就是正文本身。
	Msg string
	// MsgArgs MsgKey 的 `%s` 位置参数（已 strconv 成字符串）。
	MsgArgs []string
	// Back 自动跳转目标（站内相对路径）。
	Back string
	// BackTextKey / BackText 「立即前往」的链接文字；两者都空则不渲染链接。
	BackTextKey string
	BackText    string
	// BackTextShow 由 RenderJumpPage 计算（模板按它决定要不要渲染链接）：
	// 让模板只读一个布尔，省掉「key 或兜底非空」这种复合条件（Jet 里写两个 ||
	// 一旦漏一边就是静默不渲染链接）。
	BackTextShow bool
	// Seconds 停留秒数：>0 时按 meta refresh 自动跳转。
	Seconds int
}

// RenderJumpPage 渲染提示页（标题取正文的成品串：标签页说清刚才发生了什么）。
//
// status 由调用方给：写动作是 200（失败也是一次成功的响应，只是结论是失败），
// 页面拒绝是真实 403/500（200 会让失败看起来像成功，监控与客户端都靠状态码区分）。
func RenderJumpPage(c *gin.Context, status int, v JumpView) {
	RenderJumpPageWith(c, status, v, Prepare)
}

// RenderJumpPageWith 同 RenderJumpPage，但由调用方给壳键注入器。
//
// 拒绝出口（reject.go）要保留「壳由调用方实现」这条边界（ShellInjector 的全部意义），
// 所以它用这个变体；写动作出口（shell.RenderJump）没有这层需要，直接用 Prepare。
func RenderJumpPageWith(c *gin.Context, status int, v JumpView, inject ShellInjector) {
	if c == nil {
		return
	}
	if inject == nil {
		inject = Prepare
	}
	// 正文为空时回落归口事实：页面不能显示空串（用户会以为操作成功了）。
	if v.MsgKey == "" && v.Msg == "" {
		fact := InternalErrorFact()
		v.MsgKey, v.Msg = fact.Key, fact.Fallback
	}
	if v.MsgArgs == nil {
		v.MsgArgs = []string{}
	}
	v.BackTextShow = v.BackTextKey != "" || v.BackText != ""
	if v.Seconds < 0 {
		v.Seconds = 0
	}

	c.Header("Cache-Control", "no-store")
	c.HTML(status, "admin/jump.html", inject(c, gin.H{
		// 标题必须自己先填好占位符，否则标签页会留下一个 %s
		//（Prepare 会把 title 当 key 再查一次词条，查不到回落原文）。
		"title": TranslateArgs(c, v.MsgKey, v.Msg, v.MsgArgs),
		"Jump":  v,
	}))
}
