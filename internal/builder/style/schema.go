package style

// Schema 插件组件样式声明的结构定义（manifest.json "styles" 段的 Go 投影）。
//
// 数据流：插件上传时 Validate（结构/白名单校验，坏 schema 直接拒绝安装）
// → 构建时 Compile（props 值绑定 + 断点分发进 CSSBuckets）。
// 声明用 slice 保序（禁 map），保证确定性编译。

import (
	"fmt"
	"strings"

	"go_wp/internal/builder/core"
)

// Schema 一组受控样式规则（manifest styles 段）。
type Schema struct {
	// Rules 规则列表，按声明顺序编译（确定性）。
	Rules []Rule `json:"rules"`
}

// Rule 单条受控样式规则：选择器（受控）+ 条件（枚举匹配）+ 声明 + 断点覆盖。
type Rule struct {
	// Target 子元素选择器：""（组件根）或 ".badge"、".head .title"（白名单校验）。
	Target string `json:"target,omitempty"`
	// Pseudo 伪类枚举：""/hover/focus/active/focus-visible/focus-within/disabled。
	// 由引擎拼接到选择器尾部，不接受任意伪类字符串。
	Pseudo string `json:"pseudo,omitempty"`
	// When 变体条件："key=value" 形式（key 为 props 键，value 为枚举等值匹配）。
	// 对标内置组件 variant 系统（如 button 的 solid/outline/ghost）。
	When string `json:"when,omitempty"`
	// Decls 静态声明：[N][2]{"属性名", "值"}，值经 core.IsSafeCSSValue。
	Decls [][2]string `json:"decls,omitempty"`
	// Bindings 属性绑定：检查器控件值 → CSS 声明（值同样过值白名单）。
	Bindings []Binding `json:"bindings,omitempty"`
	// Breakpoints 断点覆盖：tablet/mobile 的静态声明（desktop 落主声明）。
	Breakpoints map[string][][2]string `json:"breakpoints,omitempty"`
}

// Binding 属性绑定：CSS 属性 ← props 键。
type Binding struct {
	// Prop 目标 CSS 属性名（白名单）。
	Prop string `json:"prop"`
	// From 取值的 props 键（manifest props schema 定义的控件键）。
	From string `json:"from"`
	// Suffix 可选值后缀（如 transform 绑定 "translateY(" + 值 + ")" 场景的
	// 前后缀拼接；前后缀同样过值白名单）。通常留空。
	Prefix string `json:"prefix,omitempty"`
	Suffix string `json:"suffix,omitempty"`
}

// Validate 校验 schema 结构与白名单（插件上传/安装时调用，坏 schema 拒绝）。
//
// 校验项：选择器 target/伪类枚举、When 键值字符、全部静态声明与前后缀
// 的属性名白名单 + 值白名单、断点标识。绑定值是运行期用户输入，
// 由 Compile 时校验（防御深度：两道都过 IsSafeCSSValue）。
func (s *Schema) Validate() (err error) {
	if s == nil {
		return fmt.Errorf("样式 schema 为空")
	}
	for i, r := range s.Rules {
		if !IsSafeTarget(r.Target) {
			return fmt.Errorf("规则 %d: 非法子元素选择器 %q", i, r.Target)
		}
		if !IsSafePseudo(r.Pseudo) {
			return fmt.Errorf("规则 %d: 非法伪类 %q", i, r.Pseudo)
		}
		if r.When != "" {
			if err = validateWhen(r.When, i); err != nil {
				return err
			}
		}
		if err = validateDecls(i, "decls", r.Decls); err != nil {
			return err
		}
		for j, b := range r.Bindings {
			if !IsSafeProp(b.Prop) {
				return fmt.Errorf("规则 %d 绑定 %d: CSS 属性 %q 不在白名单", i, j, b.Prop)
			}
			if b.From == "" {
				return fmt.Errorf("规则 %d 绑定 %d: 缺少取值键 from", i, j)
			}
			for _, part := range []string{b.Prefix, b.Suffix} {
				if part != "" && !core.IsSafeCSSValue(part) {
					return fmt.Errorf("规则 %d 绑定 %d: 前后缀含非法字符", i, j)
				}
			}
		}
		for bp, decls := range r.Breakpoints {
			if !breakpointsLegal(bp) {
				return fmt.Errorf("规则 %d: 非法断点 %q", i, bp)
			}
			if err = validateDecls(i, "breakpoints["+bp+"]", decls); err != nil {
				return err
			}
		}
	}
	return nil
}

// validateDecls 校验一组静态声明（属性名白名单 + 值白名单）。
func validateDecls(ruleIdx int, where string, decls [][2]string) (err error) {
	for j, d := range decls {
		if !IsSafeProp(d[0]) {
			return fmt.Errorf("规则 %d %s 声明 %d: CSS 属性 %q 不在白名单", ruleIdx, where, j, d[0])
		}
		if !core.IsSafeCSSValue(d[1]) {
			return fmt.Errorf("规则 %d %s 声明 %d: 值含非法字符", ruleIdx, where, j)
		}
	}
	return nil
}

// validateWhen 校验条件格式 "key=value"（键值均为白名单字符，防条件注入）。
func validateWhen(when string, ruleIdx int) (err error) {
	parts := strings.SplitN(when, "=", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return fmt.Errorf("规则 %d: 条件 %q 必须形如 key=value", ruleIdx, when)
	}
	for _, p := range parts {
		if !keyCharRe.MatchString(p) {
			return fmt.Errorf("规则 %d: 条件 %q 含非法字符", ruleIdx, when)
		}
	}
	return nil
}
