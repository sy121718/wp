package templates

// admin_content_list_filter_test.go — 内容域列表页筛选栏的渲染级契约（审计 02-L §2 P1-12）。
//
// 两类筛选机制、两组判据，缺一不可：
//
//   - 服务端（pages / articles）：模板提交 query，handler 先筛后分页并回显。
//     handler 的过滤条件由各模块测试钉住；本文件钉模板控件与空态。
//   - 客户端（blocks / navigations）：admin.js 按 [data-filter-input] 过滤
//     [data-filter-text] 行、显隐 [data-filter-empty] 提示。三处契约**必须同时存在** ——
//     只给输入框不给行的匹配文本，就是一个「能输入、无反应、也不报错」的死控件
//     （项目在筛选栏上已经踩过一次，见 02-L §0.3 M-3）。
//
// 另外钉住两条容易静默失效的东西：
//   · 缺可选键（SelectedProject / FilteredProject）时整页仍要渲染完 —— Jet 拿缺失键直接参与
//     if 判断会让**整页**在那一行中断（HTTP 仍 200，之后整块 HTML 消失）；
//   · 新词条必须在 413 迁移里中英成对（模板里的中文只是 t() 兜底，词条命中时显示库里的值）。

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// contentFilterTemplate 一个待检查的页面：模板名 + 渲染数据 + 期望的行数。
type contentFilterTemplate struct {
	name  string
	data  map[string]any
	rows  int
	label string
}

// contentFilterClientPages 客户端筛选页的渲染数据（形状取自各页 handler 的真实注入键）。
func contentFilterClientPages() []contentFilterTemplate {
	return []contentFilterTemplate{
		{
			name:  "admin/block/blocks",
			label: "blocks",
			rows:  2,
			data: map[string]any{
				"Headers": []any{map[string]any{
					"ID": "b1", "Name": "站点页眉", "UpdatedAt": "2026-09-18 09:00", "RefCountText": "3 个页面",
				}},
				"Footers": []any{},
				"Blocks": []any{map[string]any{
					"ID": "b2", "Name": "信任徽章", "KindLabel": "区块", "ReuseModeLabel": "全局引用",
					"UpdatedAt": "2026-09-18 09:00", "RefCountText": "2 个页面",
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

// filterTextAttr 抓渲染结果里的 data-filter-text 值。
var filterTextAttr = regexp.MustCompile(`data-filter-text="([^"]*)"`)

// TestContentListFilterBarIsWired 客户端筛选栏的三处契约必须同时在场。
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
			// 行侧契约：每一行都得给可匹配文本，否则筛选就是个死控件 ——
			// 输入框能打字、也不报错，只是什么都没有发生。
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

// TestContentListFilterBarSurvivesEmptyData 空数据时筛选栏照常在。
//
// 空态两档的分工是 —— 筛选栏告诉用户「你可以筛」，「本来就没数据」的空态告诉他去创建；
// 筛选栏跟着数据一起消失，用户就只剩一个「没有数据」的结论（还以为是筛选条件的问题）。
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

// TestPagesListFilterBarServerSide 服务端筛选栏：控件、回显、单工程不渲染。
func TestPagesListFilterBarServerSide(t *testing.T) {
	base := func(projects []any) map[string]any {
		return map[string]any{
			"Projects": projects, "Pages": []any{}, "Blueprints": []any{},
			"Err": "", "Done": "",
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
		// 直接渲染模板的单测就是这个形态：handler 之外的调用方不会给这两个键。
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

// TestPagesListEmptyStateTwoTiers 空态两档由 FilteredProject 驱动（判据在 handler，模板只读）。
func TestPagesListEmptyStateTwoTiers(t *testing.T) {
	data := func(filtered bool) map[string]any {
		return map[string]any{
			"Projects":        []any{map[string]any{"ID": "p1", "Name": "官网"}, map[string]any{"ID": "p2", "Name": "活动站"}},
			"SelectedProject": "p2", "Pages": []any{}, "Blueprints": []any{},
			"FilteredProject": filtered,
			"Err":             "", "Done": "",
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

// TestClientFilterContractIsImplementedInAdminJS 模板侧契约必须有人消费。
//
// 判据是 JS 里那三处选择器都在：只有模板给出 [data-filter-input] 而 admin.js 不认它，
// 就是「能输入、无反应、也不报错」的死控件 —— 这正是本批要防的那一类缺陷。
func TestClientFilterContractIsImplementedInAdminJS(t *testing.T) {
	src, err := os.ReadFile(filepath.Join("static", "js", "admin.js"))
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

// TestContentListFilterI18nKeysSeeded 新增词条必须中英成对落进 413 迁移。
//
// 模板里的中文只是 t() 兜底：词条命中时显示的是库里的值，缺 en-US 行时英文界面永远显示中文。
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

	sqlPath := filepath.Join("..", "..", "public", "migrations", "413_i18n_content_list_filter.sql")
	src, err := os.ReadFile(sqlPath)
	if err != nil {
		t.Fatalf("读 413 迁移失败: %v", err)
	}
	sqlText := string(src)

	seedRe := regexp.MustCompile(`\('([^']+)',\s*'(zh-CN|en-US)'`)
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
			t.Errorf("词条 %q 不在 413 迁移里：英文界面上会一直显示中文", key)
			continue
		}
		if !got["zh-CN"] || !got["en-US"] {
			t.Errorf("词条 %q 语言不成对：zh-CN=%v en-US=%v", key, got["zh-CN"], got["en-US"])
		}
	}

	// 反向：模板必须真的取用这些 key（写了词条却没人取，等于词条表里的一堆死行）。
	for _, key := range wantKeys {
		if !contentFilterTemplateUsesKey(t, key) {
			t.Errorf("词条 %q 没有任何模板取用（词条与模板失去同步）", key)
		}
	}
}

// contentFilterTemplateUsesKey 在四个相关模板里找这个 key 的取词点。
func contentFilterTemplateUsesKey(t *testing.T, key string) bool {
	t.Helper()
	for _, name := range []string{"articles.html", "pages.html", "blocks.html", "navigations.html"} {
		// 按 basename 递归找（模板已按后端模块分进子目录），别硬编码 admin/ 下的路径。
		src := adminTemplateSource(t, name)
		if strings.Contains(src, `"`+key+`"`) {
			return true
		}
	}
	return false
}
