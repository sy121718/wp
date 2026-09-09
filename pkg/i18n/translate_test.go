package i18n

// translate_test.go — 取词兜底链（多语言 P1/P4 共用的唯一实现）。
//
// 直接注入内存缓存（不查库），逐级验证：当前语言 → 默认语言 → fallback → key，
// 并确认任何一层缺失都不会返回空串、不会 panic。

import "testing"

// withCache 临时替换全局缓存并在测试结束后清空（本包测试串行执行）。
func withCache(t *testing.T, data map[string]map[string]string) {
	t.Helper()
	cache.Update(data, map[string]int{})
	t.Cleanup(func() { cache.Update(map[string]map[string]string{}, map[string]int{}) })
}

// TestTranslateFallbackChain 四级兜底链逐级断言。
func TestTranslateFallbackChain(t *testing.T) {
	withCache(t, map[string]map[string]string{
		"site.component.gallery.prev": {"zh-CN": "上一张", "en-US": "Previous"},
		"site.component.video.title":  {"zh-CN": "视频"},
	})

	// 1) 命中当前语言
	if got := Translate("site.component.gallery.prev", "原中文", "en-US"); got != "Previous" {
		t.Fatalf("命中 en-US 应返回译文，实际 %q", got)
	}
	// 2) 当前语言缺失 → 回退默认语言（zh-CN）
	if got := Translate("site.component.video.title", "原中文", "en-US"); got != "视频" {
		t.Fatalf("当前语言缺失应回退默认语言，实际 %q", got)
	}
	// 3) key 不存在 → fallback（组件包内中文原文）
	if got := Translate("site.component.not.exists", "站点导航", "en-US"); got != "站点导航" {
		t.Fatalf("缺词条应回退 fallback，实际 %q", got)
	}
	// 4) key 不存在且 fallback 为空 → 返回 key 本身（绝不空串）
	if got := Translate("site.component.not.exists", "", "en-US"); got != "site.component.not.exists" {
		t.Fatalf("fallback 为空应返回 key，实际 %q", got)
	}
	// 空 key → fallback / key，同样不 panic、不空串
	if got := Translate("", "兜底", "en-US"); got != "兜底" {
		t.Fatalf("空 key 应返回 fallback，实际 %q", got)
	}
	if got := Translate("  ", "", "en-US"); got != "" {
		t.Fatalf("空 key 且无 fallback 应返回空 key 本身，实际 %q", got)
	}

	// 语言为空 → 默认语言
	if got := Translate("site.component.gallery.prev", "原中文", ""); got != "上一张" {
		t.Fatalf("空语言应取默认语言，实际 %q", got)
	}
}

// TestTranslateFuncBindsLang TranslateFunc 绑定语言且与 Translate 行为一致。
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
	// 缺词条 → fallback；取词函数可安全并发调用（此处仅验证非空语义）
	if got := en("site.component.missing", "原中文"); got != "原中文" {
		t.Fatalf("缺词条应回退 fallback，实际 %q", got)
	}
}
