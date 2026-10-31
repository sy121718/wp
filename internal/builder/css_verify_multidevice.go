package builder

import (
	"cmp"
	"fmt"
	"log"
	"os"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
)

const (
	ruleBareHover       = "bare-hover"
	ruleFixedWidth      = "fixed-px-width"
	ruleHoverNoFallback = "hover-without-touch-fallback"
	ruleClampLower      = "clamp-lower-than-viewport"
)

const (
	CSSGuardModeOff   = "off"
	CSSGuardModeWarn  = "warn"
	CSSGuardModeError = "error"

	CSSGuardModeEnv = "SKY_CSS_GUARD"
)

const (
	cssGuardFixedWidthPx = 64

	cssGuardNarrowestViewportPx = 375
)

type CSSViolation struct {
	Rule     string
	Scope    string
	File     string
	Line     int
	Selector string
	Detail   string
}

func (v CSSViolation) String() string {
	loc := v.Scope
	if v.File != "" {
		loc = v.File
		if v.Line > 0 {
			loc = fmt.Sprintf("%s:%d", v.File, v.Line)
		}
	}
	sel := v.Selector
	if sel == "" {
		sel = "-"
	}
	return fmt.Sprintf("[%s] %s | %s | %s", v.Rule, loc, sel, v.Detail)
}

type cssDecl struct {
	prop  string
	value string
	line  int
}

type cssRuleBlock struct {
	prelude  string
	line     int
	decls    []cssDecl
	children []cssRuleBlock
}

const (
	kindSelector     = "selector"
	kindHoverBucket  = "hover-bucket"
	kindNoHoverMedia = "no-hover-media"
	kindActiveBucket = "active-bucket"
	kindAtRule       = "at-rule"
)

const (
	byteDoubleQuote = 0x22
	byteSingleQuote = 0x27
	byteBackslash   = 0x5c
)

var (
	reHoverMediaCond = regexp.MustCompile("[(][ ]*(any-)?hover[ ]*:[ ]*hover[ ]*[)]")
	reNoneMediaCond  = regexp.MustCompile("[(][ ]*(any-)?hover[ ]*:[ ]*none[ ]*[)]")
	rePurePx         = regexp.MustCompile("^([0-9]+([.][0-9]+)?)px$")
	rePropName       = regexp.MustCompile("^[a-z-]+")

	reSourceCondLine = regexp.MustCompile("^@(if|ifnot|each|endif|endfor)( |$)")
)

var cssGuardWidthProps = map[string]bool{
	"width": true, "min-width": true, "flex-basis": true,
	"inline-size": true, "min-inline-size": true,
}

func maskCSSNonCode(src string) string {
	out := []byte(src)
	i := 0
	for i < len(src) {
		switch {
		case src[i] == 0x2f && i+1 < len(src) && src[i+1] == 0x2a:
			end := len(src)
			if j := strings.Index(src[i+2:], "*/"); j >= 0 {
				end = i + 2 + j + 2
			}
			for k := i; k < end; k++ {
				if out[k] != 0x0a {
					out[k] = 0x20
				}
			}
			i = end
		case src[i] == byteDoubleQuote || src[i] == byteSingleQuote:
			quote := src[i]
			j := i + 1
			for j < len(src) && src[j] != quote {
				if src[j] == byteBackslash {
					j++
				}
				j++
			}
			end := j + 1
			if end > len(src) {
				end = len(src)
			}
			for k := i; k < end; k++ {
				if out[k] != 0x0a {
					out[k] = 0x20
				}
			}
			i = end
		default:
			i++
		}
	}
	return string(out)
}

func stripSourceDirectives(src string) string {
	lines := strings.Split(src, "\n")
	for i, raw := range lines {
		if reSourceCondLine.MatchString(strings.TrimSpace(raw)) {
			lines[i] = ""
		}
	}
	return strings.Join(lines, "\n")
}

type cssScanner struct {
	src        string
	lineStarts []int
	scope      string
	file       string
	srcMode    bool
}

func newCSSScanner(src, scope, file string, srcMode bool) *cssScanner {
	starts := []int{0}
	for i := 0; i < len(src); i++ {
		if src[i] == 0x0a {
			starts = append(starts, i+1)
		}
	}
	return &cssScanner{src: src, lineStarts: starts, scope: scope, file: file, srcMode: srcMode}
}

func (s *cssScanner) lineAt(off int) int {
	if off < 0 {
		return 0
	}
	lo, hi := 0, len(s.lineStarts)-1
	for lo < hi {
		mid := (lo + hi + 1) / 2
		if s.lineStarts[mid] <= off {
			lo = mid
		} else {
			hi = mid - 1
		}
	}
	return lo + 1
}

