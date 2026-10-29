package adminui

// 既有提示模板的渲染契约。认证拒绝和商品操作不再使用此出口。
// 依赖方向保持 shell → adminui，不反向依赖 shell。

import (
	"github.com/gin-gonic/gin"
)

// ShellInjector 向既有页面模板注入外壳键，签名与 Prepare 一致。
type ShellInjector func(c *gin.Context, data gin.H) gin.H

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
// status 由既有调用方给出，鉴权失败使用 Reject 的 JSON 出口。
func RenderJumpPage(c *gin.Context, status int, v JumpView) {
	RenderJumpPageWith(c, status, v, Prepare)
}

// RenderJumpPageWith 同 RenderJumpPage，但由调用方给壳键注入器。
//
// 注入器用于壳装配与渲染契约测试；不参与认证拒绝响应。
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
