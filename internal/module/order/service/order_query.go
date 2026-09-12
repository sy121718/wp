package orderservice

// order_query.go — 订单查询（BIZ-1 销售侧）。

import (
	"context"
	"encoding/json"
	"errors"

	orderdto "go_wp/internal/module/order/dto"
	orderenums "go_wp/internal/module/order/enums"
	ordermodel "go_wp/internal/module/order/model"
)

// GetOrder 订单详情：头 + 订单项 + 状态流转链。
//
// 一次取齐三块而不是三个接口：订单详情一屏就是这些，拆开只会让页面出现
// 「上半截已显示、下半截还在转」的中间态。
func (s *Service) GetOrder(ctx context.Context, orderID uint64) (res *orderdto.OrderDetailResp, err error) {
	if orderID == 0 {
		return nil, errors.New(orderenums.ErrInvalidParam)
	}
	head, err := s.orders.GetByID(ctx, orderID)
	if err != nil {
		return nil, err
	}
	if head == nil {
		return nil, errors.New(orderenums.ErrOrderNotFound)
	}
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
			Remark: lg.Remark, CreateTime: lg.CreateTime,
		})
	}
	return res, nil
}

// ListOrders 订单列表 + 各状态计数。
//
// 计数不受列表筛选影响（它回答的是「各状态各有多少单」这个全局问题），
// 只按工程聚合。
func (s *Service) ListOrders(ctx context.Context, req *orderdto.ListOrderReq) (res *orderdto.OrderListResp, err error) {
	if req == nil || req.ProjectID == "" {
		return nil, errors.New(orderenums.ErrProjectRequired)
	}
	list, total, err := s.orders.List(ctx, ordermodel.OrderFilter{
		ProjectID:     req.ProjectID,
		Status:        req.Status,
		UserID:        req.UserID,
		Keyword:       req.Keyword,
		PaymentMethod: req.PaymentMethod,
		Offset:        req.Offset,
		Limit:         req.Limit,
	})
	if err != nil {
		return nil, err
	}
	counts, err := s.orders.CountByStatus(ctx, req.ProjectID)
	if err != nil {
		return nil, err
	}
	res = &orderdto.OrderListResp{
		List:   make([]*orderdto.OrderResp, 0, len(list)),
		Total:  total,
		Counts: counts,
	}
	for _, e := range list {
		res.List = append(res.List, toOrderResp(e))
	}
	return res, nil
}

// toOrderResp 实体 → 视图。归因列在这里反序列化：形状由 dto 定义，
// 存的是 RawMessage，读出来给调用方结构化的对象（前端不用再解一次 JSON）。
func toOrderResp(e *ordermodel.OrderEntity) *orderdto.OrderResp {
	r := &orderdto.OrderResp{
		ID: e.ID, ProjectID: e.ProjectID, OrderNo: e.OrderNo, Status: e.Status,
		UserID: e.UserID, CustomerEmail: e.CustomerEmail, CustomerName: e.CustomerName,
		CustomerPhone: e.CustomerPhone, Currency: e.Currency,
		Subtotal: e.Subtotal, DiscountTotal: e.DiscountTotal,
		ShippingTotal: e.ShippingTotal, TaxTotal: e.TaxTotal, Total: e.Total,
		ShipName: e.ShipName, ShipPhone: e.ShipPhone, ShipProvince: e.ShipProvince,
		ShipCity: e.ShipCity, ShipDistrict: e.ShipDistrict, ShipAddress: e.ShipAddress, ShipZip: e.ShipZip,
		BillName: e.BillName, BillPhone: e.BillPhone, BillProvince: e.BillProvince,
		BillCity: e.BillCity, BillDistrict: e.BillDistrict, BillAddress: e.BillAddress, BillZip: e.BillZip,
		PaymentMethod: e.PaymentMethod, PaymentMethodTitle: e.PaymentMethodTitle,
		TransactionID: e.TransactionID, PaidAt: e.PaidAt, CompletedAt: e.CompletedAt,
		CreatedVia: e.CreatedVia, IPAddress: e.IPAddress, UserAgent: e.UserAgent,
		AdminNote: e.AdminNote, Remark: e.Remark, CancelReason: e.CancelReason,
		CreateTime: e.CreateTime, UpdateTime: e.UpdateTime,
	}
	if len(e.Attribution) > 0 {
		var a orderdto.Attribution
		if uerr := json.Unmarshal(e.Attribution, &a); uerr == nil {
			r.Attribution = &a
		}
	}
	return r
}
