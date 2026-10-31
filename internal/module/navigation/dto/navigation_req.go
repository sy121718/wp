package navigationdto

type CreateReq struct {
	ProjectID string `json:"projectId" form:"projectId" binding:"required"`
	Title     string `json:"title" form:"title" binding:"required"`
	Path      string `json:"path" form:"path" binding:"required"`
	Kind      string `json:"kind" form:"kind" binding:"required"`

	ParentID *string `json:"parentId" form:"parentId"`

	SortOrder int `json:"sortOrder" form:"sortOrder"`

	SourceType string `json:"sourceType" form:"sourceType"`

	SourceID *string `json:"sourceId" form:"sourceId"`

	Target string `json:"target" form:"target"`

	PanelBlockID *string `json:"panelBlockId" form:"panelBlockId"`

	PanelWidth string `json:"panelWidth" form:"panelWidth"`
}

type UpdateReq struct {
	ID        string  `json:"id" form:"id" binding:"required"`
	Title     *string `json:"title" form:"title"`
	Path      *string `json:"path" form:"path"`
	Kind      *string `json:"kind" form:"kind"`
	ParentID  *string `json:"parentId" form:"parentId"`
	SortOrder *int    `json:"sortOrder" form:"sortOrder"`

	SourceType *string `json:"sourceType" form:"sourceType"`
	SourceID   *string `json:"sourceId" form:"sourceId"`
	Target     *string `json:"target" form:"target"`

	PanelBlockID *string `json:"panelBlockId" form:"panelBlockId"`

	PanelWidth *string `json:"panelWidth" form:"panelWidth"`

	ExpectedUpdatedAt *string `json:"expectedUpdatedAt" form:"expectedUpdatedAt"`
}

type GetReq struct {
	ID string `form:"id" binding:"required"`
}

type ListReq struct {
	ProjectID string `form:"projectId" binding:"required"`
	Kind      string `form:"kind"`
}

type DeleteReq struct {
	ID string `json:"id" form:"id" binding:"required"`
}

type MoveReq struct {
	ID        string `json:"id" form:"id" binding:"required"`
	SortOrder *int   `json:"sortOrder" form:"sortOrder"`
}

type SwapMoveReq struct {
	ID string `json:"id" form:"id" binding:"required"`

	ProjectID string `json:"projectId" form:"projectId"`

	Dir string `json:"dir" form:"dir" binding:"required"`
}
