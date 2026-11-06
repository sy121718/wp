package navigationhttp

import (
	"go_wp/internal/middleware/builtin"
	"strings"

	navigationenums "go_wp/internal/module/navigation/enums"
	"go_wp/pkg/facing"
	"go_wp/pkg/logger"
	"go_wp/pkg/response"

	"github.com/gin-gonic/gin"
)

const navigationErrScene = "navigation"

const navigationInternalKey = navigationenums.InternalKey

const navigationInternalFallback = "操作失败，请稍后重试（细节只进日志）"

func navigationDetailSep(c *gin.Context) string {
	if strings.HasPrefix(response.RequestLanguage(c), "zh") {
		return "："
	}
	return ": "
}

func navigationFact(err error) (facing.Fact, bool) {
	fact, ok := facing.FactOf(err)
	if !ok || !fact.Business {
		return facing.Fact{}, false
	}

	if fact.Fallback == "" || fact.Fallback == fact.Key {
		key, _, _ := strings.Cut(fact.Key, "|")
		if text := navigationReasonFallback(key); text != "" {
			fact.Fallback = text
		}
	}
	return fact, true
}

func navigationErrText(c *gin.Context, err error) string {
	fact, business := navigationFact(err)
	if !business {
		logger.Scene(navigationErrScene).
			With("user_id", builtin.CurrentUserID(c)).
			Error(err, "navigation 接口内部错误")
		return navigationInternalKey
	}

	if fact.Tail != "" {
		return fact.Key + ": " + fact.Tail
	}
	return fact.Key
}

func navigationErrMsg(c *gin.Context, err error) facing.Msg {
	fact, business := navigationFact(err)
	if !business {
		return facing.Msg{Key: navigationInternalKey, Fallback: navigationInternalFallback}
	}
	return navigationFactMsg(c, fact)
}

func navigationFactMsg(c *gin.Context, fact facing.Fact) facing.Msg {
	if key, param, hasParam := strings.Cut(fact.Key, "|"); hasParam {
		return facing.Msg{Key: key, Fallback: fact.Fallback}.WithArgs(param)
	}
	if fact.Tail != "" {
		return facing.Msg{
			Fallback: response.ArgsText(c, fact.Key, fact.Fallback, nil) +
				navigationDetailSep(c) + fact.Tail,
		}
	}
	return facing.Msg{Key: fact.Key, Fallback: fact.Fallback}
}

var navigationReasonFallbacks = map[string]string{
	navigationenums.NoPublicPath: "（暂无公开路径，先发布后再添加）",
}

func navigationReasonFallback(key string) string {
	return navigationReasonFallbacks[key]
}

func navTr(c *gin.Context) func(key, fallback string) string {
	return func(key, fallback string) string { return response.ArgsText(c, key, fallback, nil) }
}

var navNoticeItemMissing = navText{Key: navigationenums.ErrNotFound, Fallback: "菜单项不存在"}

var navInvalidParamMsg = navText{Key: navigationenums.ErrInvalidParam, Fallback: "参数错误"}

type navText = facing.Msg
