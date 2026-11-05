package pipeline

import (
	"context"
	"errors"
	"strings"
	"testing"

	"go_wp/internal/builder"
	"go_wp/pkg/builder/core"
)

type stubPort struct {
	docs map[string][]byte
	errs map[string]error
}

func (p stubPort) ResolveStructureDocument(_ context.Context, _, templateID string) ([]byte, error) {
	if err, ok := p.errs[templateID]; ok {
		return nil, err
	}
	if doc, ok := p.docs[templateID]; ok {
		return doc, nil
	}
	return nil, errors.New("模板不存在")
}

type stubBlocks struct {
	roots map[string][]*core.Node
}

func (b stubBlocks) ResolveBlockRoot(blockID string) ([]*core.Node, error) {
	if nodes, ok := b.roots[blockID]; ok {
		return nodes, nil
	}
	return nil, errors.New("块不可用")
}

func templateDoc(id, text string) []byte {
	return []byte(`{"settings":{"layout":{"mode":"full"}},"root":[{"type":"core.text","id":"` + id + `","props":{"text":"` + text + `"}}]}`)
}

func emptyTemplateDoc() []byte {
	return []byte(`{"settings":{"layout":{"mode":"full"}},"root":[]}`)
}

func TestStructureSlotsPublishFailsOnUnavailableBoundTemplate(t *testing.T) {
	in := StructureSlotInput{
		Port:      stubPort{},
		ProjectID: "p1",
		Structure: builder.StructureBindings{HeaderTemplateID: "tpl-missing"},
		Mode:      builder.CompileModePublish,
	}
	_, err := BuildStructureSlots(context.Background(), in)
	if err == nil {
		t.Fatal("发布模式下绑定的结构模板拿不到时必须失败（旧行为：静默回退 / 不渲染）")
	}
	if !strings.Contains(err.Error(), "header") || !strings.Contains(err.Error(), "tpl-missing") {
		t.Fatalf("失败原因要能定位到槽位与模板，实际: %v", err)
	}
}

func TestStructureSlotsPreviewDegradesUnavailableTemplateWithAttribution(t *testing.T) {
	ctx := context.Background()

	blocks := stubBlocks{roots: map[string][]*core.Node{"blk-1": nil}}
	diags := builder.NewDegradeCollector()
	res, err := BuildStructureSlots(ctx, StructureSlotInput{
		Port: stubPort{}, ProjectID: "p1",
		Structure:   builder.StructureBindings{HeaderTemplateID: "tpl-missing", HeaderBlockID: "blk-1"},
		Inner:       blocks,
		Mode:        builder.CompileModePreview,
		Diagnostics: diags,
	})
	if err != nil {
		t.Fatalf("预览模式不该失败: %v", err)
	}
	if len(res.Slots) != 1 || res.Slots[0].BlockID != "blk-1" {
		t.Fatalf("预览应回退到块绑定，实际: %+v", res.Slots)
	}
	items := diags.Items()
	if len(items) != 1 || items[0].Slot != "header" || items[0].Kind != builder.DegradeKindTemplate ||
		items[0].RefID != "tpl-missing" || items[0].Reason != core.RefReasonTemplateUnavailable {
		t.Fatalf("预览降级必须留归因诊断，实际: %+v", items)
	}
	for _, d := range res.Deps {
		if d.Kind == DepKindContentTemplate {
			t.Fatalf("回退掉的模板不该被登记成依赖: %+v", res.Deps)
		}
	}

	diags = builder.NewDegradeCollector()
	res, err = BuildStructureSlots(ctx, StructureSlotInput{
		Port: stubPort{}, ProjectID: "p1",
		Structure:   builder.StructureBindings{HeaderTemplateID: "tpl-missing"},
		Inner:       blocks,
		Mode:        builder.CompileModePreview,
		Diagnostics: diags,
	})
	if err != nil {
		t.Fatalf("预览模式不该失败: %v", err)
	}
	if len(res.Slots) != 1 || !builder.IsStructureTemplateRef(res.Slots[0].BlockID) {
		t.Fatalf("无块可回退时应保留模板虚拟引用的槽位占位，实际: %+v", res.Slots)
	}
	_, rerr := res.Resolver.ResolveBlockRoot(res.Slots[0].BlockID)
	if got := core.RefDegradeReason(rerr, ""); got != core.RefReasonTemplateUnavailable {
		t.Fatalf("虚拟引用失败必须带模板归因码，实际 %q（%v）", got, rerr)
	}
	if len(res.Deps) != 0 {
		t.Fatalf("未展开的模板不该登记依赖，实际: %+v", res.Deps)
	}
}

