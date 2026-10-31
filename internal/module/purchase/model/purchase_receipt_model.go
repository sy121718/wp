package purchasemodel

import (
	"context"
	"time"

	json "go_wp/pkg/jsonx"

	"gorm.io/gorm"

	"go_wp/pkg/rls"
)

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

func (ReceiptEntity) TableName() string { return "inventory_purchase_receipts" }

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

func (ReceiptItemEntity) TableName() string { return "inventory_purchase_receipt_items" }

type HistoryFilter struct {
	ProjectID string
	SKUCode   string
	VariantID string
	SourceID  string
	OrderID   string
}

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

func (m *Model) CountOrderReceiptsTx(ctx context.Context, tx *gorm.DB, orderID, projectID string) (n int64, err error) {
	if serr := rls.ScopeTx(tx, projectID); serr != nil {
		return 0, serr
	}
	err = tx.WithContext(ctx).Model(&ReceiptEntity{}).Where("order_id = ?", orderID).Count(&n).Error
	return n, err
}

func (m *Model) CreateReceiptTx(ctx context.Context, tx *gorm.DB, e *ReceiptEntity, items []*ReceiptItemEntity) (err error) {

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

func (m *Model) SetReceiptMovementTx(ctx context.Context, tx *gorm.DB, receiptID, batchID, status, projectID string) (err error) {
	if serr := rls.ScopeTx(tx, projectID); serr != nil {
		return serr
	}
	return tx.WithContext(ctx).Model(&ReceiptEntity{}).Where("id = ?", receiptID).
		Updates(map[string]any{"movement_batch_id": batchID, "status": status}).Error
}

func (m *Model) UpdateReceiptItemCost(ctx context.Context, itemID string, updated bool, errText, projectID string) (err error) {
	return rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {
		return tx.WithContext(ctx).Model(&ReceiptItemEntity{}).Where("id = ?", itemID).
			Updates(map[string]any{"cost_updated": updated, "cost_error": errText}).Error
	})
}

func (m *Model) GetReceipt(ctx context.Context, id, projectID string) (e *ReceiptEntity, err error) {
	e = &ReceiptEntity{}
	err = rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {
		return tx.WithContext(ctx).Model(&ReceiptEntity{}).Where("id = ?", id).First(e).Error
	})
	return e, err
}

func (m *Model) FindReceiptByRequestID(ctx context.Context, projectID, requestID string) (e *ReceiptEntity, err error) {
	e = &ReceiptEntity{}
	err = rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {
		return tx.WithContext(ctx).Model(&ReceiptEntity{}).
			Where("project_id = ? AND request_id = ?", projectID, requestID).First(e).Error
	})
	return e, err
}

func (m *Model) ListReceiptItems(ctx context.Context, receiptID, projectID string) (list []*ReceiptItemEntity, err error) {
	err = rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {
		return tx.WithContext(ctx).Model(&ReceiptItemEntity{}).Where("receipt_id = ?", receiptID).
			Order("create_time ASC, id ASC").Find(&list).Error
	})
	return list, err
}

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

func (m *Model) CountHistoryRows(ctx context.Context, f HistoryFilter) (n int64, err error) {
	err = rls.InProjectScope(ctx, m.db, f.ProjectID, func(tx *gorm.DB) error {
		q := m.historyRows(tx.WithContext(ctx))
		return applyHistoryFilter(q, f).Count(&n).Error
	})
	return n, err
}
