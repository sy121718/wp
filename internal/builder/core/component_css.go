package core

import (
	"fmt"
	"strings"
)

func cssApplyError(line int, format string, args ...any) error {
	return fmt.Errorf("组件样式第 %d 行: %s", line, fmt.Sprintf(format, args...))
}

func ApplyComponentCSS(b *CSSBuckets, scope, source string) error {
	return ApplyComponentCSSTmpl(b, scope, source, nil)
}

func ApplyComponentCSSTmpl(b *CSSBuckets, scope, source string, vars map[string]string) error {
	return ApplyComponentCSSTmplLists(b, scope, source, vars, nil)
}

func ApplyComponentCSSTmplLists(b *CSSBuckets, scope, source string, vars map[string]string, lists map[string][]map[string]string) error {
	if b == nil {
		return fmt.Errorf("CSSBuckets 为空")
	}
	if strings.TrimSpace(source) == "" {
		return nil
	}
	parser := &cssSourceParser{
		src: source, scope: scope, buckets: b, vars: vars, lists: lists,
		used: map[string]bool{}, usedLists: map[string]bool{},
	}
	if err := parser.run(); err != nil {
		return err
	}

	for name := range vars {
		if !parser.used[name] {
			return fmt.Errorf("组件样式变量 %q 未被样式源使用（Go 侧与 .css 变量名不一致？）", name)
		}
	}
	for name := range lists {
		if !parser.usedLists[name] {
			return fmt.Errorf("组件样式列表 %q 未被样式源使用（Go 侧与 .css 列表名不一致？）", name)
		}
	}
	return nil
}

type cssSourceParser struct {
	src string

	scope string

	buckets *CSSBuckets

	mediaBP string

	vars map[string]string

	used map[string]bool

	lists     map[string][]map[string]string
	usedLists map[string]bool

	loopVars map[string]string

	lenientLoop bool
}

