package templates

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
	"time"

	"go_wp/pkg/utils"

	"github.com/CloudyKit/jet/v6"
)

var siteStructureTemplates = []string{
	"admin/project/theme",
	"admin/project/theme_settings",
	"admin/page/pages",
	"admin/page/page_translations",
	"admin/block/blocks",
	"admin/navigation/navigations",
	"admin/navigation/navigation_translations",
	"admin/system/menus",
}

func siteStructureData() map[string]any {
	return map[string]any{
		"lang": "zh-CN", "title": "站点结构", "menu": "pages", "t": TranslateFunc("en-US"),
		"csrf_token": "tok",
	}
}

func TestSiteStructureTemplatesParse(t *testing.T) {
	loader := testTemplateLoader()
	set := jet.NewSet(loader, jet.WithTemplateNameExtensions([]string{"", ".html"}))
	injectGlobals(set)
	for _, name := range siteStructureTemplates {
		if _, err := set.GetTemplate(name); err != nil {
			t.Errorf("%s 模板解析失败: %v", name, err)
		}
	}
}

func TestSiteStructureTemplatesRender(t *testing.T) {
	cases := []struct {
		name  string
		data  map[string]any
		wants []string
	}{
		{
			name: "admin/project/theme",
			data: map[string]any{
				"SelectedProject": "p1",
				"Projects":        []map[string]any{{"ID": "p1", "Name": "官网"}},

				"Buttons": map[string]any{"theme.create": true},

				"Err": map[string]any{"Key": "", "Fallback": ""},
				"Themes": []map[string]any{{
					"ID": "t1", "ProjectID": "p1", "Name": "春季促销版", "IsActive": false,
					"CreatedAt": "2026-01-01", "UpdatedAt": "2026-01-02",
				}},
			},
			wants: []string{"主题管理", "主题列表", "名称", "未激活", "设置", "激活", "删除", "确定删除主题「"},
		},
		{
			name: "admin/project/theme_settings",
			data: map[string]any{
				"ThemeName": "春季促销版", "ProjectID": "p1", "ThemeID": "t1",
				"ThemeSettings": "{}",

				"Groups": []map[string]any{{
					"TitleKey": "", "TitleFallback": "颜色",
					"Fields": []map[string]any{
						{"Kind": "select", "LabelKey": "", "LabelFallback": "主色", "Name": "colors.primary", "Value": "",
							"Options": []map[string]any{{"Value": "#111111", "LabelKey": "", "LabelFallback": "深色", "Selected": true}}},
						{"Kind": "datalist", "LabelKey": "", "LabelFallback": "标题字体", "Name": "typography.heading.family", "Value": "",
							"Options": []map[string]any{{"Value": "Inter", "LabelKey": "", "LabelFallback": "Inter"}}},
						{"Kind": "color", "LabelKey": "", "LabelFallback": "背景色", "Name": "surface.bg", "Value": "", "Options": []map[string]any{}},
					},
				}},
				"HeaderBlocks":       []map[string]any{{"ID": "b1", "NameKey": "", "NameFallback": "站点页眉"}},
				"FooterBlocks":       []map[string]any{{"ID": "b2", "NameKey": "", "NameFallback": "站点页脚"}},
				"AnnouncementBlocks": []map[string]any{{"ID": "b3", "NameKey": "", "NameFallback": "公告条"}},
				"HeaderBlockID":      "b1", "FooterBlockID": "b2", "AnnouncementBlockID": "b3",
			},
			wants: []string{"主题设置：", "返回列表", "全局页眉 / 页脚块", "页眉块", "页脚块", "公告条块", "保存并应用到该主题页面", "说明"},
		},
		{
			name: "admin/page/pages",
			data: map[string]any{
				"Projects":   []map[string]any{{"ID": "p1", "Name": "官网"}},
				"Blueprints": []map[string]any{{"ID": "b1", "Name": "落地页"}},

				"Buttons": map[string]any{"project.create": true, "page.create": true},
				"Pages": []map[string]any{{
					"ID": "pg1", "DraftPath": "/about", "Kind": "page", "Active": true, "ActivePath": "/site/about/",
					"Publications": []map[string]any{{"Lang": "zh-CN", "ActivePath": "/site/about/"}},
					"Stale":        true, "Staged": false, "Version": 3, "UpdatedAt": utils.NewJSONTime(time.Date(2026, 1, 2, 0, 0, 0, 0, time.Local)),
				}},
			},

			wants: []string{"新建站点工程", "创建工程", "新建页面", "创建页面", "页面列表", "已发布", "有更新未发布", "编辑", "打开页面", "多语言", `href="/site/about/"`},
		},
		{
			name: "admin/page/page_translations",
			data: map[string]any{
				"PagePath": "/about", "Errors": []any{},
				"PageID": "pg1", "Lang": "en-US",
				"Langs":    []map[string]any{{"Code": "en-US", "Label": "English", "Active": true}},
				"PageDone": 1, "PageTotal": 2, "SiteNote": map[string]any{"Key": "", "Fallback": ""},
				"SiteDone": 1, "SiteTotal": 2,
				"LoadError": map[string]any{"Key": "", "Fallback": ""}, "ParamError": map[string]any{"Key": "", "Fallback": ""},
				"IsDefaultLang": false, "RowCount": 1,
				"Groups": []map[string]any{{
					"Component": "hero",
					"Rows": []map[string]any{{
						"Field": "title", "Rich": true, "Origin": "全站共享", "Source": "关于我们",
						"Target": "", "Context": "hero.title", "SourceHash": "h1", "Limit": 200,
						"Translated": false, "Engine": "", "ReusePages": 2, "ReuseTotal": 3,
						"ReusePaths": []string{"/header", "/footer"},
					}},
				}},
			},
			wants: []string{"多语言 · ", "切换语言", "本页完成度", "全站完成度", "筛选：", "全部", "只看缺失", "只看人工", "只看 AI 翻译", "组件", "字段", "富文本", "填写译文", "上限 ", "缺失", "保存全部", "还用在另外 "},
		},
		{
			name: "admin/block/blocks",
			data: map[string]any{
				"SelectedProject": "p1",
				"Projects":        []map[string]any{{"ID": "p1", "Name": "官网"}},
				"Buttons":         map[string]any{"block.create": true},

				"Headers": []map[string]any{{
					"ID": "b1", "Name": "站点页眉", "Kind": "header", "ReuseMode": "global",
					"UpdatedAt": utils.NewJSONTime(time.Date(2026, 1, 2, 15, 4, 0, 0, time.Local)),
					"RefCount":  int64(1),
				}},
				"Footers": []map[string]any{{
					"ID": "b2", "Name": "站点页脚", "Kind": "footer", "ReuseMode": "global",
					"UpdatedAt": utils.NewJSONTime(time.Date(2026, 1, 2, 15, 4, 0, 0, time.Local)),
					"RefCount":  int64(0),
				}},
				"Blocks": []map[string]any{{
					"ID": "b3", "Name": "商品卡", "Kind": "block", "ReuseMode": "template",
					"UpdatedAt": utils.NewJSONTime(time.Date(2026, 1, 2, 15, 4, 0, 0, time.Local)),
					"RefCount":  int64(-1),
				}, {

					"ID": "b4", "Name": "公告条", "Kind": "announcement", "ReuseMode": "global",
					"UpdatedAt": utils.NewJSONTime(time.Date(2026, 1, 2, 15, 4, 0, 0, time.Local)),
					"RefCount":  int64(-1),
				}},
			},

			wants: []string{
				"全局块", "新建并编辑", "页眉", "页脚", "类型", "复用方式", "说明", "确定删除块「",
				"站点页眉 页眉 引用 1 个页面引用",
				"站点页脚 页脚 引用 未被页面引用",
				"商品卡 区块 复制 —",
				"公告条 公告栏 引用 未知",
			},
		},
		{
			name: "admin/navigation/navigations",
			data: map[string]any{
				"SelectedProject": "p1", "Kind": "header",
				"Buttons":       map[string]any{"navigation.update": true, "navigation.create": true},
				"Projects":      []map[string]any{{"ID": "p1", "Name": "官网"}},
				"ParentOptions": []map[string]any{{"ID": "n1", "Title": "商品"}},
				"SourceGroups": []map[string]any{{
					"Title": "页面", "Type": "page",
					"Items": []map[string]any{{"ID": "s1", "Label": "关于我们", "URL": ""}},
				}},
				"Rows": []map[string]any{{
					"ID": "n1", "Title": "关于我们", "Path": "/about", "Target": "self",
					"TargetLabel": "当前窗口", "Indent": "0px", "Depth": 0, "First": true, "Last": true,
				}},
			},

			wants: []string{"导航菜单", "页眉导航", "添加菜单项", "当前窗口", "新标签页", "作为顶级项", "放到「", "从已有内容添加", "暂无公开路径", "加入菜单", "菜单结构", "菜单项", "打开方式", "编辑", "hx-get=\"/admin/navigations/edit", "确定删除「"},
		},
		{
			name: "admin/navigation/navigation_translations",
			data: map[string]any{
				"ProjectID": "p1", "Lang": "en-US", "Errors": []any{}, "Saved": true,
				"SavedNote": "已保存 1 条译文。", "RowCount": 1, "Done": 0,
				"Langs": []map[string]any{{"Code": "en-US", "Label": "English", "Active": true}},
				"Groups": []map[string]any{
					{"Title": "页眉导航", "Rows": []map[string]any{{
						"Context": "navigation.label", "Source": "商品", "Path": "/shop",
						"Target": "", "SourceHash": "h1", "Translated": false,
					}}},
					{"Title": "页脚导航", "Rows": []map[string]any{}},
				},
			},
			wants: []string{"导航译文", "切换语言", "共 ", "条菜单文字，已翻译", "译文", "该位置还没有菜单项", "保存译文"},
		},
		{
			name: "admin/system/menus",
			data: map[string]any{

				"Rows": []map[string]any{
					{
						"ID": "m1", "ParentID": "0", "Title": "内容管理", "Path": "/admin/pages",
						"Remark": "站点结构", "Icon": "folder", "Type": 1, "Status": 1,
						"SortOrder": 1, "Indent": "", "PermissionCodes": []string{"page:list"},
						"Depth": 0, "HasChildren": true, "Hidden": false, "Expanded": false, "Matched": false,
					},
					{
						"ID": "m2", "ParentID": "m1", "Title": "页面管理", "Path": "/admin/pages/list",
						"Remark": "", "Icon": "", "Type": 2, "Status": 1,
						"SortOrder": 2, "Indent": "", "PermissionCodes": []string{"page:list"},
						"Depth": 1, "HasChildren": false, "Hidden": true, "Expanded": false, "Matched": false,
					},
				},
				"Parents": []map[string]any{{"ID": "m0", "Title": "根", "Type": 1, "Indent": ""}},
				"Buttons": map[string]any{"menu.create": true, "menu.update": true, "menu.delete": true},
			},

			wants: []string{"菜单管理", "新建菜单", "筛选：", "图标", "标题", "类型", "权限点", "路径", "状态", "排序", "备注", "操作", "目录", "启用", "创建菜单", "确认删除？", "hx-get=\"/admin/menus/edit?id=", "page:list"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			data := siteStructureData()
			for k, v := range tc.data {
				data[k] = v
			}
			loader := testTemplateLoader()
			set := jet.NewSet(loader, jet.WithTemplateNameExtensions([]string{"", ".html"}))
			injectGlobals(set)
			out, err := render(t, set, tc.name, data, groupDRenderScope())
			if err != nil {
				t.Fatalf("%s 渲染失败: %v", tc.name, err)
			}
			for _, want := range tc.wants {
				if !strings.Contains(out, want) {
					t.Errorf("%s 渲染输出缺少 %q（模板可能中途中断或取词写错）", tc.name, want)
				}
			}
			if strings.Contains(out, "{{") {
				t.Errorf("%s 输出里残留模板语法字面量", tc.name)
			}
			if !strings.Contains(out, "</html>") {
				t.Errorf("%s 未渲染到布局尾部（渲染在中途中断）", tc.name)
			}
		})
	}
}

