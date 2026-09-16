// inventory_purchase_model.go — 采购单 / 采购行 / 入库单 / 入库单行（issue #18）。
//
// 与 inventory_model.go / inventory_change_model.go 同属一个「表访问单元（Repository）」：
// 这里只做本模块四张新表的 CRUD、聚合内原子组合与只读投影。业务规则（状态推导、
// 超收拒绝、幂等命中、成本价回写时机）一律留在 service 层。
//
// 三条落在本文件的实现要点：
//
//	· LockPurchaseOrderTx —— 登记入库的**第一件事**是锁住采购单行（FOR UPDATE）。
//	  同一张采购单的并发收货因此被串行化：已入库数量的原子递增、单号序号、
//	  状态重算都在同一把锁之下发生。
//	· IncrPurchaseLineReceivedTx —— 「已入库数量可原子递增」的落点：一条
//	  UPDATE … SET received_quantity = received_quantity + ? WHERE id = ? AND
//	  received_quantity + ? <= quantity。守卫写进 WHERE（不是先读后写），
//	  返回受影响行数 0 即「超收」，由 service 转成业务错误并回滚整批。
//	· HistoryRows —— 「某 SKU 的进货历史」的只读投影：入库单行 JOIN 入库单头 +
//	  货源 + 仓库 + 采购单（全部本模块表），单价取的是入库当时的快照。
package inventorymodel

import (
	"context"
	"encoding/json"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"go_wp/pkg/rls"
)

// PurchaseOrderEntity 采购单头（单号 / 货源 / 收货仓 / 推导状态）。
type PurchaseOrderEntity struct {
	ID          string          `gorm:"column:id;type:uuid;primaryKey"`
	ProjectID   string          `gorm:"column:project_id;type:uuid;not null"`
	Code        string          `gorm:"column:code;type:text;not null"`
	SourceID    string          `gorm:"column:source_id;type:uuid;not null"`
	WarehouseID string          `gorm:"column:warehouse_id;type:uuid;not null"`
	Status      string          `gorm:"column:status;type:text;not null"`
	OrderedAt   time.Time       `gorm:"column:ordered_at;not null"`
	ExpectedAt  *time.Time      `gorm:"column:expected_at"`
	Remark      string          `gorm:"column:remark;type:text;not null"`
	OperatorID  string          `gorm:"column:operator_id;type:text;not null"`
	Metadata    json.RawMessage `gorm:"column:metadata;type:jsonb;not null"`
	CreatedAt   time.Time       `gorm:"column:create_time;not null"`
	UpdatedAt   time.Time       `gorm:"column:update_time;not null"`
}

// TableName 实现 gorm 表名。
func (PurchaseOrderEntity) TableName() string { return "inventory_purchase_orders" }

// PurchaseLineEntity 采购行（SKU × 采购数量 × 采购单价 × 已入库数量）。
type PurchaseLineEntity struct {
	ID               string          `gorm:"column:id;type:uuid;primaryKey"`
	OrderID          string          `gorm:"column:order_id;type:uuid;not null"`
	ProjectID        string          `gorm:"column:project_id;type:uuid;not null"`
	ProductID        string          `gorm:"column:product_id;type:uuid;not null"`
	VariantID        string          `gorm:"column:variant_id;type:uuid;not null"`
	SKUCode          string          `gorm:"column:sku_code;type:text;not null"`
	Quantity         int             `gorm:"column:quantity;not null"`
	ReceivedQuantity int             `gorm:"column:received_quantity;not null"`
	UnitPrice        float64         `gorm:"column:unit_price;type:numeric(12,2);not null"`
	Sort             int             `gorm:"column:sort;not null"`
	Remark           string          `gorm:"column:remark;type:text;not null"`
	Metadata         json.RawMessage `gorm:"column:metadata;type:jsonb;not null"`
	CreatedAt        time.Time       `gorm:"column:create_time;not null"`
	UpdatedAt        time.Time       `gorm:"column:update_time;not null"`
}

