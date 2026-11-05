package model

import (
	"context"
	"errors"
	"fmt"
	json "go_wp/pkg/jsonx"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"go_wp/pkg/rls"
)

const (
	OrderStatusPending   = "pending"
	OrderStatusPaid      = "paid"
	OrderStatusShipped   = "shipped"
	OrderStatusCompleted = "completed"
	OrderStatusCancelled = "cancelled"
	OrderStatusRefunded  = "refunded"
)

const (
	CreatedViaCheckout = "checkout"
	CreatedViaAdmin    = "admin"
	CreatedViaAPI      = "api"
)

const (
	OperatorTypeAdmin    = "admin"
	OperatorTypeSystem   = "system"
	OperatorTypeCustomer = "customer"
)

type OrderEntity struct {
	ID            uint64  `gorm:"column:id;primaryKey"`
	ProjectID     string  `gorm:"column:project_id"`
	OrderNo       string  `gorm:"column:order_no"`
	Status        string  `gorm:"column:status"`
	UserID        *uint64 `gorm:"column:user_id"`
	CustomerEmail string  `gorm:"column:customer_email"`
	CustomerName  string  `gorm:"column:customer_name"`
	CustomerPhone string  `gorm:"column:customer_phone"`
	Currency      string  `gorm:"column:currency"`
	Subtotal      int64   `gorm:"column:subtotal"`
	DiscountTotal int64   `gorm:"column:discount_total"`

	MembershipDiscountTotal int64  `gorm:"column:membership_discount_total"`
	ShippingTotal           int64  `gorm:"column:shipping_total"`
	TaxTotal                int64  `gorm:"column:tax_total"`
	Total                   int64  `gorm:"column:total"`
	ShipName                string `gorm:"column:ship_name"`
	ShipPhone               string `gorm:"column:ship_phone"`
	ShipProvince            string `gorm:"column:ship_province"`
	ShipCity                string `gorm:"column:ship_city"`
	ShipDistrict            string `gorm:"column:ship_district"`
	ShipAddress             string `gorm:"column:ship_address"`
	ShipZip                 string `gorm:"column:ship_zip"`

	ShipCountry        string     `gorm:"column:ship_country"`
	BillName           string     `gorm:"column:bill_name"`
	BillPhone          string     `gorm:"column:bill_phone"`
	BillProvince       string     `gorm:"column:bill_province"`
	BillCity           string     `gorm:"column:bill_city"`
	BillDistrict       string     `gorm:"column:bill_district"`
	BillAddress        string     `gorm:"column:bill_address"`
	BillZip            string     `gorm:"column:bill_zip"`
	BillCountry        string     `gorm:"column:bill_country"`
	PaymentMethod      string     `gorm:"column:payment_method"`
	PaymentMethodTitle string     `gorm:"column:payment_method_title"`
	TransactionID      string     `gorm:"column:transaction_id"`
	PaidAt             *time.Time `gorm:"column:paid_at"`
	CompletedAt        *time.Time `gorm:"column:completed_at"`
	CreatedVia         string     `gorm:"column:created_via"`
	IPAddress          string     `gorm:"column:ip_address"`
	UserAgent          string     `gorm:"column:user_agent"`

	Attribution json.RawMessage `gorm:"column:attribution;type:jsonb;not null"`

	AdminNote    string    `gorm:"column:admin_note"`
	RequestID    string    `gorm:"column:request_id"`
	Remark       string    `gorm:"column:remark"`
	CancelReason string    `gorm:"column:cancel_reason"`
	CreateBy     uint64    `gorm:"column:create_by"`
	CreateTime   time.Time `gorm:"column:create_time"`
	UpdateTime   time.Time `gorm:"column:update_time"`

	FXRate     *string `gorm:"column:fx_rate"`
	FXBaseCode string  `gorm:"column:fx_base_code"`

	CarrierID   *string    `gorm:"column:carrier_id"`
	CarrierName string     `gorm:"column:carrier_name"`
	TrackingNo  string     `gorm:"column:tracking_no"`
	ShippedAt   *time.Time `gorm:"column:shipped_at"`

	FulfillmentStatus string `gorm:"column:fulfillment_status"`
}

func (OrderEntity) TableName() string { return "orders" }

const (
	FulfillmentPending    = "pending"
	FulfillmentPaid       = "paid"
	FulfillmentShipped    = "shipped"
	FulfillmentPartial    = "partial"
	FulfillmentReceived   = "received"
	FulfillmentException  = "exception"
	FulfillmentCancelled  = "cancelled"
	FulfillmentReship     = "reship"
	FulfillmentReturned   = "returned"
	FulfillmentReturnDone = "return_done"
	FulfillmentRefunded   = "refunded"
	FulfillmentFailed     = "failed"
)

