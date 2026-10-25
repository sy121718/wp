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

// AutomationNodeOutlets 出口名表：节点类型 → 该类型的出口名（有序）。
//
// **这是唯一一份出口名数据**：service 的 nodeOutlets（校验与执行）、画布的 palette 与
// 出口圆钮、模板结构树的白名单全部经它取值。放在 enums 而不是 service，是因为两边都要读
// ——service 已经 import enums，反向不成立；而各写一份的结果是「画布上画得出、保存被拒」。
//
// `ab_test` 不在这里：它的出口是动态的变体 id（来自 params.variants），由调用方读参数得出。
var AutomationNodeOutlets = map[string][]string{
	"trigger":            {"next"},
	"delay":              {"next"},
	"email":              {"next"},
	"tag":                {"next"},
	"webhook":            {"next"},
	"filter":             {"yes", "no"},
	"branch":             {"yes", "no"}, // v1 的类型名，读取时被升级成 filter
	"list_status_branch": {"not_in_list", "active", "non_active"},
	"end":                {},
}

// AutomationNodeTypeOutlets 取某类型的出口名（未登记的类型返回 nil，由调用方报未知类型）。
func AutomationNodeTypeOutlets(nodeType string) []string {
	outlets, ok := AutomationNodeOutlets[nodeType]
	if !ok {
		return nil
	}
	return outlets
}

// AutomationNodeTypes 节点类型选项（顺序即界面顺序 = palette 的顺序）。
//
// 十个类型（v2 的终局形状）：条件分支 v1 叫 branch、v2 叫 filter，界面只出现 filter
// 一个名字（存量 branch 在读取时被升级，见 service/automation_definition.go）。
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

// AutomationCanvasEmailDisabled 画布上「发信节点」被禁用时的原因（tooltip）。
//
// 写清「去哪里配」而不是「不可用」：把选项灰掉却不说是哪一步没做，用户只会以为画布坏了。
var AutomationCanvasEmailDisabled = LabelPair{
	Key:      "admin.mail.automation_canvas.email_disabled",
	Fallback: "先在「发信账号」里配置一个营销用途的 SMTP，再回来加发信节点",
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
// 为什么 handler 先取词再把**成品文案**交给 adminui.Prepare：Prepare 内部是 `t(title, title)`
// （兜底就是 key 本身），词条缺失时页面标题会显示 `admin.mail.heading` 这样的裸 key。
// 先取一次词，交出去的就是可读文案，中文兜底也回到了代码里。
// 已存在的 key 一律复用（admin.mail.heading / templates.heading / marketing.contacts.heading /
// marketing.campaigns.heading / automation.heading / automation.runs.heading / automation_run.heading），
// 只有「编辑页 / 报表页 / 画布页」三个标题是新增词条。
var (
	// PageTitleAccounts 发信账号页（/admin/mail）的 H1 标题。
	// 侧栏菜单标题仍是 admin.mail.heading「邮箱管理」——菜单描述模块归属、H1 描述页面内容，
	// 两者不是同一个 key，别再合回去（合并的结果是页面 H1 写着模块名，用户看不出这页干什么）。
	PageTitleAccounts = LabelPair{"admin.mail.accounts.heading", "发信账号"}
	// 营销页（/admin/mail/marketing）已拆成联系人页与群发活动页（issue #37），
	// 旧标题标签随之删除；侧栏菜单标题由 sys_menus 维护，不再经 LabelPair。
	// PageTitleCampaignReport 活动报表页。
	PageTitleCampaignReport = LabelPair{"admin.mail.campaign.heading", "活动报表"}
	// PageTitleAutomation 自动化流程列表页。
	PageTitleAutomation = LabelPair{"admin.mail.automation.heading", "自动化流程"}
	// PageTitleAutomationRun 实例排障页。
	PageTitleAutomationRun = LabelPair{"admin.mail.automation_run.heading", "实例排障"}
	// PageTitleAutomationCanvas 流程画布页。
	PageTitleAutomationCanvas = LabelPair{"admin.mail.automation_canvas.title", "流程画布"}
	// PageTitleTemplates 邮件模板页（从邮箱设置页拆出的独立职能）。
	PageTitleTemplates = LabelPair{"admin.mail.templates.heading", "邮件模板"}
	// PageTitleContacts 联系人页（从邮件营销页拆出，含导入）。
	PageTitleContacts = LabelPair{"admin.mail.marketing.contacts.heading", "联系人"}
	// PageTitleCampaigns 群发活动页（从邮件营销页拆出）。
	PageTitleCampaigns = LabelPair{"admin.mail.marketing.campaigns.heading", "群发活动"}
	// PageTitleAutomationRuns 自动化运行记录页（从自动化流程页拆出）。
	PageTitleAutomationRuns = LabelPair{"admin.mail.automation.runs.heading", "运行实例（排障）"}
)

// —— 自动化编辑器：表单页遗留的词条已全部删除 ——
//
// 表单编辑页（/admin/mail/automation/edit）随「画布是唯一编辑器」整页删除，随之失去
// 调用点的是三组：
//
//   · AutomationWaitUnits / AutomationConditions / AutomationRowNone（与两个 Option 类型）
//     —— 等待单位与条件下拉；
//   · AutomationFormErr*（13 条「第 N 步：…」）—— 表单行级校验文案；
//   · AutomationStepNone / ConditionNone / TargetNext / TargetEnd / StepLabel /
//     NextStepLabel / NextEnd —— 步骤行的标签。
//
// 全删而不是留作「存量」：它们零引用，留着只会让下一个人以为界面上还有那个下拉或那条
// 校验，而图校验的失败文案早已改由 graphErr 的 detail key 承担（admin.mail.detail.*）。
// 画布 palette 与属性面板取的是 meta.nodeTypes（由下面 AutomationNodeTypes 取词）。
