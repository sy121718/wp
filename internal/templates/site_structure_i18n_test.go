package templates

// site_structure_i18n_test.go — 站点结构类后台模板的取词与渲染回归（审计 I18N-001 组E）。
//
// 覆盖 9 个模板：site_slots / theme / theme_settings / pages / page_translations /
// blocks / navigations / navigation_translations / menus。
//
// 为什么必须真渲染：这些模板里有大量 range 内取词（{{tr := .["t"]}} 之后在 range 里
// tr("key","兜底")）。Jet 的作用域与类型错误都不在解析期暴露，表现是
// **HTTP 200 但 body 从某一行起整块消失**（列表、表单全没了）—— 从「少了一行数据」
// 几乎定位不到模板中间那一行。所以把「能解析 + 能渲染 + 关键文案与字段在」固化成断言。
//
// 断言用中文兜底文案：测试进程不连库，pkg/i18n 缓存为空，t() 一律回落到模板内原文。

import (
	"strings"
	"testing"

	"github.com/CloudyKit/jet/v6"
)

// siteStructureTemplates 本批模板名（不带 .html 扩展）。
var siteStructureTemplates = []string{
	"admin/page/site_slots",
	"admin/project/theme",
	"admin/project/theme_settings",
	"admin/page/pages",
	"admin/page/page_translations",
	"admin/block/blocks",
	"admin/navigation/navigations",
	"admin/navigation/navigation_translations",
	"admin/system/menus",
}

// siteStructureData 外壳（layout.html）所需的最小数据。
func siteStructureData() map[string]any {
	return map[string]any{
		"lang": "zh-CN", "title": "站点结构", "menu": "pages", "t": TranslateFunc("en-US"),
		"csrf_token": "tok",
	}
}

// TestSiteStructureTemplatesParse 九个模板都必须能整份解析（extends layout.html 一起解析）。
func TestSiteStructureTemplatesParse(t *testing.T) {
	loader := jet.NewOSFileSystemLoader(".")
	set := jet.NewSet(loader, jet.WithTemplateNameExtensions([]string{"", ".html"}))
	for _, name := range siteStructureTemplates {
		if _, err := set.GetTemplate(name); err != nil {
			t.Errorf("%s 模板解析失败: %v", name, err)
		}
	}
}

