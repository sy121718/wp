package core

import "strings"

const nodeScopeClass = ".sky-c-"

const wrappedRuleOpen = " {\n  "

const wrappedRuleClose = "\n}"

type ruleParts struct {
	head string
	sel  string
	body string
	tail string
}

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

func isCSSIdentByte(b byte) bool {
	return b == '-' || b == '_' ||
		(b >= '0' && b <= '9') || (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z')
}

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

	if strings.HasPrefix(raw, "@") {
		return ruleParts{}, false
	}
	sel, body, ok := splitRule(raw)
	if !ok {
		return ruleParts{}, false
	}
	return ruleParts{sel: sel, body: body}, true
}

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
