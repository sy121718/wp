package pubservice

// publication_route.go — 路由占用与激活（占用预检、激活/取消激活、重定向、按页面或实例删除路由）。

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	pubdto "go_wp/internal/module/publication/dto"
	pubenums "go_wp/internal/module/publication/enums"
	pubmodel "go_wp/internal/module/publication/model"
	"go_wp/pkg/logger"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// RenameReserved 修改页面的草稿路径占用；仅允许 reserved 状态改名。
func (s *Service) RenameReserved(ctx context.Context, req *pubdto.RenameReservedReq) (err error) {
	if req == nil {
		return errors.New(pubenums.ErrInvalidParam)
	}
	oldPath, err := normalizePath(req.OldPath)
	if err != nil {
		return err
	}
	newPath, err := normalizePath(req.NewPath)
	if err != nil {
		return err
	}
	if oldPath == newPath {
		return nil
	}
	now := time.Now().UTC()
	err = s.model.Transaction(ctx, func(tx *gorm.DB) error {
		// 区分「旧路径无任何占用」与「旧路径不是 reserved」：
		// 前者视为幂等成功（草稿路由可能尚未建立）；后者需按归属判断——
		// 本页 active/redirect 行是页面改 URL 流程（UpdateURL）的 DB 同步步骤
		// （发布时 reserved 被原地升级为 active，无独立 reserved 行），必须允许迁移；
		// 他人 active/redirect 行禁止直接改名（不应触碰他人线上路径）。
		var existing pubmodel.RouteEntity
		switch ferr := tx.Where("project_id = ? AND path = ?", req.ProjectID, oldPath).
			First(&existing).Error; {
		case errors.Is(ferr, gorm.ErrRecordNotFound):
			return nil
		case ferr != nil:
			return ferr
		}
		if existing.RouteKind != pubmodel.RouteReserved {
			// OnlyReserved：调用方（多语言下改其他语言 URL）只希望迁移草稿占用，
			// 明确要求不动本页 active/redirect 行。
			if req.OnlyReserved {
				return nil
			}
			if existing.PageID == nil || *existing.PageID != req.PageID {
				return errors.New(pubenums.ErrRouteActiveRename)
			}
		}
		// 先清理新路径上本页 active 残留（避免迁移行与 (project_id, path)
		// 唯一约束冲突），再迁移旧路径行（reserved 或本页 active）。
		if err := tx.Where("project_id = ? AND path = ? AND page_id = ? AND route_kind = ?",
			req.ProjectID, newPath, req.PageID, pubmodel.RouteActive).
			Delete(&pubmodel.RouteEntity{}).Error; err != nil {
			return err
		}
		result := tx.Model(&pubmodel.RouteEntity{}).
			Where("project_id = ? AND path = ? AND page_id = ?", req.ProjectID, oldPath, req.PageID).
			Updates(map[string]any{"path": newPath, "updated_at": now})
		if result.Error != nil {
			// 新路径被他人占用时 UPDATE 撞 (project_id, path) 唯一约束——
			// 归一为 ErrRouteOccupied（语义：改名目标路径已被其他页面占用）。
			if errors.Is(result.Error, gorm.ErrDuplicatedKey) || strings.Contains(result.Error.Error(), "23505") {
				return errRouteOccupied
			}
			return result.Error
		}
		if result.RowsAffected != 1 {
			// 旧路径被其他页面 reserved 占用（page_id 不匹配）：保持幂等成功，
			// 不触碰他人草稿占用。
			return nil
		}
		return nil
	})
	return err
}

