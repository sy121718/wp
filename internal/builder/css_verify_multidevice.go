package builder

// css_verify_multidevice.go — 产物级「多端硬规则」守卫（审计 UI-015）。
//
// css_verify.go 原本只做一件事：产物里引用的 sky-* 动画必须有对应 @keyframes。
// 本文件把同一层校验扩成**多端硬规则守卫**，规则来自 docs/02-C0-component-base-spec.md 6.9 节
// 与 AGENTS.md「测试」一节的多端适配硬规则：
//
//  1. bare-hover                     裸 :hover —— 依赖悬停的规则没有包在 @media (hover: hover) 里。
//                                    触屏上它照样匹配，点一下就把悬停态粘住（sticky hover）；
//                                    而依赖悬停展开的形态在触屏上等于不存在。
//  2. fixed-px-width                 写死 px 宽度 —— width / min-width / flex-basis 的值就是
//                                    一个绝对值 px，没有 min() / clamp() 按容器收口，
//                                    窄视口下会把文档撑宽（cardstack 第一版的 620px 卡片就是这样）。
//  3. hover-without-touch-fallback   有 hover 桶却没有触屏侧输出 —— 触屏上既没有悬停等价形态
//                                    （@hovernone）也没有按压反馈（@active），该组件在手机上零反馈。
//  4. clamp-lower-than-viewport      clamp() 的下界不小于最窄验收视口（375px）—— clamp 取下界，
//                                    窄屏上必然横向溢出（工作台的 clamp(800px, 86vw, 1280px)）。
//
// 为什么是文本分析而不是完整 CSS 解析器：产物 CSS 由 CSSBuckets 拼装，格式固定（每条规则
// 「选择器 { 换行 声明 换行 }」、桶规则外层再包一层 @media）；组件样式源也是受控语法
// （见 core/component_css.go）。这里实现的是**带块栈的轻量解析**：掩码注释与字符串后按花括号
// 切块，既能拿到选择器与声明，也不必引入 CSS 解析依赖 —— 输入是我们自己生成的，依赖换来的
// 容错在这里用不上。
//
// 误报控制（每一条都写进对应的判定函数里）：
//   · 注释与字符串先被掩码：注释里提到 :hover 不会当规则报（组件样式源里有大量这类说明）。
//   · min() / clamp() / calc() / var() / 百分比一律不算「写死宽度」—— 判定的是
//     「值本身就是一个绝对 px 长度」这个精确形态，而不是「值里出现了 px」。
//   · max-width / max-inline-size 不在检查属性里：上限不会撑破窄屏。
//   · @keyframes 子树跳过：帧里的百分比尺寸不是布局宽度来源，判定它们只有噪音。

import (
	"fmt"
	"log"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
)

// 规则 id（稳定标识：豁免清单、CI 输出、测试断言都用它）。
const (
	ruleBareHover       = "bare-hover"
	ruleFixedWidth      = "fixed-px-width"
	ruleHoverNoFallback = "hover-without-touch-fallback"
	ruleClampLower      = "clamp-lower-than-viewport"
)

// 守卫模式。
const (
	CSSGuardModeOff   = "off"
	CSSGuardModeWarn  = "warn"
	CSSGuardModeError = "error"

	// CSSGuardModeEnv 守卫模式的环境变量名。
	//
	// 为什么走环境变量而不是 CompileOption：调用点在 builder.go 的 Compile 里（本批不改该
	// 文件，见任务边界），环境变量是唯一「零改调用点」就能同时覆盖构建期产物与 CI 脚本两个
	// 入口的办法。取值：off / warn（默认）/ error；未识别的取值按 warn ——
	// 拼错了不该让守卫静默消失，也不该让它变成硬失败。
	CSSGuardModeEnv = "SKY_CSS_GUARD"
)

// 阈值及其依据。
const (
	// cssGuardFixedWidthPx 写死 px 宽度阈值：只报严格大于它的绝对值宽度。
	//
	// 64px 的依据：项目现有图标档位是 16/20/24/32/40/48px，全部落在阈值内 —— 图标尺寸是
	// 设计常量，不看视口，报它们只会制造噪音并逼出无意义的豁免。阈值之上才是「占视口可观的
	// 一块、窄屏会被撑破」的档位（审计 UI-017 点名的 72px / 96px 都在其上）。
	cssGuardFixedWidthPx = 64

	// cssGuardNarrowestViewportPx 最窄验收视口（AGENTS.md 测试节：手机 375px）。
	//
	// clamp() 的下界一旦不小于视口宽，375 下取到的就是下界本身 —— 元素已占满整屏，
	// 再算上容器内距 / 边框 / 滚动条必然溢出。
	cssGuardNarrowestViewportPx = 375
)

// CSSViolation 一条多端硬规则违规。
type CSSViolation struct {
	Rule     string // 规则 id
	Scope    string // 归属：组件名 / compiled-page / admin-static
	File     string // 源文件（相对仓库根）；产物级为空
	Line     int    // 行号（1-based）；0 表示未知
	Selector string // 出问题的选择器（或声明所在规则的选择器）
	Detail   string // 具体值 / 说明
}

