// inventory_change_model.go — 库存流水 / 变动原因字典 / 物料清单 / 缓存同步台账（issue #16）。
//
// 与 inventory_model.go 同属一个「表访问单元（Repository）」：这里只做本模块表的
// CRUD、聚合内原子组合与只读投影。业务规则（方向语义、原因方向必须匹配、清单成环、
// 缓存同步时机）一律留在 service。
//
// 并发安全的两条约定（issue #16 验收 1/2）落在本文件：
//
//	· LockStockRowTx —— 对 (variant_id, warehouse_id) 那一行取 FOR UPDATE 行锁，
//	  可用量的判定只发生在持锁之后，绝不读 product_variants.stock_total 缓存；
//	· EnsureStocksTx —— 目标行不存在时先幂等建行（ON CONFLICT DO NOTHING），
//	  并发的两批变动因此不会各自插出第二条同维度记录。
//
// 加锁顺序由 service 决定并以入参顺序传给本文件（model 不写死业务条件）。
package inventorymodel

import (
	"context"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"go_wp/pkg/rls"
)

// ReasonEntity 变动原因字典条目。
//
// ProjectID 为 nil 表示**内置原因**（迁移 103 seed，全工程可见，不可修改）；
// 非 nil 表示工程自定义原因（工程内 code 唯一，可改名 / 停用）。
type ReasonEntity struct {
	ID         int64     `gorm:"column:id;type:bigint;primaryKey"`
	ProjectID  *string   `gorm:"column:project_id;type:uuid"`
	Code       string    `gorm:"column:code;type:text;not null"`
	Name       string    `gorm:"column:name;type:text;not null"`
	Direction  string    `gorm:"column:direction;type:text;not null"`
	IsBuiltin  bool      `gorm:"column:is_builtin;not null"`
	Status     string    `gorm:"column:status;type:text;not null"`
	Sort       int       `gorm:"column:sort;not null"`
	CreateTime time.Time `gorm:"column:create_time;not null"`
	UpdatedAt  time.Time `gorm:"column:update_time;not null"`
}

// TableName 实现 gorm 表名。
func (ReasonEntity) TableName() string { return "inventory_change_reasons" }

// MovementEntity 库存流水（每次真源变动一行）。
//
// Quantity 是绝对变化量（恒 > 0），Delta 是带符号的实际变化量；
// ReasonCode 是快照列（原因字典条目被删后历史流水仍可读）。
type MovementEntity struct {
	ID              string    `gorm:"column:id;type:uuid;primaryKey"`
	ProjectID       string    `gorm:"column:project_id;type:uuid;not null"`
	WarehouseID     string    `gorm:"column:warehouse_id;type:uuid;not null"`
	ProductID       string    `gorm:"column:product_id;type:uuid;not null"`
	VariantID       string    `gorm:"column:variant_id;type:uuid;not null"`
	SKUCode         string    `gorm:"column:sku_code;type:text;not null"`
	Direction       string    `gorm:"column:direction;type:text;not null"`
	Quantity        int       `gorm:"column:quantity;not null"`
	Delta           int       `gorm:"column:delta;not null"`
	QuantityBefore  int       `gorm:"column:quantity_before;not null"`
	QuantityAfter   int       `gorm:"column:quantity_after;not null"`
	ReasonID        *int64    `gorm:"column:reason_id;type:bigint"`
	ReasonCode      string    `gorm:"column:reason_code;type:text;not null"`
	ParentVariantID *string   `gorm:"column:parent_variant_id;type:uuid"`
	SourceType      string    `gorm:"column:source_type;type:text;not null"`
	SourceRef       string    `gorm:"column:source_ref;type:text;not null"`
	Remark          string    `gorm:"column:remark;type:text;not null"`
	OperatorID      string    `gorm:"column:operator_id;type:text;not null"`
	BatchID         string    `gorm:"column:batch_id;type:uuid;not null"`
	CreatedAt       time.Time `gorm:"column:create_time;not null"`
}

// TableName 实现 gorm 表名。
func (MovementEntity) TableName() string { return "inventory_stock_movements" }

