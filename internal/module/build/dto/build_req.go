package builddto

type EnqueueReq struct {
	SourceType string `json:"sourceType" form:"sourceType" binding:"required"`

	SourceID string `json:"sourceId" form:"sourceId" binding:"required"`

	ProjectID string `json:"projectId" form:"projectId"`

	Lang           string `json:"lang" form:"lang"`
	Intent         string `json:"intent" form:"intent"`
	DraftVersion   int64  `json:"draftVersion" form:"draftVersion"`
	BuildInputHash string `json:"buildInputHash" form:"buildInputHash"`
}

type ListReq struct {
	ProjectID string `form:"project" json:"projectId"`
	Status    string `form:"status" json:"status"`
	Limit     int    `form:"limit" json:"limit"`
}
