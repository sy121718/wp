// Package masterdatamodel 主数据变更记录域持久化（issue #19）。
//
// 本 model 是「表访问单元（Repository）」，只做本模块 master_data_changes 一张表的
// **追加与只读查询**；业务规则（实体类型白名单、动作白名单、字段级 diff、值截断）
// 一律留在 service 层。
//
// 语义：这张表是 **append-only** 的 ——
//
//   - 代码层：本 model 只提供 Append 与查询方法，没有任何 Update / Delete 入口；
//   - 数据库层：迁移 111 建了 BEFORE UPDATE OR DELETE 触发器，任何改写（包括
//     psql 手工 UPDATE）都被数据库直接拒绝。审计表能被改写就等于没有审计表。
//
// 表隔离：本 model 只碰本模块表，不 JOIN 商品 / 库存模块的表；实体身份以
// (entity_type, entity_id) 裸列承载，展示名以 entity_label 快照承载 ——
// 实体被删掉之后变更历史仍然可读（这正是审计需要的）。
package masterdatamodel

import (
	"context"
	"time"

	"gorm.io/gorm"

	"go_wp/pkg/rls"
)

// TableMasterDataChanges 表名（迁移 111）。
const TableMasterDataChanges = "master_data_changes"

// ChangeEntity 一条字段级变更。
//
// 一行 = 一次写操作里的**一个字段**：动作 / 字段 / 旧值 / 新值 / 操作人 / 时间。
// 新增与删除同样逐字段落行（新增时 old 为空，删除时 new 为空），
// 因此「按实体查询」拿到的是一份与动作无关、形状一致的字段级时间线。
type ChangeEntity struct {
	ID string `gorm:"column:id;type:uuid;primaryKey"`
	// ProjectID 工程（查询与鉴权的最小范围）。
	ProjectID string `gorm:"column:project_id;type:uuid;not null"`
	// EntityType 实体类型白名单（product / product_variant / inventory_source）。
	EntityType string `gorm:"column:entity_type;type:text;not null"`
	EntityID   string `gorm:"column:entity_id;type:uuid;not null"`
	// EntityLabel 实体展示名快照（商品名 / SKU 编码 / 货源名），实体删除后仍可读。
	EntityLabel string `gorm:"column:entity_label;type:text;not null"`
	Action      string `gorm:"column:action;type:text;not null"`
	Field       string `gorm:"column:field;type:text;not null"`
	OldValue    string `gorm:"column:old_value;type:text;not null"`
	NewValue    string `gorm:"column:new_value;type:text;not null"`
	// Origin 这条记录由哪条写入路径产生（product / variant / pricing / receipt / source …）。
	Origin string `gorm:"column:origin;type:text;not null"`
	// OperatorID 操作人（会话里的登录名；缺失时为空串）。历史记录允许为空，
	// 但绝不允许事后补写 —— 本表 append-only。
	OperatorID string    `gorm:"column:operator_id;type:text;not null"`
	CreatedAt  time.Time `gorm:"column:create_time;not null"`
}

// TableName 实现 gorm 表名。
func (ChangeEntity) TableName() string { return TableMasterDataChanges }

// ChangeFilter 查询条件（条件以参数传入，方法内不写死业务判断）。
type ChangeFilter struct {
	ProjectID  string
	EntityType string
	EntityID   string
	Field      string
	Action     string
	// Keyword 命中的是实体展示名快照列（模糊匹配，大小写不敏感）。
	Keyword    string
	OperatorID string
	// Since / Until 半开区间 [Since, Until)，nil 表示该端不限。
	Since *time.Time
	Until *time.Time
}

// EntityHistoryRow 按实体聚合的一行（「某实体的变更历史」入口清单）。
//
// 展示列取「最近一次变更」的原样值：最后动作 / 最后字段 / 最后时间 / 最后操作人。
type EntityHistoryRow struct {
	EntityType     string    `gorm:"column:entity_type"`
	EntityID       string    `gorm:"column:entity_id"`
	EntityLabel    string    `gorm:"column:entity_label"`
	ChangeCount    int64     `gorm:"column:change_count"`
	LastAction     string    `gorm:"column:last_action"`
	LastField      string    `gorm:"column:last_field"`
	LastOperatorID string    `gorm:"column:last_operator_id"`
	LastAt         time.Time `gorm:"column:last_at"`
}

// Model 变更记录仓储。
type Model struct{ db *gorm.DB }

// NewModel 构造（不持有业务状态）。
func NewModel(db *gorm.DB) *Model { return &Model{db: db} }

// DB 本模块表句柄（内部实现细节，只允许被本 model 的仓储方法消费）。
func (m *Model) DB(ctx context.Context) *gorm.DB {
	return m.db.WithContext(ctx).Model(&ChangeEntity{})
}

