package blockmodel

import (
	"context"
	"errors"
	json "go_wp/pkg/jsonx"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"go_wp/pkg/rls"
)

var ErrProjectRequired = errors.New("block: 需要显式工程作用域")

const tableNameBlocks = "blocks"

const (
	KindBlock        = "block"
	KindHeader       = "header"
	KindFooter       = "footer"
	KindAnnouncement = "announcement"
	KindSidebar      = "sidebar"
	KindBreadcrumb   = "breadcrumb"
	KindDrawer       = "drawer"
	KindSearch       = "search"
	KindCTA          = "cta"
	KindTrust        = "trust"
	KindBrands       = "brands"
	KindContact      = "contact"
	KindAbout        = "about"
	KindBanner       = "banner"
	KindGrid         = "grid"
	KindSnippet      = "snippet"
)

const (
	ReuseGlobal = "global"

	ReuseTemplate = "template"
)

const DefaultCategory = "general"

type BlockEntity struct {
	ID         string          `gorm:"column:id;primaryKey"`
	ProjectID  string          `gorm:"column:project_id;not null"`
	Name       string          `gorm:"column:name;not null"`
	Kind       string          `gorm:"column:kind;not null"`
	Category   string          `gorm:"column:category;not null;default:general"`
	ReuseMode  string          `gorm:"column:reuse_mode;not null;default:global"`
	Document   json.RawMessage `gorm:"column:document;type:jsonb;not null"`
	CreateTime time.Time       `gorm:"column:create_time;not null"`
	UpdatedAt  time.Time       `gorm:"column:update_time;not null"`

	Version int64 `gorm:"column:version;not null;default:1"`

	PresetKey     *string `gorm:"column:preset_key"`
	PresetVersion *int    `gorm:"column:preset_version"`
}

func (BlockEntity) TableName() string { return tableNameBlocks }

type Model struct {
	db *gorm.DB
}

func NewBlockModel(db *gorm.DB) *Model { return &Model{db: db} }

func (m *Model) DB(ctx context.Context) *gorm.DB {
	return m.db.WithContext(ctx).Model(&BlockEntity{})
}

func (m *Model) Create(ctx context.Context, e *BlockEntity) (err error) {
	return rls.InProjectScope(ctx, m.db, e.ProjectID, func(tx *gorm.DB) error {
		return tx.Model(&BlockEntity{}).Create(e).Error
	})
}

func (m *Model) ListByProject(ctx context.Context, projectID, kind, category, reuseMode string) (list []BlockEntity, err error) {
	err = rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {
		q := tx.Model(&BlockEntity{}).Where("project_id = ?", projectID)
		if kind != "" {
			q = q.Where("kind = ?", kind)
		}
		if category != "" {
			q = q.Where("category = ?", category)
		}
		if reuseMode != "" {
			q = q.Where("reuse_mode = ?", reuseMode)
		}
		return q.Order("kind ASC, create_time ASC").Find(&list).Error
	})
	return list, err
}

func (m *Model) ExistsByName(ctx context.Context, projectID, name string) (exists bool, err error) {
	var count int64
	err = rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {
		return tx.Model(&BlockEntity{}).
			Where("project_id = ? AND LOWER(name) = LOWER(?)", projectID, name).
			Count(&count).Error
	})
	return count > 0, err
}

func (m *Model) GetByID(ctx context.Context, id string, projectID string) (e *BlockEntity, err error) {
	e = &BlockEntity{}

	if strings.TrimSpace(projectID) == "" {
		if err = m.DB(ctx).Where("id = ?", id).First(e).Error; err != nil {
			return nil, err
		}
		return e, nil
	}
	if err = rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {
		return tx.Model(&BlockEntity{}).Where("id = ?", id).Where("project_id = ?", projectID).First(e).Error
	}); err != nil {
		return nil, err
	}
	return e, nil
}

