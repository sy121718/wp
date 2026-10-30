package css

// seo_audit_hidden_contract_test.go — SEO 体检结果区「四档互斥」的样式契约（本条为回归）。
//
// 现场（真实浏览器，/admin/seo）：clean / empty / failed 三档节点在初始页面里
// hidden 属性为 true，但 computed display 仍是 flex。原因是这三档本身是带 display
// 的组件 —— .empty-state 在 theme.css 里 display:flex、.badge 在 ui.css 里
// display:inline-flex。作者样式的来源优先于 UA 样式，而非这两个类选择器的
// specificity 更高，因此必须显式恢复作用域内的 hidden 语义。
//
// 为什么这条必须由测试钉住：Go 侧只能断言 hidden 属性写没写，页面照样 200，
// 「hidden=true 却看得见」在 DOM 断言里是绿的。真实判据是「hidden 时没有任何
// 作者规则能让它显示」，所以这里做静态判据。
//
// 判据边界：只查 [data-seo-audit-scope] 作用域内的 hidden 复位，不要求全局
// 改 hidden 语义（本仓有 20 余处组件自带 [hidden]{display:none}，那是各组件自负其责）。

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// readCSSDir 读取 admin 样式目录下所有 .css 的源码（不含子目录与测试之外的资源）。
func readCSSDir(t *testing.T) map[string]string {
	t.Helper()
	out := map[string]string{}
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("读样式目录失败: %v", err)
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".css") {
			continue
		}
		b, err := os.ReadFile(filepath.Join(".", e.Name()))
		if err != nil {
			t.Fatalf("读 %s 失败: %v", e.Name(), err)
		}
		out[e.Name()] = string(b)
	}
	if len(out) == 0 {
		t.Fatal("样式目录下没有 .css 文件（相对路径不对？）")
	}
	return out
}

// seoScopeHiddenRe 匹配「作用域复位 hidden」的规则：
// 选择器同时含 [data-seo-audit-scope] 与 [hidden]，声明里有 display: none。
var seoScopeHiddenRe = regexp.MustCompile(`(?s)\[data-seo-audit-scope\][^{]*\[hidden\][^{]*\{[^}]*display\s*:\s*none`)

// TestSEOAuditScopeResetsHidden 体检作用域内必须存在 [hidden]{display:none} 的复位规则。
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

// TestSEOAuditScopeRuleIsScoped 复位规则只作用于体检作用域，不得写成全局 [hidden] 或
// 影响本页其它区块（.stack / .card 之外）。
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
	// 选择器部分（第一个 { 之前）必须同时带作用域锚点，且不得出现通配符式的裸露 [hidden]。
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