func FulfillmentOfStatus(status string) string {
	switch status {
	case OrderStatusPending:
		return FulfillmentPending
	case OrderStatusPaid:
		return FulfillmentPaid
	case OrderStatusShipped:
		return FulfillmentShipped
	case OrderStatusCompleted:
		return FulfillmentReceived
	case OrderStatusCancelled:
		return FulfillmentCancelled
	case OrderStatusRefunded:
		return FulfillmentRefunded
	default:
		return FulfillmentPending
	}
}

func IsManualFulfillment(v string) bool {
	return v == FulfillmentException || v == FulfillmentReship ||
		v == FulfillmentReturned || v == FulfillmentReturnDone
}

type OrderFilter struct {
	ProjectID     string
	Status        string
	UserID        *uint64
	Keyword       string
	PaymentMethod string

	CreatedVia        string
	FulfillmentStatus string
	CreatedFrom       *time.Time
	CreatedTo         *time.Time
	Offset            int
	Limit             int
}

type OrderModel struct{ db *gorm.DB }

func NewOrderModel(db *gorm.DB) *OrderModel { return &OrderModel{db: db} }

func (m *OrderModel) DB(ctx context.Context) *gorm.DB {
	return m.db.WithContext(ctx).Model(&OrderEntity{})
}

func (m *OrderModel) Transaction(ctx context.Context, fn func(tx *gorm.DB) error) error {
	return m.db.WithContext(ctx).Transaction(fn)
}

func (m *OrderModel) Create(ctx context.Context, e *OrderEntity) (err error) {
	fillFulfillment(e)
	return rls.InProjectScope(ctx, m.db, e.ProjectID, func(tx *gorm.DB) error {
		return tx.Model(&OrderEntity{}).Create(e).Error
	})
}

func fillFulfillment(e *OrderEntity) {
	if e != nil && strings.TrimSpace(e.FulfillmentStatus) == "" {
		e.FulfillmentStatus = FulfillmentOfStatus(e.Status)
	}
}

func (m *OrderModel) CreateTx(ctx context.Context, tx *gorm.DB, e *OrderEntity) (err error) {
	fillFulfillment(e)
	if err = rls.ScopeTx(tx, e.ProjectID); err != nil {
		return err
	}
	return tx.WithContext(ctx).Model(&OrderEntity{}).Create(e).Error
}

func (m *OrderModel) GetByID(ctx context.Context, id uint64, projectID string) (e *OrderEntity, err error) {
	e = &OrderEntity{}
	if strings.TrimSpace(projectID) == "" {
		return nil, ErrProjectRequired
	}
	if err = rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {
		return tx.Model(&OrderEntity{}).Where("id = ? AND project_id = ?", id, projectID).First(e).Error
	}); err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return e, nil
}

func (m *OrderModel) GetByIDForUser(ctx context.Context, projectID string, id uint64, userID uint64) (e *OrderEntity, err error) {
	e = &OrderEntity{}
	if strings.TrimSpace(projectID) == "" {
		return nil, ErrProjectRequired
	}
	if err = rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {
		return tx.Model(&OrderEntity{}).
			Where("id = ? AND user_id = ? AND project_id = ?", id, userID, projectID).First(e).Error
	}); err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return e, nil
}

func (m *OrderModel) LockByIDTx(ctx context.Context, tx *gorm.DB, projectID string, id uint64) (e *OrderEntity, err error) {
	if strings.TrimSpace(projectID) != "" {
		if serr := rls.ScopeTx(tx, projectID); serr != nil {
			return nil, serr
		}
	}
	e = &OrderEntity{}
	q := tx.WithContext(ctx).Model(&OrderEntity{}).
		Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("id = ?", id)
	if strings.TrimSpace(projectID) != "" {
		q = q.Where("project_id = ?", projectID)
	}
	if err = q.First(e).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return e, nil
}

func (m *OrderModel) GetByNo(ctx context.Context, projectID string, orderNo string) (e *OrderEntity, err error) {
	e = &OrderEntity{}
	if err = rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {
		return tx.Model(&OrderEntity{}).Where("project_id = ? AND order_no = ?", projectID, orderNo).First(e).Error
	}); err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return e, nil
}

func (m *OrderModel) GetByRequestID(ctx context.Context, projectID string, requestID string) (e *OrderEntity, err error) {
	if strings.TrimSpace(requestID) == "" {
		return nil, nil
	}
	e = &OrderEntity{}
	if err = rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {
		return tx.Model(&OrderEntity{}).Where("project_id = ? AND request_id = ?", projectID, requestID).First(e).Error
	}); err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return e, nil
}