// Activate 把路径占用切换为 active（两段式回执，docs/03-pipeline.md §9）：
//
//	第一段：pending 回执在独立事务中先行持久化并提交——进程在后续任一步
//	崩溃时，恢复流程（RollbackReceipts）有据可查；
//	第二段：路由事务内完成占用归属校验 + 路由切换 + 置 committed，三者原子；
//	路由事务失败时把 pending 回执补偿为 rolled_back（补偿失败则保持
//	pending 供下次恢复处理）。
//
// 占用归属校验：目标路径已被其他页面（或展示实例，page_id 为空）占用时
// 返回 ErrRouteOccupied，绝不覆盖他人占用（H2/H7 的 DB 层兜底）。
func (s *Service) Activate(ctx context.Context, req *pubdto.ActivateReq) (res *pubdto.RouteResp, err error) {
	if req == nil {
		return nil, errors.New(pubenums.ErrInvalidParam)
	}
	path, err := normalizePath(req.Path)
	if err != nil {
		return nil, err
	}
	owner, err := parseRouteOwner(req.PageID, req.PresentationID)
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	receiptData, merr := json.Marshal(receiptPayload{To: req.ArtifactID})
	if merr != nil {
		receiptData = json.RawMessage(`{}`)
	}
	receipt := &pubmodel.ReceiptEntity{
		SourceType: owner.sourceType(), SourceID: owner.sourceID(),
		Action: receiptAction(req.Action, "activate"), Path: path,
		ToArtifact: strPtr(req.ArtifactID), ReceiptState: pubmodel.ReceiptPending,
		ReceiptData: receiptData, CreateTime: now,
	}

	// 第一段：pending 回执独立事务提交（故障恢复依据，H1）。
	if cerr := s.model.Transaction(ctx, func(tx *gorm.DB) error {
		return tx.Create(receipt).Error
	}); cerr != nil {
		logger.Scene("publication").With("url", path).With("kind", "activate").Error(cerr, "pending 回执写入失败")
		return nil, cerr
	}

	// 第二段：路由事务（占用归属校验 + 路由切换 + 置 committed）。
	//
	// 原子抢占：单条 INSERT ... ON CONFLICT DO UPDATE（PG 方言，主库）替代
	// 原「SELECT 无锁检查 → UPDATE → CREATE」三语句。语义：
	//   - 无既有占用            → 插入 active 行（RowsAffected=1）；
	//   - 既有占用且归属者本人    → 原地升级 active（含 reserved 升级，
	//                              RowsAffected=1，幂等重复激活）；
	//   - 既有占用且非归属者      → DO UPDATE WHERE 不匹配，PG 静默 DO NOTHING
	//                              （RowsAffected=0）→ ErrRouteOccupied。
	// 消除原实现 SELECT→CREATE 的 TOCTOU 窗口：并发抢占时败者不再产生失败的
	// CREATE 撞 23505 与补偿回执，唯一约束冲突在语句内被原子消化。
	err = s.model.Transaction(ctx, func(tx *gorm.DB) error {
		result := tx.Clauses(clause.OnConflict{
			Columns: []clause.Column{{Name: "project_id"}, {Name: "path"}},
			// DO UPDATE 仅当冲突行归属者本人：页面按 page_id、展示实例按
			// presentation_id 比对（用 IS NOT DISTINCT FROM 让 NULL 也能相等，
			// 否则「实例行 page_id 为 NULL」这一半永远匹配不上）。他人时
			// WHERE 不成立 → 0 行 → occupied。
			Where:     clause.Where{Exprs: []clause.Expression{owner.ownershipExpr()}},
			DoUpdates: clause.AssignmentColumns([]string{"route_kind", "artifact_id", "updated_at"}),
		}).Create(&pubmodel.RouteEntity{
			ProjectID: req.ProjectID, Path: path,
			PageID: owner.pageIDPtr(), PresentationID: owner.presentationIDPtr(),
			RouteKind: pubmodel.RouteActive, ArtifactID: strPtr(req.ArtifactID), UpdatedAt: now,
		})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			// 目标路径被其他实体占用：ON CONFLICT 未执行更新。
			return errRouteOccupied
		}
		return markReceipt(tx, receipt.ID, pubmodel.ReceiptCommitted, now)
	})
	if err != nil {
		// 路由事务失败：pending → rolled_back（补偿失败保持 pending 供恢复）。
		if rberr := s.model.Transaction(ctx, func(tx *gorm.DB) error {
			return markReceipt(tx, receipt.ID, pubmodel.ReceiptRolledBack, time.Now().UTC())
		}); rberr != nil {
			logger.Scene("publication").With("url", path).With("receiptId", receipt.ID).
				Error(rberr, "pending 回执补偿失败（保持 pending 供恢复流程处理）")
		}
		if errors.Is(err, errRouteOccupied) {
			return nil, errors.New(pubenums.ErrRouteOccupied)
		}
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errors.New(pubenums.ErrRouteNotFound)
		}
		logger.Scene("publication").With("url", path).With("kind", "activate").Error(err, "路由激活失败")
		return nil, err
	}
	route, err := s.model.GetRoute(ctx, req.ProjectID, path)
	if err != nil {
		return nil, err
	}
	return routeResp(route), nil
}

