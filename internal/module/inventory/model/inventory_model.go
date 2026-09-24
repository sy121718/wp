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
	"gorm.io/gorm/clause"

	"go_wp/pkg/rls"
)

// WarehouseEntity 仓库实体。
//
// Code 是短码：工程内唯一（DDL 侧 upper(code) 唯一索引），也是 SKU 编码的前缀
// （{仓短码}_{商品码}_{序号}）—— 它表达的是「默认发货仓」，不是「这个 SKU 只属于这个仓」，
// SKU 本身是全局的，货可以在多个仓分布。
// IsDefault 标记默认仓：每工程至多一行（DDL 侧部分唯一索引），是「未指定仓库」时的兜底。
// Type 是仓库类型（self / third_party / virtual，DDL 侧有 check 约束）。
//
// 它不参与归属仓解析 —— 解析仍然只看 is_default 与 status（见 service.resolveWarehouse）：
// 类型表达的是「这个仓在业务上是什么」，不是「这个 SKU 归谁」，混在一起会让
// 「未指定仓库」的兜底规则变得不可预测。
type WarehouseEntity struct {
	ID        string `gorm:"column:id;primaryKey"`
	ProjectID string `gorm:"column:project_id;not null"`
	Code      string `gorm:"column:code;not null"`
	Name      string `gorm:"column:name;not null"`
	Type      string `gorm:"column:type;not null"`
	Status    string `gorm:"column:status;not null"`
	IsDefault bool   `gorm:"column:is_default;not null"`
	Sort      int    `gorm:"column:sort;not null"`
	// Config 是第三方仓的对接配置（jsonb）：非敏感项明文，凭据只存密文或引用名。
	//
	// json.RawMessage 保留 type:jsonb —— 这是「Go 值怎么变成 SQL 值」的映射声明，
	// 不是列类型真相（见 AGENTS.md「model 一律不声明列型」的唯一例外）。
	Config    json.RawMessage `gorm:"column:config;type:jsonb;not null"`
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
	ID          string `gorm:"column:id;primaryKey"`
	ProjectID   string `gorm:"column:project_id;not null"`
	WarehouseID string `gorm:"column:warehouse_id;not null"`
	ProductID   string `gorm:"column:product_id;not null"`
	VariantID   string `gorm:"column:variant_id;not null"`
	SKUCode     string `gorm:"column:sku_code;not null"`
	// ExternalSKU 是这条库存**在该仓**的外部 / 第三方编码（迁移 251，docs/14 §9.3）。
	//
	// 空串 = 该仓用我们自己的 SKU（自营仓的常态）；非空 = 第三方仓 / 平台仓的编码
	//（对方的编码我们改不了，只能映射）。映射是 **N:1**：同一个商品的多个变体
	//（十几口味）在仓库侧可能共用同一个外码，所以这一列**没有**唯一约束 ——
	// 违反它会让合法数据被判成冲突；跨商品的冲突由 service 弱校验拦住
	//（同一仓内同一外码必须指向同一个 product_id）。
	ExternalSKU string `gorm:"column:external_sku;not null"`
	// TrackQuantity 是否跟踪数量（迁移 261）：false = **无限**（不跟踪）——
	// 扣减不校验可用量、也不扣减（订单照卖）；true = 按 Quantity 跟踪。
	//
	// 「无限」用显式开关表达而不是可空数量：quantity 恒为 NOT NULL，
	// 由 DDL 的 CHECK (track_quantity OR quantity = 0) 兜底 —— 不跟踪的行不允许带数字。
	// 于是 quantity = 0 有**两义**（跟踪且卖光 / 不跟踪无限），区分它们的唯一依据就是
	// 这一列，所以每个读模型都必须把它一起带出去，不能只投影一个数量。
	//
	// 存量行一律 true（迁移 261 的保守口径：存量那些 0 分不清是占位还是卖光，
	// 把卖光的行判成无限会直接导致超卖）；新建行默认 false，由 service 显式赋值；
	// 直接走 model 而「给了数量却忘了翻开关」的调用点由 normalizeStockTracking 兜住。
	TrackQuantity bool `gorm:"column:track_quantity;not null"`
	Quantity      int  `gorm:"column:quantity;not null"`
	// CostPrice 是这一 (仓库, SKU) 的**当前成本价**（迁移 244；维度与库存同源）。
	//
	// 三条口径（docs/14 §4）：① 只记一个当前值、**不做成本流水**，覆盖式 —— 最近一次
	// 入库或显式写入为准；② 可空，NULL = **尚未核算**，绝不用 0 冒充「未知成本」
	//（0 是合法的显式成本：赠品 / 内部划拨）；③ 核算归采购侧（可以不走采购流程，
	// 由外部核算后导入），本表只是「这个仓里这条 SKU 现在按多少算成本」的落点。
	CostPrice *float64        `gorm:"column:cost_price"`
	Metadata  json.RawMessage `gorm:"column:metadata;type:jsonb;not null"`
	CreatedAt time.Time       `gorm:"column:create_time;not null"`
	UpdatedAt time.Time       `gorm:"column:update_time;not null"`
}

