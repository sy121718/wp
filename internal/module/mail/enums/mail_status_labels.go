package mailenums

import "strings"

var (
	AutomationStatusActive  = LabelPair{"admin.mail.automation.status.active", "启用中"}
	AutomationStatusPaused  = LabelPair{"admin.mail.automation.status.paused", "已暂停"}
	AutomationStatusDraft   = LabelPair{"admin.mail.automation.status.draft", "草稿"}
	AutomationStatusUnknown = LabelPair{"admin.mail.automation.status.unknown", "未知状态"}
)

const AutomationStatusChanged = "admin.mail.automation.statusChanged"

const AutomationStatusChangedFallback = "状态已更新为 {status}。"

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

var (
	TestSendFailedTemporary     = LabelPair{"admin.mail.test_send.temporary", "测试邮件发送失败（可重试的临时故障），详情见服务端日志。"}
	TestSendFailedPermanent     = LabelPair{"admin.mail.test_send.permanent", "测试邮件发送失败（被对方永久拒绝），详情见服务端日志。"}
	TestSendFailedConfiguration = LabelPair{"admin.mail.test_send.configuration", "测试邮件发送失败（配置问题，需人工处理），详情见服务端日志。"}
	TestSendFailedUnknown       = LabelPair{"admin.mail.test_send.unknown", "测试邮件发送失败（未分类），详情见服务端日志。"}
)