// MovementRow 流水 + 仓库 + 原因展示信息的只读投影（本模块三张表 join 的唯一处）。
type MovementRow struct {
	ID              string    `gorm:"column:id"`
	ProjectID       string    `gorm:"column:project_id"`
	WarehouseID     string    `gorm:"column:warehouse_id"`
	WarehouseCode   string    `gorm:"column:warehouse_code"`
	WarehouseName   string    `gorm:"column:warehouse_name"`
	ProductID       string    `gorm:"column:product_id"`
	VariantID       string    `gorm:"column:variant_id"`
	SKUCode         string    `gorm:"column:sku_code"`
	Direction       string    `gorm:"column:direction"`
	Quantity        int       `gorm:"column:quantity"`
	Delta           int       `gorm:"column:delta"`
	QuantityBefore  int       `gorm:"column:quantity_before"`
	QuantityAfter   int       `gorm:"column:quantity_after"`
	ReasonID        *int64    `gorm:"column:reason_id"`
	ReasonCode      string    `gorm:"column:reason_code"`
	ReasonName      string    `gorm:"column:reason_name"`
	ParentVariantID *string   `gorm:"column:parent_variant_id"`
	SourceType      string    `gorm:"column:source_type"`
	SourceRef       string    `gorm:"column:source_ref"`
	Remark          string    `gorm:"column:remark"`
	OperatorID      string    `gorm:"column:operator_id"`
	BatchID         string    `gorm:"column:batch_id"`
	CreatedAt       time.Time `gorm:"column:create_time"`
}

// MovementFilter 流水查询条件（条件以参数传入，方法内不写死业务判断）。
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
}

// ReasonFilter 原因字典查询条件。
type ReasonFilter struct {
	// ProjectID 非空时返回「该工程的自定义原因 + 全部内置原因」。
	ProjectID       string
	Direction       string
	Keyword         string
	IncludeDisabled bool
}

// BOMItemEntity 物料清单的一条子项（父 SKU → 子项 SKU × 用量）。
type BOMItemEntity struct {
	ID                 string    `gorm:"column:id;type:uuid;primaryKey"`
	ProjectID          string    `gorm:"column:project_id;type:uuid;not null"`
	ParentVariantID    string    `gorm:"column:parent_variant_id;type:uuid;not null"`
	ParentSKUCode      string    `gorm:"column:parent_sku_code;type:text;not null"`
	ComponentVariantID string    `gorm:"column:component_variant_id;type:uuid;not null"`
	ComponentSKUCode   string    `gorm:"column:component_sku_code;type:text;not null"`
	Quantity           int       `gorm:"column:quantity;not null"`
	CreatedAt          time.Time `gorm:"column:create_time;not null"`
	UpdatedAt          time.Time `gorm:"column:update_time;not null"`
}

// TableName 实现 gorm 表名。
func (BOMItemEntity) TableName() string { return "inventory_bom_items" }

// CacheSyncEntity 商品侧库存缓存同步台账（每个变体一行：最近一次同步状态）。
type CacheSyncEntity struct {
	ID          string    `gorm:"column:id;type:uuid;primaryKey"`
	ProjectID   string    `gorm:"column:project_id;type:uuid;not null"`
	VariantID   string    `gorm:"column:variant_id;type:uuid;not null"`
	SKUCode     string    `gorm:"column:sku_code;type:text;not null"`
	TrueTotal   int       `gorm:"column:true_total;not null"`
	CachedTotal int       `gorm:"column:cached_total;not null"`
	Status      string    `gorm:"column:status;type:text;not null"`
	Error       string    `gorm:"column:error;type:text;not null"`
	SyncedAt    time.Time `gorm:"column:synced_at;not null"`
	UpdatedAt   time.Time `gorm:"column:update_time;not null"`
}

// TableName 实现 gorm 表名。
func (CacheSyncEntity) TableName() string { return "inventory_stock_cache_syncs" }

