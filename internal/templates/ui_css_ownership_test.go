package templates

// ui_css_ownership_test.go — 控件外观的归属与死代码守卫（审计 UIK-009 / UIK-010 / UIK-011）。
//
// UIK-010 的处置口径（先证明、再删；证不出来的保留）：
//   · 删除的部分确实死透了：theme.css 里的 .pages-* / .attr-form* / .var-actions /
//     .theme-color-val / .pages-fold* 这批规则，删前实测「匹配元素数为 0」——
//     不是「被桥接压制」，是没有任何元素会命中（admin 模板在同期已把它们迁到公共类，
//     并由 pages_class_migration_test.go 钉住）。ui.css 桥接段里的 .pages-form /
//     .attr-form-head 选择器同理：容器不再存在，选择器留着只会让「哪些容器还受支持」含糊。
//     .locale-add input 保留：settings.html 的语言新增行仍在使用（本轮唯一还在用的桥接）。
//   · 曾经判定为「被压制」的两条（.pages-form input / select 的 min-width: 220px、
//     .locale-add input 的视觉五项）在删除前用特异性 + 加载顺序证明过它们从未生效：
//     layout.html 先引 theme.css 再引 ui.css，桥接的 (0,6,1)/(0,1,1) 分别压过 theme.css 的
//     (0,3,1)/(0,1,1)，且每一项声明都能在桥接里找到对应项。
//
// UIK-011 的处置：抽屉外观搬进 ui.css（它的行为本来就在基座 uiBlocks 里）；
//   tabs 没有搬 —— 复核发现后台模板里没有任何页签结构（唯一成体系的页签在工作台的
//   workbench.css：.wb-tabs / .wb-lib-tabs / .wb-subtabs，命名与密度都是工作台专属）。
//   给 ui.css 加一个没有使用点的 tabs 块等于新增死样式，所以不加，改成把「后台出现页签」
//   变成一条会失败的契约，提醒收敛到基座（见最后一个测试）。
//
// UI-01 的处置：后台专属段（owner=backend）不进产物（见 builder 的 uiCSSOwner）。
//   两条门禁：① 产物注入的 CSS 不含后台专属规则；② 后台专属类不得出现在 admin/ 与
//   workbench/ 之外的模板里（这两处都是 <link> 直引整份 ui.css 的控制面，只有站点产物走段切分）。

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"go_wp/internal/builder"
)

var uiCssCommentRe = regexp.MustCompile(`(?s)/[*].*?[*]/`)

func uiCssStripComments(src string) string {
	return uiCssCommentRe.ReplaceAllString(src, "")
}

// uiCssSelectors 提取顶层规则的选择器并按逗号拆成单项（@media 内的规则不取：本文件关心的
// 表单与控件规则都在顶层，且 @media 内的选择器提取需要额外的嵌套处理，收益不抵复杂度）。
func uiCssSelectors(src string) []string {
	css := uiCssStripComments(src)
	var out []string
	depth, start := 0, 0
	for i := 0; i < len(css); i++ {
		switch css[i] {
		case 0x7B: // 左花括号
			if depth == 0 {
				for _, sel := range strings.Split(css[start:i], ",") {
					sel = strings.Join(strings.Fields(sel), " ")
					if sel != "" && !strings.HasPrefix(sel, "@") {
						out = append(out, sel)
					}
				}
			}
			depth++
		case 0x7D: // 右花括号
			depth--
			if depth <= 0 {
				depth = 0
				start = i + 1
			}
		}
	}
	return out
}

func uiCssHasSelector(sels []string, want string) bool {
	for _, s := range sels {
		if s == want {
			return true
		}
	}
	return false
}

func uiCssSelWithPrefix(sels []string, prefix string) []string {
	var out []string
	for _, s := range sels {
		if strings.HasPrefix(s, prefix) {
			out = append(out, s)
		}
	}
	sort.Strings(out)
	return out
}

func readUIOwnershipFile(t *testing.T, rel string) string {
	t.Helper()
	raw, err := os.ReadFile(rel)
	if err != nil {
		t.Fatalf("读取 %s 失败: %v", rel, err)
	}
	return string(raw)
}