// ReservePath 创建草稿路径 reserved 占用（页面创建时预留）。
// 路径已被其他实体占用（含展示实例）时返回 ErrRouteOccupied——
// 替代原 page model 三表事务里直接 INSERT reserved 撞主键的检测方式，
// 使 URL 占用单一归 publication 所有。
func (s *Service) ReservePath(ctx context.Context, req *pubdto.ReserveReq) (err error) {
	if req == nil {
		return errors.New(pubenums.ErrInvalidParam)
	}
	path, err := normalizePath(req.Path)
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	pageIDCopy := req.PageID
	err = s.model.Transaction(ctx, func(tx *gorm.DB) error {
		if cerr := tx.Create(&pubmodel.RouteEntity{
			ProjectID: req.ProjectID, Path: path, PageID: &pageIDCopy,
			RouteKind: pubmodel.RouteReserved, UpdatedAt: now,
		}).Error; cerr != nil {
			if errors.Is(cerr, gorm.ErrDuplicatedKey) || strings.Contains(cerr.Error(), "23505") {
				return errRouteOccupied
			}
			return cerr
		}
		return nil
	})
	if errors.Is(err, errRouteOccupied) {
		return errors.New(pubenums.ErrRouteOccupied)
	}
	return err
}

// DeleteRoutesByPage 清理页面全部路径占用（reserved/active/redirect 任一 kind），
// 页面删除时释放路径。幂等（无占用时 RowsAffected=0 不报错）。
func (s *Service) DeleteRoutesByPage(ctx context.Context, req *pubdto.DeleteRoutesReq) (err error) {
	if req == nil {
		return errors.New(pubenums.ErrInvalidParam)
	}
	result := s.model.RouteDB(ctx).
		Where("project_id = ? AND page_id = ?", req.ProjectID, req.PageID).
		Delete(&pubmodel.RouteEntity{})
	return result.Error
}

// DeleteRoutesByPresentation 清理展示实例全部路径占用（实例删除时释放）。
// 幂等（无占用时 RowsAffected=0 不报错）。
func (s *Service) DeleteRoutesByPresentation(ctx context.Context, req *pubdto.DeleteRoutesByPresentationReq) (err error) {
	if req == nil {
		return errors.New(pubenums.ErrInvalidParam)
	}
	result := s.model.RouteDB(ctx).
		Where("project_id = ? AND presentation_id = ?", req.ProjectID, req.PresentationID).
		Delete(&pubmodel.RouteEntity{})
	return result.Error
}

// ListReferencedArtifactIDs 返回全部被路由引用的产物行 ID（产物 GC 的保护集合）。
func (s *Service) ListReferencedArtifactIDs(ctx context.Context) (ids []string, err error) {
	return s.model.ListReferencedArtifactIDs(ctx)
}

// ListActivePaths 返回页面已激活（active/redirect）的路径集合。
//
// 访问面（/site）直接服务 active 目录的文件系统状态：调用方清理页面时必须
// 按这些路径解除激活（删除符号链接），只删 DB 路由行不会让内容下线。
func (s *Service) ListActivePaths(ctx context.Context, req *pubdto.ListActivePathsReq) (paths []string, err error) {
	if req == nil {
		return nil, errors.New(pubenums.ErrInvalidParam)
	}
	paths = []string{}
	if err = s.model.RouteDB(ctx).
		Where("project_id = ? AND page_id = ? AND route_kind IN ?",
			req.ProjectID, req.PageID, []string{pubmodel.RouteActive, pubmodel.RouteRedirect}).
		Pluck("path", &paths).Error; err != nil {
		return nil, err
	}
	return paths, nil
}

