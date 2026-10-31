package inventorymodel

import (
	"context"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"go_wp/pkg/rls"
)

type ReasonEntity struct {
	ID        int64   `gorm:"column:id;primaryKey"`
	ProjectID *string `gorm:"column:project_id"`
	Code      string  `gorm:"column:code;not null"`

	Name       string    `gorm:"column:name;not null"`
	Direction  string    `gorm:"column:direction;not null"`
	IsBuiltin  bool      `gorm:"column:is_builtin;not null"`
	Status     string    `gorm:"column:status;not null"`
	Sort       int       `gorm:"column:sort;not null"`
	CreateTime time.Time `gorm:"column:create_time;not null"`
	UpdatedAt  time.Time `gorm:"column:update_time;not null"`
}

func (ReasonEntity) TableName() string { return "inventory_change_reasons" }

type MovementEntity struct {
	ID          string `gorm:"column:id;primaryKey"`
	ProjectID   string `gorm:"column:project_id;not null"`
	WarehouseID string `gorm:"column:warehouse_id;not null"`

	ProductID       *string `gorm:"column:product_id"`
	VariantID       *string `gorm:"column:variant_id"`
	SKUCode         string  `gorm:"column:sku_code;not null"`
	Direction       string  `gorm:"column:direction;not null"`
	Quantity        int     `gorm:"column:quantity;not null"`
	Delta           int     `gorm:"column:delta;not null"`
	QuantityBefore  int     `gorm:"column:quantity_before;not null"`
	QuantityAfter   int     `gorm:"column:quantity_after;not null"`
	ReasonID        *int64  `gorm:"column:reason_id"`
	ReasonCode      string  `gorm:"column:reason_code;not null"`
	ParentVariantID *string `gorm:"column:parent_variant_id"`

	StockID *string `gorm:"column:stock_id"`

	StockName     string `gorm:"column:stock_name;not null"`
	StockImageURL string `gorm:"column:stock_image_url;not null"`
	SourceType    string `gorm:"column:source_type;not null"`
	SourceRef     string `gorm:"column:source_ref;not null"`
	Remark        string `gorm:"column:remark;not null"`
	OperatorID    string `gorm:"column:operator_id;not null"`

	UnitCost  *float64  `gorm:"column:unit_cost"`
	BatchID   string    `gorm:"column:batch_id;not null"`
	CreatedAt time.Time `gorm:"column:create_time;not null"`
}

func (MovementEntity) TableName() string { return "inventory_stock_movements" }

type MovementRow struct {
	ID            string `gorm:"column:id"`
	ProjectID     string `gorm:"column:project_id"`
	WarehouseID   string `gorm:"column:warehouse_id"`
	WarehouseCode string `gorm:"column:warehouse_code"`
	WarehouseName string `gorm:"column:warehouse_name"`

	ProductID       *string `gorm:"column:product_id"`
	VariantID       *string `gorm:"column:variant_id"`
	SKUCode         string  `gorm:"column:sku_code"`
	Direction       string  `gorm:"column:direction"`
	Quantity        int     `gorm:"column:quantity"`
	Delta           int     `gorm:"column:delta"`
	QuantityBefore  int     `gorm:"column:quantity_before"`
	QuantityAfter   int     `gorm:"column:quantity_after"`
	ReasonID        *int64  `gorm:"column:reason_id"`
	ReasonCode      string  `gorm:"column:reason_code"`
	ReasonName      string  `gorm:"column:reason_name"`
	ParentVariantID *string `gorm:"column:parent_variant_id"`

	StockID *string `gorm:"column:stock_id"`

	StockName     string `gorm:"column:stock_name"`
	StockImageURL string `gorm:"column:stock_image_url"`
	SourceType    string `gorm:"column:source_type"`
	SourceRef     string `gorm:"column:source_ref"`
	Remark        string `gorm:"column:remark"`
	OperatorID    string `gorm:"column:operator_id"`

	UnitCost  *float64  `gorm:"column:unit_cost"`
	BatchID   string    `gorm:"column:batch_id"`
	CreatedAt time.Time `gorm:"column:create_time"`
}

