package model

import (
	"context"
	"time"

	"gorm.io/gorm"
)

type OrderCarrierEntity struct {
	ID        string `gorm:"column:id;primaryKey"`
	ProjectID string `gorm:"column:project_id;not null"`
	Code      string `gorm:"column:code;not null"`
	Name      string `gorm:"column:name;not null"`

	TrackingURLTemplate string `gorm:"column:tracking_url_template;not null"`
	Status              string `gorm:"column:status;not null"`
	Sort                int    `gorm:"column:sort;not null"`

	Metadata   []byte    `gorm:"column:metadata;not null"`
	CreateTime time.Time `gorm:"column:create_time;not null"`
	UpdateTime time.Time `gorm:"column:update_time;not null"`
}

func (OrderCarrierEntity) TableName() string { return "order_carriers" }

type OrderShipmentEntity struct {
	ID           string    `gorm:"column:id;primaryKey"`
	ProjectID    string    `gorm:"column:project_id;not null"`
	OrderID      uint64    `gorm:"column:order_id;not null"`
	ShipmentNo   string    `gorm:"column:shipment_no;not null"`
	CarrierID    *string   `gorm:"column:carrier_id"`
	CarrierName  string    `gorm:"column:carrier_name;not null"`
	TrackingNo   string    `gorm:"column:tracking_no;not null"`
	ShippedAt    time.Time `gorm:"column:shipped_at;not null"`
	OperatorID   int64     `gorm:"column:operator_id;not null"`
	OperatorName string    `gorm:"column:operator_name;not null"`
	Remark       string    `gorm:"column:remark;not null"`

	RequestID  string    `gorm:"column:request_id;not null"`
	CreateTime time.Time `gorm:"column:create_time;not null"`
}

func (OrderShipmentEntity) TableName() string { return "order_shipments" }

type OrderShipmentItemEntity struct {
	ID          int64   `gorm:"column:id;primaryKey"`
	ShipmentID  string  `gorm:"column:shipment_id;not null"`
	OrderItemID uint64  `gorm:"column:order_item_id;not null"`
	WarehouseID string  `gorm:"column:warehouse_id;not null"`
	StockID     *string `gorm:"column:stock_id"`
	Quantity    int     `gorm:"column:quantity;not null"`
}

func (OrderShipmentItemEntity) TableName() string { return "order_shipment_items" }

type OrderCarrierModel struct{ db *gorm.DB }

func NewOrderCarrierModel(db *gorm.DB) *OrderCarrierModel { return &OrderCarrierModel{db: db} }

func (m *OrderCarrierModel) List(ctx context.Context, projectID string, includeDisabled bool) (list []*OrderCarrierEntity, err error) {
	q := m.db.WithContext(ctx).Model(&OrderCarrierEntity{}).Where("project_id = ?", projectID)
	if !includeDisabled {
		q = q.Where("status = ?", "active")
	}
	err = q.Order("sort ASC, code ASC").Find(&list).Error
	return list, err
}

func (m *OrderCarrierModel) Get(ctx context.Context, id, projectID string) (e *OrderCarrierEntity, err error) {
	e = &OrderCarrierEntity{}
	err = m.db.WithContext(ctx).Model(&OrderCarrierEntity{}).
		Where("id = ? AND project_id = ?", id, projectID).First(e).Error
	return e, err
}

func (m *OrderCarrierModel) CodeExists(ctx context.Context, projectID, code, excludeID string) (exists bool, err error) {
	q := m.db.WithContext(ctx).Model(&OrderCarrierEntity{}).
		Where("project_id = ? AND upper(code) = upper(?)", projectID, code)
	if excludeID != "" {
		q = q.Where("id <> ?", excludeID)
	}
	var n int64
	if err = q.Count(&n).Error; err != nil {
		return false, err
	}
	return n > 0, nil
}

func (m *OrderCarrierModel) Create(ctx context.Context, e *OrderCarrierEntity) error {
	return m.db.WithContext(ctx).Create(e).Error
}

func (m *OrderCarrierModel) Update(ctx context.Context, e *OrderCarrierEntity) error {
	return m.db.WithContext(ctx).Model(&OrderCarrierEntity{}).Where("id = ?", e.ID).Save(e).Error
}

type OrderShipmentModel struct{ db *gorm.DB }

func NewOrderShipmentModel(db *gorm.DB) *OrderShipmentModel { return &OrderShipmentModel{db: db} }

func (m *OrderShipmentModel) CreateTx(ctx context.Context, tx *gorm.DB, e *OrderShipmentEntity,
	items []*OrderShipmentItemEntity) (err error) {
	if err = tx.WithContext(ctx).Create(e).Error; err != nil {
		return err
	}
	for _, it := range items {
		it.ShipmentID = e.ID
	}
	if len(items) == 0 {

		return nil
	}
	return tx.WithContext(ctx).Create(&items).Error
}

