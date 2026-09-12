package core

// component_css.go — 组件样式源（.css）的解析与应用。
//
// 背景：组件样式此前以 Go 字符串数组的形式散在 37 个组件的 compileCSS 里（共 425 处 b.Add），
// 写样式没有补全、没有 lint、不能格式化。本文件让样式可以写成**真正的 CSS 文件**：
//
//     & .sky-form-field { display: flex; flex-direction: column; }
//     & .sky-form-submit:hover { filter: brightness(.95); }
//
// 顶层 `&` 在应用时替换成该 node 的作用域选择器，其余（`:is()` / 属性选择器 / 伪类）原样保留，
// 于是「按 node id 作用域」与「同一组件多实例互不污染」这两条约束继续成立。
//
// 设计取舍：
//   · **只支持现有代码实际用到的语法**，遇到不认识的写法**返回 error** 而不是静默漏掉 ——
//     静默漏掉的样式会在产物里「看起来正常但少了点什么」，比构建失败难查得多；
//   · 桶（desktop / tablet / mobile / hover / active）默认 desktop，与迁移前逐条 b.Add 的行为一致；
//     需要别的桶时用 `@media`（按断点）或 `@hover` / `@active` 分组；
//   · Go 侧的计算声明（FocusRingDecls / FocusTransitionDecl）用**占位指令**表达，
//     如 `@focus-ring;` —— 这样「一份实现」没有变成两份。

import (
	"fmt"
	"strings"
)

// cssApplyError 描述样式源里的问题，带行号便于定位。
func cssApplyError(line int, format string, args ...any) error {
	return fmt.Errorf("组件样式第 %d 行: %s", line, fmt.Sprintf(format, args...))
}

// ApplyComponentCSS 解析组件 CSS 源并写入 b。
//
// scope 是该 node 的作用域选择器（形如 ".sky-node-xxxx"），顶层 `&` 会被替换成它。
// container 用于 @media 断点归桶时判断版式（tablet / mobile）。
func ApplyComponentCSS(b *CSSBuckets, scope, source string) error {
	if b == nil {
		return fmt.Errorf("CSSBuckets 为空")
	}
	if strings.TrimSpace(source) == "" {
		return nil
	}
	parser := &cssSourceParser{src: source, scope: scope, buckets: b}
	return parser.run()
}

// cssSourceParser 极简 CSS 源解析器：只覆盖组件样式的实际用法。
type cssSourceParser struct {
	src string
	// scope 顶层 & 的替换值（该组件实例的作用域选择器）。
	scope string
	// buckets 输出目标。
	buckets *CSSBuckets
	// mediaBP 非空表示当前处于某个 @media 块内，规则进该断点桶。
	mediaBP string
}

// run 扫描源文本，按规则块逐个处理。
func (p *cssSourceParser) run() error {
	lines := strings.Split(p.src, "\n")
	i := 0
	for i < len(lines) {
		raw := lines[i]
		// 注意顺序：多行注释必须先按**原始行**判断。stripCSSComment 会把 `/*` 开头的行清成空串，
		// 于是注释块的第一行看起来像「空行」、第二行 ` *` 看起来像「选择器」—— 报错报在莫名其妙的位置。
		if trimmedRaw := strings.TrimSpace(raw); strings.HasPrefix(trimmedRaw, "/*") && !strings.Contains(trimmedRaw, "*/") {
			for i < len(lines) {
				if strings.Contains(lines[i], "*/") {
					break
				}
				i++
			}
			i++
			continue
		}
		line := strings.TrimSpace(stripCSSComment(raw))
		if line == "" {
			i++
			continue
		}
		// @keyframes：整体取出（含花括号内部），交给 AddKeyframes。
		if strings.HasPrefix(line, "@keyframes ") {
			name := strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(line, "@keyframes "), "{"))
			body, next, err := collectBlock(lines, i, line)
			if err != nil {
				return cssApplyError(i+1, "%v", err)
			}
			p.buckets.AddKeyframes(name, body)
			i = next
			continue
		}
		// @media：块内是普通规则，按其断点归桶。
		if strings.HasPrefix(line, "@media") {
			body, next, err := collectBlock(lines, i, line)
			if err != nil {
				return cssApplyError(i+1, "%v", err)
			}
			bp, err := mediaBreakpointOf(line)
			if err != nil {
				return cssApplyError(i+1, "%v", err)
			}
			inner := &cssSourceParser{src: body, scope: p.scope, buckets: p.buckets, mediaBP: bp}
			if err := inner.run(); err != nil {
				return err
			}
			i = next
			continue
		}
		// 普通规则：选择器 + 声明块。
		if !strings.Contains(line, "{") {
			return cssApplyError(i+1, "看不懂这一行（期望「选择器 {」或 @media/@keyframes）：%q", line)
		}
		selector := strings.TrimSpace(strings.TrimSuffix(line, "{"))
		body, next, err := collectBlock(lines, i, line)
		if err != nil {
			return cssApplyError(i+1, "%v", err)
		}
		decls, err := p.parseDecls(body)
		if err != nil {
			return cssApplyError(i+1, "%v", err)
		}
		// 选择器里的 & 一律替换成作用域前缀；不含 & 的写法拒绝（避免「以为在作用域内其实不是」）。
		if !strings.Contains(selector, "&") {
			return cssApplyError(i+1, "选择器必须以 & 开头（& 会被替换成该组件实例的作用域），got %q", selector)
		}

		// @hover / @active 用标记前缀指定桶（构建期据此决定要不要包 @media (hover: hover)）。
		scoped := replaceAmp(selector, p.scope)
		switch {
		case strings.HasPrefix(selector, "@hover"):
			scoped = replaceAmp(strings.TrimSpace(strings.TrimPrefix(selector, "@hover")), p.scope)
			p.buckets.AddHover(scoped, decls)
		case strings.HasPrefix(selector, "@active"):
			scoped = replaceAmp(strings.TrimSpace(strings.TrimPrefix(selector, "@active")), p.scope)
			p.buckets.AddActive(scoped, decls)
		default:
			bp := p.mediaBP
			if bp == "" {
				bp = BreakpointDesktop
			}
			p.buckets.Add(bp, scoped, decls)
		}
		i = next
	}
	return nil
}

