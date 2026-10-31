package model

import (
	"context"
	"errors"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"go_wp/pkg/rls"
)

const (
	CouponTypePercent = "percent"
	CouponTypeFixed   = "fixed"
)

const (
	CouponStatusDisabled = 0
	CouponStatusEnabled  = 1
)

type CouponEntity struct {
	ID        uint64 `gorm:"column:id;primaryKey"`
	ProjectID string `gorm:"column:project_id"`

	Code string `gorm:"column:code"`
	Name string `gorm:"column:name"`

	DiscountType  string `gorm:"column:discount_type"`
	DiscountValue int64  `gorm:"column:discount_value"`

	MinSubtotal int64 `gorm:"column:min_subtotal"`

	MaxUses   int `gorm:"column:max_uses"`
	UsedCount int `gorm:"column:used_count"`

	PerUserLimit int        `gorm:"column:per_user_limit"`
	StartsAt     *time.Time `gorm:"column:starts_at"`
	EndsAt       *time.Time `gorm:"column:ends_at"`

	Status int `gorm:"column:status"`

	Remark     string    `gorm:"column:remark"`
	CreateBy   uint64    `gorm:"column:create_by"`
	UpdateBy   uint64    `gorm:"column:update_by"`
	CreateTime time.Time `gorm:"column:create_time"`
	UpdateTime time.Time `gorm:"column:update_time"`
}

func (CouponEntity) TableName() string { return "coupons" }

type CouponRedemptionEntity struct {
	ID        uint64 `gorm:"column:id;primaryKey"`
	CouponID  uint64 `gorm:"column:coupon_id"`
	ProjectID string `gorm:"column:project_id"`
	Code      string `gorm:"column:code"`
	OrderID   uint64 `gorm:"column:order_id"`
	OrderNo   string `gorm:"column:order_no"`

	DiscountAmount int64 `gorm:"column:discount_amount"`

	UserID     *uint64   `gorm:"column:user_id"`
	CreateTime time.Time `gorm:"column:create_time"`
}

func (CouponRedemptionEntity) TableName() string { return "coupon_redemptions" }

type CouponFilter struct {
	ProjectID string
	Keyword   string

	Status *int

	Expired *bool

	Exhausted *bool
	Offset    int
	Limit     int
}

type CouponRedemptionFilter struct {
	ProjectID string
	CouponID  uint64
	Code      string
	OrderID   uint64
	Offset    int
	Limit     int
}

type CouponModel struct{ db *gorm.DB }

func NewCouponModel(db *gorm.DB) *CouponModel { return &CouponModel{db: db} }

func (m *CouponModel) DB(ctx context.Context) *gorm.DB {
	return m.db.WithContext(ctx).Model(&CouponEntity{})
}

func (m *CouponModel) Transaction(ctx context.Context, fn func(tx *gorm.DB) error) error {
	return m.db.WithContext(ctx).Transaction(fn)
}

func (m *CouponModel) Create(ctx context.Context, e *CouponEntity) (err error) {
	return rls.InProjectScope(ctx, m.db, e.ProjectID, func(tx *gorm.DB) error {
		return tx.Model(&CouponEntity{}).Create(e).Error
	})
}

func (m *CouponModel) GetByID(ctx context.Context, projectID string, id uint64) (e *CouponEntity, err error) {
	e = &CouponEntity{}
	if strings.TrimSpace(projectID) == "" {
		return nil, ErrProjectRequired
	}
	if err = rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {
		return tx.Model(&CouponEntity{}).Where("id = ? AND project_id = ?", id, projectID).First(e).Error
	}); err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return e, nil
}

func (m *CouponModel) GetByCode(ctx context.Context, projectID string, code string) (e *CouponEntity, err error) {
	e = &CouponEntity{}
	if err = rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {
		return tx.Model(&CouponEntity{}).Where("project_id = ? AND code = ?", projectID, code).First(e).Error
	}); err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return e, nil
}

func (m *CouponModel) LockByIDTx(ctx context.Context, tx *gorm.DB, projectID string, id uint64) (e *CouponEntity, err error) {
	if strings.TrimSpace(projectID) != "" {
		if serr := rls.ScopeTx(tx, projectID); serr != nil {
			return nil, serr
		}
	}
	e = &CouponEntity{}
	q := tx.WithContext(ctx).Model(&CouponEntity{}).
		Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("id = ?", id)
	if strings.TrimSpace(projectID) != "" {
		q = q.Where("project_id = ?", projectID)
	}
	if err = q.First(e).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return e, nil
}

func (m *CouponModel) List(ctx context.Context, f CouponFilter) (list []*CouponEntity, total int64, err error) {
	err = rls.InProjectScope(ctx, m.db, f.ProjectID, func(tx *gorm.DB) error {
		return m.listLocked(tx, f, &list, &total)
	})
	return list, total, err
}

func (m *CouponModel) listLocked(tx *gorm.DB, f CouponFilter, list *[]*CouponEntity, total *int64) error {
	q := tx.Model(&CouponEntity{}).Where("project_id = ?", f.ProjectID)
	if f.Status != nil {
		q = q.Where("status = ?", *f.Status)
	}
	if f.Expired != nil {
		now := time.Now()
		if *f.Expired {
			q = q.Where("ends_at IS NOT NULL AND ends_at < ?", now)
		} else {
			q = q.Where("ends_at IS NULL OR ends_at >= ?", now)
		}
	}
	if f.Exhausted != nil {
		if *f.Exhausted {
			q = q.Where("max_uses > 0 AND used_count >= max_uses")
		} else {
			q = q.Where("max_uses = 0 OR used_count < max_uses")
		}
	}
	if kw := strings.TrimSpace(f.Keyword); kw != "" {
		like := "%" + kw + "%"
		q = q.Where("code ILIKE ? OR name ILIKE ?", like, like)
	}
	if err := q.Count(total).Error; err != nil {
		return err
	}
	limit := f.Limit
	if limit <= 0 || limit > 200 {
		limit = 20
	}
	return q.Order("id DESC").Offset(f.Offset).Limit(limit).Find(list).Error
}

