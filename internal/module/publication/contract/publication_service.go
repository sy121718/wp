// Package pubcontract 定义 publication 模块对外能力。
package pubcontract

import (
	"context"

	pubdto "go_wp/internal/module/publication/dto"
)

// 请求/响应 DTO 重导出：跨模块调用方只依赖 contract，不直接 import publication/dto。
type (
	ActivateReq                      = pubdto.ActivateReq
	DeactivateReq                    = pubdto.DeactivateReq
	RedirectReq                      = pubdto.RedirectReq
	RenameReservedReq                = pubdto.RenameReservedReq
	ReserveReq                       = pubdto.ReserveReq
	DeleteRoutesReq                  = pubdto.DeleteRoutesReq
	DeleteRoutesByPresentationReq    = pubdto.DeleteRoutesByPresentationReq
	ListActivePathsByPresentationReq = pubdto.ListActivePathsByPresentationReq
	ListActivePathsReq               = pubdto.ListActivePathsReq
	IsOccupiedReq                    = pubdto.IsOccupiedReq
	RouteResp                        = pubdto.RouteResp

	// 发布回执（TX-009）的两个形状：登记入参与未结案回执视图。
	// 它们此前漏在重导出列表外，结果是 page 侧的发布账本文件被迫直接 import publication/dto ——
	// 同一模块的其余 8 个文件都走 pubcontract，只有这一处破例，属遗漏而非设计。
	BeginPublishReceiptReq = pubdto.BeginPublishReceiptReq
	PendingReceiptResp     = pubdto.PendingReceiptResp
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
	// BeginPublishReceipt 在**切换访问面之前**登记一条 pending 发布回执，返回回执 id。
	//
	// 为什么必须在切换之前：切换是访问面原子的、但不可逆的副作用；登记放在之后，
	// 崩溃窗口里就查不到「这次发布发生过」，只能人工比对链接与数据库（TX-009）。
	BeginPublishReceipt(ctx context.Context, req *pubdto.BeginPublishReceiptReq) (receiptID string, err error)
	// CompletePublishReceipt 标记回执完成（数据库状态已与访问面一致）。
	CompletePublishReceipt(ctx context.Context, receiptID string) (err error)
	// AbortPublishReceipt 把回执标记为已回滚（切换尚未发生或明确失败）。
	AbortPublishReceipt(ctx context.Context, receiptID string) (err error)
	// ListPendingReceipts 列出未完成的回执（启动恢复扫描）。
	ListPendingReceipts(ctx context.Context) (list []pubdto.PendingReceiptResp, err error)
	// RenameReserved 修改页面的草稿路径占用（reserved 状态改名）。
	RenameReserved(ctx context.Context, req *pubdto.RenameReservedReq) (err error)
	// ReservePath 创建草稿路径 reserved 占用（页面创建时预留，冲突返回占用错误）。
	ReservePath(ctx context.Context, req *pubdto.ReserveReq) (err error)
	// RefreshSiteFiles 依据已激活路由重写站点 sitemap.xml / robots.txt / feed.xml
	// 与自定义 404 页（dir 为空则跳过）。
	// langs 为站点启用语言（默认语言在前，多语言 P3），defaultLang 用于 x-default；
	// 二者为空时按单语言输出（与 P3 之前一致）。
	// notFoundHTML 为站点自定义 404 页内容（projects.settings 里的 HTML）；空 = 未配置，
	// 此时删除激活目录里既有的 404.html（激活目录直接对外服务，不删等于继续返回已下线的旧页）。
	// 内容由调用方（page 装配层，持有 project 契约）传入 —— publication 不跨模块读站点设置。
	RefreshSiteFiles(ctx context.Context, projectID, baseURL, dir string, langs []string, defaultLang string, notFoundHTML string) (err error)
	// DeleteRoutesByPage 清理页面全部路径占用（页面删除时释放，幂等）。
	DeleteRoutesByPage(ctx context.Context, req *pubdto.DeleteRoutesReq) (err error)
	// ListActivePathsByPresentation 返回展示实例已激活（active/redirect）的路径集合。
	//
	// 删除实例前必须按它逐个解除访问面激活：改过 URL 的实例除新路径外还有一条
	// 旧路径的 redirect 链接，只清 DB 路由行在线上的表现仍是 301 到一个死页面。
	ListActivePathsByPresentation(ctx context.Context, req *pubdto.ListActivePathsByPresentationReq) (paths []string, err error)
	// DeleteRoutesByPresentation 清理展示实例全部路径占用（实例删除时释放，幂等）。
	// 与 DeleteRoutesByPage 分开：归属列不同，合成一个可空组合参数会让
	// 「忘了传归属者」变成静默全删。
	DeleteRoutesByPresentation(ctx context.Context, req *pubdto.DeleteRoutesByPresentationReq) (err error)
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
