package style

import (
	"fmt"
	"strconv"
	"strings"

	"go_wp/internal/builder/core"
	"go_wp/internal/builder/source"
)

func Compile(nodeID string, props map[string]any, schema *Schema, b source.StyleSink) (err error) {
	if schema == nil {
		return nil
	}
	if b == nil {
		return fmt.Errorf("样式编译缺少 CSS 收集器")
	}

	if !IsSafeNodeID(nodeID) {
		return fmt.Errorf("样式编译收到非法节点 ID: %q", nodeID)
	}
	base := "." + core.NodeClass(nodeID)
	for _, r := range schema.Rules {
		if !matchWhen(r.When, props) {
			continue
		}

		selPseudo := r.Pseudo
		if selPseudo == "hover-none" {
			selPseudo = ""
		}
		selector := buildSelector(base, r.Target, selPseudo)

		decls := make([]string, 0, len(r.Decls)+len(r.Vars)+len(r.Bindings))
		for _, d := range r.Decls {
			decls = append(decls, core.CSSDecl(d[0], d[1]))
		}
		for _, v := range r.Vars {
			val, ok := propString(props, v.From)
			if !ok || val == "" {
				continue
			}
			if !core.IsSafeCSSValue(val) {
				return fmt.Errorf("节点 %s: 变量 --%s 的值（来自 %q）含非法字符", nodeID, v.Name, v.From)
			}
			decls = append(decls, core.CSSDecl("--"+v.Name, val))
		}
		for _, bind := range r.Bindings {
			v, ok := propString(props, bind.From)
			if !ok || v == "" {
				continue
			}
			full := bind.Prefix + v + bind.Suffix
			if !core.IsSafeCSSValue(full) {
				return fmt.Errorf("节点 %s: 属性 %q 的绑定值（来自 %q）含非法字符",
					nodeID, bind.Prop, bind.From)
			}
			decls = append(decls, core.CSSDecl(bind.Prop, full))
		}

		switch r.Pseudo {
		case "hover":
			b.AddHover(selector, decls)
		case "hover-none":
			b.AddHoverNone(selector, decls)
		case "active":
			b.AddActive(selector, decls)
		default:
			b.Add(core.BreakpointDesktop, selector, decls)
		}

		for _, q := range r.Queries {
			qDecls := make([]string, 0, len(q.Decls))
			for _, d := range q.Decls {
				qDecls = append(qDecls, core.CSSDecl(d[0], d[1]))
			}
			switch q.Kind {
			case "size":
				b.AddContainer(q.Condition, selector, qDecls)
			case "theme":
				b.AddThemeQuery(q.Container, q.Prop, q.Value, selector, qDecls)
			default:
				b.AddStyleQuery(q.Container, q.Prop, q.Value, selector, qDecls)
			}
		}

		for _, bp := range []string{core.BreakpointTablet, core.BreakpointMobile} {
			over, ok := r.Breakpoints[bp]
			if !ok {
				continue
			}
			bpDecls := make([]string, 0, len(over))
			for _, d := range over {
				bpDecls = append(bpDecls, core.CSSDecl(d[0], d[1]))
			}
			b.Add(bp, selector, bpDecls)
		}
	}
	return nil
}

func buildSelector(base, target, pseudo string) string {
	sel := base
	if target != "" {
		sel += " " + target
	}
	if pseudo != "" {
		sel += ":" + pseudo
	}
	return sel
}

func matchWhen(when string, props map[string]any) bool {
	if when == "" {
		return true
	}
	parts := strings.SplitN(when, "=", 2)
	v, ok := propString(props, parts[0])
	return ok && v == parts[1]
}

func propString(props map[string]any, key string) (string, bool) {
	raw, ok := props[key]
	if !ok || raw == nil {
		return "", false
	}
	switch v := raw.(type) {
	case string:
		return v, true
	case bool:
		return strconv.FormatBool(v), true
	case int:
		return strconv.Itoa(v), true
	case int64:
		return strconv.FormatInt(v, 10), true
	case float64:

		if v == float64(int64(v)) {
			return strconv.FormatInt(int64(v), 10), true
		}
		return strconv.FormatFloat(v, 'g', -1, 64), true
	default:
		return fmt.Sprintf("%v", v), true
	}
}
