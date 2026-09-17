package model

// coupon_model.go — 优惠码与核销记录的表访问单元（BIZ-1）。
//
// 优惠码与核销记录是**同一个聚合**：次数上限（coupons.used_count）与核销明细
// （coupon_redemptions）必须一起变，否则会出现「明细两条、计数只加了一次」这种
// 只能靠对账发现的脏数据。因此核销的「插明细 + 加计数 + 次数守卫」作为
// 聚合内原子组合放在本 model，由 service 决定事务边界（Transaction 透传）。

import (
	"context"
	"errors"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"go_wp/pkg/rls"
)

// 优惠类型：percent 按小计百分比 / fixed 固定金额（分）。
const (
	CouponTypePercent = "percent"
	CouponTypeFixed   = "fixed"
)

// 优惠码状态列取值。
const (
	CouponStatusDisabled = 0
	CouponStatusEnabled  = 1
)

// CouponEntity 对应 coupons 表。
type CouponEntity struct {
	ID        uint64 `gorm:"column:id;primaryKey"`
	ProjectID string `gorm:"column:project_id"`
	// Code 券码（存归一化后的大写）：工程内唯一。
	Code string `gorm:"column:code"`
	Name string `gorm:"column:name"`
	// DiscountType / DiscountValue 折扣口径：百分比 1..100 或固定金额（分）。
	DiscountType  string `gorm:"column:discount_type"`
	DiscountValue int64  `gorm:"column:discount_value"`
	// MinSubtotal 使用门槛（分）。
	MinSubtotal int64 `gorm:"column:min_subtotal"`
	// MaxUses 总可用次数（0 = 不限）；UsedCount 由核销原子递增。
	MaxUses   int `gorm:"column:max_uses"`
	UsedCount int `gorm:"column:used_count"`
	// PerUserLimit 每人可用次数（0 = 不限）。
	PerUserLimit int        `gorm:"column:per_user_limit"`
	StartsAt     *time.Time `gorm:"column:starts_at"`
	EndsAt       *time.Time `gorm:"column:ends_at"`
	// Status 1 启用 / 0 停用。停用不删：历史核销记录还要读它。
	Status int `gorm:"column:status"`
	// Remark 备注（活动说明 / 内部口径）。
	Remark     string    `gorm:"column:remark"`
	CreateBy   uint64    `gorm:"column:create_by"`
	UpdateBy   uint64    `gorm:"column:update_by"`
	CreateTime time.Time `gorm:"column:create_time"`
	UpdateTime time.Time `gorm:"column:update_time"`
}

// TableName 实现 gorm 表名（显式给：默认复数推断会得到 coupon_entities）。
func (CouponEntity) TableName() string { return "coupons" }

// CouponRedemptionEntity 对应 coupon_redemptions 表。
//
// 订单号在这里存**快照**：核销记录是「哪张券在哪一单上用掉」的凭据，
// 它必须能独立读出来，不该依赖 orders 表当前的状态。
type CouponRedemptionEntity struct {
	ID        uint64 `gorm:"column:id;primaryKey"`
	CouponID  uint64 `gorm:"column:coupon_id"`
	ProjectID string `gorm:"column:project_id"`
	Code      string `gorm:"column:code"`
	OrderID   uint64 `gorm:"column:order_id"`
	OrderNo   string `gorm:"column:order_no"`
	// DiscountAmount 本次实际抵扣（分）。
	DiscountAmount int64 `gorm:"column:discount_amount"`
	// UserID 核销人（匿名下单时为 NULL）。
	UserID     *uint64   `gorm:"column:user_id"`
	CreateTime time.Time `gorm:"column:create_time"`
}

// TableName 实现 gorm 表名。
func (CouponRedemptionEntity) TableName() string { return "coupon_redemptions" }

