// Package pubcontract 定义 publication 模块对外能力。
package pubcontract

import (
	"context"

	pubdto "go_wp/internal/module/publication/dto"
)

// 请求/响应 DTO 重导出：跨模块调用方只依赖 contract，不直接 import publication/dto。
type (
	ActivateReq       = pubdto.ActivateReq
	DeactivateReq     = pubdto.DeactivateReq
	RedirectReq       = pubdto.RedirectReq
	RenameReservedReq = pubdto.RenameReservedReq
	ReserveReq        = pubdto.ReserveReq
	DeleteRoutesReq   = pubdto.DeleteRoutesReq
	IsOccupiedReq     = pubdto.IsOccupiedReq
	RouteResp         = pubdto.RouteResp
)

// PublicationService URL 占用、激活与回滚控制能力。
type PublicationService interface {
	// Activate 把路径占用从 reserved 切换为 active 并记录回执。
	Activate(ctx context.Context, req *pubdto.ActivateReq) (res *pubdto.RouteResp, err error)
	// Deactivate 取消路径占用（幂等）。
	Deactivate(ctx context.Context, req *pubdto.DeactivateReq) (err error)
	// Redirect 把旧路径标记为指向新路径的重定向。
	Redirect(ctx context.Context, req *pubdto.RedirectReq) (res *pubdto.RouteResp, err error)
	// RollbackReceipts 把 pending 回执批量标记为 rolled_back（启动恢复用）。
	RollbackReceipts(ctx context.Context) (count int64, err error)
	// RenameReserved 修改页面的草稿路径占用（reserved 状态改名）。
	RenameReserved(ctx context.Context, req *pubdto.RenameReservedReq) (err error)
	// ReservePath 创建草稿路径 reserved 占用（页面创建时预留，冲突返回占用错误）。
	ReservePath(ctx context.Context, req *pubdto.ReserveReq) (err error)
	// RefreshSiteFiles 依据已激活路由重写站点 sitemap.xml / robots.txt（dir 为空则跳过）。
	RefreshSiteFiles(ctx context.Context, projectID, baseURL, dir string) (err error)
	// DeleteRoutesByPage 清理页面全部路径占用（页面删除时释放，幂等）。
	DeleteRoutesByPage(ctx context.Context, req *pubdto.DeleteRoutesReq) (err error)
	// IsPathOccupied 查询路径是否被其他实体占用（页面创建/发布前预检）。
	IsPathOccupied(ctx context.Context, req *pubdto.IsOccupiedReq) (occupied bool, err error)
}
