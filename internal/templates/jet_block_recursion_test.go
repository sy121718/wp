package templates

// jet_block_recursion_test.go — Jet 的两条方言边界（实测，不是猜）。
//
// 决定「复杂计算交给模板」这件事能做到什么程度的，是这两条：
//
//  1. **模板内变量赋值能影响外层作用域** —— `{{sum := 0}}{{range …}}{{sum = sum + r.n}}{{end}}`
//     真的累加出 15。所以「区间求和 / 计数 / 按条件挑一个值」这类聚合可以在模板里做，
//     不必先由 Go 算好再传下来。
//
//  2. **块递归只在「块定义在另一个文件、再 import」时成立** —— 同文件里定义块再 yield，
//     连**单次调用**都报 `missing name for block parameter 'x'`（不是递归的问题，是 yield
//     同文件块本身不成立）。所以树形渲染（导航树、分类树、菜单树）要用递归块时，
//     必须把块放进片段文件再 import。
//
// 这两条是「模板能不能承担计算」的判据依据，所以固化成测试：Jet 升级后行为一变就红，
// 而不是等某个页面的树渲染悄悄少一层。

import (
	"bytes"
	"testing"

	"github.com/CloudyKit/jet/v6"
)

type jetTreeNode struct {
	Name     string
	Children []jetTreeNode
}

// TestJetTemplateAccumulator 模板内累加影响外层作用域（聚合可以留在模板里）。
func TestJetTemplateAccumulator(t *testing.T) {
	vars := jet.VarMap{}
	vars.Set("rows", []map[string]any{{"n": 3}, {"n": 5}, {"n": 7}})
	got := mustRender(t, `{{sum := 0}}{{range _, r := rows}}{{sum = sum + r.n}}{{end}}S={{sum}}`, vars)
	if got != "S=15" {
		t.Fatalf("模板内累加失效（Jet 行为变了）：%q", got)
	}
}

// TestJetRecursiveBlockRequiresImport 递归块必须经 import 引入。
func TestJetRecursiveBlockRequiresImport(t *testing.T) {
	tree := jetTreeNode{Name: "root", Children: []jetTreeNode{
		{Name: "a", Children: []jetTreeNode{{Name: "a1"}, {Name: "a2"}}},
		{Name: "b"},
	}}

	t.Run("经 import 成立", func(t *testing.T) {
		set := memSet(t, map[string]string{
			"partials/tree.html": `{{block node(n)}}{{n.Name}}{{if len(n.Children) > 0}}({{range _, c := n.Children}}{{yield node(n=c)}}{{end}}){{end}}{{end}}`,
			"probe.html":         `{{import "partials/tree.html"}}{{yield node(n=tree)}}`,
		})
		tmpl, err := set.GetTemplate("probe.html")
		if err != nil {
			t.Fatalf("取模板失败: %v", err)
		}
		vars := jet.VarMap{}
		vars.Set("tree", tree)
		var buf bytes.Buffer
		if err := tmpl.Execute(&buf, vars, nil); err != nil {
			t.Fatalf("渲染失败: %v", err)
		}
		if got := buf.String(); got != "root(a(a1a2)b)" {
			t.Fatalf("递归块产物不对：%q", got)
		}
	})

	t.Run("同文件定义 + yield 不成立", func(t *testing.T) {
		// 这条断言「不成立」：Jet 的报错是 missing name for block parameter，
		// 看起来像参数没命名（实际上命名了），根因是同文件块不能 yield —— 报错文本会误导人。
		// 一旦 Jet 支持了这条，测试会红，那时可以把树渲染直接写进页面文件。
		_, err := renderGlobal(t,
			`{{block n1(x)}}{{x.Name}}{{end}}{{yield n1(x=tree)}}`, func() jet.VarMap {
				vars := jet.VarMap{}
				vars.Set("tree", tree)
				return vars
			}())
		if err == nil {
			t.Fatal("同文件块 yield 现在成立了：本条判据需要更新（可以把递归块写回页面文件）")
		}
	})
}
