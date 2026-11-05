package inventorymodel

import (
	"context"
	"errors"
	json "go_wp/pkg/jsonx"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"go_wp/pkg/rls"
)

type WarehouseEntity struct {
	ID        string `gorm:"column:id;primaryKey"`
	ProjectID string `gorm:"column:project_id;not null"`
	Code      string `gorm:"column:code;not null"`
	Name      string `gorm:"column:name;not null"`
	Type      string `gorm:"column:type;not null"`
	Status    string `gorm:"column:status;not null"`
	IsDefault bool   `gorm:"column:is_default;not null"`
	Sort      int    `gorm:"column:sort;not null"`

	DeductPriority int `gorm:"column:deduct_priority;not null"`

	Config    json.RawMessage `gorm:"column:config;type:jsonb;not null"`
	Metadata  json.RawMessage `gorm:"column:metadata;type:jsonb;not null"`
	CreatedAt time.Time       `gorm:"column:create_time;not null"`
	UpdatedAt time.Time       `gorm:"column:update_time;not null"`
}

func (WarehouseEntity) TableName() string { return "inventory_warehouses" }

type StockEntity struct {
	ID          string `gorm:"column:id;primaryKey"`
	ProjectID   string `gorm:"column:project_id;not null"`
	WarehouseID string `gorm:"column:warehouse_id;not null"`

	ProductID *string `gorm:"column:product_id"`
	VariantID *string `gorm:"column:variant_id"`
	SKUCode   string  `gorm:"column:sku_code;not null"`

	ExternalSKU string `gorm:"column:external_sku;not null"`

	TrackQuantity bool `gorm:"column:track_quantity;not null"`
	Quantity      int  `gorm:"column:quantity;not null"`

	ReservedQuantity int `gorm:"column:reserved_quantity;not null"`

	CostPrice *float64 `gorm:"column:cost_price"`

	Name string `gorm:"column:name;not null"`

	ImageURL string `gorm:"column:image_url;not null"`

	Barcode string `gorm:"column:barcode;not null"`

	FreightPrice *float64 `gorm:"column:freight_price;not null;default:70"`

	CostCurrency     string   `gorm:"column:cost_currency;not null;default:CNY"`
	CostBaseCurrency string   `gorm:"column:cost_base_currency;not null;default:CNY"`
	CostFXRate       *float64 `gorm:"column:cost_fx_rate"`

	Status string `gorm:"column:status;not null;default:active"`
	Remark string `gorm:"column:remark;not null"`

	Metadata  json.RawMessage `gorm:"column:metadata;type:jsonb;not null"`
	CreatedAt time.Time       `gorm:"column:create_time;not null"`
	UpdatedAt time.Time       `gorm:"column:update_time;not null"`
}

func (StockEntity) TableName() string { return "inventory_stocks" }

type StockRow struct {
	ID            string `gorm:"column:id"`
	ProjectID     string `gorm:"column:project_id"`
	WarehouseID   string `gorm:"column:warehouse_id"`
	WarehouseCode string `gorm:"column:warehouse_code"`
	WarehouseName string `gorm:"column:warehouse_name"`
	ProductID     string `gorm:"column:product_id"`
	VariantID     string `gorm:"column:variant_id"`
	SKUCode       string `gorm:"column:sku_code"`
	ExternalSKU   string `gorm:"column:external_sku"`

	TrackQuantity bool `gorm:"column:track_quantity"`
	Quantity      int  `gorm:"column:quantity"`

	ReservedQuantity int      `gorm:"column:reserved_quantity"`
	CostPrice        *float64 `gorm:"column:cost_price"`

	Name             string    `gorm:"column:name"`
	ImageURL         string    `gorm:"column:image_url"`
	Barcode          string    `gorm:"column:barcode"`
	FreightPrice     *float64  `gorm:"column:freight_price"`
	CostCurrency     string    `gorm:"column:cost_currency"`
	CostBaseCurrency string    `gorm:"column:cost_base_currency"`
	CostFXRate       *float64  `gorm:"column:cost_fx_rate"`
	Status           string    `gorm:"column:status"`
	Remark           string    `gorm:"column:remark"`
	CreatedAt        time.Time `gorm:"column:create_time"`
	UpdatedAt        time.Time `gorm:"column:update_time"`
}