// TableName 实现 gorm 表名。
func (StockEntity) TableName() string { return "inventory_stocks" }

// StockRow 库存记录 + 仓库展示信息（本模块两张表只读 join 的投影）。
//
// metadata 是「默认查询不取」的列，投影不含它（与商品域一致）。
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
	// TrackQuantity 与 StockEntity 同义（false = 无限）：列表投影与真源不能各给一个口径。
	TrackQuantity bool      `gorm:"column:track_quantity"`
	Quantity      int       `gorm:"column:quantity"`
	CostPrice     *float64  `gorm:"column:cost_price"`
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
	// ExternalSKU 按「该仓的外部编码」过滤：N:1 下同一个外码会命中同一商品的多个变体行，
	// 这正是「这个外码在本仓一共有多少条货」的查询口径。
	ExternalSKU string
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
// 本方法只负责**事务边界**：自己开一个事务并委托给 EnsureStockTx ——
// 语义与 Tx 版本逐字一致，两处不会各写一份判定（2026-09-19 商品域要求
// 「商品 + 变体 + 各仓库存行」同事务，故写路径必须能接外部事务）。
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

// EnsureStockTx 在**调用方给定的**事务内幂等地确保库存记录存在，返回库中那一行。
//
// 三条口径：
//
//  1. 先读后写：绝大多数调用是「这一行已经有了」，读到的行直接返回（本次传入的
//     quantity / track_quantity / external_sku 都被忽略，覆盖式修改走显式入口）；
//  2. 插入用 ON CONFLICT (variant_id, warehouse_id) DO NOTHING：**不能让它报错** ——
//     并发命中唯一键时，那句 INSERT 会把调用方的整个事务标记为 aborted，
//     之后同事务里的任何语句都会失败（current transaction is aborted），
//     而调用方那批「商品 + 变体」的写入会一起回滚；
//  3. 插入 0 行（说明并发方先插进去了）时**在同一事务内回读**：DO NOTHING 不报错，
//     事务仍然可用，所以这里可以安全回读，不会产生第二条同维度记录。
//
// projectID 由行自带：inventory_stocks 有 FORCE 策略，缺 scope 时定位恒
// ErrRecordNotFound、每次调用都去 Create —— 表现为「重试偶尔能过」，实际是每次都在撞。
// 作用域设在**传入的 tx** 上（ScopeTx 不新开事务），错误原样返回绝不吞掉：
// 静默无视「没设上作用域」会让后面所有语句都在无隔离上下文里跑。
func (m *Model) EnsureStockTx(ctx context.Context, tx *gorm.DB, e *StockEntity) (out *StockEntity, err error) {
	if err = rls.ScopeTx(tx, e.ProjectID); err != nil {
		return nil, err
	}
	existing, gerr := m.getStockByVariantWarehouseTx(ctx, tx, e.ProjectID, e.VariantID, e.WarehouseID)
	if gerr == nil {
		return existing, nil
	}
	if !errors.Is(gerr, gorm.ErrRecordNotFound) {
		return nil, gerr
	}
	// 新建这一行之前兜住「不跟踪却带数量」：DDL 的 CHECK (track_quantity OR quantity = 0)
	// 会让这种行以一个没有上下文的 23514 冒出来（页面侧看到的是内部错误）。
	normalizeStockTracking(e)
	res := tx.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "variant_id"}, {Name: "warehouse_id"}},
		DoNothing: true,
	}).Create(e)
	if res.Error != nil {
		return nil, res.Error
	}
	if res.RowsAffected == 0 {
		return m.getStockByVariantWarehouseTx(ctx, tx, e.ProjectID, e.VariantID, e.WarehouseID)
	}
	return e, nil
}

