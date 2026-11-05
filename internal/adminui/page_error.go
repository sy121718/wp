package adminui

import (
	"github.com/gin-gonic/gin"

	"go_wp/pkg/facing"
	"go_wp/pkg/response"
	text "go_wp/pkg/view/jet"
)

const MsgInternalError = text.InternalKey

const internalErrorFallback = response.InternalFallback

type ErrFact = facing.Fact

func PageErrorFact(c *gin.Context, scene string, err error) ErrFact {
	return text.FactOf(c, scene, err)
}

func PageErrorFactQuiet(_ *gin.Context, _ string, err error) ErrFact {
	if err == nil {
		return ErrFact{}
	}
	if fact, ok := facing.FactOf(err); ok {
		return fact
	}
	return ErrFact{Key: MsgInternalError, Fallback: internalErrorFallback}
}

func ErrorFactText(c *gin.Context, fact ErrFact) string {
	if fact.Key == "" {
		return ""
	}
	return translateFor(c)(fact.Key, fact.Fallback)
}

func PageErrorText(c *gin.Context, scene string, err error) string {
	return ErrorFactText(c, PageErrorFact(c, scene, err))
}

func PageErrorTextQuiet(c *gin.Context, scene string, err error) string {
	return ErrorFactText(c, PageErrorFactQuiet(c, scene, err))
}

func InternalErrorFact() ErrFact {
	return ErrFact{Key: MsgInternalError, Fallback: internalErrorFallback}
}

func toAnyStrings(in []string) []any {
	out := make([]any, len(in))
	for i, s := range in {
		out[i] = s
	}
	return out
}

// InternalMsg 是归口错误消息事实（key + 兜底），供页面侧拼装受控提示。
func InternalMsg() facing.Msg {
	return facing.Msg{Key: MsgInternalError, Fallback: internalErrorFallback}
}
