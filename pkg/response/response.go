package response

import (
	"errors"
	"fmt"
	"net/http"
	"strings"

	"go_wp/pkg/enums"
	"go_wp/pkg/facing"
	"go_wp/pkg/i18n"
	"go_wp/pkg/sitehttps"

	"github.com/gin-gonic/gin"
)

var ErrHandled = errors.New("response already written")

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

	writeResponse(c, http.StatusOK, Response{
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

	writeResponse(c, http.StatusOK, Response{
		Code:    http.StatusOK,
		Message: translate(c, message),
		Data:    responseData,
	})
}

func ErrorWithMessage(c *gin.Context, code int, message string) {
	writeResponse(c, code, Response{
		Code:    code,
		Message: translate(c, message),
	})
}

func ErrorWithData(c *gin.Context, code int, message string, data interface{}) {
	writeResponse(c, code, Response{
		Code:    code,
		Message: translate(c, message),
		Data:    data,
	})
}

func ErrorInternal(c *gin.Context, scene string, err error) {
	if err != nil {
		if scene == "" {
			scene = "http"
		}
		logInternalError(c, scene, "handler 内部错误", err)
	}
	ErrorWithMessage(c, http.StatusInternalServerError, InternalKey)
}

func IsBusinessError(err error) bool {
	return facing.IsBusinessError(err)
}

func ErrorAuto(c *gin.Context, code int, scene string, err error) {
	var bodyLimit *http.MaxBytesError
	if errors.As(err, &bodyLimit) {
		code = http.StatusRequestEntityTooLarge
		err = facing.New(enums.ErrRequestEntityTooLarge)
	}
	if fact, ok := facing.FactOf(err); ok && fact.Business {

		if data, blocked := blockedDataOf(c, scene, err); blocked {
			ErrorWithData(c, code, businessText(c, scene, fact), data)
			return
		}
		ErrorWithMessage(c, code, businessText(c, scene, fact))
		return
	}
	if err != nil {
		if scene == "" {
			scene = "http"
		}
		logInternalError(c, scene, "handler 内部错误", err)
	}

	ErrorWithMessage(c, http.StatusInternalServerError, InternalKey)
}

func ErrorStatus(err error) int {
	var bodyLimit *http.MaxBytesError
	if errors.As(err, &bodyLimit) {
		return http.StatusRequestEntityTooLarge
	}
	if status, ok := facing.StatusOf(err); ok {
		return status
	}
	if fact, ok := facing.FactOf(err); ok && fact.Business {
		return http.StatusBadRequest
	}
	return http.StatusInternalServerError
}

func ErrorStatusAuto(c *gin.Context, scene string, err error) {
	ErrorAuto(c, ErrorStatus(err), scene, err)
}

func blockedDataOf(c *gin.Context, scene string, err error) (gin.H, bool) {
	var blocked facing.BlockedError
	if !errors.As(err, &blocked) {
		return nil, false
	}
	items := blocked.BlockedItems()
	rows := make([]gin.H, 0, len(items))
	for _, it := range items {
		rows = append(rows, gin.H{
			"id":        it.ID,
			"name":      it.Name,
			"reasonKey": it.ReasonKey,
			"message":   blockItemText(c, scene, it),
		})
	}
	return gin.H{"items": rows}, true
}

func businessText(c *gin.Context, scene string, fact facing.Fact) string {
	out := translate(c, fact.Key)
	if out == fact.Key {
		if fact.Fallback != "" && fact.Fallback != fact.Key {
			out = fact.Fallback
		} else {
			// 词库缺词、错误声明也没带兜底：给共享提示，不把裸 key 交出去。
			// 前端只看到归口文案，缺哪条词只能靠这条日志定位 —— 只记 key / lang / scene / path，
			// 不记请求参数与错误明细（那些可能带用户输入或内部原文）。
			logMissingMessage(c, scene, fact.Key)
			return InternalText(c)
		}
	}
	if fact.Tail != "" {
		return out + "：" + detailText(c, fact.Tail)
	}
	return out
}

// blockItemText 复用 businessText：主错误与拒绝回执的行都只走这一条取词出口。
func blockItemText(c *gin.Context, scene string, it facing.BlockedItem) string {
	return businessText(c, scene, facing.Fact{Key: it.ReasonKey, Fallback: it.Fallback, Business: true})
}

func MessageOf(c *gin.Context, scene string, err error) string {
	if err == nil {
		return ""
	}
	if fact, ok := facing.FactOf(err); ok && fact.Business {
		return businessText(c, scene, fact)
	}
	if scene == "" {
		scene = "http"
	}
	logInternalError(c, scene, "handler 内部错误", err)
	// 与 ErrorAuto / ErrorInternal 同一条归口语义：走 InternalKey → 词库 → InternalFallback。
	return translate(c, InternalKey)
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

func TranslateMessage(c *gin.Context, message string) string {
	return translate(c, message)
}

const LangCookieName = "lang"

const langCookieMaxAge = 365 * 24 * 60 * 60

func RequestLanguage(c *gin.Context) string {
	return requestLanguage(c)
}

func NormalizeLang(raw string) (string, bool) {
	fallback := i18n.GetDefaultLang()
	if lang := normalizeLang(raw, ""); lang != "" {
		return lang, true
	}
	return fallback, false
}

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
		Secure:   sitehttps.Enabled(),
		SameSite: http.SameSiteLaxMode,
	})
}

func requestLanguage(c *gin.Context) string {
	fallback := i18n.GetDefaultLang()

	if c == nil {
		return fallback
	}

	if cookie, err := c.Cookie(LangCookieName); err == nil {
		if lang := strings.TrimSpace(cookie); lang != "" {
			return normalizeLang(lang, fallback)
		}
	}

	if lang := strings.TrimSpace(c.Query("lang")); lang != "" {
		return normalizeLang(lang, fallback)
	}

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

	return fallback
}

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

func translate(c *gin.Context, message string) string {
	if text, ok := translateKnown(c, message); ok {
		return text
	}
	if message == InternalKey {
		return InternalFallback
	}
	return message
}

func translateKnown(c *gin.Context, message string) (string, bool) {
	lang := requestLanguage(c)

	if text := i18n.GetText(message, lang); text != message {
		return text, true
	}

	if idx := strings.Index(message, "|"); idx > 0 {
		key := message[:idx]
		if text := i18n.GetText(key, lang); text != key {

			// 参数个数与可填位置数必须相等：非 %s 动词、索引、宽度一律不接受，
			// 否则 Sprintf 会在屏幕上留下 %!s(MISSING) / %!d 之类标记。
			want, fillable := i18n.StringPlaceholderCount(text)
			if !fillable {
				return message, false
			}
			args := strings.Split(message[idx+1:], "|")
			if want != len(args) {
				return message, false
			}
			anyArgs := make([]interface{}, len(args))
			for i, a := range args {
				anyArgs[i] = a
			}
			return fmt.Sprintf(text, anyArgs...), true
		}
	}

	if idx := strings.Index(message, ": "); idx > 0 {
		prefix := message[:idx]
		if text := i18n.GetText(prefix, lang); text != prefix {
			return text + message[idx:], true
		}
	}

	return message, false
}
