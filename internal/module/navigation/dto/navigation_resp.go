package navigationdto

type SwapMoveResp struct {
	Moved bool `json:"moved"`

	Reason string `json:"reason,omitempty"`
}

type NavigationResp struct {
	ID        string  `json:"id"`
	ProjectID string  `json:"projectId"`
	Title     string  `json:"title"`
	Path      string  `json:"path"`
	Kind      string  `json:"kind"`
	ParentID  *string `json:"parentId"`
	SortOrder int     `json:"sortOrder"`

	SourceType string `json:"sourceType"`

	SourceID *string `json:"sourceId"`

	Target string `json:"target"`

	PanelBlockID *string `json:"panelBlockId"`

	PanelWidth string `json:"panelWidth"`

	UpdatedAt string `json:"updatedAt"`
}

type NavigationNode struct {
	ID         string  `json:"id"`
	Title      string  `json:"title"`
	Path       string  `json:"path"`
	SourceType string  `json:"sourceType"`
	SourceID   *string `json:"sourceId"`
	Target     string  `json:"target"`
	SortOrder  int     `json:"sortOrder"`

	PanelBlockID *string `json:"panelBlockId"`
	PanelWidth   string  `json:"panelWidth"`

	UpdatedAt string            `json:"updatedAt"`
	Children  []*NavigationNode `json:"children,omitempty"`
}
