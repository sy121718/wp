package builder

// css_verify_scan.go — 多端硬规则守卫的**源文件扫描**（CI 与本地清单入口）。
//
// guardProductCSS 守的是「编译后的产物」（含插件样式、主题样式这些非组件来源），但产物里
// 只有选择器、没有文件与行号 —— 拿它当整改清单，得先反查是哪个组件写的。所以这里再扫一遍
// **源文件**：组件样式源（internal/builder/components/<组件>/*.css）、组件里的内嵌 CSS
// （*.go 字符串，如购物车片段的数量输入框宽度），以及后台静态样式
// （internal/templates/static/css/*.css，工作台的弹窗宽度就在那里）。
//
// 两条路径共用同一份判定逻辑（analyzeCSS），区别只在输入：
//   · 产物：@media (hover: hover) 已是真实媒体查询；
//   · 源：@hover / @hovernone / @active 是编译期桶前缀，按同样语义识别（srcMode）。
//
// 源扫描是「潜在形态」扫描：@if 条件分支在构建期才定，这里一律按存在处理；
// 含 {{变量}} 的声明跳过（值待构建期填充，静态判定不了）。

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// 扫描根目录（相对仓库根）。
const (
	CSSGuardComponentsDir  = "internal/builder/components"
	CSSGuardAdminStaticDir = "internal/templates/static/css"
)

// adminStaticScope 后台静态样式的归属标识（报告分区用它区分「产物路径」与「后台页面」）。
const adminStaticScope = "admin-static"

// CSSGuardReport 一次扫描的完整结果。
type CSSGuardReport struct {
	Mode       string
	Root       string
	Components int
	Files      int
	Violations []CSSViolation // 未豁免的违规（本轮应整改的对象）
	Exempted   []CSSViolation // 被显式豁免的违规（保留展示，便于复核理由）
	Notes      []string       // 次级观察：不构成违规，但整改时值得一起看
}

// HasViolations 是否有未豁免的违规。
func (r CSSGuardReport) HasViolations() bool { return len(r.Violations) > 0 }

var (
	reHoverDirective = regexp.MustCompile("@hover[[:space:]]")
	reActiveDirectiv = regexp.MustCompile("@active[[:space:]]")
	reGoSelector     = regexp.MustCompile("[.]([a-zA-Z0-9_-]+)[ ]*[{]")
	// 先抓「完整属性名 + 值」再按属性集合过滤：如果正则里直接写 width|min-width，那么
	// max-width: 640px 会被当成「width: 640px」命中 —— 一处真实误报（image 的 sizes 属性）。
	reGoDecl       = regexp.MustCompile("([a-zA-Z-]+)[ ]*:[ ]*([0-9]+([.][0-9]+)?)px")
	reGoClampLower = regexp.MustCompile("clamp[(][ ]*([0-9]+([.][0-9]+)?)px")
)

// ScanMultiDeviceCSS 扫描仓库根 root 下的全部样式来源，返回违规清单。
func ScanMultiDeviceCSS(root string) (CSSGuardReport, error) {
	report := CSSGuardReport{Mode: CSSGuardMode(), Root: root}
	compRoot := filepath.Join(root, filepath.FromSlash(CSSGuardComponentsDir))
	entries, err := os.ReadDir(compRoot)
	if err != nil {
		return report, fmt.Errorf("读取组件样式目录失败：%w", err)
	}
	var all []CSSViolation
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		report.Components++
		v, notes, files, err := scanComponentDir(root, filepath.Join(compRoot, e.Name()), e.Name())
		if err != nil {
			return report, err
		}
		report.Files += files
		all = append(all, v...)
		report.Notes = append(report.Notes, notes...)
	}
	// 后台静态样式：同一套多端规则对后台页面同样成立（工作台的 clamp(800px, 86vw, 1280px)
	// 是审计 UI-007 的实证）。不走组件源语法，按产物式 CSS 扫描。
	adminRoot := filepath.Join(root, filepath.FromSlash(CSSGuardAdminStaticDir))
	if files, err := os.ReadDir(adminRoot); err == nil {
		for _, f := range files {
			if f.IsDir() || !strings.HasSuffix(f.Name(), ".css") {
				continue
			}
			path := filepath.Join(adminRoot, f.Name())
			content, rerr := os.ReadFile(path)
			if rerr != nil {
				continue
			}
			report.Files++
			all = append(all, analyzeCSS(string(content), adminStaticScope, relSlash(root, path), false)...)
		}
	}
	kept, exempted := applyCSSGuardExemptions(dedupViolations(all))
	report.Violations = kept
	report.Exempted = exempted
	sort.Strings(report.Notes)
	return report, nil
}