func (m *CouponModel) UpdateFields(ctx context.Context, projectID string, id uint64, fields map[string]any) (err error) {
	if strings.TrimSpace(projectID) == "" {
		return ErrProjectRequired
	}
	return rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {
		return tx.Model(&CouponEntity{}).Where("id = ? AND project_id = ?", id, projectID).Updates(fields).Error
	})
}

func (m *CouponModel) Delete(ctx context.Context, projectID string, id uint64) (err error) {
	if strings.TrimSpace(projectID) == "" {
		return ErrProjectRequired
	}
	return rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {
		return tx.Model(&CouponEntity{}).Where("id = ? AND project_id = ?", id, projectID).Delete(&CouponEntity{}).Error
	})
}

func (m *CouponModel) CountRedemptions(ctx context.Context, projectID string, couponID uint64, userID *uint64) (n int64, err error) {
	if strings.TrimSpace(projectID) == "" {
		return 0, ErrProjectRequired
	}
	err = rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {
		return countRedemptions(ctx, tx.Model(&CouponRedemptionEntity{}), projectID, couponID, userID, &n)
	})
	return n, err
}

func (m *CouponModel) CountRedemptionsTx(ctx context.Context, tx *gorm.DB, projectID string, couponID uint64, userID *uint64) (n int64, err error) {
	if strings.TrimSpace(projectID) == "" {
		return 0, ErrProjectRequired
	}
	if serr := rls.ScopeTx(tx, projectID); serr != nil {
		return 0, serr
	}
	if err = countRedemptions(ctx, tx.Model(&CouponRedemptionEntity{}), projectID, couponID, userID, &n); err != nil {
		return 0, err
	}
	return n, nil
}

func countRedemptions(ctx context.Context, q *gorm.DB, projectID string, couponID uint64, userID *uint64, n *int64) error {
	q = q.WithContext(ctx).Where("project_id = ? AND coupon_id = ?", projectID, couponID)
	if userID != nil {
		q = q.Where("user_id = ?", *userID)
	}
	return q.Count(n).Error
}

func (m *CouponModel) InsertRedemptionTx(ctx context.Context, tx *gorm.DB, e *CouponRedemptionEntity) (inserted bool, err error) {

	if serr := rls.ScopeTx(tx, e.ProjectID); serr != nil {
		return false, serr
	}
	res := tx.WithContext(ctx).Model(&CouponRedemptionEntity{}).
		Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "coupon_id"}, {Name: "order_id"}},
			DoNothing: true,
		}).
		Create(e)
	if res.Error != nil {
		return false, res.Error
	}
	return res.RowsAffected > 0, nil
}

func (m *CouponModel) GetRedemptionByOrderTx(ctx context.Context, tx *gorm.DB, orderID uint64) (e *CouponRedemptionEntity, err error) {
	if orderID == 0 {
		return nil, nil
	}
	e = &CouponRedemptionEntity{}
	if err = tx.WithContext(ctx).Model(&CouponRedemptionEntity{}).
		Where("order_id = ?", orderID).First(e).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return e, nil
}

func (m *CouponModel) ReleaseRedemptionByOrderTx(ctx context.Context, tx *gorm.DB, orderID uint64) (released bool, err error) {
	red, err := m.GetRedemptionByOrderTx(ctx, tx, orderID)
	if err != nil || red == nil {
		return false, err
	}
	res := tx.WithContext(ctx).Where("id = ?", red.ID).Delete(&CouponRedemptionEntity{})
	if res.Error != nil {
		return false, res.Error
	}
	if res.RowsAffected == 0 {
		return false, nil
	}
	dec := tx.WithContext(ctx).Model(&CouponEntity{}).
		Where("id = ? AND used_count > 0", red.CouponID).
		UpdateColumns(map[string]any{"used_count": gorm.Expr("used_count - 1"), "update_time": time.Now()})
	if dec.Error != nil {
		return false, dec.Error
	}
	return true, nil
}

func (m *CouponModel) IncrementUsedTx(ctx context.Context, tx *gorm.DB, couponID uint64) (ok bool, err error) {
	res := tx.WithContext(ctx).Model(&CouponEntity{}).
		Where("id = ? AND (max_uses = 0 OR used_count < max_uses)", couponID).
		UpdateColumns(map[string]any{"used_count": gorm.Expr("used_count + 1"), "update_time": time.Now()})
	if res.Error != nil {
		return false, res.Error
	}
	return res.RowsAffected > 0, nil
}

func (m *CouponModel) ListRedemptions(ctx context.Context, f CouponRedemptionFilter) (list []*CouponRedemptionEntity, total int64, err error) {
	err = rls.InProjectScope(ctx, m.db, f.ProjectID, func(tx *gorm.DB) error {
		q := tx.Model(&CouponRedemptionEntity{}).Where("project_id = ?", f.ProjectID)
		if f.CouponID != 0 {
			q = q.Where("coupon_id = ?", f.CouponID)
		}
		if code := strings.TrimSpace(f.Code); code != "" {
			q = q.Where("code = ?", code)
		}
		if f.OrderID != 0 {
			q = q.Where("order_id = ?", f.OrderID)
		}
		if cerr := q.Count(&total).Error; cerr != nil {
			return cerr
		}
		limit := f.Limit
		if limit <= 0 || limit > 200 {
			limit = 20
		}
		return q.Order("id DESC").Offset(f.Offset).Limit(limit).Find(&list).Error
	})
	return list, total, err
}
