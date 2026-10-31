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

const msgInternalError = "服务器内部错误，请稍后重试"

func ErrorInternal(c *gin.Context, scene string, err error) {
	if err != nil {
		if scene == "" {
			scene = "http"
		}
		logInternalError(c, scene, "handler 内部错误", err)
	}
	ErrorWithMessage(c, http.StatusInternalServerError, msgInternalError)
}

func IsBusinessError(err error) bool {
	return facing.IsBusinessError(err)
}

func ErrorAuto(c *gin.Context, code int, scene string, err error) {
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

	ErrorWithMessage(c, http.StatusInternalServerError, msgInternalError)
}

func ErrorStatus(err error) int {
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
			"message":   businessText(c, scene, facing.Fact{Key: it.ReasonKey, Fallback: it.Fallback, Business: true}),
		})
	}
	return gin.H{"items": rows}, true
}

func businessText(c *gin.Context, scene string, fact facing.Fact) string {
	out := translate(c, fact.Key)
	if out != fact.Key {

		if fact.Tail != "" {
			return out + "：" + fact.Tail
		}
		return out
	}

	if fact.Fallback != "" && fact.Fallback != fact.Key {
		return fact.Fallback
	}

	return out
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
	return translate(c, msgInternalError)
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

			if !i18n.HasStringPlaceholdersOnly(text) {
				return message, false
			}
			args := strings.Split(message[idx+1:], "|")
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
