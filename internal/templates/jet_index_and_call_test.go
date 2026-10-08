package templates

// jet_index_and_call_test.go — 两条被订单页依赖的 Jet 方言能力（实测固化）。
//
// 订单页要在模板里画「各状态各有多少单」的徽章行，并要把详情里的国家代码翻成显示名。
// 这两件事分别依赖下面两条能力，都不是「显然成立」的写法，所以钉住：
//
//  1. **map 可以用变量做键**：`{{ counts[key] }}` 取得到值；键不存在 / map 为 nil 时
//     输出空串且不报错（与字面量键 `.["x"]` 的行为一致）。这条让「按状态取计数」不必
//     先在 Go 里把徽章行拼成切片。
//  2. **data 里的函数值可以直接调用**：`{{ label("CN") }}`。这条让「按当前语言把国家代码
//     换成显示名」这种需要请求上下文的解析留在数据里，而不必把每个字段都预先翻好。
//
// 两条都是「删掉 Go 侧预拼视图」的前提：它们不成立时，页面只能退回「Go 拼好一切」。
// Jet 升级后行为一变就红，而不是等页面上少一排徽章。

import (
	"testing"

	"github.com/CloudyKit/jet/v6"
)

// TestJetMapIndexByVariableKey map 的键可以是变量；缺键与 nil map 都给空串。
func TestJetMapIndexByVariableKey(t *testing.T) {
	vars := jet.VarMap{}
	vars.Set("counts", map[string]int64{"pending": 3, "paid": 5})
	vars.Set("key", "pending")
	vars.Set("nilCounts", map[string]int64(nil))

	cases := []struct {
		expr   string
		expect string
	}{
		{`{{ counts[key] }}`, "3"},
		{`{{ counts["paid"] }}`, "5"},
		{`{{ counts["nope"] }}`, ""},
		{`{{ nilCounts[key] }}`, ""},
		// 索引结果要能参与比较（徽章行据此决定高亮哪一个）。
		{`{{ counts[key] > 0 ? "有" : "无" }}`, "有"},
	}
	for _, c := range cases {
		t.Run(c.expr, func(t *testing.T) {
			if got := mustRender(t, c.expr, vars); got != c.expect {
				t.Fatalf("期望 %q，实际 %q", c.expect, got)
			}
		})
	}
}

// TestJetCallFuncFromData data 里的函数值可以直接调用（用于请求上下文的解析函数）。
func TestJetCallFuncFromData(t *testing.T) {
	vars := jet.VarMap{}
	vars.Set("label", func(code string) string { return "国家:" + code })
	if got := mustRender(t, `{{ label("CN") }}`, vars); got != "国家:CN" {
		t.Fatalf("data 里的函数调用失效（Jet 行为变了）：%q", got)
	}
}
