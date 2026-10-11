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
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
	"time"

	"go_wp/pkg/i18n"
	"go_wp/pkg/utils"

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
	injectGlobals(set)
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
				// Err 是「本页装载失败」槽：handler 给**事实**（key + 中文兜底），
				// 两项都空 = 无提示；取词由模板的 tf 做。给空串会让模板的 .Err.Key 报错。
				"Err": map[string]any{"Key": "", "Fallback": ""}, "Ok": "", "SelectedProject": "p1",
				"Projects":   []map[string]any{{"ID": "p1", "Name": "官网"}},
				"BoundCount": 1, "Total": 1, "UnpublishedCount": 0, "DeletedCount": 0,
				// 分档由组装侧给（page 模块的 unpublishedSlotsTone / deletedSlotsTone）：
				// 这里计数都是 0 → 两档都是中性，模板读 badge-{{.UnpublishedTone}}。
				"UnpublishedTone": "mute", "DeletedTone": "mute",
				"NoPages": false,
				"Rows": []map[string]any{{
					"Slot":        "checkout",
					"SlotNameKey": "admin.site_slots.slot.checkout", "SlotNameFallback": "结算页",
					"UsageKey": "admin.site_slots.usage.checkout", "UsageFallback": "购物车去结算",
					"Unbound": false, "PageDeleted": false,
					"DraftPath": "/checkout", "Published": false, "PublicURL": "", "Path": "",
					"Bound": true, "PageOptions": []map[string]any{},
				}},
			},
			wants: []string{"系统页面", "概览", "槽位", "解绑", "草稿路径", "这个工程还没有页面"},
		},
		{
			name: "admin/project/theme",
			data: map[string]any{
				"SelectedProject": "p1",
				"Projects":        []map[string]any{{"ID": "p1", "Name": "官网"}},
				// 新建走抽屉后按钮显隐依赖权限集合（adminui.Prepare 一定注入，测试数据补齐）。
				"Buttons": map[string]any{"theme.create": true},
				// Err 是「本页装载失败」的降级提示槽（写动作的结论不再回带本页）。
				// handler 的 templateMap 一定给**事实**（key + 中文兜底，两项都空 = 无提示），
				// 取词由模板的 tf 做 —— 测试数据补齐。
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
				// 分组标题 / 字段 label / 选项 label 与块名收的是**事实**（*Key + *Fallback 两字段，
				// key 为空时兜底即中文原文）—— 取词由模板的 tr 做，handler 不再提前翻成译文。
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
				"Buttons": map[string]any{"project.create": true, "page.create": true},
				"Pages": []map[string]any{{
					"ID": "pg1", "DraftPath": "/about", "Kind": "page", "Active": true,
					"Stale": true, "Staged": false, "Version": 3, "UpdatedAt": utils.NewJSONTime(time.Date(2026, 1, 2, 0, 0, 0, 0, time.Local)),
				}},
			},
			wants: []string{"新建站点工程", "创建工程", "新建页面", "创建页面", "页面列表", "已发布", "有更新未发布", "编辑", "预览", "多语言", "线上"},
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
				// 行数据是业务事实（Kind / ReuseMode / UpdatedAt / RefCount），标签与
				// 「{count} 个页面引用」这类文案由 block_row.html 取词生成；
				// UpdatedAt 是 utils.JSONTime（dateTime 只认 time.Time / *time.Time / JSONTime，
				// 传字符串会在渲染期 panic），RefCount 为 -1 表示引用数未知。
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
					// 引用模式 + 读不到引用数：必须显示「未知」，与「未被页面引用」分开
					// （把读不到显示成 0 会让人以为这个块可以放心删）。
					"ID": "b4", "Name": "公告条", "Kind": "announcement", "ReuseMode": "global",
					"UpdatedAt": utils.NewJSONTime(time.Date(2026, 1, 2, 15, 4, 0, 0, time.Local)),
					"RefCount":  int64(-1),
				}},
			},
			// 「区块 / 复用资产」原是三段表第三段的段标题（02-L P1-4 合并成一张表后，
			// 段的区分改由「类型」列承担，分段标题不再存在）。断言的**语义**保持为
			// 「这一页能表达块的不同类型」，改为断言承担该语义的新表头；
			// 「页眉」「页脚」继续由新建抽屉的类型下拉兜底，断言原样保留。
			//
			// 另四条是 block_row.html 的取词契约（本页唯一一处把行数据翻译成 UI 文案的地方，
			// 断言用整条 data-filter-text：名称 + 类型标签 + 复用标签 + 影响面文案一次比完）：
			// 三种引用态各自成句、两种复用方式各自成词 —— 「读不到 / 零引用 / N 个页面引用」
			// 与「引用 / 复制」显示成同一句话时，页面看起来仍然正常。
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
			// 每行编辑表单按需 GET 加载，列表只保留编辑入口；保存按钮在独立片段中验证。
			wants: []string{"导航菜单", "页眉导航", "添加菜单项", "当前窗口", "新标签页", "作为顶级项", "放到「", "从已有内容添加", "暂无公开路径", "加入菜单", "菜单结构", "菜单项", "打开方式", "编辑", "data-drawer-url", "确定删除「"},
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
				// 树状行：Depth / HasChildren / Hidden / Expanded / Matched 由 service 算好交给模板
				// （浏览态 Depth=0 的顶级行 + 一个被折叠的子行；搜索态的空态另有一组测试）。
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
			injectGlobals(set)
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
// 为什么必须钉形状（而不是只看渲染成功）：保存端 page_translations.go 的
// SavePageTranslations 按**四个数组的下标**配对（rowContext / rowSource / rowHash / rowTarget），
// 任一行的控件被拆散（分组行把数据行包起来、hidden 掉到 tr 外、某个字段被条件包裹）都会让长度不等，
// 于是**整批拒绝**并回带 MsgTranslationInvalid —— 模板层看不出任何异常，用户看到的是「提交数据不完整」。
// 多张表合并成一张正是这个风险最集中的改动。
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

	loader := jet.NewOSFileSystemLoader(".")
	set := jet.NewSet(loader, jet.WithTemplateNameExtensions([]string{"", ".html"}))
	injectGlobals(set)
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

