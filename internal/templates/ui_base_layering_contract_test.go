package templates

// ui_base_layering_contract_test.go —— 表单控件**层叠契约**的单一真源。
//
// 为什么要有这个文件（用户要求「层叠契约能不能统一在一个地方」）：
// 「谁赢谁、哪个属性归哪一层」此前只存在于注释和读者的脑子里，于是同一类缺陷反复出现 ——
// 靠书写顺序生效的规则一旦重排就静默失效。本项目已经真实踩过三次：
//   · 语言下拉用 background 简写把刚加的原生箭头图整块抹掉；
//   · sticky 操作列的悬停规则与选中规则同权重，靠「选中写在后面」定胜负；
//   · 页面私有类提到 (0,2,0) 后与 .form-select:focus 平级，把聚焦的主色边框静默压掉（实测复现）。
// 这个文件把那套层次写成机器可验收的断言：改动只要破坏层次，这里就红。
//
// 基座的层叠层次（(a,b,c) = id / 类·属性·伪类 / 元素）：
//
//	(0,1,0)  共享外观组   .form-input / .form-select / .form-textarea / .wbs-trigger
//	(0,1,1)  原生专属     select.form-select（箭头：自绘替身有自己的 .wbs-caret）
//	(0,2,0)  尺寸修饰类   .form-select.form-select--sm（双类，赢基座靠特异性）
//	(0,2,0)  状态规则     :focus / :focus-visible / :disabled / [aria-invalid] / ::placeholder
//
// 尺寸修饰类与状态规则同层，但**属性互斥**：前者管尺寸，后者管反馈。
// 这一条是整套层次的关键 —— 同层且属性重叠，胜负就又要靠书写顺序。