func TestStructureSlotsPublishUsesAvailableTemplate(t *testing.T) {
	diags := builder.NewDegradeCollector()
	res, err := BuildStructureSlots(context.Background(), StructureSlotInput{
		Port: stubPort{docs: map[string][]byte{
			"tpl-1": templateDoc("tpl-mark", "TPL"),
		}},
		ProjectID:   "p1",
		Structure:   builder.StructureBindings{HeaderTemplateID: "tpl-1", HeaderBlockID: "blk-unused"},
		Inner:       stubBlocks{},
		Mode:        builder.CompileModePublish,
		Diagnostics: diags,
	})
	if err != nil {
		t.Fatalf("模板可用时不该失败: %v", err)
	}
	if len(res.Slots) != 1 || res.Slots[0].BlockID != builder.StructureTemplateRef("tpl-1") {
		t.Fatalf("模板可用时应走模板虚拟引用，实际: %+v", res.Slots)
	}
	roots, rerr := res.Resolver.ResolveBlockRoot(res.Slots[0].BlockID)
	if rerr != nil || len(roots) != 1 {
		t.Fatalf("虚拟引用必须解析出模板节点: roots=%d err=%v", len(roots), rerr)
	}
	if len(res.Deps) == 0 || res.Deps[0].Kind != DepKindContentTemplate || !strings.HasSuffix(res.Deps[0].Key, "tpl-1") {
		t.Fatalf("模板被消费时应登记 content_template 依赖，实际: %+v", res.Deps)
	}
	if len(diags.Items()) != 0 {
		t.Fatalf("正常路径不该有降级诊断: %+v", diags.Items())
	}
}

func TestStructureSlotsEmptyTemplateFallsBackInPublish(t *testing.T) {
	diags := builder.NewDegradeCollector()
	res, err := BuildStructureSlots(context.Background(), StructureSlotInput{
		Port: stubPort{docs: map[string][]byte{
			"tpl-empty": emptyTemplateDoc(),
		}},
		ProjectID:   "p1",
		Structure:   builder.StructureBindings{HeaderTemplateID: "tpl-empty", HeaderBlockID: "blk-1"},
		Inner:       stubBlocks{},
		Mode:        builder.CompileModePublish,
		Diagnostics: diags,
	})
	if err != nil {
		t.Fatalf("空模板不该让发布失败: %v", err)
	}
	if len(res.Slots) != 1 || res.Slots[0].BlockID != "blk-1" {
		t.Fatalf("空模板应回退到块绑定，实际: %+v", res.Slots)
	}
	items := diags.Items()
	if len(items) != 1 || items[0].Reason != core.RefReasonTemplateEmpty {
		t.Fatalf("空模板回退必须留归因诊断，实际: %+v", items)
	}
}

func TestStructureSlotDependenciesTolerantOnUnavailableTemplate(t *testing.T) {
	deps := StructureSlotDependencies(context.Background(), stubPort{}, "p1",
		builder.StructureBindings{HeaderTemplateID: "tpl-missing", HeaderBlockID: "blk-1"})
	if len(deps) != 1 || deps[0].Key != "block:blk-1" {
		t.Fatalf("回退到块时只登记块依赖，实际: %+v", deps)
	}
}