// TestSiteStructureTemplatesRender 九个模板都必须能渲染到尾部，且关键文案/字段在输出里。
func TestSiteStructureTemplatesRender(t *testing.T) {
	cases := []struct {
		name  string
		data  map[string]any
		wants []string
	}{
		{
			name: "admin/page/site_slots",
			data: map[string]any{
				"Err": "", "Ok": "", "SelectedProject": "p1",
				"Projects":   []map[string]any{{"ID": "p1", "Name": "官网"}},
				"BoundCount": 1, "Total": 1, "UnpublishedCount": 0, "DeletedCount": 0,
				"NoPages": false,
				"Rows": []map[string]any{{
					"SlotName": "结算页", "Slot": "checkout", "Badge": "badge-mute", "StateLabel": "已绑定",
					"Usage": "购物车去结算", "Unbound": false, "PageDeleted": false,
					"DraftPath": "/checkout", "Published": false, "PublicURL": "", "Path": "",
					"Bound": true, "PageOptions": []map[string]any{}, "BindLabel": "换绑",
				}},
			},
			wants: []string{"系统页面", "概览", "槽位", "解绑", "草稿路径", "这个工程还没有页面"},
		},
		{
			name: "admin/project/theme",
			data: map[string]any{
				"SelectedProject": "p1",
				"Projects":        []map[string]any{{"ID": "p1", "Name": "官网"}},
				// 新建走抽屉后按钮显隐依赖权限集合（shell.Prepare 一定注入，测试数据补齐）。
				"PermSet": map[string]any{"theme:create": true},
				// Err 是「上一次写操作失败」的提示槽（?err= 回带 / 装载失败降级都写它）。
				// handler 的 templateMap 一定给（空串 = 无提示），测试数据补齐。
				"Err": "",
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
					"Title": "颜色",
					"Fields": []map[string]any{
						{"Kind": "select", "Label": "主色", "Name": "colors.primary", "Value": "",
							"Options": []map[string]any{{"Value": "#111111", "Label": "深色", "Selected": true}}},
						{"Kind": "datalist", "Label": "标题字体", "Name": "typography.heading.family", "Value": "",
							"Options": []map[string]any{{"Value": "Inter", "Label": "Inter"}}},
						{"Kind": "color", "Label": "背景色", "Name": "surface.bg", "Value": "", "Options": []map[string]any{}},
					},
				}},
				"HeaderBlocks":       []map[string]any{{"ID": "b1", "Name": "站点页眉"}},
				"FooterBlocks":       []map[string]any{{"ID": "b2", "Name": "站点页脚"}},
				"AnnouncementBlocks": []map[string]any{{"ID": "b3", "Name": "公告条"}},
				"HeaderBlock":        "b1", "FooterBlock": "b2", "AnnouncementBlock": "b3",
			},
			wants: []string{"主题设置：", "返回列表", "全局页眉 / 页脚块", "页眉块", "页脚块", "公告条块", "保存并应用到该主题页面", "说明"},
		},
		{
			name: "admin/page/pages",
			data: map[string]any{
				"Projects":   []map[string]any{{"ID": "p1", "Name": "官网"}},
				"Blueprints": []map[string]any{{"ID": "b1", "Name": "落地页"}},
				// 一页两个创建入口 → 两个权限码都要给（缺一个按钮就不渲染，wants 会红）。
				"PermSet": map[string]any{"project:create": true, "page:create": true},
				"Pages": []map[string]any{{
					"ID": "pg1", "DraftPath": "/about", "Kind": "page", "Active": true,
					"Stale": true, "Staged": false, "Version": 3, "UpdatedAt": "2026-01-02",
				}},
			},
			wants: []string{"新建站点工程", "创建工程", "新建页面", "创建页面", "页面列表", "已发布", "有更新未发布", "编辑", "预览", "多语言", "线上"},
		},
		{
			name: "admin/page/page_translations",
			data: map[string]any{
				"PagePath": "/about", "Errors": []string{}, "Saved": true, "SavedCount": 1,
				"PageID": "pg1", "Lang": "en-US",
				"Langs":    []map[string]any{{"Code": "en-US", "Label": "English", "Active": true}},
				"PageDone": 1, "PageTotal": 2, "SiteNote": "", "SiteDone": 1, "SiteTotal": 2,
				"IsDefaultLang": false, "RowCount": 1,
				"Groups": []map[string]any{{
					"Component": "hero",
					"Rows": []map[string]any{{
						"Field": "title", "Rich": true, "Origin": "全站共享", "Source": "关于我们",
						"Target": "", "Context": "hero.title", "SourceHash": "h1", "Limit": 200,
						"Translated": false, "Engine": "", "ReusePages": 2, "ReuseTotal": 3,
						"ReuseHint": "页眉 / 页脚",
					}},
				}},
			},
			wants: []string{"多语言 · ", "已保存 ", "切换语言", "本页完成度", "全站完成度", "筛选：", "全部", "只看缺失", "只看人工", "只看 AI 翻译", "组件", "字段", "富文本", "填写译文", "上限 ", "缺失", "保存全部", "还用在另外 "},
		},
		{
			name: "admin/block/blocks",
			data: map[string]any{
				"SelectedProject": "p1",
				"Projects":        []map[string]any{{"ID": "p1", "Name": "官网"}},
				"PermSet":         map[string]any{"block:create": true},
				"Headers":         []map[string]any{{"ID": "b1", "Name": "站点页眉", "UpdatedAt": "2026-01-02"}},
				"Footers":         []map[string]any{{"ID": "b2", "Name": "站点页脚", "UpdatedAt": "2026-01-02"}},
				"Blocks":          []map[string]any{{"ID": "b3", "Name": "商品卡", "KindLabel": "区块", "ReuseModeLabel": "一次性复制", "UpdatedAt": "2026-01-02"}},
			},
			// 「区块 / 复用资产」原是三段表第三段的段标题（02-L P1-4 合并成一张表后，
			// 段的区分改由「类型」列承担，分段标题不再存在）。断言的**语义**保持为
			// 「这一页能表达块的不同类型」，改为断言承担该语义的新表头；
			// 「页眉」「页脚」继续由新建抽屉的类型下拉兜底，断言原样保留。
			wants: []string{"全局块", "新建并编辑", "页眉", "页脚", "类型", "复用方式", "说明", "确定删除块「"},
		},
		{
			name: "admin/navigation/navigations",
			data: map[string]any{
				"SelectedProject": "p1", "Kind": "header",
				"PermSet":       map[string]any{"navigation:update": true, "navigation:create": true},
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
			// 每行编辑表单按需 GET 加载，列表只保留编辑入口；保存按钮在独立片段中验证。
			wants: []string{"导航菜单", "页眉导航", "添加菜单项", "当前窗口", "新标签页", "作为顶级项", "放到「", "从已有内容添加", "暂无公开路径", "加入菜单", "菜单结构", "菜单项", "打开方式", "编辑", "data-drawer-url", "确定删除「"},
		},
		{
			name: "admin/navigation/navigation_translations",
			data: map[string]any{
				"ProjectID": "p1", "Lang": "en-US", "Errors": []string{}, "Saved": true,
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
				"Rows": []map[string]any{{
					"ID": "m1", "ParentID": "0", "Title": "内容管理", "Path": "/admin/pages",
					"Remark": "站点结构", "Icon": "folder", "Type": 1, "Status": 1,
					"SortOrder": 1, "Indent": "", "PermissionCodes": []string{"page:list"},
				}},
				"Parents": []map[string]any{{"ID": "m0", "Title": "根", "Type": 1, "Indent": ""}},
				"PermSet": map[string]any{"menu:create": true, "menu:update": true, "menu:delete": true},
			},
			// 编辑表单按需从 /admin/menus/edit 获取；列表只交付编辑入口和新建表单。
			wants: []string{"菜单管理", "新建菜单", "筛选：", "图标", "标题", "类型", "权限点", "路径", "状态", "排序", "备注", "操作", "目录", "启用", "创建菜单", "确认删除？", "data-drawer-url", "page:list"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			data := siteStructureData()
			for k, v := range tc.data {
				data[k] = v
			}
			loader := jet.NewOSFileSystemLoader(".")
			set := jet.NewSet(loader, jet.WithTemplateNameExtensions([]string{"", ".html"}))
			out, err := render(t, set, tc.name, data)
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

// TestPageTranslationsSingleTableMergedGroups 翻译工作台的「按组件分段的多张表」已合并成一张表，
// 且「每行的 textarea 与它自己的 3 个 hidden 同处一个 tr、四类字段顺序一致」这个形状被钉住。
//
// 为什么必须钉形状（而不是只看渲染成功）：保存端 page_translations_handle.go 的
// SavePageTranslations 按**四个数组的下标**配对（rowContext / rowSource / rowHash / rowTarget），
// 任一行的控件被拆散（分组行把数据行包起来、hidden 掉到 tr 外、某个字段被条件包裹）都会让长度不等，
// 于是**整批拒绝**并回带 MsgTranslationInvalid —— 模板层看不出任何异常，用户看到的是「提交数据不完整」。
// 多张表合并成一张正是这个风险最集中的改动。
func TestPageTranslationsSingleTableMergedGroups(t *testing.T) {
	newRow := func(field, source, contextName, hash string) map[string]any {
		return map[string]any{
			"Field": field, "Rich": false, "Origin": "", "Source": source,
			"Target": "", "Context": contextName, "SourceHash": hash, "Limit": 200,
			"Translated": false, "Engine": "", "ReusePages": 0, "ReuseTotal": 0, "ReuseHint": "",
		}
	}
	reused := newRow("text", "联系我们", "core.button.text", "h2")
	reused["ReusePages"], reused["ReuseTotal"], reused["ReuseHint"] = 2, 3, "/contact"

	data := siteStructureData()
	data["PagePath"] = "/about"
	data["Errors"] = []string{}
	data["Saved"], data["SavedCount"] = false, 0
	data["PageID"], data["Lang"] = "pg1", "en-US"
	data["Langs"] = []map[string]any{{"Code": "en-US", "Label": "English", "Active": true}}
	data["PageDone"], data["PageTotal"] = 0, 3
	data["SiteNote"], data["SiteDone"], data["SiteTotal"] = "", 0, 3
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

	loader := jet.NewOSFileSystemLoader(".")
	set := jet.NewSet(loader, jet.WithTemplateNameExtensions([]string{"", ".html"}))
	out, err := render(t, set, "admin/page/page_translations", data)
	if err != nil {
		t.Fatalf("渲染失败: %v", err)
	}

	// 只在编辑表范围内断言：布局壳（侧栏）里也有表格，且筛选栏里还有一个嵌套的 GET 表单。
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

	// 分组行 = 合并整行的 td[colspan]（theme.css §14 的 .data-table tbody tr > td[colspan] 就吃这个形状）。
	for _, comp := range []string{"core.heading", "core.button"} {
		if !strings.Contains(table, `<td colspan="6">`+comp+" ") {
			t.Fatalf("缺少组件 %s 的合并整行分组行", comp)
		}
	}

	// 四类字段数量相等（渲染 HTML 断言）。
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

	// 表头列数与每一行的 td 数必须一致：数据行少一个 td 时浏览器把缺口补在**行尾**，
	// 整表左移一列（字段值显示在「组件」列、状态值显示在「译文」列），而模板本身不会报错。
	thead := table[strings.Index(table, "<thead>"):strings.Index(table, "</thead>")]
	headCols := strings.Count(thead, "<th>")
	if headCols != 6 {
		t.Fatalf("表头应为 6 列（组件 / 字段 / 原文 / 译文 / 状态 / 来源），实际 %d 列", headCols)
	}

	// 每一行：textarea 与它自己的 3 个 hidden 同处一个 tr，且顺序固定。
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

	// 附加行（复用行）也要占满 6 列：1 个空 td + colspan=5。
	reuse := table[strings.Index(table, `<tr class="tr-reuse">`):]
	reuse = reuse[:strings.Index(reuse, "</tr>")]
	if n := strings.Count(reuse, "<td"); n != 2 || !strings.Contains(reuse, `colspan="5"`) {
		t.Fatalf("复用行应为「1 个空 td + colspan=5」，实际 %s", reuse)
	}
}