func TestPageTranslationsSingleTableMergedGroups(t *testing.T) {
	newRow := func(field, source, contextName, hash string) map[string]any {
		return map[string]any{
			"Field": field, "Rich": false, "Origin": "", "Source": source,
			"Target": "", "Context": contextName, "SourceHash": hash, "Limit": 200,
			"Translated": false, "Engine": "", "ReusePages": 0, "ReuseTotal": 0, "ReusePaths": []string{},
		}
	}
	reused := newRow("text", "联系我们", "core.button.text", "h2")
	reused["ReusePages"], reused["ReuseTotal"], reused["ReusePaths"] = 2, 3, []string{"/contact"}

	data := siteStructureData()
	data["PagePath"] = "/about"
	data["Errors"] = []any{}
	data["Saved"], data["SavedCount"] = false, 0
	data["PageID"], data["Lang"] = "pg1", "en-US"
	data["Langs"] = []map[string]any{{"Code": "en-US", "Label": "English", "Active": true}}
	data["PageDone"], data["PageTotal"] = 0, 3
	data["SiteNote"], data["SiteDone"], data["SiteTotal"] = map[string]any{"Key": "", "Fallback": ""}, 0, 3
	data["IsDefaultLang"], data["RowCount"] = false, 3
	data["Groups"] = []map[string]any{
		{"Component": "core.heading", "Rows": []map[string]any{
			newRow("text", "关于我们", "core.heading.text", "h1"),
		}},
		{"Component": "core.button", "Rows": []map[string]any{
			newRow("text", "了解更多", "core.button.text", "h2"),
			reused,
		}},
	}

	loader := testTemplateLoader()
	set := jet.NewSet(loader, jet.WithTemplateNameExtensions([]string{"", ".html"}))
	injectGlobals(set)
	out, err := render(t, set, "admin/page/page_translations", data)
	if err != nil {
		t.Fatalf("渲染失败: %v", err)
	}

	const tableMark = `class="data-table data-table-wide tr-table"`
	if n := strings.Count(out, tableMark); n != 1 {
		t.Fatalf("两个组件应共用一张编辑表，实际 %d 张", n)
	}
	table := out[strings.Index(out, tableMark):]
	if i := strings.Index(table, "</table>"); i >= 0 {
		table = table[:i]
	}

	if n := strings.Count(table, "<thead>"); n != 1 {
		t.Fatalf("两个组件应共用一个表头，实际 %d 个", n)
	}
	if !strings.Contains(table, "<th>组件</th>") {
		t.Fatal("表头缺少「组件」列（合并后组件名归这一列）")
	}
	if strings.Contains(table, "<h2>") {
		t.Fatal("组件名应落在表内分组行，不应再有每组的 h2")
	}

	for _, comp := range []string{"core.heading", "core.button"} {
		if !strings.Contains(table, `<td colspan="6">`+comp+" ") {
			t.Fatalf("缺少组件 %s 的合并整行分组行", comp)
		}
	}

	counts := map[string]int{}
	for _, name := range []string{"rowTarget", "rowContext", "rowSource", "rowHash"} {
		counts[name] = strings.Count(table, `name="`+name+`"`)
	}
	if counts["rowTarget"] != 3 {
		t.Fatalf("三行数据应渲染 3 个 rowTarget，实际 %d", counts["rowTarget"])
	}
	for _, name := range []string{"rowContext", "rowSource", "rowHash"} {
		if counts[name] != counts["rowTarget"] {
			t.Fatalf("四类字段数量不等：rowTarget=%d %s=%d（保存端按下标配对，不等会整体拒绝）",
				counts["rowTarget"], name, counts[name])
		}
	}

	thead := table[strings.Index(table, "<thead>"):strings.Index(table, "</thead>")]
	headCols := strings.Count(thead, "<th>")
	if headCols != 6 {
		t.Fatalf("表头应为 6 列（组件 / 字段 / 原文 / 译文 / 状态 / 来源），实际 %d 列", headCols)
	}

	rows := 0
	for _, chunk := range strings.Split(table, "<tr>")[1:] {
		tr := chunk
		if i := strings.Index(tr, "</tr>"); i >= 0 {
			tr = tr[:i]
		}
		if !strings.Contains(tr, `name="rowTarget"`) {
			continue
		}
		rows++
		if n := strings.Count(tr, "<td"); n != headCols {
			t.Fatalf("第 %d 个数据行有 %d 个 td，表头 %d 列 —— 列会错位", rows, n, headCols)
		}
		prev := -1
		for _, name := range []string{"rowTarget", "rowContext", "rowSource", "rowHash"} {
			at := strings.Index(tr, `name="`+name+`"`)
			if at < 0 {
				t.Fatalf("第 %d 行缺少 %s", rows, name)
			}
			if at < prev {
				t.Fatalf("第 %d 行的 %s 顺序错位（须 rowTarget → rowContext → rowSource → rowHash）", rows, name)
			}
			prev = at
		}
	}
	if rows != 3 {
		t.Fatalf("应渲染 3 个数据行（分组行 / 复用行不计），实际 %d", rows)
	}

	reuse := table[strings.Index(table, `<tr class="tr-reuse">`):]
	reuse = reuse[:strings.Index(reuse, "</tr>")]
	if n := strings.Count(reuse, "<td"); n != 2 || !strings.Contains(reuse, `colspan="5"`) {
		t.Fatalf("复用行应为「1 个空 td + colspan=5」，实际 %s", reuse)
	}
}