// scanComponentDir 扫描一个组件目录：样式源 + 内嵌 CSS 的 Go 源 + 触屏等价形态信号。
func scanComponentDir(root, dir, comp string) (violations []CSSViolation, notes []string, files int, err error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, nil, 0, err
	}
	hasHover, hasHoverNone, hasActive := false, false, false
	firstHoverFile, firstHoverLine, firstHoverSel := "", 0, ""
	markHover := func(file string, line int, sel string) {
		hasHover = true
		if firstHoverFile == "" {
			firstHoverFile, firstHoverLine, firstHoverSel = file, line, sel
		}
	}
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() {
			continue
		}
		path := filepath.Join(dir, name)
		switch {
		case strings.HasSuffix(name, ".css"):
			content, rerr := os.ReadFile(path)
			if rerr != nil {
				continue
			}
			files++
			rel := relSlash(root, path)
			violations = append(violations, analyzeCSS(string(content), comp, rel, true)...)
			masked := maskCSSNonCode(string(content))
			if hasHoverNone != true && strings.Contains(masked, "@hovernone") {
				hasHoverNone = true
			}
			if !hasActive && reActiveDirectiv.MatchString(masked) {
				hasActive = true
			}
			if reHoverDirective.MatchString(masked) {
				markHover(rel, firstHoverDirectiveLine(string(content)), firstHoverDirectiveSelector(string(content)))
			}
		case strings.HasSuffix(name, ".go") && !strings.HasSuffix(name, "_test.go"):
			content, rerr := os.ReadFile(path)
			if rerr != nil {
				continue
			}
			files++
			rel := relSlash(root, path)
			v, hv, hn, ha, hfile, hline, hsel := scanGoCSSSource(string(content), comp, rel)
			violations = append(violations, v...)
			if hn {
				hasHoverNone = true
			}
			if ha {
				hasActive = true
			}
			if hv {
				markHover(hfile, hline, hsel)
			}
		}
	}
	if hasHover && !hasHoverNone && !hasActive {
		violations = append(violations, CSSViolation{
			Rule: ruleHoverNoFallback, Scope: comp, File: firstHoverFile, Line: firstHoverLine,
			Selector: firstHoverSel,
			Detail: "该组件只有 hover 桶，既没有触屏等价形态（@hovernone / AddHoverNone）也没有按压反馈" +
				"（@active / AddActive）—— 触屏上这个组件的交互反馈为零",
		})
	} else if hasHover && !hasHoverNone {
		notes = append(notes, fmt.Sprintf(
			"%s：有 hover 桶 + 按压反馈但无触屏等价形态（@hovernone）—— 触屏仍有按压反馈，不计违规；"+
				"若该 hover 承载信息或功能形态，整改时补常驻形态", comp))
	}
	return violations, notes, files, nil
}

// firstHoverDirectiveLine 源 CSS 里第一条 @hover 指令所在的行号（1-based）。
func firstHoverDirectiveLine(src string) int {
	for i, line := range strings.Split(src, "\n") {
		if reHoverDirective.MatchString(line) {
			return i + 1
		}
	}
	return 0
}

// firstHoverDirectiveSelector 源 CSS 里第一条 @hover 指令后面的选择器文本。
func firstHoverDirectiveSelector(src string) string {
	masked := maskCSSNonCode(src)
	idx := reHoverDirective.FindStringIndex(masked)
	if idx == nil {
		return ""
	}
	rest := masked[idx[1]:]
	if end := strings.Index(rest, "{"); end >= 0 {
		rest = rest[:end]
	}
	if end := strings.Index(rest, "\n"); end >= 0 {
		rest = rest[:end]
	}
	return flattenSelector(rest)
}

