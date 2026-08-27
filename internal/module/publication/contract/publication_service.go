// Package pubcontract 定义 publication 模块对外能力。
package pubcontract

import (
	"context"

	pubdto "go_wp/internal/module/publication/dto"
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
}
