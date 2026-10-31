package mailenums

import "testing"

func TestAutomationStatusLabels(t *testing.T) {
	cases := []struct {
		status, key, fallback string
	}{
		{"active", "admin.mail.automation.status.active", "启用中"},
		{"paused", "admin.mail.automation.status.paused", "已暂停"},
		{"draft", "admin.mail.automation.status.draft", "草稿"},

		{"", "admin.mail.automation.status.unknown", "未知状态"},
		{"bogus", "admin.mail.automation.status.unknown", "未知状态"},

		{"  active  ", "admin.mail.automation.status.active", "启用中"},
	}
	for _, tc := range cases {
		got := AutomationStatusLabel(tc.status)
		if got.Key != tc.key || got.Fallback != tc.fallback {
			t.Errorf("AutomationStatusLabel(%q) = (%q, %q)，期望 (%q, %q)",
				tc.status, got.Key, got.Fallback, tc.key, tc.fallback)
		}
	}
}

func TestAutomationStatusChangedTemplate(t *testing.T) {
	if AutomationStatusChanged != "admin.mail.automation.statusChanged" {
		t.Errorf("AutomationStatusChanged = %q", AutomationStatusChanged)
	}
	if AutomationStatusChangedFallback != "状态已更新为 {status}。" {
		t.Errorf("AutomationStatusChangedFallback 必须保留 {status} 占位符，实际 %q",
			AutomationStatusChangedFallback)
	}
}

func TestTestSendFailedLabels(t *testing.T) {
	cases := []struct {
		got           LabelPair
		key, fallback string
	}{
		{TestSendFailedTemporary, "admin.mail.test_send.temporary", "测试邮件发送失败（可重试的临时故障），详情见服务端日志。"},
		{TestSendFailedPermanent, "admin.mail.test_send.permanent", "测试邮件发送失败（被对方永久拒绝），详情见服务端日志。"},
		{TestSendFailedConfiguration, "admin.mail.test_send.configuration", "测试邮件发送失败（配置问题，需人工处理），详情见服务端日志。"},
		{TestSendFailedUnknown, "admin.mail.test_send.unknown", "测试邮件发送失败（未分类），详情见服务端日志。"},
	}
	for _, tc := range cases {
		if tc.got.Key != tc.key || tc.got.Fallback != tc.fallback {
			t.Errorf("发送失败标签 = (%q, %q)，期望 (%q, %q)",
				tc.got.Key, tc.got.Fallback, tc.key, tc.fallback)
		}
	}
}