type MovementFilter struct {
	ProjectID   string
	WarehouseID string
	ProductID   string
	VariantID   string
	SKUCode     string
	Direction   string
	ReasonCode  string
	SourceType  string
	SourceRef   string
	BatchID     string

	StockID string

	TimeFrom *time.Time
	TimeTo   *time.Time
}

type ReasonFilter struct {
	ProjectID       string
	Direction       string
	Keyword         string
	IncludeDisabled bool
}

func (m *Model) Transaction(ctx context.Context, fn func(tx *gorm.DB) error) error {
	return m.db.WithContext(ctx).Transaction(fn)
}

func (m *Model) ListStocksByVariantsTx(ctx context.Context, tx *gorm.DB, variantIDs []string, projectID string) (list []*StockEntity, err error) {
	if len(variantIDs) == 0 {
		return nil, nil
	}
	if serr := rls.ScopeTx(tx, projectID); serr != nil {
		return nil, serr
	}
	err = tx.WithContext(ctx).Model(&StockEntity{}).Where("variant_id IN ?", variantIDs).Find(&list).Error
	return list, err
}

func (m *Model) EnsureStocksTx(ctx context.Context, tx *gorm.DB, rows []*StockEntity) (err error) {
	if len(rows) == 0 {
		return nil
	}

	if serr := rls.ScopeTx(tx, rows[0].ProjectID); serr != nil {
		return serr
	}

	for _, row := range rows {
		normalizeStockTracking(row)
	}
	return tx.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "warehouse_id"}, {Name: "sku_code"}},
		DoNothing: true,
	}).Create(&rows).Error
}

func (m *Model) LockStockBySKUTx(ctx context.Context, tx *gorm.DB, projectID, warehouseID, skuCode string) (e *StockEntity, err error) {
	e = &StockEntity{}
	if serr := rls.ScopeTx(tx, projectID); serr != nil {
		return nil, serr
	}
	err = tx.WithContext(ctx).Model(&StockEntity{}).
		Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("warehouse_id = ? AND sku_code = ?", warehouseID, skuCode).First(e).Error
	return e, err
}

func (m *Model) LockStockByIDTx(ctx context.Context, tx *gorm.DB, projectID, id string) (e *StockEntity, err error) {
	e = &StockEntity{}
	if serr := rls.ScopeTx(tx, projectID); serr != nil {
		return nil, serr
	}
	err = tx.WithContext(ctx).Model(&StockEntity{}).
		Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("id = ?", id).First(e).Error
	return e, err
}

func (m *Model) UpdateStockQuantityAndTrackingTx(ctx context.Context, tx *gorm.DB, id string, quantity int, track bool, at time.Time) (err error) {
	return tx.WithContext(ctx).Model(&StockEntity{}).Where("id = ?", id).
		Updates(map[string]any{"quantity": quantity, "track_quantity": track, "update_time": at}).Error
}

func (m *Model) UpdateStockCostTx(ctx context.Context, tx *gorm.DB, id string, cost float64, at time.Time) (err error) {
	return tx.WithContext(ctx).Model(&StockEntity{}).Where("id = ?", id).
		Updates(map[string]any{"cost_price": cost, "update_time": at}).Error
}

func (m *Model) CreateMovementsTx(ctx context.Context, tx *gorm.DB, rows []*MovementEntity) (err error) {
	if len(rows) == 0 {
		return nil
	}

	if serr := rls.ScopeTx(tx, rows[0].ProjectID); serr != nil {
		return serr
	}
	return tx.WithContext(ctx).CreateInBatches(&rows, 100).Error
}

func movementRowsQuery(ctx context.Context, db *gorm.DB) *gorm.DB {
	return db.WithContext(ctx).Table("inventory_stock_movements AS mv").
		Select("mv.id, mv.project_id, mv.warehouse_id, mv.product_id, mv.variant_id, mv.sku_code, " +
			"mv.stock_id, mv.stock_name, mv.stock_image_url, " +
			"mv.direction, mv.quantity, mv.delta, mv.quantity_before, mv.quantity_after, " +
			"mv.reason_id, mv.reason_code, mv.parent_variant_id, mv.source_type, mv.source_ref, " +
			"mv.remark, mv.operator_id, mv.unit_cost, mv.batch_id, mv.create_time, " +
			"w.code AS warehouse_code, w.name AS warehouse_name, " +
			"COALESCE(r.name, '') AS reason_name").
		Joins("JOIN inventory_warehouses AS w ON w.id = mv.warehouse_id").
		Joins("LEFT JOIN inventory_change_reasons AS r ON r.id = mv.reason_id")
}

