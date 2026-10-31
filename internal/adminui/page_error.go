package adminui

import (
	"github.com/gin-gonic/gin"

	"go_wp/internal/text"
	"go_wp/pkg/facing"
	"go_wp/pkg/response"
)

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

func BusinessErrFact(err error) (ErrFact, bool) {
	return facing.FactOf(err)
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

func TranslateArgs(c *gin.Context, key, fallback string, args []string) string {
	return response.ArgsText(c, key, fallback, args)
}

func TranslateFill(c *gin.Context, key, fallback string, params map[string]string) string {
	return response.FillText(c, key, fallback, params)
}

func toAnyStrings(in []string) []any {
	out := make([]any, len(in))
	for i, s := range in {
		out[i] = s
	}
	return out
}
