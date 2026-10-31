package orderservice

import (
	"context"
	json "go_wp/pkg/jsonx"
	"strings"
	"time"

	orderdto "go_wp/internal/module/order/dto"
	orderenums "go_wp/internal/module/order/enums"
	ordermodel "go_wp/internal/module/order/model"
	"go_wp/pkg/utils"
)

func (s *Service) GetOrder(ctx context.Context, req *orderdto.GetOrderReq) (res *orderdto.OrderDetailResp, err error) {
	if req == nil || req.OrderID == 0 || strings.TrimSpace(req.ProjectID) == "" {
		return nil, orderenums.Err(orderenums.ErrProjectRequired)
	}
	head, err := s.orders.GetByID(ctx, req.OrderID, req.ProjectID)
	if err != nil {
		return nil, err
	}
	if head == nil {
		return nil, orderenums.Err(orderenums.ErrOrderNotFound)
	}
	return s.detailOf(ctx, head)
}

func (s *Service) detailOf(ctx context.Context, head *ordermodel.OrderEntity) (res *orderdto.OrderDetailResp, err error) {
	orderID := head.ID
	items, err := s.items.ListByOrderID(ctx, orderID)
	if err != nil {
		return nil, err
	}
	logs, err := s.logs.ListByOrderID(ctx, orderID)
	if err != nil {
		return nil, err
	}
	res = &orderdto.OrderDetailResp{
		Head:  toOrderResp(head),
		Items: make([]*orderdto.OrderItemResp, 0, len(items)),
		Logs:  make([]*orderdto.StatusLogResp, 0, len(logs)),
	}
	for _, it := range items {
		res.Items = append(res.Items, &orderdto.OrderItemResp{
			ID: it.ID, ProductID: it.ProductID, VariantID: it.VariantID,
			ProductName: it.ProductName, VariantLabel: it.VariantLabel, SKU: it.SKU,
			UnitPrice: it.UnitPrice, Quantity: it.Quantity,
			LineSubtotal: it.LineSubtotal, LineDiscount: it.LineDiscount,
			LineTax: it.LineTax, LineTotal: it.LineTotal, CostPrice: it.CostPrice,
		})
	}
	for _, lg := range logs {
		res.Logs = append(res.Logs, &orderdto.StatusLogResp{
			FromStatus: lg.FromStatus, ToStatus: lg.ToStatus,
			OperatorType: lg.OperatorType, OperatorName: lg.OperatorName,
			Remark: lg.Remark, CreateTime: utils.NewJSONTime(lg.CreateTime),
		})
	}
	return res, nil
}

func (s *Service) ListOrders(ctx context.Context, req *orderdto.ListOrderReq) (res *orderdto.OrderListResp, err error) {
	if req == nil || req.ProjectID == "" {
		return nil, orderenums.Err(orderenums.ErrProjectRequired)
	}
	list, total, err := s.orders.List(ctx, ordermodel.OrderFilter{
		ProjectID:         req.ProjectID,
		Status:            req.Status,
		UserID:            req.UserID,
		Keyword:           req.Keyword,
		PaymentMethod:     req.PaymentMethod,
		CreatedVia:        req.CreatedVia,
		FulfillmentStatus: req.FulfillmentStatus,
		Offset:            req.Offset,
		Limit:             req.Limit,
	})
	if err != nil {
		return nil, err
	}
	counts, err := s.orders.CountByStatus(ctx, req.ProjectID)
	if err != nil {
		return nil, err
	}
	fulfillmentCounts, err := s.orders.CountByFulfillment(ctx, req.ProjectID)
	if err != nil {
		return nil, err
	}
	res = &orderdto.OrderListResp{
		List:              make([]*orderdto.OrderResp, 0, len(list)),
		Total:             total,
		Counts:            counts,
		FulfillmentCounts: fulfillmentCounts,
	}
	for _, e := range list {
		res.List = append(res.List, toOrderResp(e))
	}
	return res, nil
}

func (s *Service) GetOrderByNo(ctx context.Context, req *orderdto.GetOrderByNoReq) (res *orderdto.OrderResp, err error) {
	if req == nil || strings.TrimSpace(req.ProjectID) == "" || strings.TrimSpace(req.OrderNo) == "" {
		return nil, orderenums.Err(orderenums.ErrOrderNoInvalid)
	}
	head, err := s.orders.GetByNo(ctx, req.ProjectID, strings.TrimSpace(req.OrderNo))
	if err != nil {
		return nil, err
	}
	if head == nil {
		return nil, orderenums.Err(orderenums.ErrOrderNotFound)
	}
	return toOrderResp(head), nil
}

