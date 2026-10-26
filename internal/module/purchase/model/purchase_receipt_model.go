// purchase_receipt_model.go — 入库单头与入库单行的表访问，以及进货历史的只读投影。
//
// 入库的写入形状是「记账 + 库存变动同一事务」（见 service.RegisterReceipt）：
// 本文件的 *Tx 变体都只做本模块表的事，库存变动由 service 经库存契约的 Tx 形态并进同一事务。
//
// 进货历史（historyRows）的 join 只在本模块表之间：入库单行 + 单头 + 货源 + 采购单。
// 仓库名与货源名一样是展示字段，但 inventory_warehouses 属库存模块 —— 该列由 service
// 经库存契约的仓名映射补齐，不在 SQL 里跨模块 join。
package purchasemodel

import (
	"context"
	"time"

	json "go_wp/pkg/jsonx"

	"gorm.io/gorm"

	"go_wp/pkg/rls"
)

// ReceiptEntity 入库单头（采购收货 / 自家工厂生产入库）。
type ReceiptEntity struct {
	ID              string          `gorm:"column:id;primaryKey"`
	ProjectID       string          `gorm:"column:project_id;not null"`
	Code            string          `gorm:"column:code;not null"`
	Kind            string          `gorm:"column:kind;not null"`
	OrderID         *string         `gorm:"column:order_id"`
	SourceID        string          `gorm:"column:source_id;not null"`
	WarehouseID     string          `gorm:"column:warehouse_id;not null"`
	RequestID       string          `gorm:"column:request_id;not null"`
	Status          string          `gorm:"column:status;not null"`
	MovementBatchID string          `gorm:"column:movement_batch_id;not null"`
	Remark          string          `gorm:"column:remark;not null"`
	OperatorID      string          `gorm:"column:operator_id;not null"`
	ReceivedAt      time.Time       `gorm:"column:received_at;not null"`
	Metadata        json.RawMessage `gorm:"column:metadata;type:jsonb;not null"`
	CreatedAt       time.Time       `gorm:"column:create_time;not null"`
}

// TableName 实现 gorm 表名。
func (ReceiptEntity) TableName() string { return "inventory_purchase_receipts" }

// ReceiptItemEntity 入库单行（数量 + 单价快照 + 成本价回写结果）。
type ReceiptItemEntity struct {
	ID          string    `gorm:"column:id;primaryKey"`
	ReceiptID   string    `gorm:"column:receipt_id;not null"`
	ProjectID   string    `gorm:"column:project_id;not null"`
	LineID      *string   `gorm:"column:line_id"`
	ProductID   string    `gorm:"column:product_id;not null"`
	VariantID   string    `gorm:"column:variant_id;not null"`
	SKUCode     string    `gorm:"column:sku_code;not null"`
	Quantity    int       `gorm:"column:quantity;not null"`
	UnitPrice   float64   `gorm:"column:unit_price;not null"`
	CostUpdated bool      `gorm:"column:cost_updated;not null"`
	CostError   string    `gorm:"column:cost_error;not null"`
	CreatedAt   time.Time `gorm:"column:create_time;not null"`
}

// TableName 实现 gorm 表名。
func (ReceiptItemEntity) TableName() string { return "inventory_purchase_receipt_items" }

// HistoryFilter 进货历史查询条件（维度是 SKU，可按货源 / 采购单收窄）。
type HistoryFilter struct {
	ProjectID string
	SKUCode   string
	VariantID string
	SourceID  string
	OrderID   string
}

// PurchaseHistoryRow 一条进货历史（入库单行 + 入库单头 + 货源 + 采购单）。
//
// WarehouseName 由 service 经库存契约补齐（见文件头）。
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
	WarehouseName   string    `gorm:"-"`
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

