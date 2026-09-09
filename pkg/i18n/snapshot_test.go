package i18n

// snapshot_test.go — 构建期冻结快照（多语言 P2，docs/06-D §2.3 第 2 条 / §12）。
//
// 与 translate_test.go 共用 withCache（本包测试串行执行）。
// 断言三件事：
//  1. 快照冻结：创建后缓存更新不影响已持有的取词函数；
//  2. 兜底链固定为「当前语言 → 默认语言 → fallback → key」；
//  3. 不做「遍历所有可用语言」的随机兜底（构建期确定性）。

import "testing"

// TestSnapshotFreezesCache 快照不受后续缓存刷新影响。
func TestSnapshotFreezesCache(t *testing.T) {
	withCache(t, map[string]map[string]string{
		"site.component.form.submit": {"zh-CN": "提交", "en-US": "Submit"},
	})
	en := Snapshot("en-US")
	if got := en("site.component.form.submit", "提交"); got != "Submit" {
		t.Fatalf("快照首次取词错误: %q", got)
	}

	// 构建中途改文案（等价 20s 自动刷新 / Reload）：快照必须保持旧值。
	cache.Update(map[string]map[string]string{
		"site.component.form.submit": {"zh-CN": "提交（新）", "en-US": "Send"},
	}, map[string]int{})
	if got := en("site.component.form.submit", "提交"); got != "Submit" {
		t.Fatalf("快照被后续刷新污染: %q", got)
	}
	// 实时取词（请求期语义）看得到新值——两条路径互不干扰。
	if got := Translate("site.component.form.submit", "提交", "en-US"); got != "Send" {
		t.Fatalf("实时取词应看到新值: %q", got)
	}
}

// TestSnapshotFallbackChain 快照兜底链：当前语言 → 默认语言 → fallback → key。
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
	// 只有 fr-FR 词条：快照不得「随机取一个可用语言」，必须回退 fallback
	// （cache.Get 的第三级兜底按 map 随机顺序，构建期会抖动）。
	if got := en("site.component.only.fr", "原文"); got != "原文" {
		t.Fatalf("不应随机取其他语言: %q", got)
	}
	// 空语言 → 默认语言。
	if got := Snapshot("")("site.component.video.title", "原中文"); got != "视频" {
		t.Fatalf("空语言应取默认语言: %q", got)
	}
}
