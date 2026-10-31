package model

import (
	"context"
	"time"

	"gorm.io/gorm"
)

type OrderItemEntity struct {
	ID           uint64 `gorm:"column:id;primaryKey"`
	OrderID      uint64 `gorm:"column:order_id"`
	ProductID    string `gorm:"column:product_id"`
	VariantID    string `gorm:"column:variant_id"`
	ProductName  string `gorm:"column:product_name"`
	VariantLabel string `gorm:"column:variant_label"`
	SKU          string `gorm:"column:sku"`
	UnitPrice    int64  `gorm:"column:unit_price"`
	Quantity     int    `gorm:"column:quantity"`
	LineSubtotal int64  `gorm:"column:line_subtotal"`
	LineDiscount int64  `gorm:"column:line_discount"`
	LineTax      int64  `gorm:"column:line_tax"`
	LineTotal    int64  `gorm:"column:line_total"`

	CostPrice *int64 `gorm:"column:cost_price"`

	WarehouseID *string   `gorm:"column:warehouse_id"`
	CreateTime  time.Time `gorm:"column:create_time"`
}

func (OrderItemEntity) TableName() string { return "order_items" }

type OrderItemModel struct{ db *gorm.DB }

func NewOrderItemModel(db *gorm.DB) *OrderItemModel { return &OrderItemModel{db: db} }

func (m *OrderItemModel) DB(ctx context.Context) *gorm.DB {
	return m.db.WithContext(ctx).Model(&OrderItemEntity{})
}

func (m *OrderItemModel) Transaction(ctx context.Context, fn func(tx *gorm.DB) error) error {
	return m.db.WithContext(ctx).Transaction(fn)
}

func (m *OrderItemModel) CreateBatchTx(ctx context.Context, tx *gorm.DB, items []*OrderItemEntity) (err error) {
	if len(items) == 0 {
		return nil
	}
	return tx.WithContext(ctx).Model(&OrderItemEntity{}).Create(&items).Error
}

func (m *OrderItemModel) CreateBatch(ctx context.Context, items []*OrderItemEntity) (err error) {
	if len(items) == 0 {
		return nil
	}
	return m.DB(ctx).Create(&items).Error
}

func (m *OrderItemModel) ListByOrderID(ctx context.Context, orderID uint64) (list []*OrderItemEntity, err error) {
	err = m.DB(ctx).Where("order_id = ?", orderID).Order("id ASC").Find(&list).Error
	return list, err
}

func (m *OrderItemModel) ListByOrderIDTx(ctx context.Context, tx *gorm.DB, orderID uint64) (list []*OrderItemEntity, err error) {
	err = tx.WithContext(ctx).Model(&OrderItemEntity{}).
		Where("order_id = ?", orderID).Order("id ASC").Find(&list).Error
	return list, err
}

func (m *OrderItemModel) WarehouseByIDs(ctx context.Context, ids []uint64) (out map[uint64]string, err error) {
	out = make(map[uint64]string, len(ids))
	if len(ids) == 0 {
		return out, nil
	}
	var rows []struct {
		ID          uint64  `gorm:"column:id"`
		WarehouseID *string `gorm:"column:warehouse_id"`
	}
	if err = m.DB(ctx).Select("id, warehouse_id").Where("id IN ?", ids).Scan(&rows).Error; err != nil {
		return nil, err
	}
	for _, r := range rows {
		if r.WarehouseID != nil && *r.WarehouseID != "" {
			out[r.ID] = *r.WarehouseID
		}
	}
	return out, nil
}

func (m *OrderItemModel) CountByOrderID(ctx context.Context, orderID uint64) (count int64, err error) {
	err = m.DB(ctx).Where("order_id = ?", orderID).Count(&count).Error
	return count, err
}

func (m *OrderItemModel) SetWarehouseIDIfNullTx(ctx context.Context, tx *gorm.DB, id uint64, warehouseID string) error {
	return tx.WithContext(ctx).Model(&OrderItemEntity{}).
		Where("id = ? AND warehouse_id IS NULL", id).
		Update("warehouse_id", warehouseID).Error
}
