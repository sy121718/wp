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

// TestAdminFormFallbackIsGone 裸控件兜底不得回来。
//
// 它曾经存在（.admin-layout :where(input…, select, textarea)，审计 UIK-009）。2026-10-01 清洗：
// 真裸控件已清零（见 admin_form_base_test.go），控件外观只有「基座类」一个真源 ——
// 「容器可以强制给外观」会把这一个真源变成两个（基座类 + 容器兜底），
// 往后每次改基座都要重新判断谁赢。这里反向钉住它不回流。
func TestAdminFormFallbackIsGone(t *testing.T) {
	sels := uiCssSelectors(readUIOwnershipFile(t, "static/css/ui.css"))
	for _, s := range sels {
		if strings.Contains(s, ".admin-layout") {
			t.Errorf("ui.css 又出现了 .admin-layout 相关规则 %q：控件外观只应由基座类提供", s)
		}
	}
	// 作用域隔离（原断言保留）：.admin-layout 只应出现在后台外壳模板里。
	// 递归枚举（模板已按后端模块分进子目录）：退回 `admin/*.html` 只会扫到根下 3 个壳页面，
	// 「.admin-layout 只该有一个宿主」这条判据就形同虚设了。
	pages := adminTemplateFiles(t)
	if len(pages) == 0 {
		t.Fatalf("枚举 admin 模板失败：一个都没找到")
	}
	for _, p := range pages {
		if p == filepath.Join("admin", "layout.html") {
			continue
		}
		if strings.Contains(readUIOwnershipFile(t, p), "admin-layout") {
			t.Errorf("%s 自带 .admin-layout：这个类只该有一个宿主（admin/layout.html）", p)
		}
	}
}