func (m *OrderShipmentModel) ListByOrder(ctx context.Context, orderID uint64) (list []*OrderShipmentEntity, err error) {
	err = m.db.WithContext(ctx).Model(&OrderShipmentEntity{}).
		Where("order_id = ?", orderID).Order("shipped_at ASC, create_time ASC").Find(&list).Error
	return list, err
}

func (m *OrderShipmentModel) ListItemsByShipments(ctx context.Context, shipmentIDs []string) (list []*OrderShipmentItemEntity, err error) {
	if len(shipmentIDs) == 0 {
		return nil, nil
	}
	err = m.db.WithContext(ctx).Model(&OrderShipmentItemEntity{}).
		Where("shipment_id IN ?", shipmentIDs).Order("id ASC").Find(&list).Error
	return list, err
}

func (m *OrderShipmentModel) ListItemsByOrder(ctx context.Context, orderID uint64) (list []*OrderShipmentItemEntity, err error) {
	err = m.db.WithContext(ctx).Model(&OrderShipmentItemEntity{}).
		Joins("JOIN order_shipments AS s ON s.id = order_shipment_items.shipment_id").
		Where("s.order_id = ?", orderID).Order("order_shipment_items.id ASC").Find(&list).Error
	return list, err
}

func (m *OrderShipmentModel) SumShippedQuantityTx(ctx context.Context, tx *gorm.DB,
	orderID uint64, itemIDs []uint64) (sum map[uint64]int, err error) {
	sum = make(map[uint64]int, len(itemIDs))
	if len(itemIDs) == 0 {
		return sum, nil
	}
	var rows []struct {
		OrderItemID uint64 `gorm:"column:order_item_id"`
		Quantity    int    `gorm:"column:quantity"`
	}
	err = tx.WithContext(ctx).Model(&OrderShipmentItemEntity{}).
		Select("order_shipment_items.order_item_id, SUM(order_shipment_items.quantity) AS quantity").
		Joins("JOIN order_shipments AS s ON s.id = order_shipment_items.shipment_id").
		Where("s.order_id = ? AND order_shipment_items.order_item_id IN ?", orderID, itemIDs).
		Group("order_shipment_items.order_item_id").Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	for _, r := range rows {
		sum[r.OrderItemID] = r.Quantity
	}
	return sum, nil
}

func (m *OrderShipmentModel) FindByRequestID(ctx context.Context, projectID, requestID string) (e *OrderShipmentEntity, err error) {
	if requestID == "" {
		return nil, nil
	}
	e = &OrderShipmentEntity{}
	err = m.db.WithContext(ctx).Model(&OrderShipmentEntity{}).
		Where("project_id = ? AND request_id = ?", projectID, requestID).First(e).Error
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, nil
		}
		return nil, err
	}
	return e, nil
}

func (m *OrderShipmentModel) CountShipmentsOnDayTx(ctx context.Context, tx *gorm.DB,
	projectID string, dayStart, dayEnd time.Time) (n int64, err error) {
	err = tx.WithContext(ctx).Model(&OrderShipmentEntity{}).
		Where("project_id = ? AND create_time >= ? AND create_time < ?", projectID, dayStart, dayEnd).
		Count(&n).Error
	return n, err
}

func (m *OrderShipmentModel) ListShipmentItemsByOrderItem(ctx context.Context,
	orderItemID uint64) (list []*OrderShipmentItemEntity, err error) {
	err = m.db.WithContext(ctx).Model(&OrderShipmentItemEntity{}).
		Where("order_item_id = ?", orderItemID).Order("id ASC").Find(&list).Error
	return list, err
}

func (m *OrderShipmentModel) WarehouseByOrderItem(ctx context.Context, orderItemIDs []uint64) (out map[uint64]string, err error) {
	out = make(map[uint64]string, len(orderItemIDs))
	if len(orderItemIDs) == 0 {
		return out, nil
	}
	var rows []struct {
		OrderItemID uint64 `gorm:"column:order_item_id"`
		WarehouseID string `gorm:"column:warehouse_id"`
	}

	if err = m.db.WithContext(ctx).Model(&OrderShipmentItemEntity{}).
		Select("order_item_id, warehouse_id").
		Where("order_item_id IN ?", orderItemIDs).
		Order("id DESC").Scan(&rows).Error; err != nil {
		return nil, err
	}
	for _, r := range rows {
		if _, seen := out[r.OrderItemID]; !seen {
			out[r.OrderItemID] = r.WarehouseID
		}
	}
	return out, nil
}

func (m *OrderShipmentModel) SumShippedQuantity(ctx context.Context, orderID uint64,
	itemIDs []uint64) (sum map[uint64]int, err error) {
	return m.SumShippedQuantityTx(ctx, m.db.WithContext(ctx), orderID, itemIDs)
}

func (m *OrderShipmentModel) ListByOrderTx(ctx context.Context, tx *gorm.DB, orderID uint64) (list []*OrderShipmentEntity, err error) {
	err = tx.WithContext(ctx).Model(&OrderShipmentEntity{}).
		Where("order_id = ?", orderID).Find(&list).Error
	return list, err
}
