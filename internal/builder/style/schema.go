package style

import (
	"fmt"
	"strings"

	"go_wp/internal/builder/core"
)

type Schema struct {
	Rules []Rule `json:"rules"`
}

type Rule struct {
	Target string `json:"target,omitempty"`

	Pseudo string `json:"pseudo,omitempty"`

	When string `json:"when,omitempty"`

	Decls [][2]string `json:"decls,omitempty"`

	Vars []VarBinding `json:"vars,omitempty"`

	Bindings []Binding `json:"bindings,omitempty"`

	Queries []Query `json:"queries,omitempty"`

	Breakpoints map[string][][2]string `json:"breakpoints,omitempty"`
}

type VarBinding struct {
	Name string `json:"name"`

	From string `json:"from"`
}

type Query struct {
	Kind string `json:"kind"`

	Condition string `json:"condition,omitempty"`

	Container string `json:"container,omitempty"`

	Prop string `json:"prop,omitempty"`

	Value string `json:"value,omitempty"`

	Decls [][2]string `json:"decls,omitempty"`
}

type Binding struct {
	Prop string `json:"prop"`

	From string `json:"from"`

	Prefix string `json:"prefix,omitempty"`
	Suffix string `json:"suffix,omitempty"`
}

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
