package mailenums

type LabelPair struct {
	Key      string
	Fallback string
}

type AutomationNodeTypeOption struct {
	Value string
	Label LabelPair
	Hint  LabelPair
}

var AutomationNodeOutlets = map[string][]string{
	"trigger":            {"next"},
	"delay":              {"next"},
	"email":              {"next"},
	"tag":                {"next"},
	"webhook":            {"next"},
	"filter":             {"yes", "no"},
	"branch":             {"yes", "no"},
	"list_status_branch": {"not_in_list", "active", "non_active"},
	"end":                {},
}

func AutomationNodeTypeOutlets(nodeType string) []string {
	outlets, ok := AutomationNodeOutlets[nodeType]
	if !ok {
		return nil
	}
	return outlets
}

var AutomationNodeTypes = []AutomationNodeTypeOption{
	{
		Value: "trigger",
		Label: LabelPair{"admin.mail.automation_node.trigger.label", "入口"},
		Hint:  LabelPair{"admin.mail.automation_node.trigger.hint", "流程从这里开始，不需要参数"},
	},
	{
		Value: "delay",
		Label: LabelPair{"admin.mail.automation_node.delay.label", "等待"},
		Hint:  LabelPair{"admin.mail.automation_node.delay.hint", "等待若干分钟再继续"},
	},
	{
		Value: "email",
		Label: LabelPair{"admin.mail.automation_node.email.label", "发邮件"},
		Hint:  LabelPair{"admin.mail.automation_node.email.hint", "用某个邮件模板发一封信"},
	},
	{
		Value: "filter",
		Label: LabelPair{"admin.mail.automation_node.filter.label", "条件分支"},
		Hint:  LabelPair{"admin.mail.automation_node.filter.hint", "条件满足走 yes，不满足走 no"},
	},
	{
		Value: "list_status_branch",
		Label: LabelPair{"admin.mail.automation_node.list_status_branch.label", "按订阅状态分流"},
		Hint:  LabelPair{"admin.mail.automation_node.list_status_branch.hint", "已订阅 / 待确认 / 不在名单，各走一条"},
	},
	{
		Value: "ab_test",
		Label: LabelPair{"admin.mail.automation_node.ab_test.label", "A/B 分流"},
		Hint:  LabelPair{"admin.mail.automation_node.ab_test.hint", "按权重把联系人分到不同变体（2–5 个，权重和 100）"},
	},
	{
		Value: "webhook",
		Label: LabelPair{"admin.mail.automation_node.webhook.label", "回调"},
		Hint:  LabelPair{"admin.mail.automation_node.webhook.hint", "向一个 https 地址发 POST（内网地址会被拒绝）"},
	},
	{
		Value: "tag",
		Label: LabelPair{"admin.mail.automation_node.tag.label", "打标签"},
		Hint:  LabelPair{"admin.mail.automation_node.tag.hint", "给联系人加标签或减标签"},
	},
	{
		Value: "end",
		Label: LabelPair{"admin.mail.automation_node.end.label", "结束"},
		Hint:  LabelPair{"admin.mail.automation_node.end.hint", "流程到此结束，不需要参数"},
	},
}

var AutomationCanvasEmailDisabled = LabelPair{
	Key:      "admin.mail.automation_canvas.email_disabled",
	Fallback: "先在「发信账号」里配置一个营销用途的 SMTP，再回来加发信节点",
}

type AutomationTriggerOption struct {
	Value string
	Label LabelPair
}

var AutomationTriggers = []AutomationTriggerOption{
	{"manual", LabelPair{"admin.mail.automation.trigger.manual", "手工添加（后台选人加入）"}},
	{"contact_created", LabelPair{"admin.mail.automation.trigger.contact_created", "新联系人产生"}},
	{"contact_subscribed", LabelPair{"admin.mail.automation.trigger.contact_subscribed", "变为已订阅"}},
	{"email_opened", LabelPair{"admin.mail.automation.trigger.email_opened", "打开过营销邮件"}},
	{"email_clicked", LabelPair{"admin.mail.automation.trigger.email_clicked", "点击过营销链接"}},
	{"tag_added", LabelPair{"admin.mail.automation.trigger.tag_added", "被打上某个标签"}},
}

var AutomationRowNone = LabelPair{"admin.mail.automation_edit.row.none", "（不用这行）"}

const TestMailHTMLKey = "mail.test.mailHtml"

const (
	TestMailSubjectFallback = "go_wp 邮件配置测试"

	TestMailTextFallback = "邮件配置连通性测试\n\n如果你看到这封邮件，说明发信账号的配置可用：\n- SMTP 连接与认证通过\n- 中文主题编码正常\n- 纯文本正文正常\n\n本邮件由后台「测试发送」触发。"

	TestMailHTMLFallback = `<div style="font-family:system-ui,sans-serif;line-height:1.6"><h2>邮件配置连通性测试</h2><p>如果你看到这封邮件，说明发信账号的配置可用：</p><ul><li>SMTP 连接与认证通过</li><li>中文主题编码正常</li><li>HTML 与纯文本正文正常</li></ul><p style="color:#888;font-size:13px">本邮件由后台「测试发送」触发。</p></div>`
)

var (
	PageTitleAccounts = LabelPair{"admin.mail.accounts.heading", "发信账号"}

	PageTitleCampaignReport = LabelPair{"admin.mail.campaign.heading", "活动报表"}

	PageTitleAutomation = LabelPair{"admin.mail.automation.heading", "自动化流程"}

	PageTitleAutomationRun = LabelPair{"admin.mail.automation_run.heading", "实例排障"}

	PageTitleTemplateEditor = LabelPair{"admin.mail.template_editor.title", "模板编辑器"}

	PageTitleAutomationCanvas = LabelPair{"admin.mail.automation_canvas.title", "流程画布"}

	PageTitleTemplates = LabelPair{"admin.mail.templates.heading", "邮件模板"}

	PageTitleContacts = LabelPair{"admin.mail.marketing.contacts.heading", "联系人"}

	PageTitleCampaigns = LabelPair{"admin.mail.marketing.campaigns.heading", "群发活动"}

	PageTitleAutomationRuns = LabelPair{"admin.mail.automation.runs.heading", "运行实例（排障）"}
)
