package mailenums

// mail_status_labels.go — mail 模块**状态类**展示标签与状态回执的真源。
//
// 与 mail_ui_labels.go 同一形态（LabelPair = i18n key + 中文兜底）、同一去向（调用点取词）。
// 单独成文件是因为它们的消费面多一层：状态回执与测试发送回执由**写侧**在响应体里渲染
//（shell.RenderJump 整页提示，见 inbound/http/mail_jump.go）。
//
// 硬约束：**状态标签与句子外壳必须引用同一份拼装**（AutomationStatusLabel +
// AutomationStatusChanged）。两处各写一份字面量时，改了其中一侧的表现是英文后台
// 半句中文、半句英文 —— 不报错、不留痕。所以句子外壳也在这里给 key，不在 handler 里拼字面量。

import "strings"

// —— 自动化流程状态（mail_automations.status）——

// 状态标签。
//
// 前三条**复用状态徽章已经在用的词条**（mail_automation.html 按同一批 key 取词，
// 词条已在库内、中英成对）：Go 侧此前写的是中文常量，同一页的徽章与回执因此
// 在英文后台一英一中。「未知状态」是新词条，对应下面 default 那一档。
var (
	AutomationStatusActive  = LabelPair{"admin.mail.automation.status.active", "启用中"}
	AutomationStatusPaused  = LabelPair{"admin.mail.automation.status.paused", "已暂停"}
	AutomationStatusDraft   = LabelPair{"admin.mail.automation.status.draft", "草稿"}
	AutomationStatusUnknown = LabelPair{"admin.mail.automation.status.unknown", "未知状态"}
)

// AutomationStatusChanged 状态变更回执的整句模板（{status} 由调用点填当前语言的标签）。
//
// 为什么整句也要词条：只把标签词条化的话，英文界面会显示成
// 「状态已更新为 Enabled.」—— 半句中文比全句中文更糟（看起来像渲染 bug）。
const AutomationStatusChanged = "admin.mail.automation.statusChanged"

// AutomationStatusChangedFallback AutomationStatusChanged 的中文兜底（词条缺失时用）。
const AutomationStatusChangedFallback = "状态已更新为 {status}。"

// AutomationStatusLabel 自动化流程状态 → 展示标签。
//
// 只有三个已知状态会落到前三条，其余（含空串、表单里塞进来的任意值）一律回落
// 「未知状态」：状态值来自表单字段，把原值直接拼进回执等于让提交方决定页面显示什么。
func AutomationStatusLabel(status string) LabelPair {
	switch strings.TrimSpace(status) {
	case "active":
		return AutomationStatusActive
	case "paused":
		return AutomationStatusPaused
	case "draft":
		return AutomationStatusDraft
	default:
		return AutomationStatusUnknown
	}
}

// —— 测试邮件发送失败的受控文案（SMTP 原文只进日志，见 inbound/http/mail_err.go）——

// 分类取自 mailer 的 Kind（temporary / permanent / configuration）—— 有限的枚举，
// 而 SMTP 的响应码与主机名一律不出现在页面上：它们既不是给运营看的，
// 也不该经浏览器历史与 Referer 留在 URL 里。
//
// 取值 → 标签的映射留在 handler（那边已经 import mailer，用 KindXxx 常量判定；
// enums 不引入对 pkg/mailer 的依赖，避免「改一个 Kind 值要动语言表」）。
var (
	TestSendFailedTemporary     = LabelPair{"admin.mail.test_send.temporary", "测试邮件发送失败（可重试的临时故障），详情见服务端日志。"}
	TestSendFailedPermanent     = LabelPair{"admin.mail.test_send.permanent", "测试邮件发送失败（被对方永久拒绝），详情见服务端日志。"}
	TestSendFailedConfiguration = LabelPair{"admin.mail.test_send.configuration", "测试邮件发送失败（配置问题，需人工处理），详情见服务端日志。"}
	TestSendFailedUnknown       = LabelPair{"admin.mail.test_send.unknown", "测试邮件发送失败（未分类），详情见服务端日志。"}
)
