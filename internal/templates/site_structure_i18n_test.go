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
	"admin/site_slots",
	"admin/theme",
	"admin/theme_settings",
	"admin/pages",
	"admin/page_translations",
	"admin/blocks",
	"admin/navigations",
	"admin/navigation_translations",
	"admin/menus",
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
			name: "admin/site_slots",
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
			name: "admin/theme",
			data: map[string]any{
				"SelectedProject": "p1",
				"Projects":        []map[string]any{{"ID": "p1", "Name": "官网"}},
				// 新建走抽屉后按钮显隐依赖权限集合（shell.Prepare 一定注入，测试数据补齐）。
				"PermSet": map[string]any{"theme:create": true},
				"Themes": []map[string]any{{
					"ID": "t1", "ProjectID": "p1", "Name": "春季促销版", "IsActive": false,
					"CreatedAt": "2026-01-01", "UpdatedAt": "2026-01-02",
				}},
			},
			wants: []string{"主题管理", "主题列表", "名称", "未激活", "设置", "激活", "删除", "确定删除主题「"},
		},
		{
			name: "admin/theme_settings",
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
			name: "admin/pages",
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
			name: "admin/page_translations",
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
			wants: []string{"多语言 · ", "已保存 ", "切换语言", "本页完成度", "全站完成度", "筛选：", "全部", "只看缺失", "只看人工", "只看 AI 翻译", "字段", "富文本", "填写译文", "上限 ", "缺失", "保存全部", "还用在另外 "},
		},
		{
			name: "admin/blocks",
			data: map[string]any{
				"SelectedProject": "p1",
				"Projects":        []map[string]any{{"ID": "p1", "Name": "官网"}},
				"PermSet":         map[string]any{"block:create": true},
				"Headers":         []map[string]any{{"ID": "b1", "Name": "站点页眉", "UpdatedAt": "2026-01-02"}},
				"Footers":         []map[string]any{{"ID": "b2", "Name": "站点页脚", "UpdatedAt": "2026-01-02"}},
				"Blocks":          []map[string]any{{"ID": "b3", "Name": "商品卡", "KindLabel": "区块", "ReuseModeLabel": "一次性复制", "UpdatedAt": "2026-01-02"}},
			},
			wants: []string{"全局块", "新建并编辑", "页眉", "页脚", "区块 / 复用资产", "复用方式", "说明", "确定删除块「"},
		},
		{
			name: "admin/navigations",
			data: map[string]any{
				"SelectedProject": "p1", "Kind": "header",
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
			wants: []string{"导航菜单", "页眉导航", "添加菜单项", "当前窗口", "新标签页", "作为顶级项", "放到「", "从已有内容添加", "暂无公开路径", "加入菜单", "菜单结构", "菜单项", "打开方式", "编辑", "保存", "确定删除「"},
		},
		{
			name: "admin/navigation_translations",
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
			name: "admin/menus",
			data: map[string]any{
				"Rows": []map[string]any{{
					"ID": "m1", "ParentID": "0", "Title": "内容管理", "Path": "/admin/pages",
					"Remark": "站点结构", "Icon": "folder", "Type": 1, "Status": 1,
					"SortOrder": 1, "Indent": "",
				}},
				"Parents": []map[string]any{{"ID": "m0", "Title": "根", "Type": 1, "Indent": ""}},
				"PermSet": map[string]any{"menu:create": true, "menu:update": true, "menu:delete": true},
			},
			wants: []string{"菜单管理", "新建菜单", "筛选：", "图标", "标题", "类型", "路径", "状态", "排序", "备注", "操作", "目录", "启用", "菜单标题", "上级菜单", "根菜单", "取消", "创建菜单", "保存修改", "确认删除？"},
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
