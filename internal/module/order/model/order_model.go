package model

// order_model.go — 订单头的表访问单元（BIZ-1 销售侧）。
//
// 定位照 AGENTS.md「model 层定位」：这里是 Repository，不是领域模型 ——
// 只做本模块表的 CRUD 与通用查询，条件一律以参数传入；
// 状态机（哪条流转边合法、谁有权流转）留在 service。

import (
	"context"
	"errors"
	"fmt"
	json "go_wp/pkg/jsonx"
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
	ID            uint64  `gorm:"column:id;primaryKey"`
	ProjectID     string  `gorm:"column:project_id"`
	OrderNo       string  `gorm:"column:order_no"`
	Status        string  `gorm:"column:status"`
	UserID        *uint64 `gorm:"column:user_id"`
	CustomerEmail string  `gorm:"column:customer_email"`
	CustomerName  string  `gorm:"column:customer_name"`
	CustomerPhone string  `gorm:"column:customer_phone"`
	Currency      string  `gorm:"column:currency"`
	Subtotal      int64   `gorm:"column:subtotal"`
	DiscountTotal int64   `gorm:"column:discount_total"`
	// MembershipDiscountTotal 会员折扣金额（分，BIZ-3）：与 DiscountTotal（券）**各自独立计账**、
	// 相加即总扣减。迁移 462 已加列（BIGINT NOT NULL DEFAULT 0），本批开始写入。
	MembershipDiscountTotal int64  `gorm:"column:membership_discount_total"`
	ShippingTotal           int64  `gorm:"column:shipping_total"`
	TaxTotal                int64  `gorm:"column:tax_total"`
	Total                   int64  `gorm:"column:total"`
	ShipName                string `gorm:"column:ship_name"`
	ShipPhone               string `gorm:"column:ship_phone"`
	ShipProvince            string `gorm:"column:ship_province"`
	ShipCity                string `gorm:"column:ship_city"`
	ShipDistrict            string `gorm:"column:ship_district"`
	ShipAddress             string `gorm:"column:ship_address"`
	ShipZip                 string `gorm:"column:ship_zip"`
	// ShipCountry / BillCountry 国家/地区代码快照（迁移 501，ISO 3166-1 alpha-2）。
	// 列型不在这里声明（AGENTS.md）：真源是迁移 SQL。
	ShipCountry        string     `gorm:"column:ship_country"`
	BillName           string     `gorm:"column:bill_name"`
	BillPhone          string     `gorm:"column:bill_phone"`
	BillProvince       string     `gorm:"column:bill_province"`
	BillCity           string     `gorm:"column:bill_city"`
	BillDistrict       string     `gorm:"column:bill_district"`
	BillAddress        string     `gorm:"column:bill_address"`
	BillZip            string     `gorm:"column:bill_zip"`
	BillCountry        string     `gorm:"column:bill_country"`
	PaymentMethod      string     `gorm:"column:payment_method"`
	PaymentMethodTitle string     `gorm:"column:payment_method_title"`
	TransactionID      string     `gorm:"column:transaction_id"`
	PaidAt             *time.Time `gorm:"column:paid_at"`
	CompletedAt        *time.Time `gorm:"column:completed_at"`
	CreatedVia         string     `gorm:"column:created_via"`
	IPAddress          string     `gorm:"column:ip_address"`
	UserAgent          string     `gorm:"column:user_agent"`
	// Attribution 归因与轨迹快照（JSONB）：下单时刻的流量来源 / 广告参数 / 会话 / 浏览轨迹。
	// 存 RawMessage 而不是结构化类型 —— 形状由 dto 定义，model 不重复声明一遍。
	Attribution json.RawMessage `gorm:"column:attribution;type:jsonb;not null"`
	// AdminNote 后台备注：自建订单与代发订单的填写位置（与客户填的 remark 分开）。
	AdminNote    string    `gorm:"column:admin_note"`
	RequestID    string    `gorm:"column:request_id"`
	Remark       string    `gorm:"column:remark"`
	CancelReason string    `gorm:"column:cancel_reason"`
	CreateBy     uint64    `gorm:"column:create_by"`
	CreateTime   time.Time `gorm:"column:create_time"`
	UpdateTime   time.Time `gorm:"column:update_time"`

	// —— 最近一次发货的快照（迁移 627，S1）——
	//
	// 订单头只留「最后一次」：一张订单可以多次发货（部分发货 / 分仓），
	// 历史发货看 order_shipments，不看这里。carrier_name / tracking_no 是**名称快照**，
	// 承运商改名 / 停用 / 删除之后这里仍显示当时的名字。
	// —— 汇率快照（迁移 633，W5）——
	//
	// FXRate 下单那一刻的汇率（1 订单币 = fx_rate 基准币）；nil = **汇率未固化**，
	// 不推算（历史订单 / 外部导入 / 没配汇率的工程）。快照之后汇率怎么变都不影响这张单。
	//
	// 用 *string 承载 numeric(18,8) 的**精确文本**：读成 float64 会丢精度（见 currency_rate_model.go）。
	FXRate     *string `gorm:"column:fx_rate"`
	FXBaseCode string  `gorm:"column:fx_base_code"`

	CarrierID   *string    `gorm:"column:carrier_id"`
	CarrierName string     `gorm:"column:carrier_name"`
	TrackingNo  string     `gorm:"column:tracking_no"`
	ShippedAt   *time.Time `gorm:"column:shipped_at"`

	// FulfillmentStatus 履约状态（迁移 652）：独立于 Status 的第二条事实轴，取值见 Fulfillment* 常量。
	//
	// 它**不由 Status 推导**（FulfillmentOfStatus 只用于首次落库与历史回填）：
	// 发货时用例知道「是否发完」，退货时用例知道「货是否回仓」，这些事实 status 表达不了。
	FulfillmentStatus string `gorm:"column:fulfillment_status"`
}

