package templates

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

func uiCssSelectors(src string) []string {
	css := uiCssStripComments(src)
	var out []string
	depth, start := 0, 0
	for i := 0; i < len(css); i++ {
		switch css[i] {
		case 0x7B:
			if depth == 0 {
				for _, sel := range strings.Split(css[start:i], ",") {
					sel = strings.Join(strings.Fields(sel), " ")
					if sel != "" && !strings.HasPrefix(sel, "@") {
						out = append(out, sel)
					}
				}
			}
			depth++
		case 0x7D:
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

func TestAdminFormFallbackIsGone(t *testing.T) {
	sels := uiCssSelectors(readUIOwnershipFile(t, "static/css/ui.css"))
	for _, s := range sels {
		if strings.Contains(s, ".admin-layout") {
			t.Errorf("ui.css 又出现了 .admin-layout 相关规则 %q：控件外观只应由基座类提供", s)
		}
	}

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

func TestLegacyBridgeSelectorsRemovedFromBase(t *testing.T) {
	sels := uiCssSelectors(readUIOwnershipFile(t, "static/css/ui.css"))
	for _, prefix := range []string{".pages-form ", ".pages-form:", ".attr-form-head", ".locale-add"} {
		if hits := uiCssSelWithPrefix(sels, prefix); len(hits) > 0 {
			t.Errorf("ui.css 仍有容器桥接选择器 %v：模板已带基座类，视觉只该由基座类提供", hits)
		}
	}
}

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

	layout := readUIOwnershipFile(t, filepath.Join("admin", "layout.html"))
	for _, want := range []string{`class="drawer-mask"`, `class="drawer"`, "data-drawer"} {
		if !strings.Contains(layout, want) {
			t.Errorf("admin/layout.html 缺少抽屉结构 %s：ui.css 的抽屉基座块会变成无宿主的死样式", want)
		}
	}

	ui := uiCssStripComments(readUIOwnershipFile(t, "static/css/ui.css"))
	for _, raw := range []string{"220ms", "opacity 200ms ease"} {
		if strings.Contains(ui, raw) {
			t.Errorf("ui.css 的抽屉仍有裸时长 %q：应取 --ui-motion-base（后台动效只有一个时长来源）", raw)
		}
	}
}

func TestAdminTabsBaseAndUsageStayInSync(t *testing.T) {
	tabTokens := map[string]bool{
		"tab": true, "tabs": true, "nav-tabs": true, "tab-nav": true, "tab-btn": true,
		"tab-panel": true, "tab-content": true, "tablist": true, "tab-item": true, "subtab": true,
		"tab-list": true,
	}
	var found []string

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
			html:      `<div class="lang-switch"><select class="form-select form-select--sm"></select></div>`,
			forbidden: ".lang-switch",
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

func TestBackendExclusiveClassesStayInControlPlane(t *testing.T) {
	classes, err := builder.BackendExclusiveSectionClasses(UICSS())
	if err != nil {
		t.Fatal(err)
	}
	if len(classes) == 0 {
		t.Fatal("没有解析到后台专属类：owner=backend 的段可能没登记，本约束会在空转中通过")
	}

	patterns := []string{
		"../builder/components/*/*.jet", "components/*.jet", "components/*/*.jet", "components/*.html", "components/*/*.html",
		"fragments/*.jet",
		"partials/*.html", "*.html", "*.jet",
	}
	var files []string
	for _, pat := range patterns {
		m, err := filepath.Glob(pat)
		if err != nil {
			t.Fatal(err)
		}
		files = append(files, m...)
	}

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

func TestLangSelectComesFromBaseAndSizeModifier(t *testing.T) {
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

	if strings.Contains(src, ".lang-select") {
		t.Error("ui.css 又出现了语言下拉的私有视觉类：控件外观只该由基座（.form-select + " +
			".form-select--sm）提供，私有块一旦回来就重新变成两处真源")
	}

	layout := readUIOwnershipFile(t, filepath.Join("admin", "layout.html"))
	if !strings.Contains(layout, `class="form-select form-select--sm"`) {
		t.Error(`admin/layout.html 的语言下拉必须写成 class="form-select form-select--sm"：` +
			"紧凑尺寸在基座修饰类里，模板不带它就会退回基座默认尺寸（视觉变化）")
	}
	if regexp.MustCompile(`class="[^"]*\blang-select\b`).MatchString(layout) {
		t.Error("admin/layout.html 仍在用语言下拉的私有类：模板与 CSS 必须同批退场")
	}

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
			if i+1 < len(sel) && sel[i+1] == ':' {
				spec[2]++
				i = skipCSSIdent(sel, i+2)
				continue
			}
			spec[1]++
			i = skipCSSIdent(sel, i+1)
			if i < len(sel) && sel[i] == '(' {
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

func cssMoreSpecific(a, b string) bool {
	sa, sb := cssSpecificity(a), cssSpecificity(b)
	for i := 0; i < 3; i++ {
		if sa[i] != sb[i] {
			return sa[i] > sb[i]
		}
	}
	return false
}

func cssRuleBlock(src, selector string) (string, bool) {
	re := regexp.MustCompile(`(?m)^[ \t]*` + regexp.QuoteMeta(selector) + `[ \t]*\{([^}]*)\}`)
	m := re.FindStringSubmatch(src)
	if m == nil {
		return "", false
	}
	return m[1], true
}

func cssPropNames(block string) map[string]struct{} {
	out := map[string]struct{}{}
	re := regexp.MustCompile(`(?m)(?:^|[;{])[ \t]*(-?[a-zA-Z][a-zA-Z-]*)[ \t]*:`)
	for _, m := range re.FindAllStringSubmatch(block, -1) {
		out[strings.ToLower(m[1])] = struct{}{}
	}
	return out
}

func classTokens(src string) []string {
	var out []string
	for _, m := range regexp.MustCompile(`class="([^"]*)"`).FindAllStringSubmatch(src, -1) {
		out = append(out, strings.Fields(m[1])...)
	}
	return out
}