// normalizeStockTracking 兜住迁移 261 的不变量：**不跟踪（无限）的行不允许带非零数量**
// （DDL 侧是 CHECK (track_quantity OR quantity = 0)，迁移 261）。
//
// 选择「改判成跟踪行」而不是「返回业务错误」的理由：
//
//	· 数量这一列只对跟踪行有意义，写下 7 就是在说「这行有 7 件货」——意图无歧义；
//	  而 TrackQuantity 的零值是 false（正是「不填 = 无限」那条口径的默认值），
//	  所以矛盾组合的真实成因是**漏填开关**，不是「想表达无限且有 7 件」。
//	· 与 service 层既有的口径一致：ensureStockRowWithExternalTx 传了数量就置 true，
//	  applyStockChangesTx 的入库 / 调整也是「显式给了数量就把行切成跟踪」。
//	· 报错方案会把一个冗余布尔变成新调用点的必修项，而漏填的表现就是一个页面读不懂的
//	  SQLSTATE 23514 —— 把矛盾消解掉比再加一道必填更容易不再踩。
//
// 只在**新建那一行**上生效（本函数由 EnsureStockTx 在 Create 之前调用）：已存在的行由调用方
// 决定，覆盖式改数量 / 开关走 UpdateStockQuantityAndTrackingTx —— 那条路径不「改判」而是
// 由 service 返回 ErrStockUntrackedQuantity：那里是**用户显式**把货关成无限却留着数字，
// 属于要打回给人的输入，不是可以替它决定的默认值。
func normalizeStockTracking(e *StockEntity) {
	if e != nil && !e.TrackQuantity && e.Quantity != 0 {
		e.TrackQuantity = true
	}
}

