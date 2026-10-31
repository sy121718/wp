package projecthttp

import (
	"context"
	"testing"

	projectcontract "go_wp/internal/module/project/contract"
)

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

	h.markPagesStaleForSiteSettingsChange(context.Background(), "p-1", false)
	if marker.calls != 0 {
		t.Fatalf("字段未变时不该标记，实际调用 %d 次", marker.calls)
	}

	h.markPagesStaleForSiteSettingsChange(context.Background(), "p-1", true)
	if marker.calls != 0 {
		t.Fatalf("未注入 stale 网时不该凭空调用，实际 %d 次", marker.calls)
	}
	t.Log("未变不标、未注入不 panic —— 两条都成立")
}

func TestMergeSiteSettingsDetectsBuildVisibleChange(t *testing.T) {
	raw := []byte(`{"headScripts":"<script>a</script>","siteName":"旧名"}`)

	same, err := mergeSiteSettings(raw, projectcontract.SiteSettings{
		HeadScripts: "<script>a</script>",
		SiteName:    "旧名",
	})
	if err != nil {
		t.Fatalf("合并失败: %v", err)
	}

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
