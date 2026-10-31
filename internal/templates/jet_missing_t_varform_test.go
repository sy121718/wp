package templates

import (
	"strings"
	"testing"
)

func TestProbeMissingTVariableForm(t *testing.T) {
	set := memSet(t, map[string]string{
		"varform.html": `{{tr := .["t"]}}V[{{tr("k", "兜底")}}]`,
	})

	out, err := render(t, set, "varform.html", map[string]any{})
	t.Logf("变量形态缺 t → err=%v out=%q", err, out)

	if err != nil {

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
