package templates

import (
	"os"
	"time"

	"go_wp/pkg/utils"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

type contentFilterTemplate struct {
	name  string
	data  map[string]any
	rows  int
	label string
}

func contentFilterClientPages() []contentFilterTemplate {
	return []contentFilterTemplate{
		{
			name:  "admin/block/blocks",
			label: "blocks",
			rows:  2,
			data: map[string]any{
				"Headers": []any{map[string]any{
					"ID": "b1", "Name": "站点页眉", "Kind": "header", "ReuseMode": "global", "UpdatedAt": utils.NewJSONTime(time.Date(2026, 9, 18, 9, 0, 0, 0, time.Local)), "RefCount": int64(3),
				}},
				"Footers": []any{},
				"Blocks": []any{map[string]any{
					"ID": "b2", "Name": "信任徽章", "Kind": "trust", "ReuseMode": "global",
					"UpdatedAt": utils.NewJSONTime(time.Date(2026, 9, 18, 9, 0, 0, 0, time.Local)), "RefCount": int64(2),
				}},
				"Projects":        []any{map[string]any{"ID": "p1", "Name": "官网"}},
				"SelectedProject": "p1",
			},
		},
		{
			name:  "admin/navigation/navigations",
			label: "navigations",
			rows:  1,
			data: map[string]any{
				"Rows": []any{map[string]any{
					"ID": "n1", "Title": "关于我们", "Path": "/about", "TargetLabel": "当前窗口",
					"Indent": "0", "Depth": 0, "First": true, "Last": true, "UpdatedAt": "2026-09-18 09:00",
				}},
				"Parents":         []any{},
				"Projects":        []any{map[string]any{"ID": "p1", "Name": "官网"}},
				"SelectedProject": "p1",
				"Kind":            "header",
			},
		},
	}
}

var filterTextAttr = regexp.MustCompile(`data-filter-text="([^"]*)"`)

func TestContentListFilterBarIsWired(t *testing.T) {
	for _, tc := range contentFilterClientPages() {
		tc := tc
		t.Run(tc.label, func(t *testing.T) {
			out := renderAdminEmptyProbe(t, tc.name, tc.data)

			if !strings.Contains(out, `class="filter-bar"`) {
				t.Fatal("没有 .filter-bar：这一页仍然没有筛选入口（02-L P1-12 的原缺陷）")
			}
			if !strings.Contains(out, "data-filter-input") {
				t.Fatal("筛选栏里没有 [data-filter-input]：admin.js 不知道要监听谁")
			}
			if !strings.Contains(out, "data-filter-empty") {
				t.Fatal("没有 [data-filter-empty]：「筛出来是空的」与「本来就没数据」在界面上分不开")
			}

			matches := filterTextAttr.FindAllStringSubmatch(out, -1)
			if len(matches) != tc.rows {
				t.Fatalf("data-filter-text 行数 = %d，期望 %d（有行没给匹配文本：那几行永远筛不出来）",
					len(matches), tc.rows)
			}
			for i, m := range matches {
				if strings.TrimSpace(m[1]) == "" {
					t.Errorf("第 %d 行的 data-filter-text 是空的：这一行只剩「永不匹配」一种命运", i+1)
				}
			}
		})
	}
}

func TestArticlesListFilterBarServerSide(t *testing.T) {
	data := map[string]any{
		"Rows": []any{}, "Total": 0, "Empty": true, "BlogBase": "/blog",
		"Keyword": "摘要甲", "Limit": 20, "ClearFilterURL": "/admin/articles?limit=20",
	}
	out := renderAdminEmptyProbe(t, "admin/content/articles", data)
	for _, want := range []string{`class="filter-bar"`, `method="get" action="/admin/articles"`, `name="keyword" value="摘要甲"`, `name="limit" value="20"`, `href="/admin/articles?limit=20"`, "没有匹配的文章"} {
		if !strings.Contains(out, want) {
			t.Errorf("文章服务端筛选缺少 %q", want)
		}
	}
	if strings.Contains(out, "data-filter-input") || strings.Contains(out, "data-filter-empty") {
		t.Error("文章列表仍混用客户端当前页筛选")
	}
}

func TestContentListFilterBarSurvivesEmptyData(t *testing.T) {
	cases := []struct {
		name string
		data map[string]any
	}{
		{"admin/content/articles", map[string]any{"Rows": []any{}, "Total": 0, "Empty": true, "BlogBase": "/blog"}},
		{"admin/block/blocks", map[string]any{
			"Headers": []any{}, "Footers": []any{}, "Blocks": []any{},
			"Projects": []any{map[string]any{"ID": "p1", "Name": "官网"}}, "SelectedProject": "p1",
		}},
		{"admin/navigation/navigations", map[string]any{
			"Rows": []any{}, "Parents": []any{},
			"Projects":        []any{map[string]any{"ID": "p1", "Name": "官网"}},
			"SelectedProject": "p1", "Kind": "header",
		}},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			out := renderAdminEmptyProbe(t, tc.name, tc.data)
			if !strings.Contains(out, `class="filter-bar"`) {
				t.Error("空数据时筛选栏消失了：用户失去了「我是不是筛错了」的最后一条线索")
			}
			if tc.name != "admin/content/articles" && !strings.Contains(out, "data-filter-input") {
				t.Error("空数据时客户端筛选输入框不在：清空关键词的路径断了")
			}
			if tc.name == "admin/content/articles" && !strings.Contains(out, `name="keyword"`) {
				t.Error("空数据时文章服务端关键词筛选框不在")
			}
		})
	}
}

