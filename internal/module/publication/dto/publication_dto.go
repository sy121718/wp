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