// TableName 实现 gorm 表名。
func (PurchaseLineEntity) TableName() string { return "inventory_purchase_order_lines" }

// ReceiptEntity 入库单头（采购收货 / 自家工厂生产入库）。
type ReceiptEntity struct {
	ID              string          `gorm:"column:id;type:uuid;primaryKey"`
	ProjectID       string          `gorm:"column:project_id;type:uuid;not null"`
	Code            string          `gorm:"column:code;type:text;not null"`
	Kind            string          `gorm:"column:kind;type:text;not null"`
	OrderID         *string         `gorm:"column:order_id;type:uuid"`
	SourceID        string          `gorm:"column:source_id;type:uuid;not null"`
	WarehouseID     string          `gorm:"column:warehouse_id;type:uuid;not null"`
	RequestID       string          `gorm:"column:request_id;type:text;not null"`
	Status          string          `gorm:"column:status;type:text;not null"`
	MovementBatchID string          `gorm:"column:movement_batch_id;type:text;not null"`
	Remark          string          `gorm:"column:remark;type:text;not null"`
	OperatorID      string          `gorm:"column:operator_id;type:text;not null"`
	ReceivedAt      time.Time       `gorm:"column:received_at;not null"`
	Metadata        json.RawMessage `gorm:"column:metadata;type:jsonb;not null"`
	CreatedAt       time.Time       `gorm:"column:create_time;not null"`
}

// TableName 实现 gorm 表名。
func (ReceiptEntity) TableName() string { return "inventory_purchase_receipts" }

// ReceiptItemEntity 入库单行（数量 + 单价快照 + 成本价回写结果）。
type ReceiptItemEntity struct {
	ID          string    `gorm:"column:id;type:uuid;primaryKey"`
	ReceiptID   string    `gorm:"column:receipt_id;type:uuid;not null"`
	ProjectID   string    `gorm:"column:project_id;type:uuid;not null"`
	LineID      *string   `gorm:"column:line_id;type:uuid"`
	ProductID   string    `gorm:"column:product_id;type:uuid;not null"`
	VariantID   string    `gorm:"column:variant_id;type:uuid;not null"`
	SKUCode     string    `gorm:"column:sku_code;type:text;not null"`
	Quantity    int       `gorm:"column:quantity;not null"`
	UnitPrice   float64   `gorm:"column:unit_price;type:numeric(12,2);not null"`
	CostUpdated bool      `gorm:"column:cost_updated;not null"`
	CostError   string    `gorm:"column:cost_error;type:text;not null"`
	CreatedAt   time.Time `gorm:"column:create_time;not null"`
}

// TableName 实现 gorm 表名。
func (ReceiptItemEntity) TableName() string { return "inventory_purchase_receipt_items" }

// PurchaseOrderFilter 采购单查询条件（条件以参数传入，方法内不写死业务判断）。
type PurchaseOrderFilter struct {
	ProjectID string
	Status    string
	SourceID  string
	Keyword   string
}

// PurchaseOrderRow 采购单 + 货源 / 仓库展示信息 + 行数量汇总（本模块表只读投影）。
type PurchaseOrderRow struct {
	ID               string     `gorm:"column:id"`
	ProjectID        string     `gorm:"column:project_id"`
	Code             string     `gorm:"column:code"`
	SourceID         string     `gorm:"column:source_id"`
	SourceName       string     `gorm:"column:source_name"`
	SourceType       string     `gorm:"column:source_type"`
	WarehouseID      string     `gorm:"column:warehouse_id"`
	WarehouseName    string     `gorm:"column:warehouse_name"`
	Status           string     `gorm:"column:status"`
	OrderedAt        time.Time  `gorm:"column:ordered_at"`
	ExpectedAt       *time.Time `gorm:"column:expected_at"`
	Remark           string     `gorm:"column:remark"`
	OperatorID       string     `gorm:"column:operator_id"`
	TotalQuantity    int        `gorm:"column:total_quantity"`
	ReceivedQuantity int        `gorm:"column:received_quantity"`
	CreatedAt        time.Time  `gorm:"column:create_time"`
	UpdatedAt        time.Time  `gorm:"column:update_time"`
}