func TestBlockRowLabelsComeFromSeededKeys(t *testing.T) {

	anyKeyRe := regexp.MustCompile(`"(admin\.[a-zA-Z0-9_.]+)"`)
	kindKeyRe := regexp.MustCompile(`"(admin\.blocks\.kind\.[a-z]+)"`)

	rowSrc := adminTemplateSource(t, "block_row.html")
	rowKind := keySetOf(kindKeyRe, rowSrc)
	dropKind := keySetOf(kindKeyRe, adminTemplateSource(t, "blocks.html"))

	if len(rowKind) < 10 || len(dropKind) < 10 {
		t.Fatalf("kind key 解析结果过少（行内 %d / 下拉 %d），判据已退化成空转", len(rowKind), len(dropKind))
	}
	if !equalKeySets(rowKind, dropKind) {
		t.Fatalf("行内 kind 文案与类型下拉不是同一批 key：行内多 %v，下拉多 %v",
			missingIn(dropKind, rowKind), missingIn(rowKind, dropKind))
	}

	seeded := seededI18nKeys(t)
	rowKeys := sortedKeys(keySetOf(anyKeyRe, rowSrc))
	if len(rowKeys) < 15 {
		t.Fatalf("block_row.html 只解析出 %d 个取词 key，判据已退化成空转", len(rowKeys))
	}
	for _, key := range rowKeys {
		langs := seeded[key]
		if langs == nil {
			t.Errorf("block_row.html 用了 key %s，但迁移里没有这条词条", key)
			continue
		}
		for _, lang := range []string{"zh-CN", "en-US"} {
			if !langs[lang] {
				t.Errorf("key %s 缺 %s 词条（模板里的中文只是 t() 兜底）", key, lang)
			}
		}
	}
}