// CountOrderReceiptsTx 事务内数某采购单已开的入库单数（单号序号的依据；调用前已锁单头）。
func (m *Model) CountOrderReceiptsTx(ctx context.Context, tx *gorm.DB, orderID, projectID string) (n int64, err error) {
	if serr := rls.ScopeTx(tx, projectID); serr != nil {
		return 0, serr
	}
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

// SetReceiptMovementTx 在给定事务内写回入库单的库存批次号与单据状态。
//
// **必须与库存变动同事务**：单据的「已 posted + 批次号」如果落在库存变动之外，
// 就又会造出「库存动没动」与「单据什么状态」两处不一致（原先正是提交后的独立步骤，
// 而且错误被调用方的 if 吞掉 —— 库存动了、单据静默停在 pending）。
func (m *Model) SetReceiptMovementTx(ctx context.Context, tx *gorm.DB, receiptID, batchID, status, projectID string) (err error) {
	if serr := rls.ScopeTx(tx, projectID); serr != nil {
		return serr
	}
	return tx.WithContext(ctx).Model(&ReceiptEntity{}).Where("id = ?", receiptID).
		Updates(map[string]any{"movement_batch_id": batchID, "status": status}).Error
}

// UpdateReceiptItemCost 记下某入库行的成本价回写结果（提交后的独立步骤）。
func (m *Model) UpdateReceiptItemCost(ctx context.Context, itemID string, updated bool, errText, projectID string) (err error) {
	return rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {
		return tx.WithContext(ctx).Model(&ReceiptItemEntity{}).Where("id = ?", itemID).
			Updates(map[string]any{"cost_updated": updated, "cost_error": errText}).Error
	})
}

// GetReceipt 按 ID 查入库单头。
func (m *Model) GetReceipt(ctx context.Context, id, projectID string) (e *ReceiptEntity, err error) {
	e = &ReceiptEntity{}
	err = rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {
		return tx.WithContext(ctx).Model(&ReceiptEntity{}).Where("id = ?", id).First(e).Error
	})
	return e, err
}

// FindReceiptByRequestID 按幂等键取入库单（命中表示这是一次重放，工程内至多一张）。
func (m *Model) FindReceiptByRequestID(ctx context.Context, projectID, requestID string) (e *ReceiptEntity, err error) {
	e = &ReceiptEntity{}
	err = rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {
		return tx.WithContext(ctx).Model(&ReceiptEntity{}).
			Where("project_id = ? AND request_id = ?", projectID, requestID).First(e).Error
	})
	return e, err
}

// ListReceiptItems 某入库单的全部入库行（按创建时间：顺序确定）。
func (m *Model) ListReceiptItems(ctx context.Context, receiptID, projectID string) (list []*ReceiptItemEntity, err error) {
	err = rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {
		return tx.WithContext(ctx).Model(&ReceiptItemEntity{}).Where("receipt_id = ?", receiptID).
			Order("create_time ASC, id ASC").Find(&list).Error
	})
	return list, err
}

// —— 进货历史（验收 6）——

// historyRows 进货历史的只读投影：入库单行 + 入库单头 + 货源 + 采购单（全部本模块表）。
//
// 与 purchaseOrderRows 同理：接收 db 句柄，保证 join 落在已设作用域的事务上。
func (m *Model) historyRows(db *gorm.DB) *gorm.DB {
	return db.Table("inventory_purchase_receipt_items AS i").
		Select("i.receipt_id, r.code AS receipt_code, r.kind, r.order_id, " +
			"COALESCE(o.code, '') AS order_code, r.source_id, s.name AS source_name, s.type AS source_type, " +
			"r.warehouse_id, i.variant_id, i.sku_code, " +
			"i.quantity, i.unit_price, i.cost_updated, r.movement_batch_id, r.remark, r.operator_id, r.received_at").
		Joins("JOIN inventory_purchase_receipts AS r ON r.id = i.receipt_id").
		Joins("JOIN inventory_sources AS s ON s.id = r.source_id").
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
	err = rls.InProjectScope(ctx, m.db, f.ProjectID, func(tx *gorm.DB) error {
		q := applyHistoryFilter(m.historyRows(tx.WithContext(ctx)), f).
			Order("r.received_at DESC, i.create_time DESC, i.id DESC")
		if limit > 0 {
			q = q.Limit(limit).Offset(offset)
		}
		return q.Scan(&list).Error
	})
	return list, err
}

// CountHistoryRows 进货历史计数（同过滤条件，供分页用）。
func (m *Model) CountHistoryRows(ctx context.Context, f HistoryFilter) (n int64, err error) {
	err = rls.InProjectScope(ctx, m.db, f.ProjectID, func(tx *gorm.DB) error {
		q := m.historyRows(tx.WithContext(ctx))
		return applyHistoryFilter(q, f).Count(&n).Error
	})
	return n, err
}
