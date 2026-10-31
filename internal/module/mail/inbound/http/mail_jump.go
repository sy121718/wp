package mailhttp

import (
	"net/http"
	"net/url"
	"strings"

	"github.com/gin-gonic/gin"

	"strconv"

	"go_wp/internal/adminui"
	"go_wp/internal/backref"
	mailenums "go_wp/internal/module/mail/enums"
	"go_wp/internal/shell"
	"go_wp/pkg/reqparams"
)

const (
	mailAccountsPath       = "/admin/mail"
	mailTemplatesPath      = "/admin/mail/templates"
	mailCampaignsPath      = "/admin/mail/campaigns"
	mailAutomationPath     = "/admin/mail/automation"
	mailAutomationRunsPath = "/admin/mail/automation/runs"
)

var (
	mailContactsBackKeys       = []string{"keyword", "status", "tags", "page"}
	mailCampaignsBackKeys      = []string{"page"}
	mailAutomationBackKeys     = []string{"page"}
	mailAutomationRunsBackKeys = []string{"automationId", "runStatus", "page"}
)

func mailListQuery(params map[string]string) string {
	q := url.Values{}
	for k, v := range params {
		if strings.TrimSpace(v) != "" {
			q.Set(k, v)
		}
	}
	if len(q) == 0 {
		return ""
	}
	return "?" + q.Encode()
}

func mailPageQuery(page int) string {
	if page <= 1 {
		return ""
	}
	return mailListQuery(map[string]string{"page": strconv.Itoa(page)})
}

func mailAutomationRunsListQuery(automationID, runStatus string, page int) string {
	params := map[string]string{"automationId": automationID, "runStatus": runStatus}
	if page > 1 {
		params["page"] = strconv.Itoa(page)
	}
	return mailListQuery(params)
}

func mailTemplatesBack(c *gin.Context) string {
	return mailTemplatesPath
}

func mailContactsBack(c *gin.Context) string {
	return backref.BackPath(c, mailContactsPath, mailContactsBackKeys...)
}

func mailCampaignsBack(c *gin.Context) string {
	return backref.BackPath(c, mailCampaignsPath, mailCampaignsBackKeys...)
}

func mailAutomationBack(c *gin.Context) string {
	return backref.BackPath(c, mailAutomationPath, mailAutomationBackKeys...)
}

func mailAutomationRunsBack(c *gin.Context) string {
	return backref.BackPath(c, mailAutomationRunsPath, mailAutomationRunsBackKeys...)
}

type mailMsg = adminui.Msg

func mailLabelMsg(pair mailenums.LabelPair) mailMsg {
	return mailMsg{Key: pair.Key, Fallback: pair.Fallback}
}

var (
	mailAccountsBackMsg       = mailLabelMsg(mailenums.PageTitleAccounts)
	mailTemplatesBackMsg      = mailLabelMsg(mailenums.PageTitleTemplates)
	mailContactsBackMsg       = mailLabelMsg(mailenums.PageTitleContacts)
	mailCampaignsBackMsg      = mailLabelMsg(mailenums.PageTitleCampaigns)
	mailAutomationBackMsg     = mailLabelMsg(mailenums.PageTitleAutomation)
	mailAutomationRunsBackMsg = mailLabelMsg(mailenums.PageTitleAutomationRuns)
)

func mailJump(c *gin.Context, ok bool, msg mailMsg, back string, backText mailMsg) {
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

func mailAccountsJump(c *gin.Context, ok bool, msg mailMsg) {
	mailJump(c, ok, msg, mailAccountsPath, mailAccountsBackMsg)
}

func mailTemplatesJump(c *gin.Context, ok bool, msg mailMsg) {
	mailJump(c, ok, msg, mailTemplatesBack(c), mailTemplatesBackMsg)
}

func mailContactsJump(c *gin.Context, ok bool, msg mailMsg) {
	mailJump(c, ok, msg, mailContactsBack(c), mailContactsBackMsg)
}

func mailCampaignsJump(c *gin.Context, ok bool, msg mailMsg) {
	mailJump(c, ok, msg, mailCampaignsBack(c), mailCampaignsBackMsg)
}

func mailAutomationJump(c *gin.Context, ok bool, msg mailMsg) {
	mailJump(c, ok, msg, mailAutomationBack(c), mailAutomationBackMsg)
}

func mailAutomationRunsJump(c *gin.Context, ok bool, msg mailMsg) {
	mailJump(c, ok, msg, mailAutomationRunsBack(c), mailAutomationRunsBackMsg)
}

func mailRedirect(c *gin.Context, target string) {
	if reqparams.IsHXRequest(c) {
		c.Header("HX-Redirect", target)
		c.Status(http.StatusOK)
		return
	}
	c.Redirect(http.StatusFound, target)
}

func mailBulkJump(c *gin.Context, done, warn mailMsg, back string, backText mailMsg) {
	switch {
	case !warn.Empty():
		mailJump(c, false, warn, back, backText)
	case !done.Empty():
		mailJump(c, true, done, back, backText)
	default:
		mailRedirect(c, back)
	}
}

var mailDoneMsg = mailMsg{Key: mailenums.MsgSaveSuccess, Fallback: "保存成功"}
