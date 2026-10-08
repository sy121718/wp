package mailenums

import "testing"

// 状态类展示标签的取值覆盖：key 与中文兜底逐字稳定。
//
// 这一组比别处更要紧：状态回执的整句由 AutomationStatusLabel + AutomationStatusChanged
// 拼成，再由 shell.RenderJump 直接渲染（见 inbound/http/mail_jump.go）。key 或兜底漂一个字，
// 表现就是英文后台半句中文、半句英文 —— 服务端不报错、日志里也看不出来。
func TestAutomationStatusLabels(t *testing.T) {
	cases := []struct {
		status, key, fallback string
	}{
		{"active", "admin.mail.automation.status.active", "启用中"},
		{"paused", "admin.mail.automation.status.paused", "已暂停"},
		{"draft", "admin.mail.automation.status.draft", "草稿"},
		// 未知档：表单塞进来的任意取值都落这里（不能把原值拼进回执）。
		{"", "admin.mail.automation.status.unknown", "未知状态"},
		{"bogus", "admin.mail.automation.status.unknown", "未知状态"},
		// 前后空白按 trim 后判定。
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

// TestAutomationStatusChangedTemplate 整句外壳的中文兜底里必须留着 {status}：
// 漏了占位符的表现是回执退化成「状态已更新为 。」（句子里没有状态），不报错。
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
