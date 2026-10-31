package commentdto

type ListReq struct {
	ProjectID string `json:"projectId" form:"projectId"`

	EntityType string `json:"entityType" form:"entityType"`

	EntityID string `json:"entityId" form:"entityId"`

	Page int `json:"page" form:"page"`

	PageSize int `json:"pageSize" form:"pageSize"`
}

type SubmitReq struct {
	ProjectID  string
	EntityType string
	EntityID   string

	UserID uint64

	ParentID int64

	Body string

	IPHash string
}

type AdminListReq struct {
	ProjectID string `json:"projectId" form:"projectId"`

	Status string `json:"status" form:"status"`

	EntityType string `json:"entityType" form:"entityType"`

	Keyword  string `json:"keyword" form:"keyword"`
	Page     int    `json:"page" form:"page"`
	PageSize int    `json:"pageSize" form:"pageSize"`
}

type ReviewReq struct {
	ProjectID string `json:"projectId" form:"projectId"`

	IDs []int64 `json:"ids" form:"ids"`

	Status string `json:"status" form:"status"`

	ReviewerID uint64 `json:"-" form:"-"`
}
