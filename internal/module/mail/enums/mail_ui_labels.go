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
		Hint:  LabelPair{"admin.mail.automation_edit.node.delay.hint", "填时长并选单位，例如 2 小时"},
	},
	{
		Value: "email",
		Label: LabelPair{"admin.mail.automation_edit.node.email.label", "发邮件"},
		Hint:  LabelPair{"admin.mail.automation_edit.node.email.hint", "选一个邮件模板"},
	},
	{
		Value: "branch",
		Label: LabelPair{"admin.mail.automation_edit.node.branch.label", "条件分支"},
		Hint: LabelPair{"admin.mail.automation_edit.node.branch.hint",
			"选一个判断条件，再指定满足与不满足时各跳到哪一步"},
	},
	{
		Value: "tag",
		Label: LabelPair{"admin.mail.automation_edit.node.tag.label", "打标签"},
		Hint:  LabelPair{"admin.mail.automation_edit.node.tag.hint", "填要加的标签（逗号分隔）"},
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
	// PageTitleAutomationEdit 流程编辑页。
	PageTitleAutomationEdit = LabelPair{"admin.mail.automation_edit.title", "编辑自动化流程"}
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

// —— 自动化编辑器的「步骤」概念（issue #38 P3 重做）——
//
// 用户概念只有「触发方式 + 按顺序的步骤」：标识 / 下一步 / yes / no 四个输入框从界面消失
// （用户原话「新建自动化不知道是个什么东西完全没法用」）。下面这几组取值是**编辑器**的
// 界面概念，与引擎的图定义之间有一次换算（见 inbound/http/mail_automation_form.go）：
//
//	· 等待 = 时长（数字）+ 单位（分钟 / 小时 / 天）→ 引擎的 `minutes` 整数；
//	· 条件分支 = 一个条件下拉（+ 标签名）→ 引擎的 `conditions` 编码（has_tag:<标签>）；
//	· 跳转 = 第 N 步 / 结束 → 引擎的节点 key。
//
// 换算只写一处：编码是内部语法（`has_tag:vip`、`minutes=1440`），露给用户就是旧版的问题。

// AutomationWaitUnitOption 等待步骤的时长单位。
type AutomationWaitUnitOption struct {
	Value string
	// Minutes 一个单位等于多少分钟（提交时按它换算，读回来时按同样规则反解）。
	Minutes int
	Label   LabelPair
}

// AutomationWaitUnits 等待单位选项（顺序即界面顺序）。
var AutomationWaitUnits = []AutomationWaitUnitOption{
	{"minute", 1, LabelPair{"admin.mail.automation_edit.unit.minute", "分钟"}},
	{"hour", 60, LabelPair{"admin.mail.automation_edit.unit.hour", "小时"}},
	{"day", 1440, LabelPair{"admin.mail.automation_edit.unit.day", "天"}},
}

// AutomationConditionOption 条件分支的判断条件。
type AutomationConditionOption struct {
	Value string
	Label LabelPair
	// NeedsTag 该条件还要再填一个标签名（只有 has_tag）。
	NeedsTag bool
}

// AutomationConditions 条件分支的选项（顺序即界面顺序）。
var AutomationConditions = []AutomationConditionOption{
	{"opened", LabelPair{"admin.mail.automation_edit.cond.opened", "打开过邮件"}, false},
	{"clicked", LabelPair{"admin.mail.automation_edit.cond.clicked", "点击过链接"}, false},
	{"subscribed", LabelPair{"admin.mail.automation_edit.cond.subscribed", "已经订阅"}, false},
	{"has_tag", LabelPair{"admin.mail.automation_edit.cond.has_tag", "带着某个标签"}, true},
}

// AutomationFormErr* 编辑页的表单校验文案（B5 十二条口径）。
//
// 具名而不是按下标访问：插一条就全错位，而错位的表现是
// 「第 3 步报的是第 5 步的错」—— 用户照着改永远改不对。
// 文案由 mailFormErrText 原样回带（校验失败走 200 回显，见 mail_page.go 的 renderAutomationForm）。
var (
	AutomationFormErrNameRequired     = LabelPair{"admin.mail.automation_edit.err.name_required", "流程名称不能为空"}
	AutomationFormErrStepsRequired    = LabelPair{"admin.mail.automation_edit.err.steps_required", "至少要排一个步骤"}
	AutomationFormErrStepTypeRequired = LabelPair{"admin.mail.automation_edit.err.step_type_required", "第 %d 步：请选择步骤类型"}
	AutomationFormErrStepTemplate     = LabelPair{"admin.mail.automation_edit.err.step_template_required", "第 %d 步：请选择要发送的邮件模板"}
	AutomationFormErrStepDelayValue   = LabelPair{"admin.mail.automation_edit.err.step_delay_value", "第 %d 步：等待时长要填大于 0 的整数"}
	AutomationFormErrStepDelayUnit    = LabelPair{"admin.mail.automation_edit.err.step_delay_unit", "第 %d 步：等待单位只能选分钟 / 小时 / 天"}
	AutomationFormErrStepCondition    = LabelPair{"admin.mail.automation_edit.err.step_condition_required", "第 %d 步：请选择判断条件"}
	AutomationFormErrStepTag          = LabelPair{"admin.mail.automation_edit.err.step_tag_required", "第 %d 步：请填写至少一个标签"}
	// 目标选错有两种事实，措辞必须分开：往回跳时那个步号**就在页面上**，
	// 说「不存在」与用户所见直接冲突（他会以为是自己没选上而反复重试）；
	// 只有目标步被删掉之后步号才真的失效。合成一条会让前者变成假话。
	AutomationFormErrStepTargetBackward = LabelPair{"admin.mail.automation_edit.err.step_target_backward", "第 %d 步：跳转目标只能选本步之后的步骤"}
	AutomationFormErrStepTargetInvalid  = LabelPair{"admin.mail.automation_edit.err.step_target_invalid", "第 %d 步：跳转目标已不存在（可能已被删除），请重新选择"}
	AutomationFormErrStepsTooMany       = LabelPair{"admin.mail.automation_edit.err.steps_too_many", "最多只能排 12 步，请拆分流程"}
	AutomationFormErrEndNotLast         = LabelPair{"admin.mail.automation_edit.err.end_not_last", "第 %d 步：结束步骤必须是最后一步"}
	AutomationFormErrAssemble           = LabelPair{"admin.mail.automation_edit.err.assemble_failed", "流程保存失败，请检查各步的填写内容后重试"}
)

// AutomationFormMessages 已随「写动作结论走 shell.RenderJump」删除：
//
// 它只服务于读侧白名单（mailFormNoticeTemplates 把切片取词后交给 shell.NoticeTemplate 归一），
// 而结论现在直接渲染进响应体、不再经查询参数回带，读侧判定整批消失。
// 校验文案的真源仍是上面的 AutomationFormErr* 具名常量（写侧 mailLabel 取词后 Sprintf）。

// 编辑页的控件文案（下拉空选项 / 行内只读标签）。
//
// 「第 %d 步」这类带数字的模板由 Go 侧 Sprintf：Jet 没有 Sprintf，而把步号拼在模板里
// 等于在模板里做逻辑（下一轮改文案的人不会想到这里还有一处拼装）。
var (
	AutomationStepNone      = LabelPair{"admin.mail.automation_edit.step_none", "请选择步骤类型"}
	AutomationConditionNone = LabelPair{"admin.mail.automation_edit.cond.none", "请选择判断条件"}
	AutomationTargetNext    = LabelPair{"admin.mail.automation_edit.target.next", "下一步"}
	AutomationTargetEnd     = LabelPair{"admin.mail.automation_edit.target.end", "结束"}
	AutomationStepLabel     = LabelPair{"admin.mail.automation_edit.step_label", "第 %d 步"}
	AutomationNextStepLabel = LabelPair{"admin.mail.automation_edit.next_step", "下一步：第 %d 步"}
	AutomationNextEnd       = LabelPair{"admin.mail.automation_edit.next_end", "下一步：结束"}
)