type StockFilter struct {
	ProjectID   string
	WarehouseID string
	ProductID   string
	VariantID   string
	SKUCode     string

	ExternalSKU string
}

func PtrString(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

func StringPtr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

type Model struct{ db *gorm.DB }

func NewModel(db *gorm.DB) *Model { return &Model{db: db} }

func (m *Model) DB(ctx context.Context) *gorm.DB {
	return m.db.WithContext(ctx).Model(&WarehouseEntity{})
}

func (m *Model) StockDB(ctx context.Context) *gorm.DB {
	return m.db.WithContext(ctx).Model(&StockEntity{})
}

func (m *Model) CreateWarehouse(ctx context.Context, e *WarehouseEntity, asDefault bool) (err error) {

	if !asDefault {
		return rls.InProjectScope(ctx, m.db, e.ProjectID, func(tx *gorm.DB) error {
			return tx.Model(&WarehouseEntity{}).Create(e).Error
		})
	}
	return rls.InProjectScope(ctx, m.db, e.ProjectID, func(tx *gorm.DB) error {
		if err := tx.Model(&WarehouseEntity{}).
			Where("project_id = ? AND is_default", e.ProjectID).
			Update("is_default", false).Error; err != nil {
			return err
		}
		return tx.Model(&WarehouseEntity{}).Create(e).Error
	})
}

func (m *Model) GetWarehouse(ctx context.Context, id, projectID string) (e *WarehouseEntity, err error) {
	e = &WarehouseEntity{}
	err = rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {
		return tx.WithContext(ctx).Model(&WarehouseEntity{}).Where("id = ?", id).First(e).Error
	})
	return e, err
}

func (m *Model) GetDefaultWarehouse(ctx context.Context, projectID string) (e *WarehouseEntity, err error) {
	e = &WarehouseEntity{}
	err = rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {
		return tx.WithContext(ctx).Model(&WarehouseEntity{}).
			Where("project_id = ? AND is_default", projectID).First(e).Error
	})
	return e, err
}

func (m *Model) CodeExists(ctx context.Context, projectID, code, excludeID string) (exists bool, err error) {

	err = rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {
		q := tx.WithContext(ctx).Model(&WarehouseEntity{}).
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
	return exists, err
}

func (m *Model) ListWarehouses(ctx context.Context, projectID string) (list []*WarehouseEntity, err error) {
	err = rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {
		return tx.WithContext(ctx).Model(&WarehouseEntity{}).
			Order("is_default DESC, sort ASC, code ASC").Find(&list).Error
	})
	return list, err
}

func (m *Model) UpdateWarehouse(ctx context.Context, e *WarehouseEntity) (err error) {
	return rls.InProjectScope(ctx, m.db, e.ProjectID, func(tx *gorm.DB) error {
		return tx.Model(&WarehouseEntity{}).Where("id = ?", e.ID).Save(e).Error
	})
}

func (m *Model) SetDefaultWarehouse(ctx context.Context, projectID, id string) (err error) {
	return rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {
		if err := tx.Model(&WarehouseEntity{}).
			Where("project_id = ? AND is_default AND id <> ?", projectID, id).
			Update("is_default", false).Error; err != nil {
			return err
		}
		return tx.Model(&WarehouseEntity{}).Where("id = ?", id).Update("is_default", true).Error
	})
}

// GetWarehouseTx 在调用方事务内读取仓库（工程作用域已由事务持有者设置）。
func (m *Model) GetWarehouseTx(ctx context.Context, tx *gorm.DB, id, projectID string) (*WarehouseEntity, error) {
	e := &WarehouseEntity{}
	if err := tx.WithContext(ctx).Model(&WarehouseEntity{}).
		Where("id = ? AND project_id = ?", id, projectID).First(e).Error; err != nil {
		return nil, err
	}
	return e, nil
}

// CountNonZeroStocksTx 在调用方事务内统计有库存的 SKU 行数。
func (m *Model) CountNonZeroStocksTx(ctx context.Context, tx *gorm.DB, warehouseID, projectID string) (int64, error) {
	var n int64
	err := tx.WithContext(ctx).Model(&StockEntity{}).
		Where("warehouse_id = ? AND quantity <> 0 AND project_id = ?", warehouseID, projectID).
		Count(&n).Error
	return n, err
}

