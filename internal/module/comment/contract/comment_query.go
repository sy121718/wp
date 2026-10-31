package commentcontract

import (
	"context"

	commentdto "go_wp/internal/module/comment/dto"
)

type QueryReader interface {
	AdminList(ctx context.Context, req *commentdto.AdminListReq) (res *commentdto.AdminListResp, err error)
}

type ReviewWriter interface {
	Review(ctx context.Context, req *commentdto.ReviewReq) (res *commentdto.ReviewResp, err error)
}
