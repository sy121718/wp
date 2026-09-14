package model

// order_model.go — 订单头的表访问单元（BIZ-1 销售侧）。
//
// 定位照 AGENTS.md「model 层定位」：这里是 Repository，不是领域模型 ——
// 只做本模块表的 CRUD 与通用查询，条件一律以参数传入；
// 状态机（哪条流转边合法、谁有权流转）留在 service。

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// 订单状态。取值集合与迁移 135 的注释一致：
//
//	pending 待付款 → paid 已付款 → shipped 已发货 → completed 已完成
//	pending / paid / shipped → cancelled 已取消
//	paid / shipped / completed → refunded 已退款
//
// 合法流转边由 service 判定；这里只声明取值，不做校验（model 不放业务规则）。
const (
	OrderStatusPending   = "pending"
	OrderStatusPaid      = "paid"
	OrderStatusShipped   = "shipped"
	OrderStatusCompleted = "completed"
	OrderStatusCancelled = "cancelled"
	OrderStatusRefunded  = "refunded"
)

// 下单入口：后台代客下单与访客结账要能区分（审计与纠纷取证都要看这个）。
const (
	CreatedViaCheckout = "checkout"
	CreatedViaAdmin    = "admin"
	CreatedViaAPI      = "api"
)

// 状态流转的操作人类型。
const (
	OperatorTypeAdmin    = "admin"
	OperatorTypeSystem   = "system"
	OperatorTypeCustomer = "customer"
)

// OrderEntity 对应 orders 表。
//
// 客户信息、收货地址、账单地址与金额分解全部是**快照**列：商品改名改价、
// 用户改邮箱改地址，都不该改写历史订单。
type OrderEntity struct {
	ID                 uint64     `gorm:"column:id;primaryKey"`
	ProjectID          string     `gorm:"column:project_id;type:uuid"`
	OrderNo            string     `gorm:"column:order_no;type:varchar(40)"`
	Status             string     `gorm:"column:status;type:varchar(20)"`
	UserID             *uint64    `gorm:"column:user_id"`
	CustomerEmail      string     `gorm:"column:customer_email;type:varchar(120)"`
	CustomerName       string     `gorm:"column:customer_name;type:varchar(60)"`
	CustomerPhone      string     `gorm:"column:customer_phone;type:varchar(40)"`
	Currency           string     `gorm:"column:currency;type:varchar(8)"`
	Subtotal           int64      `gorm:"column:subtotal"`
	DiscountTotal      int64      `gorm:"column:discount_total"`
	ShippingTotal      int64      `gorm:"column:shipping_total"`
	TaxTotal           int64      `gorm:"column:tax_total"`
	Total              int64      `gorm:"column:total"`
	ShipName           string     `gorm:"column:ship_name;type:varchar(60)"`
	ShipPhone          string     `gorm:"column:ship_phone;type:varchar(40)"`
	ShipProvince       string     `gorm:"column:ship_province;type:varchar(40)"`
	ShipCity           string     `gorm:"column:ship_city;type:varchar(40)"`
	ShipDistrict       string     `gorm:"column:ship_district;type:varchar(40)"`
	ShipAddress        string     `gorm:"column:ship_address;type:varchar(255)"`
	ShipZip            string     `gorm:"column:ship_zip;type:varchar(20)"`
	BillName           string     `gorm:"column:bill_name;type:varchar(60)"`
	BillPhone          string     `gorm:"column:bill_phone;type:varchar(40)"`
	BillProvince       string     `gorm:"column:bill_province;type:varchar(40)"`
	BillCity           string     `gorm:"column:bill_city;type:varchar(40)"`
	BillDistrict       string     `gorm:"column:bill_district;type:varchar(40)"`
	BillAddress        string     `gorm:"column:bill_address;type:varchar(255)"`
	BillZip            string     `gorm:"column:bill_zip;type:varchar(20)"`
	PaymentMethod      string     `gorm:"column:payment_method;type:varchar(40)"`
	PaymentMethodTitle string     `gorm:"column:payment_method_title;type:varchar(60)"`
	TransactionID      string     `gorm:"column:transaction_id;type:varchar(120)"`
	PaidAt             *time.Time `gorm:"column:paid_at;type:timestamp(3)"`
	CompletedAt        *time.Time `gorm:"column:completed_at;type:timestamp(3)"`
	CreatedVia         string     `gorm:"column:created_via;type:varchar(20)"`
	IPAddress          string     `gorm:"column:ip_address;type:varchar(50)"`
	UserAgent          string     `gorm:"column:user_agent;type:varchar(255)"`
	// Attribution 归因与轨迹快照（JSONB）：下单时刻的流量来源 / 广告参数 / 会话 / 浏览轨迹。
	// 存 RawMessage 而不是结构化类型 —— 形状由 dto 定义，model 不重复声明一遍。
	Attribution json.RawMessage `gorm:"column:attribution;type:jsonb;not null"`
	// AdminNote 后台备注：自建订单与代发订单的填写位置（与客户填的 remark 分开）。
	AdminNote    string    `gorm:"column:admin_note;type:varchar(500)"`
	RequestID    string    `gorm:"column:request_id;type:varchar(64)"`
	Remark       string    `gorm:"column:remark;type:varchar(255)"`
	CancelReason string    `gorm:"column:cancel_reason;type:varchar(255)"`
	CreateBy     uint64    `gorm:"column:create_by"`
	CreateTime   time.Time `gorm:"column:create_time;type:timestamp(3)"`
	UpdateTime   time.Time `gorm:"column:update_time;type:timestamp(3)"`
}

