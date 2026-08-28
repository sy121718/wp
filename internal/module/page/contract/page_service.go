// Package pagecontract 定义 page 模块对外能力。
package pagecontract

import (
	"context"

	pagedto "go_wp/internal/module/page/dto"
)

// PageService 手工 Page 草稿、修订与发布管理能力。
type PageService interface {
	Create(ctx context.Context, req *pagedto.CreateReq) (res *pagedto.PageResp, err error)
	// List 列出全部页面摘要（不含草稿文档）。
	List(ctx context.Context) (res []pagedto.PageResp, err error)
	Detail(ctx context.Context, req *pagedto.DetailReq) (res *pagedto.PageResp, err error)
	SaveDraft(ctx context.Context, req *pagedto.SaveDraftReq) (res *pagedto.PageResp, err error)
	ListRevisions(ctx context.Context, req *pagedto.RevisionReq) (res []pagedto.RevisionResp, err error)

	// Build 基于当前草稿构建并暂存产物（不激活线上）。
	Build(ctx context.Context, req *pagedto.BuildReq) (res *pagedto.PublishResp, err error)
	// Publish 激活暂存产物。
	Publish(ctx context.Context, req *pagedto.PublishReq) (res *pagedto.PublishResp, err error)
	// Rollback 秒级回滚到历史产物。
	Rollback(ctx context.Context, req *pagedto.RollbackReq) (res *pagedto.PublishResp, err error)
	// UpdateURL 修改访问路径，旧路径按策略 301 或取消激活。
	UpdateURL(ctx context.Context, req *pagedto.UpdateURLReq) (res *pagedto.PublishResp, err error)
}