// TestAdminFormFallbackScopeIsNarrow 裸控件兜底必须存在、且只用 :where 降特异性。
func TestAdminFormFallbackScopeIsNarrow(t *testing.T) {
	sels := uiCssSelectors(readUIOwnershipFile(t, "static/css/ui.css"))
	var fallback []string
	for _, s := range sels {
		if strings.Contains(s, ".admin-layout") {
			fallback = append(fallback, s)
		}
	}
	if len(fallback) == 0 {
		t.Fatal("ui.css 缺少后台裸控件的兜底规则（.admin-layout :where(...)）：新增裸控件会掉回浏览器默认外观")
	}
	for _, s := range fallback {
		if !strings.HasPrefix(s, ".admin-layout :where(") {
			t.Errorf("兜底选择器 %q 没有用 :where() 降特异性：它会和基座类、桥接规则抢样式", s)
		}
	}
	// 作用域隔离：.admin-layout 只应出现在后台外壳模板里，否则兜底会跟着产物投递出去。
	pages, err := filepath.Glob("admin/*.html")
	if err != nil || len(pages) == 0 {
		t.Fatalf("枚举 admin 模板失败: %v", err)
	}
	for _, p := range pages {
		if p == filepath.Join("admin", "layout.html") {
			continue
		}
		if strings.Contains(readUIOwnershipFile(t, p), "admin-layout") {
			t.Errorf("%s 自带 .admin-layout：兜底样式的作用域前提被打破（这个类只该有一个宿主）", p)
		}
	}
}

// TestLegacyBridgeSelectorsRemovedFromBase ui.css 里不再有零匹配的容器桥接选择器。
func TestLegacyBridgeSelectorsRemovedFromBase(t *testing.T) {
	sels := uiCssSelectors(readUIOwnershipFile(t, "static/css/ui.css"))
	for _, prefix := range []string{".pages-form ", ".pages-form:", ".attr-form-head"} {
		if hits := uiCssSelWithPrefix(sels, prefix); len(hits) > 0 {
			t.Errorf("ui.css 仍有零匹配的桥接选择器 %v：容器已不在 admin 模板里出现（pages_class_migration_test.go），应改用基座类", hits)
		}
	}
	if !uiCssHasSelector(sels, ".locale-add input") {
		t.Error("ui.css 缺少 .locale-add input：settings.html 的语言新增行仍依赖它（本轮唯一还在用的桥接）")
	}
	// 反向：桥接的宿主仍在模板里，否则上面那条断言会因为「容器也没了」而失去意义。
	if !strings.Contains(readUIOwnershipFile(t, filepath.Join("admin", "settings.html")), "locale-add") {
		t.Error("settings.html 不再使用 locale-add：ui.css 的 .locale-add input 已成死选择器，请一并删除")
	}
}

// TestThemeCSSHasNoOrphanControlChrome theme.css 不再保留零匹配的旧控件规则（UIK-010 的验收）。
func TestThemeCSSHasNoOrphanControlChrome(t *testing.T) {
	sels := uiCssSelectors(readUIOwnershipFile(t, "static/css/theme.css"))
	prefixes := []string{".pages-", "details.pages-", ".attr-form", ".var-actions", ".theme-color-val"}
	for _, s := range sels {
		for _, p := range prefixes {
			if strings.HasPrefix(s, p) {
				t.Errorf("theme.css 仍有旧控件规则 %q：这些类在 admin 模板里已零匹配（迁到公共类后无人引用），"+
					"视觉来源只该有一处（ui.css 基座层）", s)
			}
		}
	}
}

