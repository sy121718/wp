package templates

import (
	"strings"
	"testing"

	"github.com/CloudyKit/jet/v6"
)

// TestRawPipeSafeWriter raw 管道必须原样输出 HTML（不转义）。
func TestRawPipeSafeWriter(t *testing.T) {
	loader := jet.NewInMemLoader()
	set := jet.NewSet(loader, jet.WithTemplateNameExtensions([]string{"", ".jet"}))
	injectGlobals(set)
	loader.Set("t.jet", `<div>{{ v | raw }}</div>`)
	tpl, terr := set.GetTemplate("t.jet")
	if terr != nil {
		t.Fatalf("取模板失败: %v", terr)
	}
	var sb strings.Builder
	vars := jet.VarMap{}.Set("v", `<li class="x">`)
	if err := tpl.Execute(&sb, vars, nil); err != nil {
		t.Fatalf("渲染失败: %v", err)
	}
	if got := sb.String(); got != `<div><li class="x"></div>` {
		t.Fatalf("raw 应原样输出，实际: %s", got)
	}
}
