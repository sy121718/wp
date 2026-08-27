// Package pagecontract 定义 page 模块对外能力。
package pagecontract

import (
	"context"

	pagedto "go_wp/internal/module/page/dto"
)

// PageService 手工 Page 草稿与修订管理能力。
type PageService interface {
	Create(ctx context.Context, req *pagedto.CreateReq) (res *pagedto.PageResp, err error)
	Detail(ctx context.Context, req *pagedto.DetailReq) (res *pagedto.PageResp, err error)
	SaveDraft(ctx context.Context, req *pagedto.SaveDraftReq) (res *pagedto.PageResp, err error)
	ListRevisions(ctx context.Context, req *pagedto.RevisionReq) (res []pagedto.RevisionResp, err error)
}