// DeleteWarehouseTx 在调用方事务内删除仓库。
func (m *Model) DeleteWarehouseTx(ctx context.Context, tx *gorm.DB, id string) error {
	return tx.WithContext(ctx).Model(&WarehouseEntity{}).Where("id = ?", id).Delete(&WarehouseEntity{}).Error
}

func (m *Model) DeleteWarehouse(ctx context.Context, id, projectID string) (err error) {
	return rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {
		return tx.WithContext(ctx).Model(&WarehouseEntity{}).
			Where("id = ?", id).Delete(&WarehouseEntity{}).Error
	})
}

func (m *Model) GetStock(ctx context.Context, id, projectID string) (e *StockEntity, err error) {
	e = &StockEntity{}
	err = rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {
		return tx.WithContext(ctx).Model(&StockEntity{}).Where("id = ?", id).First(e).Error
	})
	return e, err
}

func (m *Model) GetStockByVariantWarehouse(ctx context.Context, variantID, warehouseID, projectID string) (e *StockEntity, err error) {
	e = &StockEntity{}
	err = rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {
		return tx.WithContext(ctx).Model(&StockEntity{}).
			Where("variant_id = ? AND warehouse_id = ?", variantID, warehouseID).
			Order("sku_code ASC").First(e).Error
	})
	return e, err
}

func (m *Model) GetStockScopedTx(ctx context.Context, tx *gorm.DB, id, projectID string) (e *StockEntity, err error) {
	if serr := rls.ScopeTx(tx, projectID); serr != nil {
		return nil, serr
	}
	e = &StockEntity{}
	if gerr := tx.WithContext(ctx).Model(&StockEntity{}).
		Where("id = ?", id).First(e).Error; gerr != nil {
		return nil, gerr
	}
	return e, nil
}

func (m *Model) UpdateStockFieldsTx(ctx context.Context, tx *gorm.DB, id, projectID string, fields map[string]any) (err error) {
	if len(fields) == 0 {
		return nil
	}
	if serr := rls.ScopeTx(tx, projectID); serr != nil {
		return serr
	}
	res := tx.WithContext(ctx).Model(&StockEntity{}).Where("id = ?", id).Updates(fields)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {

		return gorm.ErrRecordNotFound
	}
	return nil
}

func (m *Model) GetStockByWarehouseSKU(ctx context.Context, warehouseID, skuCode, projectID string) (e *StockEntity, err error) {
	e = &StockEntity{}
	err = rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {
		return tx.WithContext(ctx).Model(&StockEntity{}).
			Where("warehouse_id = ? AND sku_code = ?", warehouseID, skuCode).First(e).Error
	})
	return e, err
}

func (m *Model) EnsureStock(ctx context.Context, e *StockEntity) (out *StockEntity, err error) {
	err = m.Transaction(ctx, func(tx *gorm.DB) error {
		var terr error
		out, terr = m.EnsureStockTx(ctx, tx, e)
		return terr
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (m *Model) EnsureStockTx(ctx context.Context, tx *gorm.DB, e *StockEntity) (out *StockEntity, err error) {
	if err = rls.ScopeTx(tx, e.ProjectID); err != nil {
		return nil, err
	}
	existing, gerr := m.getStockByWarehouseSKUTx(ctx, tx, e.ProjectID, e.WarehouseID, e.SKUCode)
	if gerr == nil {

		return existing, nil
	}
	if !errors.Is(gerr, gorm.ErrRecordNotFound) {
		return nil, gerr
	}

	normalizeStockTracking(e)
	res := tx.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "warehouse_id"}, {Name: "sku_code"}},
		DoNothing: true,
	}).Create(e)
	if res.Error != nil {
		return nil, res.Error
	}
	if res.RowsAffected == 0 {
		return m.getStockByWarehouseSKUTx(ctx, tx, e.ProjectID, e.WarehouseID, e.SKUCode)
	}
	return e, nil
}

func normalizeStockTracking(e *StockEntity) {
	if e != nil && !e.TrackQuantity && e.Quantity != 0 {
		e.TrackQuantity = true
	}
}