// TableName 实现 gorm 表名。
//
// 必须显式给：默认的复数推断会把 OrderEntity 推成 order_entities，与迁移建的表名不一致，
// 报错是 relation "order_entities" does not exist —— 看起来像迁移没跑，实际只是名字对不上。
func (OrderEntity) TableName() string { return "orders" }

// OrderFilter 订单列表查询条件（条件一律以参数传入，方法内不写死业务条件）。
type OrderFilter struct {
	ProjectID     string
	Status        string
	UserID        *uint64
	Keyword       string // 订单号 / 客户邮箱 / 客户姓名
	PaymentMethod string
	CreatedFrom   *time.Time
	CreatedTo     *time.Time
	Offset        int
	Limit         int
}

// OrderModel 订单头表访问单元。
type OrderModel struct{ db *gorm.DB }

func NewOrderModel(db *gorm.DB) *OrderModel { return &OrderModel{db: db} }

// DB 返回绑定本表的句柄（**只允许本 model 的仓储方法消费**，service 不得调用）。
func (m *OrderModel) DB(ctx context.Context) *gorm.DB {
	return m.db.WithContext(ctx).Model(&OrderEntity{})
}

// Transaction 透传事务：跨表 / 跨模块编排由 service 决定边界。
func (m *OrderModel) Transaction(ctx context.Context, fn func(tx *gorm.DB) error) error {
	return m.db.WithContext(ctx).Transaction(fn)
}

// Create 落一单。
func (m *OrderModel) Create(ctx context.Context, e *OrderEntity) (err error) {
	return m.DB(ctx).Create(e).Error
}

// CreateTx 事务内落一单。
func (m *OrderModel) CreateTx(ctx context.Context, tx *gorm.DB, e *OrderEntity) (err error) {
	return tx.WithContext(ctx).Model(&OrderEntity{}).Create(e).Error
}

