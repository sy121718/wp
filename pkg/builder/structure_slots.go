package builder

import (
	"cmp"
	"slices"
	"strings"

	"go_wp/pkg/builder/components/layoutslot"
	"go_wp/pkg/builder/core"
)

const (
	SlotHeader = layoutslot.SlotHeader
	SlotFooter = layoutslot.SlotFooter
)

type StructureSlot struct {
	Slot    string
	BlockID string
}

const structureTemplateRefPrefix = "__structure_template__"

func StructureTemplateRef(templateID string) string {
	return structureTemplateRefPrefix + templateID
}

func IsStructureTemplateRef(ref string) bool {
	return strings.HasPrefix(ref, structureTemplateRefPrefix)
}

type SlotFrame struct {
	Slot string

	Ref string

	RefKind string

	NodeID string
}

func SlotRefKind(ref string) string {
	if IsStructureTemplateRef(ref) {
		return "template"
	}
	return "block"
}

func WithStructureSlots(slots ...StructureSlot) CompileOption {
	return func(c *compileConfig) { c.structureSlots = slots }
}

func SortedSlots(bindings map[string]string) []string {
	out := make([]string, 0, len(bindings))
	for slot := range bindings {
		out = append(out, slot)
	}
	pos := func(s string) int {
		if p := layoutslot.PositionOf(s); p != 0 {
			return p
		}
		return 1 << 30
	}
	slices.SortStableFunc(out, func(a, b string) int {
		if pa, pb := pos(a), pos(b); pa != pb {
			return cmp.Compare(pa, pb)
		}
		return cmp.Compare(a, b)
	})
	return out
}

func isLayoutSlotNode(n *core.Node) bool {
	return n != nil && n.Type == layoutslot.Type
}

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
		slices.SortStableFunc(list, func(a, b *core.Node) int {
			sa, _ := layoutslot.SlotOf(a)
			sb, _ := layoutslot.SlotOf(b)
			return cmp.Compare(layoutslot.PositionOf(sa), layoutslot.PositionOf(sb))
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
