// Package deptdto dept 模块数据传输对象。
package deptdto

// TreeNode 部门树节点。
type TreeNode struct {
	ID        uint64     `json:"id"`
	ParentID  uint64     `json:"parent_id"`
	DeptName  string     `json:"dept_name"`
	DeptCode  string     `json:"dept_code"`
	LeaderID  *uint64    `json:"leader_id"`
	SortOrder int        `json:"sort_order"`
	Status    int        `json:"status"`
	Remark    string     `json:"remark"`
	Children  []TreeNode `json:"children,omitempty"`
}