package builder

// structure_slots.go — 结构槽位绑定（审计 VIS-001）。
//
// 背景：页眉页脚此前是「编译成 HTML 片段再与页面主体做字符串相加」，于是它们不在
// Page AST 里 —— 翻译候选收集、失效依赖传播、workbench 画布、main 地标判定、SEO 头
// 各需要一份「外加的块」特判，而这些特判迟早会漏一处（典型后果：改了页眉引用的块，
// 引用页不会被标 stale，站点上页眉一直显示旧内容）。
//
// 本文件把那一步搬进 AST：绑定在编译期展开成 root 首尾的 core.layoutSlot 节点，
// 之后的一切都走既有路径。
//
// 组件 import 的触发点在这里多了一个（此前集中在 jetview.go）：本文件是槽位节点
// 类型的唯一使用者，放在这里比让 builder.go 也 import 组件包更收敛。

import (
	"sort"
	"strings"

	"go_wp/internal/builder/components/layoutslot"
	"go_wp/internal/builder/core"
)

// 槽位名常量（与 settings.structure 的字段一一对应）。
//
// 暴露在这里是为了让调用方（page / presentation / dashboard）不必 import 组件包：
// 组件 import 的触发点越少越好，槽位名属于配置面而不是渲染面。
const (
	SlotHeader = layoutslot.SlotHeader
	SlotFooter = layoutslot.SlotFooter
)

// StructureSlot 一个结构槽位绑定（槽位名 → 全局块 ID）。
type StructureSlot struct {
	Slot    string
	BlockID string
}

// WithStructureSlots 注入结构槽位绑定：编译期展开成 root 首尾的 core.layoutSlot 节点。
//
// 为什么放在编译期而不是让调用方自己往文档里塞节点：页面、自动发布实例、预览
// 三条路径都要同一份行为，谁漏接谁就表现为「页眉不见了」；放进 Compile 之后，
// 三条路径只需各传一次绑定，行为不可能分叉。
//
// 展开结果只存在于本次编译的局部 root 切片，**不改动传入的 Page**。
func WithStructureSlots(slots ...StructureSlot) CompileOption {
	return func(c *compileConfig) { c.structureSlots = slots }
}

// SortedSlots 按槽位位置排序的槽位名列表（未知槽位排在最后）。
//
// 用途：把绑定（map）转成**确定性顺序**的遍历 —— 内容翻译候选、依赖登记这类地方
// 若按 map 的随机顺序产出，同一份文档两次运行会得到不同的中间结果：产物字节或许相同，
// 但缓存键、日志与「哪个块先被登记」会抖，排查时很难解释。
func SortedSlots(bindings map[string]string) []string {
	out := make([]string, 0, len(bindings))
	for slot := range bindings {
		out = append(out, slot)
	}
	pos := func(s string) int {
		if p := layoutslot.PositionOf(s); p != 0 {
			return p
		}
		return 1 << 30 // 未知槽位最后（它们本来就会被展开逻辑跳过）
	}
	sort.SliceStable(out, func(i, j int) bool {
		if pi, pj := pos(out[i]), pos(out[j]); pi != pj {
			return pi < pj
		}
		return out[i] < out[j]
	})
	return out
}

// isLayoutSlotNode 是否为结构槽位节点（main 地标判定跳过它们）。
func isLayoutSlotNode(n *core.Node) bool {
	return n != nil && n.Type == layoutslot.Type
}

// expandStructureSlots 把槽位绑定展开成 root 首尾的结构节点。
//
// 两条规则：
//   - 文档里已有的同槽位节点优先（绑定只是「文档没写时补上」）：否则保存过的文档
//     会在每次改主题后多出第二份页眉，而且两份的字节还不一样；
//   - 位置按 layoutslot.PositionOf 排序（负数在主体之前、正数在之后）：将来加
//     「公告条」「侧边栏」只需给一个位置值，这里的插入逻辑不动。
func expandStructureSlots(root []*core.Node, slots []StructureSlot) []*core.Node {
	if len(slots) == 0 {
		return root
	}
	existing := map[string]bool{}
	for _, n := range root {
		if !isLayoutSlotNode(n) {
			continue
		}
		if slot, err := layoutslot.SlotOf(n); err == nil {
			existing[slot] = true
		}
	}
	var before, after []*core.Node
	for _, s := range slots {
		if strings.TrimSpace(s.BlockID) == "" || !layoutslot.IsValidSlot(s.Slot) || existing[s.Slot] {
			continue
		}
		node := layoutslot.NewNode(s.Slot, s.BlockID)
		if layoutslot.PositionOf(s.Slot) < 0 {
			before = append(before, node)
		} else {
			after = append(after, node)
		}
	}
	if len(before) == 0 && len(after) == 0 {
		return root
	}
	byPos := func(list []*core.Node) {
		sort.SliceStable(list, func(i, j int) bool {
			si, _ := layoutslot.SlotOf(list[i])
			sj, _ := layoutslot.SlotOf(list[j])
			return layoutslot.PositionOf(si) < layoutslot.PositionOf(sj)
		})
	}
	byPos(before)
	byPos(after)
	out := make([]*core.Node, 0, len(before)+len(root)+len(after))
	out = append(out, before...)
	out = append(out, root...)
	out = append(out, after...)
	return out
}