func (m *Model) ExistsMovementBySource(ctx context.Context, projectID, sourceType, sourceRef string) (bool, error) {
	if projectID == "" || sourceType == "" || sourceRef == "" {
		return false, nil
	}
	var count int64

	err := rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {
		return tx.WithContext(ctx).Table("inventory_stock_movements").
			Where("project_id = ? AND source_type = ? AND source_ref = ?", projectID, sourceType, sourceRef).
			Limit(1).Count(&count).Error
	})
	return count > 0, err
}

func (m *Model) ListMovementRows(ctx context.Context, f MovementFilter, limit, offset int) (list []*MovementRow, err error) {
	err = rls.InProjectScope(ctx, m.db, f.ProjectID, func(tx *gorm.DB) error {
		return m.scanMovementRows(ctx, movementRowsQuery(ctx, tx), f, limit, offset, &list)
	})
	return list, err
}

func applyMovementFilter(q *gorm.DB, f MovementFilter) *gorm.DB {
	if f.ProjectID != "" {
		q = q.Where("mv.project_id = ?", f.ProjectID)
	}
	if f.WarehouseID != "" {
		q = q.Where("mv.warehouse_id = ?", f.WarehouseID)
	}
	if f.ProductID != "" {
		q = q.Where("mv.product_id = ?", f.ProductID)
	}
	if f.VariantID != "" {
		q = q.Where("mv.variant_id = ?", f.VariantID)
	}
	if f.SKUCode != "" {
		q = q.Where("mv.sku_code = ?", f.SKUCode)
	}
	if f.Direction != "" {
		q = q.Where("mv.direction = ?", f.Direction)
	}
	if f.ReasonCode != "" {
		q = q.Where("mv.reason_code = ?", f.ReasonCode)
	}
	if f.SourceType != "" {
		q = q.Where("mv.source_type = ?", f.SourceType)
	}
	if f.SourceRef != "" {
		q = q.Where("mv.source_ref = ?", f.SourceRef)
	}
	if f.BatchID != "" {
		q = q.Where("mv.batch_id = ?", f.BatchID)
	}
	if f.StockID != "" {
		q = q.Where("mv.stock_id = ?", f.StockID)
	}

	if f.TimeFrom != nil {
		q = q.Where("mv.create_time >= ?", *f.TimeFrom)
	}
	if f.TimeTo != nil {
		q = q.Where("mv.create_time <= ?", *f.TimeTo)
	}
	return q
}

func (m *Model) scanMovementRows(ctx context.Context, q *gorm.DB, f MovementFilter,
	limit, offset int, list *[]*MovementRow) error {
	q = applyMovementFilter(q, f).Order("mv.create_time DESC, mv.id DESC")
	if limit > 0 {
		q = q.Limit(limit).Offset(offset)
	}
	return q.Scan(list).Error
}

func (m *Model) CountMovements(ctx context.Context, f MovementFilter) (n int64, err error) {
	err = rls.InProjectScope(ctx, m.db, f.ProjectID, func(tx *gorm.DB) error {
		return applyMovementFilter(movementRowsQuery(ctx, tx), f).Count(&n).Error
	})
	return n, err
}

func (m *Model) reasonDB(ctx context.Context) *gorm.DB {
	return m.db.WithContext(ctx).Model(&ReasonEntity{})
}

