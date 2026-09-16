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

	"go_wp/pkg/rls"
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
	CreatedAt time.Time       `gorm:"column:create_time;not null"`
	UpdatedAt time.Time       `gorm:"column:update_time;not null"`
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
	CreatedAt   time.Time       `gorm:"column:create_time;not null"`
	UpdatedAt   time.Time       `gorm:"column:update_time;not null"`
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
	CreatedAt     time.Time `gorm:"column:create_time"`
	UpdatedAt     time.Time `gorm:"column:update_time"`
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
	// RLS（迁移 215）：inventory_warehouses 已启用 FORCE 策略，两个分支都承 e.ProjectID
	// 的工程作用域（清旧默认标记的那条 UPDATE 同样受策略约束，缺 scope 会静默匹配 0 行，
	// 表现为「设了新默认仓但旧仓还是默认」）。
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

// GetWarehouse 按 ID 查仓库。
//
// projectID 由调用方给出：inventory_warehouses 在迁移 215 名单里，跨工程的行不可见。
func (m *Model) GetWarehouse(ctx context.Context, id, projectID string) (e *WarehouseEntity, err error) {
	e = &WarehouseEntity{}
	err = rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {
		return tx.WithContext(ctx).Model(&WarehouseEntity{}).Where("id = ?", id).First(e).Error
	})
	return e, err
}

// GetDefaultWarehouse 取某工程的默认仓（不存在返回 gorm.ErrRecordNotFound）。
func (m *Model) GetDefaultWarehouse(ctx context.Context, projectID string) (e *WarehouseEntity, err error) {
	e = &WarehouseEntity{}
	err = rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {
		return tx.WithContext(ctx).Model(&WarehouseEntity{}).
			Where("project_id = ? AND is_default", projectID).First(e).Error
	})
	return e, err
}

// CodeExists 某工程内短码是否被占用（大小写不敏感，excludeID 为空表示新建场景）。
func (m *Model) CodeExists(ctx context.Context, projectID, code, excludeID string) (exists bool, err error) {
	// 唯一性判定要作用域：缺 scope 时恒「不存在」⇒ 重复创建被静默放行（DB-009）。
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

// ListWarehouses 某工程的仓库列表（默认仓在最前，其后按排序号与短码）。
func (m *Model) ListWarehouses(ctx context.Context, projectID string) (list []*WarehouseEntity, err error) {
	err = rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {
		return tx.WithContext(ctx).Model(&WarehouseEntity{}).
			Order("is_default DESC, sort ASC, code ASC").Find(&list).Error
	})
	return list, err
}

// UpdateWarehouse 更新仓库行（全字段保存）。
func (m *Model) UpdateWarehouse(ctx context.Context, e *WarehouseEntity) (err error) {
	return rls.InProjectScope(ctx, m.db, e.ProjectID, func(tx *gorm.DB) error {
		return tx.Model(&WarehouseEntity{}).Where("id = ?", e.ID).Save(e).Error
	})
}

// SetDefaultWarehouse 把 id 设为该工程唯一默认仓（同一事务内清旧标记）。
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

// DeleteWarehouse 删除仓库（库存行由外键 ON DELETE CASCADE 连带删除；
// 默认仓与有非零库存的仓由 service 先拒绝）。
//
// projectID 由调用方给出：inventory_warehouses 在迁移 215 名单里，缺作用域时 DELETE
// 静默匹配 0 行 —— 前置的归属校验（GetWarehouse + 非零库存守卫）全都过了，唯独真删
// 不动，表现是「删除按钮点了没反应也不报错」。
func (m *Model) DeleteWarehouse(ctx context.Context, id, projectID string) (err error) {
	return rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {
		return tx.WithContext(ctx).Model(&WarehouseEntity{}).
			Where("id = ?", id).Delete(&WarehouseEntity{}).Error
	})
}

// —— 库存记录 ——

// GetStock 按 ID 查库存记录。
//
// projectID 由调用方给出：inventory_stocks 在迁移 215 名单里，跨工程的行不可见。
func (m *Model) GetStock(ctx context.Context, id, projectID string) (e *StockEntity, err error) {
	e = &StockEntity{}
	err = rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {
		return tx.WithContext(ctx).Model(&StockEntity{}).Where("id = ?", id).First(e).Error
	})
	return e, err
}

// GetStockByVariantWarehouse 按「SKU × 仓库」定位库存记录（维度唯一键）。
//
// projectID 由调用方给出：inventory_stocks 在迁移 215 名单里，无作用域时定位恒
// ErrRecordNotFound（与 EnsureStock 里那条注释同一个坑）。
func (m *Model) GetStockByVariantWarehouse(ctx context.Context, variantID, warehouseID, projectID string) (e *StockEntity, err error) {
	e = &StockEntity{}
	err = rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {
		return tx.WithContext(ctx).Model(&StockEntity{}).
			Where("variant_id = ? AND warehouse_id = ?", variantID, warehouseID).First(e).Error
	})
	return e, err
}

