package productservice

import (
	"testing"
	"time"

	productmodel "go_wp/internal/module/product/model"
)

func TestPublishRowsOfAggregatesLangs(t *testing.T) {
	old := time.Date(2026, 10, 1, 8, 0, 0, 0, time.UTC)
	fresh := time.Date(2026, 10, 20, 9, 30, 0, 0, time.UTC)
	rows := publishRowsOf([]productmodel.PublishRecordPageRow{
		{ProductID: "p1", ProductName: "商品一", ProductSlug: "one", Facts: []productmodel.PublishRecordFactRow{
			{Lang: "en-US", Stale: true},
			{Lang: "zh-CN", ActivePath: "/products/one", Active: true, PublishedAt: old, ArtifactVersion: 3, ArtifactHash: "h3"},
			{Lang: "ja-JP", ActivePath: "/ja/products/one", Active: true, PublishedAt: fresh, ArtifactVersion: 5, ArtifactHash: "h5"},
		}},
		{ProductID: "p2", ProductName: "商品二", ProductSlug: "two"},
	})
	if len(rows) != 2 {
		t.Fatalf("两个商品都应有一行，实际 %d 行", len(rows))
	}
	first := rows[0]
	if !first.HasInstance || !first.Online {
		t.Fatalf("p1 应已上线：%+v", first)
	}
	if !first.PublishedAt.Equal(fresh) {
		t.Fatalf("发布时间应取最新的那条语言：期望 %v，得到 %v", fresh, first.PublishedAt)
	}
	if first.ArtifactVersion != 5 || first.ArtifactHash != "h5" {
		t.Fatalf("产物版本应取最大的那条语言：%+v", first)
	}
	if !first.Stale {
		t.Fatal("任一条语言待重建，行级就应标记待重建")
	}

	if first.Langs[0].Lang != "ja-JP" || first.Langs[1].Lang != "zh-CN" || first.Langs[2].Lang != "en-US" {
		t.Fatalf("语言排序不对（已上线在前、新的更前）：%+v", first.Langs)
	}
	second := rows[1]
	if second.HasInstance || second.Online || len(second.Langs) != 0 {
		t.Fatalf("p2 没有发布登记，应是「未建实例」：%+v", second)
	}
}

func TestNormalizePublishStatus(t *testing.T) {
	cases := map[string]string{
		"":          "",
		"  active ": publishFilterActive,
		"draft":     publishFilterDraft,
		"none":      publishFilterNone,
		"bogus":     "",
	}
	for in, want := range cases {
		if got := normalizePublishStatus(in); got != want {
			t.Fatalf("normalizePublishStatus(%q)：期望 %q，得到 %q", in, want, got)
		}
	}
}
