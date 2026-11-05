package mailhttp

import (
	"go_wp/pkg/facing"
	view "go_wp/pkg/view"
	"net/http"
	"net/url"
	"strings"

	"github.com/gin-gonic/gin"

	"strconv"

	mailenums "go_wp/internal/module/mail/enums"
	"go_wp/pkg/backpath"
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
	return backpath.FromRequest(c.Request.URL.Query(), mailContactsPath, "/admin", "mail", mailContactsBackKeys...)
}

func mailCampaignsBack(c *gin.Context) string {
	return backpath.FromRequest(c.Request.URL.Query(), mailCampaignsPath, "/admin", "mail", mailCampaignsBackKeys...)
}

func mailAutomationBack(c *gin.Context) string {
	return backpath.FromRequest(c.Request.URL.Query(), mailAutomationPath, "/admin", "mail", mailAutomationBackKeys...)
}

func mailAutomationRunsBack(c *gin.Context) string {
	return backpath.FromRequest(c.Request.URL.Query(), mailAutomationRunsPath, "/admin", "mail", mailAutomationRunsBackKeys...)
}

type mailMsg = facing.Msg

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

func mailJump(c *gin.Context, ok bool, msg mailMsg, _ string, _ mailMsg) {
	if ok {
		view.SuccessWith(c, view.SuccessFact{Key: msg.Key, Fallback: msg.Fallback, Args: msg.Args})
		return
	}
	view.FailMessage(c, mailErrScene, msg.Key, msg.Fallback, msg.Args...)
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