// GetByID 按主键取单；projectID 非空时追加工程归属条件（防跨工程 IDOR）。
// 不存在返回 (nil, nil)，由 service 决定报什么错。
func (m *OrderModel) GetByID(ctx context.Context, id uint64, projectID string) (e *OrderEntity, err error) {
	e = &OrderEntity{}
	q := m.DB(ctx).Where("id = ?", id)
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

// GetByIDForUser 按主键 + 归属取单（访客侧专用）。
//
// 归属条件写在 SQL 里而不是「取回来再比对」：后者的失败模式是「访客看到别人的订单」，
// 而它只差一次调用顺序的调整。查不到与不属于本人返回同一个结果（nil），
// 让调用方无法用响应差异探测订单是否存在。
func (m *OrderModel) GetByIDForUser(ctx context.Context, id uint64, userID uint64) (e *OrderEntity, err error) {
	e = &OrderEntity{}
	if err = m.DB(ctx).Where("id = ? AND user_id = ?", id, userID).First(e).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return e, nil
}

// LockByIDTx 事务内按主键加行锁取单。
//
// 状态流转必须串行：并发的两次「发货」只应成功一次，否则会写出两条流转记录、
// 或两次库存动作叠加。
func (m *OrderModel) LockByIDTx(ctx context.Context, tx *gorm.DB, id uint64) (e *OrderEntity, err error) {
	e = &OrderEntity{}
	if err = tx.WithContext(ctx).Model(&OrderEntity{}).
		Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("id = ?", id).First(e).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return e, nil
}

// GetByNo 按订单号取单（工程内）。
func (m *OrderModel) GetByNo(ctx context.Context, projectID string, orderNo string) (e *OrderEntity, err error) {
	e = &OrderEntity{}
	if err = m.DB(ctx).Where("project_id = ? AND order_no = ?", projectID, orderNo).First(e).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return e, nil
}

// GetByRequestID 按幂等键取单：重复提交命中既有单时原样返回。
func (m *OrderModel) GetByRequestID(ctx context.Context, projectID string, requestID string) (e *OrderEntity, err error) {
	if strings.TrimSpace(requestID) == "" {
		return nil, nil
	}
	e = &OrderEntity{}
	if err = m.DB(ctx).Where("project_id = ? AND request_id = ?", projectID, requestID).First(e).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return e, nil
}

// List 订单列表。关键词匹配订单号 / 客户邮箱 / 客户姓名（ILIKE，PG 专有）。
func (m *OrderModel) List(ctx context.Context, f OrderFilter) (list []*OrderEntity, total int64, err error) {
	q := m.DB(ctx).Where("project_id = ?", f.ProjectID)
	if f.Status != "" {
		q = q.Where("status = ?", f.Status)
	}
	if f.UserID != nil {
		q = q.Where("user_id = ?", *f.UserID)
	}
	if f.PaymentMethod != "" {
		q = q.Where("payment_method = ?", f.PaymentMethod)
	}
	if f.CreatedFrom != nil {
		q = q.Where("create_time >= ?", *f.CreatedFrom)
	}
	if f.CreatedTo != nil {
		q = q.Where("create_time <= ?", *f.CreatedTo)
	}
	if kw := strings.TrimSpace(f.Keyword); kw != "" {
		like := "%" + kw + "%"
		q = q.Where("order_no ILIKE ? OR customer_email ILIKE ? OR customer_name ILIKE ?", like, like, like)
	}
	if err = q.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	limit := f.Limit
	if limit <= 0 || limit > 200 {
		limit = 20
	}
	err = q.Omit("attribution").Order("id DESC").Offset(f.Offset).Limit(limit).Find(&list).Error
	return list, total, err
}

// UpdateFields 更新指定字段（调用方只传该改的列）。
func (m *OrderModel) UpdateFields(ctx context.Context, id uint64, fields map[string]any) (err error) {
	return m.DB(ctx).Where("id = ?", id).Updates(fields).Error
}

// UpdateFieldsTx 事务内更新指定字段。
func (m *OrderModel) UpdateFieldsTx(ctx context.Context, tx *gorm.DB, id uint64, fields map[string]any) (err error) {
	return tx.WithContext(ctx).Model(&OrderEntity{}).Where("id = ?", id).Updates(fields).Error
}

// ListPendingCreatedBefore 列出创建时间早于 cutoff 的待付款订单（超时取消扫描用）。
func (m *OrderModel) ListPendingCreatedBefore(ctx context.Context, cutoff time.Time, limit int) (list []*OrderEntity, err error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	err = m.DB(ctx).Where("status = ? AND create_time < ?", OrderStatusPending, cutoff).
		Order("create_time ASC").Limit(limit).Find(&list).Error
	return list, err
}

// CountByStatus 按状态分组计数（列表页状态页签的角标）。
func (m *OrderModel) CountByStatus(ctx context.Context, projectID string) (counts map[string]int64, err error) {
	var rows []struct {
		Status string `gorm:"column:status"`
		N      int64  `gorm:"column:n"`
	}
	if err = m.DB(ctx).Select("status, COUNT(*) AS n").
		Where("project_id = ?", projectID).Group("status").Scan(&rows).Error; err != nil {
		return nil, err
	}
	counts = make(map[string]int64, len(rows))
	for _, r := range rows {
		counts[r.Status] = r.N
	}
	return counts, nil
}

// paidStatuses 计入「累计消费」的订单状态。
//
// 取消与退款不算消费（钱没进来，或者已经退回去了），待付款的单还没付。
// 这份名单只在这里出现一次：页面上的数字与聚合的条件必须同源，
// 两边各写一份的话，它们会在某次「顺手加个状态」之后悄悄分叉。
var paidStatuses = []string{OrderStatusPaid, OrderStatusShipped, OrderStatusCompleted}

// CustomerOrderAggregate 按客户聚合的订单事实。
type CustomerOrderAggregate struct {
	OrderCount     int64
	PaidOrderCount int64
	TotalAmount    int64
}

// AggregateByUser 按「工程 + 客户」聚合订单数量与累计消费（分）。
//
// 单数与金额在**同一条 SQL** 里算出来：分两次查时，第二次之前刚好落了一单，
// 就会得到「3 单 200 元」这种自相矛盾的数字 —— 对不上账的汇总比没有汇总更糟。
func (m *OrderModel) AggregateByUser(ctx context.Context, projectID string, userID uint64) (agg CustomerOrderAggregate, err error) {
	var row struct {
		OrderCount     int64 `gorm:"column:order_count"`
		PaidOrderCount int64 `gorm:"column:paid_order_count"`
		TotalAmount    int64 `gorm:"column:total_amount"`
	}
	if err = m.DB(ctx).
		Select("COUNT(*) AS order_count, "+
			"COALESCE(SUM(CASE WHEN status IN ? THEN 1 ELSE 0 END), 0) AS paid_order_count, "+
			"COALESCE(SUM(CASE WHEN status IN ? THEN total ELSE 0 END), 0) AS total_amount",
			paidStatuses, paidStatuses).
		Where("project_id = ? AND user_id = ?", projectID, userID).
		Scan(&row).Error; err != nil {
		return agg, err
	}
	return CustomerOrderAggregate{
		OrderCount:     row.OrderCount,
		PaidOrderCount: row.PaidOrderCount,
		TotalAmount:    row.TotalAmount,
	}, nil
}
