package mailhttp

import (
	"errors"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"go_wp/internal/adminui"
	"go_wp/internal/middleware/builtin"
	mailenums "go_wp/internal/module/mail/enums"
	"go_wp/pkg/bulkids"
	"go_wp/pkg/i18n"
	"go_wp/pkg/logger"
	"go_wp/pkg/mailer"
	"go_wp/pkg/reqparams"
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
			return mailMsg{Fallback: adminui.TranslateArgs(c, fact.Key, fact.Fallback, nil) + "：" + detail}
		}
	}
	return mailMsg{Key: fact.Key, Fallback: fact.Fallback}
}

func mailErrPageText(c *gin.Context, err error) string {
	return adminui.MsgText(c, mailErrMsg(c, err))
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

func mailBulkIDsMsg(c *gin.Context, err error) mailMsg {
	var tooMany *bulkids.IDsError
	if !errors.As(err, &tooMany) {

		logger.Scene(mailErrScene).With("path", c.Request.URL.Path).
			Error(err, "批量 id 读取失败：未命中受控错误，只对外给归口文案")
		return mailInternalMsg()
	}
	logger.Scene(mailErrScene).With("path", c.Request.URL.Path).
		With("count", tooMany.Count).With("max", tooMany.Max).
		Warn("批量操作被拒：提交的 id 数量超过上限")
	return mailMsg{Key: bulkids.MsgTooMany, Fallback: bulkids.TooManyTemplate}.
		WithArgs(strconv.Itoa(tooMany.Max), strconv.Itoa(tooMany.Count))
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
	mailBulkAllDoneMsg    = mailMsg{Key: mailenums.NoticeBulkAllDone, Fallback: "已%s %s 个%s。"}
	mailBulkAllSkippedMsg = mailMsg{Key: mailenums.NoticeBulkAllSkipped, Fallback: "0 个%s被%s：%s 个被跳过（不存在或被服务端拒绝）。"}
	mailBulkPartialMsg    = mailMsg{Key: mailenums.NoticeBulkPartial, Fallback: "已%s %s 个%s，另有 %s 个被跳过（不存在或被服务端拒绝）。"}

	mailAutomationTickMsg  = mailMsg{Key: mailenums.NoticeAutomationTick, Fallback: "已补投 %s 个到点实例"}
	mailContactImportMsg   = mailMsg{Key: mailenums.MsgImportSuccess, Fallback: "导入完成：新增 %s，更新 %s，跳过 %s，抑制名单命中 %s，非法 %s"}
	mailCampaignStartedMsg = mailMsg{Key: mailenums.MsgCampaignStarted, Fallback: "活动已开始发送，目标 %s 人；进度可在下方列表刷新查看。"}

	mailAutomationDeletedMsg = mailMsg{Key: mailenums.MsgDeleteSuccess, Fallback: "已删除"}

	mailTemplateListFailedMsg = mailMsg{Key: mailenums.NoticeTemplateListFailed, Fallback: "读取模板列表失败，本次没有删除任何模板。"}
	mailContactStatusBadMsg   = mailMsg{Key: mailenums.NoticeContactStatusBad, Fallback: "目标状态不合法，本次没有处理任何联系人。"}

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
