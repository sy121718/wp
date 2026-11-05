package blockcontract

import (
	"context"
	blockenums "go_wp/internal/module/block/enums"

	blockdto "go_wp/internal/module/block/dto"
)

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

type BlockService interface {
	List(ctx context.Context, req *blockdto.ListReq) (res []blockdto.BlockResp, err error)

	Detail(ctx context.Context, req *blockdto.DetailReq) (res *blockdto.BlockResp, err error)

	Create(ctx context.Context, req *blockdto.CreateReq) (res *blockdto.BlockResp, err error)

	Update(ctx context.Context, req *blockdto.UpdateReq) (res *blockdto.BlockResp, err error)

	Delete(ctx context.Context, req *blockdto.DeleteReq) (err error)

	// BulkDelete 在一个事务里删除多个块：任一失败整批回滚。
	BulkDelete(ctx context.Context, req *blockdto.BulkDeleteReq) error

	CloneAST(ctx context.Context, req *blockdto.CloneReq) (res *blockdto.CloneResp, err error)

	ListBlockSourceRefs(ctx context.Context, blockID string) ([]BlockUsage, error)
}

var (
	ErrParamRequired = blockenums.Err(blockenums.ErrBlockParamRequired)

	ErrNotFound = blockenums.Err(blockenums.ErrBlockNotFound)

	ErrProjectNotFound = blockenums.Err(blockenums.ErrProjectNotFound)

	ErrProjectRequired = blockenums.Err(blockenums.ErrBlockProjectRequired)

	ErrNameRequired = blockenums.Err(blockenums.ErrBlockNameRequired)

	ErrInvalidDoc = blockenums.Err(blockenums.ErrBlockInvalidDoc)

	ErrInvalidKind = blockenums.Err(blockenums.ErrBlockInvalidKind)

	ErrInvalidCategory = blockenums.Err(blockenums.ErrBlockInvalidCategory)

	ErrDuplicate = blockenums.Err(blockenums.ErrBlockDuplicate)

	ErrPresetUnavailable = blockenums.Err(blockenums.ErrBlockPresetUnavailable)

	ErrInvalidReuseMode = blockenums.Err(blockenums.ErrBlockInvalidReuseMode)

	ErrBlockInUse = blockenums.Err(blockenums.ErrBlockInUse)

	ErrBlockVersionConflict = blockenums.Err(blockenums.ErrBlockVersionConflict)
)

type BlockReader interface {
	List(ctx context.Context, req *blockdto.ListReq) (res []blockdto.BlockResp, err error)
	Detail(ctx context.Context, req *blockdto.DetailReq) (res *blockdto.BlockResp, err error)
}

type BlockWriter interface {
	Create(ctx context.Context, req *blockdto.CreateReq) (res *blockdto.BlockResp, err error)
	Update(ctx context.Context, req *blockdto.UpdateReq) (res *blockdto.BlockResp, err error)
	Delete(ctx context.Context, req *blockdto.DeleteReq) (err error)
}
