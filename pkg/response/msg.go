package response

import (
	"fmt"
	"strings"

	"github.com/gin-gonic/gin"

	"go_wp/pkg/facing"
	"go_wp/pkg/i18n"
)

const (
	InternalKey      = "MsgInternalError"
	InternalFallback = "系统内部错误，请稍后重试"
)

func MsgText(c *gin.Context, m facing.Msg) string {
	if len(m.Named) > 0 {
		return FillText(c, m.Key, m.Fallback, m.Named)
	}
	return ArgsText(c, m.Key, m.Fallback, m.Args)
}

func ArgsText(c *gin.Context, key, fallback string, args []string) string {
	text := lookupText(c, key, fallback)
	if len(args) == 0 {
		return text
	}
	if !i18n.HasStringPlaceholdersOnly(text) {
		text = fallback
	}
	values := make([]any, len(args))
	for i, s := range args {
		values[i] = s
	}
	return fmt.Sprintf(text, values...)
}

func FillText(c *gin.Context, key, fallback string, params map[string]string) string {
	return i18n.FillTranslate(func(k, f string) string { return lookupText(c, k, f) }, key, fallback, params)
}

func InternalText(c *gin.Context) string {
	return lookupText(c, InternalKey, InternalFallback)
}

func InternalMsg() facing.Msg {
	return facing.Msg{Key: InternalKey, Fallback: InternalFallback}
}

func SuccessMsg(c *gin.Context, m facing.Msg, data ...interface{}) {
	SuccessWithMessage(c, MsgText(c, m), data...)
}

func ErrorMsg(c *gin.Context, code int, m facing.Msg) {
	ErrorWithMessage(c, code, MsgText(c, m))
}

func lookupText(c *gin.Context, key, fallback string) string {
	out := translate(c, key)
	if out != key {
		return out
	}
	if fallback != "" && fallback != key {
		return fallback
	}
	return out
}

func ErrorRows(c *gin.Context, code int, title facing.Msg, rows []facing.Labeled) {
	parts := make([]string, 0, len(rows))
	for _, r := range rows {
		body := MsgText(c, r.Msg)
		if r.Label != "" {
			body = r.Label + "：" + body
		}
		parts = append(parts, body)
	}
	if len(parts) == 0 {
		ErrorWithMessage(c, code, InternalText(c))
		return
	}
	ErrorWithMessage(c, code, MsgText(c, title)+"："+strings.Join(parts, "；"))
}
