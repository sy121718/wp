package model

// return_model.go — 退货申请与退货明细的表访问单元（BIZ-1，退货入库）。
//
// 两张表是**同一个聚合**（order_returns + order_return_items）：退货单头与它的明细
// 必须一起变，所以「建单头 + 写明细」作为聚合内原子组合放在本 model（由 service 决定事务边界）。
//
// 「已退多少」不存冗余计数，靠明细聚合算出；本 model 只提供**参数化**的求和，
// 「哪些申请算占用额度」是业务规则，由 service 先算出申请 id 再传进来 ——
// model 不认识「已拒绝的不占额度」这种话。

import (
	"context"
	"errors"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"go_wp/pkg/rls"
)

// 退货单状态。
//
//	requested 客户已申请 → approved 管理员同意待收货 → received 已入库待退款 → completed 完成
//	requested → rejected 已拒绝 / cancelled 客户撤销（两个终态）
//
// **received 的含义是「货已经入库」**，它是入库那一步的**门闩**：
// 只有把状态从 approved 推进到 received 的那一次调用才执行入库，
// 因此重复点击、重试、并发点两次都不会把同一批货加两遍。
const (
	ReturnStatusRequested = "requested"
	ReturnStatusApproved  = "approved"
	ReturnStatusReceived  = "received"
	ReturnStatusCompleted = "completed"
	ReturnStatusRejected  = "rejected"
	ReturnStatusCancelled = "cancelled"
)

// ReturnActiveStatuses 占用退货额度的状态集合。
//
// 被**拒绝**与**撤销**的不占额度 —— 客户被拒之后当然可以改个理由重新申请；
// 把它们也算占用，等于一次手滑的申请就永久锁死了这件商品的退货权。
var ReturnActiveStatuses = []string{
	ReturnStatusRequested, ReturnStatusApproved, ReturnStatusReceived, ReturnStatusCompleted,
}

// ReturnEntity 对应 order_returns 表。
type ReturnEntity struct {
	ID        uint64 `gorm:"column:id;primaryKey"`
	ProjectID string `gorm:"column:project_id;type:uuid"`
	OrderID   uint64 `gorm:"column:order_id"`
	OrderNo   string `gorm:"column:order_no;type:varchar(40)"`
	ReturnNo  string `gorm:"column:return_no;type:varchar(40)"`
	Status    string `gorm:"column:status;type:varchar(20)"`
	Reason    string `gorm:"column:reason;type:varchar(255)"`
	// RefundAmount 整单退款额（分，明细之和）。
	RefundAmount  int64      `gorm:"column:refund_amount"`
	UserID        *uint64    `gorm:"column:user_id"`
	CustomerEmail string     `gorm:"column:customer_email;type:varchar(120)"`
	CustomerName  string     `gorm:"column:customer_name;type:varchar(60)"`
	AdminNote     string     `gorm:"column:admin_note;type:varchar(500)"`
	ReviewerID    uint64     `gorm:"column:reviewer_id"`
	ReviewerName  string     `gorm:"column:reviewer_name;type:varchar(60)"`
	ReviewedAt    *time.Time `gorm:"column:reviewed_at;type:timestamp(3)"`
	ReceivedAt    *time.Time `gorm:"column:received_at;type:timestamp(3)"`
	RefundedAt    *time.Time `gorm:"column:refunded_at;type:timestamp(3)"`
	TransactionID string     `gorm:"column:transaction_id;type:varchar(120)"`
	RequestID     string     `gorm:"column:request_id;type:varchar(64)"`
	CreateTime    time.Time  `gorm:"column:create_time;type:timestamp(3)"`
	UpdateTime    time.Time  `gorm:"column:update_time;type:timestamp(3)"`
}

// TableName 实现 gorm 表名（显式给：默认复数推断会得到 return_entities）。
func (ReturnEntity) TableName() string { return "order_returns" }

