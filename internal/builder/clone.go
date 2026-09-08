package builder

import (
	"encoding/json"

	"go_wp/internal/builder/core"

	"github.com/google/uuid"
)

// ClonePageWithNewIDs 复制完整 Page Document：settings 原样保留，root 每个节点递归
// 重写 ID，得到与源文档脱钩的独立副本（docs/02-D §5.2 复用语义的公共机制）。
//
// 调用方：blueprint（整页初始化）、block（片段模板「插入-复制」）。两者复用同一套
// 「复制 AST + 重新生成 Node ID」机制，只是目标层级不同（完整 Page vs Page 子树）。
func ClonePageWithNewIDs(page *Page) *Page {
	cloned := &Page{Settings: page.Settings, Root: make([]*core.Node, 0, len(page.Root))}
	for _, n := range page.Root {
		cloned.Root = append(cloned.Root, CloneNodeWithNewIDs(n))
	}
	return cloned
}

// CloneNodeWithNewIDs 递归复制 AST 节点：每个 node.ID 换新 UUID，Children 递归复制。
// Type/Props/编辑元数据（Name/Hidden/Locked）原样保留，仅重写 ID。
func CloneNodeWithNewIDs(n *core.Node) *core.Node {
	if n == nil {
		return nil
	}
	cloned := &core.Node{
		ID:     uuid.NewString(),
		Type:   n.Type,
		Props:  append(json.RawMessage(nil), n.Props...),
		Name:   n.Name,
		Hidden: n.Hidden,
		Locked: n.Locked,
	}
	if len(n.Children) > 0 {
		cloned.Children = make([]*core.Node, 0, len(n.Children))
		for _, c := range n.Children {
			cloned.Children = append(cloned.Children, CloneNodeWithNewIDs(c))
		}
	}
	return cloned
}
