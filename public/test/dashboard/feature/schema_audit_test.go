package feature

// schema_audit_test.go — 全组件 schema 审计（客户端渲染能力覆盖检查）。
//
// 目的：服务端把 schema 渲染成面板/增强槽，客户端按 kind 分派控件。
// 若某个组件声明了客户端不认识的 kind，该字段会掉进默认分支（渲染成普通文本框
// 或空白），属于静默缺陷。本测试把「未被任何渲染分支覆盖的 kind」直接暴露出来。

import (
	"encoding/json"
	"sort"
	"testing"

	"go_wp/internal/builder"
	"go_wp/internal/builder/core"
)

// clientSupportedKinds 客户端/服务端已实现的控件类型（见 inspector_handle.go 与 inspector.js）。
var clientSupportedKinds = map[string]bool{
	// 服务端直出 HTML
	"string": true, "safe": true, "url": true, "regex": true,
	"text": true, "textarea": true, "int": true, "slider": true, "number": true,
	"bool": true, "select": true, "classes": true, "cssdecls": true,
	// 服务端输出 slot，客户端用既有控件填充
	"color": true, "spacing": true, "margin": true, "rtext": true,
	"dimension": true, "media": true, "mediaList": true, "boxspacing": true,
	// 集合字段映射 / 内容字段绑定（core.cardstack 字段映射、item.<字段>）：
	// 选项来自后端字段白名单，服务端输出 slot、客户端渲染成下拉。
	"collectionfield": true, "bindingfield": true,
	// richtext：服务端输出 slot，客户端 richTextField（Trix）填充；
	// core.text 的 mode=plaintext 由 isPlainTextMode 回退多行输入。
	"richtext": true,
}

// TestSchemaKindsCovered 每个组件的每个 ct 控件 kind 都必须被渲染分支覆盖。
func TestSchemaKindsCovered(t *testing.T) {
	schemas, err := builder.ComponentSchemas()
	if err != nil {
		t.Fatalf("生成 schema 失败: %v", err)
	}
	if len(schemas) == 0 {
		t.Fatal("schema 为空")
	}
	type item struct {
		Key  string `json:"key"`
		Kind string `json:"kind"`
	}
	unknown := map[string][]string{}
	empty := []string{}
	for _, typeName := range core.Types() {
		raw, ok := schemas[typeName]
		if !ok {
			empty = append(empty, typeName+"(无 schema)")
			continue
		}
		var items []item
		if err = json.Unmarshal(raw, &items); err != nil {
			t.Fatalf("%s schema 解析失败: %v", typeName, err)
		}
		if len(items) == 0 {
			empty = append(empty, typeName+"(0 字段)")
		}
		for _, it := range items {
			if !clientSupportedKinds[it.Kind] {
				unknown[it.Kind] = append(unknown[it.Kind], typeName+"."+it.Key)
			}
		}
	}
	if len(empty) > 0 {
		t.Errorf("以下组件没有可用 schema 字段（检查器将为空）: %v", empty)
	}
	if len(unknown) > 0 {
		kinds := make([]string, 0, len(unknown))
		for k := range unknown {
			kinds = append(kinds, k)
		}
		sort.Strings(kinds)
		for _, k := range kinds {
			t.Errorf("未覆盖的控件 kind %q 出现在: %v", k, unknown[k])
		}
	}
	t.Logf("已审计组件 %d 个", len(schemas))
}