// ListActivePathsByPresentation 返回展示实例已激活（active/redirect）的路径集合。
//
// 改过 URL 的实例有两条路径：新路径（active）与旧路径（redirect）。删除实例时
// 必须按这两条都解除访问面激活 —— 只删 DB 路由行会让旧路径的符号链接留在
// active 目录里继续 301，指向一个已经不存在的页面。
func (s *Service) ListActivePathsByPresentation(ctx context.Context, req *pubdto.ListActivePathsByPresentationReq) (paths []string, err error) {
	if req == nil {
		return nil, errors.New(pubenums.ErrInvalidParam)
	}
	paths = []string{}
	if err = s.model.RouteDB(ctx).
		Where("project_id = ? AND presentation_id = ? AND route_kind IN ?",
			req.ProjectID, req.PresentationID, []string{pubmodel.RouteActive, pubmodel.RouteRedirect}).
		Pluck("path", &paths).Error; err != nil {
		return nil, err
	}
	return paths, nil
}

// IsPathOccupied 查询路径是否被其他实体占用（page_id 为空即展示实例占用，
// page_id 非 excludePageID 即他人页面占用），供页面创建/发布前预检。
func (s *Service) IsPathOccupied(ctx context.Context, req *pubdto.IsOccupiedReq) (occupied bool, err error) {
	if req == nil {
		return false, errors.New(pubenums.ErrInvalidParam)
	}
	path, err := normalizePath(req.Path)
	if err != nil {
		return false, err
	}
	var foreign int64
	q := s.model.RouteDB(ctx).Where("project_id = ? AND path = ?", req.ProjectID, path)
	// ExcludePageID 为空时**不能**加 uuid 比较条件：把空串当 uuid 传给 PG 会直接报
	// invalid input syntax for type uuid: ""（新建页预检不携带排除项，必踩此路径）。
	if exclude := strings.TrimSpace(req.ExcludePageID); exclude != "" {
		q = q.Where("(page_id IS NULL OR page_id <> ?)", exclude)
	}
	// 展示实例改 URL 时排除自身：它的行 page_id 为 NULL，用 ExcludePageID
	// 排除不掉自己，会把「自己占着旧路径」误判成冲突而无法改名。
	if exclude := strings.TrimSpace(req.ExcludePresentationID); exclude != "" {
		q = q.Where("(presentation_id IS NULL OR presentation_id <> ?)", exclude)
	}
	if err = q.Count(&foreign).Error; err != nil {
		return false, err
	}
	return foreign > 0, nil
}

// Deactivate 取消路径占用；路由不存在时幂等返回。
//
// 归属者三分支：展示实例按 presentation_id 精确匹配（它的行 page_id 为 NULL，
// 历史判据 page_id IS NOT NULL 够不着）、页面按 page_id 精确匹配、都未指定时
// 保持「任意页面占用」的历史行为（page 侧既有调用不携带归属者，逐字不变）。
// 只删 active 行：redirect 行是「旧路径的 301 承诺」，取消激活不该顺手销毁它。
func (s *Service) Deactivate(ctx context.Context, req *pubdto.DeactivateReq) (err error) {
	if req == nil {
		return errors.New(pubenums.ErrInvalidParam)
	}
	path, err := normalizePath(req.Path)
	if err != nil {
		return err
	}
	q := s.model.RouteDB(ctx).
		Where("project_id = ? AND path = ? AND route_kind = ?", req.ProjectID, path, pubmodel.RouteActive)
	switch {
	case strings.TrimSpace(req.PresentationID) != "":
		q = q.Where("presentation_id = ?", strings.TrimSpace(req.PresentationID))
	case strings.TrimSpace(req.PageID) != "":
		q = q.Where("page_id = ?", strings.TrimSpace(req.PageID))
	default:
		q = q.Where("page_id IS NOT NULL")
	}
	result := q.Delete(&pubmodel.RouteEntity{})
	if result.Error != nil {
		logger.Scene("publication").With("url", path).With("kind", "deactivate").Error(result.Error, "路由取消失败")
		return result.Error
	}
	return nil
}