func (m *OrderModel) List(ctx context.Context, f OrderFilter) (list []*OrderEntity, total int64, err error) {
	err = rls.InProjectScope(ctx, m.db, f.ProjectID, func(tx *gorm.DB) error {
		return m.listLocked(tx, f, &list, &total)
	})
	return list, total, err
}

func (m *OrderModel) listLocked(tx *gorm.DB, f OrderFilter, list *[]*OrderEntity, total *int64) error {
	q := tx.Model(&OrderEntity{}).Where("project_id = ?", f.ProjectID)
	if f.Status != "" {
		q = q.Where("status = ?", f.Status)
	}
	if f.UserID != nil {
		q = q.Where("user_id = ?", *f.UserID)
	}
	if f.PaymentMethod != "" {
		q = q.Where("payment_method = ?", f.PaymentMethod)
	}
	if f.CreatedVia != "" {
		q = q.Where("created_via = ?", f.CreatedVia)
	}
	if f.FulfillmentStatus != "" {
		q = q.Where("fulfillment_status = ?", f.FulfillmentStatus)
	}
	if f.CreatedFrom != nil {
		q = q.Where("create_time >= ?", *f.CreatedFrom)
	}
	if f.CreatedTo != nil {
		q = q.Where("create_time <= ?", *f.CreatedTo)
	}
	if kw := strings.TrimSpace(f.Keyword); kw != "" {
		like := "%" + kw + "%"
		q = q.Where("order_no ILIKE ? OR customer_email ILIKE ? OR customer_name ILIKE ?", like, like, like)
	}
	if err := q.Count(total).Error; err != nil {
		return err
	}
	limit := f.Limit
	if limit <= 0 || limit > 200 {
		limit = 20
	}
	return q.Omit("attribution").Order("id DESC").Offset(f.Offset).Limit(limit).Find(list).Error
}

func (m *OrderModel) UpdateFields(ctx context.Context, projectID string, id uint64, fields map[string]any) (err error) {
	if strings.TrimSpace(projectID) == "" {
		return ErrProjectRequired
	}
	return rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {
		return tx.Model(&OrderEntity{}).Where("id = ? AND project_id = ?", id, projectID).Updates(fields).Error
	})
}

func (m *OrderModel) UpdateFieldsTx(ctx context.Context, tx *gorm.DB, projectID string, id uint64, fields map[string]any) (err error) {
	if strings.TrimSpace(projectID) != "" {
		if serr := rls.ScopeTx(tx, projectID); serr != nil {
			return serr
		}
	}
	q := tx.WithContext(ctx).Model(&OrderEntity{}).Where("id = ?", id)
	if strings.TrimSpace(projectID) != "" {
		q = q.Where("project_id = ?", projectID)
	}
	return q.Updates(fields).Error
}

var ErrProjectRequired = errors.New("order: 需要显式工程作用域")

func (m *OrderModel) ListPendingCreatedBefore(ctx context.Context, projectID string, cutoff time.Time, limit int) (list []*OrderEntity, err error) {
	if strings.TrimSpace(projectID) == "" {
		return nil, ErrProjectRequired
	}
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	err = rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {
		return tx.Model(&OrderEntity{}).
			Where("project_id = ? AND status = ? AND create_time < ?", projectID, OrderStatusPending, cutoff).
			Order("create_time ASC").Limit(limit).Find(&list).Error
	})
	return list, err
}

type PendingShipFilter struct {
	ProjectID string
	Keyword   string
	Offset    int
	Limit     int
}

func (m *OrderModel) ListPendingShipPage(ctx context.Context, f PendingShipFilter) (list []*OrderEntity, total int64, err error) {
	err = rls.InProjectScope(ctx, m.db, f.ProjectID, func(tx *gorm.DB) error {
		q := tx.WithContext(ctx).Model(&OrderEntity{}).
			Where("project_id = ? AND status = ?", f.ProjectID, OrderStatusPaid)
		if kw := strings.TrimSpace(f.Keyword); kw != "" {
			like := "%" + kw + "%"
			q = q.Where("(order_no ILIKE ? OR customer_name ILIKE ?)", like, like)
		}
		q = q.Where(pendingShipRemainingSQL)
		if cerr := q.Count(&total).Error; cerr != nil {
			return cerr
		}
		limit := f.Limit
		if limit <= 0 || limit > 200 {
			limit = 20
		}
		return q.Omit("attribution").Order("create_time ASC").
			Offset(f.Offset).Limit(limit).Find(&list).Error
	})
	return list, total, err
}

const pendingShipRemainingSQL = "(SELECT COALESCE(SUM(oi.quantity), 0) FROM order_items AS oi WHERE oi.order_id = orders.id)" +
	" > (SELECT COALESCE(SUM(si.quantity), 0) FROM order_shipment_items AS si" +
	" JOIN order_shipments AS sh ON sh.id = si.shipment_id WHERE sh.order_id = orders.id)"

