package mailenums

import "testing"

// 状态类展示标签的取值覆盖：key 与中文兜底逐字稳定。
//
// 这一组比别处更要紧：它们的句子会被**写侧**拼进 302 的 query、**读侧**再用同一份
// 取值重拼候选集做整句比对（见 inbound/http/mail_err.go 的 mailNoticeTexts）。
// key 或兜底漂一个字，表现就是「运营点完按钮，页面上什么都没有」——
// 服务端不报错、日志里也看不出来（inbound/http 的 TestMailNoticeWriteSideMatchesReadSide
// 钉的是拼装一致，这里钉的是取值本身）。
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
