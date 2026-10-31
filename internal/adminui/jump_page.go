package adminui

import (
	"github.com/gin-gonic/gin"
)

type ShellInjector func(c *gin.Context, data gin.H) gin.H

type JumpView struct {
	OK bool

	MsgKey string

	Msg string

	MsgArgs []string

	Back string

	BackTextKey string
	BackText    string

	BackTextShow bool

	Seconds int
}

func RenderJumpPage(c *gin.Context, status int, v JumpView) {
	RenderJumpPageWith(c, status, v, Prepare)
}

func RenderJumpPageWith(c *gin.Context, status int, v JumpView, inject ShellInjector) {
	if c == nil {
		return
	}
	if inject == nil {
		inject = Prepare
	}

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

		"title": TranslateArgs(c, v.MsgKey, v.Msg, v.MsgArgs),
		"Jump":  v,
	}))
}
