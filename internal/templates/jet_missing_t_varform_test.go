package templates

import (
	"strings"
	"testing"
)

// TestProbeMissingTVariableForm 探测「变量形态 + 缺 t」的真实行为。
//
// 背景：既有的 TestJetTranslateFuncPath 已证实**直调形态** `{{ .["t"]("k","兜底") }}` 在缺 t 时
// 渲染空串且不报错。但模板的事实主流是**变量形态** `{{tr := .["t"]}}` + `{{tr(...)}}`
//（Jet 不允许 `.["t"](...)` 出现在赋值右侧 / 单目表达式里，见 internal/templates/CLAUDE.md）。
//
// 两者在 Jet 里的求值路径不同：直调形态的 base 是 chain 索引，变量形态的 base 是变量标识符，
// 后者会走到 evalCallExpression 里 `baseExpr.Kind() != reflect.Func → errorf` 那一支。
// 因此「缺 t 静默」这条结论必须对两种形态分别确认，不能从一种推广到另一种。
func TestProbeMissingTVariableForm(t *testing.T) {
	set := memSet(t, map[string]string{
		"varform.html": `{{tr := .["t"]}}V[{{tr("k", "兜底")}}]`,
	})

	out, err := render(t, set, "varform.html", map[string]any{})
	t.Logf("变量形态缺 t → err=%v out=%q", err, out)

	if err != nil {
		// 报错路径：门禁声明为「运行时中断」而非「静默空文案」。
		if !strings.Contains(err.Error(), "reflect") && !strings.Contains(err.Error(), "func") {
			t.Logf("报错文本未含 reflect/func 关键词，实录: %v", err)
		}
		return
	}
	if strings.Contains(out, "兜底") {
		t.Fatalf("缺 t 却输出了 fallback（说明 base 取值不是无效值）：%q", out)
	}
	if out != "V[]" {
		t.Logf("静默路径但输出形态与预期不同（预期 V[]）：%q", out)
	}
}
