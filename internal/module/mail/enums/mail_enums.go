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
	// —— 访客面：邮件里的公开链接（跟踪 / 退订，见 inbound/http/mail_tracking.go）——
	//
	// 这五条服务的**不是**后台运营，而是收件人：他们在邮件客户端里点开链接，浏览器直接打开
	// /_t/c/{token}（点击追踪）或 /_t/u/{token}（一键退订）—— 无登录态、无后台页面壳。
	// 所以文案此前是 Go 里的硬编码中文，英文收件人（Accept-Language: en）打开只能看到中文。
	// 现在按请求语言取词条（response.RequestLanguage 的协商链，不需要登录态）。
	//
	// 形态与出参不变：失败仍是**一句受控短句**（纯文本 400），成功仍是**自带样式的整页 HTML**——
	// 访客没有可回归的列表页，也没有解析 JSON 的客户端，303 + ?err= 在这里是错的方向。
	ErrTrackLinkInvalid       = "mail.err.trackLinkInvalid"       // 点击追踪链接无效或已过期
	ErrUnsubscribeLinkInvalid = "mail.err.unsubscribeLinkInvalid" // 退订链接无效或已过期
	MsgUnsubscribeDoneTitle   = "mail.msg.unsubscribeDoneTitle"   // 退订成功页标题
	MsgUnsubscribeDoneBody    = "mail.msg.unsubscribeDoneBody"    // 退订成功页正文（%s = 收件人邮箱，占位符在 Go 侧替换）
	MsgUnsubscribeDoneNote    = "mail.msg.unsubscribeDoneNote"    // 退订成功页补充说明（事务类邮件不受影响）

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
	// 访客面（邮件里的公开链接）文案。
	//
	// 它们与上面那些的差别只在**来源**：这一组由 handler 自己产出（不是 service 上抛的错误），
	// 但它们同样是「可以对外展示的邮箱文案」，所以留在同一份白名单里 ——
	// 判据是「这句话能不能给外部看」，不是「它从哪一层冒出来」。
	// 不登记的话 mail_enums_test.go 会直接变红（本包每个 Err* / Msg* 常量都得在这里有位置）。
	ErrTrackLinkInvalid, ErrUnsubscribeLinkInvalid,
	MsgUnsubscribeDoneTitle, MsgUnsubscribeDoneBody, MsgUnsubscribeDoneNote,
}

// 测试邮件内容（后台「测试发送」触发，用于验证 SMTP 配置）。
const (
	TestMailSubject = "mail.test.mailSubject"
	TestMailHTML    = `<div style="font-family:system-ui,sans-serif;line-height:1.6"><h2>邮件配置连通性测试</h2><p>如果你看到这封邮件，说明发信账号的配置可用：</p><ul><li>SMTP 连接与认证通过</li><li>中文主题编码正常</li><li>HTML 与纯文本正文正常</li></ul><p style="color:#888;font-size:13px">本邮件由后台「测试发送」触发。</p></div>`
	TestMailText    = "mail.test.mailText"
)
