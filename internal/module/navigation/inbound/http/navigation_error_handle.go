package navigationhttp

import (
	"go_wp/internal/middleware/builtin"
	"strings"

	"go_wp/internal/adminui"
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

func navigationErrMsg(c *gin.Context, err error) adminui.Msg {
	fact, business := navigationFact(err)
	if !business {
		return adminui.Msg{Key: navigationInternalKey, Fallback: navigationInternalFallback}
	}
	return navigationFactMsg(c, fact)
}

func navigationFactMsg(c *gin.Context, fact facing.Fact) adminui.Msg {
	if key, param, hasParam := strings.Cut(fact.Key, "|"); hasParam {
		return adminui.Msg{Key: key, Fallback: fact.Fallback}.WithArgs(param)
	}
	if fact.Tail != "" {
		return adminui.Msg{
			Fallback: adminui.TranslateArgs(c, fact.Key, fact.Fallback, nil) +
				navigationDetailSep(c) + fact.Tail,
		}
	}
	return adminui.Msg{Key: fact.Key, Fallback: fact.Fallback}
}

var navigationReasonFallbacks = map[string]string{
	navigationAddSourceNoPathKey: "（暂无公开路径，先发布后再添加）",
}

func navigationReasonFallback(key string) string {
	if text := navigationenums.FallbackFor(key); text != "" {
		return text
	}
	return navigationReasonFallbacks[key]
}

func navigationReasonText(c *gin.Context, err error) string {
	fact, business := navigationFact(err)
	if !business {
		logger.Scene(navigationErrScene).
			With("user_id", builtin.CurrentUserID(c)).
			Error(err, "navigation 接口内部错误")

		if text := response.TranslateMessage(c, navigationInternalKey); text != navigationInternalKey {
			return text
		}
		return navigationInternalFallback
	}
	return navigationFactReasonText(c, fact)
}

func navigationFactReasonText(c *gin.Context, fact facing.Fact) string {
	if fact.Tail == "" {
		return navigationReasonKeyText(c, fact.Key)
	}
	text := response.TranslateMessage(c, fact.Key)
	if text == fact.Key {
		if fallback := navigationReasonFallback(fact.Key); fallback != "" {
			text = fallback
		}
	}
	return text + ": " + fact.Tail
}

func navigationReasonKeyText(c *gin.Context, msg string) string {
	if text := response.TranslateMessage(c, msg); text != msg {
		return text
	}
	return navigationReasonFallbackText(msg)
}

func navigationReasonFallbackText(msg string) string {
	key := msg
	if k, _, ok := strings.Cut(msg, "|"); ok {

		key = k
	}
	if text := navigationReasonFallback(key); text != "" {
		return text
	}
	return navigationInternalFallback
}

func navTr(c *gin.Context) func(key, fallback string) string {
	return func(key, fallback string) string { return adminui.TranslateArgs(c, key, fallback, nil) }
}

var navNoticeItemMissing = navText{Key: navigationenums.ErrNotFound, Fallback: "菜单项不存在"}

var navInvalidParamMsg = navText{Key: navigationenums.ErrInvalidParam, Fallback: "参数错误"}

var navNoticeNoSourcePicked = navText{
	Key: navigationenums.NoticeNoSourcePicked, Fallback: "请至少勾选一项要加入菜单的内容。",
}

type navText = adminui.Msg
