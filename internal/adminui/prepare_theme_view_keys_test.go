package adminui_test

import (
	"testing"

	"go_wp/internal/adminui"
)

// ViewMap 的键名规则：默认取字段名，只有字段带 view tag 才换名。
// /admin/themes 与 /admin/themes/settings 曾经整页 500，就是因为两页的
// struct 视图字段名（SelectedProjectID / ThemeSettingsJSON）与模板取的
// 键名（SelectedProject / ThemeSettings）漂了，而字段上当时没有 view tag。
//
// 这里用真实 ViewMap 钉住这两个字段的对外键名；模板侧另有一条真渲染用例
// （internal/templates/admin_project_theme_view_keys_test.go）按同名 tag 造
// data 渲染整页。两段合起来等价于「结构体经 ViewMap 后渲染通过」——
// 之所以不在一个包内端到端，是因为 internal/templates 不能 import adminui
// （adminui → internal/text → internal/templates 会成环）。

type themeManageViewLike struct {
	Title             string
	Menu              string
	SelectedProjectID string `view:"SelectedProject"`
}

type themeSettingsViewLike struct {
	ThemeName         string
	ThemeSettingsJSON string `view:"ThemeSettings"`
}

func TestViewMapRenamesTaggedFieldsForThemePages(t *testing.T) {
	cases := []struct {
		name     string
		view     any
		wantKeys []string
		goneKeys []string
	}{
		{
			name:     "主题管理页",
			view:     &themeManageViewLike{Title: "主题管理", Menu: "themes", SelectedProjectID: "p2"},
			wantKeys: []string{"SelectedProject"},
			goneKeys: []string{"SelectedProjectID"},
		},
		{
			name:     "主题设置页",
			view:     &themeSettingsViewLike{ThemeName: "默认主题", ThemeSettingsJSON: `{"colors":{}}`},
			wantKeys: []string{"ThemeSettings"},
			goneKeys: []string{"ThemeSettingsJSON"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := adminui.ViewMap(tc.view)
			for _, key := range tc.wantKeys {
				if _, ok := got[key]; !ok {
					t.Fatalf("ViewMap 后缺少 %q（模板按这个名字取值，缺了整页渲染中断）；现有键：%v", key, keysOfViewMap(got))
				}
			}
			for _, key := range tc.goneKeys {
				if _, ok := got[key]; ok {
					t.Fatalf("ViewMap 后不该还有 %q —— 模板取的是 tag 名", key)
				}
			}
		})
	}
}

func TestViewMapKeepsFieldNameWithoutTag(t *testing.T) {
	// 对照：没有 tag 的字段照旧用字段名。这条防止有人把「所有字段都改 tag」
	// 当成修法，反而把别的页面键名一起改掉。
	got := adminui.ViewMap(&themeManageViewLike{Title: "主题管理"})
	if _, ok := got["Title"]; !ok {
		t.Fatalf("无 tag 的字段应保留字段名 Title；现有键：%v", keysOfViewMap(got))
	}
}

func keysOfViewMap(m map[string]any) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
