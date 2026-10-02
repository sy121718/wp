package mediaservice

// media_reconcile_test.go — 存储对账的纯逻辑护栏（不连库）。
//
// 这里断言的是**判定结果**，不是「函数没报错」：
//   · 反向归属规则（哪个文件名能算作某个附件的产物、哪些只能计为「无法归属」）；
//   · 变体补偿的触发条件（缺记录 / 条数不齐 / 非 ready 才补，全 ready 一律跳过）；
//   · 清单截断语义（截断只置标记，不静默丢行表达）。

import (
	"testing"

	mediamodel "go_wp/internal/module/media/model"
)

// TestMediaFileAttachmentID 反向对账的归属规则：只有 <id>.<ext> / <id>_<type>.jpg /
// <id> 才敢归到附件 id；存量随机名（<unixNano>_<hex>.<ext>）一律不归属 ——
// 它同样以数字开头，笼统按「数字开头」归属会把每一张历史图片都报成孤儿。
func TestMediaFileAttachmentID(t *testing.T) {
	yes := []struct {
		path string
		id   uint64
	}{
		{"12.png", 12},
		{"a/b/1024.jpg", 1024},
		// 旧格式（无指纹）：存量文件与历史产物引用的都是这种，必须继续认 ——
		// 只认新名会把整批存量变体报成「有文件没记录」。
		{"7_thumb.jpg", 7},
		{"7_medium.jpg", 7},
		{"7_full.jpg", 7},
		// 历史槽位名 webp：迁移 496 只改 DB 侧的 variant_type，磁盘上那批 *_webp.jpg
		// **不会被改名** —— 反查必须继续认它们，否则存量文件从「可归属」退化成「无法归属」。
		{"7_webp.jpg", 7},
		{"7_webp-1-ab12cd34.jpg", 7},
		// 新格式（带内容指纹）：<generation>-<sha256 前 8 位小写 hex>。
		{"7_thumb-1-ab12cd34.jpg", 7},
		{"7_medium-12-0f1e2d3c.jpg", 7},
		{"a/b/7_full-99-deadbeef.jpg", 7},
		{"99.webp", 99},
		{"5", 5},
	}
	for _, c := range yes {
		id, ok := mediaFileAttachmentID(c.path)
		if !ok {
			t.Fatalf("%q 应按命名约定归属到附件 id，实际未命中", c.path)
		}
		if id != c.id {
			t.Fatalf("%q 的 id 分量应为 %d，实际 %d", c.path, c.id, id)
		}
	}
	no := []string{
		"1699999999_ab12cd.png",  // 历史随机名（时间戳_随机 hex）
		"logo.png",               // 有语义的原名
		"tmp/replace_3_1.png",    // 换图暂存（由 tmp/ 前缀单独识别）
		"7_thumb.png",            // 变体只可能是 .jpg（别的形状不是本系统的产物）
		"7_thumb-1-AB12CD34.jpg", // 指纹只认小写 hex（大写是另一套命名，不猜）
		"7_thumb-1-ab12cd3.jpg",  // 7 位指纹：位数不符
		"7_thumb-ab12cd34.jpg",   // 有指纹无 generation：两段必须成对出现
		"12.jpg.bak",
		"",
	}
	for _, path := range no {
		if id, ok := mediaFileAttachmentID(path); ok {
			t.Fatalf("%q 不该被反向归属到附件 id（实际归到 %d）；误报会让整份对账结果不可信", path, id)
		}
	}
}

// TestMediaNormalizeStorageKey 路径归一化：带不带前导斜杠、反斜杠、首尾空白都归到同一形态，
// 否则「已知集合」与「磁盘遍历结果」永远对不上，会把所有文件都报成孤儿。
func TestMediaNormalizeStorageKey(t *testing.T) {
	cases := map[string]string{
		"/12.png":        "12.png",
		"12.png":         "12.png",
		"  /a/b/c.jpg  ": "a/b/c.jpg",
		"a\\b.jpg":       "a/b.jpg",
		"":               "",
	}
	for in, want := range cases {
		if got := normalizeStorageKey(in); got != want {
			t.Fatalf("normalizeStorageKey(%q) = %q，期望 %q", in, got, want)
		}
	}
}

// TestMediaParseUint64 宽松解析：非数字 / 溢出 / 空串都不产生一个「看似合法」的 id。
func TestMediaParseUint64(t *testing.T) {
	if got := parseUint64("1024"); got != 1024 {
		t.Fatalf("parseUint64(1024) = %d", got)
	}
	for _, bad := range []string{"", "12a", "a12", "-1", "99999999999999999999"} {
		if got := parseUint64(bad); got != 0 {
			t.Fatalf("parseUint64(%q) 应返回 0（非法 id 不能参与归属），实际 %d", bad, got)
		}
	}
}

// TestMediaVariantBackfillReason 补偿触发条件：只有真的不齐 / 未就绪才重放。
//
// 全 ready 却仍被判为需要补偿 → 每次调用都把全部附件重新生成一遍（幂等但白烧 CPU/IO）；
// 非 ready 却被判为不需要 → 存量永远补不齐。
func TestMediaVariantBackfillReason(t *testing.T) {
	// 由 VariantTypes() 派生，不手写列表：档位增删时这里必须自动跟随 ——
	// 手写列表会让「按设计加了档位」表现成这条用例失败，而它想验的是补偿判据本身。
	ready := func() []mediamodel.MediaVariantEntity {
		rows := make([]mediamodel.MediaVariantEntity, 0, len(mediamodel.VariantTypes()))
		for _, vt := range mediamodel.VariantTypes() {
			rows = append(rows, mediamodel.MediaVariantEntity{
				VariantType: vt, Status: mediamodel.VariantStatusReady,
			})
		}
		return rows
	}
	if got := variantBackfillReason(ready()); got != "" {
		t.Fatalf("三个变体全 ready 不该触发补偿，实际 %q", got)
	}
	if got := variantBackfillReason(nil); got == "" {
		t.Fatal("没有任何变体记录必须触发补偿")
	}
	short := ready()[:2]
	if got := variantBackfillReason(short); got == "" {
		t.Fatal("变体记录条数不齐必须触发补偿")
	}
	pending := ready()
	pending[0].Status = mediamodel.VariantStatusPending
	if got := variantBackfillReason(pending); got == "" {
		t.Fatal("存在 pending 变体必须触发补偿")
	}
	failed := ready()
	failed[1].Status = mediamodel.VariantStatusFailed
	if got := variantBackfillReason(failed); got == "" {
		t.Fatal("存在 failed 变体必须触发补偿")
	}
}

// TestMediaAppendItemTruncation 清单截断：超出上限时置 Truncated，且不增长列表。
func TestMediaAppendItemTruncation(t *testing.T) {
	truncated := false
	var list []ReconcileItem
	for i := 0; i < 5; i++ {
		list = appendItem(list, ReconcileItem{Kind: ReconcileKindOrphanFile, ID: uint64(i)}, 3, &truncated)
	}
	if len(list) != 3 {
		t.Fatalf("清单应被截断到 3 条，实际 %d", len(list))
	}
	if !truncated {
		t.Fatal("截断后必须置 Truncated（否则页面上看起来「就这些」）")
	}
	truncated = false
	list = appendItem(list, ReconcileItem{Kind: ReconcileKindOrphanFile, ID: 9}, 10, &truncated)
	if truncated {
		t.Fatal("未超上限不该置 Truncated")
	}
}