// EnsureStock 幂等地确保库存记录存在（初始 0），返回库中那一行。
//
// 「新建变体 → 归属仓生成库存记录」跨模块、无共享事务，重复调用（重试 / 补偿 /
// 后台按钮）必须落到同一行，故先读后写，并在唯一约束被并发命中时回读既有行 ——
// 绝不产生第二条同维度记录。
func (m *Model) EnsureStock(ctx context.Context, e *StockEntity) (out *StockEntity, err error) {
	// inventory_stocks 有策略：定位与创建都必须在工程作用域内。缺 scope 时定位恒
	// ErrRecordNotFound，每次调用都去 Create 并撞 (variant_id, warehouse_id) 唯一键 ——
	// 表现为「重试偶尔能过」，实际是每次都在撞。
	existing, gerr := m.getStockByVariantWarehouseScoped(ctx, e.ProjectID, e.VariantID, e.WarehouseID)
	if gerr == nil {
		return existing, nil
	}
	if !errors.Is(gerr, gorm.ErrRecordNotFound) {
		return nil, gerr
	}
	// 建失败（并发命中唯一键）时的回读**不并进同一事务**：PG 里一句出错就把事务标记为
	// aborted，同事务内的后续 SELECT 会直接报 current transaction is aborted。
	if err = rls.InProjectScope(ctx, m.db, e.ProjectID, func(tx *gorm.DB) error {
		return tx.Model(&StockEntity{}).Create(e).Error
	}); err != nil {
		if again, rerr := m.getStockByVariantWarehouseScoped(ctx, e.ProjectID, e.VariantID, e.WarehouseID); rerr == nil {
			return again, nil
		}
		return nil, err
	}
	return e, nil
}

// getStockByVariantWarehouseScoped 带工程作用域的「SKU × 仓库」定位。
func (m *Model) getStockByVariantWarehouseScoped(ctx context.Context, projectID, variantID, warehouseID string) (e *StockEntity, err error) {
	e = &StockEntity{}
	err = rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {
		return tx.Model(&StockEntity{}).Where("variant_id = ? AND warehouse_id = ?", variantID, warehouseID).First(e).Error
	})
	return e, err
}

// CountNonZeroStocks 某仓下数量不为 0 的库存记录数（删仓前的守卫依据）。
//
// projectID 由调用方给出：inventory_stocks 在迁移 215 名单里。这条守卫是**反向**失效的
// —— 缺作用域时数出 0，于是「还有货的仓」被判定成可以删，DELETE 会把整仓库存连同
// 库存行一起清掉（外键级联）。这就是静默丢账，不是「查得慢一点」。
func (m *Model) CountNonZeroStocks(ctx context.Context, warehouseID, projectID string) (n int64, err error) {
	err = rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {
		return tx.WithContext(ctx).Model(&StockEntity{}).
			Where("warehouse_id = ? AND quantity <> 0", warehouseID).Count(&n).Error
	})
	return n, err
}

// CountNonZeroStocksByVariant 某变体在各仓的非零库存行数（删变体前的守卫依据）。
//
// projectID 由调用方给出：inventory_stocks 在迁移 215 名单里。与 CountNonZeroStocks
// 同一条反向失效路径 —— 缺作用域数出 0 ⇒ 有货的 SKU 被判成可删。
func (m *Model) CountNonZeroStocksByVariant(ctx context.Context, variantID, projectID string) (n int64, err error) {
	err = rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {
		return tx.WithContext(ctx).Model(&StockEntity{}).
			Where("variant_id = ? AND quantity <> 0", variantID).Count(&n).Error
	})
	return n, err
}

// stockRowsQuery 库存行 + 仓库信息的只读投影查询（本模块两表 join 的唯一定义处）。
//
// 句柄由调用方给：列表路径的作用域闭包必须把查询建在**同一个 tx** 上 ——
// 改用 m.db 会另取一条连接、脱离事务，策略谓词读到的 app.project_id 恒为 NULL，
// 列表静默空集（这正是 DB-009 要消灭的形态）。
func stockRowsQuery(ctx context.Context, db *gorm.DB) *gorm.DB {
	return db.WithContext(ctx).Table("inventory_stocks AS s").
		Select("s.id, s.project_id, s.warehouse_id, s.product_id, s.variant_id, s.sku_code, " +
			"s.quantity, s.create_time, s.update_time, " +
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
//
// RLS（迁移 215）：inventory_stocks 与 join 的 inventory_warehouses 都在名单里，
// 作用域取自 f.ProjectID —— 列表**必须**带工程，缺它时这里直接返回
// rls.ErrInvalidProjectID，不退化成「不限工程」。后者换非超级角色后是**静默空集**：
// 列表页显示「暂无数据」，既不报错也没有日志，排障时会一路查到业务逻辑上去。
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