// StockTotalRow 某个变体的真源汇总（跨仓求和）+ SKU 快照。
type StockTotalRow struct {
	VariantID string `gorm:"column:variant_id"`
	SKUCode   string `gorm:"column:sku_code"`
	Total     int    `gorm:"column:total"`
}

// —— 事务与库存行（issue #16 验收 1/2）——

// Transaction 透传事务句柄：跨表的库存变动（真源行 + 流水）事务边界由 service 决定。
func (m *Model) Transaction(ctx context.Context, fn func(tx *gorm.DB) error) error {
	return m.db.WithContext(ctx).Transaction(fn)
}

// ListStocksByVariantsTx 取若干变体的库存行（**不加锁**，用于变动前的元数据解析）。
//
// 加锁是独立的第二步（LockStockRowTx），两步分开是为了让 service 能先把
// 「按标识排序」的锁顺序固定下来 —— 边查边锁会让并发批次的等待链成环。
func (m *Model) ListStocksByVariantsTx(ctx context.Context, tx *gorm.DB, variantIDs []string) (list []*StockEntity, err error) {
	if len(variantIDs) == 0 {
		return nil, nil
	}
	err = tx.WithContext(ctx).Model(&StockEntity{}).Where("variant_id IN ?", variantIDs).Find(&list).Error
	return list, err
}

// EnsureStocksTx 在给定事务内幂等地确保这些库存行存在（ON CONFLICT DO NOTHING）。
//
// 行已存在时本次传入的元数据被忽略（PostgreSQL 的 DO NOTHING 语义），
// 因此「目标 SKU 首次入库」才需要给全 product_id / sku_code 快照。
func (m *Model) EnsureStocksTx(ctx context.Context, tx *gorm.DB, rows []*StockEntity) (err error) {
	if len(rows) == 0 {
		return nil
	}
	// inventory_stocks 有策略：scope 设在调用方事务上（另开事务会脱离外层原子性）。
	if serr := rls.ScopeTx(tx, rows[0].ProjectID); serr != nil {
		return serr
	}
	return tx.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "variant_id"}, {Name: "warehouse_id"}},
		DoNothing: true,
	}).Create(&rows).Error
}

// LockStockRowTx 对 (variant_id, warehouse_id) 那一行取 FOR UPDATE 行锁，返回加锁后的行。
//
// 可用量的判定只发生在持锁之后：并行扣减因此被串行化，超扣不可能发生。
func (m *Model) LockStockRowTx(ctx context.Context, tx *gorm.DB, variantID, warehouseID string) (e *StockEntity, err error) {
	e = &StockEntity{}
	err = tx.WithContext(ctx).Model(&StockEntity{}).
		Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("variant_id = ? AND warehouse_id = ?", variantID, warehouseID).
		First(e).Error
	return e, err
}

// UpdateStockQuantityTx 在给定事务内写回某库存行的数量。
func (m *Model) UpdateStockQuantityTx(ctx context.Context, tx *gorm.DB, id string, quantity int, at time.Time) (err error) {
	return tx.WithContext(ctx).Model(&StockEntity{}).Where("id = ?", id).
		Updates(map[string]any{"quantity": quantity, "update_time": at}).Error
}

// CreateMovementsTx 在给定事务内批量写流水（与数量写回同一事务：有变动必有流水）。
func (m *Model) CreateMovementsTx(ctx context.Context, tx *gorm.DB, rows []*MovementEntity) (err error) {
	if len(rows) == 0 {
		return nil
	}
	// inventory_stock_movements 是**分区表**：策略装在父表上（分区单独装，见迁移 215 与
	// internal/partition.EnsureAhead）。写入走父表路由，scope 设在调用方事务上。
	if serr := rls.ScopeTx(tx, rows[0].ProjectID); serr != nil {
		return serr
	}
	return tx.WithContext(ctx).CreateInBatches(&rows, 100).Error
}