// TableName 实现 gorm 表名。
//
// 必须显式给：默认的复数推断会把 OrderEntity 推成 order_entities，与迁移建的表名不一致，
// 报错是 relation "order_entities" does not exist —— 看起来像迁移没跑，实际只是名字对不上。
func (OrderEntity) TableName() string { return "orders" }

// 履约状态（迁移 652，B7）：与 status 正交的第二条事实轴。
//
// status 回答「钱走到哪了」（待付款 / 已付款 / 已退款 / 已取消），
// fulfillment 回答「货走到哪了」（待发 / 部分发 / 已发 / 客户收到 / 退货 / 补发 / 异常）。
// 两者**不互相推导**：一张「已退款且货已退回」的单，status = refunded、
// fulfillment = returned —— 合成一个枚举就必然丢一半信息。
//
// 取值对齐 dy_erp 的 11 态，去掉 go_wp 没有实体支撑的两态（synced / syncing，那是
// 多平台同步作业，go_wp 没有同步实体）。
const (
	FulfillmentPending    = "pending"
	FulfillmentPaid       = "paid"
	FulfillmentShipped    = "shipped"
	FulfillmentPartial    = "partial"
	FulfillmentReceived   = "received"
	FulfillmentException  = "exception"
	FulfillmentCancelled  = "cancelled"
	FulfillmentReship     = "reship"
	FulfillmentReturned   = "returned"
	FulfillmentReturnDone = "return_done"
	FulfillmentRefunded   = "refunded"
	FulfillmentFailed     = "failed"
)

// 履约状态的**有序清单**在 enums（orderenums.FulfillmentStatuses）：控制器与模板只能
// 看到 enums，看不到 model（分层判据：inbound 不 import model）。这里只留取值常量，
// 写路径按常量引用，不按清单遍历 —— 清单是显示口径（筛选行从左到右的顺序）。

