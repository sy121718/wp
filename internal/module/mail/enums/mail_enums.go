package mailenums

import "go_wp/pkg/facing"

const (
	MsgSendSuccess       = "mail.msg.sendSuccess"
	MsgSaveSuccess       = "mail.msg.saveSuccess"
	MsgDeleteSuccess     = "mail.msg.deleteSuccess"
	MsgImportSuccess     = "mail.msg.importSuccess"
	MsgTestSent          = "mail.msg.testSent"
	MsgCampaignStarted   = "mail.msg.campaignStarted"
	MsgAutomationStarted = "mail.msg.automationStarted"
)

const (
	ErrInvalidParam       = "mail.err.invalidParam"
	ErrAccountNotFound    = "mail.err.accountNotFound"
	ErrTemplateNotFound   = "mail.err.templateNotFound"
	ErrContactNotFound    = "mail.err.contactNotFound"
	ErrCampaignNotFound   = "mail.err.campaignNotFound"
	ErrAccountDisabled    = "mail.err.accountDisabled"
	ErrAccountIncomplete  = "mail.err.accountIncomplete"
	ErrSuppressed         = "mail.err.suppressed"
	ErrEmailRequired      = "mail.err.emailRequired"
	ErrEmailInvalid       = "mail.err.emailInvalid"
	ErrContactEmailExists = "mail.err.contactEmailExists"
	ErrContactTagEmpty    = "mail.err.contactTagEmpty"

	ErrConsentSourceRequired    = "mail.err.consentSourceRequired"
	ErrImportEmpty              = "mail.err.importEmpty"
	ErrImportTooLarge           = "mail.err.importTooLarge"
	ErrCampaignNotDraft         = "mail.err.campaignNotDraft"
	ErrCampaignNoRecipient      = "mail.err.campaignNoRecipient"
	ErrCipherSecretMissing      = "mail.err.cipherSecretMissing"
	ErrCipherUnavailable        = "mail.err.cipherUnavailable"
	ErrTemplateSyntax           = "mail.err.templateSyntax"
	ErrCampaignSending          = "mail.err.campaignSending"
	ErrAutomationNotFound       = "mail.err.automationNotFound"
	ErrAutomationTriggerInvalid = "mail.err.automationTriggerInvalid"
	ErrAutomationGraphInvalid   = "mail.err.automationGraphInvalid"
	ErrAutomationRunExists      = "mail.err.automationRunExists"
	ErrAutomationRunNotFound    = "mail.err.automationRunNotFound"

	ErrTrackLinkInvalid       = "mail.err.trackLinkInvalid"
	ErrUnsubscribeLinkInvalid = "mail.err.unsubscribeLinkInvalid"
	MsgUnsubscribeDoneTitle   = "mail.msg.unsubscribeDoneTitle"
	MsgUnsubscribeDoneBody    = "mail.msg.unsubscribeDoneBody"
	MsgUnsubscribeDoneNote    = "mail.msg.unsubscribeDoneNote"

	ErrInternal = "mail.err.internal"
)

const (
	NoticeBulkAllDone    = "mail.bulk.allDone"
	NoticeBulkAllSkipped = "mail.bulk.allSkipped"
	NoticeBulkPartial    = "mail.bulk.partial"

	NoticeAutomationTick = "mail.msg.automationTick"

	NoticeTemplateListFailed = "mail.page.templateListFailed"

	NoticeContactStatusBad = "mail.page.contactStatusBad"

	NoticeCampaignIDRequired = "mail.page.campaignIDRequired"
	NoticeRunIDRequired      = "mail.page.runIDRequired"
)

const (
	TestMailSubject = "mail.test.mailSubject"
	TestMailText    = "mail.test.mailText"
)

var errFallbacks = map[string]string{
	ErrInvalidParam:             "参数不合法",
	ErrAccountNotFound:          "发信账号不存在",
	ErrTemplateNotFound:         "邮件模板不存在",
	ErrContactNotFound:          "联系人不存在",
	ErrCampaignNotFound:         "活动不存在",
	ErrAccountDisabled:          "发信账号已停用",
	ErrAccountIncomplete:        "发信账号配置不完整（缺少主机 / 端口 / 发件人）",
	ErrSuppressed:               "该地址在抑制名单中，不允许发送",
	ErrEmailRequired:            "邮箱不能为空",
	ErrEmailInvalid:             "邮箱格式不正确",
	ErrContactEmailExists:       "该邮箱已存在：同一个人不用建两条；若只是换了地址，请直接编辑已有联系人。",
	ErrContactTagEmpty:          "请至少填一个要加上或去掉的标签。",
	ErrConsentSourceRequired:    "置为「已订阅」必须填写同意来源：谁在什么时候声明过同意，是这条名单的合规依据。",
	ErrImportEmpty:              "导入内容为空",
	ErrImportTooLarge:           "导入内容过大",
	ErrCampaignNotDraft:         "只有草稿状态的活动可以修改",
	ErrCampaignNoRecipient:      "投递目标为 0 人，无法发送",
	ErrCipherSecretMissing:      "未配置敏感数据加密密钥（config.yaml 的 app.secret），无法保存邮箱密码",
	ErrCipherUnavailable:        "邮箱密码无法解密：加密密钥可能已变更，请重新填写密码",
	ErrTemplateSyntax:           "模板语法错误",
	ErrCampaignSending:          "活动正在发送中，无法删除",
	ErrAutomationNotFound:       "自动化流程不存在",
	ErrAutomationTriggerInvalid: "触发方式不合法",
	ErrAutomationGraphInvalid:   "流程定义不合法",
	ErrAutomationRunExists:      "该联系人已在此流程中",
	ErrAutomationRunNotFound:    "自动化实例不存在",
	ErrTrackLinkInvalid:         "链接无效或已过期",
	ErrUnsubscribeLinkInvalid:   "退订链接无效或已过期",
	ErrInternal:                 "操作失败，请稍后重试",
}

func Err(key string) error { return facing.New(key, errFallbacks[key]) }

func FallbackFor(key string) string { return errFallbacks[key] }
