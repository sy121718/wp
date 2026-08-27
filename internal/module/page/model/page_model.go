// Package pagemodel 实现 page 模块 pages、page_revisions 与 page_routes 表持久化。
package pagemodel

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"gorm.io/gorm"
)

var (
	// ErrDraftVersionConflict 表示乐观锁更新未命中当前草稿版本。
	ErrDraftVersionConflict = errors.New("page 草稿版本冲突")
)

const (
	tableNamePages         = "pages"
	tableNamePageRevisions = "page_revisions"
	tableNamePageRoutes    = "page_routes"
)

// PageEntity 对应 pages 表的手工 Page 字段。
type PageEntity struct {
	ID                string          `gorm:"column:id;type:uuid;primaryKey"`
	ProjectID         string          `gorm:"column:project_id;type:uuid;not null"`
	Kind              string          `gorm:"column:kind;type:text;not null"`
	ContentTargetType string          `gorm:"column:content_target_type;type:text;not null"`
	ContentTargetID   *string         `gorm:"column:content_target_id;type:uuid"`
	DraftPath         string          `gorm:"column:draft_path;type:text;not null"`
	ActivePath        *string         `gorm:"column:active_path;type:text"`
	DraftDocument     json.RawMessage `gorm:"column:draft_document;type:jsonb;not null"`
	DraftVersion      int64           `gorm:"column:draft_version;not null"`
	StagedArtifactID  *string         `gorm:"column:staged_artifact_id;type:uuid"`
	ActiveArtifactID  *string         `gorm:"column:active_artifact_id;type:uuid"`
	Stale             bool            `gorm:"column:stale;not null"`
	DeletedAt         *time.Time      `gorm:"column:deleted_at"`
	PublishedAt       *time.Time      `gorm:"column:published_at"`
	CreatedAt         time.Time       `gorm:"column:created_at;not null"`
	UpdatedAt         time.Time       `gorm:"column:updated_at;not null"`
}

func (PageEntity) TableName() string { return tableNamePages }

// RevisionEntity 对应 page_revisions 表：每次保存的不可变草稿快照。
type RevisionEntity struct {
	ID            string          `gorm:"column:id;type:uuid;primaryKey"`
	PageID        string          `gorm:"column:page_id;type:uuid;not null"`
	Version       int64           `gorm:"column:version;not null"`
	DraftPath     string          `gorm:"column:draft_path;type:text;not null"`
	DraftDocument json.RawMessage `gorm:"column:draft_document;type:jsonb;not null"`
	SourceHash    string          `gorm:"column:source_hash;type:text;not null"`
	CreatedAt     time.Time       `gorm:"column:created_at;not null"`
}

func (RevisionEntity) TableName() string { return tableNamePageRevisions }

// RouteEntity 对应 page_routes 表；用于 project 内 draft/active/redirect 的全局路径占用。
type RouteEntity struct {
	ProjectID string    `gorm:"column:project_id;type:uuid;primaryKey"`
	Path      string    `gorm:"column:path;type:text;primaryKey"`
	PageID    *string   `gorm:"column:page_id;type:uuid"`
	RouteKind string    `gorm:"column:route_kind;type:text;not null"`
	UpdatedAt time.Time `gorm:"column:updated_at;not null"`
}

func (RouteEntity) TableName() string { return tableNamePageRoutes }

// Model 封装 page 表数据访问。
type Model struct {
	db *gorm.DB
}

// NewPageModel 创建 Page Model。
func NewPageModel(db *gorm.DB) *Model { return &Model{db: db} }

// DB 返回已绑定 pages 表的 GORM 实例。
func (m *Model) DB(ctx context.Context) *gorm.DB {
	return m.db.WithContext(ctx).Model(&PageEntity{})
}

// RevisionDB 返回已绑定 page_revisions 表的 GORM 实例。
func (m *Model) RevisionDB(ctx context.Context) *gorm.DB {
	return m.db.WithContext(ctx).Model(&RevisionEntity{})
}

// RouteDB 返回已绑定 page_routes 表的 GORM 实例。
func (m *Model) RouteDB(ctx context.Context) *gorm.DB {
	return m.db.WithContext(ctx).Model(&RouteEntity{})
}

// Transaction 在数据库事务中执行给定函数；草稿、修订与路径占用必须原子提交。
func (m *Model) Transaction(ctx context.Context, fn func(tx *gorm.DB) error) error {
	return m.db.WithContext(ctx).Transaction(fn)
}

// GetByID 按 ID 查询未删除的 Page。
func (m *Model) GetByID(ctx context.Context, id string) (e *PageEntity, err error) {
	e = &PageEntity{}
	if err = m.DB(ctx).Where("id = ? AND deleted_at IS NULL", id).First(e).Error; err != nil {
		return nil, err
	}
	return e, nil
}