// ReturnItemEntity 对应 order_return_items 表（商品与价格全是快照）。
type ReturnItemEntity struct {
	ID           uint64 `gorm:"column:id;primaryKey"`
	ReturnID     uint64 `gorm:"column:return_id"`
	OrderItemID  uint64 `gorm:"column:order_item_id"`
	ProductID    string `gorm:"column:product_id;type:uuid"`
	VariantID    string `gorm:"column:variant_id;type:uuid"`
	ProductName  string `gorm:"column:product_name;type:varchar(200)"`
	VariantLabel string `gorm:"column:variant_label;type:varchar(200)"`
	SKU          string `gorm:"column:sku;type:varchar(80)"`
	UnitPrice    int64  `gorm:"column:unit_price"`
	Quantity     int    `gorm:"column:quantity"`
	// ReceivedQuantity 实际入库数量（首版等于申请数量，列留着以备「少件 / 折价」）。
	ReceivedQuantity int       `gorm:"column:received_quantity"`
	RefundAmount     int64     `gorm:"column:refund_amount"`
	CreateTime       time.Time `gorm:"column:create_time;type:timestamp(3)"`
}

// TableName 实现 gorm 表名。
func (ReturnItemEntity) TableName() string { return "order_return_items" }

// ReturnFilter 退货单列表查询条件（只有条件，没有业务判断）。
type ReturnFilter struct {
	ProjectID string
	Status    string
	Keyword   string // 退货单号 / 订单号 / 客户邮箱
	OrderID   uint64
	// UserID 非 nil 时只列该访客自己的申请（访客侧查询用）。
	UserID *uint64
	Offset int
	Limit  int
}

// ReturnModel 退货聚合的表访问单元。
type ReturnModel struct{ db *gorm.DB }

// NewReturnModel 构造。
func NewReturnModel(db *gorm.DB) *ReturnModel { return &ReturnModel{db: db} }

// DB 返回绑定本表的句柄（只允许本 model 的仓储方法消费）。
func (m *ReturnModel) DB(ctx context.Context) *gorm.DB {
	return m.db.WithContext(ctx).Model(&ReturnEntity{})
}

// Transaction 透传事务：建单头 + 写明细由 service 划边界。
func (m *ReturnModel) Transaction(ctx context.Context, fn func(tx *gorm.DB) error) error {
	return m.db.WithContext(ctx).Transaction(fn)
}

// CreateTx 事务内写退货单头。
func (m *ReturnModel) CreateTx(ctx context.Context, tx *gorm.DB, e *ReturnEntity) (err error) {
	// 用 ScopeTx 而不是 InProjectScope：tx 是 service 编排的事务，作用域必须设在它上面
	// （另开事务会看不到外层未提交数据、并与外层同表写入自锁）。
	if serr := rls.ScopeTx(tx, e.ProjectID); serr != nil {
		return serr
	}
	return tx.WithContext(ctx).Model(&ReturnEntity{}).Create(e).Error
}

// CreateItemsTx 事务内批量写明细。
func (m *ReturnModel) CreateItemsTx(ctx context.Context, tx *gorm.DB, items []*ReturnItemEntity) (err error) {
	if len(items) == 0 {
		return nil
	}
	return tx.WithContext(ctx).Model(&ReturnItemEntity{}).Create(&items).Error
}

// GetByID 按主键取本工程内的退货单；不存在返回 (nil, nil)。
//
// projectID 必填（DB-009 第五批）：order_returns 带 FORCE 策略。原先「为空 = 不限工程」
// 的分支在第四批改造后已无调用者，留着它就是静默 fail-closed（表现为「退货单不存在」）。
func (m *ReturnModel) GetByID(ctx context.Context, projectID string, id uint64) (e *ReturnEntity, err error) {
	e = &ReturnEntity{}
	if strings.TrimSpace(projectID) == "" {
		return nil, ErrProjectRequired
	}
	if err = rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {
		return tx.Model(&ReturnEntity{}).Where("id = ? AND project_id = ?", id, projectID).First(e).Error
	}); err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return e, nil
}

// LockByIDTx 事务内按主键加行锁取单。
//
// 审核与收货都必须串行：并发两次「确认收货」若都读到 approved，就会都去入库 ——
// 而入库没有幂等键，结果是同一批货被加了两遍。
// projectID 非空时把作用域设进调用方的事务并在 SQL 里带工程条件（DB-009 第二批）。
func (m *ReturnModel) LockByIDTx(ctx context.Context, tx *gorm.DB, projectID string, id uint64) (e *ReturnEntity, err error) {
	if strings.TrimSpace(projectID) != "" {
		if serr := rls.ScopeTx(tx, projectID); serr != nil {
			return nil, serr
		}
	}
	e = &ReturnEntity{}
	q := tx.WithContext(ctx).Model(&ReturnEntity{}).
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

// GetByRequestID 按幂等键取（工程内）。
func (m *ReturnModel) GetByRequestID(ctx context.Context, projectID, requestID string) (e *ReturnEntity, err error) {
	if strings.TrimSpace(requestID) == "" {
		return nil, nil
	}
	e = &ReturnEntity{}
	if err = rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {
		return tx.Model(&ReturnEntity{}).Where("project_id = ? AND request_id = ?", projectID, requestID).First(e).Error
	}); err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return e, nil
}