// TestDrawerChromeLivesInBase 抽屉外观在基座，业务页面只留布局。
func TestDrawerChromeLivesInBase(t *testing.T) {
	uiSels := uiCssSelectors(readUIOwnershipFile(t, "static/css/ui.css"))
	themeSels := uiCssSelectors(readUIOwnershipFile(t, "static/css/theme.css"))
	for _, sel := range []string{".drawer", ".drawer-mask", ".drawer-head", ".drawer-close", ".drawer-body"} {
		if !uiCssHasSelector(uiSels, sel) {
			t.Errorf("ui.css 缺少抽屉基座选择器 %s（审计 UIK-011：行为在基座，外观也该在）", sel)
		}
		if uiCssHasSelector(themeSels, sel) {
			t.Errorf("theme.css 仍在定义抽屉外观 %s：抽屉是基座控件，外观只该在 ui.css", sel)
		}
	}
	// 结构仍在后台外壳里：样式与 data-drawer 协议配套，缺一不可。
	layout := readUIOwnershipFile(t, filepath.Join("admin", "layout.html"))
	for _, want := range []string{`class="drawer-mask"`, `class="drawer"`, "data-drawer"} {
		if !strings.Contains(layout, want) {
			t.Errorf("admin/layout.html 缺少抽屉结构 %s：ui.css 的抽屉基座块会变成无宿主的死样式", want)
		}
	}
	// 动效时长与曲线取统一令牌，而不是搬迁时把裸值一起带过来。
	ui := uiCssStripComments(readUIOwnershipFile(t, "static/css/ui.css"))
	for _, raw := range []string{"220ms", "opacity 200ms ease"} {
		if strings.Contains(ui, raw) {
			t.Errorf("ui.css 的抽屉仍有裸时长 %q：应取 --ui-motion-base（后台动效只有一个时长来源）", raw)
		}
	}
}

// TestAdminTabsBaseAndUsageStayInSync 后台页签用法与 ui.css 基座块必须同时存在。
//
// 原判据（审计 UIK-011）是单向的：后台零用法之前基座不加 tabs 块，避免预置死样式，
// 并约定等真正出现用法时「按这些用法归纳基座类，与 workbench.css 的 .wb-tabs / .wb-subtabs 划清边界」。
// 现在后台确实有了用法（admin/masterdata_changes.html 的「逐条记录 / 按实体汇总」），
// 基座已按约定补进 ui.css —— 判据随之转成双向守卫：
//
//	· 有用法、无基座 → 页签退化成浏览器默认的描边方块按钮（本仓库真踩过一次）；
//	· 无用法、有基座 → 死样式。
//
// 边界：基座只管通用 .tabs / .tab-list / .tab / .tab-panel；
// 工作台专用的 .wb-tabs / .wb-subtab 归 workbench.css，不在本判据范围内。
func TestAdminTabsBaseAndUsageStayInSync(t *testing.T) {
	tabTokens := map[string]bool{
		"tab": true, "tabs": true, "nav-tabs": true, "tab-nav": true, "tab-btn": true,
		"tab-panel": true, "tab-content": true, "tablist": true, "tab-item": true, "subtab": true,
		"tab-list": true,
	}
	var found []string
	files, err := filepath.Glob("admin/*.html")
	if err != nil {
		t.Fatal(err)
	}
	partials, _ := filepath.Glob(filepath.Join("admin", "partials", "*.html"))
	files = append(files, partials...)
	for _, p := range files {
		for _, token := range classTokens(readUIOwnershipFile(t, p)) {
			if tabTokens[token] {
				found = append(found, p+" → "+token)
			}
		}
	}
	sort.Strings(found)

	sels := uiCssSelectors(uiCssStripComments(readUIOwnershipFile(t, "static/css/ui.css")))
	hasBase := uiCssHasSelector(sels, ".tabs") &&
		uiCssHasSelector(sels, ".tab-list") &&
		uiCssHasSelector(sels, ".tab-panel")
	switch {
	case len(found) > 0 && !hasBase:
		t.Errorf("后台出现了页签用法 %v，但 ui.css 没有对应基座块（.tabs / .tab-list / .tab-panel）："+
			"缺基座时页签会退化成浏览器默认的描边方块按钮（role / aria 都对，只是看上去不是一个页签）", found)
	case len(found) == 0 && hasBase:
		t.Error("ui.css 预置了页签基座，但后台没有任何使用点：这是新增死样式（审计 UIK-011）")
	}
}

