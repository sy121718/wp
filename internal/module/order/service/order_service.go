package orderservice

import (
	"context"
	"strings"
	"time"

	membershipcontract "go_wp/internal/module/membership/contract"
	ordercontract "go_wp/internal/module/order/contract"
	orderenums "go_wp/internal/module/order/enums"
	ordermodel "go_wp/internal/module/order/model"
	productcontract "go_wp/internal/module/product/contract"
	projectcontract "go_wp/internal/module/project/contract"
	usercontract "go_wp/internal/module/user/contract"
	webhookcontract "go_wp/internal/module/webhook/contract"
	"go_wp/pkg/logger"
)

type Service struct {
	orders  *ordermodel.OrderModel
	items   *ordermodel.OrderItemModel
	logs    *ordermodel.OrderStatusLogModel
	coupons *ordermodel.CouponModel
	returns *ordermodel.ReturnModel
	product productcontract.VariantSnapshotPort
	stock   ordercontract.StockOperator

	guest usercontract.GuestAccountProvisioner

	webhooks webhookcontract.Dispatcher

	projects projectcontract.ProjectService

	membership membershipcontract.Reader

	warehouses ordercontract.WarehouseResolver

	stockRowLocator ordercontract.StockRowLocator

	shipments *ordermodel.OrderShipmentModel

	carriers *ordermodel.OrderCarrierModel

	warehouseSource ordercontract.ReturnWarehouseSource

	stockProbe ordercontract.ShipStockProbe

	currencies ordercontract.CurrencyLister
}

func NewService(
	orders *ordermodel.OrderModel,
	items *ordermodel.OrderItemModel,
	logs *ordermodel.OrderStatusLogModel,
	coupons *ordermodel.CouponModel,
	returns *ordermodel.ReturnModel,
	product productcontract.VariantSnapshotPort,
	stock ordercontract.StockOperator,
	guest usercontract.GuestAccountProvisioner,
	webhooks webhookcontract.Dispatcher,
) *Service {
	return &Service{
		orders: orders, items: items, logs: logs, coupons: coupons, returns: returns,
		product: product, stock: stock, guest: guest,
		webhooks: webhooks,
	}
}

func (s *Service) SetWarehouseResolver(resolver ordercontract.WarehouseResolver) {
	if s == nil {
		return
	}
	s.warehouses = resolver
}

func (s *Service) SetStockRowLocator(locator ordercontract.StockRowLocator) {
	if s == nil {
		return
	}
	s.stockRowLocator = locator
}

func (s *Service) SetShipStockProbe(probe ordercontract.ShipStockProbe) {
	if s == nil {
		return
	}
	s.stockProbe = probe
}

func (s *Service) SetWarehouseSource(source ordercontract.ReturnWarehouseSource) {
	if s == nil {
		return
	}
	s.warehouseSource = source
}

const (
	pendingShipPageDefault = 20
	pendingShipPageMax     = 200
)

func (s *Service) SetShipmentModels(shipments *ordermodel.OrderShipmentModel, carriers *ordermodel.OrderCarrierModel) {
	if s == nil {
		return
	}
	s.shipments = shipments
	s.carriers = carriers
}

var _ ordercontract.OrderService = (*Service)(nil)

func (s *Service) SetCurrencyLister(lister ordercontract.CurrencyLister) {
	if s == nil {
		return
	}
	s.currencies = lister
}

func (s *Service) SetProjects(projects projectcontract.ProjectService) {
	if s == nil {
		return
	}
	s.projects = projects
}

func (s *Service) projectIDs(ctx context.Context) ([]string, error) {
	if s == nil || s.orders == nil || s.projects == nil {
		return nil, orderenums.Err(orderenums.ErrProjectRequired)
	}
	list, err := s.projects.List(ctx)
	if err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(list))
	for i := range list {
		if id := strings.TrimSpace(list[i].ID); id != "" {
			ids = append(ids, id)
		}
	}
	if len(ids) == 0 {
		return nil, orderenums.Err(orderenums.ErrProjectRequired)
	}
	return ids, nil
}

func (s *Service) locateOrderProject(ctx context.Context, orderID uint64) (string, error) {
	ids, err := s.projectIDs(ctx)
	if err != nil {
		return "", err
	}
	for _, pid := range ids {
		if ctx.Err() != nil {
			break
		}
		e, gerr := s.orders.GetByID(ctx, orderID, pid)
		if gerr != nil {
			return "", gerr
		}
		if e != nil {
			return e.ProjectID, nil
		}
	}
	return "", orderenums.Err(orderenums.ErrOrderNotFound)
}

func (s *Service) resolveOrderProject(ctx context.Context, orderID uint64, explicit string) (string, error) {
	if pid := strings.TrimSpace(explicit); pid != "" {
		return pid, nil
	}
	return s.locateOrderProject(ctx, orderID)
}

func (s *Service) locateReturnProject(ctx context.Context, returnID uint64) (string, error) {
	ids, err := s.projectIDs(ctx)
	if err != nil {
		return "", err
	}
	for _, pid := range ids {
		if ctx.Err() != nil {
			break
		}
		e, gerr := s.returns.GetByID(ctx, pid, returnID)
		if gerr != nil {
			return "", gerr
		}
		if e != nil {
			return e.ProjectID, nil
		}
	}
	return "", orderenums.Err(orderenums.ErrReturnNotFound)
}

func (s *Service) locateCouponProject(ctx context.Context, couponID uint64) (string, error) {
	ids, err := s.projectIDs(ctx)
	if err != nil {
		return "", err
	}
	for _, pid := range ids {
		if ctx.Err() != nil {
			break
		}
		e, gerr := s.coupons.GetByID(ctx, pid, couponID)
		if gerr != nil {
			return "", gerr
		}
		if e != nil {
			return e.ProjectID, nil
		}
	}
	return "", orderenums.Err(orderenums.ErrCouponNotFound)
}

type OrderPaidEvent struct {
	OrderID uint64 `json:"orderId"`

	OrderNo string `json:"orderNo"`

	Status string `json:"status"`

	Currency string `json:"currency"`

	Total int64 `json:"total"`

	Subtotal      int64 `json:"subtotal"`
	DiscountTotal int64 `json:"discountTotal"`
	ShippingTotal int64 `json:"shippingTotal"`
	TaxTotal      int64 `json:"taxTotal"`

	PaymentMethod string `json:"paymentMethod"`

	TransactionID string `json:"transactionId"`

	PaidAt time.Time `json:"paidAt"`
}

func (s *Service) dispatchEvent(ctx context.Context, eventType string, payload any) {
	if s.webhooks == nil {
		return
	}
	if _, err := s.webhooks.DispatchEvent(ctx, eventType, payload); err != nil {

		logger.Error(err, "订单事件派发失败（不影响业务事实）："+eventType)
	}
}