func TestPagesListFilterBarServerSide(t *testing.T) {
	base := func(projects []any) map[string]any {
		return map[string]any{
			"Projects": projects, "Pages": []any{}, "Blueprints": []any{},

			"Err": map[string]any{"Key": "", "Fallback": ""}, "Done": map[string]any{"Key": "", "Fallback": ""},
		}
	}
	twoProjects := []any{
		map[string]any{"ID": "p1", "Name": "官网"},
		map[string]any{"ID": "p2", "Name": "活动站"},
	}

	t.Run("多工程→筛选栏+回显", func(t *testing.T) {
		data := base(twoProjects)
		data["SelectedProject"] = "p2"
		out := renderAdminEmptyProbe(t, "admin/page/pages", data)

		if !strings.Contains(out, `class="filter-bar"`) {
			t.Fatal("页面列表没有筛选栏（02-L P1-12：这一页只能看第一个工程）")
		}
		if !strings.Contains(out, `name="project"`) {
			t.Fatal("筛选栏里没有 name=\"project\"：提交的 query 键与 handler 读的键对不上，等于没筛")
		}
		if !strings.Contains(out, `<option value="p2" selected>活动站</option>`) {
			t.Errorf("选中的工程没有回显：用户看到的是别的工程的数据，下拉却显示另一个")
		}
	})

	t.Run("单工程→不渲染筛选栏", func(t *testing.T) {
		data := base([]any{map[string]any{"ID": "p1", "Name": "官网"}})
		data["SelectedProject"] = "p1"
		out := renderAdminEmptyProbe(t, "admin/page/pages", data)

		if strings.Contains(out, `class="filter-bar"`) {
			t.Error("只有一个工程时渲染了筛选栏：一个恒选项的下拉是噪声，不是能力")
		}
	})

	t.Run("缺可选键→整页仍渲染完（Jet 缺键会让整页中断）", func(t *testing.T) {

		data := base(twoProjects)
		out := renderAdminEmptyProbe(t, "admin/page/pages", data)

		if !strings.Contains(out, `class="filter-bar"`) {
			t.Fatal("缺 SelectedProject 时筛选栏没渲染出来")
		}
		if !strings.Contains(out, "</html>") {
			t.Fatal("缺可选键让整页中断了（HTTP 仍会是 200，之后整块 HTML 消失）")
		}
	})
}

