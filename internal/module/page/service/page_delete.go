package pageservice

import (
	"context"
	"fmt"
	"strings"
	"time"

	pagedto "go_wp/internal/module/page/dto"
	pubcontract "go_wp/internal/module/publication/contract"
)

// Delete 软删页面：deleted_at 置时间（审计留痕，行保留），并释放该页面
// 全部路径占用（reserved/active/redirect）。释放后同路径可被新页面重新创建，
// 解决「软删后路由残留、路径永久占用」的能力缺口。
// nil/空 ID 属于请求不合法（ErrInvalidParam）；页面不存在或已软删统一返回 ErrPageNotFound。
//
// 顺序：解除访问面激活（删 active 符号链接）→ 清理路由占用（publication contract）
// → 软删页面。前两步失败即中断（页面记录完整，可重试）；第三步失败时访问面与
// 路由已下线，页面记录仍在，属可恢复状态。
//
// 为什么必须先解激活：/site 直接服务 active 目录的文件系统状态（不查 DB），
// 只清路由行不会让内容下线；而清完路由就查不到「该删哪个链接」了，
// 残留符号链接会变成不可恢复的幽灵页面。
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
		// 先按已激活路径把页面从访问面下线，再清 DB 路由占用。
		paths, lerr := s.routes.ListActivePaths(ctx, &pubcontract.ListActivePathsReq{
			ProjectID: page.ProjectID, PageID: req.ID,
		})
		if lerr != nil {
			return lerr
		}
		if err = s.deactivatePaths(paths); err != nil {
			return err
		}
		if rerr := s.routes.DeleteRoutesByPage(ctx, &pubcontract.DeleteRoutesReq{
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

// deactivatePaths 解除一组路径的访问面激活（删除 active 符号链接，幂等）。
// publication 为 nil（降级装配 / 单元测试）时跳过。
func (s *Service) deactivatePaths(paths []string) error {
	if s.publication == nil {
		return nil
	}
	for _, p := range paths {
		if p == "" {
			continue
		}
		if derr := s.publication.Deactivate(p); derr != nil {
			return fmt.Errorf("解除访问面激活失败 %s: %w", p, derr)
		}
	}
	return nil
}