// String 单行描述，CI 与日志共用同一份格式。
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

// cssDecl 一条声明。
type cssDecl struct {
	prop  string
	value string
	line  int
}

// cssRuleBlock 一个块：声明块（decls）或嵌套块（children）。
type cssRuleBlock struct {
	prelude  string
	line     int
	decls    []cssDecl
	children []cssRuleBlock
}

// 块前导文本的分类结果。
const (
	kindSelector     = "selector"       // 普通选择器
	kindHoverBucket  = "hover-bucket"   // @media (hover: hover) 或源里的 @hover 前缀
	kindNoHoverMedia = "no-hover-media" // @media (hover: none) 或源里的 @hovernone 前缀
	kindActiveBucket = "active-bucket"  // 源里的 @active 前缀（不包媒体查询）
	kindAtRule       = "at-rule"        // @layer / @container / @supports / @keyframes / 断点 @media
)

// 字节常量：用数字字面量避免在判定里写引号（掩码逻辑要认「双引号 / 单引号 / 反斜杠」三个字符）。
const (
	byteDoubleQuote = 0x22
	byteSingleQuote = 0x27
	byteBackslash   = 0x5c
)

var (
	// 正则刻意只用字符类、不用简写转义：扫的是我们自己的产物，不需要容忍千奇百怪的写法。
	reHoverMediaCond = regexp.MustCompile("[(][ ]*(any-)?hover[ ]*:[ ]*hover[ ]*[)]")
	reNoneMediaCond  = regexp.MustCompile("[(][ ]*(any-)?hover[ ]*:[ ]*none[ ]*[)]")
	rePurePx         = regexp.MustCompile("^([0-9]+([.][0-9]+)?)px$")
	rePropName       = regexp.MustCompile("^[a-z-]+")
	// reSourceCondLine 源 CSS 里独占一行的编译期条件指令（@if / @endif / @each / @endfor）。
	// 它们不是 CSS：留着会被当成选择器文本，必须按行清掉（保留行号）。
	reSourceCondLine = regexp.MustCompile("^@(if|ifnot|each|endif|endfor)( |$)")
)

// cssGuardWidthProps 检查写死宽度的属性。
//
// 刻意不含 max-width / max-inline-size：上限只限制变宽，不会把窄视口撑破。
// 也不含 height 类：纵向溢出是滚动，不是多端适配的硬规则（规范只管宽度与绝对值上限）。
var cssGuardWidthProps = map[string]bool{
	"width": true, "min-width": true, "flex-basis": true,
	"inline-size": true, "min-inline-size": true,
}

// ===== CSS 文本预处理：掩码注释与字符串 =====