func (s *Service) ListVisitorOrders(ctx context.Context, req *orderdto.VisitorOrderListReq) (res *orderdto.VisitorOrderListResp, err error) {
	if req == nil || strings.TrimSpace(req.ProjectID) == "" {
		return nil, orderenums.Err(orderenums.ErrProjectRequired)
	}
	if req.UserID == 0 {
		return nil, orderenums.Err(orderenums.ErrInvalidParam)
	}
	uid := req.UserID
	list, total, err := s.orders.List(ctx, ordermodel.OrderFilter{
		ProjectID: req.ProjectID,
		Status:    req.Status,
		UserID:    &uid,
		Offset:    req.Offset,
		Limit:     req.Limit,
	})
	if err != nil {
		return nil, err
	}
	res = &orderdto.VisitorOrderListResp{
		List:  make([]*orderdto.VisitorOrderResp, 0, len(list)),
		Total: total,
	}
	for _, e := range list {
		res.List = append(res.List, toVisitorOrderResp(e))
	}
	return res, nil
}

func (s *Service) GetVisitorOrder(ctx context.Context, req *orderdto.VisitorOrderDetailReq) (res *orderdto.VisitorOrderDetailResp, err error) {
	if req == nil || req.OrderID == 0 || req.UserID == 0 {
		return nil, orderenums.Err(orderenums.ErrInvalidParam)
	}
	head, err := s.orders.GetByIDForUser(ctx, req.ProjectID, req.OrderID, req.UserID)
	if err != nil {
		return nil, err
	}
	if head == nil {

		return nil, orderenums.Err(orderenums.ErrOrderNotFound)
	}
	if pid := strings.TrimSpace(req.ProjectID); pid != "" && head.ProjectID != pid {
		return nil, orderenums.Err(orderenums.ErrOrderNotFound)
	}
	items, err := s.items.ListByOrderID(ctx, head.ID)
	if err != nil {
		return nil, err
	}
	logs, err := s.logs.ListByOrderID(ctx, head.ID)
	if err != nil {
		return nil, err
	}
	res = &orderdto.VisitorOrderDetailResp{Head: toVisitorOrderResp(head), Items: make([]*orderdto.VisitorOrderItemResp, 0, len(items)), Logs: make([]*orderdto.VisitorOrderLogResp, 0, len(logs))}
	for _, it := range items {
		res.Items = append(res.Items, &orderdto.VisitorOrderItemResp{ID: it.ID, ProductName: it.ProductName, VariantLabel: it.VariantLabel, SKU: it.SKU, UnitPrice: it.UnitPrice, Quantity: it.Quantity, LineTotal: it.LineTotal})
	}
	for _, log := range logs {
		res.Logs = append(res.Logs, &orderdto.VisitorOrderLogResp{FromStatus: log.FromStatus, ToStatus: log.ToStatus, CreateTime: utils.NewJSONTime(log.CreateTime)})
	}
	return res, nil
}

func toOrderResp(e *ordermodel.OrderEntity) *orderdto.OrderResp {
	r := &orderdto.OrderResp{
		ID: e.ID, ProjectID: e.ProjectID, OrderNo: e.OrderNo, Status: e.Status,
		StatusTone:        orderenums.OrderStatusTone(e.Status),
		FulfillmentStatus: e.FulfillmentStatus,
		FulfillmentTone:   orderenums.FulfillmentTone(e.FulfillmentStatus),
		UserID:            e.UserID, CustomerEmail: e.CustomerEmail, CustomerName: e.CustomerName,
		CustomerPhone: e.CustomerPhone, Currency: e.Currency,
		Subtotal: e.Subtotal, DiscountTotal: e.DiscountTotal,
		ShippingTotal: e.ShippingTotal, TaxTotal: e.TaxTotal, Total: e.Total,
		ShipName: e.ShipName, ShipPhone: e.ShipPhone, ShipProvince: e.ShipProvince,
		ShipCity: e.ShipCity, ShipDistrict: e.ShipDistrict, ShipAddress: e.ShipAddress, ShipZip: e.ShipZip,
		ShipCountry: e.ShipCountry,
		BillName:    e.BillName, BillPhone: e.BillPhone, BillProvince: e.BillProvince,
		BillCity: e.BillCity, BillDistrict: e.BillDistrict, BillAddress: e.BillAddress, BillZip: e.BillZip,
		BillCountry:   e.BillCountry,
		PaymentMethod: e.PaymentMethod, PaymentMethodTitle: e.PaymentMethodTitle,
		TransactionID: e.TransactionID, PaidAt: utils.NewJSONTimePtr(e.PaidAt), CompletedAt: utils.NewJSONTimePtr(e.CompletedAt),
		CreatedVia: e.CreatedVia, IPAddress: e.IPAddress, UserAgent: e.UserAgent,
		AdminNote: e.AdminNote, Remark: e.Remark, CancelReason: e.CancelReason,
		CreateTime: utils.NewJSONTime(e.CreateTime), UpdateTime: utils.NewJSONTime(e.UpdateTime),
	}
	if len(e.Attribution) > 0 {
		var a orderdto.Attribution
		if uerr := json.Unmarshal(e.Attribution, &a); uerr == nil {
			r.Attribution = &a
		}
	}
	return r
}