func (m *Model) getStockByWarehouseSKUTx(ctx context.Context, tx *gorm.DB, projectID, warehouseID, skuCode string) (e *StockEntity, err error) {
	e = &StockEntity{}
	if serr := rls.ScopeTx(tx, projectID); serr != nil {
		return nil, serr
	}
	err = tx.WithContext(ctx).Model(&StockEntity{}).
		Where("warehouse_id = ? AND sku_code = ?", warehouseID, skuCode).First(e).Error
	return e, err
}

func (m *Model) getStockByVariantWarehouseTx(ctx context.Context, tx *gorm.DB, projectID string, variantID *string, warehouseID string) (e *StockEntity, err error) {
	e = &StockEntity{}
	if serr := rls.ScopeTx(tx, projectID); serr != nil {
		return nil, serr
	}
	q := tx.WithContext(ctx).Model(&StockEntity{}).Where("warehouse_id = ?", warehouseID)
	if variantID == nil {
		q = q.Where("variant_id IS NULL")
	} else {
		q = q.Where("variant_id = ?", *variantID)
	}
	err = q.First(e).Error
	return e, err
}

func (m *Model) CountNonZeroStocks(ctx context.Context, warehouseID, projectID string) (n int64, err error) {
	err = rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {
		return tx.WithContext(ctx).Model(&StockEntity{}).
			Where("warehouse_id = ? AND quantity <> 0", warehouseID).Count(&n).Error
	})
	return n, err
}

func (m *Model) ListStockFactsByIDs(ctx context.Context, projectID string, stockIDs []string) (list []*StockFactRow, err error) {
	if len(stockIDs) == 0 {
		return nil, nil
	}
	err = rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {
		return stockRowsQuery(ctx, tx).
			Where("s.id IN ?", stockIDs).
			Order("w.is_default DESC, w.sort ASC, w.code ASC, s.sku_code ASC, s.id ASC").
			Scan(&list).Error
	})
	return list, err
}

func (m *Model) ListAllStockIDs(ctx context.Context, projectID string) (ids []string, err error) {
	err = rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {
		return tx.WithContext(ctx).Model(&StockEntity{}).Order("id ASC").Pluck("id", &ids).Error
	})
	return ids, err
}

type StockFactRow struct {
	ID               string   `gorm:"column:id"`
	WarehouseID      string   `gorm:"column:warehouse_id"`
	WarehouseCode    string   `gorm:"column:warehouse_code"`
	WarehouseName    string   `gorm:"column:warehouse_name"`
	SKUCode          string   `gorm:"column:sku_code"`
	TrackQuantity    bool     `gorm:"column:track_quantity"`
	Quantity         int      `gorm:"column:quantity"`
	ReservedQuantity int      `gorm:"column:reserved_quantity"`
	CostPrice        *float64 `gorm:"column:cost_price"`

	Name     string `gorm:"column:name"`
	ImageURL string `gorm:"column:image_url"`
}

type ProductWarehouseStockRow struct {
	ProductID     string   `gorm:"column:product_id"`
	WarehouseID   string   `gorm:"column:warehouse_id"`
	WarehouseCode string   `gorm:"column:warehouse_code"`
	WarehouseName string   `gorm:"column:warehouse_name"`
	SKUCode       string   `gorm:"column:sku_code"`
	TrackQuantity bool     `gorm:"column:track_quantity"`
	Quantity      int      `gorm:"column:quantity"`
	CostPrice     *float64 `gorm:"column:cost_price"`
}

func (m *Model) WarehouseStocksByProducts(ctx context.Context, projectID string, productIDs []string) (list []ProductWarehouseStockRow, err error) {
	if len(productIDs) == 0 {
		return nil, nil
	}
	err = rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {
		return tx.WithContext(ctx).Table("inventory_stocks AS s").
			Select("s.product_id, s.warehouse_id, s.sku_code, s.track_quantity, s.quantity, s.cost_price, "+
				"w.code AS warehouse_code, w.name AS warehouse_name").
			Joins("JOIN inventory_warehouses AS w ON w.id = s.warehouse_id").
			Where("s.product_id IN ?", productIDs).
			Order("s.product_id ASC, w.is_default DESC, w.sort ASC, w.code ASC, s.sku_code ASC").
			Scan(&list).Error
	})
	return list, err
}

