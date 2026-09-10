// Package pubcontract 定义 publication 模块对外能力。
package pubcontract

import (
	"context"

	pubdto "go_wp/internal/module/publication/dto"
)

// 请求/响应 DTO 重导出：跨模块调用方只依赖 contract，不直接 import publication/dto。
type (
	ActivateReq        = pubdto.ActivateReq
	DeactivateReq      = pubdto.DeactivateReq
	RedirectReq        = pubdto.RedirectReq
	RenameReservedReq  = pubdto.RenameReservedReq
	ReserveReq         = pubdto.ReserveReq
	DeleteRoutesReq    = pubdto.DeleteRoutesReq
	ListActivePathsReq = pubdto.ListActivePathsReq
	IsOccupiedReq      = pubdto.IsOccupiedReq
	RouteResp          = pubdto.RouteResp
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
	// langs 为站点启用语言（默认语言在前，多语言 P3），defaultLang 用于 x-default；
	// 二者为空时按单语言输出（与 P3 之前一致）。
	RefreshSiteFiles(ctx context.Context, projectID, baseURL, dir string, langs []string, defaultLang string) (err error)
	// DeleteRoutesByPage 清理页面全部路径占用（页面删除时释放，幂等）。
	DeleteRoutesByPage(ctx context.Context, req *pubdto.DeleteRoutesReq) (err error)
	// ListActivePaths 返回页面已激活（active/redirect）的路径集合。
	//
	// 调用方（页面删除）须按这些路径解除访问面激活：/site 直接服务 active 目录的
	// 文件系统状态，只清 DB 路由行不会让已删除内容下线——符号链接还在，内容就还在。
	ListActivePaths(ctx context.Context, req *pubdto.ListActivePathsReq) (paths []string, err error)
	// IsPathOccupied 查询路径是否被其他实体占用（页面创建/发布前预检）。
	IsPathOccupied(ctx context.Context, req *pubdto.IsOccupiedReq) (occupied bool, err error)
	// ListReferencedArtifactIDs 返回全部被路由引用的产物行 ID（产物 GC 的保护集合）。
	// 路由指向的产物文件被删除意味着线上直接 404，且路由行不会因文件消失而失效。
	ListReferencedArtifactIDs(ctx context.Context) (ids []string, err error)
}
