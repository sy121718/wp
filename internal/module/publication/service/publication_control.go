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

// Activate 把路径占用切换为 active：先写 pending 回执，更新路由，再落 committed；
// 任一步失败回执保持 pending 供恢复流程处理。
func (s *Service) Activate(ctx context.Context, req *pubdto.ActivateReq) (res *pubdto.RouteResp, err error) {
	if req == nil {
		return nil, errors.New(pubenums.ErrRouteNotFound)
	}
	path, err := normalizePath(req.Path)
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	receipt := &pubmodel.ReceiptEntity{
		ID: uuid.NewString(), SourceType: "page", SourceID: req.PageID,
		Action: receiptAction(req.Action, "activate"), Path: path,
		ToArtifact: strPtr(req.ArtifactID), ReceiptState: pubmodel.ReceiptPending,
		ReceiptData: json.RawMessage(`{"to":"` + req.ArtifactID + `"}`), CreatedAt: now,
	}

	err = s.model.Transaction(ctx, func(tx *gorm.DB) error {
		if err := tx.Create(receipt).Error; err != nil {
			return err
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
			// 无既有占用时直接建立 active 行（幂等激活）。
			pageIDCopy := req.PageID
			if err := tx.Create(&pubmodel.RouteEntity{
				ProjectID: req.ProjectID, Path: path, PageID: &pageIDCopy,
				RouteKind: pubmodel.RouteActive, ArtifactID: strPtr(req.ArtifactID), UpdatedAt: now,
			}).Error; err != nil {
				return err
			}
		}
		return markReceipt(tx, receipt.ID, pubmodel.ReceiptCommitted, now)
	})
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, errors.New(pubenums.ErrRouteNotFound)
	}
	if err != nil {
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
		return tx.Create(&pubmodel.ReceiptEntity{
			ID: uuid.NewString(), SourceType: "page", SourceID: req.PageID,
			Action: "redirect", Path: oldPath,
			FromArtifact: nil, ToArtifact: strPtr(req.ArtifactID),
			ReceiptState: pubmodel.ReceiptCommitted,
			ReceiptData:  json.RawMessage(`{"redirect":"` + req.ArtifactID + `"}`),
			CreatedAt:    now, CompletedAt: &now,
		}).Error
	})
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, errors.New(pubenums.ErrRouteNotFound)
	}
	if err != nil {
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