// maskCSSNonCode 把注释与字符串的内容替换成空格（保留换行）。
//
// 必须做：组件样式源里大量注释在解释「:hover 在触屏上永不触发」，直接扫文本会把说明当成
// 违规报出来；行号必须保持不变，否则报错定位会漂。
func maskCSSNonCode(src string) string {
	out := []byte(src)
	i := 0
	for i < len(src) {
		switch {
		case src[i] == 0x2f && i+1 < len(src) && src[i+1] == 0x2a: // 注释起点
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

// stripSourceDirectives 清空源 CSS 里独占一行的编译期条件指令（保留行号）。
//
// 只清 @if / @endif / @each / @endfor 这类「无花括号、无分号」的行：留着它们会粘进相邻选择器
// （变成 "@if hoverState & .x:hover"），把桶前缀判定搅乱。分支内容一律保留 ——
// 条件在构建期才定，静态扫描只能按「潜在形态」看待。
func stripSourceDirectives(src string) string {
	lines := strings.Split(src, "\n")
	for i, raw := range lines {
		if reSourceCondLine.MatchString(strings.TrimSpace(raw)) {
			lines[i] = ""
		}
	}
	return strings.Join(lines, "\n")
}

// ===== 轻量块解析 =====

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

// lineAt 偏移 → 行号（1-based）。
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

// parseRange 解析 [start,end) 内的块列表。
func (s *cssScanner) parseRange(start, end int) []cssRuleBlock {
	var out []cssRuleBlock
	i := start
	preludeStart := start
	for i < end {
		switch s.src[i] {
		case 0x3b, 0x7d: // ; }
			i++
			preludeStart = i
		case 0x7b: // {
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

// parseDecls 解析 [start,end) 内的声明列表。
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
		if s.src[i] == 0x3b { // ;
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

// ===== 前导文本分类 =====

// classifyPrelude 把块前导文本归到「选择器 / 桶 / at-rule」，并返回去掉桶前缀后的选择器。
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

// flattenSelector 压掉选择器里的换行与多余空白（多行前导文本要能一行报出来）。
func flattenSelector(sel string) string {
	return strings.Join(strings.Fields(sel), " ")
}

// ===== 规则实现 =====

// walkHover 规则 1：裸 :hover。
//
// 判定依据：一条含 :hover 的选择器规则，若它的祖先块里没有任何 hover 分桶
// （@media (hover: hover) / @media (hover: none)，或源里的 @hover / @hovernone 前缀），
// 就是「所有设备都生效的悬停规则」—— 触屏上点一下会粘住，而且规范要求这类规则必须分桶。
// 不会误伤的地方：AddHover 产出的规则天然带 @media (hover: hover) 外层；手写的复合条件
// （@media (hover: hover) and (pointer: fine)）按括号内条件判定，同样算已分桶；
// @media (hover: none) 里的 :hover 虽然自相矛盾，但它是「已分桶」的写法，本规则只守
// 「有没有分桶」这一件事，不额外发明审计没要求的规则。
func (s *cssScanner) walkHover(blocks []cssRuleBlock, inBucket bool, out *[]CSSViolation) {
	for _, blk := range blocks {
		kind, sel := classifyPrelude(blk.prelude)
		switch kind {
		case kindAtRule:
			// @layer / @container / @supports / 断点 @media：继承桶状态继续下钻。
			s.walkHover(blk.children, inBucket, out)
			continue
		case kindHoverBucket, kindNoHoverMedia, kindActiveBucket:
			// 桶规则本身的分桶已成立；前缀规则自带的 :hover（源里的 @hover &:hover）不报。
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
		// CSS 原生嵌套：外层是普通规则时，内层选择器要继承同一份桶状态。
		if len(blk.children) > 0 {
			s.walkHover(blk.children, inBucket, out)
		}
	}
}

// walkDecls 规则 2 / 4：写死 px 宽度、clamp 下界超视口。
//
// 跳过 @keyframes 子树：帧里的尺寸是动画中间值，不是布局宽度来源。
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
				continue // 值由 Props 在构建期填充，静态扫描判定不了（空值会让整条声明消失）
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

// purePxValue 判定「值本身就是一个绝对 px 长度」，返回其数值。
//
// 只有这个形态才算写死宽度：min(100%, 320px) / clamp(...) / calc(...) / var(--x) / 60% 都不算 ——
// 它们要么已经按容器收口，要么要运行时才知道。判据是形态而不是「值里有没有 px」，
// 所以 min(100%, 320px) 里的 320px 不会被单独拎出来报（那正是规范要求的写法）。
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

// clampLowerBounds 取声明值里所有 clamp() 调用的下界（只认纯 px 的下界）。
func clampLowerBounds(value string) []float64 {
	var out []float64
	lower := value
	for {
		idx := strings.Index(lower, "clamp(")
		if idx < 0 {
			return out
		}
		rest := lower[idx+len("clamp("):]
		// 取第一个顶层逗号之前的文本作为下界实参（括号内的逗号不算）。
		depth := 0
		end := len(rest)
		for i := 0; i < len(rest); i++ {
			switch rest[i] {
			case 0x28: // (
				depth++
			case 0x29: // )
				if depth == 0 {
					end = i
				} else {
					depth--
				}
			case 0x2c: // ,
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

// ===== 分析入口 =====

// analyzeCSS 对一段 CSS 文本跑四条多端硬规则。
//
// srcMode=true 表示输入是组件样式源（含 @hover / @hovernone / @active 前缀指令与 {{变量}}），
// false 表示输入是编译产物 CSS。两种输入共用同一份判定逻辑 —— 否则「源里合规、产物里违规」
// 这类分叉就没人守着，而那正是产物级校验存在的理由。
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

// dedupViolations 去重（同一位置同一规则只留一条）并排序，保证输出稳定。
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
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].File != out[j].File {
			return out[i].File < out[j].File
		}
		if out[i].Line != out[j].Line {
			return out[i].Line < out[j].Line
		}
		if out[i].Rule != out[j].Rule {
			return out[i].Rule < out[j].Rule
		}
		return out[i].Selector < out[j].Selector
	})
	return out
}

// CSSGuardMode 当前守卫模式（环境变量驱动，默认 warn）。
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

// cssGuardLogged 已输出过的告警指纹：同一批违规只打印一次。
//
// 产物编译会被调用很多次（一页一次、批量发布 N 次），逐次打印会把日志淹掉；而「只打印一次就
// 再也不提」同样不对 —— 指纹按内容算，违规内容变了会重新打。
var cssGuardLogged sync.Map

// guardProductCSS 产物级守卫：在 Compile 拼完 CSS 之后对整份产物跑规则。
//
// 模式语义（默认 warn）：
//
//	· off   —— 不检查（本地极端情况下的逃生口）；
//	· warn  —— 记录并打印一次告警，**不**让构建失败（本批口径：先摸清全量违规，整改是后续批次）；
//	· error —— 未豁免的违规直接让构建失败。
//
// 为什么默认 warn：审计 UI-015 的整改步骤明确要求「先以 warn 形式跑一遍拿到全量违规清单，
// 整改完成后改为 error」。默认 error 会让这一批的产物直接构建失败。
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

// logCSSGuardWarnings 以 warn 模式输出一次违规摘要（内容指纹去重）。
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

// summarizeViolations 前 n 条违规拼成一行（错误信息用）。
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
