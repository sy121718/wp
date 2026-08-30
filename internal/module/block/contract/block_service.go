// Package blockcontract 定义 block 模块对外契约。
package blockcontract

import (
	"context"

	blockdto "go_wp/internal/module/block/dto"
)

// BlockService 全局块能力：跨页面复用的结构片段（页眉/页脚/区块）。
type BlockService interface {
	// List 列出工程全部块（kind 可选过滤：block/header/footer）。
	List(ctx context.Context, req *blockdto.ListReq) (res []blockdto.BlockResp, err error)
	// Detail 按 ID 查询块。
	Detail(ctx context.Context, req *blockdto.DetailReq) (res *blockdto.BlockResp, err error)
	// Create 新建块（同工程名称唯一，文档与页面文档同构）。
	Create(ctx context.Context, req *blockdto.CreateReq) (res *blockdto.BlockResp, err error)
	// Update 更新块（名称/类型/文档整树保存）。
	Update(ctx context.Context, req *blockdto.UpdateReq) (res *blockdto.BlockResp, err error)
	// Delete 删除块（引用方 stale 由调用方编排）。
	Delete(ctx context.Context, req *blockdto.DeleteReq) (err error)
}
