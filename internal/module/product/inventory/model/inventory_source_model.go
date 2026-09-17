// inventory_source_model.go — 货源表访问（issue #17）。
//
// 本 model 是「表访问单元（Repository）」，只做本模块 inventory_sources 一张表的 CRUD、
// 聚合内原子组合与只读投影；业务规则（谁能删、内部即关联方、结算价只属于内部货源、
// 编码归一）一律留在 service 层。
//
// 表隔离：本 model 只碰本模块表。采购单（issue #18）将来引用货源时以自己的表承载外键，
// 不在本 model 里做跨模块关联查询。
package inventorymodel

import (
	"context"
	"encoding/json"
	"time"

	"gorm.io/gorm"

	"go_wp/pkg/rls"
)

// SourceEntity 货源（供应商 / 集团内关联公司 / 自家工厂）。
//
// 三条不可动摇的语义：
//
//   - Type 区分内外部（external / internal）—— 它决定这条货源是否按内部交易口径出报表；
//   - RelatedParty 是**关联方标志**，独立于类型：内部货源恒为真（DDL CHECK 兜住），
//     外部供应商也可被标成关联方。报表按「类型 × 关联方」取数，这就是「关联方可用于
//     报表区分」的落点；
//   - Config 是**异构对接扩展信息**（JSON 对象）：不同来源的字段形状各不相同，只有它是
//     JSONB；SettlePrice / Status 等必须结构化，报表与采购单都按它们校验。
type SourceEntity struct {
	ID           string          `gorm:"column:id;primaryKey"`
	ProjectID    string          `gorm:"column:project_id;not null"`
	Code         string          `gorm:"column:code;not null"`
	Name         string          `gorm:"column:name;not null"`
	Type         string          `gorm:"column:type;not null"`
	RelatedParty bool            `gorm:"column:related_party;not null"`
	SettlePrice  *float64        `gorm:"column:settle_price"`
	Status       string          `gorm:"column:status;not null"`
	Config       json.RawMessage `gorm:"column:config;type:jsonb;not null"`
	Sort         int             `gorm:"column:sort;not null"`
	Metadata     json.RawMessage `gorm:"column:metadata;type:jsonb;not null"`
	CreatedAt    time.Time       `gorm:"column:create_time;not null"`
	UpdatedAt    time.Time       `gorm:"column:update_time;not null"`
}

// TableName 实现 gorm 表名。
func (SourceEntity) TableName() string { return "inventory_sources" }

// SourceFilter 货源查询条件（条件以参数传入，方法内不写死业务判断）。
//
// RelatedParty 是三态：nil 不过滤 / true 仅关联方 / false 仅非关联方 ——
// 「关联方可用于报表区分」在查询层就落成可组合的维度。
type SourceFilter struct {
	ProjectID    string
	Type         string
	RelatedParty *bool
	Status       string
	Keyword      string
	// HasSettlePrice 三态：nil 不过滤 / true 仅有结算价 / false 仅无结算价
	//（报表要单列「已设内部结算价的货源」，那是自产商品成本的来源）。
	HasSettlePrice *bool
}

// SourceSummaryRow 货源按「类型 × 关联方」分组的计数（报表区分的直接依据）。
type SourceSummaryRow struct {
	Type         string `gorm:"column:type"`
	RelatedParty bool   `gorm:"column:related_party"`
	Count        int64  `gorm:"column:count"`
}

// SourceDB 货源表句柄（内部实现细节，只允许被本 model 的仓储方法消费）。
func (m *Model) SourceDB(ctx context.Context) *gorm.DB {
	return m.db.WithContext(ctx).Model(&SourceEntity{})
}

// CreateSource 写入货源行。
func (m *Model) CreateSource(ctx context.Context, e *SourceEntity) (err error) {
	return rls.InProjectScope(ctx, m.db, e.ProjectID, func(tx *gorm.DB) error {
		return tx.Model(&SourceEntity{}).Create(e).Error
	})
}