func keySetOf(re *regexp.Regexp, src string) map[string]bool {
	set := map[string]bool{}
	for _, m := range re.FindAllStringSubmatch(src, -1) {
		set[m[1]] = true
	}
	return set
}

func sortedKeys(set map[string]bool) []string {
	out := make([]string, 0, len(set))
	for k := range set {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func missingIn(want, have map[string]bool) []string {
	var out []string
	for k := range want {
		if !have[k] {
			out = append(out, k)
		}
	}
	sort.Strings(out)
	return out
}

func equalKeySets(a, b map[string]bool) bool {
	return len(a) == len(b) && len(missingIn(a, b)) == 0
}

func seededI18nKeys(t *testing.T) map[string]map[string]bool {
	t.Helper()
	paths, err := filepath.Glob(filepath.Join("..", "..", "public", "migrations", "*.sql"))
	if err != nil || len(paths) == 0 {
		t.Fatalf("找不到迁移目录: %v（%d 个文件）", err, len(paths))
	}
	seedRe := regexp.MustCompile(`\('([^']+)',\s*'(zh-CN|en-US)'`)
	seeded := map[string]map[string]bool{}
	for _, p := range paths {
		src, err := os.ReadFile(p)
		if err != nil {
			t.Fatalf("读迁移 %s 失败: %v", p, err)
		}
		for _, m := range seedRe.FindAllStringSubmatch(string(src), -1) {
			if seeded[m[1]] == nil {
				seeded[m[1]] = map[string]bool{}
			}
			seeded[m[1]][m[2]] = true
		}
	}
	return seeded
}
