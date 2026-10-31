package pubmodel

import (
	"context"
	"errors"
	"strings"
	"time"

	pubenums "go_wp/internal/module/publication/enums"
	"go_wp/pkg/rls"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var ErrRouteOccupied = errors.New("publication: 目标路径已被其他实体占用")

func markRouteOccupiedErr(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, gorm.ErrDuplicatedKey) || strings.Contains(err.Error(), "23505") {
		return ErrRouteOccupied
	}
	return err
}

func (m *Model) MarkReceiptStateTx(ctx context.Context, tx *gorm.DB, id int64, state string, now time.Time) error {
	return tx.WithContext(ctx).Model(&ReceiptEntity{}).
		Where("id = ?", id).
		Updates(map[string]any{"receipt_state": state, "completed_at": now}).Error
}

func (m *Model) CreateReceiptTx(ctx context.Context, tx *gorm.DB, r *ReceiptEntity) error {
	return tx.WithContext(ctx).Create(r).Error
}

func (m *Model) RenameReservedTx(ctx context.Context, tx *gorm.DB, projectID, pageID, oldPath, newPath string, onlyReserved bool, now time.Time) error {

	var existing RouteEntity
	switch ferr := tx.WithContext(ctx).Where("project_id = ? AND path = ?", projectID, oldPath).
		First(&existing).Error; {
	case errors.Is(ferr, gorm.ErrRecordNotFound):
		return nil
	case ferr != nil:
		return ferr
	}
	if existing.RouteKind != RouteReserved {

		if onlyReserved {
			return nil
		}
		if existing.PageID == nil || *existing.PageID != pageID {
			return pubenums.Err(pubenums.ErrRouteActiveRename)
		}
	}

	if err := tx.WithContext(ctx).Where("project_id = ? AND path = ? AND page_id = ? AND route_kind = ?",
		projectID, newPath, pageID, RouteActive).
		Delete(&RouteEntity{}).Error; err != nil {
		return err
	}
	result := tx.WithContext(ctx).Model(&RouteEntity{}).
		Where("project_id = ? AND path = ? AND page_id = ?", projectID, oldPath, pageID).
		Updates(map[string]any{"path": newPath, "update_time": now})
	if result.Error != nil {

		return markRouteOccupiedErr(result.Error)
	}
	if result.RowsAffected != 1 {

		return nil
	}
	return nil
}

func (m *Model) RedirectInTx(ctx context.Context, tx *gorm.DB, projectID, oldPath, pageID, presentationID string, toArtifact *string, now time.Time) error {
	q := tx.WithContext(ctx).Model(&RouteEntity{}).
		Where("project_id = ? AND path = ?", projectID, oldPath)
	if presentationID != "" {
		q = q.Where("presentation_id = ?", presentationID)
	} else {
		q = q.Where("page_id = ?", pageID)
	}
	result := q.Updates(map[string]any{
		"route_kind":  RouteRedirect,
		"update_time": now,
	})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {

		ins := &RouteEntity{
			ProjectID: projectID, Path: oldPath,
			PageID: nil, PresentationID: nil,
			RouteKind: RouteRedirect, UpdatedAt: now,
		}
		if pageID != "" {
			ins.PageID = &pageID
		}
		if presentationID != "" {
			ins.PresentationID = &presentationID
		}
		if cerr := tx.WithContext(ctx).Create(ins).Error; cerr != nil {

			return markRouteOccupiedErr(cerr)
		}
	}
	if toArtifact != nil {
		if uerr := tx.WithContext(ctx).Model(&RouteEntity{}).
			Where("project_id = ? AND path = ?", projectID, oldPath).
			Update("artifact_id", *toArtifact).Error; uerr != nil {
			return uerr
		}
	}
	return nil
}

const ownershipExprSQL = "page_routes.page_id IS NOT DISTINCT FROM EXCLUDED.page_id" +
	" AND page_routes.presentation_id IS NOT DISTINCT FROM EXCLUDED.presentation_id"

func (m *Model) ActivateRouteTx(ctx context.Context, tx *gorm.DB, projectID, path string, pageID, presentationID *string, artifactID string, now time.Time) error {
	result := tx.WithContext(ctx).Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "project_id"}, {Name: "path"}},

		Where:     clause.Where{Exprs: []clause.Expression{clause.Expr{SQL: ownershipExprSQL}}},
		DoUpdates: clause.AssignmentColumns([]string{"route_kind", "artifact_id", "update_time"}),
	}).Create(&RouteEntity{
		ProjectID: projectID, Path: path,
		PageID: pageID, PresentationID: presentationID,
		RouteKind: RouteActive, ArtifactID: &artifactID, UpdatedAt: now,
	})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {

		return ErrRouteOccupied
	}
	return nil
}

func (m *Model) ReservePathTx(ctx context.Context, tx *gorm.DB, projectID, path, pageID string, now time.Time) error {
	pageIDCopy := pageID
	if cerr := tx.WithContext(ctx).Create(&RouteEntity{
		ProjectID: projectID, Path: path, PageID: &pageIDCopy,
		RouteKind: RouteReserved, UpdatedAt: now,
	}).Error; cerr != nil {
		return markRouteOccupiedErr(cerr)
	}
	return nil
}

func (m *Model) deleteRoutesByPageScope(db *gorm.DB, projectID, pageID string) *gorm.DB {
	return db.Where("project_id = ? AND page_id = ?", projectID, pageID)
}

func (m *Model) DeleteRoutesByPageTx(ctx context.Context, tx *gorm.DB, projectID, pageID string) error {
	return m.deleteRoutesByPageScope(tx.WithContext(ctx), projectID, pageID).Delete(&RouteEntity{}).Error
}

func (m *Model) DeleteRoutesByPage(ctx context.Context, projectID, pageID string) error {
	return rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {
		return m.deleteRoutesByPageScope(tx.WithContext(ctx), projectID, pageID).Delete(&RouteEntity{}).Error
	})
}

func (m *Model) deleteRoutesByPresentationScope(db *gorm.DB, projectID, presentationID string) *gorm.DB {
	return db.Where("project_id = ? AND presentation_id = ?", projectID, presentationID)
}

func (m *Model) DeleteRoutesByPresentationTx(ctx context.Context, tx *gorm.DB, projectID, presentationID string) error {
	return m.deleteRoutesByPresentationScope(tx.WithContext(ctx), projectID, presentationID).
		Delete(&RouteEntity{}).Error
}

func (m *Model) deactivateRouteScope(db *gorm.DB, projectID, path, pageID, presentationID string) *gorm.DB {
	q := db.Where("project_id = ? AND path = ? AND route_kind = ?", projectID, path, RouteActive)
	switch {
	case strings.TrimSpace(presentationID) != "":
		q = q.Where("presentation_id = ?", strings.TrimSpace(presentationID))
	case strings.TrimSpace(pageID) != "":
		q = q.Where("page_id = ?", strings.TrimSpace(pageID))
	default:
		q = q.Where("page_id IS NOT NULL")
	}
	return q
}

func (m *Model) DeactivateRouteTx(ctx context.Context, tx *gorm.DB, projectID, path, pageID, presentationID string) error {
	return m.deactivateRouteScope(tx.WithContext(ctx), projectID, path, pageID, presentationID).
		Delete(&RouteEntity{}).Error
}

func (m *Model) DeactivateRoute(ctx context.Context, projectID, path, pageID, presentationID string) error {
	return rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {
		return m.deactivateRouteScope(tx.WithContext(ctx), projectID, path, pageID, presentationID).
			Delete(&RouteEntity{}).Error
	})
}