// Append 追加变更记录（本 model 唯一的写入口；没有更新与删除入口）。
//
// 行数为 0 时直接成功返回（一次写操作一个字段都没变 = 不产生记录）。
func (m *Model) Append(ctx context.Context, rows []*ChangeEntity) (err error) {
	if len(rows) == 0 {
		return nil
	}
	// master_data_changes 是**分区表**（策略在父表与各子表上，见迁移 215 与
	// internal/partition.EnsureAhead）；写入承首行的工程 id。
	return rls.InProjectScope(ctx, m.db, rows[0].ProjectID, func(tx *gorm.DB) error {
		return tx.Model(&ChangeEntity{}).Create(rows).Error
	})
}

// AppendTx 在外部事务内追加变更记录（与业务写操作同事务）。
func (m *Model) AppendTx(tx *gorm.DB, rows []*ChangeEntity) (err error) {
	if len(rows) == 0 || tx == nil {
		return nil
	}
	// scope 设在调用方事务上：留痕必须与业务写同生共死，另开事务会破坏这个原子性。
	if serr := rls.ScopeTx(tx, rows[0].ProjectID); serr != nil {
		return serr
	}
	return tx.Create(rows).Error
}

// applyChangeFilter 把查询条件施加到变更记录查询上（条件以参数传入）。
func applyChangeFilter(q *gorm.DB, f ChangeFilter) *gorm.DB {
	if f.ProjectID != "" {
		q = q.Where("project_id = ?", f.ProjectID)
	}
	if f.EntityType != "" {
		q = q.Where("entity_type = ?", f.EntityType)
	}
	if f.EntityID != "" {
		q = q.Where("entity_id = ?", f.EntityID)
	}
	if f.Field != "" {
		q = q.Where("field = ?", f.Field)
	}
	if f.Action != "" {
		q = q.Where("action = ?", f.Action)
	}
	if f.OperatorID != "" {
		q = q.Where("operator_id = ?", f.OperatorID)
	}
	if f.Keyword != "" {
		like := "%" + f.Keyword + "%"
		q = q.Where("entity_label ILIKE ?", like)
	}
	if f.Since != nil {
		q = q.Where("create_time >= ?", *f.Since)
	}
	if f.Until != nil {
		q = q.Where("create_time < ?", *f.Until)
	}
	return q
}

// List 变更记录列表（固定按「时间倒序 → id」返回，同一时刻的记录顺序也确定）。
func (m *Model) List(ctx context.Context, f ChangeFilter, limit, offset int) (list []*ChangeEntity, err error) {
	err = rls.InProjectScope(ctx, m.db, f.ProjectID, func(tx *gorm.DB) error {
		q := applyChangeFilter(tx.Model(&ChangeEntity{}), f).Order("create_time DESC, id ASC")
		if limit > 0 {
			q = q.Limit(limit).Offset(offset)
		}
		return q.Find(&list).Error
	})
	return list, err
}

// Count 变更记录计数（同条件，供分页用）。
func (m *Model) Count(ctx context.Context, f ChangeFilter) (n int64, err error) {
	err = rls.InProjectScope(ctx, m.db, f.ProjectID, func(tx *gorm.DB) error {
		return applyChangeFilter(tx.Model(&ChangeEntity{}), f).Count(&n).Error
	})
	return n, err
}

// ListEntityHistories 按实体聚合的变更历史（验收 4「后台可按实体查询」的入口清单）。
//
// 取「最近一次变更」的原样值用 array_agg(...)[1]：同一时刻并列时按 id 兜底，
// 保证同一份数据每次聚合出同样的结果（后台核对依赖确定性）。
func (m *Model) ListEntityHistories(ctx context.Context, f ChangeFilter, limit, offset int) (list []*EntityHistoryRow, err error) {
	var q *gorm.DB
	err = rls.InProjectScope(ctx, m.db, f.ProjectID, func(tx *gorm.DB) error {
		q = applyChangeFilter(tx.Model(&ChangeEntity{}), f).
			Select("entity_type, entity_id, " +
				"(array_agg(entity_label ORDER BY create_time DESC, id ASC))[1] AS entity_label, " +
				"COUNT(*) AS change_count, " +
				"(array_agg(action ORDER BY create_time DESC, id ASC))[1] AS last_action, " +
				"(array_agg(field ORDER BY create_time DESC, id ASC))[1] AS last_field, " +
				"(array_agg(operator_id ORDER BY create_time DESC, id ASC))[1] AS last_operator_id, " +
				"MAX(create_time) AS last_at").
			Group("entity_type, entity_id").
			Order("last_at DESC, entity_type ASC, entity_id ASC")
		if limit > 0 {
			q = q.Limit(limit).Offset(offset)
		}
		return q.Scan(&list).Error
	})
	return list, err
}

// CountEntities 实体计数（分组数，供分页用）。
//
// COUNT(DISTINCT (a, b)) 是 PostgreSQL 的行构造去重计数：直接由数据库算，
// 不把分组行拉回进程再数一遍。
func (m *Model) CountEntities(ctx context.Context, f ChangeFilter) (n int64, err error) {
	err = rls.InProjectScope(ctx, m.db, f.ProjectID, func(tx *gorm.DB) error {
		return applyChangeFilter(tx.Model(&ChangeEntity{}), f).
			Select("COUNT(DISTINCT (entity_type, entity_id)) AS count").
			Scan(&n).Error
	})
	return n, err
}
