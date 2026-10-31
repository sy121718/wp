package membershipmodel

import (
	"context"
	"time"

	"gorm.io/gorm"
)

type EntitlementEntity struct {
	ID         int64     `gorm:"column:id;primaryKey"`
	TierID     int64     `gorm:"column:tier_id"`
	Kind       string    `gorm:"column:kind"`
	ValueInt   int64     `gorm:"column:value_int"`
	CreateTime time.Time `gorm:"column:create_time"`
	UpdateTime time.Time `gorm:"column:update_time"`
}

func (EntitlementEntity) TableName() string { return "membership_entitlements" }

func (m *Model) ListByTierTx(ctx context.Context, tx *gorm.DB, tierID int64) (list []*EntitlementEntity, err error) {
	err = tx.WithContext(ctx).Model(&EntitlementEntity{}).
		Where("tier_id = ?", tierID).
		Order("kind ASC").
		Find(&list).Error
	return list, err
}

func (m *Model) ListByTierIDsTx(ctx context.Context, tx *gorm.DB, tierIDs []int64) (out map[int64][]*EntitlementEntity, err error) {
	out = map[int64][]*EntitlementEntity{}
	if len(tierIDs) == 0 {
		return out, nil
	}
	var rows []*EntitlementEntity
	if err = tx.WithContext(ctx).Model(&EntitlementEntity{}).
		Where("tier_id IN ?", tierIDs).
		Order("tier_id ASC, kind ASC").
		Find(&rows).Error; err != nil {
		return nil, err
	}
	for _, r := range rows {
		out[r.TierID] = append(out[r.TierID], r)
	}
	return out, nil
}

func (m *Model) ReplaceForTierTx(ctx context.Context, tx *gorm.DB, tierID int64, rows []*EntitlementEntity) error {
	if err := tx.WithContext(ctx).Model(&EntitlementEntity{}).
		Where("tier_id = ?", tierID).
		Delete(&EntitlementEntity{}).Error; err != nil {
		return err
	}
	if len(rows) == 0 {
		return nil
	}
	now := time.Now()
	for _, r := range rows {
		r.TierID = tierID
		r.CreateTime = now
		r.UpdateTime = now
	}
	return tx.WithContext(ctx).Model(&EntitlementEntity{}).Create(&rows).Error
}

func (m *Model) DeleteForTierTx(ctx context.Context, tx *gorm.DB, tierID int64) (int64, error) {
	res := tx.WithContext(ctx).Model(&EntitlementEntity{}).
		Where("tier_id = ?", tierID).
		Delete(&EntitlementEntity{})
	return res.RowsAffected, res.Error
}
