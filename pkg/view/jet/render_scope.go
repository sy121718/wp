package jet

import (
	"errors"

	"github.com/CloudyKit/jet/v6"
)

// RenderScope 是「这一次请求的模板取词上下文」。
//
// 它只装语言与取词函数两类东西，不装业务数据：业务数据仍走控制器交来的 data / 根对象。
// 放这里的理由是可见性 —— t / tf / tn 必须对 base 模板、include 进去的片段、
// 以及传给 include 的裸 struct 都可见，而 Jet 的 include 只沿 scope 的 parent 链解析变量
// （eval.go:186-215 的 resolve）：顶层 map 里的键能被继承，struct 的字段不会变成变量名。
// 把取词函数放进执行期 VarMap，它们就在顶层变量表里，任意 include 深度都继承得到。
//
// 值类型、不持有 gin.Context：取词函数是按请求语言生成的闭包，
// 放进 VarMap（每次 Execute 一份）才能保证每个请求用自己的语言。
type RenderScope struct {
	// Lang 是这一次请求的界面语言。它作为顶层变量 lang 出现，
	// 与根对象自身的字段（.Lang / .lang）是两回事，不覆盖调用方数据。
	Lang string
	// Translate 对应模板的 t：取词，缺词回退 fallback。
	Translate func(key, fallback string) string
	// TranslateArgs 对应模板的 tf：取词后填位置参数。
	TranslateArgs func(key, fallback string, args []string) string
	// TranslateNamed 对应模板的 tn：取词后填具名占位符。
	TranslateNamed func(key, fallback string, args map[string]any) string
}

// VarMap 把这份上下文转成 Jet 的执行期变量表。
//
// 键名必须与模板里已经在用的形态一致（t / tf / tn / lang），
// 否则模板要么报「变量不存在」，要么静默渲染成空串。
func (s RenderScope) VarMap() jet.VarMap {
	vars := jet.VarMap{}
	if s.Translate != nil {
		vars.Set("t", s.Translate)
	}
	if s.TranslateArgs != nil {
		vars.Set("tf", s.TranslateArgs)
	}
	if s.TranslateNamed != nil {
		vars.Set("tn", s.TranslateNamed)
	}
	if s.Lang != "" {
		vars.Set("lang", s.Lang)
	}
	return vars
}

// TemplateRenderError 表示「模板这一层渲染失败」，与业务错误区分开。
//
// 它不是业务声明，所以没有 key、没有状态码：模板炸了就是 500。
// 它携带原始 err 与模板名，只给日志用 —— JSON 出口只出归口文案，
// 既不把模板路径交给前端，也不把 Jet 的报错原文上屏。
type TemplateRenderError struct {
	Template string
	Err      error
}

func (e *TemplateRenderError) Error() string {
	if e == nil {
		return ""
	}
	name := e.Template
	if name == "" {
		name = "unknown"
	}
	if e.Err == nil {
		return "template render failed: " + name
	}
	return "template render failed: " + name + ": " + e.Err.Error()
}

func (e *TemplateRenderError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

// AsTemplateRenderError 供出口侧判定：只有这个类型走模板失败分支。
func AsTemplateRenderError(err error) (*TemplateRenderError, bool) {
	var target *TemplateRenderError
	if errors.As(err, &target) {
		return target, true
	}
	return nil, false
}
