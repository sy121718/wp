package admindto

import "go_wp/pkg/tree"

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

type DeptParentChoice struct {
	ID       uint64
	DeptName string
	Indent   string
}

type DeptPageResp struct {
	Total   int64
	Rows    []DeptPageRow
	Parents []DeptParentChoice
}

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

func (n *DeptTreeNode) TreeID() uint64       { return n.ID }
func (n *DeptTreeNode) TreeParentID() uint64 { return n.ParentID }

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
