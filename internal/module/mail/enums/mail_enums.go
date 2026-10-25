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
	// ErrConsentSourceRequired 置为「已订阅」但没写同意来源。
	//
	// 与导入路径的 ConsentDeclared 是同一条合规底线的两个入口：那边靠勾选强制，
	// 这边（新建 / 编辑）必须由服务端强制 —— 页面文案承诺「要置为已订阅就得写清来源」，
	// 服务端不执行就是文案在撒谎，而这条底线正是本模块页头 help 里写的那件事。
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

// Notice* 后台页面级受控提示的 key（批量结论、带计数的回执、缺参引导）。
//
// 它们与上面的 Msg* / Err* 差别只在**来源**：不是 service 上抛、也不是 response 的 message，
// 而是 handler 自己写出的句子（见 inbound/http/mail_err.go 的 mail*Msg 事实块）。
// 「能不能对外说」不再靠枚举常量判定 —— 判定只有一条：pkg/response.IsBusinessError，
// 经 internal/adminui 的 PageErrorFact 出口；这批常量的职责只是给这些句子一个**可翻译的 key**。
//
// 词条由迁移 seed（本次改造新增的清单见改造报告「新 i18n key」一节），
// 词条缺失时回落 handler 里的中文兜底（与改造前逐字相同）。
const (
	NoticeBulkAllDone    = "mail.bulk.allDone"
	NoticeBulkAllSkipped = "mail.bulk.allSkipped"
	NoticeBulkPartial    = "mail.bulk.partial"
	// NoticeAutomationTick 手工补投一轮延时实例的回执（补投条数是位置参数）。
	NoticeAutomationTick = "mail.msg.automationTick"
	// NoticeTemplateListFailed 模板列表取数失败：本次批量删除没有执行。
	NoticeTemplateListFailed = "mail.page.templateListFailed"
	// NoticeContactStatusBad 批量改联系人状态时目标状态不在受控取值里。
	NoticeContactStatusBad = "mail.page.contactStatusBad"
	// 缺 id 的两句引导（分档判据见 inbound/http 的 mailQueryID）。
	//
	// 画布那条（automationIDRequired）已随「缺 id 即新建」删除：画布现在把「没给 id」
	// 当正常的建流程入口，不再是一句引导。
	NoticeCampaignIDRequired = "mail.page.campaignIDRequired"
	NoticeRunIDRequired      = "mail.page.runIDRequired"
)

// 触发事件与「哪些事件能当触发器」的口径。
//
// 有两类事件**刻意不作为触发器**：
//
//	email.sent / email.delivered —— 自动化自己发信会产生 sent，若它也触发流程，
//	「发信 → 触发 → 发信」就是发送循环（Notifuse 同样排除这两类）。
//	unsubscribe / complaint / bounce —— 它们是**抑制信号**，正确反应是「少发」，
//	拿来做流程触发器几乎总是配错（而且与合规底线冲突）。
//
// 哪些能当触发器由 model.Trigger* 常量与 validTrigger 决定，不在这里重复列一遍。
//
// MailFacingMessages 已于「控制器不取词」这一批删除，这里留一条路标。
//
// 它是一份必须与上面每个 Err* / Msg* 常量逐条对齐的**白名单**（漏一条 = 业务错误在页面上
// 变成一句通用提示，不报错、不记日志；黑名单漏一条 = service 上抛的 PostgreSQL / SMTP
// 原文直出）。现在判定只有一条形状判据（pkg/response.IsBusinessError：a.b.c 形态的 key，
// 或 Err* / Msg* 常量名），统一经 internal/adminui 的 PageErrorFact 出口 —— 见
// internal/module/mail/inbound/http/mail_err.go 的文件头说明。
//
// 于是「能对外说的话」与「模块有哪些文案常量」不再需要人工对账：前者由错误的形状决定，
// 后者是 enums 自己的事。原来钉那份对账的 AST 测试改成「key 形态与唯一性」自检
//（mail_enums_test.go）。

// 测试邮件内容（后台「测试发送」触发，用于验证 SMTP 配置）。
//
// 三条**都是 i18n key**：service 发送前按请求语言取词（见 service/mail_account.go 的 TestSend），
// 英文界面上点「测试发送」收到的该是英文邮件。中文兜底在 mail_ui_labels.go：
// TestMailSubject / TestMailText 的兜底写在 TestSend 调用点，HTML 的兜底是 TestMailHTMLFallback。
const (
	TestMailSubject = "mail.test.mailSubject"
	TestMailText    = "mail.test.mailText"
)
