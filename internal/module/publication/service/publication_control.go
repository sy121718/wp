package pubservice

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

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// RenameReserved 修改页面的草稿路径占用；仅允许 reserved 状态改名。
func (s *Service) RenameReserved(ctx context.Context, req *pubdto.RenameReservedReq) (err error) {
	if req == nil {
		return errors.New(pubenums.ErrRouteNotFound)
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
		result := tx.Model(&pubmodel.RouteEntity{}).
			Where("project_id = ? AND path = ? AND page_id = ? AND route_kind = ?", req.ProjectID, oldPath, req.PageID, pubmodel.RouteReserved).
			Updates(map[string]any{"path": newPath, "updated_at": now})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			// 无占用视为幂等成功（草稿路由可能尚未建立）。
			return nil
		}
		return tx.Where("project_id = ? AND path = ? AND page_id = ? AND route_kind = ?",
			req.ProjectID, newPath, req.PageID, pubmodel.RouteActive).
			Delete(&pubmodel.RouteEntity{}).Error
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
		return nil, errors.New(pubenums.ErrRouteNotFound)
	}
	path, err := normalizePath(req.Path)
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	receiptData, merr := json.Marshal(receiptPayload{To: req.ArtifactID})
	if merr != nil {
		receiptData = json.RawMessage(`{}`)
	}
	receipt := &pubmodel.ReceiptEntity{
		ID: uuid.NewString(), SourceType: "page", SourceID: req.PageID,
		Action: receiptAction(req.Action, "activate"), Path: path,
		ToArtifact: strPtr(req.ArtifactID), ReceiptState: pubmodel.ReceiptPending,
		ReceiptData: receiptData, CreatedAt: now,
	}

	// 第一段：pending 回执独立事务提交（故障恢复依据，H1）。
	if cerr := s.model.Transaction(ctx, func(tx *gorm.DB) error {
		return tx.Create(receipt).Error
	}); cerr != nil {
		logger.Scene("publication").With("url", path).With("kind", "activate").Error(cerr, "pending 回执写入失败")
		return nil, cerr
	}

	// 第二段：路由事务（占用归属校验 + 路由切换 + 置 committed）。
	err = s.model.Transaction(ctx, func(tx *gorm.DB) error {
		var existing pubmodel.RouteEntity
		switch ferr := tx.Where("project_id = ? AND path = ?", req.ProjectID, path).
			First(&existing).Error; {
		case ferr == nil:
			// 已有占用：只允许归属者本人升级；page_id 为空表示展示实例占用。
			if existing.PageID == nil || *existing.PageID != req.PageID {
				return errRouteOccupied
			}
		case errors.Is(ferr, gorm.ErrRecordNotFound):
			// 无既有占用：下面直接建立 active 行（幂等激活）。
		default:
			return ferr
		}
		result := tx.Model(&pubmodel.RouteEntity{}).
			Where("project_id = ? AND path = ? AND page_id = ?", req.ProjectID, path, req.PageID).
			Updates(map[string]any{
				"route_kind":  pubmodel.RouteActive,
				"artifact_id": req.ArtifactID,
				"updated_at":  now,
			})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			pageIDCopy := req.PageID
			if err := tx.Create(&pubmodel.RouteEntity{
				ProjectID: req.ProjectID, Path: path, PageID: &pageIDCopy,
				RouteKind: pubmodel.RouteActive, ArtifactID: strPtr(req.ArtifactID), UpdatedAt: now,
			}).Error; err != nil {
				// 并发抢占同一路径：两个事务都通过上面的无占用检查（READ COMMITTED
				// 无行锁），后提交者 CREATE 撞 (project_id, path) 唯一约束——
				// 归一为 ErrRouteOccupied（TranslateError 未开启时是原始 23505）。
				if errors.Is(err, gorm.ErrDuplicatedKey) || strings.Contains(err.Error(), "23505") {
					return errRouteOccupied
				}
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

// Deactivate 取消路径占用；路由不存在时幂等返回。
func (s *Service) Deactivate(ctx context.Context, req *pubdto.DeactivateReq) (err error) {
	if req == nil {
		return errors.New(pubenums.ErrRouteNotFound)
	}
	path, err := normalizePath(req.Path)
	if err != nil {
		return err
	}
	result := s.model.RouteDB(ctx).
		Where("project_id = ? AND path = ? AND page_id IS NOT NULL", req.ProjectID, path).
		Delete(&pubmodel.RouteEntity{})
	if result.Error != nil {
		logger.Scene("publication").With("url", path).With("kind", "deactivate").Error(result.Error, "路由取消失败")
		return result.Error
	}
	return nil
}

// Redirect 把旧路径占用改为 redirect 并指向重定向产物。
func (s *Service) Redirect(ctx context.Context, req *pubdto.RedirectReq) (res *pubdto.RouteResp, err error) {
	if req == nil {
		return nil, errors.New(pubenums.ErrRouteNotFound)
	}
	oldPath, err := normalizePath(req.OldPath)
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	err = s.model.Transaction(ctx, func(tx *gorm.DB) error {
		result := tx.Model(&pubmodel.RouteEntity{}).
			Where("project_id = ? AND path = ? AND page_id = ?", req.ProjectID, oldPath, req.PageID).
			Updates(map[string]any{
				"route_kind": pubmodel.RouteRedirect,
				"updated_at": now,
			})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			// 无既有占用时直接建立 redirect 行（幂等）。
			pageIDCopy := req.PageID
			if err := tx.Create(&pubmodel.RouteEntity{
				ProjectID: req.ProjectID, Path: oldPath, PageID: &pageIDCopy,
				RouteKind: pubmodel.RouteRedirect, UpdatedAt: now,
			}).Error; err != nil {
				return err
			}
		}
		if strings.TrimSpace(req.ArtifactID) != "" {
			if err := tx.Model(&pubmodel.RouteEntity{}).
				Where("project_id = ? AND path = ?", req.ProjectID, oldPath).
				Update("artifact_id", req.ArtifactID).Error; err != nil {
				return err
			}
		}
		// ArtifactID 允许为空（重定向产物不入库，DTO 契约）：回执不得写入空串 uuid。
		var toArtifact *string
		if strings.TrimSpace(req.ArtifactID) != "" {
			toArtifact = strPtr(req.ArtifactID)
		}
		receiptData, err := json.Marshal(map[string]string{"redirect": req.ArtifactID})
		if err != nil {
			return err
		}
		return tx.Create(&pubmodel.ReceiptEntity{
			ID: uuid.NewString(), SourceType: "page", SourceID: req.PageID,
			Action: "redirect", Path: oldPath,
			FromArtifact: nil, ToArtifact: toArtifact,
			ReceiptState: pubmodel.ReceiptCommitted,
			ReceiptData:  receiptData,
			CreatedAt:    now, CompletedAt: &now,
		}).Error
	})
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, errors.New(pubenums.ErrRouteNotFound)
	}
	if err != nil {
		logger.Scene("publication").With("url", oldPath).With("kind", "redirect").Error(err, "路由重定向失败")
		return nil, err
	}
	route, err := s.model.GetRoute(ctx, req.ProjectID, oldPath)
	if err != nil {
		return nil, err
	}
	return routeResp(route), nil
}

// RollbackReceipts 启动恢复：全部 pending 回执标记 rolled_back，返回处理数量。
func (s *Service) RollbackReceipts(ctx context.Context) (count int64, err error) {
	now := time.Now().UTC()
	result := s.model.ReceiptDB(ctx).
		Where("receipt_state = ?", pubmodel.ReceiptPending).
		Updates(map[string]any{"receipt_state": pubmodel.ReceiptRolledBack, "completed_at": now})
	return result.RowsAffected, result.Error
}

func receiptAction(action, fallback string) string {
	if strings.TrimSpace(action) == "" {
		return fallback
	}
	return action
}

// errRouteOccupied 事务内占位冲突哨兵，外层映射为 pubenums.ErrRouteOccupied。
var errRouteOccupied = errors.New(pubenums.ErrRouteOccupied)

// receiptPayload 回执数据结构化序列化（替代手工拼接 JSON，避免特殊字符生成非法 jsonb）。
type receiptPayload struct {
	To string `json:"to,omitempty"`
}

func markReceipt(tx *gorm.DB, id, state string, now time.Time) error {
	return tx.Model(&pubmodel.ReceiptEntity{}).
		Where("id = ?", id).
		Updates(map[string]any{"receipt_state": state, "completed_at": now}).Error
}

// normalizePath 规范化路径：根之外的结尾斜杠去除，保证与 page_routes 主键一致。
func normalizePath(raw string) (string, error) {
	if raw == "" || !strings.HasPrefix(raw, "/") {
		return "", errors.New(pubenums.ErrRouteNotFound)
	}
	if raw != "/" {
		raw = strings.TrimSuffix(raw, "/")
	}
	return raw, nil
}

func strPtr(s string) *string { return &s }

func routeResp(e *pubmodel.RouteEntity) *pubdto.RouteResp {
	return &pubdto.RouteResp{
		ProjectID: e.ProjectID, Path: e.Path, PageID: e.PageID,
		RouteKind: e.RouteKind, ArtifactID: e.ArtifactID, UpdatedAt: e.UpdatedAt,
	}
}