// List 退货单列表。
func (m *ReturnModel) List(ctx context.Context, f ReturnFilter) (list []*ReturnEntity, total int64, err error) {
	err = rls.InProjectScope(ctx, m.db, f.ProjectID, func(tx *gorm.DB) error {
		return m.listLocked(tx, f, &list, &total)
	})
	return list, total, err
}

// listLocked 在已带工程作用域的句柄上执行退货单列表查询。
func (m *ReturnModel) listLocked(tx *gorm.DB, f ReturnFilter, list *[]*ReturnEntity, total *int64) error {
	q := tx.Model(&ReturnEntity{}).Where("project_id = ?", f.ProjectID)
	if f.Status != "" {
		q = q.Where("status = ?", f.Status)
	}
	if f.OrderID != 0 {
		q = q.Where("order_id = ?", f.OrderID)
	}
	if f.UserID != nil {
		q = q.Where("user_id = ?", *f.UserID)
	}
	if kw := strings.TrimSpace(f.Keyword); kw != "" {
		like := "%" + kw + "%"
		q = q.Where("return_no ILIKE ? OR order_no ILIKE ? OR customer_email ILIKE ?", like, like, like)
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

// CountByStatus 按状态分组计数（列表页状态页签的角标）。
func (m *ReturnModel) CountByStatus(ctx context.Context, projectID string) (counts map[string]int64, err error) {
	var rows []struct {
		Status string `gorm:"column:status"`
		N      int64  `gorm:"column:n"`
	}
	if err = rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {
		return tx.Model(&ReturnEntity{}).Select("status, COUNT(*) AS n").
			Where("project_id = ?", projectID).Group("status").Scan(&rows).Error
	}); err != nil {
		return nil, err
	}
	counts = make(map[string]int64, len(rows))
	for _, r := range rows {
		counts[r.Status] = r.N
	}
	return counts, nil
}

// UpdateFieldsTx 事务内更新指定列（作用域设进调用方的事务，不另开）。
func (m *ReturnModel) UpdateFieldsTx(ctx context.Context, tx *gorm.DB, projectID string, id uint64, fields map[string]any) (err error) {
	if strings.TrimSpace(projectID) != "" {
		if serr := rls.ScopeTx(tx, projectID); serr != nil {
			return serr
		}
	}
	q := tx.WithContext(ctx).Model(&ReturnEntity{}).Where("id = ?", id)
	if strings.TrimSpace(projectID) != "" {
		q = q.Where("project_id = ?", projectID)
	}
	return q.Updates(fields).Error
}

// ItemsByReturnID 某退货单的明细。
func (m *ReturnModel) ItemsByReturnID(ctx context.Context, returnID uint64) (items []*ReturnItemEntity, err error) {
	err = m.db.WithContext(ctx).Model(&ReturnItemEntity{}).
		Where("return_id = ?", returnID).Order("id ASC").Find(&items).Error
	return items, err
}

// ItemsByReturnIDs 批量取明细（列表页一次取齐，不做 N+1）。
func (m *ReturnModel) ItemsByReturnIDs(ctx context.Context, returnIDs []uint64) (items []*ReturnItemEntity, err error) {
	if len(returnIDs) == 0 {
		return nil, nil
	}
	err = m.db.WithContext(ctx).Model(&ReturnItemEntity{}).
		Where("return_id IN ?", returnIDs).Order("id ASC").Find(&items).Error
	return items, err
}

// UpdateItemReceivedTx 事务内登记某行的入库数量与退款额。
func (m *ReturnModel) UpdateItemReceivedTx(ctx context.Context, tx *gorm.DB, itemID uint64, received int, refund int64) (err error) {
	return tx.WithContext(ctx).Model(&ReturnItemEntity{}).Where("id = ?", itemID).
		Updates(map[string]any{"received_quantity": received, "refund_amount": refund}).Error
}

// ActiveIDsByOrder 该订单下**占用退货额度**的申请 id（哪些算占用由 service 说，见调用处）。
//
// 拆成「先取 id、再按 id 求和」两步而不是一次 join：本层不做多表关联，
// 而「已拒绝 / 已撤销不算占用」是业务判断，不该写进查询层。
//
// projectID 必填（DB-009 第五批）：order_returns 带 FORCE 策略，不带作用域时这条查询在
// 非超级角色下**静默返回空集** —— 「已占用额度」恒为 0 ⇒ 可退数量被高估 ⇒ 允许超退。
// 这是本批里唯一的**业务数据风险**（不只是功能缺失），所以作用域 + 显式 project_id 双保险。
func (m *ReturnModel) IDsByOrder(ctx context.Context, projectID string, orderID uint64, statuses []string) (ids []uint64, err error) {
	if strings.TrimSpace(projectID) == "" {
		return nil, ErrProjectRequired
	}
	if orderID == 0 || len(statuses) == 0 {
		return nil, nil
	}
	err = rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {
		return tx.Model(&ReturnEntity{}).
			Where("project_id = ? AND order_id = ? AND status IN ?", projectID, orderID, statuses).
			Order("id ASC").Pluck("id", &ids).Error
	})
	return ids, err
}

