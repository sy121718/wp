// Package inventorymodel inventory 域持久化（issue #15）。
//
// 本 model 是「表访问单元（Repository）」，只做本模块两张表（inventory_warehouses /
// inventory_stocks）的 CRUD、聚合内原子组合与只读投影；业务规则（默认仓唯一、
// 谁能删、短码派生、兜底解析）一律留在 service 层。
//
// 表隔离：本 model 只碰本模块表。跨模块的变体身份（variant_id / product_id）以
// 裸列 + 外键承载，不做跨模块关联查询 —— SKU 编码在本模块留有快照列（sku_code），
// 按 SKU 查库存不必 JOIN 商品模块的表。
//
// gorm tag 不写 default 子句：默认值由 service 显式赋值，DDL 侧已有 DEFAULT。
package inventorymodel

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"gorm.io/gorm"
)

// WarehouseEntity 仓库实体。
//
// Code 是短码：工程内唯一（DDL 侧 upper(code) 唯一索引），也是 SKU 编码的前缀
// （{仓短码}_{商品码}_{序号}）—— 它表达的是「默认发货仓」，不是「这个 SKU 只属于这个仓」，
// SKU 本身是全局的，货可以在多个仓分布。
// IsDefault 标记默认仓：每工程至多一行（DDL 侧部分唯一索引），是「未指定仓库」时的兜底。
type WarehouseEntity struct {
	ID        string          `gorm:"column:id;type:uuid;primaryKey"`
	ProjectID string          `gorm:"column:project_id;type:uuid;not null"`
	Code      string          `gorm:"column:code;type:text;not null"`
	Name      string          `gorm:"column:name;type:text;not null"`
	Status    string          `gorm:"column:status;type:text;not null"`
	IsDefault bool            `gorm:"column:is_default;not null"`
	Sort      int             `gorm:"column:sort;not null"`
	Metadata  json.RawMessage `gorm:"column:metadata;type:jsonb;not null"`
	CreatedAt time.Time       `gorm:"column:created_at;not null"`
	UpdatedAt time.Time       `gorm:"column:updated_at;not null"`
}

// TableName 实现 gorm 表名。
func (WarehouseEntity) TableName() string { return "inventory_warehouses" }

// StockEntity 库存记录 —— 库存**真源**。
//
// 维度是「SKU × 仓库」：UNIQUE (variant_id, warehouse_id)，同一 SKU 可在多个仓各有一行。
// Quantity 是可用量真源；一切影响可用量的判断（扣减、超卖校验）只能读本表的这一列
// 并加行锁，绝不读 product_variants.stock_total（那只是后台列表的冗余缓存）。
type StockEntity struct {
	ID          string          `gorm:"column:id;type:uuid;primaryKey"`
	ProjectID   string          `gorm:"column:project_id;type:uuid;not null"`
	WarehouseID string          `gorm:"column:warehouse_id;type:uuid;not null"`
	ProductID   string          `gorm:"column:product_id;type:uuid;not null"`
	VariantID   string          `gorm:"column:variant_id;type:uuid;not null"`
	SKUCode     string          `gorm:"column:sku_code;type:text;not null"`
	Quantity    int             `gorm:"column:quantity;not null"`
	Metadata    json.RawMessage `gorm:"column:metadata;type:jsonb;not null"`
	CreatedAt   time.Time       `gorm:"column:created_at;not null"`
	UpdatedAt   time.Time       `gorm:"column:updated_at;not null"`
}

// TableName 实现 gorm 表名。
func (StockEntity) TableName() string { return "inventory_stocks" }

// StockRow 库存记录 + 仓库展示信息（本模块两张表只读 join 的投影）。
//
// metadata 是「默认查询不取」的列，投影不含它（与商品域一致）。
type StockRow struct {
	ID            string    `gorm:"column:id"`
	ProjectID     string    `gorm:"column:project_id"`
	WarehouseID   string    `gorm:"column:warehouse_id"`
	WarehouseCode string    `gorm:"column:warehouse_code"`
	WarehouseName string    `gorm:"column:warehouse_name"`
	ProductID     string    `gorm:"column:product_id"`
	VariantID     string    `gorm:"column:variant_id"`
	SKUCode       string    `gorm:"column:sku_code"`
	Quantity      int       `gorm:"column:quantity"`
	CreatedAt     time.Time `gorm:"column:created_at"`
	UpdatedAt     time.Time `gorm:"column:updated_at"`
}

