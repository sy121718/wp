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
)

// ProductAttributeEntity 属性组（一行一组；值以 JSONB 数组承载）。
//
// Values 沿用 081 的 jsonb 列：本票把元素结构固定为 [{id,key,label,sort,enabled}]，
// 历史纯字符串数组由 service 读取时兜底归一（见 attribute_values.go）。
type ProductAttributeEntity struct {
	ID          string          `gorm:"column:id;type:uuid;primaryKey"`
	ProjectID   string          `gorm:"column:project_id;type:uuid;not null"`
	Key         string          `gorm:"column:key;type:text;not null"`
	Name        string          `gorm:"column:name;type:text;not null"`
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
func (m *Model) GetAttribute(ctx context.Context, id string) (e *ProductAttributeEntity, err error) {
	e = &ProductAttributeEntity{}
	err = m.AttributeDB(ctx).Where("id = ?", id).First(e).Error
	return e, err
}

// AttributeKeyExists 同工程下属性组 key 是否被占用（excludeID 为空表示新建场景）。
func (m *Model) AttributeKeyExists(ctx context.Context, projectID, key, excludeID string) (exists bool, err error) {
	q := m.AttributeDB(ctx).Where("project_id = ? AND key = ?", projectID, key)
	if excludeID != "" {
		q = q.Where("id <> ?", excludeID)
	}
	var n int64
	if err = q.Count(&n).Error; err != nil {
		return false, err
	}
	return n > 0, nil
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
	err = q.Count(&n).Error
	return n, err
}

// ListAttributesByIDs 批量取属性组（商品引用校验与构建期批量解析用，避免 N+1）。
func (m *Model) ListAttributesByIDs(ctx context.Context, ids []string) (list []*ProductAttributeEntity, err error) {
	if len(ids) == 0 {
		return nil, nil
	}
	err = m.AttributeDB(ctx).Where("id IN ?", ids).Find(&list).Error
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
	return m.AttributeDB(ctx).Create(e).Error
}

// UpdateAttribute 更新属性组（整行保存）。
func (m *Model) UpdateAttribute(ctx context.Context, e *ProductAttributeEntity) (err error) {
	return m.AttributeDB(ctx).Where("id = ?", e.ID).Save(e).Error
}

// DeleteAttribute 删除属性组。
func (m *Model) DeleteAttribute(ctx context.Context, id string) (err error) {
	return m.AttributeDB(ctx).Where("id = ?", id).Delete(&ProductAttributeEntity{}).Error
}

// ListProductAttributeIDs 某商品引用的属性组 id 数组（products.attribute_ids）。
//
// 用于「删除属性组前的引用检查」与「商品详情回显」：只读一列，不整行加载。
func (m *Model) ListProductAttributeIDs(ctx context.Context, productID string) (raw json.RawMessage, err error) {
	var row struct {
		AttributeIDs json.RawMessage `gorm:"column:attribute_ids"`
	}
	err = m.DB(ctx).Select("attribute_ids").Where("id = ?", productID).Take(&row).Error
	if err != nil {
		return nil, err
	}
	return row.AttributeIDs, nil
}

// ProductUsingAttribute 反查引用了某属性组的商品（删除前引用检查）。
//
// 以 jsonb 包含谓词查询：attribute_ids 是 id 数组，@> 命中即被引用；
// 只取一行用于拦截提示，故 Limit(1)。命中多条时取排序最靠前的一条。
func (m *Model) ProductUsingAttribute(ctx context.Context, attributeID string) (e *ProductEntity, err error) {
	probe, merr := json.Marshal([]string{attributeID})
	if merr != nil {
		return nil, gorm.ErrRecordNotFound
	}
	e = &ProductEntity{}
	err = m.DB(ctx).
		Where("attribute_ids @> ?::jsonb", string(probe)).
		Order("sort ASC, create_time ASC").Limit(1).Take(e).Error
	return e, err
}