// StockTotals 按变体汇总真源数量（跨仓求和）+ SKU 快照。
//
// projectID 为空表示不限工程；variantIDs 为空表示该范围内的全部变体。
// 这是「商品侧缓存该被同步成什么值」的**唯一依据** —— 汇总读的是真源，不是缓存。
func (m *Model) StockTotals(ctx context.Context, projectID string, variantIDs []string) (list []*StockTotalRow, err error) {
	q := m.StockDB(ctx).
		Select("variant_id, MAX(sku_code) AS sku_code, SUM(quantity) AS total").
		Group("variant_id").Order("variant_id ASC")
	if projectID != "" {
		q = q.Where("project_id = ?", projectID)
	}
	if len(variantIDs) > 0 {
		q = q.Where("variant_id IN ?", variantIDs)
	}
	err = q.Scan(&list).Error
	return list, err
}

// —— 流水查询（issue #16 验收 3）——

// movementRows 流水 + 仓库 + 原因名的只读投影查询（本模块三表 join 的唯一定义处）。
func (m *Model) movementRows(ctx context.Context) *gorm.DB {
	return m.db.WithContext(ctx).Table("inventory_stock_movements AS mv").
		Select("mv.id, mv.project_id, mv.warehouse_id, mv.product_id, mv.variant_id, mv.sku_code, " +
			"mv.direction, mv.quantity, mv.delta, mv.quantity_before, mv.quantity_after, " +
			"mv.reason_id, mv.reason_code, mv.parent_variant_id, mv.source_type, mv.source_ref, " +
			"mv.remark, mv.operator_id, mv.batch_id, mv.create_time, " +
			"w.code AS warehouse_code, w.name AS warehouse_name, " +
			"COALESCE(r.name, '') AS reason_name").
		Joins("JOIN inventory_warehouses AS w ON w.id = mv.warehouse_id").
		Joins("LEFT JOIN inventory_change_reasons AS r ON r.id = mv.reason_id")
}

// ExistsMovementBySource 是否已有指定来源引用的库存流水（采购/生产入库幂等重试用）。
func (m *Model) ExistsMovementBySource(ctx context.Context, projectID, sourceType, sourceRef string) (bool, error) {
	if projectID == "" || sourceType == "" || sourceRef == "" {
		return false, nil
	}
	var count int64
	err := m.DB(ctx).Table("inventory_stock_movements").
		Where("project_id = ? AND source_type = ? AND source_ref = ?", projectID, sourceType, sourceRef).
		Limit(1).Count(&count).Error
	return count > 0, err
}

// ListMovementRows 流水列表（按条件过滤 + 分页；limit <= 0 表示不限条数）。
//
// 排序固定「时间倒序 → id 倒序」：同一批次的流水顺序确定，便于后台核对与对比。
func (m *Model) ListMovementRows(ctx context.Context, f MovementFilter, limit, offset int) (list []*MovementRow, err error) {
	q := m.movementRows(ctx)
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
	q = q.Order("mv.create_time DESC, mv.id DESC")
	if limit > 0 {
		q = q.Limit(limit).Offset(offset)
	}
	err = q.Scan(&list).Error
	return list, err
}

// CountMovements 流水条数（与 List 同过滤条件）。
func (m *Model) CountMovements(ctx context.Context, f MovementFilter) (n int64, err error) {
	q := m.db.WithContext(ctx).Table("inventory_stock_movements")
	if f.ProjectID != "" {
		q = q.Where("project_id = ?", f.ProjectID)
	}
	if f.WarehouseID != "" {
		q = q.Where("warehouse_id = ?", f.WarehouseID)
	}
	if f.VariantID != "" {
		q = q.Where("variant_id = ?", f.VariantID)
	}
	if f.SKUCode != "" {
		q = q.Where("sku_code = ?", f.SKUCode)
	}
	if f.Direction != "" {
		q = q.Where("direction = ?", f.Direction)
	}
	if f.ReasonCode != "" {
		q = q.Where("reason_code = ?", f.ReasonCode)
	}
	if f.BatchID != "" {
		q = q.Where("batch_id = ?", f.BatchID)
	}
	err = q.Count(&n).Error
	return n, err
}

// —— 变动原因字典（issue #16 验收 4）——

// reasonDB 原因字典表句柄（同上，仅本 model 内部使用）。
func (m *Model) reasonDB(ctx context.Context) *gorm.DB {
	return m.db.WithContext(ctx).Model(&ReasonEntity{})
}

