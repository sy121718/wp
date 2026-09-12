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
//
// 属性驱动的样式（颜色 / 宽度 / 比例等由 Props 算出的值）用**值变量**表达：
//
//	& { width: {{width}}; background: {{color}}; }
//
// 变量由调用方经 ApplyComponentCSSTmpl 传入。两条配套规则：
//   · **任一变量为空 → 整条声明省略**（而不是输出 `width: ;` 这种无效声明）。
//     这让「可选属性」不必在 Go 里写 if：CSS 里的声明天然表达「设了才输出」，
//     与迁移前 b.Add 的条件拼装等价。
//   · **变量必须双向对齐**：源里引用了未提供的变量、或提供的变量没被源消费，都返回 error。
//     Go 侧与 .css 各写一半，拼写错误只会表现为「样式悄悄少了」，必须在构建期拦住。
//
// 结构性分支（同一选择器在不同模式下是**不同的声明组**，如 badge 的三种变体、
// divider 的有/无嵌入）用**声明块内的条件段**表达：
//
//	& {
//	  display: block;
//	  @if outline
//	  color: {{color}};
//	  border: 1px solid {{color}};
//	  @endif
//	}
//
// 条件段写在声明块里，命中的分支与同块其余声明**合并进同一条规则** ——
// 产物与「Go 里按条件拼一个声明切片、只 Add 一次」等价，这是迁移能做到逐字节一致的前提。
// 真值判定：变量为空串 / "0" / "false" / "no" / "off" 即假（大小写不敏感），其余为真。

import (
	"fmt"
	"strings"
)

// cssApplyError 描述样式源里的问题，带行号便于定位。
func cssApplyError(line int, format string, args ...any) error {
	return fmt.Errorf("组件样式第 %d 行: %s", line, fmt.Sprintf(format, args...))
}

// ApplyComponentCSS 解析组件 CSS 源并写入 b（样式源不含变量时的入口）。
//
// scope 是该 node 的作用域选择器（形如 ".sky-c-xxxx"），顶层 `&` 会被替换成它。
func ApplyComponentCSS(b *CSSBuckets, scope, source string) error {
	return ApplyComponentCSSTmpl(b, scope, source, nil)
}