// HistoryFilter 进货历史查询条件（维度是 SKU，可按货源 / 采购单收窄）。
type HistoryFilter struct {
	ProjectID string
	SKUCode   string
	VariantID string
	SourceID  string
	OrderID   string
}

// PurchaseHistoryRow 一条进货历史（入库单行 + 入库单头 + 货源 + 仓库 + 采购单）。
type PurchaseHistoryRow struct {
	ReceiptID       string    `gorm:"column:receipt_id"`
	ReceiptCode     string    `gorm:"column:receipt_code"`
	Kind            string    `gorm:"column:kind"`
	OrderID         *string   `gorm:"column:order_id"`
	OrderCode       string    `gorm:"column:order_code"`
	SourceID        string    `gorm:"column:source_id"`
	SourceName      string    `gorm:"column:source_name"`
	SourceType      string    `gorm:"column:source_type"`
	WarehouseID     string    `gorm:"column:warehouse_id"`
	WarehouseName   string    `gorm:"column:warehouse_name"`
	VariantID       string    `gorm:"column:variant_id"`
	SKUCode         string    `gorm:"column:sku_code"`
	Quantity        int       `gorm:"column:quantity"`
	UnitPrice       float64   `gorm:"column:unit_price"`
	CostUpdated     bool      `gorm:"column:cost_updated"`
	MovementBatchID string    `gorm:"column:movement_batch_id"`
	Remark          string    `gorm:"column:remark"`
	OperatorID      string    `gorm:"column:operator_id"`
	ReceivedAt      time.Time `gorm:"column:received_at"`
}

// —— 采购单 ——

// purchaseOrderDB 采购单表句柄（内部实现细节，只允许被本 model 的仓储方法消费）。
func (m *Model) purchaseOrderDB(ctx context.Context) *gorm.DB {
	return m.db.WithContext(ctx).Model(&PurchaseOrderEntity{})
}

// CreatePurchaseOrderTx 在同一事务内写采购单头与其采购行（聚合内原子组合）。
func (m *Model) CreatePurchaseOrderTx(ctx context.Context, tx *gorm.DB, e *PurchaseOrderEntity, lines []*PurchaseLineEntity) (err error) {
	// scope 设在调用方事务上：采购单头与行表都有策略，另开事务不仅看不到外层未提交数据，
	// 还会与外层同表写入自锁。
	if serr := rls.ScopeTx(tx, e.ProjectID); serr != nil {
		return serr
	}
	if err = tx.WithContext(ctx).Create(e).Error; err != nil {
		return err
	}
	if len(lines) == 0 {
		return nil
	}
	return tx.WithContext(ctx).CreateInBatches(&lines, 100).Error
}

// GetPurchaseOrder 按 ID 查采购单头。
func (m *Model) GetPurchaseOrder(ctx context.Context, id string) (e *PurchaseOrderEntity, err error) {
	e = &PurchaseOrderEntity{}
	err = m.purchaseOrderDB(ctx).Where("id = ?", id).First(e).Error
	return e, err
}

// LockPurchaseOrderTx 在给定事务内对采购单头取 FOR UPDATE 行锁。
//
// 登记入库的第一步：同一张单的并发收货因此串行化（原子递增、单号序号、
// 状态重算全部在这把锁之下）。
func (m *Model) LockPurchaseOrderTx(ctx context.Context, tx *gorm.DB, id string) (e *PurchaseOrderEntity, err error) {
	e = &PurchaseOrderEntity{}
	err = tx.WithContext(ctx).Model(&PurchaseOrderEntity{}).
		Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("id = ?", id).First(e).Error
	return e, err
}

