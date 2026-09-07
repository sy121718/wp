package pageservice

import (
	"context"
	"strings"
	"time"

	pagedto "go_wp/internal/module/page/dto"
	pubdto "go_wp/internal/module/publication/dto"
)

// Delete 软删页面：deleted_at 置时间（审计留痕，行保留），并释放该页面
// 全部路径占用（reserved/active/redirect）。释放后同路径可被新页面重新创建，
// 解决「软删后路由残留、路径永久占用」的能力缺口。
// nil/空 ID 属于请求不合法（ErrInvalidParam）；页面不存在或已软删统一返回 ErrPageNotFound。
//
// 顺序：先清理路由（publication contract），再软删页面——即使软删失败，
// 路由已清是可恢复的（页面仍在，后续 SaveDraft 可重建路由）；反之若先删
// 页面再清路由，清路由失败会导致「页面已删但路径残留、同路径永久无法新建」。
func (s *Service) Delete(ctx context.Context, req *pagedto.DeleteReq) (err error) {
	if req == nil || strings.TrimSpace(req.ID) == "" {
		return ErrInvalidParam
	}
	// 先查页面拿 projectID（路由清理需要 project 维度）。
	page, err := s.model.GetByID(ctx, req.ID)
	if err != nil {
		return mapPersistenceError(err)
	}
	if s.routes != nil {
		if rerr := s.routes.DeleteRoutesByPage(ctx, &pubdto.DeleteRoutesReq{
			ProjectID: page.ProjectID, PageID: req.ID,
		}); rerr != nil {
			return rerr
		}
	}
	if err = s.model.SoftDelete(ctx, req.ID, time.Now().UTC()); err != nil {
		return mapPersistenceError(err)
	}
	return nil
}
