package productservice

// product_publish_record_page_test.go — 发布记录页的纯函数口径（B9）。
//
// 这一屏拼错的地方都不会报错：状态筛错只是少几行、聚合取错只是显示旧版本号。
// 所以把三处判断钉住：状态筛的四档、跨语言聚合取哪个发布时间/版本号、页码收敛。

import (
	"testing"
	"time"

	productdto "go_wp/internal/module/product/dto"
	productmodel "go_wp/internal/module/product/model"
)

func TestPublishRowMatchesStatus(t *testing.T) {
	live := productdto.PublishRecordRowResp{HasInstance: true, Online: true}
	draft := productdto.PublishRecordRowResp{HasInstance: true}
	none := productdto.PublishRecordRowResp{}
	cases := []struct {
		name string
		row  productdto.PublishRecordRowResp
		want map[string]bool
	}{
		{"已上线", live, map[string]bool{"": true, "active": true, "draft": false, "none": false}},
		{"有实例未上线", draft, map[string]bool{"": true, "active": false, "draft": true, "none": false}},
		{"未建实例", none, map[string]bool{"": true, "active": false, "draft": false, "none": true}},
	}
	for _, c := range cases {
		for status, want := range c.want {
			if got := publishRowMatchesStatus(c.row, status); got != want {
				t.Fatalf("%s + status=%q：期望 %v，得到 %v", c.name, status, want, got)
			}
		}
	}
}

// 「待重建」不参与状态筛选：线上有旧产物的商品仍是已上线（否则想看线上内容的人看不到它）。
func TestPublishRowMatchesStatusIgnoresStale(t *testing.T) {
	row := productdto.PublishRecordRowResp{HasInstance: true, Online: true, Stale: true}
	if !publishRowMatchesStatus(row, publishFilterActive) {
		t.Fatal("已上线但待重建的行必须仍算 active")
	}
}

func TestMergePublishRowsAggregatesLangs(t *testing.T) {
	old := time.Date(2026, 10, 1, 8, 0, 0, 0, time.UTC)
	fresh := time.Date(2026, 10, 20, 9, 30, 0, 0, time.UTC)
	products := []*productmodel.ProductEntity{
		{ID: "p1", Name: "商品一", Slug: "one"},
		{ID: "p2", Name: "商品二", Slug: "two"},
	}
	langs := map[string][]productdto.PublishRecordLangResp{
		"p1": {
			{Lang: "en-US", Online: false, Stale: true},
			{Lang: "zh-CN", Online: true, OnlinePath: "/products/one", PublishedAt: old, ArtifactVersion: 3, ArtifactHash: "h3"},
			{Lang: "ja-JP", Online: true, OnlinePath: "/ja/products/one", PublishedAt: fresh, ArtifactVersion: 5, ArtifactHash: "h5"},
		},
		// p2 在库里没有发布登记：那一行必须仍出现（HasInstance=false，不参与聚合）。
	}
	rows := mergePublishRows(products, langs, "")
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
	// 已上线在前：zh-CN（旧）与 ja-JP（新）都在前，且新的更靠前。
	if first.Langs[0].Lang != "ja-JP" || first.Langs[1].Lang != "zh-CN" || first.Langs[2].Lang != "en-US" {
		t.Fatalf("语言排序不对（已上线在前、新的更前）：%+v", first.Langs)
	}
	second := rows[1]
	if second.HasInstance || second.Online || len(second.Langs) != 0 {
		t.Fatalf("p2 没有发布登记，应是「未建实例」：%+v", second)
	}
}

func TestMergePublishRowsFiltersByStatus(t *testing.T) {
	products := []*productmodel.ProductEntity{{ID: "p1", Name: "商品一"}, {ID: "p2", Name: "商品二"}}
	langs := map[string][]productdto.PublishRecordLangResp{
		"p1": {{Lang: "zh-CN", Online: true}},
	}
	if rows := mergePublishRows(products, langs, publishFilterActive); len(rows) != 1 || rows[0].ProductID != "p1" {
		t.Fatalf("active 只应留下已上线的那一行：%+v", rows)
	}
	if rows := mergePublishRows(products, langs, publishFilterNone); len(rows) != 1 || rows[0].ProductID != "p2" {
		t.Fatalf("none 只应留下没有实例的那一行：%+v", rows)
	}
}

func TestNormalizePublishStatus(t *testing.T) {
	cases := map[string]string{
		"":          "",
		"  active ": publishFilterActive,
		"draft":     publishFilterDraft,
		"none":      publishFilterNone,
		"bogus":     "", // 查询串不是可信边界：未知值退回「全部」
	}
	for in, want := range cases {
		if got := normalizePublishStatus(in); got != want {
			t.Fatalf("normalizePublishStatus(%q)：期望 %q，得到 %q", in, want, got)
		}
	}
}

func TestClampPublishPageAndSlice(t *testing.T) {
	rows := make([]productdto.PublishRecordRowResp, 25)
	for i := range rows {
		rows[i].ProductID = "p"
	}
	if got := clampPublishPage(0, 25, 20); got != 1 {
		t.Fatalf("页码 <1 应收敛成 1，得到 %d", got)
	}
	if got := clampPublishPage(9, 25, 20); got != 2 {
		t.Fatalf("越界页码应收敛到末页 2，得到 %d", got)
	}
	if got := clampPublishPage(1, 0, 20); got != 1 {
		t.Fatalf("空集时页码保持 1，得到 %d", got)
	}
	if got := pagePublishRows(rows, 2, 20); len(got) != 5 {
		t.Fatalf("第二页应有 5 行，得到 %d", len(got))
	}
	if got := pagePublishRows(rows, 9, 20); len(got) != 0 {
		t.Fatalf("越界页应回空切片而不是 panic，得到 %d 行", len(got))
	}
}
