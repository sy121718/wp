// inventory_change_model.go — 库存流水 / 变动原因字典 / 缓存同步台账（issue #16）。
//
// 与 inventory_model.go 同属一个「表访问单元（Repository）」：这里只做本模块表的
// CRUD、聚合内原子组合与只读投影。业务规则（方向语义、原因方向必须匹配、
// 缓存同步时机）一律留在 service。
//
// 并发安全的两条约定（issue #16 验收 1/2）落在本文件：
//
//	· LockStockBySKUTx —— 对 (warehouse_id, sku_code) 那一行取 FOR UPDATE 行锁，
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
// ProjectID 为 nil 表示**内置原因**（迁移 103 seed，全工程可见，只读：不可改名，可停用）；
// 非 nil 表示工程自定义原因（工程内 code 唯一，可改名 / 停用）。
//
// Name 存的是 **i18n key**，不是文案（迁移 241 收口）：全站文案的唯一真源是 sys_i18n
// （内容 → 文案词条），库存域只管「有哪些原因 / 方向 / 启停」。内置原因的 key 形如
// inventory.reason.<code>；自定义原因的 key 由 service 派生（含工程 id，避免跨工程撞词条）
// 并在保存时同步写入 sys_i18n。
type ReasonEntity struct {
	ID        int64   `gorm:"column:id;primaryKey"`
	ProjectID *string `gorm:"column:project_id"`
	Code      string  `gorm:"column:code;not null"`
	// Name 是 i18n key（见结构体注释），不是给人读的文案。
	Name       string    `gorm:"column:name;not null"`
	Direction  string    `gorm:"column:direction;not null"`
	IsBuiltin  bool      `gorm:"column:is_builtin;not null"`
	Status     string    `gorm:"column:status;not null"`
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
	ID          string `gorm:"column:id;primaryKey"`
	ProjectID   string `gorm:"column:project_id;not null"`
	WarehouseID string `gorm:"column:warehouse_id;not null"`
	// ProductID / VariantID 是**线索列**（迁移 649 起可空）：货行的身份是「仓 × 仓库 SKU」，
	// 归属在 product_variants.warehouse_skus。无归属货行（台账页直接建的行）的流水
	// 这两列为 NULL —— 空串是 uuid 的非法输入，写进去会 22P02。
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
	// StockID 是这次变动动的**那一条货行**（迁移 640）。
	//
	// 为什么不能只靠 (variant_id, warehouse_id)：那对值不再唯一 —— 同一变体在同一个仓
	// 可以对应多条货行（不同批次 / 自有码与客户码并存，见 637 / 640 / 641 的注释）。
	// 可空：货行被删时外键置 NULL（历史流水不因货行消失而丢），
	// 也兼容 640 之前写入、回填时找不到对应货行的历史（极少）。
	StockID *string `gorm:"column:stock_id"`
	// StockName / StockImageURL 是**写入时刻**的货品名称与图片快照（迁移 646）。
	//
	// 为什么快照而不 join 货行：流水是审计。货行改名、换图、甚至被删（stock_id 会置
	// NULL）之后，历史流水仍要能回答「当时动的是哪件货、长什么样」—— 与 unit_cost
	// 同一个道理（迁移 256）。
	StockName     string `gorm:"column:stock_name;not null"`
	StockImageURL string `gorm:"column:stock_image_url;not null"`
	SourceType    string `gorm:"column:source_type;not null"`
	SourceRef     string `gorm:"column:source_ref;not null"`
	Remark        string `gorm:"column:remark;not null"`
	OperatorID    string `gorm:"column:operator_id;not null"`
	// UnitCost 是这次变动时刻的**成本留痕**（元，迁移 256）：出库 = 扣减时该库存行的
	// 当前成本；入库 = 本次显式成本（采购 / 生产单价），无显式成本时记库存行当前成本。
	// 可空：nil = 当时该 (仓库, SKU) 尚未核算（0 是合法的显式成本，两者不能混）。
	UnitCost  *float64  `gorm:"column:unit_cost"`
	BatchID   string    `gorm:"column:batch_id;not null"`
	CreatedAt time.Time `gorm:"column:create_time;not null"`
}

