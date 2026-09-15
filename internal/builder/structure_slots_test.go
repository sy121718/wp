package builder

// structure_slots_test.go — 结构槽位展开（审计 VIS-001）。
//
// 这条改造把页眉页脚从「装配层拼字符串」搬进 Page AST。真正需要被钉住的不是
// 「能展开」，而是四个容易在后续改动里悄悄坏掉的点：
//   1. 没有绑定时不产生任何节点（既有产物逐字节不变，零影响）；
//   2. 展开位置正确（页眉在主体之前、页脚在之后）；
//   3. 文档里已写死的同槽位节点优先，不会出现两份页眉；
//   4. main 地标把槽位节点留在外面（页眉被包进 main 就退化成普通元素，
//      banner / contentinfo 地标随之消失 —— 这一点屏幕阅读器直接受影响）。

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"go_wp/internal/builder/core"
	"go_wp/internal/templates"
)

// stubBlocks 最小块解析器：按 ID 返回固定 root。
type stubBlocks map[string]string

func (s stubBlocks) ResolveBlockRoot(blockID string) ([]*core.Node, error) {
	doc, ok := s[blockID]
	if !ok {
		return nil, nil
	}
	page, err := ParsePage([]byte(doc))
	if err != nil {
		return nil, err
	}
	return page.Root, nil
}

// node 构造一个最小的文本节点。
func node(id, text string) *core.Node {
	raw, _ := json.Marshal(map[string]any{"text": text})
	return &core.Node{ID: id, Type: "core.text", Props: raw}
}

// layoutSlotNode 构造一个槽位节点（经编译期展开的同一构造函数）。
func layoutSlotNode(t *testing.T, slot, blockID string) *core.Node {
	t.Helper()
	n := expandStructureSlots(nil, []StructureSlot{{Slot: slot, BlockID: blockID}})
	if len(n) != 1 {
		t.Fatalf("构造槽位节点失败: %d", len(n))
	}
	return n[0]
}

// TestExpandStructureSlotsNoBindingsIsNoop 无绑定时不插入任何节点。
func TestExpandStructureSlotsNoBindingsIsNoop(t *testing.T) {
	root := []*core.Node{node("a", "A")}
	got := expandStructureSlots(root, nil)
	if len(got) != 1 || got[0].ID != "a" {
		t.Fatalf("无绑定时应原样返回，实际 %d 个节点", len(got))
	}
	// 空 blockId 与未知槽位都不该产生节点：宁可不渲染，也不要渲染一个空槽位。
	got = expandStructureSlots(root, []StructureSlot{
		{Slot: SlotHeader, BlockID: "  "},
		{Slot: "not-a-slot", BlockID: "b1"},
	})
	if len(got) != 1 {
		t.Fatalf("空 blockId / 未知槽位都不应插入节点，实际 %d 个", len(got))
	}
}

// TestExpandStructureSlotsOrder 页眉在主体之前、页脚在之后。
func TestExpandStructureSlotsOrder(t *testing.T) {
	root := []*core.Node{node("body1", "BODY")}
	got := expandStructureSlots(root, []StructureSlot{
		{Slot: SlotFooter, BlockID: "f1"},
		{Slot: SlotHeader, BlockID: "h1"},
	})
	if len(got) != 3 {
		t.Fatalf("应插入 2 个槽位节点，实际 %d 个节点", len(got))
	}
	if got[0].Type != "core.layoutSlot" || got[len(got)-1].Type != "core.layoutSlot" {
		t.Fatalf("槽位节点应在首尾，实际首=%s 尾=%s", got[0].Type, got[len(got)-1].Type)
	}
	first, _ := json.Marshal(got[0].Props)
	last, _ := json.Marshal(got[len(got)-1].Props)
	if !strings.Contains(string(first), "h1") {
		t.Fatalf("首个槽位应是页眉（绑定 h1），实际 %s", first)
	}
	if !strings.Contains(string(last), "f1") {
		t.Fatalf("末个槽位应是页脚（绑定 f1），实际 %s", last)
	}
	if got[1].ID != "body1" {
		t.Fatalf("主体节点应保持原位，实际 %s", got[1].ID)
	}
}

// TestExpandStructureSlotsDocumentWins 文档里已有的同槽位节点优先。
//
// 否则每次改主题都会在文档外再插一个页眉 —— 页面出现两份页眉，且两份的字节还不一样。
func TestExpandStructureSlotsDocumentWins(t *testing.T) {
	existing := layoutSlotNode(t, SlotHeader, "from-doc")
	root := []*core.Node{existing, node("body1", "BODY")}
	got := expandStructureSlots(root, []StructureSlot{{Slot: SlotHeader, BlockID: "from-theme"}})
	if len(got) != 2 {
		t.Fatalf("绑定与文档重复时不应再插入，实际 %d 个节点", len(got))
	}
	raw, _ := json.Marshal(got[0].Props)
	if !strings.Contains(string(raw), "from-doc") {
		t.Fatalf("应以文档里的节点为准，实际 %s", raw)
	}
}

// TestStructureSlotsRenderAroundBodyAndOutsideMain 展开后的渲染顺序与 main 地标。
func TestStructureSlotsRenderAroundBodyAndOutsideMain(t *testing.T) {
	set, serr := templates.NewComponentSet("../templates/components")
	if serr != nil {
		t.Fatalf("NewComponentSet: %v", serr)
	}
	blocks := stubBlocks{
		"h1": "{\"settings\":{},\"root\":[{\"id\":\"hdr\",\"type\":\"core.text\",\"props\":{\"text\":\"HEADER-MARK\"}}]}",
		"f1": "{\"settings\":{},\"root\":[{\"id\":\"ftr\",\"type\":\"core.text\",\"props\":{\"text\":\"FOOTER-MARK\"}}]}",
	}
	build := func(mainLandmark bool) string {
		page := &Page{
			Settings: PageSettings{Layout: PageLayout{Mode: LayoutFull, MainLandmark: mainLandmark}},
			Root:     []*core.Node{node("body1", "BODY-MARK")},
		}
		res, err := Compile(page,
			WithContext(context.Background()), WithComponentSet(set), WithBlockResolver(blocks),
			WithStructureSlots(
				StructureSlot{Slot: SlotHeader, BlockID: "h1"},
				StructureSlot{Slot: SlotFooter, BlockID: "f1"},
			))
		if err != nil {
			t.Fatalf("编译失败: %v", err)
		}
		return res.HTML
	}

	html := build(false)
	iHeader := strings.Index(html, "HEADER-MARK")
	iBody := strings.Index(html, "BODY-MARK")
	iFooter := strings.Index(html, "FOOTER-MARK")
	if iHeader < 0 || iBody < 0 || iFooter < 0 {
		t.Fatalf("三段内容都应出现在产物里: header=%d body=%d footer=%d", iHeader, iBody, iFooter)
	}
	if !(iHeader < iBody && iBody < iFooter) {
		t.Fatalf("顺序应为 页眉 → 主体 → 页脚，实际 %d/%d/%d", iHeader, iBody, iFooter)
	}

	withMain := build(true)
	iMain := strings.Index(withMain, "<main")
	if iMain < 0 {
		t.Fatalf("开启 main 地标后应输出 main 元素")
	}
	if strings.Index(withMain, "HEADER-MARK") > iMain {
		t.Fatalf("页眉应在 main 之外（被包进 main 会让 banner 地标消失）")
	}
	if iClose := strings.Index(withMain, "</main>"); strings.Index(withMain, "FOOTER-MARK") < iClose {
		t.Fatalf("页脚应在 main 之后")
	}
}