// Redirect 把旧路径占用改为 redirect 并指向重定向产物（两段式回执，与 Activate 对齐）。
//
//	第一段：pending 回执在独立事务中先行持久化——进程在后续任一步崩溃时
//	恢复流程（RollbackReceipts）有据可查；
//	第二段：路由事务内完成占用切换 + 置 committed，二者原子；失败时把
//	pending 回执补偿为 rolled_back（补偿失败保持 pending 供恢复）。
func (s *Service) Redirect(ctx context.Context, req *pubdto.RedirectReq) (res *pubdto.RouteResp, err error) {
	if req == nil {
		return nil, errors.New(pubenums.ErrInvalidParam)
	}
	oldPath, err := normalizePath(req.OldPath)
	if err != nil {
		return nil, err
	}
	owner, err := parseRouteOwner(req.PageID, req.PresentationID)
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()

	// ArtifactID 允许为空（重定向产物不入库，DTO 契约）：回执不得写入空串 uuid。
	var toArtifact *string
	if strings.TrimSpace(req.ArtifactID) != "" {
		toArtifact = strPtr(req.ArtifactID)
	}
	receiptData, merr := json.Marshal(map[string]string{"redirect": req.ArtifactID})
	if merr != nil {
		receiptData = json.RawMessage(`{}`)
	}
	receipt := &pubmodel.ReceiptEntity{
		SourceType: owner.sourceType(), SourceID: owner.sourceID(),
		Action: "redirect", Path: oldPath,
		ToArtifact: toArtifact, ReceiptState: pubmodel.ReceiptPending,
		ReceiptData: receiptData, CreateTime: now,
	}

	// 第一段：pending 回执独立事务提交（故障恢复依据，对齐 Activate）。
	if cerr := s.model.Transaction(ctx, func(tx *gorm.DB) error {
		return tx.Create(receipt).Error
	}); cerr != nil {
		logger.Scene("publication").With("url", oldPath).With("kind", "redirect").Error(cerr, "pending 回执写入失败")
		return nil, cerr
	}

	// 第二段：路由事务（占用切换 + 置 committed）。
	err = s.model.Transaction(ctx, func(tx *gorm.DB) error {
		matchSQL, matchArgs := owner.match()
		q := tx.Model(&pubmodel.RouteEntity{}).
			Where("project_id = ? AND path = ?", req.ProjectID, oldPath).
			Where(matchSQL, matchArgs...)
		result := q.Updates(map[string]any{
			"route_kind": pubmodel.RouteRedirect,
			"updated_at": now,
		})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			// 无既有占用时直接建立 redirect 行（幂等）。
			if err := tx.Create(&pubmodel.RouteEntity{
				ProjectID: req.ProjectID, Path: oldPath,
				PageID: owner.pageIDPtr(), PresentationID: owner.presentationIDPtr(),
				RouteKind: pubmodel.RouteRedirect, UpdatedAt: now,
			}).Error; err != nil {
				// 对他人占用路径建 redirect 行撞 (project_id, path) 唯一约束——
				// 归一为 ErrRouteOccupied（语义：重定向目标路径已被其他页面占用）。
				if errors.Is(err, gorm.ErrDuplicatedKey) || strings.Contains(err.Error(), "23505") {
					return errRouteOccupied
				}
				return err
			}
		}
		if toArtifact != nil {
			if err := tx.Model(&pubmodel.RouteEntity{}).
				Where("project_id = ? AND path = ?", req.ProjectID, oldPath).
				Update("artifact_id", *toArtifact).Error; err != nil {
				return err
			}
		}
		return markReceipt(tx, receipt.ID, pubmodel.ReceiptCommitted, now)
	})
	if err != nil {
		// 路由事务失败：pending → rolled_back（补偿失败保持 pending 供恢复）。
		if rberr := s.model.Transaction(ctx, func(tx *gorm.DB) error {
			return markReceipt(tx, receipt.ID, pubmodel.ReceiptRolledBack, time.Now().UTC())
		}); rberr != nil {
			logger.Scene("publication").With("url", oldPath).With("receiptId", receipt.ID).
				Error(rberr, "pending 回执补偿失败（保持 pending 供恢复流程处理）")
		}
		if errors.Is(err, errRouteOccupied) {
			return nil, errors.New(pubenums.ErrRouteOccupied)
		}
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errors.New(pubenums.ErrRouteNotFound)
		}
		logger.Scene("publication").With("url", oldPath).With("kind", "redirect").Error(err, "路由重定向失败")
		return nil, err
	}
	route, err := s.model.GetRoute(ctx, req.ProjectID, oldPath)
	if err != nil {
		return nil, err
	}
	return routeResp(route), nil
}
