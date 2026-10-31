package purchasemodel

import (
	"context"
	"time"

	json "go_wp/pkg/jsonx"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"go_wp/pkg/rls"
)

type PurchaseOrderEntity struct {
	ID          string          `gorm:"column:id;primaryKey"`
	ProjectID   string          `gorm:"column:project_id;not null"`
	Code        string          `gorm:"column:code;not null"`
	SourceID    string          `gorm:"column:source_id;not null"`
	WarehouseID string          `gorm:"column:warehouse_id;not null"`
	Status      string          `gorm:"column:status;not null"`
	OrderedAt   time.Time       `gorm:"column:ordered_at;not null"`
	ExpectedAt  *time.Time      `gorm:"column:expected_at"`
	Remark      string          `gorm:"column:remark;not null"`
	OperatorID  string          `gorm:"column:operator_id;not null"`
	Metadata    json.RawMessage `gorm:"column:metadata;type:jsonb;not null"`
	CreatedAt   time.Time       `gorm:"column:create_time;not null"`
	UpdatedAt   time.Time       `gorm:"column:update_time;not null"`
}

func (PurchaseOrderEntity) TableName() string { return "inventory_purchase_orders" }

type PurchaseLineEntity struct {
	ID               string          `gorm:"column:id;primaryKey"`
	OrderID          string          `gorm:"column:order_id;not null"`
	ProjectID        string          `gorm:"column:project_id;not null"`
	ProductID        string          `gorm:"column:product_id;not null"`
	VariantID        string          `gorm:"column:variant_id;not null"`
	SKUCode          string          `gorm:"column:sku_code;not null"`
	Quantity         int             `gorm:"column:quantity;not null"`
	ReceivedQuantity int             `gorm:"column:received_quantity;not null"`
	UnitPrice        float64         `gorm:"column:unit_price;not null"`
	Sort             int             `gorm:"column:sort;not null"`
	Remark           string          `gorm:"column:remark;not null"`
	Metadata         json.RawMessage `gorm:"column:metadata;type:jsonb;not null"`
	CreatedAt        time.Time       `gorm:"column:create_time;not null"`
	UpdatedAt        time.Time       `gorm:"column:update_time;not null"`
}

func (PurchaseLineEntity) TableName() string { return "inventory_purchase_order_lines" }

type PurchaseOrderFilter struct {
	ProjectID string
	Status    string
	SourceID  string
	Keyword   string
}

type PurchaseOrderRow struct {
	ID               string     `gorm:"column:id"`
	ProjectID        string     `gorm:"column:project_id"`
	Code             string     `gorm:"column:code"`
	SourceID         string     `gorm:"column:source_id"`
	SourceName       string     `gorm:"column:source_name"`
	SourceType       string     `gorm:"column:source_type"`
	WarehouseID      string     `gorm:"column:warehouse_id"`
	WarehouseName    string     `gorm:"-"`
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

func (m *Model) CreatePurchaseOrderTx(ctx context.Context, tx *gorm.DB, e *PurchaseOrderEntity, lines []*PurchaseLineEntity) (err error) {

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

func (m *Model) GetPurchaseOrder(ctx context.Context, id, projectID string) (e *PurchaseOrderEntity, err error) {
	e = &PurchaseOrderEntity{}
	err = rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {
		return tx.WithContext(ctx).Model(&PurchaseOrderEntity{}).Where("id = ?", id).First(e).Error
	})
	return e, err
}

func (m *Model) LockPurchaseOrderTx(ctx context.Context, tx *gorm.DB, id, projectID string) (e *PurchaseOrderEntity, err error) {
	if serr := rls.ScopeTx(tx, projectID); serr != nil {
		return nil, serr
	}
	e = &PurchaseOrderEntity{}
	err = tx.WithContext(ctx).Model(&PurchaseOrderEntity{}).
		Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("id = ?", id).First(e).Error
	return e, err
}

func (m *Model) PurchaseCodeExists(ctx context.Context, projectID, code, excludeID string) (exists bool, err error) {
	err = rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {
		q := tx.WithContext(ctx).Model(&PurchaseOrderEntity{}).
			Where("project_id = ? AND upper(code) = upper(?)", projectID, code)
		if excludeID != "" {
			q = q.Where("id <> ?", excludeID)
		}
		var n int64
		if cerr := q.Count(&n).Error; cerr != nil {
			return cerr
		}
		exists = n > 0
		return nil
	})
	if err != nil {
		return false, err
	}
	return exists, nil
}

func (m *Model) UpdatePurchaseOrderTx(ctx context.Context, tx *gorm.DB, e *PurchaseOrderEntity) (err error) {
	if serr := rls.ScopeTx(tx, e.ProjectID); serr != nil {
		return serr
	}
	return tx.WithContext(ctx).Model(&PurchaseOrderEntity{}).Where("id = ?", e.ID).Save(e).Error
}

