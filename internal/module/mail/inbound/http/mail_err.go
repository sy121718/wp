package mailhttp

import (
	"errors"
	"strings"

	"github.com/gin-gonic/gin"

	"go_wp/internal/adminui"
	"go_wp/internal/middleware/builtin"
	mailenums "go_wp/internal/module/mail/enums"
	"go_wp/pkg/i18n"
	"go_wp/pkg/logger"
	"go_wp/pkg/mailer"
	"go_wp/pkg/reqparams"
	"go_wp/pkg/response"
)

const mailErrScene = "mail"

func mailErrMsg(c *gin.Context, err error) mailMsg {
	if err == nil {
		return mailMsg{}
	}
	fact := adminui.PageErrorFact(c, mailErrScene, err)
	if !fact.Business {
		return mailInternalMsg()
	}
	if fact.Tail != "" {

		if detail := mailDetailText(mailTranslate(c), fact.Tail); detail != "" {
			return mailMsg{Fallback: response.ArgsText(c, fact.Key, fact.Fallback, nil) + "：" + detail}
		}
	}
	return mailMsg{Key: fact.Key, Fallback: fact.Fallback}
}

func mailErrPageText(c *gin.Context, err error) string {
	return response.MsgText(c, mailErrMsg(c, err))
}

func mailInternalMsg() mailMsg {
	return mailMsg{Key: mailenums.ErrInternal, Fallback: mailInternalFallback}
}

const mailInternalFallback = "操作失败，请稍后重试"

func mailDetailText(tr func(key, fallback string) string, tail string) string {
	parts, ok := i18n.ParseErrorDetails(tail)
	if !ok {
		logger.Scene(mailErrScene).With("tail", i18n.DetailTailForLog(tail)).
			Warn("业务错误的补充说明不是登记的词条形态，已丢弃（不再原样透出）")
		return ""
	}
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		fallback, registered := mailenums.ErrDetailFallbacks[p.Key]
		if !registered || fallback == "" {
			logger.Scene(mailErrScene).With("detail_key", p.Key).
				Warn("业务错误的补充说明词条未登记，已省略该段")
			continue
		}
		out = append(out, i18n.FillTranslate(tr, p.Key, fallback, p.Args))
	}
	return strings.Join(out, "；")
}

func mailBulkIDsText(c *gin.Context, err error) string {
	return adminui.BulkIDsFacingText(c, err)
}

func mailVisitorText(c *gin.Context, key, fallback string) string {
	return mailTranslate(c)(key, fallback)
}

func mailVisitorLog(c *gin.Context, err error, msg string) {
	if err == nil {
		return
	}
	logger.Scene(mailErrScene).
		With("path", c.Request.URL.Path).
		With("detail", err.Error()).
		Warn(msg)
}

func mailVisitorLogError(c *gin.Context, err error, msg string) {
	if err == nil {
		return
	}
	logger.Scene(mailErrScene).
		With("path", c.Request.URL.Path).
		Error(err, msg)
}

var (
	mailCampaignIDRequiredMsg = mailMsg{Key: mailenums.NoticeCampaignIDRequired, Fallback: "请先从活动列表选择一条活动，再查看它的报表。"}
	mailRunIDRequiredMsg      = mailMsg{Key: mailenums.NoticeRunIDRequired, Fallback: "请先从实例列表选择一条实例，再查看它的排障详情。"}
)

func mailTestSendFailedLabel(kind string) mailenums.LabelPair {
	switch strings.TrimSpace(kind) {
	case string(mailer.KindTemporary):
		return mailenums.TestSendFailedTemporary
	case string(mailer.KindPermanent):
		return mailenums.TestSendFailedPermanent
	case string(mailer.KindConfiguration):
		return mailenums.TestSendFailedConfiguration
	default:
		return mailenums.TestSendFailedUnknown
	}
}

func mailTestSendFailedMsg(c *gin.Context, kind, detail string) mailMsg {
	if strings.TrimSpace(detail) != "" {
		logger.Scene(mailErrScene).
			With("user_id", builtin.CurrentUserID(c)).
			With("path", c.Request.URL.Path).
			With("kind", kind).
			Error(errors.New(detail), "测试邮件发送失败")
	}
	return mailLabelMsg(mailTestSendFailedLabel(kind))
}

func mailQueryID(c *gin.Context) (uint64, bool) {
	raw := strings.TrimSpace(c.Query("id"))
	if raw == "" {
		return 0, false
	}
	return reqparams.ParseUint(raw), true
}

func mailAutomationStatusNotice(c *gin.Context, status string) string {
	tr := mailTranslate(c)
	return i18n.FillTranslate(tr,
		mailenums.AutomationStatusChanged, mailenums.AutomationStatusChangedFallback,
		map[string]string{"status": mailLabel(tr, mailenums.AutomationStatusLabel(status))})
}
