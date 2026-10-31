package layoutslot

import (
	_ "embed"
	"fmt"
	json "go_wp/pkg/jsonx"
	"strings"

	"go_wp/internal/builder/components/globalref"
	"go_wp/internal/builder/core"
)

const Type = "core.layoutSlot"

const (
	SlotAnnouncement = "announcement"
	SlotHeader       = "header"
	SlotFooter       = "footer"
)

func init() {
	core.Register(&Component{})
	core.RegisterTemplate("layoutslot", layoutslotTemplate)
}

type Component struct{}

func (c *Component) Type() string { return Type }

func (c *Component) PropsSpec() any { return &props{} }

type props struct {
	Slot string `json:"slot" ct:"string,maxlen=32,label=槽位"`

	BlockID string `json:"blockId" ct:"string,maxlen=64,sec=content,label=全局块 ID"`
}

func decode(node *core.Node) (p props, err error) {
	if len(node.Props) > 0 {
		if err = json.Unmarshal(node.Props, &p); err != nil {
			return p, fmt.Errorf("节点 %s props 反序列化失败: %w", node.ID, err)
		}
	}
	p.Slot = strings.TrimSpace(p.Slot)
	if !IsValidSlot(p.Slot) {
		return p, fmt.Errorf("节点 %s: 未知槽位 %q", node.ID, p.Slot)
	}
	if strings.TrimSpace(p.BlockID) == "" {
		return p, fmt.Errorf("节点 %s: blockId 不能为空", node.ID)
	}
	return p, nil
}

func IsValidSlot(slot string) bool {
	return PositionOf(slot) != 0
}

func PositionOf(slot string) int {
	switch slot {
	case SlotAnnouncement:
		return -20
	case SlotHeader:
		return -10
	case SlotFooter:
		return 10
	default:
		return 0
	}
}

func SlotOf(node *core.Node) (string, error) {
	p, err := decode(node)
	if err != nil {
		return "", err
	}
	return p.Slot, nil
}

func BlockIDOf(node *core.Node) (string, error) {
	p, err := decode(node)
	if err != nil {
		return "", err
	}
	return p.BlockID, nil
}

func NewNode(slot, blockID string) *core.Node {
	raw, _ := json.Marshal(props{Slot: slot, BlockID: blockID})
	return &core.Node{
		ID:    "__layout_" + slot,
		Type:  Type,
		Props: raw,
	}
}

func (c *Component) Validate(node *core.Node, ids map[string]bool) (err error) {
	if err = core.ValidateNodeID(node.ID, node.Name, ids); err != nil {
		return err
	}
	if len(node.Children) > 0 {
		return fmt.Errorf("节点 %s: 结构槽位为叶子节点，不允许子节点", node.ID)
	}
	if _, err = decode(node); err != nil {
		return err
	}
	return nil
}

//go:embed layoutslot.jet
var layoutslotTemplate string

func BuildView(node *core.Node, block core.BlockResolver) (globalref.View, []*core.Node, error) {
	view, roots, err := globalref.BuildView(node, block)
	if view.IsPlaceholder {
		if slot, serr := SlotOf(node); serr == nil {
			view.Slot = slot
		}
	}
	return view, roots, err
}
