package core

import json "go_wp/pkg/jsonx"

// CloneNodes 先为整组节点分配ID，再重映射内部instance目标，保留组外显式引用。
// 调用者决定复制用新ID、编译实例用确定性前缀；两者共享同一动作重映射判据。
func CloneNodes(nodes []*Node, idOf func(string) string) []*Node {
	ids := map[string]string{}
	var copyNode func(*Node) *Node
	copyNode = func(node *Node) *Node {
		if node == nil {
			return nil
		}
		copy := *node
		copy.ID = idOf(node.ID)
		ids[node.ID] = copy.ID
		copy.Props = append(json.RawMessage(nil), node.Props...)
		if node.Actions != nil {
			copy.Actions = make(map[string]ActionTarget, len(node.Actions))
			for key, target := range node.Actions {
				copy.Actions[key] = target
			}
		}
		copy.Children = make([]*Node, len(node.Children))
		for i, child := range node.Children {
			copy.Children[i] = copyNode(child)
		}
		return &copy
	}
	out := make([]*Node, len(nodes))
	for i, node := range nodes {
		out[i] = copyNode(node)
	}
	var remap func([]*Node)
	remap = func(list []*Node) {
		for _, node := range list {
			if node == nil {
				continue
			}
			for key, target := range node.Actions {
				if id, exists := ids[target.NodeID]; target.Kind == ActionInstance && exists {
					target.NodeID = id
					node.Actions[key] = target
				}
			}
			remap(node.Children)
		}
	}
	remap(out)
	return out
}
