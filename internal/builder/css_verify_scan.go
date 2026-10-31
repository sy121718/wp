package builder

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

const (
	CSSGuardComponentsDir  = "internal/builder/components"
	CSSGuardAdminStaticDir = "internal/templates/static/css"
)

const adminStaticScope = "admin-static"

type CSSGuardReport struct {
	Mode       string
	Root       string
	Components int
	Files      int
	Violations []CSSViolation
	Exempted   []CSSViolation
	Notes      []string
}

func (r CSSGuardReport) HasViolations() bool { return len(r.Violations) > 0 }

var (
	reHoverDirective = regexp.MustCompile("@hover[[:space:]]")
	reActiveDirectiv = regexp.MustCompile("@active[[:space:]]")
	reGoSelector     = regexp.MustCompile("[.]([a-zA-Z0-9_-]+)[ ]*[{]")

	reGoDecl       = regexp.MustCompile("([a-zA-Z-]+)[ ]*:[ ]*([0-9]+([.][0-9]+)?)px")
	reGoClampLower = regexp.MustCompile("clamp[(][ ]*([0-9]+([.][0-9]+)?)px")
)

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

func firstHoverDirectiveLine(src string) int {
	for i, line := range strings.Split(src, "\n") {
		if reHoverDirective.MatchString(line) {
			return i + 1
		}
	}
	return 0
}

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

func relSlash(root, path string) string {
	if rel, err := filepath.Rel(root, path); err == nil {
		return filepath.ToSlash(rel)
	}
	return filepath.ToSlash(path)
}

func countRule(in []CSSViolation, rule string) int {
	n := 0
	for _, v := range in {
		if v.Rule == rule {
			n++
		}
	}
	return n
}

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

func FormatCSSGuardReport(r CSSGuardReport) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "多端硬规则守卫（审计 UI-015）—— 模式 %s，根目录 %s\n", r.Mode, r.Root)
	fmt.Fprintf(&sb, "扫描：%d 个组件 / %d 个样式文件；违规 %d 条（其中显式豁免 %d 条）\n",
		r.Components, r.Files, len(r.Violations), len(r.Exempted))

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