// TableName 实现 gorm 表名。
func (MovementEntity) TableName() string { return "inventory_stock_movements" }

// MovementRow 流水 + 仓库 + 原因展示信息的只读投影（本模块三张表 join 的唯一处）。
type MovementRow struct {
	ID            string `gorm:"column:id"`
	ProjectID     string `gorm:"column:project_id"`
	WarehouseID   string `gorm:"column:warehouse_id"`
	WarehouseCode string `gorm:"column:warehouse_code"`
	WarehouseName string `gorm:"column:warehouse_name"`
	// ProductID / VariantID 与 MovementEntity 同义（迁移 649 起可空、仅作线索）。
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
	// StockID 与 MovementEntity 同义（迁移 640：货行级追溯）。
	StockID *string `gorm:"column:stock_id"`
	// StockName / StockImageURL 与 MovementEntity 同义（迁移 646：变动时刻的快照）。
	StockName     string `gorm:"column:stock_name"`
	StockImageURL string `gorm:"column:stock_image_url"`
	SourceType    string `gorm:"column:source_type"`
	SourceRef     string `gorm:"column:source_ref"`
	Remark        string `gorm:"column:remark"`
	OperatorID    string `gorm:"column:operator_id"`
	// UnitCost 与 MovementEntity 同义（成本留痕，元，可空）。
	UnitCost  *float64  `gorm:"column:unit_cost"`
	BatchID   string    `gorm:"column:batch_id"`
	CreatedAt time.Time `gorm:"column:create_time"`
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
	// StockID 按货行过滤（迁移 640）：流水页「这条货的全部变动」就是它。
	StockID string
	// TimeFrom / TimeTo 是流水的创建时间区间（闭区间，nil 表示该侧不限）。
	// 时间列是 timestamptz，比较直接用 time.Time —— 后台表单传进来的本地时间由调用方
	// 按站点时区解析后再传，model 不猜时区。
	TimeFrom *time.Time
	TimeTo   *time.Time
}

// ReasonFilter 原因字典查询条件。
type ReasonFilter struct {
	// ProjectID 非空时返回「该工程的自定义原因 + 全部内置原因」。
	ProjectID       string
	Direction       string
	Keyword         string
	IncludeDisabled bool
}

// —— 事务与库存行（issue #16 验收 1/2）——

// Transaction 透传事务句柄：跨表的库存变动（真源行 + 流水）事务边界由 service 决定。
func (m *Model) Transaction(ctx context.Context, fn func(tx *gorm.DB) error) error {
	return m.db.WithContext(ctx).Transaction(fn)
}

// ListStocksByVariantsTx 取若干变体的库存行（**不加锁**，用于变动前的元数据解析）。
//
// 加锁是独立的第二步（LockStockBySKUTx），两步分开是为了让 service 能先把
// 「按标识排序」的锁顺序固定下来 —— 边查边锁会让并发批次的等待链成环。
//
// projectID 由调用方给，且这里**自己设作用域**（与 EnsureStocksTx / CreateMovementsTx 同款）：
// 它是 applyStockChanges 事务里的**第一条**语句，比 EnsureStocksTx 那次设变量更早 ——
// 缺它时读到 0 行（静默），展开扣减里「子项的商品 / SKU 快照由真源解析」的设计随之失效，
// 表现为子项 productID 为空、报 ErrStockProductRequired（deduct + expandBom 直接失败）。
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