// TestProductUICSSSkipsBackendOnlySections 产物不再带上后台专属段的规则（审计 UI-01）。
//
// 走真实产物组装路径（builder.RenderDocument）而不是直接调 builder 的内部切分函数：
// 断言的是「渲染出来的文档里没有后台专属规则」，这才是消费端实际拿到的东西。
func TestProductUICSSSkipsBackendOnlySections(t *testing.T) {
	cases := []struct {
		name      string
		html      string
		keep      string
		forbidden string
	}{
		{
			name:      "表单控件：forms 段照常注入，后台兜底不跟随",
			html:      `<div class="form-group"><input class="form-input" type="text"></div>`,
			keep:      ".form-input",
			forbidden: ".admin-layout",
		},
		{
			name:      "后台语言切换：langswitch 段整段跳过",
			html:      `<div class="lang-switch"><select class="lang-select"></select></div>`,
			forbidden: ".lang-select",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out, err := builder.RenderDocument(&builder.CompiledPage{
				Lang:    "zh-CN",
				HTML:    tc.html,
				UIStyle: UICSS(),
			})
			if err != nil {
				t.Fatal(err)
			}
			if tc.keep != "" && !strings.Contains(out, tc.keep) {
				t.Errorf("产物缺少 %q：跳过后台专属段不能把用到的控件段一起清掉", tc.keep)
			}
			if strings.Contains(out, tc.forbidden) {
				t.Errorf("产物里出现后台专属规则 %q（owner=backend 的段应当在注入时整段跳过）：\n"+
					"这类规则只服务后台宿主，进产物是纯字节与语义浪费（审计 UI-01）", tc.forbidden)
			}
		})
	}
}

// TestBackendExclusiveClassesStayInControlPlane 后台专属类不得出现在控制面之外（审计 UI-01）。
//
// 为什么这条是本次改动真正的价值：owner=backend 的段不进产物，所以这些类在站点侧
// **没有样式来源**。一旦有人把 .lang-switch 用到站点组件上，页面不会有任何报错，
// 只是悄悄没有外观 —— 这条测试把它变成红色，逼作者把该类提升为 shared 段。
//
// 允许的宿主是 internal/templates/admin/ 与 internal/templates/workbench/ —— 两者都是
// <link href="/static/css/ui.css"> 直引整份的控制面，不走段切分。
// 局限：只扫模板文件；Go 代码里拼出的 HTML 与 JS 动态添加的类名覆盖不到。
func TestBackendExclusiveClassesStayInControlPlane(t *testing.T) {
	classes, err := builder.BackendExclusiveSectionClasses(UICSS())
	if err != nil {
		t.Fatal(err)
	}
	if len(classes) == 0 {
		t.Fatal("没有解析到后台专属类：owner=backend 的段可能没登记，本约束会在空转中通过")
	}
	// 控制面之外的模板。fragments/*.html 不在列表里：它们是后台 / 工作台页面片段
	// （workbench 的 inspector_panel / outline_tree、project 的 global_panel / settings_panel、
	// product 的 seo_score、content 的 article_import），由后台页面渲染、同样直引整份 ui.css；
	// 站点运行时片段是 fragments/*.jet（cart_view / order_list / user_login …）。
	patterns := []string{
		"components/*/*.jet", "components/*/*.html",
		"fragments/*.jet",
		"user/*.html", "partials/*.html", "*.html",
	}
	var files []string
	for _, pat := range patterns {
		m, err := filepath.Glob(pat)
		if err != nil {
			t.Fatal(err)
		}
		files = append(files, m...)
	}
	// 解析口径自检：一个模板都没枚举到说明路径口径失效，本约束会在空转中通过。
	if len(files) < 20 {
		t.Fatalf("只枚举到 %d 个控制面之外的模板（预期 ≥20）：路径口径可能已失效", len(files))
	}
	owned := map[string]bool{}
	for _, c := range classes {
		owned[c] = true
	}
	for _, f := range files {
		for _, cls := range classTokens(readUIOwnershipFile(t, f)) {
			if owned[cls] {
				t.Errorf("%s 使用了后台专属类 %q（后台专属段：%v）：owner=backend 的段在产物注入时被跳过，"+
					"站点侧没有样式来源；请改用基座公共类，或把该类拆到 shared 段（审计 UI-01）",
					f, cls, classes)
			}
		}
	}
}

// classTokens 取出一段 HTML 里 class 属性的所有类名 token。
func classTokens(src string) []string {
	var out []string
	for _, m := range regexp.MustCompile(`class="([^"]*)"`).FindAllStringSubmatch(src, -1) {
		out = append(out, strings.Fields(m[1])...)
	}
	return out
}
