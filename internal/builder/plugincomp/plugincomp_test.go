package plugincomp

import (
	json "go_wp/pkg/jsonx"
	"strings"
	"testing"

	"go_wp/internal/builder/core"
)

const legalManifest = `{
  "id": "marketing",
  "name": "营销组件",
  "version": "1.2.0",
  "components": [{
    "name": "campaign_card",
    "label": "活动卡片",
    "template": "campaign_card.jet",
    "props": {
      "title": {"kind": "text", "label": "标题", "default": "夏季大促"},
      "level": {"kind": "select", "label": "等级", "options": ["gold","silver"], "default": "gold"}
    },
    "styles": {"rules": [{"decls": [["display","block"]]}]}
  }]
}`

func mustParse(t *testing.T, raw string) *Manifest {
	t.Helper()
	m, err := ParseManifest([]byte(raw))
	if err != nil {
		t.Fatalf("manifest 解析失败: %v", err)
	}
	return m
}

func TestParseLegal(t *testing.T) {
	m := mustParse(t, legalManifest)
	specs := BuildSpecs(m)
	spec, ok := specs["plugin.marketing.campaign_card"]
	if !ok {
		t.Fatalf("缺规格 plugin.marketing.campaign_card")
	}
	if spec.Label != "活动卡片" || spec.Template != "plugin/marketing/campaign_card.jet" {
		t.Fatalf("规格字段错误: %+v", spec)
	}
	if len(spec.Props) != 2 || spec.Props["title"].Default != "夏季大促" {
		t.Fatalf("props 规格错误: %+v", spec.Props)
	}
	if spec.CompileStyles == nil {
		t.Fatalf("含 styles 的组件应有 CompileStyles 闭包")
	}
}

func TestValidateRejects(t *testing.T) {
	cases := []struct {
		name string
		raw  string
	}{
		{"ID 大写非法", `{"id":"Marketing","name":"x","version":"1.0.0","components":[{"name":"c","label":"c","template":"c.jet"}]}`},
		{"ID 含点", `{"id":"a.b","name":"x","version":"1.0.0","components":[{"name":"c","label":"c","template":"c.jet"}]}`},
		{"版本非法", `{"id":"mkt","name":"x","version":"v1","components":[{"name":"c","label":"c","template":"c.jet"}]}`},
		{"无组件", `{"id":"mkt","name":"x","version":"1.0.0","components":[]}`},
		{"组件名非法", `{"id":"mkt","name":"x","version":"1.0.0","components":[{"name":"Bad","label":"c","template":"c.jet"}]}`},
		{"模板名非 jet", `{"id":"mkt","name":"x","version":"1.0.0","components":[{"name":"c","label":"c","template":"c.html"}]}`},
		{"select 无选项", `{"id":"mkt","name":"x","version":"1.0.0","components":[{"name":"c","label":"c","template":"c.jet","props":{"l":{"kind":"select"}}}]}`},
		{"控件类型越权", `{"id":"mkt","name":"x","version":"1.0.0","components":[{"name":"c","label":"c","template":"c.jet","props":{"x":{"kind":"sql"}}}]}`},
		{"样式非法属性", `{"id":"mkt","name":"x","version":"1.0.0","components":[{"name":"c","label":"c","template":"c.jet","styles":{"rules":[{"decls":[["behavior","url(#x)"]]}]}}]}`},
		{"样式选择器注入", `{"id":"mkt","name":"x","version":"1.0.0","components":[{"name":"c","label":"c","template":"c.jet","styles":{"rules":[{"target":".a, .evil"}]}}]}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := ParseManifest([]byte(tc.raw)); err == nil {
				t.Fatalf("应拒绝非法 manifest")
			}
		})
	}
}

func TestInspectorSchemaDeterministic(t *testing.T) {
	m := mustParse(t, legalManifest)
	a := InspectorSchema(m)
	b := InspectorSchema(m)
	aj, _ := json.Marshal(a)
	bj, _ := json.Marshal(b)
	if string(aj) != string(bj) {
		t.Fatalf("InspectorSchema 不确定:\n%s\n%s", aj, bj)
	}
	schema := a["plugin.marketing.campaign_card"]
	var controls []map[string]any
	if err := json.Unmarshal(schema, &controls); err != nil {
		t.Fatalf("schema 解析失败: %v", err)
	}
	if len(controls) != 2 {
		t.Fatalf("控件数错误: %d", len(controls))
	}

	if controls[0]["key"] != "level" || controls[1]["key"] != "title" {
		t.Fatalf("控件应按键字典序: %v", controls)
	}

	for _, c := range controls {
		if c["section"] != "content" {
			t.Fatalf("非样式控件应进 content: %v", c)
		}
	}
}

func TestSpecCompileStyles(t *testing.T) {
	m := mustParse(t, legalManifest)
	spec := BuildSpecs(m)["plugin.marketing.campaign_card"]
	b := &core.CSSBuckets{}
	if err := spec.CompileStyles("card-1", map[string]any{}, b); err != nil {
		t.Fatalf("CompileStyles: %v", err)
	}
	if got := b.String(); got == "" || !strings.Contains(got, "display: block") {
		t.Fatalf("样式未编译进桶: %s", got)
	}
}