func stockRowsQuery(ctx context.Context, db *gorm.DB) *gorm.DB {
	return db.WithContext(ctx).Table("inventory_stocks AS s").
		Select("s.id, s.project_id, s.warehouse_id, s.product_id, s.variant_id, s.sku_code, " +
			"s.external_sku, s.track_quantity, s.quantity, s.reserved_quantity, s.cost_price, " +
			"s.name, s.image_url, s.barcode, s.freight_price, s.cost_currency, s.cost_base_currency, " +
			"s.cost_fx_rate, s.status, s.remark, " +
			"s.create_time, s.update_time, " +
			"w.code AS warehouse_code, w.name AS warehouse_name").
		Joins("JOIN inventory_warehouses AS w ON w.id = s.warehouse_id")
}

func applyStockFilter(q *gorm.DB, f StockFilter) *gorm.DB {
	if f.ProjectID != "" {
		q = q.Where("s.project_id = ?", f.ProjectID)
	}
	if f.WarehouseID != "" {
		q = q.Where("s.warehouse_id = ?", f.WarehouseID)
	}
	if f.ProductID != "" {
		q = q.Where("s.product_id = ?", f.ProductID)
	}
	if f.VariantID != "" {
		q = q.Where("s.variant_id = ?", f.VariantID)
	}
	if f.SKUCode != "" {
		q = q.Where("s.sku_code = ?", f.SKUCode)
	}
	if f.ExternalSKU != "" {
		q = q.Where("s.external_sku = ?", f.ExternalSKU)
	}
	return q
}

func (m *Model) ListStockRows(ctx context.Context, f StockFilter, limit, offset int) (list []*StockRow, err error) {
	err = rls.InProjectScope(ctx, m.db, f.ProjectID, func(tx *gorm.DB) error {
		q := applyStockFilter(stockRowsQuery(ctx, tx), f).
			Order("w.is_default DESC, w.sort ASC, w.code ASC, s.variant_id ASC")
		if limit > 0 {
			q = q.Limit(limit).Offset(offset)
		}
		return q.Scan(&list).Error
	})
	return list, err
}

type WarehouseSKURow struct {
	ID            string  `gorm:"column:id"`
	WarehouseID   string  `gorm:"column:warehouse_id"`
	WarehouseCode string  `gorm:"column:warehouse_code"`
	WarehouseName string  `gorm:"column:warehouse_name"`
	IsDefault     bool    `gorm:"column:is_default"`
	SKUCode       string  `gorm:"column:sku_code"`
	ExternalSKU   string  `gorm:"column:external_sku"`
	ProductID     *string `gorm:"column:product_id"`
	VariantID     *string `gorm:"column:variant_id"`

	HasVariant bool `gorm:"column:has_variant"`

	Quantity         int `gorm:"column:quantity"`
	ReservedQuantity int `gorm:"column:reserved_quantity"`

	TrackQuantity bool `gorm:"column:track_quantity"`

	Name             string    `gorm:"column:name"`
	ImageURL         string    `gorm:"column:image_url"`
	Barcode          string    `gorm:"column:barcode"`
	FreightPrice     *float64  `gorm:"column:freight_price"`
	CostCurrency     string    `gorm:"column:cost_currency"`
	CostBaseCurrency string    `gorm:"column:cost_base_currency"`
	CostFXRate       *float64  `gorm:"column:cost_fx_rate"`
	Status           string    `gorm:"column:status"`
	Remark           string    `gorm:"column:remark"`
	CostPrice        *float64  `gorm:"column:cost_price"`
	CreatedAt        time.Time `gorm:"column:create_time"`
	UpdatedAt        time.Time `gorm:"column:update_time"`
}

type WarehouseSKUFilter struct {
	ProjectID   string
	WarehouseID string

	Keyword string

	Status string
}

func warehouseSKUsQuery(ctx context.Context, db *gorm.DB) *gorm.DB {
	return db.WithContext(ctx).Table("inventory_stocks AS s").
		Select("s.id, s.warehouse_id, s.product_id, s.variant_id, s.sku_code, s.external_sku, " +
			"s.quantity, s.reserved_quantity, s.track_quantity, " +

			"s.cost_price, s.update_time, " +
			"s.name, s.image_url, s.barcode, s.freight_price, s.cost_currency, s.cost_base_currency, " +
			"s.cost_fx_rate, s.status, s.remark, s.create_time, " +
			"w.code AS warehouse_code, w.name AS warehouse_name, w.is_default AS is_default, " +
			"(s.variant_id IS NOT NULL) AS has_variant").
		Joins("JOIN inventory_warehouses AS w ON w.id = s.warehouse_id")
}