// GetSource 按 ID 查货源。
//
// projectID 由调用方给出：inventory_sources 在迁移 215 名单里，跨工程的行不可见。
func (m *Model) GetSource(ctx context.Context, id, projectID string) (e *SourceEntity, err error) {
	e = &SourceEntity{}
	err = rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {
		return tx.WithContext(ctx).Model(&SourceEntity{}).Where("id = ?", id).First(e).Error
	})
	return e, err
}

// SourceCodeExists 某工程内货源编码是否被占用（大小写不敏感，excludeID 为空表示新建场景）。
func (m *Model) SourceCodeExists(ctx context.Context, projectID, code, excludeID string) (exists bool, err error) {
	// 唯一性判定要作用域：缺 scope 时恒「不存在」⇒ 重复创建被静默放行（DB-009）。
	err = rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {
		q := tx.WithContext(ctx).Model(&SourceEntity{}).
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

// applySourceFilter 把查询条件施加到货源查询上（条件以参数传入）。
func applySourceFilter(q *gorm.DB, f SourceFilter) *gorm.DB {
	if f.ProjectID != "" {
		q = q.Where("project_id = ?", f.ProjectID)
	}
	if f.Type != "" {
		q = q.Where("type = ?", f.Type)
	}
	if f.RelatedParty != nil {
		q = q.Where("related_party = ?", *f.RelatedParty)
	}
	if f.Status != "" {
		q = q.Where("status = ?", f.Status)
	}
	if f.Keyword != "" {
		like := "%" + f.Keyword + "%"
		q = q.Where("name ILIKE ? OR code ILIKE ?", like, like)
	}
	if f.HasSettlePrice != nil {
		if *f.HasSettlePrice {
			q = q.Where("settle_price IS NOT NULL")
		} else {
			q = q.Where("settle_price IS NULL")
		}
	}
	return q
}

// ListSources 货源列表（按条件过滤 + 分页；limit <= 0 表示不限条数）。
//
// 排序固定「排序号 → 编码」：报表与后台列表每次都以同样顺序返回（对比与核对依赖确定性）。
func (m *Model) ListSources(ctx context.Context, f SourceFilter, limit, offset int) (list []*SourceEntity, err error) {
	q := applySourceFilter(m.SourceDB(ctx), f).Order("sort ASC, code ASC")
	if limit > 0 {
		q = q.Limit(limit).Offset(offset)
	}
	err = q.Find(&list).Error
	return list, err
}

// CountSources 货源计数（同条件，供分页与报表前置校验用）。
func (m *Model) CountSources(ctx context.Context, f SourceFilter) (n int64, err error) {
	err = applySourceFilter(m.SourceDB(ctx), f).Count(&n).Error
	return n, err
}

// SummarySources 按「类型 × 关联方标志」分组计数（关联方报表区分的数据来源）。
//
// 分组是数据库的聚合能力，不是业务规则：怎么解释这两列的组合留在 service。
func (m *Model) SummarySources(ctx context.Context, projectID string) (rows []*SourceSummaryRow, err error) {
	q := m.SourceDB(ctx).
		Select("type, related_party, COUNT(*) AS count").
		Group("type, related_party").
		Order("type ASC, related_party DESC")
	if projectID != "" {
		q = q.Where("project_id = ?", projectID)
	}
	err = q.Scan(&rows).Error
	return rows, err
}

// UpdateSource 更新货源行（全字段保存）。
func (m *Model) UpdateSource(ctx context.Context, e *SourceEntity) (err error) {
	return rls.InProjectScope(ctx, m.db, e.ProjectID, func(tx *gorm.DB) error {
		return tx.Model(&SourceEntity{}).Where("id = ?", e.ID).Save(e).Error
	})
}

// DeleteSource 删除货源（硬删除；引用守卫属采购单一侧，issue #18 在 service 层补）。
func (m *Model) DeleteSource(ctx context.Context, id string) (err error) {
	return m.SourceDB(ctx).Where("id = ?", id).Delete(&SourceEntity{}).Error
}