func (p *cssSourceParser) run() error {
	lines := strings.Split(p.src, "\n")
	i := 0
	for i < len(lines) {
		raw := lines[i]

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

		if strings.HasPrefix(line, "@each ") {
			varName, listName, ok := parseEachSpec(strings.TrimSpace(strings.TrimPrefix(line, "@each ")))
			if !ok {
				return cssApplyError(i+1, "@each 的写法是「@each <变量名> in <列表名>」，got %q", line)
			}
			items, ok := p.lists[listName]
			if !ok {
				return cssApplyError(i+1, "@each 引用了未提供的列表 %q", listName)
			}
			p.usedLists[listName] = true
			body, next, err := collectCondBlock(lines, i)
			if err != nil {
				return cssApplyError(i+1, "%v", err)
			}
			if len(items) == 0 {

				probe := &cssSourceParser{src: body, scope: p.scope, buckets: &CSSBuckets{}, mediaBP: p.mediaBP, vars: p.vars, lists: p.lists, used: p.used, usedLists: p.usedLists, loopVars: p.loopVars, lenientLoop: true}
				if err := probe.run(); err != nil {
					return cssApplyError(i+1, "%v", err)
				}
			}
			for _, item := range items {
				loopVars := make(map[string]string, len(item)+len(p.loopVars))

				for k, v := range p.loopVars {
					loopVars[k] = v
				}
				for k, v := range item {
					loopVars[varName+"."+k] = v
				}
				inner := &cssSourceParser{src: body, scope: p.scope, buckets: p.buckets, mediaBP: p.mediaBP, vars: p.vars, lists: p.lists, used: p.used, usedLists: p.usedLists, loopVars: loopVars}
				if err := inner.run(); err != nil {
					return err
				}
			}
			i = next
			continue
		}
		if line == "@endfor" {
			return cssApplyError(i+1, "@endfor 没有对应的 @each")
		}

		if strings.HasPrefix(line, "@if ") {
			name := strings.TrimSpace(strings.TrimPrefix(line, "@if "))
			v, ok := p.vars[name]
			if !ok {
				return cssApplyError(i+1, "@if 引用了未提供的变量 %q", name)
			}
			p.used[name] = true
			body, next, err := collectCondBlock(lines, i)
			if err != nil {
				return cssApplyError(i+1, "%v", err)
			}

			target := p.buckets
			if !truthy(v) {
				target = &CSSBuckets{}
			}
			inner := &cssSourceParser{src: body, scope: p.scope, buckets: target, mediaBP: p.mediaBP, vars: p.vars, lists: p.lists, used: p.used, usedLists: p.usedLists, loopVars: p.loopVars}
			if err := inner.run(); err != nil {
				return err
			}
			i = next
			continue
		}
		if line == "@endif" {
			return cssApplyError(i+1, "@endif 没有对应的 @if")
		}

		if strings.HasPrefix(line, "@keyframes ") {
			braceAt := blockBraceAt(line)

			name, _, err := p.expandVars(strings.TrimSpace(strings.TrimPrefix(line[:braceAt], "@keyframes ")))
			if err != nil {
				return cssApplyError(i+1, "%v", err)
			}
			if name == "" {
				return cssApplyError(i+1, "@keyframes 缺少名称")
			}
			body, next, err := collectBlock(lines, i, line, braceAt)
			if err != nil {
				return cssApplyError(i+1, "%v", err)
			}
			frames, err := p.expandBlockLines(body)
			if err != nil {
				return cssApplyError(i+1, "%v", err)
			}
			p.buckets.AddKeyframesDecls(name, frames)
			i = next
			continue
		}

		if strings.HasPrefix(line, "@property ") {
			braceAt := blockBraceAt(line)
			name := strings.TrimSpace(strings.TrimPrefix(line[:braceAt], "@property "))
			if name == "" {
				return cssApplyError(i+1, "@property 缺少名称")
			}
			body, next, err := collectBlock(lines, i, line, braceAt)
			if err != nil {
				return cssApplyError(i+1, "%v", err)
			}
			lines, err := p.expandBlockLines(body)
			if err != nil {
				return cssApplyError(i+1, "%v", err)
			}
			p.buckets.AddPropertyDecls(name, lines)
			i = next
			continue
		}

		if strings.HasPrefix(line, "@media") {
			body, next, err := collectBlock(lines, i, line, strings.Index(line, "{"))
			if err != nil {
				return cssApplyError(i+1, "%v", err)
			}
			bp, err := mediaBreakpointOf(line)
			if err != nil {
				return cssApplyError(i+1, "%v", err)
			}
			inner := &cssSourceParser{src: body, scope: p.scope, buckets: p.buckets, mediaBP: bp, vars: p.vars, lists: p.lists, used: p.used, usedLists: p.usedLists, loopVars: p.loopVars}
			if err := inner.run(); err != nil {
				return err
			}
			i = next
			continue
		}

		if name, ok := bucketDirective(line); ok {

			if p.mediaBP != "" {
				return cssApplyError(i+1, "%s 不能写在 @media 块内（容器查询与视口断点是两套档位，混用会互相打架）", name)
			}
			braceAt := strings.Index(line, "{")
			if strings.HasSuffix(line, "{") {
				braceAt = len(line) - 1
			}
			if braceAt <= 0 {
				return cssApplyError(i+1, "%s 缺少规则块（写成「%s … { 声明 }」）", name, name)
			}
			head := strings.TrimSpace(line[:braceAt])
			body, next, err := collectBlock(lines, i, line, braceAt)
			if err != nil {
				return cssApplyError(i+1, "%v", err)
			}
			decls, err := p.parseDecls(body)
			if err != nil {
				return cssApplyError(i+1, "%v", err)
			}
			if err := p.applyBucketQuery(name, strings.TrimSpace(strings.TrimPrefix(head, name)), decls); err != nil {
				return cssApplyError(i+1, "%v", err)
			}
			i = next
			continue
		}

		if !strings.Contains(line, "{") {
			return cssApplyError(i+1, "看不懂这一行（期望「选择器 {」或 @media/@keyframes）：%q", line)
		}

		var selector string
		braceAt := strings.Index(line, "{")
		if strings.HasSuffix(line, "{") {
			braceAt = len(line) - 1
		}
		selector = strings.TrimSpace(line[:braceAt])
		body, next, err := collectBlock(lines, i, line, braceAt)
		if err != nil {
			return cssApplyError(i+1, "%v", err)
		}
		decls, err := p.parseDecls(body)
		if err != nil {
			return cssApplyError(i+1, "%v", err)
		}

		if strings.HasPrefix(selector, "@global") {
			global, _, err := p.expandVars(strings.TrimSpace(strings.TrimPrefix(selector, "@global")))
			if err != nil {
				return cssApplyError(i+1, "%v", err)
			}
			if global == "" {
				return cssApplyError(i+1, "@global 缺少选择器")
			}
			bp := p.mediaBP
			if bp == "" {
				bp = BreakpointDesktop
			}
			p.buckets.Add(bp, global, decls)
			i = next
			continue
		}

		expanded, empty, err := p.expandVars(selector)
		if err != nil {
			return cssApplyError(i+1, "%v", err)
		}
		if empty {
			return cssApplyError(i+1, "选择器里的变量取到空值：%q", selector)
		}
		selector = expanded

		if !strings.Contains(selector, "&") {
			return cssApplyError(i+1, "选择器必须含 &（& 会被替换成该组件实例的作用域），或用 @global 显式声明全局规则，got %q", selector)
		}

		scoped := replaceAmp(selector, p.scope)
		switch {

		case strings.HasPrefix(selector, "@hover "):
			scoped = replaceAmp(strings.TrimSpace(strings.TrimPrefix(selector, "@hover")), p.scope)
			p.buckets.AddHover(scoped, decls)
		case strings.HasPrefix(selector, "@active "):
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

func blockBraceAt(line string) int {
	if strings.HasSuffix(line, "{") {
		return len(line) - 1
	}
	return strings.Index(line, "{")
}

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

	switch px {
	case 1024:
		return BreakpointTablet, nil
	case 767:
		return BreakpointMobile, nil
	}
	return "", fmt.Errorf("断点 %dpx 不在既有档位（1024 / 767），请复用现有断点", px)
}

func (p *cssSourceParser) parseDecls(body string) ([]string, error) {
	var decls []string

	var cond []bool
	active := func() bool {
		for _, ok := range cond {
			if !ok {
				return false
			}
		}
		return true
	}

	body = stripCSSComments(body)

	for _, part := range strings.Split(body, ";") {
		for _, one := range strings.Split(part, "\n") {
			d := strings.TrimSpace(one)
			if d == "" {
				continue
			}
			if strings.HasPrefix(d, "@if ") {
				name := strings.TrimSpace(strings.TrimPrefix(d, "@if "))
				v, ok := p.vars[name]
				if !ok {
					return nil, fmt.Errorf("@if 引用了未提供的变量 %q", name)
				}
				p.used[name] = true
				cond = append(cond, truthy(v))
				continue
			}
			if d == "@endif" {
				if len(cond) == 0 {
					return nil, fmt.Errorf("@endif 没有对应的 @if")
				}
				cond = cond[:len(cond)-1]
				continue
			}

			if !active() {
				if _, _, err := p.expandVars(d); err != nil {
					return nil, err
				}
				continue
			}

			if strings.HasPrefix(d, "@need-keyframes ") {

				raw := strings.TrimSpace(strings.TrimPrefix(d, "@need-keyframes "))
				name, _, err := p.expandVars(raw)
				if err != nil {
					return nil, err
				}
				if name == "" {
					return nil, fmt.Errorf("@need-keyframes 缺少名称")
				}
				p.buckets.NeedKeyframes(name)
				continue
			}

			switch d {
			case "@focus-ring":
				decls = append(decls, FocusRingDecls()...)
				continue
			case "@focus-transition":
				decls = append(decls, FocusTransitionDecl())
				continue
			}
			expanded, empty, err := p.expandVars(d)
			if err != nil {
				return nil, err
			}

			if empty {
				continue
			}

			for _, piece := range strings.Split(expanded, ";") {
				piece = strings.TrimSpace(piece)
				if piece == "" {
					continue
				}
				if !strings.Contains(piece, ":") {
					return nil, fmt.Errorf("看不懂这条声明：%q", piece)
				}
				decls = append(decls, piece)
			}
		}
	}
	if len(cond) != 0 {
		return nil, fmt.Errorf("@if 没有对应的 @endif")
	}
	return decls, nil
}

func (p *cssSourceParser) expandVars(s string) (out string, empty bool, err error) {
	if !strings.Contains(s, "{{") {
		return s, false, nil
	}
	var sb strings.Builder
	rest := s
	for {
		i := strings.Index(rest, "{{")
		if i < 0 {
			sb.WriteString(rest)
			break
		}
		j := strings.Index(rest[i:], "}}")
		if j < 0 {
			return "", false, fmt.Errorf("变量占位没有闭合：%q", s)
		}
		sb.WriteString(rest[:i])
		name := strings.TrimSpace(rest[i+2 : i+j])
		val, ok := p.vars[name]
		if !ok && p.loopVars != nil {

			val, ok = p.loopVars[name]
		}
		if !ok && p.lenientLoop && strings.Contains(name, ".") {

			val, ok = "x", true
		}
		if !ok {
			return "", false, fmt.Errorf("样式源引用了未提供的变量 %q（声明：%q）", name, s)
		}
		p.used[name] = true
		if strings.TrimSpace(val) == "" {
			empty = true
		}
		sb.WriteString(val)
		rest = rest[i+j+2:]
	}
	return sb.String(), empty, nil
}

func parseEachSpec(spec string) (varName, listName string, ok bool) {
	idx := strings.Index(spec, " in ")
	if idx < 0 {
		return "", "", false
	}
	varName = strings.TrimSpace(spec[:idx])
	listName = strings.TrimSpace(spec[idx+len(" in "):])
	if varName == "" || listName == "" || strings.ContainsAny(varName, ". ") {
		return "", "", false
	}
	return varName, listName, true
}

func collectCondBlock(lines []string, start int) (body string, next int, err error) {
	depth := 1
	var buf []string
	for i := start + 1; i < len(lines); i++ {
		t := strings.TrimSpace(stripCSSComment(lines[i]))
		switch {
		case strings.HasPrefix(t, "@if "), strings.HasPrefix(t, "@each "):
			depth++
		case t == "@endif", t == "@endfor":
			depth--
			if depth == 0 {
				return strings.Join(buf, "\n"), i + 1, nil
			}
		}
		buf = append(buf, lines[i])
	}
	return "", start, fmt.Errorf("条件块没有对应的结束标记（@endif / @endfor）")
}

func BoolVar(v bool) string {
	if v {
		return "1"
	}
	return ""
}

func (p *cssSourceParser) expandBlockLines(body string) ([]string, error) {
	lines := frameLines(body)
	out := make([]string, 0, len(lines))
	for _, ln := range lines {
		expanded, empty, err := p.expandVars(ln)
		if err != nil {
			return nil, err
		}
		if empty {
			if p.lenientLoop {

				continue
			}
			return nil, fmt.Errorf("块内变量取到空值：%q", ln)
		}
		out = append(out, expanded)
	}
	return out, nil
}

func frameLines(body string) []string {
	var frames []string
	for _, ln := range strings.Split(body, "\n") {
		if s := strings.TrimSpace(ln); s != "" {
			frames = append(frames, s)
		}
	}
	return frames
}

func truthy(v string) bool {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "", "0", "false", "no", "off":
		return false
	}
	return true
}

func collectBlock(lines []string, start int, firstLine string, braceAt int) (string, int, error) {
	if braceAt < 0 || braceAt >= len(firstLine) {
		return "", start, fmt.Errorf("规则块缺少起始花括号")
	}
	block := firstLine[braceAt:]
	depth := strings.Count(block, "{") - strings.Count(block, "}")
	var buf []string

	rest := strings.TrimSpace(firstLine[braceAt+1:])
	if rest != "" && rest != "}" {
		buf = append(buf, strings.TrimSuffix(rest, "}"))
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

func replaceAmp(selector, scope string) string {
	return strings.ReplaceAll(selector, "&", scope)
}

func stripCSSComments(s string) string {
	var sb strings.Builder
	for {
		i := strings.Index(s, "/*")
		if i < 0 {
			sb.WriteString(s)
			return sb.String()
		}
		sb.WriteString(s[:i])
		rest := s[i:]
		j := strings.Index(rest, "*/")
		if j < 0 {
			sb.WriteString(rest)
			return sb.String()
		}
		s = rest[j+2:]
	}
}

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

func bucketDirective(line string) (string, bool) {
	for _, d := range []string{"@hovernone", "@container", "@theme", "@style"} {
		if strings.HasPrefix(line, d+" ") || strings.HasPrefix(line, d+"(") {
			return d, true
		}
	}
	return "", false
}

func (p *cssSourceParser) applyBucketQuery(name, args string, decls []string) error {
	switch name {
	case "@hovernone":
		sel, err := p.bucketSelector(args)
		if err != nil {
			return err
		}
		p.buckets.AddHoverNone(sel, decls)
	case "@container":
		condition, tail, err := cutParen(args)
		if err != nil {
			return err
		}
		sel, err := p.bucketSelector(tail)
		if err != nil {
			return err
		}
		p.buckets.AddContainer(condition, sel, decls)
	case "@theme", "@style":
		tokens, tail, err := cutTokens(args, 3)
		if err != nil {
			return err
		}
		sel, err := p.bucketSelector(tail)
		if err != nil {
			return err
		}
		if name == "@theme" {
			p.buckets.AddThemeQuery(tokens[0], tokens[1], tokens[2], sel, decls)
		} else {
			p.buckets.AddStyleQuery(tokens[0], tokens[1], tokens[2], sel, decls)
		}
	}
	return nil
}

func (p *cssSourceParser) bucketSelector(raw string) (string, error) {
	if raw == "" {
		return "", fmt.Errorf("缺少选择器")
	}
	sel, empty, err := p.expandVars(raw)
	if err != nil {
		return "", err
	}
	if empty {
		return "", fmt.Errorf("选择器里的变量取到空值：%q", raw)
	}
	if !strings.Contains(sel, "&") {
		return "", fmt.Errorf("选择器必须以 & 开头（& 会被替换成该组件实例的作用域），got %q", sel)
	}
	return replaceAmp(sel, p.scope), nil
}

func cutParen(s string) (condition, rest string, err error) {
	if !strings.HasPrefix(s, "(") {
		return "", "", fmt.Errorf("@container 的条件要以 ( 开头（如 (width >= 480px)），got %q", s)
	}
	depth := 0
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 {
				return s[:i+1], strings.TrimSpace(s[i+1:]), nil
			}
		}
	}
	return "", "", fmt.Errorf("@container 的条件括号没有闭合：%q", s)
}

func cutTokens(s string, n int) (tokens []string, rest string, err error) {
	rest = strings.TrimSpace(s)
	for len(tokens) < n {
		if rest == "" {
			return nil, "", fmt.Errorf("参数不足，需要 %d 个（容器名 属性 值），got %q", n, s)
		}
		i := strings.IndexAny(rest, " \t")
		if i < 0 {
			tokens = append(tokens, rest)
			rest = ""
			break
		}
		tokens = append(tokens, rest[:i])
		rest = strings.TrimSpace(rest[i+1:])
	}
	return tokens, rest, nil
}
