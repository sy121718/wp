package sysconfigmodel

import (
	"context"

	"gorm.io/gorm"
)

func (m *Model) Transaction(ctx context.Context, fn func(tx *gorm.DB) error) error {
	return m.db.WithContext(ctx).Transaction(fn)
}

func (m *Model) UpdateDataWithVersionTx(tx *gorm.DB, groupKey string, version int64, data JSONMap, updateBy int64) (int64, error) {
	res := tx.Model(&SysConfigEntity{}).
		Where("group_key = ? AND version = ?", groupKey, version).
		Updates(map[string]any{
			"config_data": data,
			"version":     gorm.Expr("version + 1"),
			"update_by":   updateBy,
			"update_time": gorm.Expr("now()"),
		})
	return res.RowsAffected, res.Error
}

func (m *Model) VersionOfTx(tx *gorm.DB, groupKey string) (version int64, found bool, err error) {
	var row struct {
		Version int64 `gorm:"column:version"`
	}
	terr := tx.Model(&SysConfigEntity{}).Select("version").Where("group_key = ?", groupKey).Take(&row).Error
	if terr == gorm.ErrRecordNotFound {
		return 0, false, nil
	}
	if terr != nil {
		return 0, false, terr
	}
	return row.Version, true, nil
}
