package model

// order_status_log_model.go — 订单状态流转流水的表访问单元。
//
// 为什么单独一张表：**终态推不出路径**。同一张「已取消」的订单，可能来自
// 「待付款超时」也可能来自「已发货后协商取消」——后者要退货回库存、要追责，
// 前者不用。没有流转流水，这些差异在订单头上完全看不出来。
//
// append-only：本 model 不提供任何更新 / 删除方法。

import (
	"context"
	"time"

	"gorm.io/gorm"
)

// OrderStatusLogEntity 对应 order_status_logs 表。
type OrderStatusLogEntity struct {
	ID           uint64    `gorm:"column:id;primaryKey"`
	OrderID      uint64    `gorm:"column:order_id"`
	FromStatus   string    `gorm:"column:from_status;type:varchar(20)"`
	ToStatus     string    `gorm:"column:to_status;type:varchar(20)"`
	OperatorType string    `gorm:"column:operator_type;type:varchar(20)"`
	OperatorID   uint64    `gorm:"column:operator_id"`
	OperatorName string    `gorm:"column:operator_name;type:varchar(60)"`
	Remark       string    `gorm:"column:remark;type:varchar(255)"`
	CreateTime   time.Time `gorm:"column:create_time;type:timestamp(3)"`
}

// TableName 实现 gorm 表名（默认推断为 order_status_log_entities）。
func (OrderStatusLogEntity) TableName() string { return "order_status_logs" }

// OrderStatusLogModel 状态流转流水表访问单元。
type OrderStatusLogModel struct{ db *gorm.DB }

func NewOrderStatusLogModel(db *gorm.DB) *OrderStatusLogModel { return &OrderStatusLogModel{db: db} }

// DB 返回绑定本表的句柄（只允许本 model 的仓储方法消费）。
func (m *OrderStatusLogModel) DB(ctx context.Context) *gorm.DB {
	return m.db.WithContext(ctx).Model(&OrderStatusLogEntity{})
}

// Transaction 透传事务。
func (m *OrderStatusLogModel) Transaction(ctx context.Context, fn func(tx *gorm.DB) error) error {
	return m.db.WithContext(ctx).Transaction(fn)
}

// CreateTx 事务内写一条流转记录 —— 必须与订单头的状态更新同一事务，
// 否则会出现「状态改了但没留痕」或反之。
func (m *OrderStatusLogModel) CreateTx(ctx context.Context, tx *gorm.DB, e *OrderStatusLogEntity) (err error) {
	return tx.WithContext(ctx).Model(&OrderStatusLogEntity{}).Create(e).Error
}

// ListByOrderID 取某单的完整流转链（按 id 升序 = 时间顺序）。
func (m *OrderStatusLogModel) ListByOrderID(ctx context.Context, orderID uint64) (list []*OrderStatusLogEntity, err error) {
	err = m.DB(ctx).Where("order_id = ?", orderID).Order("id ASC").Find(&list).Error
	return list, err
}
