package mailenums

// mail_enums.go — 邮箱模块的响应消息与业务错误（模块内统一出口）。

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
	ErrInvalidParam             = "mail.err.invalidParam"
	ErrAccountNotFound          = "mail.err.accountNotFound"
	ErrTemplateNotFound         = "mail.err.templateNotFound"
	ErrContactNotFound          = "mail.err.contactNotFound"
	ErrCampaignNotFound         = "mail.err.campaignNotFound"
	ErrAccountDisabled          = "mail.err.accountDisabled"
	ErrAccountIncomplete        = "mail.err.accountIncomplete"
	ErrSuppressed               = "mail.err.suppressed"
	ErrEmailRequired            = "mail.err.emailRequired"
	ErrEmailInvalid             = "mail.err.emailInvalid"
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
	// ErrInternal 未归类的系统错误对外统一文案（页面路径的归口出口）。
	//
	// 存在的理由：基础设施错误（数据库 / SMTP 客户端）的原文可能带表名、列名甚至 SQL 片段，
	// 也可能带 SMTP 主机名 —— 那是给运维看的，不是给运营看的。页面归口助手
	//（inbound/http/mail_err.go 的 mailErrPageText）把「不是本模块业务文案」的错误
	// 全部落到这一条，原文只进日志。
	ErrInternal = "mail.err.internal"
)

// MailFacingMessages 可以原样展示给运营 / 前端的邮箱业务文案（**白名单**）。
//
// 方向是安全的：漏写一条只会让页面显示一句通用提示（一眼可见，且
// mail_enums_test.go 会按本包源文件逐个常量对账），而黑名单漏写会把 service 上抛的
// PostgreSQL / SMTP 原文摆到页面上（不易发现）。
//
// 命中的文案一律按 i18n key 翻译后再展示 —— 本模块 enums 的值就是 key
// （"mail.err.accountNotFound" 对应「发信账号不存在」），此前页面直接拼 err.Error()
// 的结果是运营看到一串 key。
//
// ErrInternal 本身不进白名单：它是未命中时的返回值，不是业务文案。
var MailFacingMessages = []string{
	// 成功回执（response.Success 的 message，也可能经 ?ok= 回显）
	MsgSendSuccess, MsgSaveSuccess, MsgDeleteSuccess, MsgImportSuccess,
	MsgTestSent, MsgCampaignStarted, MsgAutomationStarted,
	// 业务错误
	ErrInvalidParam, ErrAccountNotFound, ErrTemplateNotFound, ErrContactNotFound,
	ErrCampaignNotFound, ErrAccountDisabled, ErrAccountIncomplete, ErrSuppressed,
	ErrEmailRequired, ErrEmailInvalid, ErrImportEmpty, ErrImportTooLarge,
	ErrCampaignNotDraft, ErrCampaignNoRecipient, ErrCipherSecretMissing,
	ErrCipherUnavailable, ErrTemplateSyntax, ErrCampaignSending,
	ErrAutomationNotFound, ErrAutomationTriggerInvalid, ErrAutomationGraphInvalid,
	ErrAutomationRunExists, ErrAutomationRunNotFound,
}

// 测试邮件内容（后台「测试发送」触发，用于验证 SMTP 配置）。
const (
	TestMailSubject = "mail.test.mailSubject"
	TestMailHTML    = `<div style="font-family:system-ui,sans-serif;line-height:1.6"><h2>邮件配置连通性测试</h2><p>如果你看到这封邮件，说明发信账号的配置可用：</p><ul><li>SMTP 连接与认证通过</li><li>中文主题编码正常</li><li>HTML 与纯文本正文正常</li></ul><p style="color:#888;font-size:13px">本邮件由后台「测试发送」触发。</p></div>`
	TestMailText    = "mail.test.mailText"
)
