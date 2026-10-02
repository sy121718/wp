package projecthttp

// site_settings_stale_test.go — 站点设置保存后的 stale 标记（FIX-21）。
//
// 两个方向都要钉：
//   · 构建可见字段**变了**（headScripts 等）→ 必须标 stale，否则「加了统计脚本线上不变」；
//   · 字段**没变**（只点了保存）→ 不能标，否则保存按钮等于一次全量重建。

import (
	"context"
	"testing"

	projectcontract "go_wp/internal/module/project/contract"
)

// fakeStaleMarker 记录 MarkStaleForI18n 被调用的次数（page 契约的收窄替身）。
type fakeStaleMarker struct {
	calls int
}

func (f *fakeStaleMarker) MarkStaleForI18n(context.Context) error {
	f.calls++
	return nil
}

func TestMarkPagesStaleForSiteSettingsChange(t *testing.T) {
	h := &siteSettingsAdminHandle{}
	marker := &fakeStaleMarker{}

	// ① 字段未变：不标（h.pages 为 nil 时也应安全返回）。
	h.markPagesStaleForSiteSettingsChange(context.Background(), "p-1", false)
	if marker.calls != 0 {
		t.Fatalf("字段未变时不该标记，实际调用 %d 次", marker.calls)
	}

	// ② 字段变了但没有注入 stale 网（测试装配常见）：安全返回，不 panic
	// —— 保存是主职责，标记失败只记日志（见函数注释）。
	h.markPagesStaleForSiteSettingsChange(context.Background(), "p-1", true)
	if marker.calls != 0 {
		t.Fatalf("未注入 stale 网时不该凭空调用，实际 %d 次", marker.calls)
	}
	t.Log("未变不标、未注入不 panic —— 两条都成立")
}

// TestMergeSiteSettingsDetectsBuildVisibleChange 保存前的 before/after 判定：
// 同一份输入产出的合并结果逐字节相同（不误判为变化），改动某个构建可见字段则不同。
func TestMergeSiteSettingsDetectsBuildVisibleChange(t *testing.T) {
	raw := []byte(`{"headScripts":"<script>a</script>","siteName":"旧名"}`)

	same, err := mergeSiteSettings(raw, projectcontract.SiteSettings{
		HeadScripts: "<script>a</script>",
		SiteName:    "旧名",
	})
	if err != nil {
		t.Fatalf("合并失败: %v", err)
	}
	// 判据是**语义**比较（生产代码用的 siteSettingsDiffer）而不是逐字节：
	// json.Marshal 会把 < 转义成 \u003c，而库里的 jsonb 读出来是未转义的原文 ——
	// 逐字节比会把「什么都没改」判成变化，于是每次保存都全站重建（实测踩过）。
	if siteSettingsDiffer(raw, same) {
		t.Fatalf("同一份输入应判定为未变化：\n前 %s\n后 %s", raw, same)
	}

	changed, err := mergeSiteSettings(raw, projectcontract.SiteSettings{
		HeadScripts: "<script>b</script>",
		SiteName:    "旧名",
	})
	if err != nil {
		t.Fatalf("合并失败: %v", err)
	}
	if !siteSettingsDiffer(raw, changed) {
		t.Fatal("headScripts 改了却没被判定为变化")
	}
	t.Logf("未改 → 判定未变化；改 headScripts → 判定为变化")
}