// —— 行标签搬进模板后的契约（admin/block/block_row.html） ——
//
// 本次改造把 kind / reuseMode / refCount 三张标签表从 Go 侧
// （blockKindLabels / blockReuseModeLabels / blockRefCountText）搬进了模板片段
// admin/block/block_row.html —— 行数据只留业务事实（Kind / ReuseMode / UpdatedAt / RefCount）。
// 于是原来由 Go 表结构保证的三件事只剩模板里的**字面量**，必须换成可执行的判据：
//
//	① 行内 kind 的 key 集与新建抽屉的类型下拉**同一批**。这条原先写在 Go 表的注释上
//	   （「两处同义，必须共用同一条词条」），表删了就没人守 —— 失守的唯一症状是
//	   同一个 kind 在下拉里显示一个词、在列表里显示另一个词，不报错、也不会有别的测试变红。
//	② 行标签用到的 key 在 sys_i18n 的 seed 迁移里中英成对。模板里的中文只是 t() 兜底：
//	   缺 en-US 行时英文界面永远显示中文（admin_content_list_filter_test.go 同款判据）。
//	③ 三种引用态各有自己的 key —— 已由 testBody 里的 key 集覆盖（unknown / none / pages）：
//	   「读不到」与「没人用」显示成同一句话会让人以为这个块可以放心删。
func TestBlockRowLabelsComeFromSeededKeys(t *testing.T) {
	// 行内标签 key 与 kind key 两种半径：前者用于 seed 对账，后者用于与下拉比对。
	anyKeyRe := regexp.MustCompile(`"(admin\.[a-zA-Z0-9_.]+)"`)
	kindKeyRe := regexp.MustCompile(`"(admin\.blocks\.kind\.[a-z]+)"`)

	rowSrc := adminTemplateSource(t, "block_row.html")
	rowKind := keySetOf(kindKeyRe, rowSrc)
	dropKind := keySetOf(kindKeyRe, adminTemplateSource(t, "blocks.html"))

	// 非空转自检：任一集合塌成 0（正则写坏 / 文件被搬走）时，下面的相等断言变成
	// 「0 == 0 恒真」—— 门禁仍然绿，但不再守任何东西。
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

// keySetOf 取 src 里匹配 re 的全部第一个捕获组，去重成集合。
func keySetOf(re *regexp.Regexp, src string) map[string]bool {
	set := map[string]bool{}
	for _, m := range re.FindAllStringSubmatch(src, -1) {
		set[m[1]] = true
	}
	return set
}

// sortedKeys 集合 → 有序切片（断言消息与遍历顺序稳定）。
func sortedKeys(set map[string]bool) []string {
	out := make([]string, 0, len(set))
	for k := range set {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// missingIn 返回 want 有、have 没有的 key（有序）。
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

// equalKeySets 两个集合是否完全相等。
func equalKeySets(a, b map[string]bool) bool {
	return len(a) == len(b) && len(missingIn(a, b)) == 0
}

// seededI18nKeys 扫描 public/migrations 下全部 SQL，返回 key → 已落库语言集。
//
// 不写死文件名：词条散在 192（站点结构）/ 230（列表骨架）/ 451（Go 硬编码文案）等多份 seed 里，
// 写死一份就会在「词条其实在另一份里」时给出假红，而漏掉新文件则给出假绿。
// 全目录 5MB 量级，读一遍的代价远低于误判的代价（同 admin_empty_actions_test.go 的口径）。
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

// —— 槽位状态展示：档位与文案由模板按事实选（task-103） ——
//
// 原先 handler 的行投影直接给 `Badge`（"badge-mute" 这类**类名**）与 `StateLabel`（已取好词的文案），
// 页头两个档位也由 Go 的 unpublishedSlotsTone / deletedSlotsTone 给。现在 Go 只给事实
//（Bound / PageDeleted / Published 与两个计数），选档与取词都在 internal/templates/admin/page/site_slots.html。
// 这一组用例钉的正是「搬过去之后判据没走样」：四档、矛盾事实的优先级、完整零值事实、
// 缺键健壮性、页头 `n != 0`（含负数）、词条 key 与 seed 对账、中英与缺词回退。

// siteSlotRowFacts 一行槽位的事实（与 handler 的 siteSlotRow 投影同形）。
func siteSlotRowFacts(slot string, facts map[string]any) map[string]any {
	// 槽位名与用途现在交 **key + 兜底** 两个事实（取词在模板里做），不再是一个成品串。
	// 用真实词条 key，模板的 tr 才能按当前语言取词。
	row := map[string]any{
		"Slot": slot, "SlotNameKey": "admin.site_slots.slot.checkout", "SlotNameFallback": "结算页",
		"UsageKey": "admin.site_slots.usage.checkout", "UsageFallback": "购物车去结算",
		"DraftPath": "/checkout", "Path": "", "PublicURL": "",
		"PageOptions": []map[string]any{},
	}
	for k, v := range facts {
		row[k] = v
	}
	return row
}

// renderSiteSlotsPage 真渲染 admin/page/site_slots（连 layout），返回整页输出。
//
// extra 里的键值覆盖默认数据；**值为 nil 表示「这个键缺席」**（¬ 用于测缺键健壮性）。
func renderSiteSlotsPage(t *testing.T, rows []map[string]any, extra map[string]any) string {
	t.Helper()
	data := siteStructureData()
	data["Err"] = map[string]any{"Key": "", "Fallback": ""}
	data["SelectedProject"] = "p1"
	data["Projects"] = []map[string]any{{"ID": "p1", "Name": "官网"}}
	data["BoundCount"] = 1
	data["Total"] = 1
	data["UnpublishedCount"] = 0
	data["DeletedCount"] = 0
	// 列表区分支的前提：有工程、有页面、本次没有装载失败（本组用例测的是行的档位，不是空态）。
	data["NoProjectEmpty"] = false
	data["NoPages"] = false
	data["Rows"] = rows
	for k, v := range extra {
		if v == nil {
			delete(data, k)
			continue
		}
		data[k] = v
	}
	set := jet.NewSet(jet.NewOSFileSystemLoader("."), jet.WithTemplateNameExtensions([]string{"", ".html"}))
	injectGlobals(set)
	out, err := render(t, set, "admin/page/site_slots", data)
	if err != nil {
		t.Fatalf("渲染 admin/page/site_slots 失败: %v", err)
	}
	if !strings.Contains(out, "</html>") {
		t.Fatalf("未渲染到布局尾部（渲染在中途中断）:\n%s", out)
	}
	return out
}

// TestSiteSlotRowToneFromFacts 行徽章的四个档位 + 矛盾事实的优先级。
func TestSiteSlotRowToneFromFacts(t *testing.T) {
	cases := []struct {
		name  string
		facts map[string]any
		want  string // 期望的整段徽章（类名 + 文案）
	}{
		{
			name:  "未绑定（中性档）",
			facts: map[string]any{"Bound": false, "PageDeleted": false, "Published": false, "Unbound": true},
			want:  `<span class="badge badge-mute">未绑定</span>`,
		},
		{
			// 完整零值事实行：与「未绑定」走同一条路径（旧行为也是 !Bound → unbound/mute），
			// 不允许出现「零值就什么徽章都不渲染」的第三种形态。
			name:  "完整零值事实行仍是未绑定",
			facts: map[string]any{"Bound": false, "PageDeleted": false, "Published": false, "Unbound": true},
			want:  `<span class="badge badge-mute">未绑定</span>`,
		},
		{
			// 矛盾事实：未绑定却带 PageDeleted=true —— 旧 switch 先判 !Bound，所以仍是 unbound/mute。
			name:  "矛盾事实（!Bound 且 PageDeleted）按未绑定优先",
			facts: map[string]any{"Bound": false, "PageDeleted": true, "Published": false, "Unbound": true},
			want:  `<span class="badge badge-mute">未绑定</span>`,
		},
		{
			name:  "绑定但页面已删除（危险档）",
			facts: map[string]any{"Bound": true, "PageDeleted": true, "Published": false, "Unbound": false},
			want:  `<span class="badge badge-danger">绑定已失效</span>`,
		},
		{
			name:  "绑定且已发布（成功档）",
			facts: map[string]any{"Bound": true, "PageDeleted": false, "Published": true, "Unbound": false},
			want:  `<span class="badge badge-ok">已发布</span>`,
		},
		{
			name:  "绑定但未发布（降级档）",
			facts: map[string]any{"Bound": true, "PageDeleted": false, "Published": false, "Unbound": false},
			want:  `<span class="badge badge-warn">已绑定但未发布</span>`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out := renderSiteSlotsPage(t, []map[string]any{siteSlotRowFacts("checkout", tc.facts)}, nil)
			if !strings.Contains(out, tc.want) {
				t.Errorf("期望输出含 %s，实际没有（档位或文案与事实不符）", tc.want)
			}
			// 一行只该有一个状态徽章：按「整段徽章（类名 + 文案）」精确计数，
			// 四段之和必须恰好为 1，否则「同时渲染两档 / 一个都不渲染」这类穿透发现不了。
			// 用整段而不是类名计数：页头三个计数徽章面对同类名（且带计数后缀），只按类名会一起数进来。
			sum := 0
			for _, span := range []string{
				`<span class="badge badge-mute">未绑定</span>`,
				`<span class="badge badge-danger">绑定已失效</span>`,
				`<span class="badge badge-ok">已发布</span>`,
				`<span class="badge badge-warn">已绑定但未发布</span>`,
			} {
				sum += strings.Count(out, span)
			}
			if sum != 1 {
				t.Errorf("行内状态徽章数量应为 1，实际 %d（分支穿透或漏渲染）", sum)
			}
		})
	}
}

// TestSiteSlotRowMissingFactsFallBackToUnbound 缺 Bound/PageDeleted/Published 的行必须稳健。
//
// 只覆盖「行里其它必需事实（SlotName / Slot / Usage / BindLabel / PageOptions）齐全、仅缺这三个布尔」
// 这一种夹具形态 —— 不宣称任意空 map 都能渲染出完整页（那会把渲染期的其它必需键也一并放弃）。
func TestSiteSlotRowMissingFactsFallBackToUnbound(t *testing.T) {
	out := renderSiteSlotsPage(t, []map[string]any{siteSlotRowFacts("checkout", nil)}, nil)
	if !strings.Contains(out, `<span class="badge badge-mute">未绑定</span>`) {
		t.Errorf("缺三个布尔时该落「未绑定/中性」档（与完整零值事实同一条路径），实际输出里没有")
	}
}

// TestSiteSlotHeaderToneFromCounts 页头两个档位的判据是 `n != 0`（**含负数非零**），且缺键落中性。
func TestSiteSlotHeaderToneFromCounts(t *testing.T) {
	row := siteSlotRowFacts("checkout", map[string]any{
		"Bound": true, "PageDeleted": false, "Published": true, "Unbound": false,
	})
	cases := []struct {
		name                     string
		unpublished, deleted     any // nil = 键缺席
		wantUnpublished, wantDel string
	}{
		{name: "计数都是 0", unpublished: 0, deleted: 0, wantUnpublished: "badge-mute", wantDel: "badge-mute"},
		{name: "未发布有值", unpublished: 3, deleted: 0, wantUnpublished: "badge-warn", wantDel: "badge-mute"},
		{name: "已删除有值", unpublished: 0, deleted: 2, wantUnpublished: "badge-mute", wantDel: "badge-danger"},
		// 旧 helper 是 `n != 0`：负数同样算「有」，这条用例专门防止有人把判据改成 `> 0`。
		{name: "负数也算非零", unpublished: -1, deleted: -1, wantUnpublished: "badge-warn", wantDel: "badge-danger"},
		{name: "计数键缺席落中性", unpublished: nil, deleted: nil, wantUnpublished: "badge-mute", wantDel: "badge-mute"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// nil 表示「这个计数键缺席」：必须把键显式放进 extra 并置 nil，
			// 否则 helper 的默认值会让「缺席」这一档悄悄变成「值为 0」，判据就成了空转。
			extra := map[string]any{"UnpublishedCount": tc.unpublished, "DeletedCount": tc.deleted}
			out := renderSiteSlotsPage(t, []map[string]any{row}, extra)
			if !strings.Contains(out, `badge `+tc.wantUnpublished+`">已绑定但未发布`) {
				t.Errorf("「已绑定但未发布」计数徽章的档位应为 %s", tc.wantUnpublished)
			}
			if !strings.Contains(out, `badge `+tc.wantDel+`">绑定的页面已被删除`) {
				t.Errorf("「绑定的页面已被删除」计数徽章的档位应为 %s", tc.wantDel)
			}
		})
	}
}

// TestSiteSlotStateKeysComeFromSeededKeys 模板里的状态词条 key 必须与 seed 双语成对。
//
// 判据与 block_row.html 的 kind 对账同形（TestBlockRowLabelsComeFromSeededKeys）：
// 模板里的中文只是 t() 的兜底，真正显示什么由 sys_i18n 的词条决定 ——
// key 拼错或漏 seed 的症状是「中文环境看着正常、英文环境露出 key 本身」。
func TestSiteSlotStateKeysComeFromSeededKeys(t *testing.T) {
	stateKeyRe := regexp.MustCompile(`"(admin\.site_slots\.state\.[a-z]+)"`)
	keys := keySetOf(stateKeyRe, adminTemplateSource(t, "site_slots.html"))

	// 非空自检：解析塌成 0（正则写坏 / 文件被搬走）时下面的断言会变成空转。
	if len(keys) != 4 {
		t.Fatalf("模板里应恰好解析出 4 个状态 key，实际 %d 个：%v", len(keys), sortedKeys(keys))
	}
	seeded := seededI18nKeys(t)
	for _, key := range sortedKeys(keys) {
		langs := seeded[key]
		if langs == nil {
			t.Errorf("site_slots.html 用了 key %s，但迁移里没有这条词条", key)
			continue
		}
		for _, lang := range []string{"zh-CN", "en-US"} {
			if !langs[lang] {
				t.Errorf("key %s 缺 %s 词条（模板里的中文只是 t() 兜底）", key, lang)
			}
		}
	}
}

// TestSiteSlotStateLabelFollowsLanguageAndFallback 中英两语与缺词回退链。
//
// 三级判据：① 词条有请求语言 → 出该语言译文；② 只有默认语言（zh-CN）→ 回退默认语言，
// 不是模板兜底、更不是 key 本身；③ 什么都没有 → 落模板里的中文兜底，且不得露出裸 key。
func TestSiteSlotStateLabelFollowsLanguageAndFallback(t *testing.T) {
	publishedRow := siteSlotRowFacts("checkout", map[string]any{
		"Bound": true, "PageDeleted": false, "Published": true, "Unbound": false,
	})
	deletedRow := siteSlotRowFacts("checkout", map[string]any{
		"Bound": true, "PageDeleted": true, "Published": false, "Unbound": false,
	})

	// 缓存是包级单例：本用例注入的词条必须清理，否则后面的用例会按注入值取词。
	t.Cleanup(func() { i18n.InjectForTest(map[string]map[string]string{}, nil) })

	t.Run("有请求语言就出译文", func(t *testing.T) {
		i18n.InjectForTest(map[string]map[string]string{
			"admin.site_slots.state.published": {"zh-CN": "已发布", "en-US": "Published"},
		}, nil)
		out := renderSiteSlotsPage(t, []map[string]any{publishedRow}, nil)
		if !strings.Contains(out, "Published") {
			t.Errorf("英文词条存在时应出英文译文，实际没有")
		}
		if strings.Contains(out, "admin.site_slots.state.published") {
			t.Errorf("输出里露出了裸 key（取词没生效）")
		}
	})

	t.Run("只有默认语言就回退默认语言", func(t *testing.T) {
		i18n.InjectForTest(map[string]map[string]string{
			"admin.site_slots.state.deleted": {"zh-CN": "注入的默认语言文案"},
		}, nil)
		out := renderSiteSlotsPage(t, []map[string]any{deletedRow}, nil)
		if !strings.Contains(out, "注入的默认语言文案") {
			t.Errorf("请求语言缺词时应回退默认语言词条，实际没有（回退链断了）")
		}
	})

	t.Run("没有任何词条就落模板兜底且不露 key", func(t *testing.T) {
		i18n.InjectForTest(map[string]map[string]string{}, nil)
		out := renderSiteSlotsPage(t, []map[string]any{publishedRow}, nil)
		if !strings.Contains(out, `<span class="badge badge-ok">已发布</span>`) {
			t.Errorf("无词条时应落模板里的中文兜底「已发布」")
		}
		if strings.Contains(out, "admin.site_slots.state.") {
			t.Errorf("输出里露出了状态词条的裸 key")
		}
	})
}
