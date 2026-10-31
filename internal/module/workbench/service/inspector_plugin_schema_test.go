package service

import (
	"context"
	"slices"
	"testing"

	"go_wp/internal/builder/source"
	plugincontract "go_wp/internal/module/plugin/contract"
)

type inspectorPluginStub struct {
	plugincontract.PluginService
	asm *plugincontract.Assembly
}

func (s *inspectorPluginStub) EnabledAssembly(context.Context) (*plugincontract.Assembly, error) {
	return s.asm, nil
}

const pluginSchemaJSON = `[{"key":"demoNote","kind":"text","label":"示意字段","section":"content"}]`

func TestInspectorSectionsSeesPluginSchema(t *testing.T) {
	svc := New(nil, nil, nil, nil, nil, nil)
	svc.SetPlugins(&inspectorPluginStub{asm: &plugincontract.Assembly{

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

func TestInspectorSectionsIgnoresPluginWhenNotAssembled(t *testing.T) {
	svc := New(nil, nil, nil, nil, nil, nil)

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
