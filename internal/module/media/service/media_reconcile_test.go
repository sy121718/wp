package mediaservice

import (
	"testing"

	mediamodel "go_wp/internal/module/media/model"
)

func TestMediaFileAttachmentID(t *testing.T) {
	yes := []struct {
		path string
		id   uint64
	}{
		{"12.png", 12},
		{"a/b/1024.jpg", 1024},

		{"7_thumb.jpg", 7},
		{"7_medium.jpg", 7},
		{"7_full.jpg", 7},

		{"7_webp.jpg", 7},
		{"7_webp-1-ab12cd34.jpg", 7},

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
		"1699999999_ab12cd.png",
		"logo.png",
		"tmp/replace_3_1.png",
		"7_thumb.png",
		"7_thumb-1-AB12CD34.jpg",
		"7_thumb-1-ab12cd3.jpg",
		"7_thumb-ab12cd34.jpg",
		"12.jpg.bak",
		"",
	}
	for _, path := range no {
		if id, ok := mediaFileAttachmentID(path); ok {
			t.Fatalf("%q 不该被反向归属到附件 id（实际归到 %d）；误报会让整份对账结果不可信", path, id)
		}
	}
}

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

func TestMediaVariantBackfillReason(t *testing.T) {

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
