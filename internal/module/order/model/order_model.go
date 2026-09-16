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

	"go_wp/pkg/rls"
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
// RLS（迁移 215）：orders 已启用 FORCE 策略，写入承 e.ProjectID 的工程作用域。
func (m *OrderModel) Create(ctx context.Context, e *OrderEntity) (err error) {
	return rls.InProjectScope(ctx, m.db, e.ProjectID, func(tx *gorm.DB) error {
		return tx.Model(&OrderEntity{}).Create(e).Error
	})
}

// CreateTx 事务内落一单。
//
// 这里用 ScopeTx 而**不是** InProjectScope：tx 是 service 编排的事务，作用域必须设在
// 它上面。用 InProjectScope 会拿 m.db 另开事务、另取连接 —— 外层事务刚写的行在这个新
// 事务里看不见，同表写入还会自锁。设好之后该事务里**后续所有语句**（明细、状态日志、
// 库存动作）都已在同一工程作用域内，等于顺带把整条下单链路覆盖了。
func (m *OrderModel) CreateTx(ctx context.Context, tx *gorm.DB, e *OrderEntity) (err error) {
	if err = rls.ScopeTx(tx, e.ProjectID); err != nil {
		return err
	}
	return tx.WithContext(ctx).Model(&OrderEntity{}).Create(e).Error
}