// CouponFilter 优惠码列表查询条件。
//
// 这里只有**条件**没有业务判断：status=expired 这类语义由 service 翻译成
// 下面的 Expired / Exhausted / Status 三个参数，model 不认识「过期」这个词。
type CouponFilter struct {
	ProjectID string
	Keyword   string
	// Status 状态列取值过滤（nil = 不过滤）。
	Status *int
	// Expired true 只要已过期的 / false 只要未过期的（nil = 不过滤）。
	Expired *bool
	// Exhausted true 只要次数用尽的 / false 只要还有额的（nil = 不过滤）。
	Exhausted *bool
	Offset    int
	Limit     int
}

// CouponRedemptionFilter 核销记录查询条件。
type CouponRedemptionFilter struct {
	ProjectID string
	CouponID  uint64
	Code      string
	OrderID   uint64
	Offset    int
	Limit     int
}

// CouponModel 优惠码聚合的表访问单元。
type CouponModel struct{ db *gorm.DB }

// NewCouponModel 构造。
func NewCouponModel(db *gorm.DB) *CouponModel { return &CouponModel{db: db} }

// DB 返回绑定本表的句柄（只允许本 model 的仓储方法消费）。
func (m *CouponModel) DB(ctx context.Context) *gorm.DB {
	return m.db.WithContext(ctx).Model(&CouponEntity{})
}

// Transaction 透传事务：核销要与建单同生共死，边界由 service 决定。
func (m *CouponModel) Transaction(ctx context.Context, fn func(tx *gorm.DB) error) error {
	return m.db.WithContext(ctx).Transaction(fn)
}

// Create 新建优惠码。
// RLS（迁移 215）：coupons 已启用 FORCE 策略，写入承 e.ProjectID 的工程作用域。
func (m *CouponModel) Create(ctx context.Context, e *CouponEntity) (err error) {
	return rls.InProjectScope(ctx, m.db, e.ProjectID, func(tx *gorm.DB) error {
		return tx.Model(&CouponEntity{}).Create(e).Error
	})
}

// GetByID 按主键取本工程内的优惠码；不存在返回 (nil, nil)，由 service 决定报什么错。
//
// projectID 必填（DB-009 第五批）：coupons 带 FORCE 策略。原先「为空 = 不限工程」的分支
// 在第四批把调用点改成逐工程定位后已无调用者，留着它就是一条静默 fail-closed 路径
// （表现为「优惠码不存在」）。
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

// GetByCode 按券码取（工程内唯一）；code 传归一化后的大写。
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

// LockByIDTx 事务内按主键加行锁取券（核销路径用）。
// projectID 非空时把作用域设进调用方的事务并在 SQL 里带工程条件
// （核销路径的加锁读，换角色后无作用域会返回 (nil, nil) → 「优惠码不存在」，DB-009 第二批）。
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

// List 优惠码列表。
func (m *CouponModel) List(ctx context.Context, f CouponFilter) (list []*CouponEntity, total int64, err error) {
	err = rls.InProjectScope(ctx, m.db, f.ProjectID, func(tx *gorm.DB) error {
		return m.listLocked(tx, f, &list, &total)
	})
	return list, total, err
}

// listLocked 在已带工程作用域的句柄上执行券列表查询。
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

// UpdateFields 更新指定列（可改列由 service 决定，model 不写死业务规则）。
// projectID 必填（DB-009 第五批）：越界写被 WITH CHECK 拒绝；「为空 = 不限工程」的分支
// 已无调用者，且它在换角色后是**静默 0 行**（接口回报成功、券没改）。
func (m *CouponModel) UpdateFields(ctx context.Context, projectID string, id uint64, fields map[string]any) (err error) {
	if strings.TrimSpace(projectID) == "" {
		return ErrProjectRequired
	}
	return rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {
		return tx.Model(&CouponEntity{}).Where("id = ? AND project_id = ?", id, projectID).Updates(fields).Error
	})
}

// Delete 删除优惠码（是否允许删由 service 判断：有核销记录的不许删）。
// projectID 必填（DB-009 第五批）：同 UpdateFields —— 缺作用域的删除在换角色后静默 0 行。
func (m *CouponModel) Delete(ctx context.Context, projectID string, id uint64) (err error) {
	if strings.TrimSpace(projectID) == "" {
		return ErrProjectRequired
	}
	return rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {
		return tx.Model(&CouponEntity{}).Where("id = ? AND project_id = ?", id, projectID).Delete(&CouponEntity{}).Error
	})
}

