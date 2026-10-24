package service

// inspector_plugin_schema_test.go — 检查器面板必须看得见**插件组件**的字段。
//
// 守的缺陷：面板装配链（InspectorSections）取 schema 的两个来源 —— 构建内核的内置组件、
// 启用插件自带的 InspectorSchemas（docs/06 §5）。此前 canvas 入口把插件 schema 并进
// builder.ComponentSchemas() 的**进程级缓存**，面板端点靠读这张被污染的表才拿到插件字段。
// 缓存改成返回副本（并发安全）后，面板端点若继续直接读内置表，插件组件就会被静默丢掉：
// 拖得进画布、选中后一个字段都没有，不报错、200、日志干净 —— 用户只会认为插件坏了。
//
// 所以这里钉的不是「合并代码存在」，而是「面板真的拿到了插件字段」。

import (
	"context"
	"slices"
	"testing"

	"go_wp/internal/builder/source"
	plugincontract "go_wp/internal/module/plugin/contract"
)

// inspectorPluginStub 只提供 EnabledAssembly（面板取数只用这一个方法）。
type inspectorPluginStub struct {
	plugincontract.PluginService
	asm *plugincontract.Assembly
}

func (s *inspectorPluginStub) EnabledAssembly(context.Context) (*plugincontract.Assembly, error) {
	return s.asm, nil
}

const pluginSchemaJSON = `[{"key":"demoNote","kind":"text","label":"示意字段","section":"content"}]`

// TestInspectorSectionsSeesPluginSchema 插件组件的节点能装出非空面板。
func TestInspectorSectionsSeesPluginSchema(t *testing.T) {
	svc := New(nil, nil, nil, nil, nil, nil)
	svc.SetPlugins(&inspectorPluginStub{asm: &plugincontract.Assembly{
		// Specs 必须非空：PluginAssembly 用 len(PluginFS)>0 || len(Specs)>0 判「素材是不是空的」。
		Specs: map[string]*source.PluginComponentSpec{"plugin.demo.card": {}},
		InspectorSchemas: map[string][]byte{
			"plugin.demo.card": []byte(pluginSchemaJSON),
		},
	}})

	node := &DocNode{ID: "n1", Type: "plugin.demo.card"}
	sections, err := svc.InspectorSections(context.Background(), node, "", "project-1", func(k string) string { return k })
	if err != nil {
		t.Fatalf("装配插件组件的检查器面板失败: %v", err)
	}
	if len(sections) == 0 {
		t.Fatal("插件组件的检查器面板是空的：面板取 schema 时没带上插件来源 —— " +
			"用户拖进画布后选中它，一个可编辑字段都没有（不报错，最容易被当成插件坏了）")
	}
	// 字段真的进了面板（而不只是分组壳非空）：按 key 逐个核对。
	var keys []string
	for _, sec := range sections {
		for _, f := range sec.Fields {
			keys = append(keys, f.Key)
		}
	}
	if !slices.Contains(keys, "demoNote") {
		t.Fatalf("面板字段里没有插件 schema 声明的 demoNote（实际 %v）", keys)
	}
}

// TestInspectorSectionsIgnoresPluginWhenNotAssembled 未装配插件能力时面板照常工作（内置组件）。
func TestInspectorSectionsIgnoresPluginWhenNotAssembled(t *testing.T) {
	svc := New(nil, nil, nil, nil, nil, nil) // plugins 为 nil

	// 内置组件取一个一定有 schema 的类型：从内置表里随便挑一个。
	schemas, err := svc.InspectorSchemas(context.Background())
	if err != nil {
		t.Fatalf("取内置 schema 失败: %v", err)
	}
	if len(schemas) == 0 {
		t.Fatal("内置 schema 是空的")
	}
	var builtin string
	for typeName, raw := range schemas {
		if len(raw) > 2 && raw[0] == '[' {
			builtin = typeName
			break
		}
	}
	if builtin == "" {
		t.Skip("内置 schema 里没有可用的数组型条目")
	}
	if _, err = svc.InspectorSections(context.Background(), &DocNode{ID: "n1", Type: builtin}, "",
		"project-1", func(k string) string { return k }); err != nil {
		t.Fatalf("未装配插件时装配内置组件面板失败: %v", err)
	}
}
