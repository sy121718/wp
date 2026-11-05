package pagehttp

import (
	"go_wp/internal/shell"
	"go_wp/internal/backref"
	"go_wp/internal/text"
	"strings"

	"github.com/gin-gonic/gin"

	"go_wp/internal/adminui"
	pageenums "go_wp/internal/module/page/enums"
	"go_wp/pkg/response"
	"go_wp/pkg/i18n"
	"go_wp/pkg/logger"
)

var pageListBackKeys = []string{"project"}

const pageListPath = "/admin/pages"

func pageListBack(c *gin.Context) string {
	return backref.BackPath(c, pageListPath, pageListBackKeys...)
}

var pageListBackMsg = pageMsg{Key: pageenums.MsgPagesTitle, Fallback: "页面管理"}

type pageMsg = adminui.Msg

func pageMsgText(c *gin.Context, m pageMsg) string {
	return adminui.MsgText(c, m)
}

const pageErrScene = "page"

func pageErrMsg(c *gin.Context, err error) pageMsg {
	return pageErrMsgOf(c, adminui.PageErrorFact(c, pageErrScene, err))
}

func pageErrMsgQuiet(c *gin.Context, err error) pageMsg {
	return pageErrMsgOf(c, adminui.PageErrorFactQuiet(c, pageErrScene, err))
}

func pageErrMsgOf(c *gin.Context, f adminui.ErrFact) pageMsg {
	if f.Key == "" {
		return pageMsg{}
	}
	if f.Tail != "" {
		if detail := pageDetailText(text.TranslateFunc(c), f.Tail); detail != "" {

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

func pageJump(c *gin.Context, ok bool, msg pageMsg, back string, backText pageMsg) {
	seconds := 0
	if ok {
		seconds = 1
	}
	shell.RenderJump(c, shell.Jump{
		OK:          ok,
		MsgKey:      msg.Key,
		Msg:         msg.Fallback,
		MsgArgs:     msg.Args,
		Back:        back,
		BackTextKey: backText.Key,
		BackText:    backText.Fallback,
		Seconds:     seconds,
	})
}

func pageListJump(c *gin.Context, ok bool, msg pageMsg) {
	pageJump(c, ok, msg, pageListBack(c), pageListBackMsg)
}

func outcomeMsg(o pageenums.Outcome) pageMsg {
	return pageMsg{Key: o.Key, Fallback: o.Fallback, Args: o.Args}
}

func pageInternalMsg() pageMsg {
	f := adminui.InternalErrorFact()
	return pageMsg{Key: f.Key, Fallback: f.Fallback}
}
