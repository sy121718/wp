package mediadto

import (
	"strconv"
	"strings"
)

type CategoryCreateReq struct {
	ParentID     uint64 `form:"parent_id" json:"parent_id"`
	CategoryName string `form:"category_name" json:"category_name" binding:"required,max=100"`
	SortOrder    int    `form:"sort_order" json:"sort_order"`
}

type CategoryUpdateReq struct {
	ID           uint64  `form:"id" json:"id" binding:"required"`
	CategoryName *string `form:"category_name" json:"category_name" binding:"omitempty,max=100"`
	ParentID     *uint64 `form:"parent_id" json:"parent_id"`
	SortOrder    *int    `form:"sort_order" json:"sort_order"`
}

type CategoryDeleteReq struct {
	ID uint64 `form:"id" json:"id" binding:"required"`
}

type AttachmentUpdateReq struct {
	ID          uint64  `form:"id" json:"id" binding:"required"`
	FileName    *string `form:"file_name" json:"file_name" binding:"omitempty,max=255"`
	CategoryID  *uint64 `form:"category_id" json:"category_id"`
	Alt         *string `form:"alt" json:"alt" binding:"omitempty,max=500"`
	Title       *string `form:"title" json:"title" binding:"omitempty,max=500"`
	Description *string `form:"description" json:"description" binding:"omitempty,max=2000"`
}

type SyncRefsReq struct {
	RefKind  string   `json:"ref_kind" form:"ref_kind"`
	RefID    string   `json:"ref_id" form:"ref_id"`
	RefTitle string   `json:"ref_title,omitempty" form:"ref_title"`
	URLs     []string `json:"urls" form:"urls"`
}

type ListReq struct {
	Page       int     `form:"page" json:"page"`
	Limit      int     `form:"limit" json:"limit"`
	FileType   string  `form:"file_type" json:"file_type"`
	CategoryID *uint64 `form:"category_id" json:"category_id"`
	Search     string  `form:"search" json:"search"`

	Uncategorized bool `form:"uncategorized" json:"uncategorized"`

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

type DetailReq struct {
	ProjectID string `form:"projectId" json:"projectId" binding:"required"`
	ID        uint64 `form:"id" json:"id" binding:"required"`
}

type DeleteReq struct {
	ProjectID string `form:"projectId" json:"projectId" binding:"required"`
	ID        uint64 `json:"id" form:"id" binding:"required"`
}

type VariantsGenerateReq struct {
	ID uint64 `json:"id" form:"id" binding:"required"`
}

type UploadReq struct {
	CategoryID string `form:"category_id"`
}

func (r *UploadReq) CategoryIDValue() *uint64 {
	raw := strings.TrimSpace(r.CategoryID)
	if raw == "" {
		return nil
	}
	id, err := strconv.ParseUint(raw, 10, 64)
	if err != nil || id == 0 {
		return nil
	}
	return &id
}

type ReplaceReq struct {
	ID uint64 `form:"id"`
}
