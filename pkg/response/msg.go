package response

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"go_wp/pkg/facing"
	"go_wp/pkg/i18n"
)

const (
	InternalKey      = "uiInternalErr"
	InternalFallback = "系统内部错误，请稍后重试"
)

func MsgText(c *gin.Context, m facing.Msg) string {
	if len(m.Named) > 0 {
		return FillText(c, m.Key, m.Fallback, m.Named)
	}
	return ArgsText(c, m.Key, m.Fallback, m.Args)
}

// ArgsText 走「key + 中文兜底 + 位置参数」的取词。
//
// 缺词（词库没有 key）时一律退成归口文案，不把裸 key 交给前端：
// fallback 为空、或 fallback 就是 key 本身，都不构成兜底 —— 那两种情况拼出来的
// 就是内部 key（AGENTS.md「不把裸 key 或内部原文交给前端」）。
// 兜底文案本身是「消息事实」，只在真的存在时才用。
//
// 参数填不进这句话时同样退归口文案，绝不把 fmt 的错误标记交给前端：
// 少参吐 %!s(MISSING)、多参吐 %!(EXTRA ...)、非 %s 动词吐 %!d(...)，
// 三条都会原样上屏。这里只做判定不做补救，也不额外记日志 ——
// 缺词日志只属于「词条不存在」这一件事，参数不齐是调用方传参问题，不该混进去。
func ArgsText(c *gin.Context, key, fallback string, args []string) string {
	text, ok := lookupTextOrEmpty(c, key, fallback)
	if !ok {
		logMissingMessage(c, "argsText", key)
		return InternalText(c)
	}
	// 一句话能不能被这批参数填满，只问一处（i18n.StringPlaceholderCount）：
	// 非 %s 动词、显式索引、宽度/精度、星号，都在那里一次判掉。
	want, fillable := i18n.StringPlaceholderCount(text)
	if !fillable {
		return InternalText(c)
	}
	if want != len(args) {
		// %s 个数与参数个数不等（含「有占位符却零参数」）：少参/多参都会漏出 fmt 的错误标记。
		return InternalText(c)
	}
	values := make([]any, len(args))
	for i, s := range args {
		values[i] = s
	}
	return fmt.Sprintf(text, values...)
}

func FillText(c *gin.Context, key, fallback string, params map[string]string) string {
	if fallback == "" || fallback == key {
		// 空兜底与「兜底就是 key」都等于没有兜底：拼出来的是内部 key。
		// 这类 key 只认词库，缺词或参数没填全是归口文案，
		// 不把 key、也不把没替换掉的占位符拼进 message。
		// 缺词就在这里记一条可定位日志（key / lang / 调用处 / 路径），这是唯一准确的位置：
		// 参数不足与「译文恰好等于归口文案」都不该被记成缺词。
		text := translate(c, key)
		if text == key || text == "" {
			logMissingMessage(c, "fillText", key)
			return InternalText(c)
		}
		filled, ok := i18n.FillNamedPlaceholders(text, params)
		if !ok {
			return InternalText(c)
		}
		return filled
	}
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

// lookupText 是「取词，缺了就退 fallback」的宽松版本：调用方已经知道缺词可以接受
// （比如归口文案自己、以及 FillTranslate 的逐键回调）。要用它给前端产出 message 的地方
// 必须用 lookupTextOrEmpty 判 ok，否则缺词时吐出的就是 key 字面量。
func lookupText(c *gin.Context, key, fallback string) string {
	if out, ok := lookupTextOrEmpty(c, key, fallback); ok {
		return out
	}
	return key
}

// lookupTextOrEmpty 只在真的取到「不是 key 本身」的一句话时返回 ok。
// fallback 为空或等于 key 都不算兜底：那等于把内部 key 当文案交出去。
func lookupTextOrEmpty(c *gin.Context, key, fallback string) (string, bool) {
	if out := translate(c, key); out != key && out != "" {
		return out, true
	}
	if fallback != "" && fallback != key {
		return fallback, true
	}
	return "", false
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

// BadRequestForPage 是页面参数错误出口：日志带场景与内部错误，对外只给归口文案。
func BadRequestForPage(c *gin.Context, scene string, err error) {
	if err != nil {
		LogInternalErrorf(c, scene, "后台页面参数校验失败", err)
	}
	ErrorWithMessage(c, http.StatusBadRequest, InternalKey)
}

// InternalErrorForPage 是页面处理错误出口：日志带场景与内部错误，对外只给归口文案。
func InternalErrorForPage(c *gin.Context, scene string, err error) {
	if err != nil {
		LogInternalErrorf(c, scene, "后台页面处理失败", err)
	}
	ErrorWithMessage(c, http.StatusInternalServerError, InternalKey)
}
