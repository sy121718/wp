package admindto

import "go_wp/pkg/tree"

type MenuPageRow struct {
	ID        uint64
	ParentID  uint64
	Title     string
	Path      string
	Type      int
	Status    int
	SortOrder int
	Remark    string
	Icon      string

	PermissionCodes []string

	Depth int

	HasChildren bool

	Hidden bool

	Expanded bool

	Matched bool
}

type MenuParentChoice struct {
	ID     uint64
	Title  string
	Indent string
	Type   int

	Disabled bool
}

type MenuPageResp struct {
	Total int64
	Rows  []MenuPageRow

	Parents []MenuParentChoice
}

type MenuTreeNode struct {
	ID              uint64         `json:"id"`
	PermissionCodes []string       `json:"permission_codes"`
	Title           string         `json:"title"`
	TitleKey        string         `json:"title_key,omitempty"`
	ParentID        uint64         `json:"parent_id"`
	Type            int            `json:"type"`
	Path            string         `json:"path"`
	ExternalURL     string         `json:"external_url"`
	Icon            string         `json:"icon"`
	Status          int            `json:"status"`
	IsHidden        int            `json:"is_hidden"`
	IsPublic        int            `json:"is_public"`
	IsSystem        int            `json:"is_system"`
	SortOrder       int            `json:"sort_order"`
	Remark          string         `json:"remark"`
	Children        []MenuTreeNode `json:"children,omitempty"`
}

type MenuDetailResp struct {
	ID              uint64   `json:"id"`
	PermissionCodes []string `json:"permission_codes"`
	Title           string   `json:"title"`
	TitleKey        *string  `json:"title_key,omitempty"`
	ParentID        uint64   `json:"parent_id"`
	Type            int      `json:"type"`
	Path            string   `json:"path"`
	ExternalURL     string   `json:"external_url"`
	Icon            string   `json:"icon"`
	Status          int      `json:"status"`
	IsHidden        int      `json:"is_hidden"`
	IsPublic        int      `json:"is_public"`
	IsSystem        int      `json:"is_system"`
	SortOrder       int      `json:"sort_order"`
	Remark          string   `json:"remark"`
}

func (n *MenuTreeNode) TreeID() uint64       { return n.ID }
func (n *MenuTreeNode) TreeParentID() uint64 { return n.ParentID }

func (n *MenuTreeNode) SetTreeChildren(children []tree.Node[uint64]) {
	if len(children) == 0 {
		n.Children = nil
		return
	}
	out := make([]MenuTreeNode, 0, len(children))
	for _, c := range children {
		out = append(out, *(c.(*MenuTreeNode)))
	}
	n.Children = out
}