// EnsureStocksTx 在给定事务内幂等地确保这些库存行存在（ON CONFLICT DO NOTHING）。
//
// 行已存在时本次传入的元数据被忽略（PostgreSQL 的 DO NOTHING 语义），
// 因此「目标 SKU 首次入库」才需要给全 product_id / sku_code 快照。
//
// 冲突目标是 (warehouse_id, sku_code)（uq_inventory_stocks_warehouse_sku，迁移 244）——
// 641 起它就是库存行的身份键：同一变体在同一仓可以有多条货行（自有码 / 客户码 / 不同批次），
// 而同一仓里绝不能有两条同码的货。
func (m *Model) EnsureStocksTx(ctx context.Context, tx *gorm.DB, rows []*StockEntity) (err error) {
	if len(rows) == 0 {
		return nil
	}
	// inventory_stocks 有策略：scope 设在调用方事务上（另开事务会脱离外层原子性）。
	if serr := rls.ScopeTx(tx, rows[0].ProjectID); serr != nil {
		return serr
	}
	// 与 EnsureStockTx 同一道兜底（见 normalizeStockTracking）：批量建行同样不允许
	// 「不跟踪却带数量」，否则矛盾状态会以 SQLSTATE 23514 的形态冒出去。
	for _, row := range rows {
		normalizeStockTracking(row)
	}
	return tx.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "warehouse_id"}, {Name: "sku_code"}},
		DoNothing: true,
	}).Create(&rows).Error
}

// LockStockBySKUTx 对「仓库 × 仓库 SKU」那一行取 FOR UPDATE 行锁（641 起的身份键）。
//
// 为什么按编码而不是按 id 加锁：
//   - 建行路径手里只有 (仓, 编码)（收货 / 建品时还没有货行 id），必须先锁住身份键，
//     否则两个并发的首次入库会各自插一行、在 UNIQUE (warehouse_id, sku_code) 上撞车；
//   - 归属仓解析给的是仓 + 裸码，按它定位不需要先按 id 查一次。
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

// LockStockByIDTx 按库存行 id 取 FOR UPDATE 行锁。
//
// 预占 / 释放全部以**库存行 id** 为锁对象：拿到候选行之后只锁 id 即可
// （比重新按 (变体, 仓) 定位少一次读取）。
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

// UpdateStockQuantityAndTrackingTx 在给定事务内写回某库存行的数量与**跟踪开关**。
//
// 两列一起写而不是分成两次 UPDATE：它们是同一件事的两种表达 ——
// 「给了一个具体数量」本身就意味着要跟踪（迁移 261 的 CHECK
// (track_quantity OR quantity = 0) 不允许「不跟踪却带数字」）。
// 分两次写会在中间态撞上那条 CHECK（先写数量时 track 还是 false），
// 也会让「数量已写、开关没跟上」成为一个真实可发生的中间态。
func (m *Model) UpdateStockQuantityAndTrackingTx(ctx context.Context, tx *gorm.DB, id string, quantity int, track bool, at time.Time) (err error) {
	return tx.WithContext(ctx).Model(&StockEntity{}).Where("id = ?", id).
		Updates(map[string]any{"quantity": quantity, "track_quantity": track, "update_time": at}).Error
}

// UpdateStockCostTx 在给定事务内写回某库存行的**当前成本价**（(仓库, SKU) 维度，覆盖式）。
//
// 与数量写回同一个事务：入库的货与它的成本一起生效，不留「货到了成本没写」的中间态。
// 只写一列：成本不做流水（迁移 244 的口径 —— 只记一个当前值），因此没有配套的历史表。
//
// 调用方保证 id 那行已经在本事务里加过行锁（applyStockChanges 的 ②）：成本与数量
// 落在同一行上，锁序不变，不会因为写成本引入新的加锁顺序。
func (m *Model) UpdateStockCostTx(ctx context.Context, tx *gorm.DB, id string, cost float64, at time.Time) (err error) {
	return tx.WithContext(ctx).Model(&StockEntity{}).Where("id = ?", id).
		Updates(map[string]any{"cost_price": cost, "update_time": at}).Error
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

// —— 流水查询（issue #16 验收 3）——

// movementRowsQuery 流水 + 仓库 + 原因名的只读投影查询（本模块三表 join 的唯一定义处）。
//
// 句柄由调用方给（同 stockRowsQuery）：作用域闭包里必须把查询建在同一个 tx 上。
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

// ExistsMovementBySource 是否已有指定来源引用的库存流水（采购/生产入库幂等重试用）。
func (m *Model) ExistsMovementBySource(ctx context.Context, projectID, sourceType, sourceRef string) (bool, error) {
	if projectID == "" || sourceType == "" || sourceRef == "" {
		return false, nil
	}
	var count int64
	// inventory_stock_movements 在 215 名单里（含分区子表）：没有作用域时这里恒 0 条，
	// 幂等判定会退化成「每次都当新单处理」——重复记账而不报错。
	err := rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {
		return tx.WithContext(ctx).Table("inventory_stock_movements").
			Where("project_id = ? AND source_type = ? AND source_ref = ?", projectID, sourceType, sourceRef).
			Limit(1).Count(&count).Error
	})
	return count > 0, err
}

