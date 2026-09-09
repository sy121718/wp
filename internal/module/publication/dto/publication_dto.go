package pubdto

import "time"

// ActivateReq 激活请求：把 reserved 占用升级为 active 并绑定产物。
type ActivateReq struct {
	ProjectID  string `json:"projectId" binding:"required"`
	Path       string `json:"path" binding:"required"`
	PageID     string `json:"pageId" binding:"required"`
	ArtifactID string `json:"artifactId" binding:"required"`
	Action     string `json:"action"`
}

// DeactivateReq 取消激活请求。
type DeactivateReq struct {
	ProjectID string `json:"projectId" binding:"required"`
	Path      string `json:"path" binding:"required"`
}

// RedirectReq 旧路径重定向标记请求；ArtifactID 允许为空（重定向产物不入库）。
type RedirectReq struct {
	ProjectID  string `json:"projectId" binding:"required"`
	OldPath    string `json:"oldPath" binding:"required"`
	PageID     string `json:"pageId" binding:"required"`
	ArtifactID string `json:"artifactId"`
}

// RenameReservedReq 草稿路径占用改名请求（reserved → reserved）。
type RenameReservedReq struct {
	ProjectID string `json:"projectId" binding:"required"`
	PageID    string `json:"pageId" binding:"required"`
	OldPath   string `json:"oldPath" binding:"required"`
	NewPath   string `json:"newPath" binding:"required"`
	// OnlyReserved 只迁移 reserved 行，跳过本页 active/redirect 行。
	// 多语言 P3：改某语言 URL 时，其他语言的 active 行绝不能被顺带改名
	//（否则 /zh-CN/about 的激活行会被迁到新路径，线上路由丢失）。
	OnlyReserved bool `json:"onlyReserved"`
}

// RouteResp 路由占用投影。
type RouteResp struct {
	ProjectID  string    `json:"projectId"`
	Path       string    `json:"path"`
	PageID     *string   `json:"pageId,omitempty"`
	RouteKind  string    `json:"routeKind"`
	ArtifactID *string   `json:"artifactId,omitempty"`
	UpdatedAt  time.Time `json:"updatedAt"`
}

// ReserveReq 创建草稿路径 reserved 占用（页面创建时预留）。
type ReserveReq struct {
	ProjectID string `json:"projectId" binding:"required"`
	Path      string `json:"path" binding:"required"`
	PageID    string `json:"pageId" binding:"required"`
}

// DeleteRoutesReq 清理页面全部路径占用（页面删除时释放）。
type DeleteRoutesReq struct {
	ProjectID string `json:"projectId" binding:"required"`
	PageID    string `json:"pageId" binding:"required"`
}

// IsOccupiedReq 查询路径是否被其他实体占用（用于页面创建/发布前的预检）。
type IsOccupiedReq struct {
	ProjectID     string `json:"projectId" binding:"required"`
	Path          string `json:"path" binding:"required"`
	ExcludePageID string `json:"excludePageId"`
}
