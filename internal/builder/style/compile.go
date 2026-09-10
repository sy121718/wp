package style

import (
	"fmt"
	"strconv"
	"strings"

	"go_wp/internal/builder/core"
)

// Compile 把样式声明 + 当前 props 值确定性编译进 CSSBuckets。
//
//   - nodeID：组件节点 ID（选择器前缀 core.NodeClass(nodeID)，与内置组件一致，
//     编辑器桥接依赖 sky-c-{id} 还原 data-sky-id）；
//   - props：检查器控件值（键为 manifest props schema 的键，值 string/number；
//     非字符串值转字符串；空值跳过声明，与内置组件 CSSDecl 跳空值语义一致）；
//   - 值防御深度：绑定值（用户运行期输入）在此处再过 core.IsSafeCSSValue，
//     不安全值返回错误（构建失败优于静默丢样式——与草稿校验失败拒绝保存同立场）。
//
// 确定性：规则按声明序编译，声明/绑定按数组序输出，断点只追加不排序，
// 全部走 core.CSSBuckets.Add（与内置组件同一确定性管线）。
func Compile(nodeID string, props map[string]any, schema *Schema, b *core.CSSBuckets) (err error) {
	if schema == nil {
		return nil
	}
	if b == nil {
		return fmt.Errorf("样式编译缺少 CSS 收集器")
	}
	// 防御深度：节点 ID 白名单（内置路径已有页面级校验，插件路径未信任，
	// 入口自卫防畸形 ID 产出损坏选择器）。
	if !IsSafeNodeID(nodeID) {
		return fmt.Errorf("样式编译收到非法节点 ID: %q", nodeID)
	}
	base := "." + core.NodeClass(nodeID)
	for _, r := range schema.Rules {
		if !matchWhen(r.When, props) {
			continue
		}
		selector := buildSelector(base, r.Target, r.Pseudo)
		// 主声明（desktop）：静态 + 绑定。
		decls := make([]string, 0, len(r.Decls)+len(r.Bindings))
		for _, d := range r.Decls {
			decls = append(decls, core.CSSDecl(d[0], d[1]))
		}
		for _, bind := range r.Bindings {
			v, ok := propString(props, bind.From)
			if !ok || v == "" {
				continue // 控件未设置：跳过该声明（对齐内置组件跳空值语义）
			}
			full := bind.Prefix + v + bind.Suffix
			if !core.IsSafeCSSValue(full) {
				return fmt.Errorf("节点 %s: 属性 %q 的绑定值（来自 %q）含非法字符",
					nodeID, bind.Prop, bind.From)
			}
			decls = append(decls, core.CSSDecl(bind.Prop, full))
		}
		b.Add(core.BreakpointDesktop, selector, decls)
		// 断点覆盖（tablet/mobile）。
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

// buildSelector 受控拼装选择器：基类 + 后代子元素 + 伪类（无任意字符串拼接面）。
// target 以空格分隔（后代选择器：".badge" → ".sky-c-x .badge"，
// 绝不拼接成 ".sky-c-x.badge" 的同元素双类语义）。
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

// matchWhen 条件匹配："key=value" 对 props 等值比较；空条件恒真。
// props 值经 propString 归一（number → string）后比较。
func matchWhen(when string, props map[string]any) bool {
	if when == "" {
		return true
	}
	parts := strings.SplitN(when, "=", 2)
	v, ok := propString(props, parts[0])
	return ok && v == parts[1]
}

// propString props 值归一为字符串（string 原样；整数/浮点转字符串；
// bool 转true/false；其余类型不支持返回缺省）。
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
		// JSON number 归一：整数值输出整数形式（16 而非 16.0）。
		if v == float64(int64(v)) {
			return strconv.FormatInt(int64(v), 10), true
		}
		return strconv.FormatFloat(v, 'g', -1, 64), true
	default:
		return fmt.Sprintf("%v", v), true
	}
}
