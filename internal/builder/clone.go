package builder

import (
	"go_wp/internal/builder/core"

	"github.com/google/uuid"
)

// ClonePageWithNewIDs 复制完整 Page Document：settings 原样保留，root 每个节点递归
// 重写 ID，得到与源文档脱钩的独立副本（docs/02-D §5.2 复用语义的公共机制）。
//
// 调用方：blueprint（整页初始化）、block（片段模板「插入-复制」）。两者复用同一套
// 「复制 AST + 重新生成 Node ID」机制，只是目标层级不同（完整 Page vs Page 子树）。
func ClonePageWithNewIDs(page *Page) *Page {
	if page == nil {
		return nil
	}
	cloned := *page
	cloned.Root = core.CloneNodes(page.Root, func(string) string { return uuid.NewString() })
	return &cloned
}

// CloneNodeWithNewIDs 递归复制 AST 节点：每个 node.ID 换新 UUID，Children 递归复制。
// Type/Props/编辑元数据（Name/Hidden/Locked）原样保留，仅重写 ID。
func CloneNodeWithNewIDs(n *core.Node) *core.Node {
	return core.CloneNodes([]*core.Node{n}, func(string) string { return uuid.NewString() })[0]
}