// ListReasons 原因字典（工程自定义 + 内置；可按方向 / 关键字 / 是否含停用过滤）。
func (m *Model) ListReasons(ctx context.Context, f ReasonFilter) (list []*ReasonEntity, err error) {
	q := m.reasonDB(ctx)
	if f.ProjectID != "" {
		q = q.Where("project_id = ? OR project_id IS NULL", f.ProjectID)
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
	err = q.Order("is_builtin DESC, direction ASC, sort ASC, code ASC").Find(&list).Error
	return list, err
}

// GetReason 按 ID 查原因。
func (m *Model) GetReason(ctx context.Context, id int64) (e *ReasonEntity, err error) {
	e = &ReasonEntity{}
	err = m.reasonDB(ctx).Where("id = ?", id).First(e).Error
	return e, err
}

// FindReasonByCode 按 code 定位原因：工程自定义优先，其次内置。
//
// 只返回 active 的条目 —— 停用的原因不能再被新的变动引用（历史流水不受影响）。
func (m *Model) FindReasonByCode(ctx context.Context, projectID, code string) (e *ReasonEntity, err error) {
	e = &ReasonEntity{}
	q := m.reasonDB(ctx).Where("lower(code) = lower(?) AND status = 'active'", code)
	if projectID != "" {
		q = q.Where("project_id = ? OR project_id IS NULL", projectID)
	} else {
		q = q.Where("project_id IS NULL")
	}
	err = q.Order("project_id NULLS LAST, is_builtin ASC").First(e).Error
	return e, err
}

// ReasonCodeExists 同工程（含内置）下 code 是否已被占用（excludeID 为空表示新建场景）。
func (m *Model) ReasonCodeExists(ctx context.Context, projectID, code string, excludeID int64) (exists bool, err error) {
	q := m.reasonDB(ctx).Where("lower(code) = lower(?)", code)
	if projectID != "" {
		q = q.Where("project_id = ? OR project_id IS NULL", projectID)
	} else {
		q = q.Where("project_id IS NULL")
	}
	if excludeID != 0 {
		q = q.Where("id <> ?", excludeID)
	}
	var n int64
	if err = q.Count(&n).Error; err != nil {
		return false, err
	}
	return n > 0, nil
}

// CreateReason 写入自定义原因。
func (m *Model) CreateReason(ctx context.Context, e *ReasonEntity) (err error) {
	// project_id 为 NULL 的是**全局内置原因**，215 的策略对这类行有 "project_id IS NULL"
	// 放行分支，不需要（也不该）设工程作用域；自带工程的行则必须设。
	if e.ProjectID == nil || strings.TrimSpace(*e.ProjectID) == "" {
		return m.reasonDB(ctx).Create(e).Error
	}
	return rls.InProjectScope(ctx, m.db, *e.ProjectID, func(tx *gorm.DB) error {
		return tx.Model(&ReasonEntity{}).Create(e).Error
	})
}

// UpdateReason 更新原因行（全字段保存）。
func (m *Model) UpdateReason(ctx context.Context, e *ReasonEntity) (err error) {
	if e.ProjectID == nil || strings.TrimSpace(*e.ProjectID) == "" {
		return m.reasonDB(ctx).Where("id = ?", e.ID).Save(e).Error
	}
	return rls.InProjectScope(ctx, m.db, *e.ProjectID, func(tx *gorm.DB) error {
		return tx.Model(&ReasonEntity{}).Where("id = ?", e.ID).Save(e).Error
	})
}

// —— 物料清单（issue #16 验收 5）——

// bomDB 物料清单表句柄（同上，仅本 model 内部使用）。
func (m *Model) bomDB(ctx context.Context) *gorm.DB {
	return m.db.WithContext(ctx).Model(&BOMItemEntity{})
}

// ListBOMItems 某个父 SKU 的清单子项（按子项变体 id 升序：展开顺序确定）。
func (m *Model) ListBOMItems(ctx context.Context, parentVariantID string) (list []*BOMItemEntity, err error) {
	err = m.bomDB(ctx).Where("parent_variant_id = ?", parentVariantID).
		Order("component_variant_id ASC").Find(&list).Error
	return list, err
}

// ListBOMItemsByParents 批量取多个父 SKU 的清单子项（展开时不产生 N+1）。
func (m *Model) ListBOMItemsByParents(ctx context.Context, parentVariantIDs []string) (list []*BOMItemEntity, err error) {
	if len(parentVariantIDs) == 0 {
		return nil, nil
	}
	err = m.bomDB(ctx).Where("parent_variant_id IN ?", parentVariantIDs).
		Order("parent_variant_id ASC, component_variant_id ASC").Find(&list).Error
	return list, err
}

// ListBOMParents 某子项出现在哪些父 SKU 的清单里（维护入口的成环检测用）。
func (m *Model) ListBOMParents(ctx context.Context, componentVariantID string) (list []*BOMItemEntity, err error) {
	err = m.bomDB(ctx).Where("component_variant_id = ?", componentVariantID).Find(&list).Error
	return list, err
}

// ReplaceBOM 在同一事务内全量替换某父 SKU 的清单（先删后写，聚合内原子组合）。
func (m *Model) ReplaceBOM(ctx context.Context, parentVariantID string, rows []*BOMItemEntity) (err error) {
	// rows 为空时拿不到工程 id（签名里没有）：此时不带作用域 —— 换非超级角色后
	// 「清空 BOM」这条 DELETE 会静默匹配 0 行（表现是清空不生效），已列入 DB-009 剩余清单。
	if len(rows) == 0 {
		return m.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			return tx.WithContext(ctx).Model(&BOMItemEntity{}).
				Where("parent_variant_id = ?", parentVariantID).Delete(&BOMItemEntity{}).Error
		})
	}
	return rls.InProjectScope(ctx, m.db, rows[0].ProjectID, func(tx *gorm.DB) error {
		if err := tx.WithContext(ctx).Model(&BOMItemEntity{}).
			Where("parent_variant_id = ?", parentVariantID).Delete(&BOMItemEntity{}).Error; err != nil {
			return err
		}
		if len(rows) == 0 {
			return nil
		}
		return tx.WithContext(ctx).CreateInBatches(&rows, 100).Error
	})
}