// getStockByVariantWarehouseTx 在给定事务内做「SKU × 仓库」定位（不新开事务）。
func (m *Model) getStockByVariantWarehouseTx(ctx context.Context, tx *gorm.DB, projectID, variantID, warehouseID string) (e *StockEntity, err error) {
	e = &StockEntity{}
	if serr := rls.ScopeTx(tx, projectID); serr != nil {
		return nil, serr
	}
	err = tx.WithContext(ctx).Model(&StockEntity{}).
		Where("variant_id = ? AND warehouse_id = ?", variantID, warehouseID).First(e).Error
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

// ProductWarehouseStockRow 一个商品在**各仓**的库存行（分仓聚合的只读投影）。
//
// 一行 = 一个 (product_id, warehouse_id, sku_code)：与库存真源同维度，不是聚合结果 ——
// 「同一个商品在几个仓各有多少」由调用方按 product_id 分组即可，聚合不该在这里做
// （各调用方的分组口径不同：商品列表要跨仓求和，下单要按归属仓取一个）。
//
// track_quantity 必须一起出去：quantity = 0 有**两义**（跟踪且卖光 / 不跟踪无限），
// 只给一个 0 会让「无限」在商品列表里显示成「没货」。
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

// WarehouseStocksByProducts 一次取回若干商品在**各仓**的库存行（分仓聚合契约）。
//
// 这是商品列表的「分仓库存」数据出口：一次查询覆盖全部商品的全部仓，不逐商品查
// （商品列表一页就是几十个商品 × 每商品几个仓，逐商品查就是几十次往返）。
//
// 排序固定（商品 → 默认仓优先 → 仓库排序号 → 短码 → SKU）：同一批数据每次都以同样
// 顺序返回，调用方分组后各仓的先后不随查询计划摆动。
//
// 只读、不带业务判断：哪些仓该显示、无限该怎么展示一律留在调用方。
//
// projectID 必填：inventory_stocks 与 join 的 inventory_warehouses 都在迁移 215 名单里，
// 缺作用域时这里**静默返回空集** —— 商品列表会整齐地显示「所有商品都没有分仓库存」，
// 既不报错也没有日志（与 ListStockCostsByVariants 同一个坑），所以由 rls.InProjectScope
// 在空工程时直接报错。
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

// stockRowsQuery 库存行 + 仓库信息的只读投影查询（本模块两表 join 的唯一定义处）。
//
// 句柄由调用方给：列表路径的作用域闭包必须把查询建在**同一个 tx** 上 ——
// 改用 m.db 会另取一条连接、脱离事务，策略谓词读到的 app.project_id 恒为 NULL，
// 列表静默空集（这正是 DB-009 要消灭的形态）。
func stockRowsQuery(ctx context.Context, db *gorm.DB) *gorm.DB {
	return db.WithContext(ctx).Table("inventory_stocks AS s").
		Select("s.id, s.project_id, s.warehouse_id, s.product_id, s.variant_id, s.sku_code, " +
			"s.external_sku, s.track_quantity, s.quantity, s.cost_price, s.create_time, s.update_time, " +
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
	if f.ExternalSKU != "" {
		q = q.Where("s.external_sku = ?", f.ExternalSKU)
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

// —— 仓库里的一条货（(仓库, 仓库 SKU)，迁移 251）——
//
// 「仓库 SKU」不是一个新实体：它就是库存真源上已有的 (warehouse_id, sku_code)。
// docs/14 §4 已定「不另建 SKU 目录」—— 仓库侧已有的编码就是仓库 SKU，
// 本列 / 本投影只回答「这条货在这个仓叫什么、有没有已经在商品侧建过变体」。

// WarehouseSKURow 仓库里一条货的只读投影（本模块两表 join，与方法同处一份定义）。
//
// 一行 = 一个 (warehouse_id, sku_code)：迁移 244 的 UNIQUE (warehouse_id, sku_code)
// 保证了它天然不重复，查询侧不必 GROUP BY。
type WarehouseSKURow struct {
	WarehouseID   string `gorm:"column:warehouse_id"`
	WarehouseCode string `gorm:"column:warehouse_code"`
	WarehouseName string `gorm:"column:warehouse_name"`
	IsDefault     bool   `gorm:"column:is_default"`
	SKUCode       string `gorm:"column:sku_code"`
	ExternalSKU   string `gorm:"column:external_sku"`
	ProductID     string `gorm:"column:product_id"`
	VariantID     string `gorm:"column:variant_id"`
	// HasVariant 这条库存行是否已绑定到某个变体。
	//
	// 今天 inventory_stocks.variant_id 是 NOT NULL + 外键（迁移 099），所以恒为真；
	// 保留这个信号是因为它正是「仓库里有货、商品侧还没建变体」要回答的问题 ——
	// 将来若放开 variant_id 可空（仓库自有 SKU 目录），查询侧不必再改一次投影。
	HasVariant bool `gorm:"column:has_variant"`
}

// WarehouseSKUFilter 仓库里一条货的查询条件（条件以参数传入）。
type WarehouseSKUFilter struct {
	ProjectID   string
	WarehouseID string
	// Keyword 同时匹配我们自己的 sku_code 与外部编码（两个都可能是运营手上的号）。
	Keyword string
}

// warehouseSKUsQuery (仓库, 仓库 SKU) 投影查询（本模块两表 join 的第二处定义）。
//
// 句柄由调用方给：列表路径的作用域闭包必须把查询建在同一个 tx 上（同 stockRowsQuery）。
func warehouseSKUsQuery(ctx context.Context, db *gorm.DB) *gorm.DB {
	return db.WithContext(ctx).Table("inventory_stocks AS s").
		Select("s.warehouse_id, s.product_id, s.variant_id, s.sku_code, s.external_sku, " +
			"s.create_time, " +
			"w.code AS warehouse_code, w.name AS warehouse_name, w.is_default AS is_default, " +
			"(s.variant_id IS NOT NULL) AS has_variant").
		Joins("JOIN inventory_warehouses AS w ON w.id = s.warehouse_id")
}

// applyWarehouseSKUFilter 把条件施加到 (仓库, 仓库 SKU) 投影查询上。
func applyWarehouseSKUFilter(q *gorm.DB, f WarehouseSKUFilter) *gorm.DB {
	if f.ProjectID != "" {
		q = q.Where("s.project_id = ?", f.ProjectID)
	}
	if f.WarehouseID != "" {
		q = q.Where("s.warehouse_id = ?", f.WarehouseID)
	}
	if kw := f.Keyword; kw != "" {
		like := "%" + kw + "%"
		q = q.Where("(s.sku_code ILIKE ? OR s.external_sku ILIKE ?)", like, like)
	}
	return q
}

// ListWarehouseSKUs 按工程 / 仓库列出可选的仓库 SKU（关键字命中我们或对方编码，分页）。
//
// 排序与库存列表同一口径（默认仓优先 → 仓库排序号 → 短码 → SKU），保证同一次选择
// 每次都以同样顺序返回：新建商品的「从仓库选」下拉依赖这个确定性。
//
// RLS（迁移 215）：inventory_stocks 与 join 的 inventory_warehouses 都在名单里，
// 作用域取自 f.ProjectID —— 缺工程时这里直接返回 rls.ErrInvalidProjectID，
// 不退化成「不限工程」（后者换非超级角色后是静默空集）。
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

// FindStockByWarehouseSKU 按「仓库 × 我们自己那条仓库 SKU」定位库存行
// （新建商品「从仓库选」的最小查询：确认这条货确实在这个仓）。
//
// projectID 由调用方给出：inventory_stocks 在迁移 215 名单里，无作用域时定位恒
// ErrRecordNotFound（与 GetStockByVariantWarehouse 同一个坑）。
func (m *Model) FindStockByWarehouseSKU(ctx context.Context, projectID, warehouseID, skuCode string) (e *StockEntity, err error) {
	e = &StockEntity{}
	err = rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {
		return tx.WithContext(ctx).Model(&StockEntity{}).
			Where("warehouse_id = ? AND sku_code = ?", warehouseID, skuCode).First(e).Error
	})
	return e, err
}

// SetExternalSKUByVariantWarehouse 写某 (仓库, 变体) 库存行的外部编码，返回影响行数。
//
// 返回行数而不是 error-only：调用方要能区分「写成功」与「这一行根本不存在」
// （后者在 RLS 缺作用域时会静默变成 0 行 —— 本方法的 projectID 就是为此必填）。
// 空串是**合法值**（清空：该仓改回用我们自己的 SKU），所以这里不做任何非空判定。
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

// StockSKUCodeExists 该仓内是否已有这条 sku_code 的库存行（排除 excludeVariantID 那一行）。
//
// 判据与 DDL 上的 UNIQUE (warehouse_id, sku_code)（迁移 244）一致，只是提前到写入之前：
// 直接撞约束只会拿到一个没有上下文的 23505，运营看不到「哪个仓、哪条编码」。
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

// ExternalSKUProductConflict 该仓内挂了同一外部编码的行是否属于**其它商品**。
//
// 这是 N:1 映射的弱校验（docs/14 §9.3）：多个变体共用同一个外码是**合法**的
// （同一个商品的十几个口味在仓库侧共用一条 SKU），但同一个外码不能同时挂在两个商品上 ——
// 那说明映射写错了。excludeProductID 是「本行所属商品」，为空表示不排除任何商品。
//
// 返回冲突行的 product_id 样例（空串 = 无冲突）。空 / 空白的 externalSKU 不参与校验：
// 「该仓用我们自己的 SKU」不是一种映射，不该被判成冲突。
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

// —— 归属仓成本（(仓库, SKU) 的当前成本，迁移 244；订单成本快照口径见 docs/14 §9.3）——

// StockCostRow 一条库存行的当前成本（本模块单表只读投影：批量取成本用，不 join）。
type StockCostRow struct {
	VariantID   string `gorm:"column:variant_id"`
	WarehouseID string `gorm:"column:warehouse_id"`
	// CostPrice 元；nil = 尚未核算（0 是合法的显式成本，两者不能混）。
	CostPrice *float64 `gorm:"column:cost_price"`
}

// ListStockCostsByVariants 批量取若干变体在**各仓**的当前成本（(仓库, SKU) 维度）。
//
// 用途：订单行成本快照 —— service 拿到全部候选行后按「归属仓」挑选（归属仓由
// service.resolveWarehouse 解析，解析规则不在 model 里）。一次查询覆盖全部变体，
// 不逐变体查：「十来个口味共用同一个成本值」（docs/14 §9.3 的常见形态）在建单时
// 就是一次十几行的批量读取，N+1 会让建单的读放大随口味数线性增长。
//
// projectID 由调用方给出：inventory_stocks 在迁移 215 名单里，缺作用域时这里
// **静默返回空集** —— 表现是「所有变体都未核算」（订单行成本全空）而建单照常成功。
// 成本缺失本身不是错误（未核算就是未核算），但这个空集绝不能来自「忘了带工程」，
// 所以调用方必须传工程、由 rls.InProjectScope 在空工程时直接报错。
func (m *Model) ListStockCostsByVariants(ctx context.Context, projectID string, variantIDs []string) (list []*StockCostRow, err error) {
	if len(variantIDs) == 0 {
		return nil, nil
	}
	err = rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {
		return tx.WithContext(ctx).Model(&StockEntity{}).
			Select("variant_id, warehouse_id, cost_price").
			Where("variant_id IN ?", variantIDs).
			Find(&list).Error
	})
	return list, err
}
