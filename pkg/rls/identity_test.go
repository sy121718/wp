package rls

import (
	"strings"
	"testing"
)

// TestIdentityBypassesRLS 钉住「谁会无条件绕过 RLS」这一条判据。
//
// 它是 DB-04 的核心：策略铺了多少张表都不改变这个结论 —— superuser / BYPASSRLS
// 任一为真就会绕过，且不受 FORCE 约束（FORCE 只管表属主）。
func TestIdentityBypassesRLS(t *testing.T) {
	cases := []struct {
		name string
		id   Identity
		want bool
	}{
		{"普通角色", Identity{CurrentUser: "go_wp_app"}, false},
		{"表属主（普通角色）", Identity{CurrentUser: "go_wp_app", OwnedRLSTables: 53}, false},
		{"超级用户", Identity{CurrentUser: "root", IsSuperuser: true}, true},
		{"BYPASSRLS 角色", Identity{CurrentUser: "bypass", BypassRLS: true}, true},
		{"两者都是", Identity{CurrentUser: "root", IsSuperuser: true, BypassRLS: true}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.id.BypassesRLS(); got != tc.want {
				t.Fatalf("BypassesRLS() = %v，期望 %v（%+v）", got, tc.want, tc.id)
			}
		})
	}
}

// TestIdentityVerdict 结论文案必须能区分「绕过」与「生效」，并带上判定所依据的字段 ——
// 启动日志是运维唯一的现场，不能只写一句「RLS 已检查」。
func TestIdentityVerdict(t *testing.T) {
	bypass := Identity{SessionUser: "root", CurrentUser: "root", IsSuperuser: true, RLSTables: 53}.Verdict()
	for _, want := range []string{"root", "超级用户", "绕过", "53"} {
		if !strings.Contains(bypass, want) {
			t.Fatalf("绕过结论缺少 %q：%s", want, bypass)
		}
	}
	if strings.Contains(bypass, "生效：") {
		t.Fatalf("绕过结论不该说策略生效：%s", bypass)
	}

	// 角色带 BYPASSRLS 但不是超级用户时，理由要写对（运维会据此决定改哪个属性）。
	bypassOnly := Identity{SessionUser: "app", CurrentUser: "app", BypassRLS: true}.Verdict()
	if !strings.Contains(bypassOnly, "BYPASSRLS") || strings.Contains(bypassOnly, "超级用户") {
		t.Fatalf("仅 BYPASSRLS 的结论措辞不对：%s", bypassOnly)
	}

	ok := Identity{SessionUser: "go_wp_app", CurrentUser: "go_wp_app", RLSTables: 53, OwnedRLSTables: 40}.Verdict()
	for _, want := range []string{"go_wp_app", "不绕过", "53", "40"} {
		if !strings.Contains(ok, want) {
			t.Fatalf("生效结论缺少 %q：%s", want, ok)
		}
	}
}