// —— 缓存同步台账（issue #16 验收 6）——

// cacheSyncDB 缓存同步台账表句柄（同上，仅本 model 内部使用）。
func (m *Model) cacheSyncDB(ctx context.Context) *gorm.DB {
	return m.db.WithContext(ctx).Model(&CacheSyncEntity{})
}

// UpsertCacheSync 记下某个变体最近一次缓存同步的结果（每个变体一行）。
func (m *Model) UpsertCacheSync(ctx context.Context, e *CacheSyncEntity) (err error) {
	return m.cacheSyncDB(ctx).Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "variant_id"}},
		DoUpdates: clause.AssignmentColumns([]string{
			"project_id", "sku_code", "true_total", "cached_total", "status", "error", "synced_at", "update_time",
		}),
	}).Create(e).Error
}

// ListCacheSyncs 台账列表（按工程 + 可选变体过滤；默认按同步时间倒序）。
func (m *Model) ListCacheSyncs(ctx context.Context, projectID string, variantIDs []string) (list []*CacheSyncEntity, err error) {
	q := m.cacheSyncDB(ctx)
	if projectID != "" {
		q = q.Where("project_id = ?", projectID)
	}
	if len(variantIDs) > 0 {
		q = q.Where("variant_id IN ?", variantIDs)
	}
	err = q.Order("synced_at DESC, variant_id ASC").Find(&list).Error
	return list, err
}

// GetCacheSync 某个变体的台账行（不存在返回 gorm.ErrRecordNotFound）。
func (m *Model) GetCacheSync(ctx context.Context, variantID string) (e *CacheSyncEntity, err error) {
	e = &CacheSyncEntity{}
	err = m.cacheSyncDB(ctx).Where("variant_id = ?", variantID).First(e).Error
	return e, err
}