// PurchaseCodeExists 某工程内采购单号是否被占用（大小写不敏感，excludeID 为空表示新建场景）。
func (m *Model) PurchaseCodeExists(ctx context.Context, projectID, code, excludeID string) (exists bool, err error) {
	q := m.purchaseOrderDB(ctx).Where("project_id = ? AND upper(code) = upper(?)", projectID, code)
	if excludeID != "" {
		q = q.Where("id <> ?", excludeID)
	}
	var n int64
	if err = q.Count(&n).Error; err != nil {
		return false, err
	}
	return n > 0, nil
}

// UpdatePurchaseOrderTx 在给定事务内写回采购单头（全字段保存）。
func (m *Model) UpdatePurchaseOrderTx(ctx context.Context, tx *gorm.DB, e *PurchaseOrderEntity) (err error) {
	if serr := rls.ScopeTx(tx, e.ProjectID); serr != nil {
		return serr
	}
	return tx.WithContext(ctx).Model(&PurchaseOrderEntity{}).Where("id = ?", e.ID).Save(e).Error
}

// UpdatePurchaseOrderStatusTx 在给定事务内写回推导出的状态（只动状态与时间戳两列）。
func (m *Model) UpdatePurchaseOrderStatusTx(ctx context.Context, tx *gorm.DB, id, status string, at time.Time) (err error) {
	return tx.WithContext(ctx).Model(&PurchaseOrderEntity{}).Where("id = ?", id).
		Updates(map[string]any{"status": status, "update_time": at}).Error
}

// applyPurchaseOrderFilter 把查询条件施加到采购单查询上（条件以参数传入）。
func applyPurchaseOrderFilter(q *gorm.DB, f PurchaseOrderFilter) *gorm.DB {
	if f.ProjectID != "" {
		q = q.Where("o.project_id = ?", f.ProjectID)
	}
	if f.Status != "" {
		q = q.Where("o.status = ?", f.Status)
	}
	if f.SourceID != "" {
		q = q.Where("o.source_id = ?", f.SourceID)
	}
	if f.Keyword != "" {
		like := "%" + f.Keyword + "%"
		q = q.Where("o.code ILIKE ? OR o.remark ILIKE ?", like, like)
	}
	return q
}

// purchaseOrderRows 采购单 + 货源 / 仓库 + 行数量汇总的只读投影（本模块表 join 的唯一定义处）。
func (m *Model) purchaseOrderRows(ctx context.Context) *gorm.DB {
	return m.db.WithContext(ctx).Table("inventory_purchase_orders AS o").
		Select("o.id, o.project_id, o.code, o.source_id, o.warehouse_id, o.status, " +
			"o.ordered_at, o.expected_at, o.remark, o.operator_id, o.create_time, o.update_time, " +
			"s.name AS source_name, s.type AS source_type, w.name AS warehouse_name, " +
			"COALESCE(agg.total_quantity, 0) AS total_quantity, " +
			"COALESCE(agg.received_quantity, 0) AS received_quantity").
		Joins("JOIN inventory_sources AS s ON s.id = o.source_id").
		Joins("JOIN inventory_warehouses AS w ON w.id = o.warehouse_id").
		Joins("LEFT JOIN (SELECT order_id, SUM(quantity) AS total_quantity, " +
			"SUM(received_quantity) AS received_quantity FROM inventory_purchase_order_lines GROUP BY order_id) " +
			"AS agg ON agg.order_id = o.id")
}

// ListPurchaseOrderRows 采购单列表（按条件过滤 + 分页；limit <= 0 表示不限条数）。
//
// 排序固定「下单时间倒序 → 单号」：后台每次以同样顺序翻看，核对与对比才有基准。
func (m *Model) ListPurchaseOrderRows(ctx context.Context, f PurchaseOrderFilter, limit, offset int) (list []*PurchaseOrderRow, err error) {
	q := applyPurchaseOrderFilter(m.purchaseOrderRows(ctx), f).Order("o.ordered_at DESC, o.code ASC")
	if limit > 0 {
		q = q.Limit(limit).Offset(offset)
	}
	err = q.Scan(&list).Error
	return list, err
}