// FulfillmentOfStatus 由订单状态推履约状态（**只用于首次落库与历史回填**）。
//
// 运行期的每一次流转都由用例显式写 fulfillment_status（发货知道是否发完、退货知道
// 货是否回仓），这里的推导只回答「没有更具体事实时该给什么」。
func FulfillmentOfStatus(status string) string {
	switch status {
	case OrderStatusPending:
		return FulfillmentPending
	case OrderStatusPaid:
		return FulfillmentPaid
	case OrderStatusShipped:
		return FulfillmentShipped
	case OrderStatusCompleted:
		return FulfillmentReceived
	case OrderStatusCancelled:
		return FulfillmentCancelled
	case OrderStatusRefunded:
		return FulfillmentRefunded
	default:
		return FulfillmentPending
	}
}

// IsManualFulfillment 「只能由人设定」的履约状态（异常 / 补发）。
//
// 这两件事系统没有可靠的自动判据：异常是「运营发现这批货出问题了」，
// 补发是「同意给客户再发一次」。自动推导它们只会在没人操作时凭空造出状态。
func IsManualFulfillment(v string) bool {
	return v == FulfillmentException || v == FulfillmentReship ||
		v == FulfillmentReturned || v == FulfillmentReturnDone
}

// OrderFilter 订单列表查询条件（条件一律以参数传入，方法内不写死业务条件）。
type OrderFilter struct {
	ProjectID     string
	Status        string
	UserID        *uint64
	Keyword       string // 订单号 / 客户邮箱 / 客户姓名
	PaymentMethod string
	// CreatedVia / FulfillmentStatus / OrderIDs 是 B7 的筛选维度（有对应事实的维度）。
	//
	// 团队 / 站点 / 销售 / 同步站点这些 dy_erp 的维度在 go_wp 没有实体（单团队多工程模型，
	// 见报告），不在这里造：筛选条上只出现在数据库里真的有列的那几个。
	CreatedVia        string
	FulfillmentStatus string
	CreatedFrom       *time.Time
	CreatedTo         *time.Time
	Offset            int
	Limit             int
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
	fillFulfillment(e)
	return rls.InProjectScope(ctx, m.db, e.ProjectID, func(tx *gorm.DB) error {
		return tx.Model(&OrderEntity{}).Create(e).Error
	})
}

// fillFulfillment 落库前补齐履约状态（空值 → 由订单状态推导）。
//
// 为什么必须在这里补、而不是只靠迁移里的 DEFAULT 'pending'：GORM 插入时会把结构体的
// 零值 "" **显式写进 INSERT**，DEFAULT 只在列被省略时生效 —— 结果是每一条新建的订单都撞
// ck_orders_fulfillment_status（表现是「落单失败」，而不是「状态不对」，很容易误判成迁移没跑）。
//
// 补推导值而不是恒定 pending：夹具与导入路径按真实状态造历史数据（直接给 status=paid），
// 那时履约也该是 paid —— 否则「按履约筛选」在那种数据上会集体错位到「待履约」。
func fillFulfillment(e *OrderEntity) {
	if e != nil && strings.TrimSpace(e.FulfillmentStatus) == "" {
		e.FulfillmentStatus = FulfillmentOfStatus(e.Status)
	}
}

