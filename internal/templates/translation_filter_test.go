package templates

import (
	"strings"
	"testing"

	"github.com/CloudyKit/jet/v6"
)

func TestTranslationEmptyActions(t *testing.T) {
	set := jet.NewSet(jet.NewOSFileSystemLoader("."), jet.WithTemplateNameExtensions([]string{"", ".html"}))
	cases := []struct {
		name     string
		template string
		data     map[string]any
		want     string
		link     string
	}{
		{"article-empty", "admin/content/article_translations", map[string]any{"Groups": []articleTranslationGroup{}, "Keyword": "", "HasData": false}, "没有可翻译的文章内容", "/admin/articles"},
		{"article-filter", "admin/content/article_translations", map[string]any{"Groups": []articleTranslationGroup{}, "Keyword": "missing", "HasData": true}, "没有匹配的文章", "清除筛选"},
		{"navigation-project", "admin/navigation/navigation_translations", map[string]any{"Groups": []navigationTranslationGroup{}, "NoProject": true}, "还没有站点工程", "/admin/pages"},
		{"navigation-empty", "admin/navigation/navigation_translations", map[string]any{"Groups": []navigationTranslationGroup{}, "NoProject": false, "HasData": false}, "没有可翻译的菜单文字", "/admin/navigations?project=p1"},
		{"navigation-filter", "admin/navigation/navigation_translations", map[string]any{"Groups": []navigationTranslationGroup{{Kind: "header", Rows: []navigationTranslationRow{}}}, "NoProject": false, "HasData": true, "VisibleCount": 0, "Keyword": "missing"}, "没有匹配的菜单文字", "清除筛选"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			data := map[string]any{
				"title": "译文", "menu": "translation", "lang": "en-US", "t": TranslateFunc("en-US"),
				"csrf_token": "tok", "Lang": "en-US", "Langs": []translationLangOption{},
				"ProjectID": "p1", "Errors": []string{}, "RowCount": 0, "Done": 0, "Total": 0,
				"Saved": false, "SavedNote": "", "Page": 1, "Limit": 20,
			}
			for k, v := range tc.data {
				data[k] = v
			}
			out, err := render(t, set, tc.template, data)
			if err != nil {
				t.Fatal(err)
			}
			for _, needle := range []string{tc.want, tc.link, `class="empty-actions"`, "</html>"} {
				if !strings.Contains(out, needle) {
					t.Fatalf("空态缺少 %q", needle)
				}
			}
		})
	}
}