// IDsByOrderTx 事务内取占用额度的申请 id。
// projectID 必填（DB-009 第五批）：与 IDsByOrder 同一判据（漏作用域 ⇒ 可退数量高估 ⇒ 超退）。
// 这里用 rls.ScopeTx 设在调用方的事务上（不另开事务），并在 SQL 里显式带 project_id。
func (m *ReturnModel) IDsByOrderTx(ctx context.Context, tx *gorm.DB, projectID string, orderID uint64, statuses []string) (ids []uint64, err error) {
	if strings.TrimSpace(projectID) == "" {
		return nil, ErrProjectRequired
	}
	if orderID == 0 || len(statuses) == 0 {
		return nil, nil
	}
	if serr := rls.ScopeTx(tx, projectID); serr != nil {
		return nil, serr
	}
	err = tx.WithContext(ctx).Model(&ReturnEntity{}).
		Where("project_id = ? AND order_id = ? AND status IN ?", projectID, orderID, statuses).
		Order("id ASC").Pluck("id", &ids).Error
	return ids, err
}

// SumQuantityByOrderItems 统计这些申请里、这些订单项的**已申请退货数量**。
//
// returnIDs 为空表示没有任何额度被占用（返回空表，不是「全部」—— 语义差一个词，
// 而这里搞错就是超退）。
func (m *ReturnModel) SumQuantityByOrderItems(ctx context.Context, returnIDs []uint64, orderItemIDs []uint64) (sums map[uint64]int, err error) {
	sums = map[uint64]int{}
	if len(returnIDs) == 0 || len(orderItemIDs) == 0 {
		return sums, nil
	}
	var rows []struct {
		OrderItemID uint64 `gorm:"column:order_item_id"`
		Qty         int64  `gorm:"column:qty"`
	}
	if err = m.db.WithContext(ctx).Model(&ReturnItemEntity{}).
		Select("order_item_id, SUM(quantity) AS qty").
		Where("return_id IN ? AND order_item_id IN ?", returnIDs, orderItemIDs).
		Group("order_item_id").Scan(&rows).Error; err != nil {
		return nil, err
	}
	for _, r := range rows {
		sums[r.OrderItemID] = int(r.Qty)
	}
	return sums, nil
}

// SumQuantityByOrderItemsTx 事务内统计已申请退货数量。
func (m *ReturnModel) SumQuantityByOrderItemsTx(ctx context.Context, tx *gorm.DB, returnIDs []uint64, orderItemIDs []uint64) (sums map[uint64]int, err error) {
	sums = map[uint64]int{}
	if len(returnIDs) == 0 || len(orderItemIDs) == 0 {
		return sums, nil
	}
	var rows []struct {
		OrderItemID uint64 `gorm:"column:order_item_id"`
		Qty         int64  `gorm:"column:qty"`
	}
	if err = tx.WithContext(ctx).Model(&ReturnItemEntity{}).
		Select("order_item_id, SUM(quantity) AS qty").
		Where("return_id IN ? AND order_item_id IN ?", returnIDs, orderItemIDs).
		Group("order_item_id").Scan(&rows).Error; err != nil {
		return nil, err
	}
	for _, r := range rows {
		sums[r.OrderItemID] = int(r.Qty)
	}
	return sums, nil
}
