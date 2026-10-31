package contentdto

type CreateReq struct {
	EntityType string         `json:"entityType" form:"entityType" binding:"required"`
	Slug       string         `json:"slug" form:"slug" binding:"required"`
	Data       map[string]any `json:"data" form:"data"`
}

type UpdateReq struct {
	ID   string         `json:"id" form:"id" binding:"required"`
	Data map[string]any `json:"data" form:"data"`
}

type GetReq struct {
	ID string `form:"id" binding:"required"`
}

type ListReq struct {
	EntityType string `form:"entityType"`
	Keyword    string `form:"keyword"`
	Limit      int    `form:"limit"`
	Offset     int    `form:"offset"`
}

type DeleteReq struct {
	ID string `form:"id" binding:"required"`
}
