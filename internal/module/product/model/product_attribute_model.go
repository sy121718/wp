// product_attribute_model.go — 商品属性组表访问（issue #7）。
//
// 属性组与属性值合一张表（group + values JSONB，081 已定），故本文件仍是
// 「表访问单元」：只做 product_attributes 的 CRUD，不做 key 派生、默认值、
// 引用校验等业务判断（那些在 service）。
package productmodel

import (
	"context"
	"encoding/json"
	"time"

	"gorm.io/gorm"

	"go_wp/pkg/rls"
)

// ProductAttributeEntity 属性组（一行一组；值以 JSONB 数组承载）。
//
// Values 沿用 081 的 jsonb 列：本票把元素结构固定为 [{id,key,label,sort,enabled}]，
// 历史纯字符串数组由 service 读取时兜底归一（见 attribute_values.go）。
type ProductAttributeEntity struct {
	ID          string          `gorm:"column:id;primaryKey"`
	ProjectID   string          `gorm:"column:project_id;not null"`
	Key         string          `gorm:"column:key;not null"`
	Name        string          `gorm:"column:name;not null"`
	IsVariation bool            `gorm:"column:is_variation;not null"`
	Sort        int             `gorm:"column:sort;not null"`
	Values      json.RawMessage `gorm:"column:values;type:jsonb;not null"`
	Metadata    json.RawMessage `gorm:"column:metadata;type:jsonb;not null"`
	CreatedAt   time.Time       `gorm:"column:create_time;not null"`
	UpdatedAt   time.Time       `gorm:"column:update_time;not null"`
}

// TableName 实现 gorm 表名。
func (ProductAttributeEntity) TableName() string { return "product_attributes" }

// AttributeDB 属性表句柄（内部实现细节，只允许被本 model 的仓储方法消费）。
func (m *Model) AttributeDB(ctx context.Context) *gorm.DB {
	return m.db.WithContext(ctx).Model(&ProductAttributeEntity{})
}

// GetAttribute 按 ID 查属性组。
//
// projectID 由调用方给出：product_attributes 在迁移 215 名单里，跨工程的行不可见。
func (m *Model) GetAttribute(ctx context.Context, id, projectID string) (e *ProductAttributeEntity, err error) {
	e = &ProductAttributeEntity{}
	err = rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {
		return tx.WithContext(ctx).Model(&ProductAttributeEntity{}).Where("id = ?", id).First(e).Error
	})
	return e, err
}

// GetAttributeWithoutScope 按 ID 读行，**不设工程作用域**（审计 DB-009 的显式例外）。
//
// 唯一调用方是 ResolverFor —— 它在 builder.Compile **之前**被 presentation 的 renderHTML
// 调用，那时 ctx 里还没有工程 id（core.WithBuildProjectID 是 Compile 内部才补上的），
// 所以这条路径**拿不到工程上下文**。按 DB-009 的口径显式保留现状：不加空串兜底
// （那会被 rls 拒掉，把「静默 0 行」换成一个更难懂的错误），也不假装它已被隔离。
//
// 换非超级角色后本方法会 fail closed（策略谓词为 NULL ⇒ 0 行）：届时需要
// presentation 侧在 buildCtx 上补 WithBuildProjectID（本批禁改的域）。
//
// 不要给本方法加新的调用方：需要按 id 读的一律用带 projectID 的那个。
func (m *Model) GetAttributeWithoutScope(ctx context.Context, id string) (e *ProductAttributeEntity, err error) {
	e = &ProductAttributeEntity{}
	err = m.AttributeDB(ctx).Where("id = ?", id).First(e).Error
	return e, err
}

// AttributeKeyExists 同工程下属性组 key 是否被占用（excludeID 为空表示新建场景）。
func (m *Model) AttributeKeyExists(ctx context.Context, projectID, key, excludeID string) (exists bool, err error) {
	// 唯一性判定同样要作用域：缺 scope 时这里恒为「不存在」⇒ 重复创建被静默放行（DB-009）。
	err = rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {
		q := tx.WithContext(ctx).Model(&ProductAttributeEntity{}).
			Where("project_id = ? AND key = ?", projectID, key)
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

// ListAttributes 属性组列表（分页 + 可选过滤；variation 为 nil 表示不过滤）。
func (m *Model) ListAttributes(ctx context.Context, projectID, keyword string, variation *bool, limit, offset int) (list []*ProductAttributeEntity, err error) {
	q := m.AttributeDB(ctx)
	if projectID != "" {
		q = q.Where("project_id = ?", projectID)
	}
	if keyword != "" {
		q = q.Where("name ILIKE ? OR key ILIKE ?", "%"+keyword+"%", "%"+keyword+"%")
	}
	if variation != nil {
		q = q.Where("is_variation = ?", *variation)
	}
	err = q.Order("sort ASC, create_time ASC").Limit(limit).Offset(offset).Find(&list).Error
	return list, err
}

// CountAttributes 属性组总数（与 ListAttributes 同过滤条件）。
func (m *Model) CountAttributes(ctx context.Context, projectID, keyword string, variation *bool) (n int64, err error) {
	err = rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {
		q := tx.WithContext(ctx).Model(&ProductAttributeEntity{})
		if keyword != "" {
			q = q.Where("name ILIKE ? OR key ILIKE ?", "%"+keyword+"%", "%"+keyword+"%")
		}
		if variation != nil {
			q = q.Where("is_variation = ?", *variation)
		}
		return q.Count(&n).Error
	})
	return n, err
}

// ListAttributesByIDs 批量取属性组（商品引用校验与构建期批量解析用，避免 N+1），
// **必带工程作用域**。
//
// product_attributes 在迁移 215 名单里：缺作用域时换连接角色后属性组整批读空，
// 变体生成的维度归一与商品详情的内联属性都会静默退化成「没有属性」
// （审计 db-03 §2.5）。projectID 必填，空串由 rls 直接拒。
func (m *Model) ListAttributesByIDs(ctx context.Context, ids []string, projectID string) (list []*ProductAttributeEntity, err error) {
	if len(ids) == 0 {
		return nil, nil
	}
	err = rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {
		return tx.WithContext(ctx).Model(&ProductAttributeEntity{}).Where("id IN ?", ids).Find(&list).Error
	})
	return list, err
}

