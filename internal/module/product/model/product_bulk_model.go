package productmodel

import (
	"context"
	"database/sql/driver"
	"strconv"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"go_wp/pkg/rls"
)

type textArrayParam []string

func (a textArrayParam) Value() (driver.Value, error) {
	return encodePGTextArray(a), nil
}

func encodePGTextArray(ids []string) string {
	var b strings.Builder
	b.WriteByte('{')
	for i, id := range ids {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteString(strconv.Quote(id))
	}
	b.WriteByte('}')
	return b.String()
}

func (m *Model) LockProductsTx(ctx context.Context, tx *gorm.DB, ids []string, projectID string) (locked []string, err error) {
	if tx == nil || len(ids) == 0 || projectID == "" {
		return nil, nil
	}
	err = tx.WithContext(ctx).Model(&ProductEntity{}).
		Where("id IN ? AND project_id = ?", ids, projectID).
		Clauses(clause.Locking{Strength: "UPDATE"}).
		Order("id ASC").
		Pluck("id", &locked).Error
	return locked, err
}

func (m *Model) DeleteProductsTx(ctx context.Context, tx *gorm.DB, ids []string) (n int64, err error) {
	return m.deleteByIDsTx(ctx, tx, &ProductEntity{}, ids)
}

func (m *Model) DeleteCategoriesTx(ctx context.Context, tx *gorm.DB, ids []string) (n int64, err error) {
	return m.deleteByIDsTx(ctx, tx, &ProductCategoryEntity{}, ids)
}

func (m *Model) DeleteBrandsTx(ctx context.Context, tx *gorm.DB, ids []string) (n int64, err error) {
	return m.deleteByIDsTx(ctx, tx, &ProductBrandEntity{}, ids)
}

func (m *Model) DeleteTagsTx(ctx context.Context, tx *gorm.DB, ids []string) (n int64, err error) {
	return m.deleteByIDsTx(ctx, tx, &ProductTagEntity{}, ids)
}

func (m *Model) DeleteAttributesTx(ctx context.Context, tx *gorm.DB, ids []string) (n int64, err error) {
	return m.deleteByIDsTx(ctx, tx, &ProductAttributeEntity{}, ids)
}

func (m *Model) deleteByIDsTx(ctx context.Context, tx *gorm.DB, entity any, ids []string) (n int64, err error) {
	if tx == nil || len(ids) == 0 {
		return 0, nil
	}
	res := tx.WithContext(ctx).Model(entity).Where("id IN ?", ids).Delete(entity)
	return res.RowsAffected, res.Error
}

func (m *Model) CountCategoryChildrenByIDs(ctx context.Context, parentIDs []string, projectID string) (counts map[string]int64, err error) {
	counts = map[string]int64{}
	if len(parentIDs) == 0 {
		return counts, nil
	}
	type childCountRow struct {
		ParentID string `gorm:"column:parent_id"`
		N        int64  `gorm:"column:n"`
	}
	rows := make([]childCountRow, 0, len(parentIDs))
	err = rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {
		return tx.WithContext(ctx).Model(&ProductCategoryEntity{}).
			Select("parent_id, COUNT(*) AS n").
			Where("parent_id IN ?", parentIDs).
			Group("parent_id").
			Find(&rows).Error
	})
	if err != nil {
		return nil, err
	}
	for _, r := range rows {
		counts[r.ParentID] = r.N
	}
	return counts, nil
}

func (m *Model) RemoveTagsFromProductsTx(tx *gorm.DB, tagIDs []string, projectID string, now time.Time) (n int64, err error) {
	if tx == nil || len(tagIDs) == 0 {
		return 0, nil
	}
	upd := tx.Model(&ProductEntity{}).
		Where("jsonb_exists_any(tag_ids, ?::text[])", textArrayParam(tagIDs))
	if projectID != "" {
		upd = upd.Where("project_id = ?", projectID)
	}
	res := upd.UpdateColumns(map[string]any{
		"tag_ids":     gorm.Expr("tag_ids - ?::text[]", textArrayParam(tagIDs)),
		"update_time": now,
	})
	return res.RowsAffected, res.Error
}
