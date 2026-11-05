package workbenchhttp

import (
	json "go_wp/pkg/jsonx"
	"sort"
	"testing"

	"go_wp/internal/builder"
	"go_wp/pkg/builder/core"
)

var clientSupportedKinds = map[string]bool{

	"string": true, "safe": true, "url": true, "regex": true,
	"text": true, "textarea": true, "int": true, "slider": true, "number": true,
	"bool": true, "select": true, "classes": true, "cssdecls": true,

	"color": true, "spacing": true, "margin": true, "rtext": true,
	"dimension": true, "media": true, "mediaList": true, "boxspacing": true,

	"collectionfield": true, "bindingfield": true,

	"richtext": true,

	"entityref": true,

	"multientityref": true,
	"rangelist":      true,
}

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