// CreateTx 事务内落一单。
//
// 这里用 ScopeTx 而**不是** InProjectScope：tx 是 service 编排的事务，作用域必须设在
// 它上面。用 InProjectScope 会拿 m.db 另开事务、另取连接 —— 外层事务刚写的行在这个新
// 事务里看不见，同表写入还会自锁。设好之后该事务里**后续所有语句**（明细、状态日志、
// 库存动作）都已在同一工程作用域内，等于顺带把整条下单链路覆盖了。
func (m *OrderModel) CreateTx(ctx context.Context, tx *gorm.DB, e *OrderEntity) (err error) {
	fillFulfillment(e)
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
	if f.CreatedVia != "" {
		q = q.Where("created_via = ?", f.CreatedVia)
	}
	if f.FulfillmentStatus != "" {
		q = q.Where("fulfillment_status = ?", f.FulfillmentStatus)
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

// PendingShipFilter 待发货页的查询条件（条件一律以参数传入）。
//
// 关键词只匹配订单号 / 客户名：待发货是仓库作业台，手里拿的是拣货单上的订单号或收件人，
// 客户邮箱不在拣货动作的输入里。
type PendingShipFilter struct {
	ProjectID string
	Keyword   string
	Offset    int
	Limit     int
}

// ListPendingShipPage 待发货列表的一页：只含 **paid 且还有未发数量** 的订单。
//
// 「还有未发」在 SQL 里判，不在 Go 里过滤：先全量取回再过滤，页码与总数就与实际
// 行数对不上（总数偏大、末页可能空白），真实数据量下不可用。判据与
// service 侧逐行口径逐字一致 —— Σ订单行数量 > Σ发货明细数量。
func (m *OrderModel) ListPendingShipPage(ctx context.Context, f PendingShipFilter) (list []*OrderEntity, total int64, err error) {
	err = rls.InProjectScope(ctx, m.db, f.ProjectID, func(tx *gorm.DB) error {
		q := tx.WithContext(ctx).Model(&OrderEntity{}).
			Where("project_id = ? AND status = ?", f.ProjectID, OrderStatusPaid)
		if kw := strings.TrimSpace(f.Keyword); kw != "" {
			like := "%" + kw + "%"
			q = q.Where("(order_no ILIKE ? OR customer_name ILIKE ?)", like, like)
		}
		q = q.Where(pendingShipRemainingSQL)
		if cerr := q.Count(&total).Error; cerr != nil {
			return cerr
		}
		limit := f.Limit
		if limit <= 0 || limit > 200 {
			limit = 20
		}
		return q.Omit("attribution").Order("create_time ASC").
			Offset(f.Offset).Limit(limit).Find(&list).Error
	})
	return list, total, err
}

// pendingShipRemainingSQL 「还有未发数量」的判据（订单行数量合计 > 已发明细合计）。
//
// 单引号常量而不是两处各写一遍：待发货页与发货用例的「已发」必须是同一本账，
// 各写一份迟早会分叉（分叉的表现是「列表上还有剩余、点发货却说没有」）。
const pendingShipRemainingSQL = "(SELECT COALESCE(SUM(oi.quantity), 0) FROM order_items AS oi WHERE oi.order_id = orders.id)" +
	" > (SELECT COALESCE(SUM(si.quantity), 0) FROM order_shipment_items AS si" +
	" JOIN order_shipments AS sh ON sh.id = si.shipment_id WHERE sh.order_id = orders.id)"

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

// CountByFulfillment 按履约状态分组计数（订单列表履约徽章行的角标）。
//
// 与 CountByStatus 并列而不是带一个维度参数：两者的分组键不同、用途也不同
// （一个回答「钱各有多少单」、一个回答「货各有多少单」），合成一个带参数的
// 方法会把两种口径藏进一个字符串开关里。
func (m *OrderModel) CountByFulfillment(ctx context.Context, projectID string) (counts map[string]int64, err error) {
	var rows []struct {
		FulfillmentStatus string `gorm:"column:fulfillment_status"`
		N                 int64  `gorm:"column:n"`
	}
	if err = rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {
		return tx.Model(&OrderEntity{}).Select("fulfillment_status, COUNT(*) AS n").
			Where("project_id = ?", projectID).Group("fulfillment_status").Scan(&rows).Error
	}); err != nil {
		return nil, err
	}
	counts = make(map[string]int64, len(rows))
	for _, r := range rows {
		counts[r.FulfillmentStatus] = r.N
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

// selectExpr 绑定「列表达式 + 参数」，并先要求两边个数严格相等。
//
// 为什么不直接写 Select(expr, args...)：GORM 用 `strings.Count(v, "?") >= len(args)`
// 决定分派 —— 表达式里 `?` 少于实参时它**不报错**，而是把实参当**追加列名**拼到
// SELECT 列表末尾（`... AS amount,received,completed,paid`），于是一个参数都没绑上、
// SQL 直接语法错。而错误现场离这里很远（PG 的一句 syntax error，看不出是谁拼的）。
//
// 参数对不上属于**编程错误**（不是数据问题），所以这里直接 panic：写对了永远不触发，
// 写错了在第一次跑到就炸，而不是等到某个字段恰好为空时才露出。
func selectExpr(tx *gorm.DB, expr string, args ...any) *gorm.DB {
	if n := strings.Count(expr, "?"); n != len(args) {
		panic(fmt.Sprintf("order/model: 列表达式含 %d 个 ? 但有 %d 个实参（必须相等）：%s", n, len(args), expr))
	}
	return tx.Select(expr, args...)
}

// customerOrderSummarySelect 客户订单摘要的外层取数列（窗口函数，见 SummaryByUser 的说明）。
//
// total_amount 拼的是**订单净消费额表达式** orderNetTotalSQLExpr（定义在 order_spent_model.go）——
// 与会员候选聚合 SpentTotalsByProject **同一个常量**，而不是同一套口径抄两遍：
// 会员按净额分档、客户页按总额显示是两个数字静默分叉，而两边都不报错。
//
// 拼串只发生在**列表达式**上（净额表达式与两个 `FILTER (WHERE ...)` 名单）；
// 内层的 FROM / WHERE / ORDER BY / LIMIT 与参数绑定全部归 GORM（见 SummaryByUser 的构建）。
//
// 参数顺序（按 ? 在文本里出现的顺序）：paid_order_count 名单、
// orderNetTotalSQLExpr 里的退货状态名单、total_amount 的 FILTER 名单。
const customerOrderSummarySelect = "COALESCE(w.order_count, 0) AS order_count, " +
	"COALESCE(w.paid_order_count, 0) AS paid_order_count, " +
	"COALESCE(w.total_amount, 0) AS total_amount, " +
	"w.id AS last_order_id, w.order_no AS last_order_no, " +
	"w.status AS last_order_status, w.create_time AS last_order_time"

// customerOrderSummaryInnerSelect 内层窗口聚合的列表达式（同上，只有列是拼的）。
const customerOrderSummaryInnerSelect = "o.id, o.order_no, o.status, o.create_time, " +
	"COUNT(*) OVER () AS order_count, " +
	"COUNT(*) FILTER (WHERE o.status = ANY(string_to_array(?, ',')::text[])) OVER () AS paid_order_count, " +
	"COALESCE(SUM(" + orderNetTotalSQLExpr + ") FILTER (WHERE o.status = ANY(string_to_array(?, ',')::text[])) OVER (), 0) AS total_amount"

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
		// 内层：只取「最近一单」那一行，外加三个 OVER () 窗口聚合（整个过滤结果集的
		// 事实，与被取回的是哪一行无关）。
		inner := tx.Table(OrderEntity{}.TableName()+" AS o").
			Select(customerOrderSummaryInnerSelect,
				// 三个实参对应 customerOrderSummaryInnerSelect 里 ? 的出现顺序。
				strings.Join(paidStatuses, ","),
				strings.Join(ReturnedStatuses, ","),
				strings.Join(paidStatuses, ",")).
			Where("o.project_id = ?", projectID).
			Where("o.user_id = ?", userID).
			Order("o.id DESC").
			Limit(1)

		// 外层 LEFT JOIN 到单行哨兵 (SELECT 1)：零订单的客户在窗口聚合下不产生任何行，
		// 没有哨兵就分不清「一单没下」与「查不到这个客户」—— 而页面正是靠
		// LastOrderID 是否为 nil 分支的。
		return tx.Table("(SELECT 1) AS anchor").
			Select(customerOrderSummarySelect).
			Joins("LEFT JOIN (?) AS w ON TRUE", inner).
			Scan(&row).Error
	})
	return row, err
}

// ListByStatus 某工程下指定状态的订单（按创建时间升序）。
//
// 「待发货」页用它：只要 paid 的单（partially shipped 的剩余部分也停在 paid）。
// 排序固定升序：先下的单先发，是仓库作业的常识口径。
func (m *OrderModel) ListByStatus(ctx context.Context, projectID, status string) (list []*OrderEntity, err error) {
	err = rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {
		return tx.WithContext(ctx).Model(&OrderEntity{}).
			Where("project_id = ? AND status = ?", projectID, status).
			Order("create_time ASC").Find(&list).Error
	})
	return list, err
}
