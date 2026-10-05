package mediadto

// ListReq 附件列表查询。
type ListReq struct {
	Page       int     `form:"page" json:"page"`
	Limit      int     `form:"limit" json:"limit"`
	FileType   string  `form:"file_type" json:"file_type"`
	CategoryID *uint64 `form:"category_id" json:"category_id"`
	Search     string  `form:"search" json:"search"`
	// Uncategorized 只取没有分类的附件（category_id IS NULL）。
	//
	// 单开一个布尔而不是让调用方传 CategoryID=0 表示「未分类」：CategoryID 的判据是
	// `> 0 才过滤`，所以 0 等于**不过滤**（返回全部），与「未分类」正好相反。
	// AI 工具层原先只能传 0，用户问「还有哪些图没归类」时它拿到的是全部媒体 ——
	// 答案看起来很合理，只是完全不对。
	Uncategorized bool `form:"uncategorized" json:"uncategorized"`
	// Cursor 是上一页最后一条记录的不透明游标；传入后优先于 Page。
	Cursor string `form:"cursor" json:"cursor"`
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
	ProjectID string `form:"projectId" json:"projectId" binding:"required"`
	ID        uint64 `form:"id" json:"id" binding:"required"`
}

// DeleteReq 删除附件。
type DeleteReq struct {
	ProjectID string `form:"projectId" json:"projectId" binding:"required"`
	ID        uint64 `json:"id" binding:"required"`
}

// VariantsGenerateReq 重新生成图片变体（POST /api/media/variants/generate）。
type VariantsGenerateReq struct {
	ID uint64 `json:"id" binding:"required"`
}
