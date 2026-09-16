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
	// Pseudo 伪类枚举：""/hover/hover-none/focus/active/focus-visible/focus-within/
	// disabled/first-child/last-child/only-child。由引擎拼接到选择器尾部，不接受任意
	// 伪类字符串；hover / hover-none / active 走专用样式桶（触屏治理，见 compile.go）。
	Pseudo string `json:"pseudo,omitempty"`
	// When 变体条件："key=value" 形式（key 为 props 键，value 为枚举等值匹配）。
	// 对标内置组件 variant 系统（如 button 的 solid/outline/ghost）。
	When string `json:"when,omitempty"`
	// Decls 静态声明：[N][2]{"属性名", "值"}，值经 core.IsSafeCSSValue。
	Decls [][2]string `json:"decls,omitempty"`
	// Vars 自定义属性导出：检查器控件值 → CSS 变量（供插件包 assets/*.css 消费）。
	Vars []VarBinding `json:"vars,omitempty"`
	// Bindings 属性绑定：检查器控件值 → CSS 声明（值同样过值白名单）。
	Bindings []Binding `json:"bindings,omitempty"`
	// Queries 容器/主题查询：组件按所在容器宽度或主题档位切换样式（内置组件同款能力）。
	Queries []Query `json:"queries,omitempty"`
	// Breakpoints 断点覆盖：tablet/mobile 的静态声明（desktop 落主声明）。
	// 与 hover / hover-none / active 互斥（专用桶没有断点维度）。
	Breakpoints map[string][][2]string `json:"breakpoints,omitempty"`
}

// VarBinding 自定义属性导出：把检查器控件值写成 CSS 变量，声明在规则对应的选择器上。
// 用途：插件包的 assets/*.css 是全局静态 CSS，拿不到单个组件的检查器值；
// 变量导出把「配置」变成可继承的 CSS 变量，静态 CSS 用 var(--name) 即可消费
// （组件子树内生效，不污染组件外）。
type VarBinding struct {
	// Name 变量名（不含前导 "--"，字母开头，用于拼 --{name}）。
	Name string `json:"name"`
	// From 取值的 props 键（manifest props schema 定义的控件键）。
	From string `json:"from"`
}

// Query 容器查询 / 主题档位查询（对标内置组件的 AddContainer / AddThemeQuery）。
//   - kind=size  ：尺寸查询，条件形如 "(width >= 480px)" → @layer sky-auto；
//   - kind=theme ：主题档位（style 查询），如 sky-theme 上的 --sky-density: compact
//     → @layer sky-theme；
//   - kind=local ：局部样式查询（容器/作者显式声明）→ @layer sky-local。
//
// 容器未声明对应属性时不匹配，自然降级为默认样式（零副作用）。
type Query struct {
	// Kind 查询类型：size / theme / local。
	Kind string `json:"kind"`
	// Condition 尺寸查询条件（仅 kind=size 必填），形如 "(width >= 480px)"。
	Condition string `json:"condition,omitempty"`
	// Container 样式查询的容器名（kind=theme/local 必填），如 "sky-theme"。
	Container string `json:"container,omitempty"`
	// Prop 样式查询属性（kind=theme/local 必填），仅自定义属性，如 "--sky-density"。
	Prop string `json:"prop,omitempty"`
	// Value 样式查询期望值（kind=theme/local 必填），如 "compact"。
	Value string `json:"value,omitempty"`
	// Decls 命中时输出的声明（属性名/值双白名单）。
	Decls [][2]string `json:"decls,omitempty"`
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
		// 专用桶（hover / hover-none / active）没有断点维度：同时声明断点会静默丢样式，
		// 因此在校验期拒绝（与「构建失败优于静默丢样式」同立场）。
		if len(r.Breakpoints) > 0 && IsDedicatedBucketPseudo(r.Pseudo) {
			return fmt.Errorf("规则 %d: pseudo %q 走专用样式桶，不能同时声明 breakpoints", i, r.Pseudo)
		}
		for j, v := range r.Vars {
			if !IsSafeVarName(v.Name) {
				return fmt.Errorf("规则 %d 变量 %d: 名字 %q 非法（小写字母开头，字母数字连字符）", i, j, v.Name)
			}
			if v.From == "" {
				return fmt.Errorf("规则 %d 变量 %d: 缺少取值键 from", i, j)
			}
		}
		for j, q := range r.Queries {
			if err = validateQuery(i, j, q); err != nil {
				return err
			}
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

// validateQuery 校验单条容器/主题查询：kind 枚举、各 kind 的必备字段白名单、声明白名单。
func validateQuery(ruleIdx, qIdx int, q Query) (err error) {
	switch q.Kind {
	case "size":
		if !IsSafeContainerCondition(q.Condition) {
			return fmt.Errorf("规则 %d 查询 %d: 容器条件 %q 非法（形如 \"(width >= 480px)\"）", ruleIdx, qIdx, q.Condition)
		}
		if q.Container != "" || q.Prop != "" || q.Value != "" {
			return fmt.Errorf("规则 %d 查询 %d: kind=size 不接受 container/prop/value", ruleIdx, qIdx)
		}
	case "theme", "local":
		if !IsSafeContainerName(q.Container) {
			return fmt.Errorf("规则 %d 查询 %d: 容器名 %q 非法", ruleIdx, qIdx, q.Container)
		}
		if !IsSafeQueryProp(q.Prop) {
			return fmt.Errorf("规则 %d 查询 %d: 查询属性 %q 非法（须为 -- 开头的自定义属性）", ruleIdx, qIdx, q.Prop)
		}
		if !IsSafeQueryValue(q.Value) {
			return fmt.Errorf("规则 %d 查询 %d: 查询值 %q 非法", ruleIdx, qIdx, q.Value)
		}
		if q.Condition != "" {
			return fmt.Errorf("规则 %d 查询 %d: kind=%s 不接受 condition", ruleIdx, qIdx, q.Kind)
		}
	default:
		return fmt.Errorf("规则 %d 查询 %d: 未知 kind %q（size/theme/local）", ruleIdx, qIdx, q.Kind)
	}
	if len(q.Decls) == 0 {
		return fmt.Errorf("规则 %d 查询 %d: 缺少 decls", ruleIdx, qIdx)
	}
	return validateDecls(ruleIdx, "queries["+q.Kind+"]", q.Decls)
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
