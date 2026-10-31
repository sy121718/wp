package templates

import (
	"testing"

	"github.com/CloudyKit/jet/v6"
)

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

func TestJetCallFuncFromData(t *testing.T) {
	vars := jet.VarMap{}
	vars.Set("label", func(code string) string { return "国家:" + code })
	if got := mustRender(t, `{{ label("CN") }}`, vars); got != "国家:CN" {
		t.Fatalf("data 里的函数调用失效（Jet 行为变了）：%q", got)
	}
}
