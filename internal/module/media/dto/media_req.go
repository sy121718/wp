package mediadto

// CategoryCreateReq 新建文件分类（无限级，ParentID=0 为顶级）。
type CategoryCreateReq struct {
	ParentID     uint64 `form:"parent_id" json:"parent_id"`
	CategoryName string `form:"category_name" json:"category_name" binding:"required,max=100"`
	SortOrder    int    `form:"sort_order" json:"sort_order"`
}

// CategoryUpdateReq 更新分类（改名 / 移动父级 / 排序；nil 字段不改）。
type CategoryUpdateReq struct {
	ID           uint64  `form:"id" json:"id" binding:"required"`
	CategoryName *string `form:"category_name" json:"category_name" binding:"omitempty,max=100"`
	ParentID     *uint64 `form:"parent_id" json:"parent_id"`
	SortOrder    *int    `form:"sort_order" json:"sort_order"`
}

// CategoryDeleteReq 删除分类。
type CategoryDeleteReq struct {
	ID uint64 `form:"id" json:"id" binding:"required"`
}

// AttachmentUpdateReq 更新附件元数据（文件名 / 分类 / alt / 标题 / 描述；nil 字段不改）。
type AttachmentUpdateReq struct {
	ID          uint64  `form:"id" json:"id" binding:"required"`
	FileName    *string `form:"file_name" json:"file_name" binding:"omitempty,max=255"`
	CategoryID  *uint64 `form:"category_id" json:"category_id"`
	Alt         *string `form:"alt" json:"alt" binding:"omitempty,max=500"`
	Title       *string `form:"title" json:"title" binding:"omitempty,max=500"`
	Description *string `form:"description" json:"description" binding:"omitempty,max=2000"`
}

// SyncRefsReq 构建期引用同步请求（refs 的写入侧入口）。
// 语义为「全量替换」：该引用方当前引用的 URL 集合即为最终集合，
// 不再出现的附件会被移除引用（差集增删，单条 SQL 原子更新）。
type SyncRefsReq struct {
	RefKind  string   `json:"ref_kind"`            // 引用方类型，如 page
	RefID    string   `json:"ref_id"`              // 引用方标识，如页面 ID
	RefTitle string   `json:"ref_title,omitempty"` // 引用方标题
	URLs     []string `json:"urls"`                // 该引用方产物中出现的媒体 URL
}

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