// mediaBreakpointOf 解析 @media 语句，返回对应的断点桶名。
//
// 只认项目已有的两档断点（1024 / 767，见本包 breakpoint 常量）—— 组件样式不该自造断点，
// 否则同一页面上会出现两套互相打架的响应式阈值。
func mediaBreakpointOf(header string) (string, error) {
	idx := strings.Index(header, "max-width")
	if idx < 0 {
		return "", fmt.Errorf("只支持 max-width 断点（桌面优先），got %q", header)
	}
	nums := strings.TrimRight(strings.TrimSpace(header[idx+len("max-width"):]), " {)")
	nums = strings.TrimPrefix(strings.TrimSpace(nums), ":")
	nums = strings.TrimSuffix(strings.TrimSpace(nums), "px")
	var px int
	if _, err := fmt.Sscanf(strings.TrimSpace(nums), "%d", &px); err != nil {
		return "", fmt.Errorf("看不懂断点宽度: %q", header)
	}
	// 精确白名单而不是区间判断：区间判断会把 `max-width: 500px` 这类自造断点静默归到 mobile，
	// 于是同一页面上出现两套互相打架的响应式阈值。宁可构建期报错。
	switch px {
	case 1024:
		return BreakpointTablet, nil
	case 767:
		return BreakpointMobile, nil
	}
	return "", fmt.Errorf("断点 %dpx 不在既有档位（1024 / 767），请复用现有断点", px)
}

// parseDecls 把声明块文本拆成声明列表（`prop: value`），保留顺序以保证确定性。
func (p *cssSourceParser) parseDecls(body string) ([]string, error) {
	var decls []string
	// 声明之间用 ; 分隔，但要注意值里可能有 var(--x, a:b) 这类带冒号的内容 ——
	// 这里只按 ; 切分，不碰冒号，交给浏览器解析。
	for _, part := range strings.Split(body, ";") {
		for _, one := range strings.Split(part, "\n") {
			d := strings.TrimSpace(stripCSSComment(one))
			if d == "" {
				continue
			}
			// 占位指令：Go 侧计算的声明（保持「一份实现」）。
			switch d {
			case "@focus-ring":
				decls = append(decls, FocusRingDecls()...)
				continue
			case "@focus-transition":
				decls = append(decls, FocusTransitionDecl())
				continue
			}
			if !strings.Contains(d, ":") {
				return nil, fmt.Errorf("看不懂这条声明：%q", d)
			}
			decls = append(decls, d)
		}
	}
	return decls, nil
}

// collectBlock 从 start 行开始收集花括号块的内容（不含最外层花括号），返回内容与下一行下标。
func collectBlock(lines []string, start int, firstLine string) (string, int, error) {
	depth := strings.Count(firstLine, "{") - strings.Count(firstLine, "}")
	var buf []string
	// 第一行花括号之后的内容属于块内。
	if idx := strings.Index(firstLine, "{"); idx >= 0 {
		rest := strings.TrimSpace(firstLine[idx+1:])
		if rest != "" && rest != "}" {
			buf = append(buf, strings.TrimSuffix(rest, "}"))
		}
	}
	if depth == 0 {
		return strings.Join(buf, "\n"), start + 1, nil
	}
	for i := start + 1; i < len(lines); i++ {
		raw := lines[i]
		if depth == 1 && strings.TrimSpace(raw) == "}" {
			return strings.Join(buf, "\n"), i + 1, nil
		}
		buf = append(buf, raw)
		depth += strings.Count(raw, "{") - strings.Count(raw, "}")
		if depth == 0 {
			return strings.Join(buf, "\n"), i + 1, nil
		}
	}
	return "", start, fmt.Errorf("花括号没有闭合")
}

// replaceAmp 把选择器里的 & 替换成作用域前缀。
func replaceAmp(selector, scope string) string {
	return strings.ReplaceAll(selector, "&", scope)
}

// stripCSSComment 去掉行内的 /* … */ 注释。
func stripCSSComment(line string) string {
	for {
		start := strings.Index(line, "/*")
		if start < 0 {
			return line
		}
		end := strings.Index(line[start:], "*/")
		if end < 0 {
			return line[:start]
		}
		line = line[:start] + line[start+end+2:]
	}
}