func applyWarehouseSKUFilter(q *gorm.DB, f WarehouseSKUFilter) *gorm.DB {
	if f.ProjectID != "" {
		q = q.Where("s.project_id = ?", f.ProjectID)
	}
	if f.WarehouseID != "" {
		q = q.Where("s.warehouse_id = ?", f.WarehouseID)
	}
	if f.Status != "" {
		q = q.Where("s.status = ?", f.Status)
	}
	if kw := f.Keyword; kw != "" {
		like := "%" + kw + "%"
		q = q.Where("(s.sku_code ILIKE ? OR s.external_sku ILIKE ? OR s.name ILIKE ? OR s.barcode = ?)",
			like, like, like, kw)
	}
	return q
}

func (m *Model) CountWarehouseSKUs(ctx context.Context, f WarehouseSKUFilter) (total int64, err error) {
	err = rls.InProjectScope(ctx, m.db, f.ProjectID, func(tx *gorm.DB) error {
		return applyWarehouseSKUFilter(warehouseSKUsQuery(ctx, tx), f).Count(&total).Error
	})
	return total, err
}

func (m *Model) ListWarehouseSKUs(ctx context.Context, f WarehouseSKUFilter, limit, offset int) (list []*WarehouseSKURow, err error) {
	err = rls.InProjectScope(ctx, m.db, f.ProjectID, func(tx *gorm.DB) error {
		q := applyWarehouseSKUFilter(warehouseSKUsQuery(ctx, tx), f).
			Order("w.is_default DESC, w.sort ASC, w.code ASC, s.sku_code ASC")
		if limit > 0 {
			q = q.Limit(limit).Offset(offset)
		}
		return q.Scan(&list).Error
	})
	return list, err
}

func (m *Model) FindStockByWarehouseSKU(ctx context.Context, projectID, warehouseID, skuCode string) (e *StockEntity, err error) {
	e = &StockEntity{}
	err = rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {
		return tx.WithContext(ctx).Model(&StockEntity{}).
			Where("warehouse_id = ? AND sku_code = ?", warehouseID, skuCode).First(e).Error
	})
	return e, err
}

func (m *Model) SetExternalSKUByVariantWarehouse(ctx context.Context, projectID, warehouseID, variantID, externalSKU string) (affected int64, err error) {
	err = rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {
		res := tx.WithContext(ctx).Model(&StockEntity{}).
			Where("warehouse_id = ? AND variant_id = ?", warehouseID, variantID).
			Update("external_sku", externalSKU)
		if res.Error != nil {
			return res.Error
		}
		affected = res.RowsAffected
		return nil
	})
	return affected, err
}

func (m *Model) StockSKUCodeExists(ctx context.Context, projectID, warehouseID, skuCode, excludeVariantID string) (exists bool, err error) {
	err = rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {
		q := tx.WithContext(ctx).Model(&StockEntity{}).
			Where("warehouse_id = ? AND sku_code = ?", warehouseID, skuCode)
		if excludeVariantID != "" {
			q = q.Where("variant_id <> ?", excludeVariantID)
		}
		var n int64
		if cerr := q.Count(&n).Error; cerr != nil {
			return cerr
		}
		exists = n > 0
		return nil
	})
	return exists, err
}

func (m *Model) ExternalSKUProductConflict(ctx context.Context, projectID, warehouseID, externalSKU, excludeProductID string) (owner string, err error) {
	if externalSKU == "" {
		return "", nil
	}
	err = rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {
		q := tx.WithContext(ctx).Model(&StockEntity{}).
			Where("warehouse_id = ? AND external_sku = ?", warehouseID, externalSKU)
		if excludeProductID != "" {
			q = q.Where("product_id <> ?", excludeProductID)
		}
		var ids []string
		if qerr := q.Order("product_id ASC").Limit(1).Pluck("product_id", &ids).Error; qerr != nil {
			return qerr
		}
		if len(ids) > 0 {
			owner = ids[0]
		}
		return nil
	})
	return owner, err
}