// CountPurchaseOrders 采购单计数（同条件，供分页用）。
func (m *Model) CountPurchaseOrders(ctx context.Context, f PurchaseOrderFilter) (n int64, err error) {
	q := m.db.WithContext(ctx).Table("inventory_purchase_orders AS o")
	err = applyPurchaseOrderFilter(q, f).Count(&n).Error
	return n, err
}

// —— 采购行 ——

// purchaseLineDB 采购行表句柄（同上，仅本 model 内部使用）。
func (m *Model) purchaseLineDB(ctx context.Context) *gorm.DB {
	return m.db.WithContext(ctx).Model(&PurchaseLineEntity{})
}

// ListPurchaseLines 某采购单的全部采购行（按排序号 → 创建时间：显示顺序确定）。
func (m *Model) ListPurchaseLines(ctx context.Context, orderID string) (list []*PurchaseLineEntity, err error) {
	err = m.purchaseLineDB(ctx).Where("order_id = ?", orderID).
		Order("sort ASC, create_time ASC").Find(&list).Error
	return list, err
}

// ListPurchaseLinesTx 事务内取某采购单的行（登记入库时在锁内读，读到的是最新已入库数量）。
func (m *Model) ListPurchaseLinesTx(ctx context.Context, tx *gorm.DB, orderID string) (list []*PurchaseLineEntity, err error) {
	err = tx.WithContext(ctx).Model(&PurchaseLineEntity{}).Where("order_id = ?", orderID).
		Order("sort ASC, create_time ASC").Find(&list).Error
	return list, err
}

// ListPurchaseLinesByOrders 批量取多个采购单的行（列表页不产生 N+1）。
func (m *Model) ListPurchaseLinesByOrders(ctx context.Context, orderIDs []string) (list []*PurchaseLineEntity, err error) {
	if len(orderIDs) == 0 {
		return nil, nil
	}
	err = m.purchaseLineDB(ctx).Where("order_id IN ?", orderIDs).
		Order("order_id ASC, sort ASC, create_time ASC").Find(&list).Error
	return list, err
}

// GetPurchaseLineTx 事务内按 ID 取采购行（登记入库时先锁单头再取行）。
func (m *Model) GetPurchaseLineTx(ctx context.Context, tx *gorm.DB, lineID string) (e *PurchaseLineEntity, err error) {
	e = &PurchaseLineEntity{}
	err = tx.WithContext(ctx).Model(&PurchaseLineEntity{}).Where("id = ?", lineID).First(e).Error
	return e, err
}

// ReplacePurchaseLinesTx 在同一事务内全量替换某采购单的行（先删后写，聚合内原子组合）。
func (m *Model) ReplacePurchaseLinesTx(ctx context.Context, tx *gorm.DB, orderID string, lines []*PurchaseLineEntity) (err error) {
	// 行表有策略。lines 为空时只剩 DELETE，此时签名里没有工程 id，只能依赖调用方事务
	// 已经设过作用域（同一 service 链路通常已由 CreatePurchaseOrderTx 设过）。
	if len(lines) > 0 {
		if serr := rls.ScopeTx(tx, lines[0].ProjectID); serr != nil {
			return serr
		}
	}
	if err = tx.WithContext(ctx).Model(&PurchaseLineEntity{}).
		Where("order_id = ?", orderID).Delete(&PurchaseLineEntity{}).Error; err != nil {
		return err
	}
	if len(lines) == 0 {
		return nil
	}
	return tx.WithContext(ctx).CreateInBatches(&lines, 100).Error
}

