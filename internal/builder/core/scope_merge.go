package core

// scope_merge.go — 同族规则的选择器并列合并（PERF-016）。
//
// 现象：每个组件实例都有自己的作用域类（.sky-c-<nodeID>），于是「配置相同的多个实例」
// 会各自产出一条**内容完全相同、只有作用域不同**的规则。8 个同配置按钮在产物里
// 就是 8 条各约 470 字节的规则，其中 7 条是纯重复；实例越多，CSS 越线性膨胀。
//
// 为什么可以合并：作用域类的唯一职责是把样式限定在该实例子树内，规则在选择器层
// 用逗号并列就是 OR 语义 —— 「.a .x, .b .x { D }」与「.a .x { D }」加「.b .x { D }」
// 逐条等价。特异性按「匹配它的那一条选择器」独立计算，与合并前一致；产物也不需要
// 更新的选择器语法（刻意不用 :is() 并列：逗号形式在任何浏览器上都是既有写法）。
//
// 安全边界（四条，缺一条都可能改变优先级语义）：
//  1. 声明完全相同 —— 只差作用域，而不是「看起来像」的同族规则；
//  2. 外壳完全相同 —— 跨 @media / @container 合并会把响应式档位或容器条件混在一起；
//  3. 成员在输出序列里**相邻**（中间没有别的规则）。合并后规则停在首个成员的位置、
//     其余成员位置被删除；只要中间没有别的规则，任何其它规则与它们的先后关系都不变，
//     于是「谁覆盖谁」逐条不变。中间夹着规则时不合并 —— 那正是唯一会让合并改变结果的形状；
//  4. 只处理真正的样式规则：@keyframes / @property 等顶层注册规则不参与
//     （把两条 @keyframes 并列起来是一份无效 CSS）。

import "strings"

// nodeScopeClass 实例作用域类名前缀（与 NodeClass 的前缀一致：.sky-c-<nodeID>）。
const nodeScopeClass = ".sky-c-"

// wrappedRuleOpen 外壳与内层规则的分隔文本（AddHover / AddContainer 等一致的写法）。
const wrappedRuleOpen = " {\n  "

// wrappedRuleClose 外壳闭合文本。
const wrappedRuleClose = "\n}"

// ruleParts 一条规则的解剖结果：外壳（@media / @container 包裹）与选择器 / 声明块体。
// 裸规则（直接进桶的桌面 / 平板 / 手机 / 按压规则）head 与 tail 为空串。
type ruleParts struct {
	head string
	sel  string
	body string // 声明块体：含缩进与换行，不含最外层花括号
	tail string
}

// normalizeNodeScope 把选择器里的实例作用域类名折叠成占位形式（.sky-c-#），
// 用于判断两条规则是否「只差作用域」。折叠只吃掉类名本身，其余字符（空格、后代组合符、
// 伪类）原样保留 —— 于是 .sky-c-a 后接空格与后接点号的选择器折叠后仍然不同，不会误判同类。
func normalizeNodeScope(sel string) string {
	if !strings.Contains(sel, nodeScopeClass) {
		return sel
	}
	var sb strings.Builder
	rest := sel
	for {
		i := strings.Index(rest, nodeScopeClass)
		if i < 0 {
			sb.WriteString(rest)
			return sb.String()
		}
		sb.WriteString(rest[:i])
		sb.WriteString(nodeScopeClass + "#")
		rest = rest[i+len(nodeScopeClass):]
		for len(rest) > 0 && isCSSIdentByte(rest[0]) {
			rest = rest[1:]
		}
	}
}

// isCSSIdentByte 判断是否属于类名标识符的字符（ASCII 足够：nodeID 由前端生成，
// 转义序列与非 ASCII 标识符在本项目的 node id 上不会出现）。
func isCSSIdentByte(b byte) bool {
	return b == '-' || b == '_' ||
		(b >= '0' && b <= '9') || (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z')
}

// splitRule 把一条裸规则文本拆成选择器与声明块体。
//
// 只认「选择器 + 空格左花括号换行 + 声明 + 右花括号」这一种形状（本包各个 Add* 的唯一产出形状）；
// 形状不符就返回 false，调用方原样放行 —— 不认识的东西不猜。
func splitRule(rule string) (sel, body string, ok bool) {
	if !strings.HasSuffix(rule, "}") {
		return "", "", false
	}
	i := strings.Index(rule, " {\n")
	if i <= 0 {
		return "", "", false
	}
	return rule[:i], rule[i+len(" {\n") : len(rule)-1], true
}

// splitRuleParts 解剖一条桶内元素：可能是裸规则，也可能是被 @media / @container
// 整段包裹的规则（hover / 触屏等价 / 容器查询桶）。
// 认不出的形态返回 false，调用方原样放行。
func splitRuleParts(raw string) (ruleParts, bool) {
	if strings.HasPrefix(raw, "@media ") || strings.HasPrefix(raw, "@container ") {
		i := strings.Index(raw, wrappedRuleOpen)
		if i <= 0 || !strings.HasSuffix(raw, wrappedRuleClose) {
			return ruleParts{}, false
		}
		inner := raw[i+len(wrappedRuleOpen) : len(raw)-len(wrappedRuleClose)]
		sel, body, ok := splitRule(inner)
		if !ok {
			return ruleParts{}, false
		}
		return ruleParts{head: raw[:i], sel: sel, body: body, tail: wrappedRuleClose}, true
	}
	// @keyframes / @property 等顶层注册规则不参与合并：并列起来是一份无效 CSS。
	if strings.HasPrefix(raw, "@") {
		return ruleParts{}, false
	}
	sel, body, ok := splitRule(raw)
	if !ok {
		return ruleParts{}, false
	}
	return ruleParts{sel: sel, body: body}, true
}

// mergeScopeSiblings 把桶内「相邻、外壳相同、只差作用域、声明相同」的规则并列成一条。
//
// 输入与输出都是桶的规则切片（每项是一条完整规则文本），顺序保持不变：
// 合并后的规则停在首个成员的位置，因此桶内其余规则的相对次序不受影响。
func mergeScopeSiblings(rules []string) []string {
	if len(rules) < 2 {
		return rules
	}
	out := make([]string, 0, len(rules))
	for i := 0; i < len(rules); i++ {
		p, ok := splitRuleParts(rules[i])
		if !ok || strings.Contains(p.sel, "\n") {
			out = append(out, rules[i])
			continue
		}
		key := p.head + "\x00" + normalizeNodeScope(p.sel) + "\x00" + p.body + "\x00" + p.tail
		sels := []string{strings.TrimSpace(p.sel)}
		j := i + 1
		for j < len(rules) {
			q, qok := splitRuleParts(rules[j])
			if !qok || strings.Contains(q.sel, "\n") ||
				q.head+"\x00"+normalizeNodeScope(q.sel)+"\x00"+q.body+"\x00"+q.tail != key {
				break
			}
			sels = append(sels, strings.TrimSpace(q.sel))
			j++
		}
		if len(sels) == 1 {
			out = append(out, rules[i])
			continue
		}
		merged := strings.Join(sels, ", ") + " {\n" + p.body + "}"
		if p.head != "" {
			merged = p.head + wrappedRuleOpen + merged + p.tail
		}
		out = append(out, merged)
		i = j - 1
	}
	return out
}