// StockFilter 库存记录查询条件（条件以参数传入，方法内不写死业务判断）。
type StockFilter struct {
	ProjectID   string
	WarehouseID string
	ProductID   string
	VariantID   string
	SKUCode     string
}

// Model inventory 域仓储。
type Model struct{ db *gorm.DB }

// NewModel 构造（不持有业务状态）。
func NewModel(db *gorm.DB) *Model { return &Model{db: db} }

// DB 仓库表句柄（内部实现细节，只允许被本 model 的仓储方法消费）。
func (m *Model) DB(ctx context.Context) *gorm.DB {
	return m.db.WithContext(ctx).Model(&WarehouseEntity{})
}

// StockDB 库存表句柄（同上，仅本 model 内部使用）。
func (m *Model) StockDB(ctx context.Context) *gorm.DB {
	return m.db.WithContext(ctx).Model(&StockEntity{})
}

// —— 仓库 ——

// CreateWarehouse 写入仓库行；asDefault 为真时在同一事务里先清掉本工程其它默认标记。
//
// 「每工程一个默认仓」的不变量由 service 决定，原子性由本方法保证 —— 先清后写，
// 中间态若被外部看到就是「没有默认仓」。
func (m *Model) CreateWarehouse(ctx context.Context, e *WarehouseEntity, asDefault bool) (err error) {
	if !asDefault {
		return m.DB(ctx).Create(e).Error
	}
	return m.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&WarehouseEntity{}).
			Where("project_id = ? AND is_default", e.ProjectID).
			Update("is_default", false).Error; err != nil {
			return err
		}
		return tx.Create(e).Error
	})
}

// GetWarehouse 按 ID 查仓库。
func (m *Model) GetWarehouse(ctx context.Context, id string) (e *WarehouseEntity, err error) {
	e = &WarehouseEntity{}
	err = m.DB(ctx).Where("id = ?", id).First(e).Error
	return e, err
}

// GetDefaultWarehouse 取某工程的默认仓（不存在返回 gorm.ErrRecordNotFound）。
func (m *Model) GetDefaultWarehouse(ctx context.Context, projectID string) (e *WarehouseEntity, err error) {
	e = &WarehouseEntity{}
	err = m.DB(ctx).Where("project_id = ? AND is_default", projectID).First(e).Error
	return e, err
}

// CodeExists 某工程内短码是否被占用（大小写不敏感，excludeID 为空表示新建场景）。
func (m *Model) CodeExists(ctx context.Context, projectID, code, excludeID string) (exists bool, err error) {
	q := m.DB(ctx).Where("project_id = ? AND upper(code) = upper(?)", projectID, code)
	if excludeID != "" {
		q = q.Where("id <> ?", excludeID)
	}
	var n int64
	if err = q.Count(&n).Error; err != nil {
		return false, err
	}
	return n > 0, nil
}

// ListWarehouses 某工程的仓库列表（默认仓在最前，其后按排序号与短码）。
func (m *Model) ListWarehouses(ctx context.Context, projectID string) (list []*WarehouseEntity, err error) {
	q := m.DB(ctx)
	if projectID != "" {
		q = q.Where("project_id = ?", projectID)
	}
	err = q.Order("is_default DESC, sort ASC, code ASC").Find(&list).Error
	return list, err
}

// UpdateWarehouse 更新仓库行（全字段保存）。
func (m *Model) UpdateWarehouse(ctx context.Context, e *WarehouseEntity) (err error) {
	return m.DB(ctx).Where("id = ?", e.ID).Save(e).Error
}

// SetDefaultWarehouse 把 id 设为该工程唯一默认仓（同一事务内清旧标记）。
func (m *Model) SetDefaultWarehouse(ctx context.Context, projectID, id string) (err error) {
	return m.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&WarehouseEntity{}).
			Where("project_id = ? AND is_default AND id <> ?", projectID, id).
			Update("is_default", false).Error; err != nil {
			return err
		}
		return tx.Model(&WarehouseEntity{}).Where("id = ?", id).Update("is_default", true).Error
	})
}

