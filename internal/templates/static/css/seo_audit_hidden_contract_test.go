package css

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// readCSSDir 读**所有样式源**：公共 UI 资产（pkg/view/ui/assets/css）与应用专用
// 样式（本目录）。资产搬迁后只看本目录会漏掉 theme.css，让这条断言变成永远读不到
// 规则的空检查 —— 它守的是「作用域复位存在」，不是「它在本目录」。
func readCSSDir(t *testing.T) map[string]string {
	t.Helper()
	out := map[string]string{}
	for _, dir := range []string{".", "../../../../pkg/view/ui/assets/css"} {
		entries, err := os.ReadDir(dir)
		if err != nil {
			t.Fatalf("读样式目录 %s 失败: %v", dir, err)
		}
		for _, e := range entries {
			if e.IsDir() || !strings.HasSuffix(e.Name(), ".css") {
				continue
			}
			b, err := os.ReadFile(filepath.Join(dir, e.Name()))
			if err != nil {
				t.Fatalf("读 %s 失败: %v", filepath.Join(dir, e.Name()), err)
			}
			out[filepath.Join(dir, e.Name())] = string(b)
		}
	}
	if len(out) == 0 {
		t.Fatal("样式目录下没有 .css 文件（相对路径不对？）")
	}
	return out
}

var seoScopeHiddenRe = regexp.MustCompile(`(?s)\[data-seo-audit-scope\][^{]*\[hidden\][^{]*\{[^}]*display\s*:\s*none`)

func TestSEOAuditScopeResetsHidden(t *testing.T) {
	for _, src := range readCSSDir(t) {
		if seoScopeHiddenRe.MatchString(src) {
			return
		}
	}
	t.Fatalf("任何样式文件都没有 [data-seo-audit-scope] 下的 [hidden]{display:none} 复位：\n" +
		".empty-state(display:flex) 与 .badge(display:inline-flex) 会压过 UA 的 [hidden]，" +
		"clean / empty / failed 三档在初始页面里 hidden=true 却仍然可见（四档不再互斥）")
}

func TestSEOAuditScopeRuleIsScoped(t *testing.T) {
	found := ""
	for _, src := range readCSSDir(t) {
		if m := seoScopeHiddenRe.FindString(src); m != "" {
			found = m
			break
		}
	}
	if found == "" {
		t.Fatal("没有找到作用域复位规则（见上一个用例）")
	}

	sel := strings.TrimSpace(strings.SplitN(found, "{", 2)[0])
	if !strings.Contains(sel, "[data-seo-audit-scope]") {
		t.Fatalf("复位规则没有作用域锚点，会全局改变 hidden 语义: %q", sel)
	}
	for _, bad := range []string{"*[hidden]", "html [hidden]", "body [hidden]"} {
		if strings.Contains(sel, bad) {
			t.Fatalf("复位规则过宽（%s），不得全局改变 hidden 语义: %q", bad, sel)
		}
	}
}
