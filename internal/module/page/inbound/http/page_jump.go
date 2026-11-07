package pagehttp

import (
	"go_wp/pkg/backpath"
	"go_wp/pkg/facing"
	"go_wp/pkg/view/jet"
	"strings"

	"github.com/gin-gonic/gin"
	pageenums "go_wp/internal/module/page/enums"
	"go_wp/pkg/i18n"
	"go_wp/pkg/logger"
	"go_wp/pkg/response"
	view "go_wp/pkg/view"
)

var pageListBackKeys = []string{"project"}

const pageListPath = "/admin/pages"

func pageListBack(c *gin.Context) string {
	return backpath.FromRequest(c.Request.URL.Query(), pageListPath, "/admin", "page", pageListBackKeys...)
}

var pageListBackMsg = pageMsg{Key: pageenums.MsgPagesTitle, Fallback: "页面管理"}

type pageMsg = facing.Msg

func pageMsgText(c *gin.Context, m pageMsg) string {
	return response.MsgText(c, m)
}

const pageErrScene = "page"

func pageErrMsg(c *gin.Context, err error) pageMsg {
	return pageErrMsgOf(c, view.PageErrorFact(c, pageErrScene, err))
}

func pageErrMsgQuiet(c *gin.Context, err error) pageMsg {
	return pageErrMsgOf(c, view.PageErrorFactQuiet(c, pageErrScene, err))
}

func pageErrMsgOf(c *gin.Context, f view.ErrFact) pageMsg {
	if f.Key == "" {
		return pageMsg{}
	}
	if f.Tail != "" {
		if detail := pageDetailText(jet.TranslateFunc(jet.Lang(c)), f.Tail); detail != "" {

			return pageMsg{Fallback: response.ArgsText(c, f.Key, f.Fallback, nil) + "：" + detail}
		}
	}
	return pageMsg{Key: f.Key, Fallback: f.Fallback}
}

func pageErrText(c *gin.Context, err error) string {
	return pageMsgText(c, pageErrMsg(c, err))
}

func pageDetailText(tr func(key, fallback string) string, tail string) string {
	parts, ok := i18n.ParseErrorDetails(tail)
	if !ok {
		logger.Scene(pageErrScene).With("tail", i18n.DetailTailForLog(tail)).
			Warn("业务错误的补充说明不是登记的词条形态，已丢弃（不再原样透出）")
		return ""
	}
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		fallback, registered := pageenums.ErrDetailFallbacks[p.Key]
		if !registered || fallback == "" {
			logger.Scene(pageErrScene).With("detail_key", p.Key).
				Warn("业务错误的补充说明词条未登记，已省略该段")
			continue
		}
		out = append(out, i18n.FillTranslate(tr, p.Key, fallback, p.Args))
	}
	return strings.Join(out, "；")
}

// pageJump 只交结果事实：成功走统一提示组件，失败走共享失败出口。
// 回跳由发起页面声明，本层不再构造整页跳转提示。
func pageJump(c *gin.Context, ok bool, msg pageMsg, _ string, _ pageMsg) {
	if ok {
		view.SuccessWith(c, view.SuccessFact{Key: msg.Key, Fallback: msg.Fallback, Args: msg.Args})
		return
	}
	view.FailMessage(c, pageErrScene, msg.Key, msg.Fallback, msg.Args...)
}

func pageListJump(c *gin.Context, ok bool, msg pageMsg) {
	pageJump(c, ok, msg, pageListBack(c), pageListBackMsg)
}

func outcomeMsg(o pageenums.Outcome) pageMsg {
	return pageMsg{Key: o.Key, Fallback: o.Fallback, Args: o.Args}
}

func pageInternalMsg() pageMsg {
	f := view.InternalErrorFact()
	return pageMsg{Key: f.Key, Fallback: f.Fallback}
}