import (
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// baseSharedClasses 共享外观组的成员；baseStateSuffixes 状态层后缀。
var (
	baseSharedClasses = []string{".form-input", ".form-select", ".form-textarea", ".wbs-trigger"}
	baseStateSuffixes = []string{":focus-visible", ":focus", ":disabled", `[aria-invalid="true"]`, "::placeholder"}
)

// sizeProps 状态规则**绝不允许**声明的属性。
//
// 防的真实缺陷：状态规则改尺寸 → 聚焦时控件跳动（焦点一进一出，宽度高度变一下），
// 而且会与尺寸修饰类在同一层打架（都 (0,2,0)，胜负又落到书写顺序上）。
var sizeProps = []string{
	"height", "min-height", "max-height",
	"width", "min-width", "max-width",
	"padding", "padding-block", "padding-inline", "padding-top", "padding-right",
	"padding-bottom", "padding-left", "padding-block-start", "padding-block-end",
	"padding-inline-start", "padding-inline-end",
	"font-size", "line-height", "box-sizing", "letter-spacing",
}

// TestBaseAppearanceGroupIsSingleSource 控件外观只有一个真源：五个类在同一条规则里。
//
// 防的真实缺陷：同一份外观被写两遍、值还漂了 —— `.wbs-trigger` 曾与已下线的 `.wbd-trigger`
// 曾经各自写死 `padding: 8px 10px`，而基座走 token 是 `8px 12px`（同一份 UI 里三种内边距）。
// 白名单是**闭集**：往共享组里塞原生 select 专属属性（background-image 等）会被打回，
// 否则自绘触发器也会长出浏览器原生那枚箭头。
func TestBaseAppearanceGroupIsSingleSource(t *testing.T) {
	src := uiCssStripComments(readUIOwnershipFile(t, "static/css/ui.css"))
	re := regexp.MustCompile(`(?m)^\.form-input,\s*\n\.form-select,\s*\n\.form-textarea,\s*\n\.wbs-trigger\s*\{([^}]*)\}`)
	m := re.FindStringSubmatch(src)
	if m == nil {
		t.Fatal("共享外观组不是「.form-input, .form-select, .form-textarea, .wbs-trigger」这一条规则：" +
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
	// 触发器特有的排布仍然只声明在它自己那段里（两个触发器同一口径）。
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

// TestBaseStateRulesNeverDeclareSize 状态规则不得声明尺寸属性。
//
// 防的真实缺陷两条：① 聚焦时控件跳动（焦点进出的尺寸变化，鼠标点击最明显）；
// ② 与尺寸修饰类同层打架 —— 两者都是 (0,2,0)，一旦属性重叠，胜负只能靠书写顺序，
// 又一次变成「重排即静默失效」。
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

// TestSizeModifierWinsBySpecificity 尺寸修饰类必须靠特异性赢基座，且不与状态规则抢属性。
//
// 防的真实缺陷：修饰类写成单类时与基座基础块同为 (0,1,0)，只能靠「写在基类之后」生效 ——
// 任何一次规则重排都会让它静默失效（实测：把它移到基座之前，紧凑尺寸立刻丢失）。
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
	// 与状态规则同层但不得抢属性：状态层管反馈，修饰类管尺寸。
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

// chromeHit 一条候选违规（key 供豁免清单匹配，detail 供报错展示）。
type chromeHit struct{ key, detail string }

// containerChromeExempt 存量违规的**带理由**豁免清单。
//
// 2026-10-01（task-12）：theme.css 那 6 条存量已全部迁移完毕，清单随之清空 ——
// 控件侧本来就带基座类（inventory_sources.html 的 form-textarea、product_attribute_rows.html
// 的 form-input、两个翻译页的 form-textarea），删掉的是容器里重复的外观声明；
// .receipt-form 与 .toolbar-search 两条连使用点都没有（css_class_audit_test.go 登记为疑似死样式），
// 只删外观，整块清理归死样式批次。于是这门禁回到**零豁免**状态。
//
// 两类历史条目（将来若再登记，必须带可核对理由）：① 文件不在写作用域；
// ② 选择器限定在**非文本控件**上（复选框宿主容器没写 type 属性，静态判不出来）。
// 清单条目**不再命中即失败**，所以它不会变成永久豁免区。
var containerChromeExempt = map[string]string{}

// TestNoContainerSuppliedControlChrome 禁止「容器给控件外观」复活。
//
// 防的真实缺陷：审计 UIK-009 的 `.admin-layout :where(input, select, textarea)` 兜底层 ——
// 容器一旦能给外观，「输入框长什么样」就有两个真源（基座类 + 容器），改基座时要重新判断谁赢；
// 而它兜住的 55 个裸控件在迁移完成后一个都不剩，兜底层只剩副作用。
// 判据是**形状**：给 input / select / textarea 声明外观属性的选择器，必须同时包含基座类。
func TestNoContainerSuppliedControlChrome(t *testing.T) {
	chromeProps := []string{
		"border", "border-top", "border-right", "border-bottom", "border-left",
		"border-color", "border-width", "border-style", "border-radius", "border-image",
		"background", "background-color", "background-image", "box-shadow",
		"padding", "padding-top", "padding-right", "padding-bottom", "padding-left",
		"padding-block", "padding-inline", "padding-inline-start", "padding-inline-end",
		"height", "min-height", "font-size", "font-family", "color",
	}
	// 基座类：出现在选择器里即视为「按基座类命名，不是容器兜底」。
	// .wbs-native 是自绘下拉里那枚视觉隐藏的
	// 原生控件 —— 外观由各自的触发器承担，与文本类控件遵守同一条纪律。
	baseClasses := []string{".form-input", ".form-select", ".form-textarea", ".wbs-native", ".wbs-trigger"}
	elementRe := regexp.MustCompile(`(^|[\s,>+~])(input|select|textarea)($|[\s,.:\[>+~])`)
	// 非文本类控件自带宿主样式（复选框 / 单选 / 取色器 / 滑块 / 文件域…）：
	// 基座外观组管的是**文本类**控件（输入框 / 下拉 / 多行文本），按 type 显式豁免。
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
				continue // 纯布局 / 继承复位 / 勾选类控件的宿主样式，都不提供「文本类控件外观」
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
	// 豁免条目必须仍然命中：修完不删条目 = 留下永久豁免区（门禁在这里变红）。
	for k, reason := range containerChromeExempt {
		if !exemptUsed[k] {
			t.Errorf("豁免条目 %q 已不再命中（理由：%s）：对应的容器兜底应该已经修掉，请删掉这条豁免", k, reason)
		}
	}
}

// isCheckControlHost 判断规则是否在给「勾选类控件」（checkbox / radio / range）定尺寸。
//
// 判据是 accent-color：它只对勾选类控件生效，出现即说明这条规则的作用对象不是
// 文本框 / 下拉 / 多行文本（基座外观组的对象）。口径与 admin_form_base_test.go 的
// adminFormSkipTypes 一致 —— 那边同样把 checkbox / radio 排除在「必须带基座类」之外。
// 需要它是因为选择器经常不写 type（`.checkbox input` / `.media-grid-select input`），
// 静态判不出元素类型，只能看规则自己声明了什么。
func isCheckControlHost(block string) bool {
	_, ok := cssPropNames(block)["accent-color"]
	return ok
}

// isInheritanceReset 判断规则体里的外观属性是否**全部只是继承复位**（值都是 inherit）。
//
// 为什么要它：`button, input, select, textarea { font: inherit; color: inherit; }` 这类
// 复位规则把平台默认值让开、交给页面继承，它不提供任何外观 —— 与「容器给控件外观」
// 是相反的意图，不该被这条门禁当成违规。
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

// ───────────────────────── 工具 ─────────────────────────

// cssRule 一条简单规则（选择器已按逗号拆成单项、压掉多余空白）。
type cssRule struct {
	selectors []string
	body      string
}

// TestPrimaryFallbacksUseLightThemeValues `var(--sky-c-primary…, X)` 的兜底值必须是浅色真值。
//
// 防的真实缺陷：ui.css 会被注入到**没有 theme.css** 的前台产物里，那里的 --sky-c-primary
// 直接取兜底值。兜底写成深色主题的主色（这里曾经是 #aeb6c0）会让产物里的焦点环偏浅，
// 而同页的自绘控件（.wbs-trigger / .wbc-swatch…）取的是浅色真值 #3d444f —— 同一份 UI 两套焦点环。
// 与上一批修掉的 5 处 `var(--sky-c-text-secondary, #c2c8cf)` 是同一类缺陷。
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
			// 浅色主题真值：--c-primary / --c-primary-bg
		default:
			t.Errorf("--sky-c-primary 系列兜底取了 %q：必须是浅色主题真值（#3d444f / #eceef1）——"+
				"它在缺 theme.css 的前台产物里直接生效，写深色值会让焦点环在产物里偏浅", v)
		}
	}
}

// cssRuleBodyFor 取出「选择器列表里包含 sel」的那条规则的声明体 —— 基座的状态规则是
// 分组写法（`.form-input:focus,` / `.form-select:focus,` / …），cssRuleBlock 抓不到。
func cssRuleBodyFor(src, sel string) (string, bool) {
	lines := strings.Split(src, "\n")
	head := regexp.MustCompile(`^[ \t]*` + regexp.QuoteMeta(sel) + `[ \t]*(,|\{)`)
	for i, l := range lines {
		m := head.FindStringSubmatch(l)
		if m == nil {
			continue
		}
		start := i
		if m[1] == "," { // 分组选择器：向下找到带 { 的那一行
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

// cssScanRules 顺序扫出「选择器 { 声明 }」规则（@media 容器行跳过，其内部规则照常取出）。
// 只处理本仓库的实际形状：无嵌套规则、声明体以 `}` 收尾。
func cssScanRules(src string) []cssRule {
	lines := strings.Split(src, "\n")
	var rules []cssRule
	var pending []string
	for i := 0; i < len(lines); i++ {
		l := strings.TrimSpace(lines[i])
		switch {
		case l == "" || strings.HasPrefix(l, "@") || strings.HasPrefix(l, "}"):
			if strings.HasPrefix(l, "}") {
				pending = nil // @media 块结束，丢弃悬挂的选择器
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

// TestSiblingRulesWithEqualSpecificityMustNotOverlap 同权重的兄弟规则必须靠选择器互斥，
// 不能靠书写顺序。
//
// 防的真实缺陷：`.data-table tbody tr:hover td.col-actions` 与
// `.data-table tbody tr.is-selected td.col-actions` 特异性完全一致（各 3 类 + 3 元素），
// 胜负只取决于谁写在后面。实测（Chromium，只把选中规则在内存样式表里上移一行、磁盘不动）：
// 选中行悬停的 td.col-actions 立刻从选中色 rgb(228,228,228) 掉回悬停色 rgb(236,236,236)，
// 页面上没有任何报错。给悬停那条加 :not(.is-selected) 后命中集合互斥，搬动顺序不再改变结果。
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

// ───────────────────────── 工具（cssRuleBlock / cssPropNames / cssSpecificity /
// cssMoreSpecific 见 ui_css_ownership_test.go：它们是通用 CSS 文本工具，本文件复用，不另起一份）
