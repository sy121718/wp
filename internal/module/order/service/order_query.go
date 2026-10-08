package orderservice

//
// 与 ListOrders 的关系：后者是列表页的入口（全站状态计数 + 分页），本文件的两个方法
// 服务的是另一类问题 —— 「订单 20261005001 到哪了」「张三那单发了没」。
// 这类问题的共同点是**先有一个线索、再要一份完整信息**，而不是「给我一页列表」。
//
// 只读、不带任何写能力：mcp 层拿到的是 OrderQueryReader 这个两方法的窄接口，
// 手里没有 ChangeStatus / RefundOrder，「AI 顺手改一单」不会在某次改动里悄悄变得可能。

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	orderdto "go_wp/internal/module/order/dto"
	orderenums "go_wp/internal/module/order/enums"
	ordermodel "go_wp/internal/module/order/model"
	"go_wp/pkg/utils"
)

// GetOrder 订单详情：头 + 订单项 + 状态流转链。
//
// 一次取齐三块而不是三个接口：订单详情一屏就是这些，拆开只会让页面出现
// 「上半截已显示、下半截还在转」的中间态。
func (s *Service) GetOrder(ctx context.Context, req *orderdto.GetOrderReq) (res *orderdto.OrderDetailResp, err error) {
	if req == nil || req.OrderID == 0 || strings.TrimSpace(req.ProjectID) == "" {
		return nil, errors.New(orderenums.ErrProjectRequired)
	}
	head, err := s.orders.GetByID(ctx, req.OrderID, req.ProjectID)
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
			Remark: lg.Remark, CreateTime: utils.NewJSONTime(lg.CreateTime),
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
	head, err := s.orders.GetByIDForUser(ctx, req.ProjectID, req.OrderID, req.UserID)
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

// findOrderDefaultLimit 一次最多回几单。
//
// 工具的返回会整段进模型上下文：给 200 条等于把上下文烧光在一页表格上，
// 而用户问「张三那单」时真正有用的只有一两条。默认 10，上限 50。
const (
	findOrderDefaultLimit = 10
	findOrderMaxLimit     = 50
)

// FindOrders 按线索（关键词 / 状态 / 时间段）查订单。
//
// 窗口是**可选**的：不给就不限时间（用户问「张三的单」时通常没提时间）。
// 但给了就得按 UTC 日界解释，且上界要转成闭区间 —— OrderFilter.CreatedTo 是 <=，
// 直接把半开上界（次日零点）传下去会把第二天零点整的那一单也捞进来。
func (s *Service) FindOrders(ctx context.Context, req *orderdto.FindOrderReq) (res *orderdto.FindOrderResp, err error) {
	if req == nil || strings.TrimSpace(req.ProjectID) == "" {
		return nil, errors.New(orderenums.ErrProjectRequired)
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

// GetOrderDetailByNo 按商户单号取**详情**（含商品行与状态流水）。
//
// 为什么需要它（GetOrderByNo 已经在）：那个走支付通道回调的语义，回的是订单头 ——
// 回调只关心「这单付款成功了吗」。而 AI 被问「这单到哪了」时要的是状态流水
// （谁在什么时候把它推到哪一步），只看头部的 status 字段答不出「到哪了」。
func (s *Service) GetOrderDetailByNo(ctx context.Context, req *orderdto.GetOrderByNoReq) (res *orderdto.OrderDetailResp, err error) {
	if req == nil || strings.TrimSpace(req.ProjectID) == "" || strings.TrimSpace(req.OrderNo) == "" {
		return nil, errors.New(orderenums.ErrOrderNoInvalid)
	}
	head, err := s.orders.GetByNo(ctx, strings.TrimSpace(req.ProjectID), strings.TrimSpace(req.OrderNo))
	if err != nil {
		return nil, err
	}
	if head == nil {
		return nil, errors.New(orderenums.ErrOrderNotFound)
	}
	return s.detailOf(ctx, head)
}
