package response

// msg.go — 响应出口的取词（唯一一份实现）。
//
// 分工：
//   - 业务层只给「文案事实」（facing.Msg / facing.Fact / error），不取词；
//   - 响应出口在这里按请求语言取一次词，产出 {code,message,data}；
//   - 模板侧不经过这里：渲染期用注入的 t / tf（internal/text.Inject → templates.TranslateFunc）。
//
// 为什么放在 pkg/response 而不是 internal/text：MessageOf / ErrorAuto 已经在做同一件事
// （translate + 声明处兜底），两个入口各写一份必然漂移 —— 同一个事实在接口上说一句、
// 在页面槽上说另一句，而两边都不报错。

import (
	"fmt"

	"github.com/gin-gonic/gin"

	"go_wp/pkg/facing"
	"go_wp/pkg/i18n"
)

// InternalKey / InternalFallback：基础设施故障的归口文案（词条见 058 seed，scope=ui）。
const (
	InternalKey      = "MsgInternalError"
	InternalFallback = "系统内部错误，请稍后重试"
)

// MsgText 把一句文案事实取成当前语言文本。
//
// 分派按「填了哪一种参数」：有具名参数走 FillText（语序归词条），否则走 ArgsText（%s 位置）。
// 不按 Key 是否为空判 —— 那会让同一个事实在两条出口给出不同的句子。
func MsgText(c *gin.Context, m facing.Msg) string {
	if len(m.Named) > 0 {
		return FillText(c, m.Key, m.Fallback, m.Named)
	}
	return ArgsText(c, m.Key, m.Fallback, m.Args)
}

// ArgsText 取词 + %s 位置占位符填充（词条缺位时用声明处兜底）。
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

// FillText 取词 + {name} 具名占位符填充（语序由词条决定）。
func FillText(c *gin.Context, key, fallback string, params map[string]string) string {
	return i18n.FillTranslate(func(k, f string) string { return lookupText(c, k, f) }, key, fallback, params)
}

// InternalText 归口文案（当前语言）。
func InternalText(c *gin.Context) string {
	return lookupText(c, InternalKey, InternalFallback)
}

// InternalMsg 归口文案的事实（拿不到具体 err 的失败分支用）。
func InternalMsg() facing.Msg {
	return facing.Msg{Key: InternalKey, Fallback: InternalFallback}
}

// SuccessMsg 成功响应 + 一句业务给的事实文案。
func SuccessMsg(c *gin.Context, m facing.Msg, data ...interface{}) {
	SuccessWithMessage(c, MsgText(c, m), data...)
}

// ErrorMsg 错误响应 + 一句业务给的事实文案（取词只在这里发生一次）。
func ErrorMsg(c *gin.Context, code int, m facing.Msg) {
	ErrorWithMessage(c, code, MsgText(c, m))
}

// lookupText 命中词条用词条，否则用声明处兜底（两者都没有时返回 key 原文）。
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
