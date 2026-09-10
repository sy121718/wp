package response

import (
	"fmt"
	"net/http"
	"strings"

	"go_wp/pkg/enums"
	"go_wp/pkg/i18n"
	"go_wp/pkg/logger"

	"github.com/gin-gonic/gin"
)

type Response struct {
	Code    int         `json:"code"`
	Message string      `json:"message"`
	Data    interface{} `json:"data,omitempty"`
}

func Success(c *gin.Context, data ...interface{}) {
	var responseData interface{}
	if len(data) > 0 {
		responseData = data[0]
	}

	c.JSON(http.StatusOK, Response{
		Code:    http.StatusOK,
		Message: translate(c, enums.MsgOperationSuccess),
		Data:    responseData,
	})
}

func SuccessWithMessage(c *gin.Context, message string, data ...interface{}) {
	var responseData interface{}
	if len(data) > 0 {
		responseData = data[0]
	}

	c.JSON(http.StatusOK, Response{
		Code:    http.StatusOK,
		Message: translate(c, message),
		Data:    responseData,
	})
}

func ErrorWithMessage(c *gin.Context, code int, message string) {
	c.JSON(code, Response{
		Code:    code,
		Message: translate(c, message),
	})
}

// msgInternalError 500 响应的统一对外文案。
const msgInternalError = "服务器内部错误，请稍后重试"

// ErrorInternal 收敛服务端内部错误：完整错误只进日志，对外统一返回通用文案。
//
// handler 直接写 ErrorWithMessage(c, 500, err.Error()) 会把 SQL 片段、文件路径、
// 内部标识符原样返回给客户端 —— 既是信息泄漏，也违反「响应文案统一走 enums」的
// 约定。收敛到本函数后，泄漏面只剩日志，且不再依赖每个 handler 自觉。
func ErrorInternal(c *gin.Context, scene string, err error) {
	if err != nil {
		if scene == "" {
			scene = "http"
		}
		logger.Scene(scene).Error(err, "handler 内部错误")
	}
	ErrorWithMessage(c, http.StatusInternalServerError, msgInternalError)
}

func ParamError(c *gin.Context, msg ...string) {
	message := enums.ErrInvalidParams
	if len(msg) > 0 {
		message = msg[0]
	}

	ErrorWithMessage(c, http.StatusBadRequest, message)
}

func NotFound(c *gin.Context, msg ...string) {
	message := enums.ErrNotFound
	if len(msg) > 0 {
		message = msg[0]
	}

	ErrorWithMessage(c, http.StatusNotFound, message)
}

func SuccessWithData(c *gin.Context, data interface{}) {
	Success(c, data)
}

// TranslateMessage 将业务消息按请求语言翻译（translate 的公开出口）。
// 供中间件等需要直接构造 response.Response 的场景使用（如 AbortWithStatusJSON）。
func TranslateMessage(c *gin.Context, message string) string {
	return translate(c, message)
}

// LangCookieName 语言协商 Cookie 名（后台语言切换写入，requestLanguage 优先读取）。
const LangCookieName = "lang"

// langCookieMaxAge 语言 Cookie 有效期（秒，1 年）。
const langCookieMaxAge = 365 * 24 * 60 * 60

// RequestLanguage 返回请求语言（requestLanguage 的公开出口），供 service 层按语言处理（如菜单标题翻译）。
func RequestLanguage(c *gin.Context) string {
	return requestLanguage(c)
}

// NormalizeLang 校验并规范化语言代码（语言切换入口复用同一白名单，避免第二套规则）。
// 返回规范化语言码与是否受支持；非法输入返回默认语言 + false，绝不返回空串（调用方无需再兜底）。
func NormalizeLang(raw string) (string, bool) {
	fallback := i18n.GetDefaultLang()
	if lang := normalizeLang(raw, ""); lang != "" {
		return lang, true
	}
	return fallback, false
}

