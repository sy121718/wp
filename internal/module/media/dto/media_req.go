package mediadto

// ListReq 附件列表查询。
type ListReq struct {
	Page       int     `form:"page" json:"page"`
	Limit      int     `form:"limit" json:"limit"`
	FileType   string  `form:"file_type" json:"file_type"`
	CategoryID *uint64 `form:"category_id" json:"category_id"`
	Search     string  `form:"search" json:"search"`
}

func (r *ListReq) GetPage() int {
	if r.Page <= 0 {
		return 1
	}
	return r.Page
}

func (r *ListReq) GetLimit() int {
	if r.Limit <= 0 || r.Limit > 100 {
		return 20
	}
	return r.Limit
}

func (r *ListReq) GetOffset() int {
	return (r.GetPage() - 1) * r.GetLimit()
}

// DetailReq 附件详情。
type DetailReq struct {
	ID uint64 `form:"id" json:"id" binding:"required"`
}

// DeleteReq 删除附件。
type DeleteReq struct {
	ID uint64 `json:"id" binding:"required"`
}