// ApplyComponentCSSTmpl 解析带变量的组件 CSS 源并写入 b。
//
// vars 是样式源可用的变量表：`{{name}}` 取值、`@if name` 判真。
// 返回 error 时 b 可能已被部分写入 —— 调用方（组件 compileCSS）应当 panic 而非吞掉：
// 样式解析失败属于构建期缺陷，静默跳过的后果是产物悄悄少了样式。
func ApplyComponentCSSTmpl(b *CSSBuckets, scope, source string, vars map[string]string) error {
	if b == nil {
		return fmt.Errorf("CSSBuckets 为空")
	}
	if strings.TrimSpace(source) == "" {
		return nil
	}
	parser := &cssSourceParser{src: source, scope: scope, buckets: b, vars: vars, used: map[string]bool{}}
	if err := parser.run(); err != nil {
		return err
	}
	// 反向校验：提供的变量必须全部被样式源消费。
	// 组件迁移最容易出的错是「Go 侧改了变量名、.css 没跟着改」（或少改一处），
	// 这类错误不会让构建失败，只会让某个属性在产物里消失 —— 在这里拦住。
	for name := range vars {
		if !parser.used[name] {
			return fmt.Errorf("组件样式变量 %q 未被样式源使用（Go 侧与 .css 变量名不一致？）", name)
		}
	}
	return nil
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
	// vars 值变量表（nil 表示样式源不含变量）。
	vars map[string]string
	// used 记录已被消费的变量名（@if 与 {{name}} 都算），用于反向校验。
	// 递归解析 @media / @if 块时共享同一个 map。
	used map[string]bool
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
		// @if：**规则级**条件块，包住整条规则或指令（声明块内的条件段由 parseDecls 处理）。
		// 两处同名但作用域不同：这里的 @if 独占一行、位于规则之外，用来让一整段规则
		// （典型是 @keyframes —— 它不是一个声明，声明级条件段包不住）随变量决定存废。
		if strings.HasPrefix(line, "@if ") {
			name := strings.TrimSpace(strings.TrimPrefix(line, "@if "))
			v, ok := p.vars[name]
			if !ok {
				return cssApplyError(i+1, "@if 引用了未提供的变量 %q", name)
			}
			p.used[name] = true
			body, next, err := collectIfBlock(lines, i)
			if err != nil {
				return cssApplyError(i+1, "%v", err)
			}
			if truthy(v) {
				inner := &cssSourceParser{src: body, scope: p.scope, buckets: p.buckets, mediaBP: p.mediaBP, vars: p.vars, used: p.used}
				if err := inner.run(); err != nil {
					return err
				}
			}
			i = next
			continue
		}
		if line == "@endif" {
			return cssApplyError(i+1, "@endif 没有对应的 @if")
		}
		// @keyframes：整体取出（含花括号内部）。块内**每行是一帧**（与内建关键帧文件同约定），
		// 交给 AddKeyframesDecls 而不是 AddKeyframes —— 后者要求调用方自己拼好
		// "@keyframes name { … }" 整段文本，前者负责这个格式；走同一条装配路径，
		// 从 Go 迁过来的关键帧与从 @keyframes 迁过来的产物才会逐字节一致。
		if strings.HasPrefix(line, "@keyframes ") {
			name := strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(line, "@keyframes "), "{"))
			body, next, err := collectBlock(lines, i, line, strings.Index(line, "{"))
			if err != nil {
				return cssApplyError(i+1, "%v", err)
			}
			p.buckets.AddKeyframesDecls(name, frameLines(body))
			i = next
			continue
		}
		// @media：块内是普通规则，按其断点归桶。
		if strings.HasPrefix(line, "@media") {
			body, next, err := collectBlock(lines, i, line, strings.Index(line, "{"))
			if err != nil {
				return cssApplyError(i+1, "%v", err)
			}
			bp, err := mediaBreakpointOf(line)
			if err != nil {
				return cssApplyError(i+1, "%v", err)
			}
			inner := &cssSourceParser{src: body, scope: p.scope, buckets: p.buckets, mediaBP: bp, vars: p.vars, used: p.used}
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
		// 选择器提取分两种形态：
		//   · 多行格式（"sel {"）：去掉行尾的 {；
		//   · 单行规则（"sel { a: 1; b: 2; }"）：取第一个 { 之前 —— 用 TrimSuffix 会拿到整行，
		//     于是选择器里混进声明文本，产物看起来「多了个奇怪的选择器」而样式全部失效。
		// **不能一律取第一个 {**：选择器里可能有 {{变量}} 占位（如 :has(+ {{scope}})），
		// 那会让 { 提前出现，把选择器从中间截断成 "@global .x:has(+ "。
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
		// @global：**故意**产出跨实例共享的全局规则（如灯箱浮层 —— 同一页多个图片共用一份）。
		// 必须显式写出来：不含 & 又不带标记的选择器一律拒绝，避免「以为在作用域内其实漏了 &」
		// 导致样式泄漏到全站。多个实例重复登记同一条全局规则由 CSSBuckets 去重。
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
		// 选择器里的 & 一律替换成作用域前缀；不含 & 的写法拒绝（避免「以为在作用域内其实不是」）。
		if !strings.Contains(selector, "&") {
			return cssApplyError(i+1, "选择器必须以 & 开头（& 会被替换成该组件实例的作用域），或用 @global 显式声明全局规则，got %q", selector)
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
//
// 声明块内支持**条件段** `@if name` … `@endif`：段内声明只在变量为真时产出。
// 它表达「同一选择器在不同模式下是不同声明组」的情形（badge 三种变体、divider 有无嵌入），
// 这类分支不是值替换能表达的 —— 而在声明块内做，各分支的声明会合并进同一条规则，
// 产物与「Go 里按条件拼一个声明切片、只 Add 一次」逐字节一致。
func (p *cssSourceParser) parseDecls(body string) ([]string, error) {
	var decls []string
	// cond 是条件段栈：内层声明只有在所有外层都生效时才解析（支持嵌套）。
	var cond []bool
	active := func() bool {
		for _, ok := range cond {
			if !ok {
				return false
			}
		}
		return true
	}
	// 声明之间用 ; 分隔，但要注意值里可能有 var(--x, a:b) 这类带冒号的内容 ——
	// 这里只按 ; 切分，不碰冒号，交给浏览器解析。@if / @endif 独占一行（不带分号），
	// 所以按 ; 切完再按换行切时仍是独立的一行，顺序不乱。
	for _, part := range strings.Split(body, ";") {
		for _, one := range strings.Split(part, "\n") {
			d := strings.TrimSpace(stripCSSComment(one))
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
			// 未命中的分支：声明不产出，但**仍然展开变量**。
			// 展开是为了让「提供的变量必须被样式源消费」这条校验在分支场景下依然成立 ——
			// 否则 A 分支命中时，B 分支专属的变量会被判成「提供了却没用到」而误报。
			// 引用了未提供的变量在任何分支都是错误，照旧报出。
			if !active() {
				if _, _, err := p.expandVars(d); err != nil {
					return nil, err
				}
				continue
			}
			// @need-keyframes <name>：登记「本组件用到某个内建关键帧」，本身不产出声明。
			// 动效词汇表白名单留在 Go（安全检查与 prefers-reduced-motion 判定都在那边），
			// 这里只是把「需要哪一帧」这件事从 Go 代码挪到样式源里 —— 与 @focus-ring 同思路。
			if strings.HasPrefix(d, "@need-keyframes ") {
				name := strings.TrimSpace(strings.TrimPrefix(d, "@need-keyframes "))
				if name == "" {
					return nil, fmt.Errorf("@need-keyframes 缺少名称")
				}
				p.buckets.NeedKeyframes(name)
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
			expanded, empty, err := p.expandVars(d)
			if err != nil {
				return nil, err
			}
			// 变量为空 → 整条声明省略。语义是「该属性没设」，而不是「赋一个空值」：
			// `width: ;` 在浏览器里会被丢弃，但会以无效声明的形式留在产物里污染字节。
			if empty {
				continue
			}
			// 变量值可以是**多条声明**（Go 侧用 "; " 连接，如排版组三端产出的一组声明）：
			// 展开后再按分号拆开逐条收录，顺序保持。
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

// expandVars 把声明文本里的 {{name}} 替换成变量值。
//
// 返回的 empty 表示**至少有一个占位取到空值** —— 调用方据此省略整条声明。
// 引用了未提供的变量直接报错：Go 侧与 .css 各写一半，静默留着 {{x}} 会让产物里
// 出现浏览器看不懂的声明，而那在页面上只表现为「样式不太对」。
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

// collectIfBlock 从规则级 @if 行开始收集到与之匹配的 @endif 的内容（支持嵌套）。
func collectIfBlock(lines []string, start int) (body string, next int, err error) {
	depth := 1
	var buf []string
	for i := start + 1; i < len(lines); i++ {
		t := strings.TrimSpace(stripCSSComment(lines[i]))
		switch {
		case strings.HasPrefix(t, "@if "):
			depth++
		case t == "@endif":
			depth--
			if depth == 0 {
				return strings.Join(buf, "\n"), i + 1, nil
			}
		}
		buf = append(buf, lines[i])
	}
	return "", start, fmt.Errorf("@if 没有对应的 @endif")
}

// BoolVar 条件段变量的真值形态：真给 "1"、假给空串。
//
// 组件把 Go 侧的布尔判定交给样式源的 @if 时用它。真值约定是「空串与 0/false/no/off 为假、
// 其余为真」，这里只产生两端之一 —— 放在 core 是因为它是样式源协议的一部分
// （与 @if 的真值规则成对），不是某个组件的业务。
func BoolVar(v bool) string {
	if v {
		return "1"
	}
	return ""
}

// frameLines 把关键帧块内容按行拆成帧列表（每行一帧，与内建关键帧文件同约定）。
//
// 约定「每行一帧」而不是按花括号解析：关键帧的帧体通常很短（from { opacity: 0 }），
// 拆行读得清楚；按括号解析会让多行帧体（例如从别处粘过来的 keyframes）产出与内建帧
// 不同的缩进，而缩进是要进产物字节的。
func frameLines(body string) []string {
	var frames []string
	for _, ln := range strings.Split(body, "\n") {
		if s := strings.TrimSpace(ln); s != "" {
			frames = append(frames, s)
		}
	}
	return frames
}

// truthy 条件段真值判定：空串与常见假值为假，其余为真（大小写不敏感）。
func truthy(v string) bool {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "", "0", "false", "no", "off":
		return false
	}
	return true
}

// collectBlock 从 start 行开始收集花括号块的内容（不含最外层花括号），返回内容与下一行下标。
//
// braceAt 是**规则块那个 {** 的下标，由调用方给出：选择器里可能出现 {{变量}} 占位
// （如 :has(+ {{scope}})），自行用 strings.Index 找第一个花括号会在那对占位花括号上找错位置。
func collectBlock(lines []string, start int, firstLine string, braceAt int) (string, int, error) {
	if braceAt < 0 || braceAt >= len(firstLine) {
		return "", start, fmt.Errorf("规则块缺少起始花括号")
	}
	block := firstLine[braceAt:]
	depth := strings.Count(block, "{") - strings.Count(block, "}")
	var buf []string
	// 第一行花括号之后的内容属于块内。
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