// GetByID 按主键取本工程内的订单（projectID 必填，防跨工程 IDOR）。
// 不存在返回 (nil, nil)，由 service 决定报什么错。
//
// projectID 必填（DB-009 第五批）：原先保留着「为空 = 不限工程」的历史分支。第四批把
// 所有按 id 的调用点改成逐工程定位后，那个分支已无调用者 —— 留着它就是一条静默
// fail-closed 路径（换非超级角色后恒返回 (nil, nil) ⇒ 调用方报「订单不存在」）。
func (m *OrderModel) GetByID(ctx context.Context, id uint64, projectID string) (e *OrderEntity, err error) {
	e = &OrderEntity{}
	if strings.TrimSpace(projectID) == "" {
		return nil, ErrProjectRequired
	}
	if err = rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {
		return tx.Model(&OrderEntity{}).Where("id = ? AND project_id = ?", id, projectID).First(e).Error
	}); err != nil {
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
// projectID 非空时在工程作用域内查（DB-009 第二批）：orders 带 FORCE 策略，
// 不设 app.project_id 的读取在非超级角色下会「订单不存在」——访客查自己的订单
// 是**功能回归**而不是安全问题，所以调用方拿到工程时要传下来。
// projectID 必填（DB-009 第五批）：同 GetByID ——「为空 = 不限工程」的分支已无调用者，
// 且它在换角色后是静默 (nil, nil)：访客会看到「订单不存在」而不是「缺少工程上下文」。
func (m *OrderModel) GetByIDForUser(ctx context.Context, projectID string, id uint64, userID uint64) (e *OrderEntity, err error) {
	e = &OrderEntity{}
	if strings.TrimSpace(projectID) == "" {
		return nil, ErrProjectRequired
	}
	if err = rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {
		return tx.Model(&OrderEntity{}).
			Where("id = ? AND user_id = ? AND project_id = ?", id, userID, projectID).First(e).Error
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
// 状态流转必须串行：并发的两次「发货」只应成功一次，否则会写出两条流转记录、
// 或两次库存动作叠加。
// projectID 非空时把作用域设进**调用方的事务**（rls.ScopeTx）并在 SQL 里带工程条件：
// 这是订单状态流转的入口，换角色后无作用域的加锁读会 0 行 —— 表现为「订单不存在」，
// 是功能回归而非安全问题，所以要尽量把工程传下来（DB-009 第二批）。
func (m *OrderModel) LockByIDTx(ctx context.Context, tx *gorm.DB, projectID string, id uint64) (e *OrderEntity, err error) {
	if strings.TrimSpace(projectID) != "" {
		if serr := rls.ScopeTx(tx, projectID); serr != nil {
			return nil, serr
		}
	}
	e = &OrderEntity{}
	q := tx.WithContext(ctx).Model(&OrderEntity{}).
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

// GetByNo 按订单号取单（工程内）。
func (m *OrderModel) GetByNo(ctx context.Context, projectID string, orderNo string) (e *OrderEntity, err error) {
	e = &OrderEntity{}
	if err = rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {
		return tx.Model(&OrderEntity{}).Where("project_id = ? AND order_no = ?", projectID, orderNo).First(e).Error
	}); err != nil {
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
	if err = rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {
		return tx.Model(&OrderEntity{}).Where("project_id = ? AND request_id = ?", projectID, requestID).First(e).Error
	}); err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return e, nil
}

// List 订单列表。关键词匹配订单号 / 客户邮箱 / 客户姓名（ILIKE，PG 专有）。
func (m *OrderModel) List(ctx context.Context, f OrderFilter) (list []*OrderEntity, total int64, err error) {
	err = rls.InProjectScope(ctx, m.db, f.ProjectID, func(tx *gorm.DB) error {
		return m.listLocked(tx, f, &list, &total)
	})
	return list, total, err
}

// listLocked 在已带工程作用域的句柄上执行订单列表查询（拆出来只为让 List 的
// 条件拼装留在原处可读，行为与拆分前逐字一致）。
func (m *OrderModel) listLocked(tx *gorm.DB, f OrderFilter, list *[]*OrderEntity, total *int64) error {
	q := tx.Model(&OrderEntity{}).Where("project_id = ?", f.ProjectID)
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
	if err := q.Count(total).Error; err != nil {
		return err
	}
	limit := f.Limit
	if limit <= 0 || limit > 200 {
		limit = 20
	}
	return q.Omit("attribution").Order("id DESC").Offset(f.Offset).Limit(limit).Find(list).Error
}

// UpdateFields 更新指定字段（调用方只传该改的列）。
// projectID 必填（DB-009 第五批）：越界写会被 WITH CHECK 直接拒绝，而不是静默改到别的
// 工程；原先「为空 = 不限工程」的分支已无调用者，且它在换角色后是**静默 0 行**
// （接口回报成功、数据没动）。
func (m *OrderModel) UpdateFields(ctx context.Context, projectID string, id uint64, fields map[string]any) (err error) {
	if strings.TrimSpace(projectID) == "" {
		return ErrProjectRequired
	}
	return rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {
		return tx.Model(&OrderEntity{}).Where("id = ? AND project_id = ?", id, projectID).Updates(fields).Error
	})
}

// UpdateFieldsTx 事务内更新指定字段（作用域设进调用方的事务，不另开）。
func (m *OrderModel) UpdateFieldsTx(ctx context.Context, tx *gorm.DB, projectID string, id uint64, fields map[string]any) (err error) {
	if strings.TrimSpace(projectID) != "" {
		if serr := rls.ScopeTx(tx, projectID); serr != nil {
			return serr
		}
	}
	q := tx.WithContext(ctx).Model(&OrderEntity{}).Where("id = ?", id)
	if strings.TrimSpace(projectID) != "" {
		q = q.Where("project_id = ?", projectID)
	}
	return q.Updates(fields).Error
}

// ErrProjectRequired 缺少工程作用域（DB-009 第三批）。
//
// orders 在迁移 215 里带 FORCE 策略：不设 app.project_id 的查询在非超级角色下
// **静默返回空集**。超时取消扫描（后台定时任务）天然没有工程参数，因此它改为
// 自己取工程清单后**逐工程**设作用域执行；取不到工程时显式失败，
// 绝不退回「不限工程」—— 那在换角色后就是「待付款单永不超时取消」且无任何日志。
var ErrProjectRequired = errors.New("order: 需要显式工程作用域")

// ListAllProjectIDs 列出全部站点工程 id（超时取消扫描的扇出清单）。
//
// 这是订单模块唯一一处读 projects 表，理由要写清楚：
//   - orders 带 FORCE 策略，扫描必须逐工程设作用域；
//   - 工程清单只能来自 projects 表，而订单模块的装配点（routers 的 SetupOrderRoutes）
//     拿不到 project 契约（且属于并行批次的禁用区，不能改签名注入）；
//   - projects 是隔离的**主体**：它没有 project_id 列、不在迁移 215 的 53 个对象里，
//     读它不涉及任何被隔离数据。analytics model 读同一张表有先例
//     （internal/module/analytics/model/analytics_model.go 的保留期结算）。
func (m *OrderModel) ListAllProjectIDs(ctx context.Context) (ids []string, err error) {
	err = m.db.WithContext(ctx).
		Raw("SELECT id::text FROM projects ORDER BY create_time ASC, id ASC").Scan(&ids).Error
	return ids, err
}

// ListPendingCreatedBefore 列出**指定工程内**创建时间早于 cutoff 的待付款订单（超时取消扫描用）。
//
// projectID 必填（DB-009 第三批）：本方法原是「全表扫描」，而 orders 带 FORCE 策略 ——
// 不设作用域时它在非超级角色下静默返回空集：定时任务跑得好好的，一单都不会被取消，
// 库存被一直占住而日志里没有任何异常。工程清单由 service 逐工程展开调用。
func (m *OrderModel) ListPendingCreatedBefore(ctx context.Context, projectID string, cutoff time.Time, limit int) (list []*OrderEntity, err error) {
	if strings.TrimSpace(projectID) == "" {
		return nil, ErrProjectRequired
	}
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	err = rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {
		return tx.Model(&OrderEntity{}).
			Where("project_id = ? AND status = ? AND create_time < ?", projectID, OrderStatusPending, cutoff).
			Order("create_time ASC").Limit(limit).Find(&list).Error
	})
	return list, err
}

// CountByStatus 按状态分组计数（列表页状态页签的角标）。
func (m *OrderModel) CountByStatus(ctx context.Context, projectID string) (counts map[string]int64, err error) {
	var rows []struct {
		Status string `gorm:"column:status"`
		N      int64  `gorm:"column:n"`
	}
	if err = rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {
		return tx.Model(&OrderEntity{}).Select("status, COUNT(*) AS n").
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

// paidStatuses 计入「累计消费」的订单状态。
//
// 取消与退款不算消费（钱没进来，或者已经退回去了），待付款的单还没付。
// 这份名单只在这里出现一次：页面上的数字与聚合的条件必须同源，
// 两边各写一份的话，它们会在某次「顺手加个状态」之后悄悄分叉。
var paidStatuses = []string{OrderStatusPaid, OrderStatusShipped, OrderStatusCompleted}

// CustomerOrderSummaryRow 「客户订单摘要」一次查询取回的全部事实：
// 聚合三值 + 最近一单的四列（一单都没有时，最近一单四列全为 NULL）。
type CustomerOrderSummaryRow struct {
	OrderCount      int64      `gorm:"column:order_count"`
	PaidOrderCount  int64      `gorm:"column:paid_order_count"`
	TotalAmount     int64      `gorm:"column:total_amount"`
	LastOrderID     *uint64    `gorm:"column:last_order_id"`
	LastOrderNo     *string    `gorm:"column:last_order_no"`
	LastOrderStatus *string    `gorm:"column:last_order_status"`
	LastOrderTime   *time.Time `gorm:"column:last_order_time"`
}

// customerOrderSummarySQL 客户订单摘要的单条查询（窗口函数，见 SummaryByUser 的说明）。
//
// 两处 ? 都是计入消费的状态名单（由 paidStatuses 拼出，名单仍只声明一次）；
// 参数顺序：paid_order_count 名单、total_amount 名单、project_id、user_id。
const customerOrderSummarySQL = `SELECT COALESCE(w.order_count, 0)      AS order_count,
       COALESCE(w.paid_order_count, 0) AS paid_order_count,
       COALESCE(w.total_amount, 0)     AS total_amount,
       w.id          AS last_order_id,
       w.order_no    AS last_order_no,
       w.status      AS last_order_status,
       w.create_time AS last_order_time
  FROM (SELECT 1) AS anchor
  LEFT JOIN (
        SELECT id, order_no, status, create_time,
               COUNT(*) OVER () AS order_count,
               COUNT(*) FILTER (WHERE status = ANY(string_to_array(?, ',')::text[])) OVER () AS paid_order_count,
               COALESCE(SUM(total) FILTER (WHERE status = ANY(string_to_array(?, ',')::text[])) OVER (), 0) AS total_amount
          FROM orders
         WHERE project_id = ? AND user_id = ?
         ORDER BY id DESC
         LIMIT 1
  ) AS w ON TRUE`

// SummaryByUser 按「工程 + 客户」一次取回订单摘要（聚合三值 + 最近一单）。
//
// 为什么收敛成一条 SQL：这条摘要要同时回答四个互相牵连的问题 —— 下过几单、
// 几单计入消费、累计消费多少、最后一单是哪张。拆成三条（聚合 / List 里的分页计数 /
// List 取一单）时，三条之间落的新单会让「3 单 200 元」这种自相矛盾的数字漏出去；
// 其中分页计数那条在摘要场景里连结果都不用（页面不要 total），白扫一遍全量行。
//
// 窗口函数的分工：ORDER BY … LIMIT 1 决定「返回哪一行」（最近一单，与 List 的
// 排序口径一致，主键倒序即最新），带 OVER () 的三个聚合则是**整个过滤结果集**的
// 事实，与被取回的是哪一行无关。外层 LEFT JOIN 到单行哨兵 (SELECT 1) 是必需的：
// 零订单的客户在窗口聚合下不产生任何行，没有哨兵就分不清「一单没下」与
// 「查不到这个客户」—— 而页面正是靠 HasOrders 分支的。
func (m *OrderModel) SummaryByUser(ctx context.Context, projectID string, userID uint64) (row CustomerOrderSummaryRow, err error) {
	err = rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {
		return tx.Raw(customerOrderSummarySQL,
			strings.Join(paidStatuses, ","), strings.Join(paidStatuses, ","),
			projectID, userID).
			Scan(&row).Error
	})
	return row, err
}
