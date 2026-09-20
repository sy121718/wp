// Package layoutslot 实现 core.layoutSlot 组件：结构槽位（页眉 / 页脚 / 未来的公告条等）
// 对全局块的绑定。
//
// 与 core.globalref 的关系：同样是「引用一个全局块、构建期展开」。差别在语义与约束：
//
//   - slot 走**白名单**：键名写错是拒绝，而不是静默不生效 —— 槽位绑定来自主题设置，
//     拼错的槽位名只会表现为「配了但页面没变化」，这是最难查的一类问题；
//   - 展开结果**不带 wrapper**（与 globalref 共用同一份模板形态）：因此「槽位展开」与
//     「把块内容直接写在页面里」的产物字节完全一致 —— 这是从字符串拼接迁移过来时
//     能逐字节比对的前提；
//   - 槽位节点是**顶层结构节点**：main 地标判定会跳过它们（它们本来就在 main 之外），
//     而作者手动插入的 core.globalref 仍参与正常的地标判定。
//
// 为什么不是「装配层把块 HTML 拼上去」：那样页眉页脚不在 Page AST 里，于是
// 翻译候选收集、失效依赖传播、workbench 画布、SEO 头都各需要一份「外加的块」特判，
// 而这些特判迟早会漏掉一处（例如改了页眉引用的块，页面不会被标 stale）。
package layoutslot

import (
	_ "embed" // layoutslot.jet 经 //go:embed 打进二进制
	"encoding/json"
	"fmt"
	"strings"

	"go_wp/internal/builder/components/globalref"
	"go_wp/internal/builder/core"
)

// Type 组件类型标识。
const Type = "core.layoutSlot"

// 槽位名白名单。
//
// 顺序即渲染位置：Position 为负的排在页面主体之前，为正的排在之后。
// 目前只有页眉 / 页脚两个槽位（与 settings.structure 的字段一一对应）；
// 扩展槽位（公告条 / 侧边栏 / 抽屉）时在这里加一行并给出位置即可，
// 渲染与失效传播都不需要改。
const (
	SlotAnnouncement = "announcement"
	SlotHeader       = "header"
	SlotFooter       = "footer"
)

func init() {
	core.Register(&Component{})
	core.RegisterTemplate("layoutslot", layoutslotTemplate)
}

// Component 结构槽位组件。
type Component struct{}

// Type 组件类型标识。
func (c *Component) Type() string { return Type }

// PropsSpec 实现 SpecProvider：暴露 props 生成检查器 schema。
func (c *Component) PropsSpec() any { return &props{} }

// props 节点 props。
//
// 只存「哪个槽位 + 引用哪个块」：块内容不进文档，改块之后由失效传播去标记引用页，
// 与 core.globalref 同一条路子。
type props struct {
	// Slot 槽位名（白名单，见本包常量）。
	Slot string `json:"slot" ct:"string,maxlen=32,label=槽位"`
	// BlockID 全局块 ID。
	BlockID string `json:"blockId" ct:"string,maxlen=64,sec=content,label=全局块 ID"`
}

// decode 解析并校验 props。
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

// IsValidSlot 槽位名是否在白名单内。
func IsValidSlot(slot string) bool {
	return PositionOf(slot) != 0
}

// PositionOf 槽位位置：负数 = 主体之前，正数 = 之后，0 = 未知槽位。
//
// 用位置值而不是两个切片：新增槽位时只需在这里给一个数，插入逻辑不必改。
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

// SlotOf 读取节点的槽位名。
func SlotOf(node *core.Node) (string, error) {
	p, err := decode(node)
	if err != nil {
		return "", err
	}
	return p.Slot, nil
}

// BlockIDOf 读取节点引用的块 ID（与 globalref 同一形状，渲染层可共用展开实现）。
func BlockIDOf(node *core.Node) (string, error) {
	p, err := decode(node)
	if err != nil {
		return "", err
	}
	return p.BlockID, nil
}

// NewNode 构造一个槽位节点（装配层把 settings.structure 展开成 AST 节点时用）。
//
// 节点 ID 由槽位名派生：同一页面里一个槽位至多一个节点，ID 稳定才能让
// 「同一份文档 + 同一份绑定 → 同一份产物」（确定性构建）。
func NewNode(slot, blockID string) *core.Node {
	raw, _ := json.Marshal(props{Slot: slot, BlockID: blockID})
	return &core.Node{
		ID:    "__layout_" + slot,
		Type:  Type,
		Props: raw,
	}
}

// Validate 校验节点：槽位白名单、blockId 非空、叶子节点、ID 参与文档级查重。
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

// layoutslotTemplate 组件模板：与 globalref 同形 —— 非占位时**只渲染展开出来的子节点**，
// 不产生额外 wrapper。少了 wrapper 这层，槽位展开与「块内容直接写在页面里」的字节才一致。
//
//go:embed layoutslot.jet
var layoutslotTemplate string

// BuildView 展开槽位引用的块（委托 globalref：两边的 props 都有 blockId，
// 展开规则（深拷贝 + ID 前缀重写）完全一致，各写一份迟早会漂移）。
//
// 展开失败时补上槽位名：占位是给作者看的「这里本该有一份结构」，
// 只说节点 ID 等于什么都没说（节点 ID 是编译期派生的 __layout_header）。
func BuildView(node *core.Node, block core.BlockResolver) (globalref.View, []*core.Node, error) {
	view, roots, err := globalref.BuildView(node, block)
	if view.IsPlaceholder {
		if slot, serr := SlotOf(node); serr == nil {
			view.Slot = slot
		}
	}
	return view, roots, err
}