const (
	findOrderDefaultLimit = 10
	findOrderMaxLimit     = 50
)

func (s *Service) FindOrders(ctx context.Context, req *orderdto.FindOrderReq) (res *orderdto.FindOrderResp, err error) {
	if req == nil || strings.TrimSpace(req.ProjectID) == "" {
		return nil, orderenums.Err(orderenums.ErrProjectRequired)
	}

	filter := ordermodel.OrderFilter{
		ProjectID: strings.TrimSpace(req.ProjectID),
		Status:    strings.TrimSpace(req.Status),
		Keyword:   strings.TrimSpace(req.Keyword),
		Limit:     req.Limit,
	}
	if filter.Limit <= 0 || filter.Limit > findOrderMaxLimit {
		filter.Limit = findOrderDefaultLimit
	}

	res = &orderdto.FindOrderResp{List: make([]*orderdto.OrderResp, 0, filter.Limit)}
	if strings.TrimSpace(req.From) != "" || strings.TrimSpace(req.To) != "" {
		from, to, rerr := normalizeRangeWindow(req.From, req.To, time.Now())
		if rerr != nil {
			return nil, rerr
		}
		toInclusive := to.Add(-time.Nanosecond)
		filter.CreatedFrom = &from
		filter.CreatedTo = &toInclusive
		res.From = from.Format(utils.LayoutDay)
		res.To = toInclusive.Format(utils.LayoutDay)
	}

	list, total, err := s.orders.List(ctx, filter)
	if err != nil {
		return nil, err
	}
	res.Total = total
	for _, e := range list {
		res.List = append(res.List, toOrderResp(e))
	}
	return res, nil
}

func (s *Service) GetOrderDetailByNo(ctx context.Context, req *orderdto.GetOrderByNoReq) (res *orderdto.OrderDetailResp, err error) {
	if req == nil || strings.TrimSpace(req.ProjectID) == "" || strings.TrimSpace(req.OrderNo) == "" {
		return nil, orderenums.Err(orderenums.ErrOrderNoInvalid)
	}
	head, err := s.orders.GetByNo(ctx, strings.TrimSpace(req.ProjectID), strings.TrimSpace(req.OrderNo))
	if err != nil {
		return nil, err
	}
	if head == nil {
		return nil, orderenums.Err(orderenums.ErrOrderNotFound)
	}
	return s.detailOf(ctx, head)
}

func toVisitorOrderResp(e *ordermodel.OrderEntity) *orderdto.VisitorOrderResp {
	return &orderdto.VisitorOrderResp{ID: e.ID, OrderNo: e.OrderNo, Status: e.Status, FulfillmentStatus: e.FulfillmentStatus, Currency: e.Currency,
		Subtotal: e.Subtotal, DiscountTotal: e.DiscountTotal, ShippingTotal: e.ShippingTotal, TaxTotal: e.TaxTotal, Total: e.Total,
		ShipName: e.ShipName, ShipPhone: e.ShipPhone, ShipProvince: e.ShipProvince, ShipCity: e.ShipCity, ShipDistrict: e.ShipDistrict, ShipAddress: e.ShipAddress, ShipZip: e.ShipZip, ShipCountry: e.ShipCountry,
		PaymentMethod: e.PaymentMethod, PaymentMethodTitle: e.PaymentMethodTitle, PaidAt: utils.NewJSONTimePtr(e.PaidAt), CompletedAt: utils.NewJSONTimePtr(e.CompletedAt), Remark: e.Remark, CancelReason: e.CancelReason, CreateTime: utils.NewJSONTime(e.CreateTime)}
}
