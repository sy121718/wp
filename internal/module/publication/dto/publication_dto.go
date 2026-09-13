package pubdto

import "time"

// ActivateReq 激活请求：把 reserved 占用升级为 active 并绑定产物。
//
// 归属者二选一：PageID（手工页面）或 PresentationID（自动发布实例）。
// page_routes 的 CHECK 约束要求二者恰好一个非空（init_builder_schema.sql），
// 「二选一」无法用 binding:"required" 表达，由 service 层校验（parseRouteOwner）。
type ActivateReq struct {
	ProjectID string `json:"projectId" binding:"required"`
	Path      string `json:"path" binding:"required"`
	PageID    string `json:"pageId"`
	// PresentationID 自动发布实例归属（presentation_instances.id）。
	// 详情页发布实例的 URL 占用此前根本没登记进 page_routes，导致页面侧
	// 占用预检查不出详情页抢路径；这个字段是补上那半边的入口。
	PresentationID string `json:"presentationId"`
	ArtifactID     string `json:"artifactId" binding:"required"`
	Action         string `json:"action"`
}

// DeactivateReq 取消激活请求。
//
// 归属者可空：指定时只取消该归属者的占用（展示实例必须指定 —— 它的行
// page_id 为 NULL，历史判据 page_id IS NOT NULL 够不着它）；都不指定时
// 保持「取消该路径上任意页面占用」的既有语义，page 侧调用逐字不变。
type DeactivateReq struct {
	ProjectID string `json:"projectId" binding:"required"`
	Path      string `json:"path" binding:"required"`
	PageID    string `json:"pageId"`
	// PresentationID 展示实例归属；指定时按它精确匹配（page_id 为 NULL 的行）。
	PresentationID string `json:"presentationId"`
}

// RedirectReq 旧路径重定向标记请求；ArtifactID 允许为空（重定向产物不入库）。
//
// 归属者二选一同 ActivateReq：改 URL 后旧路径要由「原来的主人」标记为
// redirect，归属者不对就会被判成抢占他人路径。
type RedirectReq struct {
	ProjectID      string `json:"projectId" binding:"required"`
	OldPath        string `json:"oldPath" binding:"required"`
	PageID         string `json:"pageId"`
	PresentationID string `json:"presentationId"`
	ArtifactID     string `json:"artifactId"`
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
	ProjectID string  `json:"projectId"`
	Path      string  `json:"path"`
	PageID    *string `json:"pageId,omitempty"`
	// PresentationID 展示实例归属（页面占用的行此字段为空）。
	PresentationID *string   `json:"presentationId,omitempty"`
	RouteKind      string    `json:"routeKind"`
	ArtifactID     *string   `json:"artifactId,omitempty"`
	UpdatedAt      time.Time `json:"updatedAt"`
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

// ListActivePathsReq 查询页面已激活的路径集合（页面删除时清理访问面文件用）。
type ListActivePathsReq struct {
	ProjectID string `json:"projectId" binding:"required"`
	PageID    string `json:"pageId" binding:"required"`
}

// IsOccupiedReq 查询路径是否被其他实体占用（用于页面创建/发布前的预检）。
//
// 两个排除项各排除自己那一行：改 URL 时「自己占着旧路径」不算冲突。
// 展示实例改 URL 必须传 ExcludePresentationID —— 它的行 page_id 为 NULL，
// 用 ExcludePageID 排除不掉自己，会把自身占用判成冲突。
type IsOccupiedReq struct {
	ProjectID     string `json:"projectId" binding:"required"`
	Path          string `json:"path" binding:"required"`
	ExcludePageID string `json:"excludePageId"`
	// ExcludePresentationID 排除展示实例自身的占用行。
	ExcludePresentationID string `json:"excludePresentationId"`
}

// ListActivePathsByPresentationReq 查询展示实例已激活的路径集合（实例删除时清理访问面文件用）。
type ListActivePathsByPresentationReq struct {
	ProjectID      string `json:"projectId" binding:"required"`
	PresentationID string `json:"presentationId" binding:"required"`
}

// DeleteRoutesByPresentationReq 清理展示实例全部路径占用（实例删除时释放）。
//
// 与 DeleteRoutesByPage 并存而不是合并：两者的归属列不同，合并成一个可空
// 组合参数会让「忘了传归属者」变成静默全删，比多一个方法危险得多。
type DeleteRoutesByPresentationReq struct {
	ProjectID      string `json:"projectId" binding:"required"`
	PresentationID string `json:"presentationId" binding:"required"`
}