// ListAttributesByProject 某工程全部属性组（按排序）。
func (m *Model) ListAttributesByProject(ctx context.Context, projectID string) (list []*ProductAttributeEntity, err error) {
	err = m.AttributeDB(ctx).Where("project_id = ?", projectID).
		Order("sort ASC, create_time ASC").Find(&list).Error
	return list, err
}

// CreateAttribute 写入属性组。
func (m *Model) CreateAttribute(ctx context.Context, e *ProductAttributeEntity) (err error) {
	return rls.InProjectScope(ctx, m.db, e.ProjectID, func(tx *gorm.DB) error {
		return tx.Model(&ProductAttributeEntity{}).Create(e).Error
	})
}

// UpdateAttribute 更新属性组（整行保存）。
func (m *Model) UpdateAttribute(ctx context.Context, e *ProductAttributeEntity) (err error) {
	return rls.InProjectScope(ctx, m.db, e.ProjectID, func(tx *gorm.DB) error {
		return tx.Model(&ProductAttributeEntity{}).Where("id = ?", e.ID).Save(e).Error
	})
}

// DeleteAttribute 删除属性组。
func (m *Model) DeleteAttribute(ctx context.Context, id string) (err error) {
	return m.AttributeDB(ctx).Where("id = ?", id).Delete(&ProductAttributeEntity{}).Error
}

// ListProductAttributeIDs 某商品引用的属性组 id 数组（products.attribute_ids）。
//
// 用于「删除属性组前的引用检查」与「商品详情回显」：只读一列，不整行加载。
//
// productID 是**商品** id，projectID 才是工程作用域 —— 两者不可混用：products 在迁移 215
// 名单里，缺作用域的读在非超级角色下静默返回 0 行（fail closed 不报错）。
//
// 当前**没有调用方**：商品详情回显直接读行上已有的 attribute_ids 列（product_resp.go），
// 删除前引用检查走 ProductUsingAttribute 的 jsonb 反查。保留它是因为「按商品取引用」
// 这个形状迟早要用；签名先按 DB-009 的口径带上作用域，免得将来有人顺手拿它做跨工程读。
func (m *Model) ListProductAttributeIDs(ctx context.Context, productID, projectID string) (raw json.RawMessage, err error) {
	var row struct {
		AttributeIDs json.RawMessage `gorm:"column:attribute_ids"`
	}
	err = rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {
		return tx.WithContext(ctx).Model(&ProductEntity{}).
			Select("attribute_ids").Where("id = ?", productID).Take(&row).Error
	})
	if err != nil {
		return nil, err
	}
	return row.AttributeIDs, nil
}

// ProductUsingAttribute 反查引用了某属性组的商品（删除前引用检查）。
//
// 以 jsonb 包含谓词查询：attribute_ids 是 id 数组，@> 命中即被引用；
// 只取一行用于拦截提示，故 Limit(1)。命中多条时取排序最靠前的一条。
//
// projectID 由**调用方**给出：反查的是 products（迁移 215 名单），工程上下文只有调用方有
// （它的语义是「本次删除会撞到哪些商品」，作用域就是发起删除的那个工程）。
// 缺作用域时这里命中 0 行 ⇒ 占用检查静默放行 ⇒ 删除留下悬空引用（DB-009）。
func (m *Model) ProductUsingAttribute(ctx context.Context, attributeID, projectID string) (e *ProductEntity, err error) {
	probe, merr := json.Marshal([]string{attributeID})
	if merr != nil {
		return nil, gorm.ErrRecordNotFound
	}
	e = &ProductEntity{}
	err = rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {
		return tx.WithContext(ctx).Model(&ProductEntity{}).
			Where("attribute_ids @> ?::jsonb", string(probe)).
			Order("sort ASC, create_time ASC").Limit(1).Take(e).Error
	})
	return e, err
}