// ListRevisions 按版本倒序读取修订快照。
func (m *Model) ListRevisions(ctx context.Context, pageID string) (list []RevisionEntity, err error) {
	err = m.RevisionDB(ctx).Where("page_id = ?", pageID).Order("version DESC").Find(&list).Error
	return list, err
}

// CreateWithRevisionAndRoute 原子创建 Page、初始 Revision 与路径占用。
func (m *Model) CreateWithRevisionAndRoute(ctx context.Context, page *PageEntity, revision *RevisionEntity, path string) (err error) {
	return m.Transaction(ctx, func(tx *gorm.DB) error {
		if err := tx.Create(page).Error; err != nil {
			return err
		}
		if err := tx.Create(revision).Error; err != nil {
			return err
		}
		return tx.Create(&RouteEntity{
			ProjectID: page.ProjectID,
			Path:      path,
			PageID:    &page.ID,
			RouteKind: "reserved",
			UpdatedAt: page.UpdatedAt,
		}).Error
	})
}

// SaveDraftWithRevision 使用乐观锁原子保存草稿、修订及路径占用。
func (m *Model) SaveDraftWithRevision(
	ctx context.Context,
	pageID string,
	projectID string,
	expectedVersion int64,
	oldPath string,
	path string,
	document json.RawMessage,
	nextVersion int64,
	updatedAt time.Time,
	revision *RevisionEntity,
	changedPath bool,
) (err error) {
	return m.Transaction(ctx, func(tx *gorm.DB) error {
		result := tx.Model(&PageEntity{}).
			Where("id = ? AND deleted_at IS NULL AND draft_version = ?", pageID, expectedVersion).
			Updates(map[string]any{
				"draft_path":     path,
				"draft_document": document,
				"draft_version":  nextVersion,
				"stale":          true,
				"updated_at":     updatedAt,
			})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return ErrDraftVersionConflict
		}
		if changedPath {
			if err := tx.Where("project_id = ? AND path = ? AND page_id = ? AND route_kind = ?", projectID, oldPath, pageID, "reserved").
				Delete(&RouteEntity{}).Error; err != nil {
				return err
			}
			var ownedRouteCount int64
			if err := tx.Model(&RouteEntity{}).
				Where("project_id = ? AND path = ? AND page_id = ?", projectID, path, pageID).
				Count(&ownedRouteCount).Error; err != nil {
				return err
			}
			if ownedRouteCount == 0 {
				pageIDCopy := pageID
				if err := tx.Create(&RouteEntity{
					ProjectID: projectID,
					Path:      path,
					PageID:    &pageIDCopy,
					RouteKind: "reserved",
					UpdatedAt: updatedAt,
				}).Error; err != nil {
					return err
				}
			}
		}
		return tx.Create(revision).Error
	})
}

// MarkStaged 回写暂存产物指针（构建成功，尚未激活）。
func (m *Model) MarkStaged(ctx context.Context, pageID, artifactID string, at time.Time) (err error) {
	return m.DB(ctx).Where("id = ? AND deleted_at IS NULL", pageID).
		Updates(map[string]any{"staged_artifact_id": artifactID, "stale": false, "updated_at": at}).Error
}

// MarkPublished 回写活跃产物指针与发布元数据（发布/回滚共用）。
func (m *Model) MarkPublished(ctx context.Context, pageID, path, artifactID string, at time.Time) (err error) {
	return m.DB(ctx).Where("id = ? AND deleted_at IS NULL", pageID).
		Updates(map[string]any{
			"active_artifact_id": artifactID,
			"active_path":        path,
			"published_at":       at,
			"stale":              false,
			"updated_at":         at,
		}).Error
}

// MoveDraftPath 发布改 URL 后同步草稿路径与活跃路径。
func (m *Model) MoveDraftPath(ctx context.Context, pageID, newPath string, at time.Time) (err error) {
	return m.DB(ctx).Where("id = ? AND deleted_at IS NULL", pageID).
		Updates(map[string]any{"draft_path": newPath, "active_path": newPath, "updated_at": at}).Error
}

// DraftPathValue 返回草稿访问路径（空安全）。
func (e *PageEntity) DraftPathValue() string {
	if e == nil {
		return ""
	}
	return e.DraftPath
}

// ActivePathValue 返回当前线上路径（未发布为空）。
func (e *PageEntity) ActivePathValue() string {
	if e == nil || e.ActivePath == nil {
		return ""
	}
	return *e.ActivePath
}

// DraftDocumentFor 优先返回产物冻结源文档，回退到当前草稿。
func (e *PageEntity) DraftDocumentFor(source json.RawMessage) json.RawMessage {
	if len(source) > 0 {
		return source
	}
	return e.DraftDocument
}