// scanGoCSSSource 扫描 Go 源里的内嵌 CSS（片段样式、组件在 Go 里拼的声明）。
//
// 判定：
//
//	· 写死宽度 / clamp 下界 —— 与 CSS 源同一条判据（声明形态匹配）；
//	· 裸 :hover —— Go 里拼 :hover 的唯一合规路径是 AddHover / AddHoverNone（它们负责包媒体
//	  查询 / 分桶），所以「行里有 :hover 但没有这两个调用」就是裸写。
//
// 返回最后三个值是触屏等价形态信号与首条 hover 规则位置。
func scanGoCSSSource(src, comp, rel string) (violations []CSSViolation, hasHover, hasHoverNone, hasActive bool, hoverFile string, hoverLine int, hoverSel string) {
	hoverFile = rel
	currentSel := ""
	for i, raw := range strings.Split(src, "\n") {
		code := stripGoLineComment(raw)
		if code == "" {
			continue
		}
		if m := reGoSelector.FindStringSubmatch(code); m != nil {
			currentSel = "." + m[1]
		}
		lineNo := i + 1
		if strings.Contains(code, "AddHover(") {
			hasHover = true
			if hoverLine == 0 {
				hoverLine, hoverSel = lineNo, currentSel
			}
		}
		if strings.Contains(code, "AddHoverNone(") {
			hasHoverNone = true
		}
		if strings.Contains(code, "AddActive(") {
			hasActive = true
		}
		if strings.Contains(code, ":hover") && !strings.Contains(code, "AddHover(") && !strings.Contains(code, "AddHoverNone(") {
			violations = append(violations, CSSViolation{
				Rule: ruleBareHover, Scope: comp, File: rel, Line: lineNo, Selector: selectorOfLine(code, currentSel),
				Detail: "Go 源码里直接拼了 :hover —— 悬停规则必须走 CSSBuckets.AddHover / AddHoverNone（它们负责包 @media (hover: hover)）",
			})
		}
		for _, m := range reGoDecl.FindAllStringSubmatch(code, -1) {
			prop := strings.ToLower(m[1])
			if !cssGuardWidthProps[prop] {
				// 完整属性名参与判定：max-width 这类上限不会被 "width" 子串误伤
				// （image/jet.go 的 sizes="(max-width: 640px) 100vw" 就被坑过一次）。
				continue
			}
			if n, err := strconv.ParseFloat(m[2], 64); err == nil && n > cssGuardFixedWidthPx {
				violations = append(violations, CSSViolation{
					Rule: ruleFixedWidth, Scope: comp, File: rel, Line: lineNo, Selector: selectorOfLine(code, currentSel),
					Detail: fmt.Sprintf("%s: %gpx —— 写死绝对宽度，窄视口会撑宽文档；写成 min(100%%, %gpx)", prop, n, n),
				})
			}
		}
		if m := reGoClampLower.FindStringSubmatch(code); m != nil {
			if n, err := strconv.ParseFloat(m[1], 64); err == nil && n >= cssGuardNarrowestViewportPx {
				violations = append(violations, CSSViolation{
					Rule: ruleClampLower, Scope: comp, File: rel, Line: lineNo, Selector: selectorOfLine(code, currentSel),
					Detail: fmt.Sprintf("clamp 下界 %gpx 不小于最窄视口 %dpx，窄屏必然横向溢出", n, cssGuardNarrowestViewportPx),
				})
			}
		}
	}
	return violations, hasHover, hasHoverNone, hasActive, hoverFile, hoverLine, hoverSel
}

// selectorOfLine 取该行内出现的类名（内嵌 CSS 一行一条声明时用它定位）。
// 都取不到时退回行内容本身：只给文件 + 行号虽然能定位，但清单里一眼看不出是哪条规则。
func selectorOfLine(code, fallback string) string {
	if m := reGoSelector.FindStringSubmatch(code); m != nil {
		return "." + m[1]
	}
	if fallback != "" {
		return fallback
	}
	s := strings.TrimSpace(code)
	if len(s) > 72 {
		s = s[:72] + "..."
	}
	return s
}

