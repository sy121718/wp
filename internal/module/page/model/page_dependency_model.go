package pagemodel

import (
	"context"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"go_wp/pkg/rls"
)

const tableNamePageDependencies = "page_dependencies"

type DependencyEntity struct {
	PageID         string    `gorm:"column:page_id;primaryKey"`
	ArtifactID     string    `gorm:"column:artifact_id;primaryKey"`
	DependencyKind string    `gorm:"column:dependency_kind;primaryKey"`
	DependencyKey  string    `gorm:"column:dependency_key;primaryKey"`
	Revision       *string   `gorm:"column:revision"`
	LastChecked    time.Time `gorm:"column:last_checked;not null"`
}

func (DependencyEntity) TableName() string { return tableNamePageDependencies }

func (m *Model) DependencyDB(ctx context.Context) *gorm.DB {
	return m.db.WithContext(ctx).Model(&DependencyEntity{})
}

func (m *Model) ReplaceDependencies(ctx context.Context, projectID, pageID, artifactID string, rows []DependencyEntity) (err error) {
	if strings.TrimSpace(projectID) == "" {
		return ErrProjectRequired
	}
	return rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {
		if err := m.requirePageOwned(ctx, tx, projectID, pageID); err != nil {
			return err
		}
		if derr := tx.Where("artifact_id = ?", artifactID).Delete(&DependencyEntity{}).Error; derr != nil {
			return derr
		}
		if len(rows) == 0 {
			return nil
		}
		return tx.CreateInBatches(rows, 200).Error
	})
}

func (m *Model) ListDependencies(ctx context.Context, projectID, pageID, artifactID string) (list []DependencyEntity, err error) {
	if strings.TrimSpace(projectID) == "" {
		return nil, ErrProjectRequired
	}
	err = rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {
		if err := m.requirePageOwned(ctx, tx, projectID, pageID); err != nil {
			return err
		}
		return tx.Model(&DependencyEntity{}).Where("artifact_id = ?", artifactID).
			Order("dependency_kind, dependency_key").Find(&list).Error
	})
	return list, err
}

func (m *Model) MarkStaleByDependency(ctx context.Context, projectID, kind, key string, at time.Time) (ids []string, err error) {
	if strings.TrimSpace(projectID) == "" {
		return nil, ErrProjectRequired
	}
	if kind == "" || key == "" {
		return nil, nil
	}
	err = rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {

		ids, err = markStaleByDependencyIn(ctx, tx, projectID, kind, key, at)
		return err
	})
	if err != nil {
		return nil, err
	}
	return ids, nil
}

func markStaleByDependencyIn(ctx context.Context, tx *gorm.DB, projectID, kind, key string, at time.Time) (ids []string, err error) {
	affected := tx.WithContext(ctx).
		Table(tableNamePageDependencies+" AS d").
		Select("DISTINCT d.page_id").
		Joins("JOIN "+tableNamePages+" p ON p.id = d.page_id").
		Where("d.dependency_kind = ? AND d.dependency_key = ?", kind, key).
		Where("p.project_id = ? AND p.deleted_at IS NULL", projectID).
		Where("d.artifact_id IN (p.active_artifact_id, p.staged_artifact_id)" +
			" OR d.artifact_id IN (SELECT artifact_id FROM " + tableNamePagePublications + " WHERE page_id = p.id)" +
			" OR d.artifact_id IN (SELECT artifact_id FROM " + tableNamePageStagings + " WHERE page_id = p.id)")
	var rows []PageEntity
	if err := tx.WithContext(ctx).Model(&rows).
		Where("project_id = ? AND deleted_at IS NULL AND id IN (?)", projectID, affected).
		Clauses(clause.Returning{Columns: []clause.Column{{Name: "id"}}}).
		Updates(staleMarkUpdates(at)).Error; err != nil {
		return nil, err
	}
	return pageIDsOf(rows), nil
}

func (m *Model) CountDependenciesByKind(ctx context.Context, projectID, pageID, kind string) (n int64, err error) {
	if strings.TrimSpace(projectID) == "" {
		return 0, ErrProjectRequired
	}
	err = m.DependencyDB(ctx).
		Where("page_id = ? AND dependency_kind = ?", pageID, kind).
		Count(&n).Error
	return n, err
}