// TestLegacyBridgeSelectorsRemovedFromBase ui.css 里没有任何容器桥接选择器。
//
// 三种桥接（.pages-form / .attr-form-head / .locale-add input）到 2026-10-01 全部退役：
// 控件模板都带基座类了，靠祖先容器给外观的写法留着只会让「输入框长什么样」有两个真源。
func TestLegacyBridgeSelectorsRemovedFromBase(t *testing.T) {
	sels := uiCssSelectors(readUIOwnershipFile(t, "static/css/ui.css"))
	for _, prefix := range []string{".pages-form ", ".pages-form:", ".attr-form-head", ".locale-add"} {
		if hits := uiCssSelWithPrefix(sels, prefix); len(hits) > 0 {
			t.Errorf("ui.css 仍有容器桥接选择器 %v：模板已带基座类，视觉只该由基座类提供", hits)
		}
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
	// 递归列出（模板已按后端模块分进子目录）：退回 `admin/*.html` + `admin/partials/*.html`
	// 会只扫到根下 3 个壳页面与 8 个共享片段，模块目录里的页面与片段一个都不查。
	files := adminTemplateFiles(t)
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

// TestDataTableActionsHoverExcludesSelected sticky 操作列的「悬停 / 选中」必须互斥，
// 不能靠书写顺序决胜。
//
// 现状背景：`.data-table tbody tr:hover td.col-actions` 与 `.data-table tbody tr.is-selected
// td.col-actions` 特异性**完全相同**（各 3 个类 + 3 个元素），历史上靠「选中态写在后」生效。
// 实测（Chromium，forced-hover 伪类 + 只把选中规则在内存样式表里上移一行）：顺序一改，
// 选中行悬停的 td.col-actions 立刻从选中色 #e4e4e4 掉回悬停色 #ececec —— 即
// 「选中行悬停保持选中色」是书写顺序的副作用，任何一次重排都会静默回归。
// 给悬停那条加 :not(.is-selected) 后命中集合互斥，搬动顺序不再改变结果（同法实测）。
func TestDataTableActionsHoverExcludesSelected(t *testing.T) {
	src := uiCssStripComments(readUIOwnershipFile(t, "static/css/ui.css"))
	const wantHover = ".data-table tbody tr:not(.is-selected):hover td.col-actions"
	if !strings.Contains(src, wantHover) {
		t.Errorf("ui.css 缺少互斥的悬停规则 %q：不带 :not(.is-selected) 时它与选中规则同权重，"+
			"选中行悬停的底色由两条规则的书写顺序决定（实测顺序一改即变悬停色）", wantHover)
	}
	if strings.Contains(src, ".data-table tbody tr:hover td.col-actions") {
		t.Error("ui.css 仍有未排除选中态的悬停规则 .data-table tbody tr:hover td.col-actions：" +
			"它与选中规则命中集合重叠、特异性相同，胜负只由书写顺序决定")
	}
	// 选中规则本身也不得消失：sticky 列有自己的不透明背景，不跟随 tr.is-selected，
	// 删掉它会让选中行的操作列退回基色（整行只剩操作列「断了一截」）。
	if !strings.Contains(src, ".data-table tbody tr.is-selected td.col-actions") {
		t.Error("ui.css 缺少选中态的 sticky 操作列规则 .data-table tbody tr.is-selected td.col-actions")
	}
}

// TestLangSelectDelegatesBaseChromeToModifier 语言下拉不再用「自己的类排在基类之后」
// 压掉基座的尺寸 —— 紧凑尺寸提升为基座修饰类 .form-select--sm（forms 段）。
//
// 为什么要这条：`.lang-select` 原本重写了 height / padding / font-size / font-family，
// 全部靠书写顺序覆盖 .form-select。同样的顺序依赖已经造成过一次事故（块内注释记载：
// 有人用 `background` 简写写过一次，把基座的箭头图整块抹掉）。
// 判据三条：① 修饰类存在且进得了产物（forms 段、非后台专属）；② 修饰类不许写 padding 简写
// 或 padding-inline-end（那会重置 / 重复基座给箭头让位的算法）；③ .lang-select 块内不得再出现
// 基座已承载的属性名（尺寸 / 排版 / 背景 / 箭头），模板必须同时带基类与修饰类。
func TestLangSelectDelegatesBaseChromeToModifier(t *testing.T) {
	src := uiCssStripComments(readUIOwnershipFile(t, "static/css/ui.css"))

	const smSelector = ".form-select.form-select--sm"
	smBlock, ok := cssRuleBlock(src, smSelector)
	if !ok {
		t.Fatalf("ui.css 缺少 %s：紧凑尺寸修饰类必须写成**双类**，把特异性提到基座基础块之上；"+
			"写成单类就退回「靠书写顺序压基座」，任何一次重排都会让它静默失效", smSelector)
	}
	if regexp.MustCompile(`(?m)^[ \t]*\.form-select--sm[ \t]*\{`).MatchString(src) {
		t.Error("ui.css 仍有单类形式的 .form-select--sm 规则：它与基座基础块 .form-select 同为 (0,1,0)，" +
			"「修饰类赢基座」只能靠书写顺序")
	}
	if !cssMoreSpecific(smSelector, ".form-select") {
		t.Errorf("%s 的特异性不高于 .form-select（%v vs %v）：修饰类赢基座要由特异性保证，不是顺序",
			smSelector, cssSpecificity(smSelector), cssSpecificity(".form-select"))
	}
	smProps := cssPropNames(smBlock)
	for _, forbidden := range []string{"padding", "padding-inline-end", "padding-right", "font-size", "font-family"} {
		if _, hit := smProps[forbidden]; hit {
			t.Errorf(".form-select--sm 声明了 %q：padding 简写会重置基座给箭头让位的 padding-inline-end，"+
				"padding-inline-end/right 则是把箭头算法抄了第二份（改一处漏一处）", forbidden)
		}
	}
	for _, want := range []string{"height", "padding-block", "padding-inline-start"} {
		if _, hit := smProps[want]; !hit {
			t.Errorf(".form-select--sm 缺少 %q：紧凑尺寸没写在修饰类里，模板换类名后视觉会变", want)
		}
	}

	// .lang-select 只允许保留与基座不重复的差异（形状 / 语义色 / 指针）。
	lsBlock, ok := cssRuleBlock(src, ".lang-select")
	if !ok {
		t.Fatal("ui.css 缺少 .lang-select 规则块：该块属于 langswitch 段（owner=backend），类名必须保留")
	}
	baseOwned := []string{
		"height", "width", "min-width", "box-sizing",
		"padding", "padding-block", "padding-inline", "padding-top", "padding-right",
		"padding-bottom", "padding-left", "padding-inline-start", "padding-inline-end",
		"font-size", "font-family", "line-height",
		"background", "background-color", "background-image", "background-repeat",
		"background-position", "background-size",
		"appearance", "-webkit-appearance", "transition", "outline",
	}
	lsProps := cssPropNames(lsBlock)
	for _, prop := range baseOwned {
		if _, hit := lsProps[prop]; hit {
			t.Errorf(".lang-select 重复声明了基座已承载的 %q：这条规则排在基座之后，"+
				"重复即「靠书写顺序压基座」，改基座时这里会被静默盖住", prop)
		}
	}
	// 有意保留的差异（不许被「顺手清理」掉）：外层边线 / 正文色 / 胶囊形状 / 手形指针。
	for _, want := range []string{"border", "border-radius", "color", "cursor"} {
		if _, hit := lsProps[want]; !hit {
			t.Errorf(".lang-select 丢了 %q：它是与基座**有意的差异**（更弱的边线与文字色、胶囊形状、"+
				"手形指针），删掉就不是零变化了", want)
		}
	}

	// 模板必须同时带基类与修饰类 —— 只改 CSS 不改模板，尺寸真源依旧落在页面上。
	layout := readUIOwnershipFile(t, filepath.Join("admin", "layout.html"))
	if !strings.Contains(layout, `class="form-select form-select--sm lang-select"`) {
		t.Error(`admin/layout.html 的语言下拉必须写成 class="form-select form-select--sm lang-select"：` +
			"紧凑尺寸在基座修饰类里，模板不带它就会退回基座默认尺寸（视觉变化）")
	}

	// 修饰类必须在 forms 段（shared）：产物里要能拿到它，否则站点侧的紧凑下拉没有样式来源。
	out, err := builder.RenderDocument(&builder.CompiledPage{
		Lang:    "zh-CN",
		HTML:    `<div class="form-group"><input class="form-input" type="text"></div>`,
		UIStyle: UICSS(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, ".form-select.form-select--sm") {
		t.Error("产物里没有 .form-select.form-select--sm：它不在 forms 段（或被误判成后台专属段整段跳过）")
	}
}

// TestLangSelectKeepsSingleClassSpecificity 语言下拉必须**刻意**保持单类 (0,1,0)。
//
// 这是本块真正的技术点，也是「别顺手把特异性提上去」的机器判据。它要的层次是：
//   · 靠书写顺序赢基座**基础**块 `.form-input, .form-select, .form-textarea`（同为 (0,1,0)、
//     写在后面）—— 胶囊形状 / 语义色 / 指针要生效；
//   · 同时**严格弱于**基座的状态规则 `.form-select:focus` / `:focus-visible` / `:disabled`
//     （(0,2,0)）—— 聚焦时边框该变主色、禁用时该变淡。
//
// 反面实测（Chromium，自建 headless 实例，只改内存里的样式表文本、不动磁盘）：把本块选择器
// 临时改成 `.form-select.lang-select`（(0,2,0)，位置不变）后聚焦语言下拉，border-color 从
// 主色 rgb(174,182,192) 掉成灰 rgb(229,231,235)，页面上没有任何报错 —— 焦点反馈被静默压掉。
// 这正是「本段在文件末尾、靠顺序取胜」的代价，所以这里的『弱』是有意的，不是没收拾干净。
func TestLangSelectKeepsSingleClassSpecificity(t *testing.T) {
	src := uiCssStripComments(readUIOwnershipFile(t, "static/css/ui.css"))
	re := regexp.MustCompile(`(?m)^[ \t]*([^{}\n]*\.lang-select[^{}\n]*?)[ \t]*\{`)
	seen := map[string]bool{}
	for _, m := range re.FindAllStringSubmatch(src, -1) {
		sel := strings.Join(strings.Fields(m[1]), " ")
		seen[sel] = true
		switch sel {
		case ".lang-select", ".lang-select:hover":
		default:
			t.Errorf("ui.css 出现选择器 %q：.lang-select 只允许单类基础规则与它的 :hover。"+
				"祖先选择器 / 双类 / 额外伪类都会把它抬到 (0,2,0)，与 .form-select:focus 平级，"+
				"而本段在文件末尾靠顺序取胜 —— 聚焦时的主色边框会被静默压掉（实测过）", sel)
		}
	}
	if !seen[".lang-select"] {
		t.Fatal("ui.css 缺少 .lang-select 规则块：该块属于 langswitch 段（owner=backend），类名必须保留")
	}
	if !seen[".lang-select:hover"] {
		t.Error("ui.css 缺少 .lang-select:hover：悬停底色是语言下拉的既有行为")
	}
	if spec := cssSpecificity(".lang-select"); spec != [3]int{0, 1, 0} {
		t.Errorf(".lang-select 的选择器特异性是 %v，必须严格等于 (0,1,0)：它要弱于基座的状态规则", spec)
	}
	if !cssMoreSpecific(".form-select:focus", ".lang-select") {
		t.Error(".lang-select 的特异性不低于 .form-select:focus：聚焦态会被它压掉")
	}
	if !cssMoreSpecific(".form-select:disabled", ".lang-select") {
		t.Error(".lang-select 的特异性不低于 .form-select:disabled：禁用态会被它压掉")
	}
}

// cssSpecificity 计算选择器的特异度 (id, 类/属性/伪类, 元素/伪元素)。
//
// 刻意用最小实现而不是引入 CSS 解析库：本文件只需要「能比较大小」这一件事，
// 且用到的语法固定（类 / 元素 / 伪类 / 属性；不用 :is()/:where()/:not() 参与比较）。
// 函数式伪类的参数会被整体跳过（本项目里参与比较的选择器都没有参数）。
func cssSpecificity(sel string) [3]int {
	var spec [3]int
	i := 0
	for i < len(sel) {
		switch ch := sel[i]; {
		case ch == '#':
			spec[0]++
			i = skipCSSIdent(sel, i+1)
		case ch == '.':
			spec[1]++
			i = skipCSSIdent(sel, i+1)
		case ch == '[':
			spec[1]++
			for i < len(sel) && sel[i] != ']' {
				i++
			}
			i++
		case ch == ':':
			if i+1 < len(sel) && sel[i+1] == ':' { // 伪元素算元素层
				spec[2]++
				i = skipCSSIdent(sel, i+2)
				continue
			}
			spec[1]++ // 伪类
			i = skipCSSIdent(sel, i+1)
			if i < len(sel) && sel[i] == '(' { // 函数式伪类的参数整体跳过
				depth := 0
			paramLoop:
				for i < len(sel) {
					switch sel[i] {
					case '(':
						depth++
					case ')':
						depth--
						if depth == 0 {
							i++
							break paramLoop
						}
					}
					i++
				}
			}
		case ch == '*' || ch == '>' || ch == '+' || ch == '~' || ch == ' ' || ch == '\t' || ch == ',':
			i++
		default:
			if isCSSIdentByte(ch) {
				spec[2]++
				i = skipCSSIdent(sel, i)
				continue
			}
			i++
		}
	}
	return spec
}

func skipCSSIdent(s string, i int) int {
	for i < len(s) && isCSSIdentByte(s[i]) {
		i++
	}
	return i
}

func isCSSIdentByte(b byte) bool {
	return b == '-' || b == '_' || (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z') || (b >= '0' && b <= '9')
}

// cssMoreSpecific a 的特异度是否严格高于 b。
func cssMoreSpecific(a, b string) bool {
	sa, sb := cssSpecificity(a), cssSpecificity(b)
	for i := 0; i < 3; i++ {
		if sa[i] != sb[i] {
			return sa[i] > sb[i]
		}
	}
	return false
}

// cssRuleBlock 取出「独占一行的选择器 + { ... }」规则的声明体。
//
// 适用前提（本文件满足）：目标选择器独占一行、块内无嵌套规则。`.lang-select` 这样的
// 前缀同时出现在 `.lang-select:hover` 里，所以正则要求选择器后紧跟 `{`，避免误取派生规则。
func cssRuleBlock(src, selector string) (string, bool) {
	re := regexp.MustCompile(`(?m)^[ \t]*` + regexp.QuoteMeta(selector) + `[ \t]*\{([^}]*)\}`)
	m := re.FindStringSubmatch(src)
	if m == nil {
		return "", false
	}
	return m[1], true
}

// cssPropNames 取出一段声明体里的属性名集合（简写与长写都按原样返回）。
func cssPropNames(block string) map[string]struct{} {
	out := map[string]struct{}{}
	for _, m := range regexp.MustCompile(`(?m)^[ \t]*(-?[a-zA-Z][a-zA-Z-]*)[ \t]*:`).FindAllStringSubmatch(block, -1) {
		out[strings.ToLower(m[1])] = struct{}{}
	}
	return out
}

// classTokens 取出一段 HTML 里 class 属性的所有类名 token。
func classTokens(src string) []string {
	var out []string
	for _, m := range regexp.MustCompile(`class="([^"]*)"`).FindAllStringSubmatch(src, -1) {
		out = append(out, strings.Fields(m[1])...)
	}
	return out
}
