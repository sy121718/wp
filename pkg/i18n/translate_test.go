package i18n

import "testing"

func withCache(t *testing.T, data map[string]map[string]string) {
	t.Helper()
	cache.Update(data, map[string]int{})
	t.Cleanup(func() { cache.Update(map[string]map[string]string{}, map[string]int{}) })
}

func TestTranslateFallbackChain(t *testing.T) {
	withCache(t, map[string]map[string]string{
		"site.component.gallery.prev": {"zh-CN": "上一张", "en-US": "Previous"},
		"site.component.video.title":  {"zh-CN": "视频"},
	})

	if got := Translate("site.component.gallery.prev", "原中文", "en-US"); got != "Previous" {
		t.Fatalf("命中 en-US 应返回译文，实际 %q", got)
	}

	if got := Translate("site.component.video.title", "原中文", "en-US"); got != "视频" {
		t.Fatalf("当前语言缺失应回退默认语言，实际 %q", got)
	}

	if got := Translate("site.component.not.exists", "站点导航", "en-US"); got != "站点导航" {
		t.Fatalf("缺词条应回退 fallback，实际 %q", got)
	}

	if got := Translate("site.component.not.exists", "", "en-US"); got != "site.component.not.exists" {
		t.Fatalf("fallback 为空应返回 key，实际 %q", got)
	}

	if got := Translate("", "兜底", "en-US"); got != "兜底" {
		t.Fatalf("空 key 应返回 fallback，实际 %q", got)
	}
	if got := Translate("  ", "", "en-US"); got != "" {
		t.Fatalf("空 key 且无 fallback 应返回空 key 本身，实际 %q", got)
	}

	if got := Translate("site.component.gallery.prev", "原中文", ""); got != "上一张" {
		t.Fatalf("空语言应取默认语言，实际 %q", got)
	}
}

func TestTranslateFuncBindsLang(t *testing.T) {
	withCache(t, map[string]map[string]string{
		"site.component.form.submit": {"zh-CN": "提交", "en-US": "Submit"},
	})

	en := TranslateFunc("en-US")
	if got := en("site.component.form.submit", "提交"); got != "Submit" {
		t.Fatalf("en-US 取词应返回 Submit，实际 %q", got)
	}
	zh := TranslateFunc("zh-CN")
	if got := zh("site.component.form.submit", "提交"); got != "提交" {
		t.Fatalf("zh-CN 取词应返回提交，实际 %q", got)
	}

	if got := en("site.component.missing", "原中文"); got != "原中文" {
		t.Fatalf("缺词条应回退 fallback，实际 %q", got)
	}
}
