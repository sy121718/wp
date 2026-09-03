// Package globalref 实现 core.globalref 组件：页面文档中的全局块引用节点
// （021_blocks.sql 方案 C，对应 WP 的 synced pattern）。
//
// 节点只保存块 ID 引用；构建期经 RenderContext.Block 展开块内容
// （深拷贝块 root 节点并重写节点 ID 前缀，避免与页面节点 ID 冲突），
// 渲染进同一次编译输出。未注入解析器（预览/校验）时降级为可选中占位。
package globalref

import (
	"encoding/json"
	"fmt"
	"strings"

	"go_wp/internal/builder/core"
)

// Type 组件类型标识。
const Type = "core.globalref"

func init() { core.Register(&Component{}) }

// Component 全局块引用组件。
type Component struct{}

// Type 组件类型标识。
func (c *Component) Type() string { return Type }

// PropsSpec 实现 SpecProvider：暴露 globalrefProps 生成检查器 schema（blockId 引用）。
func (c *Component) PropsSpec() any { return &globalrefProps{} }

// globalrefProps 节点 props。
type globalrefProps struct {
	BlockID string `json:"blockId"`
}

func decode(node *core.Node) (p globalrefProps, err error) {
	if len(node.Props) > 0 {
		if err = json.Unmarshal(node.Props, &p); err != nil {
			return p, fmt.Errorf("节点 %s props 反序列化失败: %w", node.ID, err)
		}
	}
	if strings.TrimSpace(p.BlockID) == "" {
		return p, fmt.Errorf("节点 %s: blockId 不能为空", node.ID)
	}
	return p, nil
}

// Validate 校验节点：blockId 非空、叶子节点、ID 参与文档级查重。
func (c *Component) Validate(node *core.Node, ids map[string]bool) (err error) {
	if err = core.ValidateNodeID(node.ID, node.Name, ids); err != nil {
		return err
	}
	if len(node.Children) > 0 {
		return fmt.Errorf("节点 %s: 全局块引用为叶子节点，不允许子节点", node.ID)
	}
	if _, err = decode(node); err != nil {
		return err
	}
	return nil
}

// cloneWithIDPrefix 深拷贝节点树并为全部节点 ID 加前缀（CSS wp-c-<id> 类随之隔离）。
func cloneWithIDPrefix(node *core.Node, prefix string) *core.Node {
	if node == nil {
		return nil
	}
	clone := *node
	clone.ID = prefix + node.ID
	if len(node.Children) > 0 {
		clone.Children = make([]*core.Node, len(node.Children))
		for i, child := range node.Children {
			clone.Children[i] = cloneWithIDPrefix(child, prefix)
		}
	}
	return &clone
}
