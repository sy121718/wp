package navigationdto

import "strings"

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

// BulkDeleteReq 批量删除导航项；整批在一个事务里，任一失败全量回滚。
type BulkDeleteReq struct {
	IDs []string `json:"ids" form:"ids" binding:"required,min=1,dive,required"`
}

// AddSourceReq 从页面/内容等来源批量加入菜单：候选规则与写入都在服务层，
// 同一事务里全成或全败。
type AddSourceReq struct {
	ProjectID  string   `json:"projectId" form:"projectId"`
	Kind       string   `json:"kind" form:"kind" binding:"required"`
	SourceType string   `json:"sourceType" form:"sourceType"`
	IDs        []string `json:"ids" form:"ids"`
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

// AddSourceForm 是「从来源批量加入导航」的表单输入：
// projectId / kind / sourceType 定作用域，ids 是重复提交的候选标识。
type AddSourceForm struct {
	ProjectID  string   `json:"projectId" form:"projectId" binding:"required"`
	Kind       string   `json:"kind" form:"kind" binding:"required"`
	SourceType string   `json:"sourceType" form:"sourceType" binding:"required"`
	IDs        []string `json:"ids" form:"ids" binding:"required"`
}

// Req 把表单投影成 service 入参。
func (f *AddSourceForm) Req() *AddSourceReq {
	return &AddSourceReq{
		ProjectID:  strings.TrimSpace(f.ProjectID),
		Kind:       strings.TrimSpace(f.Kind),
		SourceType: strings.TrimSpace(f.SourceType),
		IDs:        f.IDs,
	}
}

// PanelForm 是导航项悬浮面板的编辑表单（设置已有面板 / 新建面板共用字段）。
type PanelForm struct {
	ProjectID         string `json:"projectId" form:"projectId"`
	ID                string `json:"id" form:"id"`
	Kind              string `json:"kind" form:"kind"`
	Title             string `json:"title" form:"title"`
	Path              string `json:"path" form:"path"`
	Target            string `json:"target" form:"target"`
	PanelBlockID      string `json:"panelBlockId" form:"panelBlockId"`
	PanelWidth        string `json:"panelWidth" form:"panelWidth"`
	ExpectedUpdatedAt string `json:"expectedUpdatedAt" form:"expectedUpdatedAt"`
}

// BulkDeleteForm 是导航批量删除的表单输入：ids 重复提交。
type BulkDeleteForm struct {
	IDs []string `json:"ids" form:"ids" binding:"required"`
}
