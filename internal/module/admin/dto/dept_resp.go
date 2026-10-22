package admindto

import "go_wp/pkg/tree"

// DeptPageRow is a flat page row with a parent label for cross-page context.
type DeptPageRow struct {
	ID          uint64
	DeptName    string
	DeptCode    string
	ParentID    uint64
	ParentTitle string
	Status      int
	SortOrder   int
	Remark      string
}

// DeptParentChoice is an unpaged selector option.
type DeptParentChoice struct {
	ID       uint64
	DeptName string
	Indent   string
}

// DeptPageResp separates page rows from the complete parent selector.
type DeptPageResp struct {
	Total   int64
	Rows    []DeptPageRow
	Parents []DeptParentChoice
}

// DeptTreeNode 部门树节点。
type DeptTreeNode struct {
	ID        uint64          `json:"id"`
	ParentID  uint64          `json:"parent_id"`
	DeptName  string          `json:"dept_name"`
	DeptCode  string          `json:"dept_code"`
	LeaderID  *uint64         `json:"leader_id"`
	SortOrder int             `json:"sort_order"`
	Status    int             `json:"status"`
	Remark    string          `json:"remark"`
	Children  []*DeptTreeNode `json:"children,omitempty"`
}

// —— tree.Node[uint64] 约束实现 ——
func (n *DeptTreeNode) TreeID() uint64       { return n.ID }
func (n *DeptTreeNode) TreeParentID() uint64 { return n.ParentID }

// SetTreeChildren 挂子节点（本类型本来就用指针切片存 Children，直接转）。
func (n *DeptTreeNode) SetTreeChildren(children []tree.Node[uint64]) {
	if len(children) == 0 {
		n.Children = nil
		return
	}
	out := make([]*DeptTreeNode, 0, len(children))
	for _, c := range children {
		out = append(out, c.(*DeptTreeNode))
	}
	n.Children = out
}