// ListMovementRows 流水列表（按条件过滤 + 分页；limit <= 0 表示不限条数）。
//
// 排序固定「时间倒序 → id 倒序」：同一批次的流水顺序确定，便于后台核对与对比。
//
// RLS（迁移 215）：inventory_stock_movements（含分区子表）与 join 的仓库 / 原因表
// 都在名单里，作用域取自 f.ProjectID。缺作用域时这条投影**静默空集** ——
// 流水页显示「暂无数据」，与「这批确实没发生过变动」在界面上完全一样。
func (m *Model) ListMovementRows(ctx context.Context, f MovementFilter, limit, offset int) (list []*MovementRow, err error) {
	err = rls.InProjectScope(ctx, m.db, f.ProjectID, func(tx *gorm.DB) error {
		return m.scanMovementRows(ctx, movementRowsQuery(ctx, tx), f, limit, offset, &list)
	})
	return list, err
}

// applyMovementFilter 施加流水的全部过滤条件（**列表与计数唯一的一份**）。
//
// 为什么必须共用：过滤维度有十个（工程 / 仓 / 商品 / 变体 / SKU / 方向 / 原因 /
// 来源类型 / 来源引用 / 批次 + 时间区间），列表与计数各写一遍时**少写一维不会报错** ——
// 表现是「共 N 条」与实际能翻出来的条数对不上，且只在用了那一维筛选时才看得出来。
// 实测踩过：CountMovements 早先只抄了六个维度，漏掉商品 / 来源类型 / 来源引用 / 时间区间 ——
// 后台按时间筛流水时，分页条给的总数与列表条目直接互相矛盾。
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
	// 时间区间**闭区间**：调用方把「到某日」折算成当日 23:59:59 这类上界
	// （而不是在这里套 date_trunc —— 那会让索引失效，也会把时区问题藏进来）。
	if f.TimeFrom != nil {
		q = q.Where("mv.create_time >= ?", *f.TimeFrom)
	}
	if f.TimeTo != nil {
		q = q.Where("mv.create_time <= ?", *f.TimeTo)
	}
	return q
}

// scanMovementRows 在给定句柄上施加过滤 / 排序 / 分页并落库结果。
//
// 过滤条件与句柄分开传，是为了让 List 这类「先包作用域再施加条件」的路径
// 不必把十个 if 塞进闭包里。
func (m *Model) scanMovementRows(ctx context.Context, q *gorm.DB, f MovementFilter,
	limit, offset int, list *[]*MovementRow) error {
	q = applyMovementFilter(q, f).Order("mv.create_time DESC, mv.id DESC")
	if limit > 0 {
		q = q.Limit(limit).Offset(offset)
	}
	return q.Scan(list).Error
}

// CountMovements 流水条数（**与 ListMovementRows 同一份过滤条件**）。
//
// 走的是同一个投影查询与同一个 applyMovementFilter —— 计数与列表分叉在这里是不可能的：
// INNER JOIN 仓库（FK 保证每行都有仓）与 LEFT JOIN 原因都不会改变行数，所以 Count
// 与逐行取回的行集大小一致。RLS 作用域也取自同一个 f.ProjectID。
//
// RLS（迁移 215）：inventory_stock_movements 在名单里。缺作用域时计数**恒为 0**
// 且不报错 —— 分页总量与列表因此会同时退化成「暂无数据」，两边一致所以更难发现。
//
// 调用方是分页页面的「共 N 条」与总页数（inventory 契约的 CountMovements）。
func (m *Model) CountMovements(ctx context.Context, f MovementFilter) (n int64, err error) {
	err = rls.InProjectScope(ctx, m.db, f.ProjectID, func(tx *gorm.DB) error {
		return applyMovementFilter(movementRowsQuery(ctx, tx), f).Count(&n).Error
	})
	return n, err
}

