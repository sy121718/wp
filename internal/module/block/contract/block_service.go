// Package blockcontract 定义 block 模块对外契约。
package blockcontract

import (
	"errors"

	"context"
	blockenums "go_wp/internal/module/block/enums"

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
	// ListBlockSourceRefs 列出**其它全局块**文档树里对该块的引用（审计 ARCH-02：块引用块）。
	//
	// 与 page / presentation / contenttemplate 契约上的同名方法一起，由装配层合并成
	// 块删除保护的完整判据；块自己那一份必须由本模块提供 —— 嵌套引用写在 blocks.document
	// 这个 JSONB 里，别的模块看不见，而它此前完全不在判据内（删掉内层块，外层块在下次
	// 构建时静默少一段）。
	ListBlockSourceRefs(ctx context.Context, blockID string) ([]BlockUsage, error)
}

// 本包 sentinel error：block 模块对外的错误契约。
//
// 错误是跨模块契约的一部分：调用方（如 dashboard）需按 errors.Is 精确分类业务错误，
// 而不得 import block 模块的 service 实现，故 sentinel 定义在 contract 包。
// service 层通过重导出（blockservice 内 `var ErrXxx = blockcontract.ErrXxx`）复用同一实例，
// 保证 errors.Is 判等一致。Error() 文案与 blockenums 常量一致，前端响应文案不变。
var (
	// ErrParamRequired 请求参数缺失（nil 请求、空/空白 ID、空 projectID 等）。
	ErrParamRequired = errors.New(blockenums.ErrBlockParamRequired)
	// ErrNotFound 全局块不存在。
	ErrNotFound = errors.New(blockenums.ErrBlockNotFound)
	// ErrProjectNotFound 工程不存在。
	ErrProjectNotFound = errors.New(blockenums.ErrProjectNotFound)
	// ErrProjectRequired 缺可作用域的工程（DB-009）：只带 id 的入口逐工程定位时工程清单为空。
	ErrProjectRequired = errors.New(blockenums.ErrBlockProjectRequired)
	// ErrNameRequired 块名称缺失。
	ErrNameRequired = errors.New(blockenums.ErrBlockNameRequired)
	// ErrInvalidDoc 块文档不合法（与页面文档同构校验失败）。
	ErrInvalidDoc = errors.New(blockenums.ErrBlockInvalidDoc)
	// ErrInvalidKind 块类型不合法（非 block/header/footer）。
	ErrInvalidKind = errors.New(blockenums.ErrBlockInvalidKind)
	// ErrInvalidCategory 块分类不合法（未通过白名单）。
	ErrInvalidCategory = errors.New(blockenums.ErrBlockInvalidCategory)
	// ErrDuplicate 同工程同名块已存在。
	ErrDuplicate = errors.New(blockenums.ErrBlockDuplicate)
	// ErrInvalidReuseMode 块复用方式不合法（非 global/template）。
	ErrInvalidReuseMode = errors.New(blockenums.ErrBlockInvalidReuseMode)
	// ErrBlockInUse global 块仍被页面/主题引用：删除或切换 global→template 前须先解除引用或 Force。
	ErrBlockInUse = errors.New(blockenums.ErrBlockInUse)
)

// BlockReader / BlockWriter 是 AI 工具用的窄接口。
//
// 刻意不直接复用 BlockService：它上面还有 CloneAST（编辑器动作）与
// ListBlockSourceRefs（删除保护的判据来源），两者都不是「让模型读/改块的元数据」
// 这件事需要的能力。只读的 List/Detail 单独声明，改写能力也只有这三个 ——
// 复用宽接口会让「这个工具能做什么」变得看不出来。
type BlockReader interface {
	List(ctx context.Context, req *blockdto.ListReq) (res []blockdto.BlockResp, err error)
	Detail(ctx context.Context, req *blockdto.DetailReq) (res *blockdto.BlockResp, err error)
}

type BlockWriter interface {
	Create(ctx context.Context, req *blockdto.CreateReq) (res *blockdto.BlockResp, err error)
	Update(ctx context.Context, req *blockdto.UpdateReq) (res *blockdto.BlockResp, err error)
	Delete(ctx context.Context, req *blockdto.DeleteReq) (err error)
}