func TestPagesListEmptyStateTwoTiers(t *testing.T) {
	data := func(filtered bool) map[string]any {
		return map[string]any{
			"Projects":        []any{map[string]any{"ID": "p1", "Name": "官网"}, map[string]any{"ID": "p2", "Name": "活动站"}},
			"SelectedProject": "p2", "Pages": []any{}, "Blueprints": []any{},
			"FilteredProject": filtered,
			"Err":             map[string]any{"Key": "", "Fallback": ""}, "Done": map[string]any{"Key": "", "Fallback": ""},
		}
	}

	filtered := renderAdminEmptyProbe(t, "admin/page/pages", data(true))
	if !strings.Contains(filtered, "这个站点工程还没有页面") {
		t.Error("指定了工程却没有页面时，应给「这个站点工程还没有页面」这一档")
	}
	if !strings.Contains(filtered, `colspan="7"`) || !strings.Contains(filtered, "<thead") {
		t.Error("筛选空态把表头一起吃掉了")
	}

	plain := renderAdminEmptyProbe(t, "admin/page/pages", data(false))
	if strings.Contains(plain, "这个站点工程还没有页面") {
		t.Error("没筛过却显示「这个工程还没有页面」：用户会去换一个本来就没有问题的工程")
	}
	if !strings.Contains(plain, "还没有页面") {
		t.Error("默认档的空态文案消失了")
	}
}

func TestClientFilterContractIsImplementedInAdminJS(t *testing.T) {
	src, err := AssetFile("js/admin.js")
	if err != nil {
		t.Fatalf("读 admin.js 失败: %v", err)
	}
	js := string(src)
	for _, want := range []string{"data-filter-input", "data-filter-text", "data-filter-empty"} {
		if !strings.Contains(js, want) {
			t.Errorf("admin.js 里没有消费 %q 的实现：模板里的筛选栏是死控件", want)
		}
	}
}

func TestContentListFilterI18nKeysSeeded(t *testing.T) {
	wantKeys := []string{
		"admin.article.list.filterLabel",
		"admin.article.list.filterPlaceholder",
		"admin.article.list.filterEmpty",
		"admin.pages.filter_project",
		"admin.pages.filter_submit",
		"admin.pages.filter_reset",
		"admin.pages.empty_project_title",
		"admin.pages.empty_project_desc",
		"admin.blocks.filter_keyword",
		"admin.blocks.filter_placeholder",
		"admin.blocks.filter_empty",
		"admin.navigations.filter_keyword",
		"admin.navigations.filter_placeholder",
		"admin.navigations.filter_empty",
	}

	sqlPath := filepath.Join("..", "..", "public", "schema", "tables", "sys_i18n.sql")
	src, err := os.ReadFile(sqlPath)
	if err != nil {
		t.Fatalf("读系统词条失败: %v", err)
	}
	sqlText := string(src)

	seedRe := regexp.MustCompile(`\(\s*\d+,\s*'([^']+)',\s*'(zh-CN|en-US)'`)
	langs := map[string]map[string]bool{}
	for _, m := range seedRe.FindAllStringSubmatch(sqlText, -1) {
		if langs[m[1]] == nil {
			langs[m[1]] = map[string]bool{}
		}
		langs[m[1]][m[2]] = true
	}

	for _, key := range wantKeys {
		got := langs[key]
		if got == nil {
			t.Errorf("词条 %q 不在 public/schema 里：英文界面上会一直显示中文", key)
			continue
		}
		if !got["zh-CN"] || !got["en-US"] {
			t.Errorf("词条 %q 语言不成对：zh-CN=%v en-US=%v", key, got["zh-CN"], got["en-US"])
		}
	}

	for _, key := range wantKeys {
		if !contentFilterTemplateUsesKey(t, key) {
			t.Errorf("词条 %q 没有任何模板取用（词条与模板失去同步）", key)
		}
	}
}

func contentFilterTemplateUsesKey(t *testing.T, key string) bool {
	t.Helper()
	for _, name := range []string{"articles.html", "pages.html", "blocks.html", "navigations.html"} {

		src := adminTemplateSource(t, name)
		if strings.Contains(src, `"`+key+`"`) {
			return true
		}
	}
	return false
}
