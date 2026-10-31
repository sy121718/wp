package commentmodel

import (
	"context"
	"strings"
	"time"

	"gorm.io/gorm"

	commentenums "go_wp/internal/module/comment/enums"
	"go_wp/pkg/rls"
)

type Entity struct {
	ID int64 `gorm:"column:id;primaryKey"`

	ProjectID string `gorm:"column:project_id"`

	EntityType string `gorm:"column:entity_type"`

	EntityID string `gorm:"column:entity_id"`

	UserID uint64 `gorm:"column:user_id"`

	ParentID *int64 `gorm:"column:parent_id"`

	Body string `gorm:"column:body"`

	Status string `gorm:"column:status"`

	AuthorIPHash string `gorm:"column:author_ip_hash"`

	ReviewedBy *uint64 `gorm:"column:reviewed_by"`

	ReviewedAt *time.Time `gorm:"column:reviewed_at"`

	CreateTime time.Time  `gorm:"column:create_time"`
	UpdateTime time.Time  `gorm:"column:update_time"`
	DeletedAt  *time.Time `gorm:"column:deleted_at"`
}

func (Entity) TableName() string { return "comments" }

type Model struct {
	db *gorm.DB
}

func NewModel(db *gorm.DB) *Model { return &Model{db: db} }

func (m *Model) DB(ctx context.Context) *gorm.DB {
	return m.db.WithContext(ctx).Model(&Entity{})
}

func (m *Model) Transaction(ctx context.Context, fn func(tx *gorm.DB) error) error {
	return m.db.WithContext(ctx).Transaction(fn)
}

func (m *Model) TransactionScoped(ctx context.Context, projectID string, fn func(tx *gorm.DB) error) error {
	return m.Transaction(ctx, func(tx *gorm.DB) error {
		if err := rls.ScopeTx(tx, projectID); err != nil {
			return err
		}
		return fn(tx)
	})
}

func (m *Model) Insert(ctx context.Context, e *Entity) error {
	if e == nil {
		return gorm.ErrInvalidData
	}
	return m.TransactionScoped(ctx, e.ProjectID, func(tx *gorm.DB) error {
		return m.InsertTx(ctx, tx, e)
	})
}

func (m *Model) InsertTx(ctx context.Context, tx *gorm.DB, e *Entity) error {
	if e == nil || tx == nil {
		return gorm.ErrInvalidData
	}
	now := time.Now()
	if e.CreateTime.IsZero() {
		e.CreateTime = now
	}
	if e.UpdateTime.IsZero() {
		e.UpdateTime = now
	}
	if strings.TrimSpace(e.Status) == "" {

		e.Status = commentenums.StatusPending
	}
	return tx.WithContext(ctx).Model(&Entity{}).Create(e).Error
}

func (m *Model) ListTopLevel(ctx context.Context, projectID, entityType, entityID string, limit, offset int) (list []*Entity, err error) {
	err = m.TransactionScoped(ctx, projectID, func(tx *gorm.DB) error {
		var ierr error
		list, ierr = m.ListTopLevelTx(ctx, tx, projectID, entityType, entityID, limit, offset)
		return ierr
	})
	return list, err
}

func (m *Model) ListTopLevelTx(ctx context.Context, tx *gorm.DB, projectID, entityType, entityID string, limit, offset int) (list []*Entity, err error) {
	err = tx.WithContext(ctx).Model(&Entity{}).
		Where("project_id = ? AND entity_type = ? AND entity_id = ? AND status = ? AND parent_id IS NULL AND deleted_at IS NULL",
			projectID, entityType, entityID, commentenums.StatusApproved).
		Order("create_time DESC, id DESC").
		Limit(limit).Offset(offset).
		Find(&list).Error
	return list, err
}

func (m *Model) CountTopLevel(ctx context.Context, projectID, entityType, entityID string) (total int64, err error) {
	err = m.TransactionScoped(ctx, projectID, func(tx *gorm.DB) error {
		return tx.WithContext(ctx).Model(&Entity{}).
			Where("project_id = ? AND entity_type = ? AND entity_id = ? AND status = ? AND parent_id IS NULL AND deleted_at IS NULL",
				projectID, entityType, entityID, commentenums.StatusApproved).
			Count(&total).Error
	})
	return total, err
}