// DeleteWarehouse 删除仓库（库存行由外键 ON DELETE CASCADE 连带删除；
// 默认仓与有非零库存的仓由 service 先拒绝）。
func (m *Model) DeleteWarehouse(ctx context.Context, id string) (err error) {
	return m.DB(ctx).Where("id = ?", id).Delete(&WarehouseEntity{}).Error
}

// —— 库存记录 ——

// GetStock 按 ID 查库存记录。
func (m *Model) GetStock(ctx context.Context, id string) (e *StockEntity, err error) {
	e = &StockEntity{}
	err = m.StockDB(ctx).Where("id = ?", id).First(e).Error
	return e, err
}

// GetStockByVariantWarehouse 按「SKU × 仓库」定位库存记录（维度唯一键）。
func (m *Model) GetStockByVariantWarehouse(ctx context.Context, variantID, warehouseID string) (e *StockEntity, err error) {
	e = &StockEntity{}
	err = m.StockDB(ctx).Where("variant_id = ? AND warehouse_id = ?", variantID, warehouseID).First(e).Error
	return e, err
}

// EnsureStock 幂等地确保库存记录存在（初始 0），返回库中那一行。
//
// 「新建变体 → 归属仓生成库存记录」跨模块、无共享事务，重复调用（重试 / 补偿 /
// 后台按钮）必须落到同一行，故先读后写，并在唯一约束被并发命中时回读既有行 ——
// 绝不产生第二条同维度记录。
func (m *Model) EnsureStock(ctx context.Context, e *StockEntity) (out *StockEntity, err error) {
	existing, gerr := m.GetStockByVariantWarehouse(ctx, e.VariantID, e.WarehouseID)
	if gerr == nil {
		return existing, nil
	}
	if !errors.Is(gerr, gorm.ErrRecordNotFound) {
		return nil, gerr
	}
	if err = m.StockDB(ctx).Create(e).Error; err != nil {
		if again, rerr := m.GetStockByVariantWarehouse(ctx, e.VariantID, e.WarehouseID); rerr == nil {
			return again, nil
		}
		return nil, err
	}
	return e, nil
}

// CountNonZeroStocks 某仓下数量不为 0 的库存记录数（删仓前的守卫依据）。
func (m *Model) CountNonZeroStocks(ctx context.Context, warehouseID string) (n int64, err error) {
	err = m.StockDB(ctx).Where("warehouse_id = ? AND quantity <> 0", warehouseID).Count(&n).Error
	return n, err
}

// CountNonZeroStocksByVariant 某变体在各仓的非零库存行数（删变体前的守卫依据）。
func (m *Model) CountNonZeroStocksByVariant(ctx context.Context, variantID string) (n int64, err error) {
	err = m.StockDB(ctx).Where("variant_id = ? AND quantity <> 0", variantID).Count(&n).Error
	return n, err
}

// stockRows 库存行 + 仓库信息的只读投影查询（本模块两表 join 的唯一定义处）。
func (m *Model) stockRows(ctx context.Context) *gorm.DB {
	return m.db.WithContext(ctx).Table("inventory_stocks AS s").
		Select("s.id, s.project_id, s.warehouse_id, s.product_id, s.variant_id, s.sku_code, " +
			"s.quantity, s.created_at, s.updated_at, " +
			"w.code AS warehouse_code, w.name AS warehouse_name").
		Joins("JOIN inventory_warehouses AS w ON w.id = s.warehouse_id")
}

// applyStockFilter 把查询条件施加到投影查询上（条件以参数传入）。
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
	return q
}

// ListStockRows 库存行列表（按条件过滤 + 分页；limit <= 0 表示不限条数）。
//
// 排序固定「默认仓优先 → 仓库排序号 → 短码 → 变体」：同一 SKU 在各仓的库存
// 每次都以同样顺序返回（后台核对与快照对比都依赖这个确定性）。
func (m *Model) ListStockRows(ctx context.Context, f StockFilter, limit, offset int) (list []*StockRow, err error) {
	q := applyStockFilter(m.stockRows(ctx), f).Order("w.is_default DESC, w.sort ASC, w.code ASC, s.variant_id ASC")
	if limit > 0 {
		q = q.Limit(limit).Offset(offset)
	}
	err = q.Scan(&list).Error
	return list, err
}
