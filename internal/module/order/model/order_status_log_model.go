package model

import (
	"context"
	"time"

	"gorm.io/gorm"
)

type OrderStatusLogEntity struct {
	ID           uint64    `gorm:"column:id;primaryKey"`
	OrderID      uint64    `gorm:"column:order_id"`
	FromStatus   string    `gorm:"column:from_status"`
	ToStatus     string    `gorm:"column:to_status"`
	OperatorType string    `gorm:"column:operator_type"`
	OperatorID   uint64    `gorm:"column:operator_id"`
	OperatorName string    `gorm:"column:operator_name"`
	Remark       string    `gorm:"column:remark"`
	CreateTime   time.Time `gorm:"column:create_time"`
}

func (OrderStatusLogEntity) TableName() string { return "order_status_logs" }

type OrderStatusLogModel struct{ db *gorm.DB }

func NewOrderStatusLogModel(db *gorm.DB) *OrderStatusLogModel { return &OrderStatusLogModel{db: db} }

func (m *OrderStatusLogModel) DB(ctx context.Context) *gorm.DB {
	return m.db.WithContext(ctx).Model(&OrderStatusLogEntity{})
}

func (m *OrderStatusLogModel) Transaction(ctx context.Context, fn func(tx *gorm.DB) error) error {
	return m.db.WithContext(ctx).Transaction(fn)
}

func (m *OrderStatusLogModel) CreateTx(ctx context.Context, tx *gorm.DB, e *OrderStatusLogEntity) (err error) {
	return tx.WithContext(ctx).Model(&OrderStatusLogEntity{}).Create(e).Error
}

func (m *OrderStatusLogModel) ListByOrderID(ctx context.Context, orderID uint64) (list []*OrderStatusLogEntity, err error) {
	err = m.DB(ctx).Where("order_id = ?", orderID).Order("id ASC").Find(&list).Error
	return list, err
}
