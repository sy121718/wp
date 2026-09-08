// Package blockcontract 定义 block 模块对外契约。
package blockcontract

import (
	"context"

	blockdto "go_wp/internal/module/block/dto"
)

// 请求/响应 DTO 重导出：跨模块调用方只依赖 contract，不直接 import block/dto。
type (
	ListReq   = blockdto.ListReq
	DetailReq = blockdto.DetailReq
	CreateReq = blockdto.CreateReq
	UpdateReq = blockdto.UpdateReq
	DeleteReq = blockdto.DeleteReq
	CloneReq  = blockdto.CloneReq
	CloneResp = blockdto.CloneResp
	BlockResp = blockdto.BlockResp
)

// BlockService 全局块能力：跨页面复用的结构片段（页眉/页脚/区块）。
type BlockService interface {
	// List 列出工程全部块（kind 可选过滤：block/header/footer）。
	List(ctx context.Context, req *blockdto.ListReq) (res []blockdto.BlockResp, err error)
	// Detail 按 ID 查询块。
	Detail(ctx context.Context, req *blockdto.DetailReq) (res *blockdto.BlockResp, err error)
	// Create 新建块（同工程名称唯一，文档与页面文档同构）。
	Create(ctx context.Context, req *blockdto.CreateReq) (res *blockdto.BlockResp, err error)
	// Update 更新块（名称/类型/复用方式/文档整树保存）。
	Update(ctx context.Context, req *blockdto.UpdateReq) (res *blockdto.BlockResp, err error)
	// Delete 删除块。global 块被引用且未 Force 时返回 ErrBlockInUse；
	// template 块（页面已持有副本）或 Force 删除无引用副作用，直接删除。
	Delete(ctx context.Context, req *blockdto.DeleteReq) (err error)
	// CloneAST 复制块文档为独立 AST（全部节点重生成 ID，docs/02-D §5.2「插入-复制」动作）。
	// 返回的文档与源块脱钩：此后源块修改不传播到已并入的页面。
	CloneAST(ctx context.Context, req *blockdto.CloneReq) (res *blockdto.CloneResp, err error)
}