func (m *Model) ListReasons(ctx context.Context, f ReasonFilter) (list []*ReasonEntity, err error) {
	pid := strings.TrimSpace(f.ProjectID)
	build := func(q *gorm.DB) *gorm.DB {
		if pid != "" {
			q = q.Where("project_id = ? OR project_id IS NULL", pid)
		} else {
			q = q.Where("project_id IS NULL")
		}
		if f.Direction != "" {
			q = q.Where("direction = ?", f.Direction)
		}
		if kw := strings.TrimSpace(f.Keyword); kw != "" {
			q = q.Where("code ILIKE ? OR name ILIKE ?", "%"+kw+"%", "%"+kw+"%")
		}
		if !f.IncludeDisabled {
			q = q.Where("status = 'active'")
		}
		return q.Order("is_builtin DESC, direction ASC, sort ASC, code ASC")
	}
	if pid == "" {
		return list, build(m.reasonDB(ctx)).Find(&list).Error
	}
	err = rls.InProjectScope(ctx, m.db, pid, func(tx *gorm.DB) error {
		return build(tx.WithContext(ctx).Model(&ReasonEntity{})).Find(&list).Error
	})
	return list, err
}

func (m *Model) GetReason(ctx context.Context, id int64, projectID string) (e *ReasonEntity, err error) {
	e = &ReasonEntity{}
	err = rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {
		return tx.WithContext(ctx).Model(&ReasonEntity{}).Where("id = ?", id).First(e).Error
	})
	return e, err
}

func (m *Model) FindReasonByCode(ctx context.Context, projectID, code string) (e *ReasonEntity, err error) {
	e = &ReasonEntity{}
	err = rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {
		q := tx.WithContext(ctx).Model(&ReasonEntity{}).
			Where("lower(code) = lower(?) AND status = 'active'", code).
			Where("project_id = ? OR project_id IS NULL", projectID)
		return q.Order("project_id NULLS LAST, is_builtin ASC").First(e).Error
	})
	return e, err
}

func (m *Model) ReasonCodeExists(ctx context.Context, projectID, code string, excludeID int64) (exists bool, err error) {
	pid := strings.TrimSpace(projectID)
	build := func(q *gorm.DB) *gorm.DB {
		q = q.Where("lower(code) = lower(?)", code)
		if pid != "" {
			q = q.Where("project_id = ? OR project_id IS NULL", pid)
		} else {
			q = q.Where("project_id IS NULL")
		}
		if excludeID != 0 {
			q = q.Where("id <> ?", excludeID)
		}
		return q
	}
	count := func(q *gorm.DB) error {
		var n int64
		if cerr := build(q).Count(&n).Error; cerr != nil {
			return cerr
		}
		exists = n > 0
		return nil
	}
	if pid == "" {
		return exists, count(m.reasonDB(ctx))
	}
	err = rls.InProjectScope(ctx, m.db, pid, func(tx *gorm.DB) error {
		return count(tx.WithContext(ctx).Model(&ReasonEntity{}))
	})
	return exists, err
}

func (m *Model) CreateReason(ctx context.Context, e *ReasonEntity) (err error) {

	if e.ProjectID == nil || strings.TrimSpace(*e.ProjectID) == "" {
		return m.reasonDB(ctx).Create(e).Error
	}
	return rls.InProjectScope(ctx, m.db, *e.ProjectID, func(tx *gorm.DB) error {
		return tx.Model(&ReasonEntity{}).Create(e).Error
	})
}

func (m *Model) UpdateReason(ctx context.Context, e *ReasonEntity) (err error) {
	if e.ProjectID == nil || strings.TrimSpace(*e.ProjectID) == "" {
		return m.reasonDB(ctx).Where("id = ?", e.ID).Save(e).Error
	}
	return rls.InProjectScope(ctx, m.db, *e.ProjectID, func(tx *gorm.DB) error {
		return tx.Model(&ReasonEntity{}).Where("id = ?", e.ID).Save(e).Error
	})
}

func (m *Model) VariantHasStockMovement(ctx context.Context, projectID, variantID string) (exists bool, err error) {
	if strings.TrimSpace(projectID) == "" || strings.TrimSpace(variantID) == "" {
		return false, nil
	}
	var count int64
	err = rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {
		return tx.WithContext(ctx).Table("inventory_stock_movements").
			Where("variant_id = ?", variantID).
			Limit(1).Count(&count).Error
	})
	return count > 0, err
}
