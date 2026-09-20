package pipeline

// structure_template_test.go — 结构槽位解析的发布 / 预览分界（审计 ARCH-05）。
//
// 这里只测**纯决策**：不建库、不渲染。分界本身是纯函数（planStructureSlots），
// 而渲染期的另一半（块引用不可用 → 发布失败 / 预览可归因占位）在
// internal/builder 与 public/test/page/feature 各有用例钉住。

import (
	"context"
	"errors"
	"strings"
	"testing"

	"go_wp/internal/builder"
	"go_wp/internal/builder/core"
)

// stubPort 结构模板端口桩：docs 命中返回文档，errs 命中返回错误，其余视为不存在。
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

// stubBlocks 块解析器桩：roots 命中返回节点，其余报错。
type stubBlocks struct {
	roots map[string][]*core.Node
}

func (b stubBlocks) ResolveBlockRoot(blockID string) ([]*core.Node, error) {
	if nodes, ok := b.roots[blockID]; ok {
		return nodes, nil
	}
	return nil, errors.New("块不可用")
}

// templateDoc 结构模板文档（一个文本节点，便于断言「模板确实被展开」）。
func templateDoc(id, text string) []byte {
	return []byte(`{"settings":{"layout":{"mode":"full"}},"root":[{"type":"core.text","id":"` + id + `","props":{"text":"` + text + `"}}]}`)
}

// emptyTemplateDoc 空 root 的结构模板文档。
func emptyTemplateDoc() []byte {
	return []byte(`{"settings":{"layout":{"mode":"full"}},"root":[]}`)
}

// TestStructureSlotsPublishFailsOnUnavailableBoundTemplate 发布模式下，显式绑定却拿不到的
// 结构模板必须让解析失败 —— 这是 ARCH-05 要落地的那条分界。
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

// TestStructureSlotsPreviewDegradesUnavailableTemplateWithAttribution 预览模式下同一条绑定
// 不失败：有块回退时用块，没有时保留一个归因到模板的槽位占位，并记诊断。
func TestStructureSlotsPreviewDegradesUnavailableTemplateWithAttribution(t *testing.T) {
	ctx := context.Background()

	// (a) 有块绑定：回退到块，依赖只登记块（不登记没被消费的模板）。
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

	// (b) 没有块可回退：仍产出槽位（供占位归因），解析该虚拟引用时带出模板归因码。
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

// TestStructureSlotsPublishUsesAvailableTemplate 模板可用：模板优先、依赖登记模板与模板内引用块。
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

// TestStructureSlotsEmptyTemplateFallsBackInPublish 模板存在但没有内容**不是**拿不到：
// 发布照常，按显式设计回退到块绑定，并留一条诊断。
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

// TestStructureSlotDependenciesTolerantOnUnavailableTemplate 依赖推导（构建后落库的静态扫描）
// 不承担把关职责：模板拿不到时不 panic、不登记没被消费的模板依赖。
func TestStructureSlotDependenciesTolerantOnUnavailableTemplate(t *testing.T) {
	deps := StructureSlotDependencies(context.Background(), stubPort{}, "p1",
		builder.StructureBindings{HeaderTemplateID: "tpl-missing", HeaderBlockID: "blk-1"})
	if len(deps) != 1 || deps[0].Key != "block:blk-1" {
		t.Fatalf("回退到块时只登记块依赖，实际: %+v", deps)
	}
}
