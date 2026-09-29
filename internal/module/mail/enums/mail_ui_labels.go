package mailenums

// mail_ui_labels.go — mail 模块「Go 侧生成、会显示在页面 / 邮件里」的展示文案。
//
// 形态统一为 (i18n key, 中文兜底) 的 LabelPair：
//
//   - 只给中文 → 英文界面恒中文（拿不到词条）；
//   - 只给 key → 词条缺失时页面显示裸 key（`admin.mail.automation_edit.node.delay.label`）；
//   - 成对给出 → 调用点 `tr(key, fallback)` 命中出译文、未命中出中文兜底。
//
// 词条真源是 sys_i18n（迁移 450 seed，中英成对）；本文件只持有 key 与中文兜底。
// 放 enums 而不是 handler：这几组取值同时被列表页、编辑页与表单组装层使用，
// 各写一份必然漂移（下拉里叫「新联系人产生」、列表里叫 contact_created 的旧缺陷就是这么来的）。
// 同一形态的先例见 user/enums 的 `LabelKeyStatusActive` + `LabelStatusActive`。

// LabelPair 一组展示名取值：i18n key + 中文兜底。
type LabelPair struct {
	Key      string
	Fallback string
}

// AutomationNodeTypeOption 自动化节点类型选项（Label 与 Hint 都要取词）。
type AutomationNodeTypeOption struct {
	Value string
	Label LabelPair
	Hint  LabelPair
}

// AutomationNodeTypes 节点类型选项（顺序即界面顺序）。
var AutomationNodeTypes = []AutomationNodeTypeOption{
	{
		Value: "trigger",
		Label: LabelPair{"admin.mail.automation_edit.node.trigger.label", "入口"},
		Hint:  LabelPair{"admin.mail.automation_edit.node.trigger.hint", "流程从这里开始（不需要参数）"},
	},
	{
		Value: "delay",
		Label: LabelPair{"admin.mail.automation_edit.node.delay.label", "等待"},
		Hint:  LabelPair{"admin.mail.automation_edit.node.delay.hint", "参数填分钟数，例如 1440 表示一天"},
	},
	{
		Value: "email",
		Label: LabelPair{"admin.mail.automation_edit.node.email.label", "发邮件"},
		Hint:  LabelPair{"admin.mail.automation_edit.node.email.hint", "参数填邮件模板的模板 key"},
	},
	{
		Value: "branch",
		Label: LabelPair{"admin.mail.automation_edit.node.branch.label", "条件分支"},
		Hint: LabelPair{"admin.mail.automation_edit.node.branch.hint",
			"参数填条件（逗号分隔）：opened / clicked / subscribed / has_tag:标签；再填 yes 与 no 两条出边"},
	},
	{
		Value: "tag",
		Label: LabelPair{"admin.mail.automation_edit.node.tag.label", "打标签"},
		Hint:  LabelPair{"admin.mail.automation_edit.node.tag.hint", "参数填要加的标签（逗号分隔）"},
	},
	{
		Value: "end",
		Label: LabelPair{"admin.mail.automation_edit.node.end.label", "结束"},
		Hint:  LabelPair{"admin.mail.automation_edit.node.end.hint", "流程到此结束（不需要参数）"},
	},
}

// AutomationTriggerOption 触发方式选项（列表展示与下拉**共用这一份**）。
type AutomationTriggerOption struct {
	Value string
	Label LabelPair
}

// AutomationTriggers 触发方式选项（顺序即界面顺序）。
var AutomationTriggers = []AutomationTriggerOption{
	{"manual", LabelPair{"admin.mail.automation.trigger.manual", "手工添加（后台选人加入）"}},
	{"contact_created", LabelPair{"admin.mail.automation.trigger.contact_created", "新联系人产生"}},
	{"contact_subscribed", LabelPair{"admin.mail.automation.trigger.contact_subscribed", "变为已订阅"}},
	{"email_opened", LabelPair{"admin.mail.automation.trigger.email_opened", "打开过营销邮件"}},
	{"email_clicked", LabelPair{"admin.mail.automation.trigger.email_clicked", "点击过营销链接"}},
	{"tag_added", LabelPair{"admin.mail.automation.trigger.tag_added", "被打上某个标签"}},
}

