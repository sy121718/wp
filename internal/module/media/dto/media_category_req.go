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