// IncrPurchaseLineReceivedTx 原子递增已入库数量（验收 1），返回受影响行数。
//
// 守卫写在 WHERE 里（received_quantity + delta <= quantity），不是「先读后写」：
// 并发收货时数据库自己串行化，受影响行数为 0 即超收 —— 调用方据此拒绝整批。
func (m *Model) IncrPurchaseLineReceivedTx(ctx context.Context, tx *gorm.DB, lineID string, delta int, at time.Time) (rows int64, err error) {
	res := tx.WithContext(ctx).Model(&PurchaseLineEntity{}).
		Where("id = ? AND received_quantity + ? <= quantity", lineID, delta).
		Updates(map[string]any{
			"received_quantity": gorm.Expr("received_quantity + ?", delta),
			"update_time":       at,
		})
	return res.RowsAffected, res.Error
}

// DecrPurchaseLineReceivedTx 回退已入库数量（库存变动失败时的补偿），返回受影响行数。
func (m *Model) DecrPurchaseLineReceivedTx(ctx context.Context, tx *gorm.DB, lineID string, delta int, at time.Time) (rows int64, err error) {
	res := tx.WithContext(ctx).Model(&PurchaseLineEntity{}).
		Where("id = ? AND received_quantity >= ?", lineID, delta).
		Updates(map[string]any{
			"received_quantity": gorm.Expr("received_quantity - ?", delta),
			"update_time":       at,
		})
	return res.RowsAffected, res.Error
}

// —— 入库单 ——

// receiptDB 入库单表句柄（同上，仅本 model 内部使用）。
func (m *Model) receiptDB(ctx context.Context) *gorm.DB {
	return m.db.WithContext(ctx).Model(&ReceiptEntity{})
}

// CountOrderReceiptsTx 事务内数某采购单已开的入库单数（单号序号的依据；调用前已锁单头）。
func (m *Model) CountOrderReceiptsTx(ctx context.Context, tx *gorm.DB, orderID string) (n int64, err error) {
	err = tx.WithContext(ctx).Model(&ReceiptEntity{}).Where("order_id = ?", orderID).Count(&n).Error
	return n, err
}

// CreateReceiptTx 在同一事务内写入库单头与全部入库行（聚合内原子组合）。
func (m *Model) CreateReceiptTx(ctx context.Context, tx *gorm.DB, e *ReceiptEntity, items []*ReceiptItemEntity) (err error) {
	// 入库单头与入库行都有策略，scope 设在调用方事务上。
	if serr := rls.ScopeTx(tx, e.ProjectID); serr != nil {
		return serr
	}
	if err = tx.WithContext(ctx).Create(e).Error; err != nil {
		return err
	}
	if len(items) == 0 {
		return nil
	}
	return tx.WithContext(ctx).CreateInBatches(&items, 100).Error
}

// SetReceiptMovement 写回入库单的库存批次号与单据状态。
//
// 这是**库存变动提交之后**的独立步骤（跨模块写不进同一事务）：
// 单据先以 pending 落库，变动成功后才置 posted 并记下批次号。
func (m *Model) SetReceiptMovement(ctx context.Context, receiptID, batchID, status string) (err error) {
	return m.receiptDB(ctx).Where("id = ?", receiptID).
		Updates(map[string]any{"movement_batch_id": batchID, "status": status}).Error
}

// UpdateReceiptItemCost 记下某入库行的成本价回写结果（提交后的独立步骤）。
func (m *Model) UpdateReceiptItemCost(ctx context.Context, itemID string, updated bool, errText string) (err error) {
	return m.db.WithContext(ctx).Model(&ReceiptItemEntity{}).Where("id = ?", itemID).
		Updates(map[string]any{"cost_updated": updated, "cost_error": errText}).Error
}

// DeleteReceiptTx 在给定事务内删除入库单头（行由外键 ON DELETE CASCADE 一并删除）。
func (m *Model) DeleteReceiptTx(ctx context.Context, tx *gorm.DB, receiptID string) (err error) {
	return tx.WithContext(ctx).Model(&ReceiptEntity{}).Where("id = ?", receiptID).Delete(&ReceiptEntity{}).Error
}

