package templates

import (
	"bytes"
	"testing"

	"github.com/CloudyKit/jet/v6"
)

type jetTreeNode struct {
	Name     string
	Children []jetTreeNode
}

func TestJetTemplateAccumulator(t *testing.T) {
	vars := jet.VarMap{}
	vars.Set("rows", []map[string]any{{"n": 3}, {"n": 5}, {"n": 7}})
	got := mustRender(t, `{{sum := 0}}{{range _, r := rows}}{{sum = sum + r.n}}{{end}}S={{sum}}`, vars)
	if got != "S=15" {
		t.Fatalf("模板内累加失效（Jet 行为变了）：%q", got)
	}
}

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
