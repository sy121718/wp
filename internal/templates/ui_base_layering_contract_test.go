package templates

import (
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

var (
	baseSharedClasses = []string{".form-input", ".form-select", ".form-textarea", ".wbs-trigger", ".wbd-trigger"}
	baseStateSuffixes = []string{":focus-visible", ":focus", ":disabled", `[aria-invalid="true"]`, "::placeholder"}
)

var sizeProps = []string{
	"height", "min-height", "max-height",
	"width", "min-width", "max-width",
	"padding", "padding-block", "padding-inline", "padding-top", "padding-right",
	"padding-bottom", "padding-left", "padding-block-start", "padding-block-end",
	"padding-inline-start", "padding-inline-end",
	"font-size", "line-height", "box-sizing", "letter-spacing",
}

func TestBaseAppearanceGroupIsSingleSource(t *testing.T) {
	src := uiCssStripComments(readUIOwnershipFile(t, "static/css/ui.css"))
	re := regexp.MustCompile(`(?m)^\.form-input,\s*\n\.form-select,\s*\n\.form-textarea,\s*\n\.wbs-trigger,\s*\n\.wbd-trigger\s*\{([^}]*)\}`)
	m := re.FindStringSubmatch(src)
	if m == nil {
		t.Fatal("共享外观组不是「.form-input, .form-select, .form-textarea, .wbs-trigger, .wbd-trigger」这一条规则：" +
			"控件外观被拆成多处真源（两个自绘触发器都曾因此与基座内边距漂了 2px）")
	}
	props := cssPropNames(m[1])
	allowed := map[string]bool{
		"width": true, "min-width": true, "box-sizing": true,
		"font-family": true, "line-height": true,
		"padding": true, "background": true, "border": true, "border-radius": true,
		"color": true, "font-size": true, "transition": true, "outline": true,
	}
	for p := range props {
		if !allowed[p] {
			t.Errorf("共享外观组多出属性 %q：它是五条选择器共用的外观真源，"+
				"原生 select 专属的东西（箭头）、状态反馈（描边 / 底色 / 透明度）都不该进来", p)
		}
	}
	for _, want := range []string{"padding", "border", "border-radius", "background", "color", "font-size"} {
		if _, hit := props[want]; !hit {
			t.Errorf("共享外观组缺少 %q：控件外观的唯一真源必须完整（缺项会被各页面的容器规则补回去）", want)
		}
	}

	for _, trigSel := range []string{".wbs-trigger"} {
		trig, ok := cssRuleBlock(src, trigSel)
		if !ok {
			t.Fatalf("ui.css 缺少 %s 规则块", trigSel)
		}
		tp := cssPropNames(trig)
		for _, p := range []string{"padding", "border", "border-radius", "background", "color", "font-size", "width", "box-sizing", "transition"} {
			if _, hit := tp[p]; hit {
				t.Errorf("%s 段落又声明了 %q：该属性属于共享外观组，写在这里即第二个真源", trigSel, p)
			}
		}
		for _, want := range []string{"display", "gap", "cursor"} {
			if _, hit := tp[want]; !hit {
				t.Errorf("%s 段落缺少它特有的 %q（可点的值触发器排布）", trigSel, want)
			}
		}
	}
}

func TestBaseStateRulesNeverDeclareSize(t *testing.T) {
	src := uiCssStripComments(readUIOwnershipFile(t, "static/css/ui.css"))
	checked := 0
	for _, cls := range baseSharedClasses {
		for _, st := range baseStateSuffixes {
			body, ok := cssRuleBodyFor(src, cls+st)
			if !ok {
				continue
			}
			checked++
			props := cssPropNames(body)
			for _, p := range sizeProps {
				if _, hit := props[p]; hit {
					t.Errorf("状态规则 %s 声明了尺寸属性 %q：状态规则只该给反馈（边框 / 描边 / 底色 /"+
						"透明度 / 光标），改尺寸会让聚焦时控件跳动，并与尺寸修饰类在同层打架", cls+st, p)
				}
			}
		}
	}
	if checked < 5 {
		t.Fatalf("只扫到 %d 条基座状态规则：选择器枚举口径可能已失效（本契约会空转通过）", checked)
	}
}

func TestSizeModifierWinsBySpecificity(t *testing.T) {
	src := uiCssStripComments(readUIOwnershipFile(t, "static/css/ui.css"))
	const smSelector = ".form-select.form-select--sm"
	body, ok := cssRuleBlock(src, smSelector)
	if !ok {
		t.Fatalf("ui.css 缺少 %s：修饰类必须用双类把特异性提到基座基础块之上", smSelector)
	}
	if regexp.MustCompile(`(?m)^[ \t]*\.form-select--sm[ \t]*\{`).MatchString(src) {
		t.Error("ui.css 仍有单类形式的 .form-select--sm：它与基座基础块同权重，只能靠书写顺序")
	}
	if !cssMoreSpecific(smSelector, ".form-select") {
		t.Errorf("%s 特异性不高于 .form-select（%v vs %v）", smSelector, cssSpecificity(smSelector), cssSpecificity(".form-select"))
	}

	modifierProps := cssPropNames(body)
	stateProps := map[string]bool{}
	for _, cls := range baseSharedClasses {
		for _, st := range baseStateSuffixes {
			if b, ok := cssRuleBodyFor(src, cls+st); ok {
				for p := range cssPropNames(b) {
					stateProps[p] = true
				}
			}
		}
	}
	for p := range modifierProps {
		if stateProps[p] {
			t.Errorf("尺寸修饰类与状态规则都声明了 %q：两者同为 (0,2,0)，属性重叠后胜负又只能靠书写顺序", p)
		}
	}
}

type chromeHit struct{ key, detail string }

var containerChromeExempt = map[string]string{}

func TestNoContainerSuppliedControlChrome(t *testing.T) {
	chromeProps := []string{
		"border", "border-top", "border-right", "border-bottom", "border-left",
		"border-color", "border-width", "border-style", "border-radius", "border-image",
		"background", "background-color", "background-image", "box-shadow",
		"padding", "padding-top", "padding-right", "padding-bottom", "padding-left",
		"padding-block", "padding-inline", "padding-inline-start", "padding-inline-end",
		"height", "min-height", "font-size", "font-family", "color",
	}

	baseClasses := []string{".form-input", ".form-select", ".form-textarea", ".wbs-native", ".wbs-trigger"}
	elementRe := regexp.MustCompile(`(^|[\s,>+~])(input|select|textarea)($|[\s,.:\[>+~])`)

	exemptRe := regexp.MustCompile(`\[type=["']?(checkbox|radio|color|range|file|submit|button|image|reset)["']?\]`)
	layoutOnly := map[string]bool{"width": true, "min-width": true, "max-width": true}

	var violations []chromeHit
	for _, f := range []string{"static/css/ui.css", "static/css/theme.css", "static/css/media-lib.css"} {
		src := uiCssStripComments(readUIOwnershipFile(t, filepath.FromSlash(f)))
		for _, rule := range cssScanRules(src) {
			props := cssPropNames(rule.body)
			var chrome []string
			for _, p := range chromeProps {
				if _, hit := props[p]; hit {
					chrome = append(chrome, p)
				}
			}
			if len(chrome) == 0 {
				continue
			}
			onlyLayout := true
			for p := range props {
				if !layoutOnly[p] {
					onlyLayout = false
					break
				}
			}
			if onlyLayout || isInheritanceReset(rule.body, chrome) || isCheckControlHost(rule.body) {
				continue
			}
			for _, sel := range rule.selectors {
				hasBase := false
				for _, c := range baseClasses {
					if strings.Contains(sel, c) {
						hasBase = true
						break
					}
				}
				if hasBase || !elementRe.MatchString(sel) || exemptRe.MatchString(sel) {
					continue
				}
				violations = append(violations, chromeHit{f + " :: " + sel, f + " :: " + sel + " -> " + strings.Join(chrome, ",")})
			}
		}
	}
	var unexpected []string
	exemptUsed := map[string]bool{}
	for _, v := range violations {
		if _, ok := containerChromeExempt[v.key]; ok {
			exemptUsed[v.key] = true
			continue
		}
		unexpected = append(unexpected, v.detail)
	}
	if len(unexpected) > 0 {
		t.Errorf("有 %d 条选择器在给原生控件提供外观、却没有基座类（容器兜底复活）：\n  %s\n"+
			"控件外观的唯一真源是共享外观组；修法是把基座类写到控件上、删掉容器里的外观声明",
			len(unexpected), strings.Join(unexpected, "\n  "))
	}

	for k, reason := range containerChromeExempt {
		if !exemptUsed[k] {
			t.Errorf("豁免条目 %q 已不再命中（理由：%s）：对应的容器兜底应该已经修掉，请删掉这条豁免", k, reason)
		}
	}
}

func isCheckControlHost(block string) bool {
	_, ok := cssPropNames(block)["accent-color"]
	return ok
}

func isInheritanceReset(block string, chrome []string) bool {
	if len(chrome) == 0 {
		return false
	}
	for _, p := range chrome {
		re := regexp.MustCompile(`(?m)(?:^|[;{])[ \t]*` + regexp.QuoteMeta(p) + `[ \t]*:[ \t]*([^;}]*)`)
		m := re.FindStringSubmatch(block)
		if m == nil || strings.TrimSpace(m[1]) != "inherit" {
			return false
		}
	}
	return true
}

type cssRule struct {
	selectors []string
	body      string
}

func TestPrimaryFallbacksUseLightThemeValues(t *testing.T) {
	src := uiCssStripComments(readUIOwnershipFile(t, "static/css/ui.css"))
	re := regexp.MustCompile(`var\(--sky-c-primary(?:-bg)?,\s*([^)]+)\)`)
	hits := re.FindAllStringSubmatch(src, -1)
	if len(hits) < 5 {
		t.Fatalf("只扫到 %d 处 --sky-c-primary 兜底（预期 ≥5）：口径可能已失效", len(hits))
	}
	for _, m := range hits {
		switch v := strings.TrimSpace(m[1]); v {
		case "#3d444f", "#eceef1":

		default:
			t.Errorf("--sky-c-primary 系列兜底取了 %q：必须是浅色主题真值（#3d444f / #eceef1）——"+
				"它在缺 theme.css 的前台产物里直接生效，写深色值会让焦点环在产物里偏浅", v)
		}
	}
}

func cssRuleBodyFor(src, sel string) (string, bool) {
	lines := strings.Split(src, "\n")
	head := regexp.MustCompile(`^[ \t]*` + regexp.QuoteMeta(sel) + `[ \t]*(,|\{)`)
	for i, l := range lines {
		m := head.FindStringSubmatch(l)
		if m == nil {
			continue
		}
		start := i
		if m[1] == "," {
			for j := i; j < len(lines); j++ {
				if strings.Contains(lines[j], "{") {
					start = j
					break
				}
			}
		}
		open := strings.Index(lines[start], "{")
		if open < 0 {
			continue
		}
		if c := strings.Index(lines[start][open+1:], "}"); c >= 0 {
			return lines[start][open+1 : open+1+c], true
		}
		body := []string{lines[start][open+1:]}
		for j := start + 1; j < len(lines); j++ {
			if c := strings.Index(lines[j], "}"); c >= 0 {
				body = append(body, lines[j][:c])
				return strings.Join(body, "\n"), true
			}
			body = append(body, lines[j])
		}
		return strings.Join(body, "\n"), true
	}
	return "", false
}

func cssScanRules(src string) []cssRule {
	lines := strings.Split(src, "\n")
	var rules []cssRule
	var pending []string
	for i := 0; i < len(lines); i++ {
		l := strings.TrimSpace(lines[i])
		switch {
		case l == "" || strings.HasPrefix(l, "@") || strings.HasPrefix(l, "}"):
			if strings.HasPrefix(l, "}") {
				pending = nil
			}
			continue
		case !strings.Contains(l, "{"):
			pending = append(pending, l)
			continue
		}
		open := strings.Index(l, "{")
		if h := strings.TrimSpace(l[:open]); h != "" {
			pending = append(pending, h)
		}
		if len(pending) == 0 {
			continue
		}
		var body []string
		rest := l[open+1:]
		for {
			if c := strings.Index(rest, "}"); c >= 0 {
				body = append(body, rest[:c])
				break
			}
			body = append(body, rest)
			i++
			if i >= len(lines) {
				break
			}
			rest = lines[i]
		}
		var sels []string
		for _, s := range pending {
			for _, one := range strings.Split(s, ",") {
				if one = strings.Join(strings.Fields(one), " "); one != "" {
					sels = append(sels, one)
				}
			}
		}
		rules = append(rules, cssRule{selectors: sels, body: strings.Join(body, "\n")})
		pending = nil
	}
	return rules
}

func TestSiblingRulesWithEqualSpecificityMustNotOverlap(t *testing.T) {
	src := uiCssStripComments(readUIOwnershipFile(t, "static/css/ui.css"))
	const wantHover = ".data-table tbody tr:not(.is-selected):hover td.col-actions"
	if !strings.Contains(src, wantHover) {
		t.Errorf("ui.css 缺少互斥的悬停规则 %q：不带 :not(.is-selected) 时它与选中规则同权重，"+
			"命中集合重叠，胜负由书写顺序决定（实测顺序一改即变悬停色）", wantHover)
	}
	if strings.Contains(src, ".data-table tbody tr:hover td.col-actions") {
		t.Error("ui.css 仍有未排除选中态的悬停规则 .data-table tbody tr:hover td.col-actions：" +
			"它与选中规则命中集合重叠、特异性相同")
	}
	if !strings.Contains(src, ".data-table tbody tr.is-selected td.col-actions") {
		t.Error("ui.css 缺少选中态的 sticky 操作列规则：sticky 列有独立不透明背景，删掉它选中行会退回基色")
	}
}