// SetLangCookie 写入语言协商 Cookie：Path=/、HttpOnly、SameSite=Lax，
// release 模式（gin.ReleaseMode，由 server.mode 驱动）自动加 Secure。
// SameSite=Lax 下同站导航与 HTMX（同源 XHR）请求都会携带该 Cookie，故 HTMX 请求同样能带上语言。
func SetLangCookie(c *gin.Context, lang string) {
	if c == nil {
		return
	}

	http.SetCookie(c.Writer, &http.Cookie{
		Name:     LangCookieName,
		Value:    lang,
		Path:     "/",
		MaxAge:   langCookieMaxAge,
		HttpOnly: true,
		Secure:   gin.Mode() == gin.ReleaseMode,
		SameSite: http.SameSiteLaxMode,
	})
}

// requestLanguage 解析请求语言：Cookie lang 优先，其次 query lang，再次 Accept-Language 首段，最后默认语言。
// 解析结果统一规范化（zh/en 大小写与区域变体 → 标准码，不支持的语言回退默认，见 i18n-issues 2-4）。
// 兜底：任何一层缺失或非法都继续降级，最终返回默认语言，绝不报错、绝不 panic（c 为 nil 亦安全）。
func requestLanguage(c *gin.Context) string {
	fallback := i18n.GetDefaultLang()

	if c == nil {
		return fallback
	}

	// 1) Cookie（语言切换落地的持久选择）
	if cookie, err := c.Cookie(LangCookieName); err == nil {
		if lang := strings.TrimSpace(cookie); lang != "" {
			return normalizeLang(lang, fallback)
		}
	}

	// 2) query lang（显式单次覆盖）
	if lang := strings.TrimSpace(c.Query("lang")); lang != "" {
		return normalizeLang(lang, fallback)
	}

	// 3) Accept-Language 首段（浏览器默认偏好）
	accept := strings.TrimSpace(c.GetHeader("Accept-Language"))
	if accept != "" {
		first := strings.TrimSpace(strings.Split(accept, ",")[0])
		if idx := strings.Index(first, ";"); idx > 0 {
			first = strings.TrimSpace(first[:idx])
		}
		if first != "" {
			return normalizeLang(first, fallback)
		}
	}

	// 4) 配置默认语言
	return fallback
}

// normalizeLang 把语言代码规范化为系统支持的格式：
// zh / zh-cn / zh-Hans 等 → zh-CN；en / en-US / en-GB 等 → en-US；
// 超长或未识别的输入回退默认语言，避免以无效 code 命中不了资源。
func normalizeLang(raw, fallback string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" || len(raw) > 10 {
		return fallback
	}

	lower := strings.ToLower(raw)
	switch {
	case lower == "zh" || lower == "zh-cn" || lower == "zh_cn" || lower == "zh-hans":
		return "zh-CN"
	case lower == "en" || lower == "en-us" || lower == "en_us" || lower == "en-gb":
		return "en-US"
	default:
		return fallback
	}
}

// translate 将业务消息按请求语言翻译，支持三种形态：
//  1. 纯 key：message 即 sys_i18n 资源 key，命中直接翻译；
//  2. key|param：带格式化参数协议（如 "ErrAccountLocked|5m0s"），翻译后 Sprintf 注入参数；
//  3. key: detail：拼接消息兼容（如 "ErrInvalidParams: xxx"），仅翻译 key 前缀。
//
// 均未命中时原样返回，保证改造窗口期（库缺资源）不出现异常输出。
func translate(c *gin.Context, message string) string {
	lang := requestLanguage(c)

	// 形态 1：纯 key
	if text := i18n.GetText(message, lang); text != message {
		return text
	}

	// 形态 2：key|param1|param2
	if idx := strings.Index(message, "|"); idx > 0 {
		key := message[:idx]
		if text := i18n.GetText(key, lang); text != key {
			// 协议限定：模板只允许 %s / %[n]s / %%（参数按字符串注入）。
			// 含 %d/%f 等协议外占位符时不做注入，按 key 原文降级，避免 Go 格式错误输出。
			if !i18n.HasStringPlaceholdersOnly(text) {
				return message
			}
			args := strings.Split(message[idx+1:], "|")
			anyArgs := make([]interface{}, len(args))
			for i, a := range args {
				anyArgs[i] = a
			}
			return fmt.Sprintf(text, anyArgs...)
		}
	}

	// 形态 3：key: detail
	if idx := strings.Index(message, ": "); idx > 0 {
		prefix := message[:idx]
		if text := i18n.GetText(prefix, lang); text != prefix {
			return text + message[idx:]
		}
	}

	return message
}