// AutomationRowNone 表单里「不用这行」的空选项。
var AutomationRowNone = LabelPair{"admin.mail.automation_edit.row.none", "（不用这行）"}

// TestMailHTMLKey 测试邮件的 HTML 正文（i18n key；按账号语言取词后交给 mailer）。
//
// 与 TestMailSubject / TestMailText 一样是 key：这三条都按**请求语言**取词再发送，
// 英文界面上点「测试发送」收到的该是英文邮件。中文兜底见下面的 *Fallback 三条。
const TestMailHTMLKey = "mail.test.mailHtml"

// 测试邮件三条文案的中文兜底（词条缺失 / i18n 未初始化时用它）。
//
// 兜底写在代码里而不是只靠 sys_i18n：词条缺失时邮件正文会退化成裸 key
// （收件人收到一封主题写着 `mail.test.mailSubject` 的邮件），那是比中文更难解释的现象。
const (
	// TestMailSubjectFallback 与 sys_i18n 的 mail.test.mailSubject（zh-CN）逐字一致。
	TestMailSubjectFallback = "go_wp 邮件配置测试"
	// TestMailTextFallback 与 sys_i18n 的 mail.test.mailText（zh-CN）逐字一致。
	TestMailTextFallback = "邮件配置连通性测试\n\n如果你看到这封邮件，说明发信账号的配置可用：\n- SMTP 连接与认证通过\n- 中文主题编码正常\n- 纯文本正文正常\n\n本邮件由后台「测试发送」触发。"
	// TestMailHTMLFallback 测试邮件 HTML 正文的中文原文。
	TestMailHTMLFallback = `<div style="font-family:system-ui,sans-serif;line-height:1.6"><h2>邮件配置连通性测试</h2><p>如果你看到这封邮件，说明发信账号的配置可用：</p><ul><li>SMTP 连接与认证通过</li><li>中文主题编码正常</li><li>HTML 与纯文本正文正常</li></ul><p style="color:#888;font-size:13px">本邮件由后台「测试发送」触发。</p></div>`
)

// —— 页面标题（layout 的 <title> 与顶栏）——
//
// 为什么 handler 先取词再把**成品文案**交给 shell.Prepare：Prepare 内部是 `t(title, title)`
// （兜底就是 key 本身），词条缺失时页面标题会显示 `admin.mail.heading` 这样的裸 key。
// 先取一次词，交出去的就是可读文案，中文兜底也回到了代码里。
// 四条已存在的 key 直接复用（admin.mail.heading / marketing.heading / automation.heading /
// automation_run.heading），只有「编辑页 / 报表页 / 画布页」三个标题是本轮新增。
var (
	// PageTitleMail 邮箱设置页。
	PageTitleMail = LabelPair{"admin.mail.heading", "邮箱设置"}
	// PageTitleMarketing 邮件营销页。
	PageTitleMarketing = LabelPair{"admin.mail.marketing.heading", "邮件营销"}
	// PageTitleCampaignReport 活动报表页。
	PageTitleCampaignReport = LabelPair{"admin.mail.campaign.heading", "活动报表"}
	// PageTitleAutomation 自动化流程列表页。
	PageTitleAutomation = LabelPair{"admin.mail.automation.heading", "自动化流程"}
	// PageTitleAutomationEdit 流程编辑页。
	PageTitleAutomationEdit = LabelPair{"admin.mail.automation_edit.title", "编辑自动化流程"}
	// PageTitleAutomationRun 实例排障页。
	PageTitleAutomationRun = LabelPair{"admin.mail.automation_run.heading", "实例排障"}
	// PageTitleAutomationCanvas 流程画布页。
	PageTitleAutomationCanvas = LabelPair{"admin.mail.automation_canvas.title", "流程画布"}
)