func (m *Model) UpdatePurchaseOrderStatusTx(ctx context.Context, tx *gorm.DB, id, status string, projectID string, at time.Time) (err error) {
	if serr := rls.ScopeTx(tx, projectID); serr != nil {
		return serr
	}
	return tx.WithContext(ctx).Model(&PurchaseOrderEntity{}).Where("id = ?", id).
		Updates(map[string]any{"status": status, "update_time": at}).Error
}

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

func (m *Model) purchaseOrderRows(db *gorm.DB) *gorm.DB {
	return db.Table("inventory_purchase_orders AS o").
		Select("o.id, o.project_id, o.code, o.source_id, o.warehouse_id, o.status, " +
			"o.ordered_at, o.expected_at, o.remark, o.operator_id, o.create_time, o.update_time, " +
			"s.name AS source_name, s.type AS source_type, " +
			"COALESCE(agg.total_quantity, 0) AS total_quantity, " +
			"COALESCE(agg.received_quantity, 0) AS received_quantity").
		Joins("JOIN inventory_sources AS s ON s.id = o.source_id").
		Joins("LEFT JOIN (SELECT order_id, SUM(quantity) AS total_quantity, " +
			"SUM(received_quantity) AS received_quantity FROM inventory_purchase_order_lines GROUP BY order_id) " +
			"AS agg ON agg.order_id = o.id")
}

func (m *Model) ListPurchaseOrderRows(ctx context.Context, f PurchaseOrderFilter, limit, offset int) (list []*PurchaseOrderRow, err error) {
	err = rls.InProjectScope(ctx, m.db, f.ProjectID, func(tx *gorm.DB) error {
		q := applyPurchaseOrderFilter(m.purchaseOrderRows(tx.WithContext(ctx)), f).
			Order("o.ordered_at DESC, o.code ASC")
		if limit > 0 {
			q = q.Limit(limit).Offset(offset)
		}
		return q.Scan(&list).Error
	})
	return list, err
}

func (m *Model) CountPurchaseOrders(ctx context.Context, f PurchaseOrderFilter) (n int64, err error) {
	err = rls.InProjectScope(ctx, m.db, f.ProjectID, func(tx *gorm.DB) error {
		q := tx.WithContext(ctx).Table("inventory_purchase_orders AS o")
		return applyPurchaseOrderFilter(q, f).Count(&n).Error
	})
	return n, err
}

func (m *Model) ListPurchaseLines(ctx context.Context, orderID, projectID string) (list []*PurchaseLineEntity, err error) {
	err = rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {
		return tx.WithContext(ctx).Model(&PurchaseLineEntity{}).Where("order_id = ?", orderID).
			Order("sort ASC, create_time ASC").Find(&list).Error
	})
	return list, err
}

func (m *Model) ListPurchaseLinesTx(ctx context.Context, tx *gorm.DB, orderID, projectID string) (list []*PurchaseLineEntity, err error) {
	if serr := rls.ScopeTx(tx, projectID); serr != nil {
		return nil, serr
	}
	err = tx.WithContext(ctx).Model(&PurchaseLineEntity{}).Where("order_id = ?", orderID).
		Order("sort ASC, create_time ASC").Find(&list).Error
	return list, err
}

func (m *Model) ListPurchaseLinesByOrders(ctx context.Context, orderIDs []string, projectID string) (list []*PurchaseLineEntity, err error) {
	if len(orderIDs) == 0 {
		return nil, nil
	}
	err = rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {
		return tx.WithContext(ctx).Model(&PurchaseLineEntity{}).Where("order_id IN ?", orderIDs).
			Order("order_id ASC, sort ASC, create_time ASC").Find(&list).Error
	})
	return list, err
}

func (m *Model) GetPurchaseLineTx(ctx context.Context, tx *gorm.DB, lineID, projectID string) (e *PurchaseLineEntity, err error) {
	if serr := rls.ScopeTx(tx, projectID); serr != nil {
		return nil, serr
	}
	e = &PurchaseLineEntity{}
	err = tx.WithContext(ctx).Model(&PurchaseLineEntity{}).Where("id = ?", lineID).First(e).Error
	return e, err
}

func (m *Model) ReplacePurchaseLinesTx(ctx context.Context, tx *gorm.DB, orderID, projectID string, lines []*PurchaseLineEntity) (err error) {
	if serr := rls.ScopeTx(tx, projectID); serr != nil {
		return serr
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

func (m *Model) IncrPurchaseLineReceivedTx(ctx context.Context, tx *gorm.DB, lineID string, delta int, projectID string, at time.Time) (rows int64, err error) {
	if serr := rls.ScopeTx(tx, projectID); serr != nil {
		return 0, serr
	}
	res := tx.WithContext(ctx).Model(&PurchaseLineEntity{}).
		Where("id = ? AND received_quantity + ? <= quantity", lineID, delta).
		Updates(map[string]any{
			"received_quantity": gorm.Expr("received_quantity + ?", delta),
			"update_time":       at,
		})
	return res.RowsAffected, res.Error
}
