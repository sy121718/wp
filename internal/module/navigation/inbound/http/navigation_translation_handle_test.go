package navigationhttp

import (
	"strings"
	"testing"

	"go_wp/pkg/i18n"
)

func TestNavigationTranslationsFilterPreservesCrossKindInvalidation(t *testing.T) {
	shared := i18n.ContentHash("共用菜单")
	other := i18n.ContentHash("另一菜单")
	data := &navigationTranslationsData{
		RowCount: 2,
		HasData:  true,
		Groups: []navigationTranslationGroup{
			{Kind: "header", Rows: []navigationTranslationRow{
				{Source: "共用菜单", Path: "/shared", SourceHash: shared},
				{Source: "另一菜单", Path: "/other", SourceHash: other},
			}},
			{Kind: "footer", Rows: []navigationTranslationRow{}},
		},
	}
	data.noteHashKind(shared, "header")
	data.noteHashKind(shared, "footer")
	data.filter("shared")
	if data.VisibleCount != 1 || len(data.Groups[0].Rows) != 1 || data.Groups[0].Rows[0].SourceHash != shared {
		t.Fatalf("按完整树中路径筛选后展示行不正确: %#v", data.Groups)
	}
	kinds := data.kindsForHashes(map[string]bool{shared: true}, nil)
	if len(kinds) != 2 || kinds[0] != "header" || kinds[1] != "footer" {
		t.Fatalf("过滤后共用译文仍需派发两个位置失效，got %v", kinds)
	}
	// 请求各自重新 build 完整树，不可在已裁剪的 Groups 上反复筛选。
	data.Groups[0].Rows = []navigationTranslationRow{
		{Source: "共用菜单", Path: "/shared", SourceHash: shared},
		{Source: "另一菜单", Path: "/other", SourceHash: other},
	}
	data.filter("不存在")
	if data.VisibleCount != 0 || !data.HasData || data.Keyword != "不存在" {
		t.Fatalf("已有菜单但筛选零匹配应单独分档: %#v", data)
	}
}

func TestNavigationTranslationFilteredLocationPreservesQuery(t *testing.T) {
	location := navigationTranslationFilteredLocation("project-1", "en-US", "导航 & 菜单")
	if strings.Contains(location, "saved=") || strings.Contains(location, "n=") {
		t.Fatalf("回跳地址不应再带结论文案：%s", location)
	}
	for _, part := range []string{"project=project-1", "lang=en-US", "keyword="} {
		if !strings.Contains(location, part) {
			t.Fatalf("保存回跳遗失 %q: %s", part, location)
		}
	}
}