// CountRedemptions 统计**本工程内**该券的核销条数；userID 非 nil 时只数该用户的。
//
// projectID 必填（DB-009 第五批）：coupon_redemptions 在迁移 215 名单里（带 FORCE 策略），
// 而这里原先用的是绕开 DB(ctx) 的裸句柄 —— 换非超级角色后**恒返回 0**，两个判据同时失效：
//
//	· 删券拦截（used > 0 才拒绝删）→ 有核销记录的券被删掉，核销明细变成悬空引用；
//	· 每人限领（used >= PerUserLimit）→ 同一用户重复领用不再被拦。
func (m *CouponModel) CountRedemptions(ctx context.Context, projectID string, couponID uint64, userID *uint64) (n int64, err error) {
	if strings.TrimSpace(projectID) == "" {
		return 0, ErrProjectRequired
	}
	err = rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {
		return countRedemptions(ctx, tx.Model(&CouponRedemptionEntity{}), projectID, couponID, userID, &n)
	})
	return n, err
}

// CountRedemptionsTx 事务内统计核销条数（核销路径用，与 LockByIDTx 配合）。
// projectID 必填（DB-009 第五批）：判据同 CountRedemptions（限领校验失效 ⇒ 可重复领用）。
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

// countRedemptions 两个入口共用的条件拼装（工程 + 券 + 可选用户）。
//
// 显式带 project_id 而不是只靠策略：调用方事务里设的作用域与这里的事务各管一段，
// 两边同时成立才不会留下「策略放行、条件漏写」的窗口。
func countRedemptions(ctx context.Context, q *gorm.DB, projectID string, couponID uint64, userID *uint64, n *int64) error {
	q = q.WithContext(ctx).Where("project_id = ? AND coupon_id = ?", projectID, couponID)
	if userID != nil {
		q = q.Where("user_id = ?", *userID)
	}
	return q.Count(n).Error
}

// InsertRedemptionTx 事务内插一条核销明细，返回是否为**本次新插入**。
//
// ON CONFLICT DO NOTHING 命中唯一键 (coupon_id, order_id) 时 RowsAffected 为 0 ——
// 那就是「这一单之前已经核销过这张券」，调用方据此幂等返回，而不是把它当失败。
// 幂等兜底放在数据库唯一约束上而不是「先查一次再插」：后者在并发下必然漏判。
func (m *CouponModel) InsertRedemptionTx(ctx context.Context, tx *gorm.DB, e *CouponRedemptionEntity) (inserted bool, err error) {
	// 核销明细自带工程 id：写入前把 scope 补进 service 的事务（幂等），
	// 使 coupon_redemptions 的 WITH CHECK 在缺 scope 的调用链上也能通过。
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

// GetRedemptionByOrderTx 事务内按订单取核销记录（至多一条，唯一键保证）。
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

// ReleaseRedemptionByOrderTx 取消订单时释放券：删核销明细并把 used_count 减一。
//
// 幂等：没有核销记录或已删过则 (false, nil)。
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

// IncrementUsedTx 事务内把已用次数加一，返回是否**真的加上了**。
//
// 守卫写在 WHERE 里（max_uses = 0 表示不限次；否则要求 used_count < max_uses），
// affected=0 就是用尽 —— 这是原子的：并发两单抢最后一次，只会有一个拿到。
// 不写成「先读出来判断再更新」，那中间有窗口，超发一张券的代价是真金白银。
func (m *CouponModel) IncrementUsedTx(ctx context.Context, tx *gorm.DB, couponID uint64) (ok bool, err error) {
	res := tx.WithContext(ctx).Model(&CouponEntity{}).
		Where("id = ? AND (max_uses = 0 OR used_count < max_uses)", couponID).
		UpdateColumns(map[string]any{"used_count": gorm.Expr("used_count + 1"), "update_time": time.Now()})
	if res.Error != nil {
		return false, res.Error
	}
	return res.RowsAffected > 0, nil
}

// ListRedemptions 核销记录列表。
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