// stripGoLineComment 去掉行注释；字符串字面量里的 // 不算注释（URL 里就有）。
func stripGoLineComment(line string) string {
	var quote byte
	for i := 0; i < len(line); i++ {
		c := line[i]
		switch {
		case quote != 0:
			if c == byteBackslash {
				i++
			} else if c == quote {
				quote = 0
			}
		case c == byteDoubleQuote || c == byteSingleQuote || c == 0x60:
			quote = c
		case c == 0x2f && i+1 < len(line) && line[i+1] == 0x2f:
			return line[:i]
		}
	}
	return line
}

// relSlash 相对仓库根的斜杠路径（跨平台稳定输出）。
func relSlash(root, path string) string {
	if rel, err := filepath.Rel(root, path); err == nil {
		return filepath.ToSlash(rel)
	}
	return filepath.ToSlash(path)
}

// countRule 统计一组违规里某条规则的命中数。
func countRule(in []CSSViolation, rule string) int {
	n := 0
	for _, v := range in {
		if v.Rule == rule {
			n++
		}
	}
	return n
}

// writeViolationGroup 按来源分组打印一组违规。
func writeViolationGroup(sb *strings.Builder, title string, in []CSSViolation) {
	fmt.Fprintf(sb, "\n%s（%d 条）：\n", title, len(in))
	if len(in) == 0 {
		sb.WriteString("  （无）\n")
		return
	}
	byScope := map[string][]CSSViolation{}
	var scopes []string
	for _, v := range in {
		if _, ok := byScope[v.Scope]; !ok {
			scopes = append(scopes, v.Scope)
		}
		byScope[v.Scope] = append(byScope[v.Scope], v)
	}
	sort.Strings(scopes)
	for _, scope := range scopes {
		fmt.Fprintf(sb, "  %s（%d 条）\n", scope, len(byScope[scope]))
		for _, v := range byScope[scope] {
			fmt.Fprintf(sb, "    [%s] %s:%d | %s | %s\n", v.Rule, v.File, v.Line, v.Selector, v.Detail)
		}
	}
}

// FormatCSSGuardReport 渲染人类可读清单（CI 输出与本地复核共用）。
func FormatCSSGuardReport(r CSSGuardReport) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "多端硬规则守卫（审计 UI-015）—— 模式 %s，根目录 %s\n", r.Mode, r.Root)
	fmt.Fprintf(&sb, "扫描：%d 个组件 / %d 个样式文件；违规 %d 条（其中显式豁免 %d 条）\n",
		r.Components, r.Files, len(r.Violations), len(r.Exempted))

	// 分两区打印：组件源是**产物路径**（构建产物由它们编译而来，多端硬规则的直接对象），
	// 后台静态样式不进产物、但同样受规则约束（工作台的弹窗宽度是审计 UI-007 的实证）。
	// 不分区的话，60 多条后台命中会把几个组件违规彻底淹掉。
	var compViol, adminViol []CSSViolation
	for _, v := range r.Violations {
		if v.Scope == adminStaticScope {
			adminViol = append(adminViol, v)
			continue
		}
		compViol = append(compViol, v)
	}

	rules := []string{ruleBareHover, ruleFixedWidth, ruleHoverNoFallback, ruleClampLower}
	sb.WriteString("\n规则命中（组件源 / 后台静态）：\n")
	for _, rule := range rules {
		fmt.Fprintf(&sb, "  %-32s %d / %d\n", rule, countRule(compViol, rule), countRule(adminViol, rule))
	}
	writeViolationGroup(&sb, "组件源（产物路径，构建期守卫的对象）", compViol)
	writeViolationGroup(&sb, "后台静态样式（不进产物，同样受多端规则约束）", adminViol)
	if len(r.Exempted) > 0 {
		sb.WriteString("\n已豁免（理由见 css_verify_exempt.go）：\n")
		for _, v := range r.Exempted {
			fmt.Fprintf(&sb, "  [%s] %s:%d | %s\n", v.Rule, v.File, v.Line, v.Selector)
		}
	}
	if len(r.Notes) > 0 {
		sb.WriteString("\n次级观察（不计违规）：\n")
		for _, n := range r.Notes {
			fmt.Fprintf(&sb, "  %s\n", n)
		}
	}
	return sb.String()
}