func (m *Model) ListRepliesOf(ctx context.Context, projectID, entityType, entityID string, parentIDs []int64) (list []*Entity, err error) {
	if len(parentIDs) == 0 {
		return nil, nil
	}
	err = m.TransactionScoped(ctx, projectID, func(tx *gorm.DB) error {
		return tx.WithContext(ctx).Model(&Entity{}).
			Where("project_id = ? AND entity_type = ? AND entity_id = ? AND status = ? AND parent_id IN ? AND deleted_at IS NULL",
				projectID, entityType, entityID, commentenums.StatusApproved, parentIDs).
			Order("create_time ASC, id ASC").
			Find(&list).Error
	})
	return list, err
}

func (m *Model) GetByID(ctx context.Context, projectID string, id int64) (e *Entity, err error) {
	err = m.TransactionScoped(ctx, projectID, func(tx *gorm.DB) error {
		var row Entity
		ferr := tx.WithContext(ctx).Model(&Entity{}).
			Where("id = ? AND project_id = ? AND deleted_at IS NULL", id, projectID).
			First(&row).Error
		if ferr == gorm.ErrRecordNotFound {
			return nil
		}
		if ferr != nil {
			return ferr
		}
		e = &row
		return nil
	})
	return e, err
}

type ReviewQuery struct {
	ProjectID string

	Statuses []string

	EntityType string

	Keyword string

	Limit  int
	Offset int
}

func (m *Model) ListForReview(ctx context.Context, q ReviewQuery) (list []*Entity, total int64, err error) {
	err = m.TransactionScoped(ctx, q.ProjectID, func(tx *gorm.DB) error {
		base := func() *gorm.DB {
			db := tx.WithContext(ctx).Model(&Entity{}).
				Where("project_id = ? AND deleted_at IS NULL", q.ProjectID)
			if len(q.Statuses) > 0 {
				db = db.Where("status IN ?", q.Statuses)
			}
			if q.EntityType != "" {
				db = db.Where("entity_type = ?", q.EntityType)
			}
			if kw := strings.TrimSpace(q.Keyword); kw != "" {

				db = db.Where("body ILIKE ? ESCAPE '\\'", "%"+escapeLike(kw)+"%")
			}
			return db
		}
		if cerr := base().Count(&total).Error; cerr != nil {
			return cerr
		}
		return base().
			Order("CASE WHEN status = '" + commentenums.StatusPending + "' THEN 0 ELSE 1 END ASC, create_time DESC, id DESC").
			Limit(q.Limit).Offset(q.Offset).
			Find(&list).Error
	})
	return list, total, err
}

func (m *Model) UpdateStatus(ctx context.Context, projectID string, ids []int64, status string, reviewerID uint64) (affected int64, err error) {
	if len(ids) == 0 {
		return 0, nil
	}
	now := time.Now()
	err = m.TransactionScoped(ctx, projectID, func(tx *gorm.DB) error {
		res := tx.WithContext(ctx).Model(&Entity{}).
			Where("id IN ? AND project_id = ? AND deleted_at IS NULL", ids, projectID).
			Updates(map[string]any{
				"status":      status,
				"reviewed_by": reviewerID,
				"reviewed_at": now,
				"update_time": now,
			})
		affected = res.RowsAffected
		return res.Error
	})
	return affected, err
}

func (m *Model) CountBySourceSince(ctx context.Context, projectID, authorIPHash string, since time.Time) (n int64, err error) {
	if strings.TrimSpace(authorIPHash) == "" {
		return 0, nil
	}
	err = m.TransactionScoped(ctx, projectID, func(tx *gorm.DB) error {
		return tx.WithContext(ctx).Model(&Entity{}).
			Where("project_id = ? AND author_ip_hash = ? AND create_time >= ?", projectID, authorIPHash, since).
			Count(&n).Error
	})
	return n, err
}

func escapeLike(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, "%", `\%`)
	s = strings.ReplaceAll(s, "_", `\_`)
	return s
}