// GetReceipt 按 ID 查入库单头。
func (m *Model) GetReceipt(ctx context.Context, id string) (e *ReceiptEntity, err error) {
	e = &ReceiptEntity{}
	err = m.receiptDB(ctx).Where("id = ?", id).First(e).Error
	return e, err
}

// FindReceiptByRequestID 按幂等键取入库单（命中表示这是一次重放，工程内至多一张）。
func (m *Model) FindReceiptByRequestID(ctx context.Context, projectID, requestID string) (e *ReceiptEntity, err error) {
	e = &ReceiptEntity{}
	err = m.receiptDB(ctx).Where("project_id = ? AND request_id = ?", projectID, requestID).First(e).Error
	return e, err
}

// ListReceiptItems 某入库单的全部入库行（按创建时间：顺序确定）。
func (m *Model) ListReceiptItems(ctx context.Context, receiptID string) (list []*ReceiptItemEntity, err error) {
	err = m.db.WithContext(ctx).Model(&ReceiptItemEntity{}).Where("receipt_id = ?", receiptID).
		Order("create_time ASC, id ASC").Find(&list).Error
	return list, err
}

// —— 进货历史（验收 6）——

// historyRows 进货历史的只读投影：入库单行 + 入库单头 + 货源 + 仓库 + 采购单。
func (m *Model) historyRows(ctx context.Context) *gorm.DB {
	return m.db.WithContext(ctx).Table("inventory_purchase_receipt_items AS i").
		Select("i.receipt_id, r.code AS receipt_code, r.kind, r.order_id, " +
			"COALESCE(o.code, '') AS order_code, r.source_id, s.name AS source_name, s.type AS source_type, " +
			"r.warehouse_id, w.name AS warehouse_name, i.variant_id, i.sku_code, " +
			"i.quantity, i.unit_price, i.cost_updated, r.movement_batch_id, r.remark, r.operator_id, r.received_at").
		Joins("JOIN inventory_purchase_receipts AS r ON r.id = i.receipt_id").
		Joins("JOIN inventory_sources AS s ON s.id = r.source_id").
		Joins("JOIN inventory_warehouses AS w ON w.id = r.warehouse_id").
		Joins("LEFT JOIN inventory_purchase_orders AS o ON o.id = r.order_id")
}

// applyHistoryFilter 把查询条件施加到进货历史上。
func applyHistoryFilter(q *gorm.DB, f HistoryFilter) *gorm.DB {
	if f.ProjectID != "" {
		q = q.Where("i.project_id = ?", f.ProjectID)
	}
	if f.SKUCode != "" {
		q = q.Where("i.sku_code = ?", f.SKUCode)
	}
	if f.VariantID != "" {
		q = q.Where("i.variant_id = ?", f.VariantID)
	}
	if f.SourceID != "" {
		q = q.Where("r.source_id = ?", f.SourceID)
	}
	if f.OrderID != "" {
		q = q.Where("r.order_id = ?", f.OrderID)
	}
	return q
}

// ListHistoryRows 进货历史列表（按 SKU / 货源 / 采购单过滤 + 分页）。
func (m *Model) ListHistoryRows(ctx context.Context, f HistoryFilter, limit, offset int) (list []*PurchaseHistoryRow, err error) {
	q := applyHistoryFilter(m.historyRows(ctx), f).Order("r.received_at DESC, i.create_time DESC, i.id DESC")
	if limit > 0 {
		q = q.Limit(limit).Offset(offset)
	}
	err = q.Scan(&list).Error
	return list, err
}

// CountHistoryRows 进货历史计数（同过滤条件，供分页用）。
func (m *Model) CountHistoryRows(ctx context.Context, f HistoryFilter) (n int64, err error) {
	q := m.db.WithContext(ctx).Table("inventory_purchase_receipt_items AS i").
		Joins("JOIN inventory_purchase_receipts AS r ON r.id = i.receipt_id")
	err = applyHistoryFilter(q, f).Count(&n).Error
	return n, err
}
