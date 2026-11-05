package membershiphttp

import (
	"strings"
	"testing"
)

// 会员页面的写入口统一为「HTMX + Jet 片段」：每个写表单声明一个本站端点、
// 一个结果区与一个错误区，信号完整；不再有页面 POST 或 JSON 提交器。
func TestMembershipTemplatesDeclareHTMXWriteAPIs(t *testing.T) {
	tier := renderMembershipPage(t, "admin/membership/membership.html", tiersPageFixture(map[string]any{
		"Edit": &membershipTierEdit{ID: 2, Name: "Silver", ThresholdAmount: 100005, SortOrder: -1},
	}))
	assign := renderMembershipPage(t, "admin/membership/membership_assignments.html", assignmentsPageFixture(nil))
	for _, tc := range []struct {
		name, body string
		apis       []string
		count      int
	}{
		{"tiers", tier, []string{"/api/membership/tier/create", "/api/membership/tier/update", "/api/membership/tier/delete", "/api/membership/entitlement/save"}, 5},
		{"assignments", assign, []string{"/api/membership/assign/set", "/api/membership/assign/unlock"}, 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, api := range tc.apis {
				if !strings.Contains(tc.body, `hx-post="`+api+`"`) {
					t.Errorf("missing API %s", api)
				}
			}
			// hx-target 的目标选择器本身含 data-write-result，所以按真实结果区元素计数。
			results := strings.Count(tc.body, "<div data-write-result>")
			// 页头另有一个页面级提示位，所以错误区按「不少于表单数」判定。
			if strings.Count(tc.body, "<form data-write ") != tc.count ||
				strings.Count(tc.body, "data-write-status") < tc.count || results != tc.count {
				t.Errorf("每个写表单要一个端点声明与一个本地结果区（表单 %d / 错误区 %d / 结果区 %d，期望 %d）",
					strings.Count(tc.body, "<form data-write "), strings.Count(tc.body, "data-write-status"),
					results, tc.count)
			}
			for _, stale := range []string{`method="post"`, `action="/admin/membership/tier/`, `action="/admin/membership/assign/`, "data-write-url"} {
				if strings.Contains(tc.body, stale) {
					t.Errorf("仍残留旧写通道 %q", stale)
				}
			}
			if !strings.Contains(tc.body, `name="csrf-token" content="test-token"`) {
				t.Error("写表单必须能在页面里取到 CSRF token")
			}
		})
	}
	// 表单字段是真实控件：门槛填「元」，权益用勾选与折扣数字，服务端负责映射。
	for _, want := range []string{`name="thresholdYuan"`, `name="tierId" value="2"`, `name="freeShipping"`, `name="discountPercent"`} {
		if !strings.Contains(tier, want) {
			t.Errorf("tier form contract missing %s", want)
		}
	}
	if strings.Contains(tier, "data-write-builder") || strings.Contains(tier, "membership_write.js") {
		t.Error("会员表单已改为真实控件，不应再依赖 JSON 提交器")
	}
}
