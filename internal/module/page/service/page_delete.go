package pageservice

import (
	"context"
	"fmt"
	"strings"
	"time"

	mediacontract "go_wp/internal/module/media/contract"
	pagedto "go_wp/internal/module/page/dto"
	pubcontract "go_wp/internal/module/publication/contract"

	"gorm.io/gorm"
)

// Delete 软删页面：deleted_at 置时间（审计留痕，行保留），并释放该页面
// 全部路径占用（reserved/active/redirect）。释放后同路径可被新页面重新创建，
// 解决「软删后路由残留、路径永久占用」的能力缺口。
// nil/空 ID 属于请求不合法（ErrInvalidParam）；页面不存在或已软删统一返回 ErrPageNotFound。
//
// 顺序：解除访问面激活（删 active 符号链接）→ 媒体引用清理 → **同一事务**里
// 清理路由占用 + 软删页面。
//
// 为什么必须先解激活：/site 直接服务 active 目录的文件系统状态（不查 DB），
// 只清路由行不会让内容下线；而清完路由就查不到「该删哪个链接」了，
// 残留符号链接会变成不可恢复的幽灵页面。
//
// 可重入：前三步都是幂等的（未激活时 Deactivate 返回 nil；DeleteRoutesByPage 无行
// 也不报错；媒体引用同步按 refKind+refID 全量替换）。任一步失败后重发同一次 Delete
// 即可继续 —— 页面记录仍在（软删只在最后一步），不会出现「删了一半再也删不掉」。
//
// DB 两步同事务：只清路由不软删会留下「页面还在但路径全释放」的窗口（新页面可以抢占
// 同一路径，而旧页面仍可被保存/发布）；只软删不清路由则留下永久占用。合并后两者
// 要么都生效、要么都不生效，残留只可能停在「访问面已下线、DB 尚未落定」，
// 而那是重发一次 Delete 就能收敛的形态。
func (s *Service) Delete(ctx context.Context, req *pagedto.DeleteReq) (err error) {
	if req == nil || strings.TrimSpace(req.ID) == "" {
		return ErrInvalidParam
	}
	// 先定位页面拿 projectID（路由清理需要 project 维度）。逐工程探测（DB-009 第四批）：
	// 删除请求只带 pageId，而 pages 带 FORCE 策略 —— 不带作用域的直查在换非超级角色后
	// 会一律报「页面不存在」，删除功能整体失效。
	page, err := s.locatePageInProjects(ctx, req.ID)
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
	}
	// 先清媒体引用再软删：引用缓存失败则整单删除中断，避免留下「页面已删、引用仍在」的残留。
	// 媒体引用表归 media 模块（无 …Tx 变体），因此它在事务之外，且必须在软删之前 ——
	// 软删之后媒体模块就读不到这个 refID 对应的引用了。
	if s.media != nil {
		if _, rerr := s.media.SyncReferences(ctx, &mediacontract.SyncRefsInput{
			RefKind:  "page",
			RefID:    req.ID,
			RefTitle: page.DraftPath,
		}); rerr != nil {
			return rerr
		}
	}
	// DB 两步同事务：路由占用清理（publication 的 …Tx）+ 软删（含 page_publications /
	// page_stagings 清理）。软删带工程作用域（DB-009 第二批）：pages 带 FORCE 策略，
	// 越界写会被 WITH CHECK 直接拒绝而不是静默改到别的工程。
	if err = s.model.TransactionScoped(ctx, page.ProjectID, func(tx *gorm.DB) error {
		if s.routes != nil {
			if rerr := s.routes.DeleteRoutesByPageTx(ctx, tx, &pubcontract.DeleteRoutesReq{
				ProjectID: page.ProjectID, PageID: req.ID,
			}); rerr != nil {
				return rerr
			}
		}
		return s.model.SoftDeleteTx(ctx, tx, page.ProjectID, req.ID, time.Now().UTC())
	}); err != nil {
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
