package orderservice

// order_query.go — 订单查询（BIZ-1 销售侧）。

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

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
	return s.detailOf(ctx, head)
}

// detailOf 用已取到的订单头组装详情（后台按 id 查与访客按归属查共用同一份组装）。
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

// GetOrderByNo 按商户单号取订单（支付通道回调的唯一入口：它只有单号）。
//
// 与 GetOrder 的区别只在入口参数，归属与展示口径完全一致 ——
// 回调是通道服务端发起的，没有会话，所以这里不做归属校验；
// 越权面由「单号不可枚举 + 端点验签」两条一起收口。
func (s *Service) GetOrderByNo(ctx context.Context, req *orderdto.GetOrderByNoReq) (res *orderdto.OrderResp, err error) {
	if req == nil || strings.TrimSpace(req.ProjectID) == "" || strings.TrimSpace(req.OrderNo) == "" {
		return nil, errors.New(orderenums.ErrOrderNoInvalid)
	}
	head, err := s.orders.GetByNo(ctx, req.ProjectID, strings.TrimSpace(req.OrderNo))
	if err != nil {
		return nil, err
	}
	if head == nil {
		return nil, errors.New(orderenums.ErrOrderNotFound)
	}
	return toOrderResp(head), nil
}

// ListVisitorOrders 访客查自己的订单。
//
// user_id 是**必填**的：请求里没带就报错，而不是「不过滤」——
// 少传一次归属条件就等于把全站订单列表发给某个访客。
func (s *Service) ListVisitorOrders(ctx context.Context, req *orderdto.VisitorOrderListReq) (res *orderdto.VisitorOrderListResp, err error) {
	if req == nil || strings.TrimSpace(req.ProjectID) == "" {
		return nil, errors.New(orderenums.ErrProjectRequired)
	}
	if req.UserID == 0 {
		return nil, errors.New(orderenums.ErrInvalidParam)
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
		List:  make([]*orderdto.OrderResp, 0, len(list)),
		Total: total,
	}
	for _, e := range list {
		res.List = append(res.List, toOrderResp(e))
	}
	return res, nil
}

// GetVisitorOrder 访客查自己的订单详情。
//
// 归属校验写在 SQL 条件里（GetByIDForUser 的 WHERE 带 user_id）：
// 「取回来再看是不是他的」一旦有人调整了调用顺序就会漏判，
// 而这里的失败模式是「访客看到别人的订单」—— 不可接受。
func (s *Service) GetVisitorOrder(ctx context.Context, req *orderdto.VisitorOrderDetailReq) (res *orderdto.OrderDetailResp, err error) {
	if req == nil || req.OrderID == 0 || req.UserID == 0 {
		return nil, errors.New(orderenums.ErrInvalidParam)
	}
	head, err := s.orders.GetByIDForUser(ctx, req.OrderID, req.UserID)
	if err != nil {
		return nil, err
	}
	if head == nil {
		// 别人的单与不存在的单返回同一句话：区分开来就是一个订单号探测器。
		return nil, errors.New(orderenums.ErrOrderNotFound)
	}
	if pid := strings.TrimSpace(req.ProjectID); pid != "" && head.ProjectID != pid {
		return nil, errors.New(orderenums.ErrOrderNotFound)
	}
	return s.detailOf(ctx, head)
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