func (m *Model) UpdateDocumentTx(ctx context.Context, projectID, id string, expectedVersion uint64,
	name, kind, category, reuseMode string, document json.RawMessage, updatedAt time.Time) (affected int64, err error) {
	updates := map[string]any{
		"name": name, "kind": kind, "category": category, "reuse_mode": reuseMode,
		"document": document, "update_time": updatedAt,
	}
	if expectedVersion == 0 {
		return 0, gorm.ErrInvalidData
	}
	updates["version"] = gorm.Expr("version + 1")
	apply := func(db *gorm.DB) error {
		q := db.Model(&BlockEntity{}).Where("id = ?", id)
		q = q.Where("version = ?", expectedVersion)
		res := q.Updates(updates)
		affected, err = res.RowsAffected, res.Error
		return res.Error
	}
	if strings.TrimSpace(projectID) == "" {
		err = apply(m.DB(ctx))
		return affected, err
	}
	err = rls.InProjectScope(ctx, m.db, projectID, apply)
	return affected, err
}

type BlockDocRefRow struct {
	ID   string `gorm:"column:id"`
	Name string `gorm:"column:name"`
}

func (m *Model) ListBlockDocumentRefs(ctx context.Context, projectID, blockID string) (rows []BlockDocRefRow, err error) {
	if strings.TrimSpace(projectID) == "" {
		return nil, ErrProjectRequired
	}
	err = rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {

		return tx.Table(BlockEntity{}.TableName()).
			Select("id::text AS id, name").
			Where("project_id = ?", projectID).
			Where("id::text <> ?::text", blockID).
			Where("jsonb_path_query_array(document, '$.**.blockId') @> jsonb_build_array(?::text)", blockID).
			Order("name ASC, id ASC").
			Scan(&rows).Error
	})
	return rows, err
}

// DeleteTx 在调用方事务内删除一个块并返回受影响行数；工程作用域由事务持有者设置。
func (m *Model) DeleteTx(ctx context.Context, tx *gorm.DB, projectID, id string) (affected int64, err error) {
	if tx == nil {
		return 0, ErrProjectRequired
	}
	q := tx.WithContext(ctx).Where("id = ?", id)
	if strings.TrimSpace(projectID) != "" {
		q = q.Where("project_id = ?", projectID)
	}
	res := q.Delete(&BlockEntity{})
	return res.RowsAffected, res.Error
}

// TransactionAny 提供一个不带工程作用域的事务句柄：跨工程批量写在同一事务里
// 按工程逐段调用 ScopeTx 切换作用域，避免逐工程独立提交造成半成功。
func (m *Model) TransactionAny(ctx context.Context, fn func(tx *gorm.DB) error) error {
	return m.db.WithContext(ctx).Transaction(fn)
}

// ScopeTx 在调用方事务内切换工程作用域；RLS 会话变量是事务级的，可多次覆盖。
func (m *Model) ScopeTx(tx *gorm.DB, projectID string) error {
	return rls.ScopeTx(tx, projectID)
}

// LockByIDsTx 按 id 升序锁定同一工程内的块，供批量删除在同一事务内复核存在性。
func (m *Model) LockByIDsTx(ctx context.Context, tx *gorm.DB, projectID string, ids []string) (list []BlockEntity, err error) {
	if tx == nil {
		return nil, ErrProjectRequired
	}
	if len(ids) == 0 {
		return nil, nil
	}
	err = tx.WithContext(ctx).Model(&BlockEntity{}).
		Where("project_id = ? AND id IN ?", projectID, ids).
		Order("id ASC").
		Clauses(clause.Locking{Strength: "UPDATE"}).
		Find(&list).Error
	return list, err
}

// Transaction 提供一个工程作用域内的事务，供批量删除整体提交或回滚。
func (m *Model) Transaction(ctx context.Context, projectID string, fn func(tx *gorm.DB) error) error {
	return rls.InProjectScope(ctx, m.db, projectID, fn)
}

func (m *Model) Delete(ctx context.Context, projectID, id string) (err error) {
	if strings.TrimSpace(projectID) == "" {
		return m.DB(ctx).Where("id = ?", id).Delete(&BlockEntity{}).Error
	}
	return rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {
		return tx.Model(&BlockEntity{}).Where("id = ? AND project_id = ?", id, projectID).Delete(&BlockEntity{}).Error
	})
}