func (m *OrderModel) CountByStatus(ctx context.Context, projectID string) (counts map[string]int64, err error) {
	var rows []struct {
		Status string `gorm:"column:status"`
		N      int64  `gorm:"column:n"`
	}
	if err = rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {
		return tx.Model(&OrderEntity{}).Select("status, COUNT(*) AS n").
			Where("project_id = ?", projectID).Group("status").Scan(&rows).Error
	}); err != nil {
		return nil, err
	}
	counts = make(map[string]int64, len(rows))
	for _, r := range rows {
		counts[r.Status] = r.N
	}
	return counts, nil
}

func (m *OrderModel) CountByFulfillment(ctx context.Context, projectID string) (counts map[string]int64, err error) {
	var rows []struct {
		FulfillmentStatus string `gorm:"column:fulfillment_status"`
		N                 int64  `gorm:"column:n"`
	}
	if err = rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {
		return tx.Model(&OrderEntity{}).Select("fulfillment_status, COUNT(*) AS n").
			Where("project_id = ?", projectID).Group("fulfillment_status").Scan(&rows).Error
	}); err != nil {
		return nil, err
	}
	counts = make(map[string]int64, len(rows))
	for _, r := range rows {
		counts[r.FulfillmentStatus] = r.N
	}
	return counts, nil
}

// DefaultSalesStatuses 是「已成交」口径：销售额、客户增长、日报等所有
// 按成交统计的查询共用这一份，不许各自写一份。
var DefaultSalesStatuses = []string{OrderStatusPaid, OrderStatusShipped, OrderStatusCompleted}

type CustomerOrderSummaryRow struct {
	OrderCount      int64      `gorm:"column:order_count"`
	PaidOrderCount  int64      `gorm:"column:paid_order_count"`
	TotalAmount     int64      `gorm:"column:total_amount"`
	LastOrderID     *uint64    `gorm:"column:last_order_id"`
	LastOrderNo     *string    `gorm:"column:last_order_no"`
	LastOrderStatus *string    `gorm:"column:last_order_status"`
	LastOrderTime   *time.Time `gorm:"column:last_order_time"`
}

func selectExpr(tx *gorm.DB, expr string, args ...any) *gorm.DB {
	if n := strings.Count(expr, "?"); n != len(args) {
		panic(fmt.Sprintf("order/model: 列表达式含 %d 个 ? 但有 %d 个实参（必须相等）：%s", n, len(args), expr))
	}
	return tx.Select(expr, args...)
}

const customerOrderSummarySelect = "COALESCE(w.order_count, 0) AS order_count, " +
	"COALESCE(w.paid_order_count, 0) AS paid_order_count, " +
	"COALESCE(w.total_amount, 0) AS total_amount, " +
	"w.id AS last_order_id, w.order_no AS last_order_no, " +
	"w.status AS last_order_status, w.create_time AS last_order_time"

const customerOrderSummaryInnerSelect = "o.id, o.order_no, o.status, o.create_time, " +
	"COUNT(*) OVER () AS order_count, " +
	"COUNT(*) FILTER (WHERE o.status = ANY(string_to_array(?, ',')::text[])) OVER () AS paid_order_count, " +
	"COALESCE(SUM(" + orderNetTotalSQLExpr + ") FILTER (WHERE o.status = ANY(string_to_array(?, ',')::text[])) OVER (), 0) AS total_amount"

func (m *OrderModel) SummaryByUser(ctx context.Context, projectID string, userID uint64) (row CustomerOrderSummaryRow, err error) {
	err = rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {

		inner := tx.Table(OrderEntity{}.TableName()+" AS o").
			Select(customerOrderSummaryInnerSelect,

				strings.Join(DefaultSalesStatuses, ","),
				strings.Join(ReturnedStatuses, ","),
				strings.Join(DefaultSalesStatuses, ",")).
			Where("o.project_id = ?", projectID).
			Where("o.user_id = ?", userID).
			Order("o.id DESC").
			Limit(1)

		return tx.Table("(SELECT 1) AS anchor").
			Select(customerOrderSummarySelect).
			Joins("LEFT JOIN (?) AS w ON TRUE", inner).
			Scan(&row).Error
	})
	return row, err
}

func (m *OrderModel) ListByStatus(ctx context.Context, projectID, status string) (list []*OrderEntity, err error) {
	err = rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {
		return tx.WithContext(ctx).Model(&OrderEntity{}).
			Where("project_id = ? AND status = ?", projectID, status).
			Order("create_time ASC").Find(&list).Error
	})
	return list, err
}