// —— 变动原因字典（issue #16 验收 4）——

// reasonDB 原因字典表句柄（同上，仅本 model 内部使用）。
func (m *Model) reasonDB(ctx context.Context) *gorm.DB {
	return m.db.WithContext(ctx).Model(&ReasonEntity{})
}

// ListReasons 原因字典（工程自定义 + 内置；可按方向 / 关键字 / 是否含停用过滤）。
//
// 工程 id 非空时包作用域：inventory_change_reasons 用的是 global 谓词（额外放行
// project_id IS NULL 的内置行），不设作用域时非超级角色只看得到内置行 —— 工程自定义的原因
// 会从下拉里静默消失，且日志里没有任何线索。工程 id 为空是「只看内置」的既有语义，
// 此时保持裸句柄（内置行本就是全局行，与 CreateReason 的 NULL 分支同一口径）。
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

// GetReason 按 ID 查原因。
//
// inventory_change_reasons 用的是 global 谓词（放行 project_id IS NULL 的内置条目）：
// 设上作用域后「本工程自定义 + 内置」都可见，与 ListReasons 的过滤口径一致。
func (m *Model) GetReason(ctx context.Context, id int64, projectID string) (e *ReasonEntity, err error) {
	e = &ReasonEntity{}
	err = rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {
		return tx.WithContext(ctx).Model(&ReasonEntity{}).Where("id = ?", id).First(e).Error
	})
	return e, err
}

// FindReasonByCode 按 code 定位原因：工程自定义优先，其次内置。
//
// 只返回 active 的条目 —— 停用的原因不能再被新的变动引用（历史流水不受影响）。
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

// ReasonCodeExists 同工程（含内置）下 code 是否已被占用（excludeID 为空表示新建场景）。
//
// 与 ListReasons 同一口径：唯一性判定漏作用域时「同 code 的工程自定义原因」不可见，
// 于是重复创建被静默放行 —— 判定类查询比重建数据更难发现，故这里必须与读路径同源。
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

// —— 变体删除守卫：有没有用过（批次 C 的入口）——

// VariantHasStockMovement 该变体是否**有过任何库存流水**。
//
// 用途（docs/14 §8.2 的保存守卫，批次 C 的删除守卫）：订单一旦建单就一定会产生扣减
// 流水，因此「有流水」等价于「这个变体被订单用过」——变体的历史快照（订单行 /
// 采购行 / 流水本身）都是按 variant_id 追溯的，硬删变体会让这些追溯断链，
// 所以调用方（商品模块保存变体清单时的删除分支）据本方法判定**不允许硬删**。
//
// 与「非零库存」是两回事：卖出后补货清零的变体库存为 0 却仍被订单用过，
// 只看库存数量会误放行 —— 两个守卫都要。
//
// projectID 由调用方给出：inventory_stock_movements（含分区子表）在迁移 215 名单里，
// 缺作用域时计数**恒 0 且不报错** —— 表现为「所有变体都没被用过」，守卫静默失效。
// 这正是本方法必须要求工程作用域的原因。
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

// —— 缓存同步台账：已随迁移 121 删除 ——
//
// 这里曾有 CacheSyncEntity 与 UpsertCacheSync / ListCacheSyncs / GetCacheSync 三个方法。
// 迁移 121 删掉商品侧库存缓存时 DROP 了 inventory_stock_cache_syncs 表（它记录的是「缓存
// 同步结果」，缓存没了它也就没有意义），三个方法从此没有任何调用者，留着只会在换连接角色
// 之后变成三个「表不存在」的运行时炸弹。整个能力已移除，不留兼容壳。