func (s *cssScanner) parseRange(start, end int) []cssRuleBlock {
	var out []cssRuleBlock
	i := start
	preludeStart := start
	for i < end {
		switch s.src[i] {
		case 0x3b, 0x7d:
			i++
			preludeStart = i
		case 0x7b:
			prelude := strings.TrimSpace(s.src[preludeStart:i])
			depth := 1
			j := i + 1
			for j < end && depth > 0 {
				switch s.src[j] {
				case 0x7b:
					depth++
				case 0x7d:
					depth--
				}
				j++
			}
			bodyStart, bodyEnd := i+1, j-1
			if bodyEnd > end {
				bodyEnd = end
			}
			blk := cssRuleBlock{prelude: prelude, line: s.lineAt(preludeStart)}
			if bodyEnd > bodyStart {
				if strings.Contains(s.src[bodyStart:bodyEnd], "{") {
					blk.children = s.parseRange(bodyStart, bodyEnd)
				} else {
					blk.decls = s.parseDecls(bodyStart, bodyEnd)
				}
			}
			out = append(out, blk)
			i = j
			preludeStart = i
		default:
			i++
		}
	}
	return out
}

func (s *cssScanner) parseDecls(start, end int) []cssDecl {
	var out []cssDecl
	i := start
	declStart := start
	flush := func(stop int) {
		raw := s.src[declStart:stop]
		if idx := strings.Index(raw, ":"); idx >= 0 {
			prop := strings.ToLower(strings.TrimSpace(raw[:idx]))
			value := strings.TrimSpace(raw[idx+1:])
			if prop != "" && value != "" && rePropName.MatchString(prop) {
				out = append(out, cssDecl{prop: prop, value: value, line: s.lineAt(declStart)})
			}
		}
	}
	for i < end {
		if s.src[i] == 0x3b {
			flush(i)
			i++
			declStart = i
			continue
		}
		i++
	}
	flush(end)
	return out
}

func classifyPrelude(prelude string) (kind string, sel string) {
	t := strings.TrimSpace(prelude)
	switch {
	case strings.HasPrefix(t, "@media"):
		switch {
		case reNoneMediaCond.MatchString(t):
			return kindNoHoverMedia, ""
		case reHoverMediaCond.MatchString(t):
			return kindHoverBucket, ""
		}
		return kindAtRule, ""
	case strings.HasPrefix(t, "@hovernone"):
		return kindNoHoverMedia, strings.TrimSpace(strings.TrimPrefix(t, "@hovernone"))
	case strings.HasPrefix(t, "@hover"):
		return kindHoverBucket, strings.TrimSpace(strings.TrimPrefix(t, "@hover"))
	case strings.HasPrefix(t, "@active"):
		return kindActiveBucket, strings.TrimSpace(strings.TrimPrefix(t, "@active"))
	case strings.HasPrefix(t, "@global"):
		return kindSelector, strings.TrimSpace(strings.TrimPrefix(t, "@global"))
	case strings.HasPrefix(t, "@"):
		return kindAtRule, t
	}
	return kindSelector, t
}

func flattenSelector(sel string) string {
	return strings.Join(strings.Fields(sel), " ")
}

func (s *cssScanner) walkHover(blocks []cssRuleBlock, inBucket bool, out *[]CSSViolation) {
	for _, blk := range blocks {
		kind, sel := classifyPrelude(blk.prelude)
		switch kind {
		case kindAtRule:

			s.walkHover(blk.children, inBucket, out)
			continue
		case kindHoverBucket, kindNoHoverMedia, kindActiveBucket:

			s.walkHover(blk.children, true, out)
			continue
		}
		if strings.Contains(sel, ":hover") && !inBucket {
			*out = append(*out, CSSViolation{
				Rule: ruleBareHover, Scope: s.scope, File: s.file, Line: blk.line,
				Selector: flattenSelector(sel),
				Detail:   "含 :hover 的规则没有包在 @media (hover: hover) 内（触屏上会粘滞；依赖它展开的形态在触屏上等于不存在）",
			})
		}

		if len(blk.children) > 0 {
			s.walkHover(blk.children, inBucket, out)
		}
	}
}

func (s *cssScanner) walkDecls(blocks []cssRuleBlock, out *[]CSSViolation) {
	for _, blk := range blocks {
		if strings.HasPrefix(strings.TrimSpace(blk.prelude), "@keyframes") {
			continue
		}
		_, sel := classifyPrelude(blk.prelude)
		selector := flattenSelector(sel)
		if selector == "" {
			selector = flattenSelector(blk.prelude)
		}
		for _, d := range blk.decls {
			if s.srcMode && strings.Contains(d.value, "{{") {
				continue
			}
			if cssGuardWidthProps[d.prop] {
				if n, ok := purePxValue(d.value); ok && n > cssGuardFixedWidthPx {
					*out = append(*out, CSSViolation{
						Rule: ruleFixedWidth, Scope: s.scope, File: s.file, Line: d.line,
						Selector: selector,
						Detail: fmt.Sprintf("%s: %s —— 写死绝对宽度，窄视口会撑宽文档；写成 min(100%%, %s)",
							d.prop, d.value, d.value),
					})
				}
			}
			for _, lower := range clampLowerBounds(d.value) {
				if lower >= cssGuardNarrowestViewportPx {
					*out = append(*out, CSSViolation{
						Rule: ruleClampLower, Scope: s.scope, File: s.file, Line: d.line,
						Selector: selector,
						Detail: fmt.Sprintf("%s: %s —— clamp 下界 %gpx 不小于最窄视口 %dpx，窄屏必然横向溢出；改用 min(100%%, %s)",
							d.prop, d.value, lower, cssGuardNarrowestViewportPx, d.value),
					})
				}
			}
		}
		if len(blk.children) > 0 {
			s.walkDecls(blk.children, out)
		}
	}
}

