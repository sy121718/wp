package i18n

import "testing"

func TestSnapshotFreezesCache(t *testing.T) {
	withCache(t, map[string]map[string]string{
		"site.component.form.submit": {"zh-CN": "提交", "en-US": "Submit"},
	})
	en := Snapshot("en-US")
	if got := en("site.component.form.submit", "提交"); got != "Submit" {
		t.Fatalf("快照首次取词错误: %q", got)
	}

	cache.Update(map[string]map[string]string{
		"site.component.form.submit": {"zh-CN": "提交（新）", "en-US": "Send"},
	}, map[string]int{})
	if got := en("site.component.form.submit", "提交"); got != "Submit" {
		t.Fatalf("快照被后续刷新污染: %q", got)
	}

	if got := Translate("site.component.form.submit", "提交", "en-US"); got != "Send" {
		t.Fatalf("实时取词应看到新值: %q", got)
	}
}

func TestSnapshotFallbackChain(t *testing.T) {
	withCache(t, map[string]map[string]string{
		"site.component.video.title": {"zh-CN": "视频"},
		"site.component.only.fr":     {"fr-FR": "Vidéo"},
	})
	en := Snapshot("en-US")
	if got := en("site.component.video.title", "原中文"); got != "视频" {
		t.Fatalf("应回退默认语言: %q", got)
	}
	if got := en("site.component.missing", "原中文"); got != "原中文" {
		t.Fatalf("缺词条应回退 fallback: %q", got)
	}
	if got := en("site.component.missing", ""); got != "site.component.missing" {
		t.Fatalf("fallback 为空应返回 key: %q", got)
	}

	if got := en("site.component.only.fr", "原文"); got != "原文" {
		t.Fatalf("不应随机取其他语言: %q", got)
	}

	if got := Snapshot("")("site.component.video.title", "原中文"); got != "视频" {
		t.Fatalf("空语言应取默认语言: %q", got)
	}
}