func purePxValue(value string) (float64, bool) {
	v := strings.TrimSpace(value)
	v = strings.TrimSpace(strings.TrimSuffix(v, "!important"))
	if m := rePurePx.FindStringSubmatch(strings.ToLower(v)); m != nil {
		if n, err := strconv.ParseFloat(m[1], 64); err == nil {
			return n, true
		}
	}
	return 0, false
}

func clampLowerBounds(value string) []float64 {
	var out []float64
	lower := value
	for {
		idx := strings.Index(lower, "clamp(")
		if idx < 0 {
			return out
		}
		rest := lower[idx+len("clamp("):]

		depth := 0
		end := len(rest)
		for i := 0; i < len(rest); i++ {
			switch rest[i] {
			case 0x28:
				depth++
			case 0x29:
				if depth == 0 {
					end = i
				} else {
					depth--
				}
			case 0x2c:
				if depth == 0 {
					end = i
				}
			}
			if end != len(rest) {
				i = len(rest)
			}
		}
		if end > len(rest) {
			end = len(rest)
		}
		if n, ok := purePxValue(rest[:end]); ok {
			out = append(out, n)
		}
		if end >= len(rest) {
			return out
		}
		lower = rest[end:]
	}
}

func analyzeCSS(css, scope, file string, srcMode bool) []CSSViolation {
	prepared := css
	if srcMode {
		prepared = stripSourceDirectives(css)
	}
	masked := maskCSSNonCode(prepared)
	sc := newCSSScanner(masked, scope, file, srcMode)
	blocks := sc.parseRange(0, len(masked))
	var out []CSSViolation
	sc.walkHover(blocks, false, &out)
	sc.walkDecls(blocks, &out)
	return dedupViolations(out)
}

func dedupViolations(in []CSSViolation) []CSSViolation {
	seen := make(map[string]bool, len(in))
	out := make([]CSSViolation, 0, len(in))
	for _, v := range in {
		key := fmt.Sprintf("%s|%s|%s|%d|%s|%s", v.Rule, v.Scope, v.File, v.Line, v.Selector, v.Detail)
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, v)
	}
	slices.SortStableFunc(out, func(a, b CSSViolation) int {
		return cmp.Or(
			cmp.Compare(a.File, b.File),
			cmp.Compare(a.Line, b.Line),
			cmp.Compare(a.Rule, b.Rule),
			cmp.Compare(a.Selector, b.Selector),
		)
	})
	return out
}

func CSSGuardMode() string {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(CSSGuardModeEnv))) {
	case CSSGuardModeOff, "false", "0":
		return CSSGuardModeOff
	case CSSGuardModeError, "fail", "1":
		return CSSGuardModeError
	default:
		return CSSGuardModeWarn
	}
}

var cssGuardLogged sync.Map

func guardProductCSS(css string) error {
	mode := CSSGuardMode()
	if mode == CSSGuardModeOff {
		return nil
	}
	all := analyzeCSS(css, "compiled-page", "", false)
	kept, _ := applyCSSGuardExemptions(all)
	if len(kept) == 0 {
		return nil
	}
	if mode == CSSGuardModeError {
		return fmt.Errorf("产物违反多端硬规则（%d 条，%s=%s）：%s",
			len(kept), CSSGuardModeEnv, CSSGuardModeError, summarizeViolations(kept, 5))
	}
	logCSSGuardWarnings(kept, mode)
	return nil
}

func logCSSGuardWarnings(kept []CSSViolation, mode string) {
	lines := make([]string, 0, len(kept))
	for _, v := range kept {
		lines = append(lines, v.String())
	}
	sig := strings.Join(lines, "\n")
	if _, loaded := cssGuardLogged.LoadOrStore(sig, struct{}{}); loaded {
		return
	}
	byRule := map[string]int{}
	for _, v := range kept {
		byRule[v.Rule]++
	}
	rules := make([]string, 0, len(byRule))
	for r, n := range byRule {
		rules = append(rules, fmt.Sprintf("%s=%d", r, n))
	}
	sort.Strings(rules)
	log.Printf("[css-guard][%s] 产物违反多端硬规则 %d 条（%s）—— 全量清单：scripts/check-multidevice-css.sh",
		mode, len(kept), strings.Join(rules, " "))
	for i, v := range kept {
		if i >= 10 {
			log.Printf("[css-guard] … 其余 %d 条见 CI 清单", len(kept)-i)
			break
		}
		log.Printf("[css-guard] %s", v.String())
	}
}

func summarizeViolations(in []CSSViolation, n int) string {
	parts := make([]string, 0, n)
	for i, v := range in {
		if i >= n {
			parts = append(parts, fmt.Sprintf("… 其余 %d 条", len(in)-i))
			break
		}
		parts = append(parts, v.String())
	}
	return strings.Join(parts, "；")
}
