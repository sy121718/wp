package orderservice

// order_query_tools.go — 「按线索找订单」的只读服务面（给 AI 工具用）。
//
// 与 ListOrders 的关系：后者是列表页的入口（全站状态计数 + 分页），本文件的两个方法
// 服务的是另一类问题 —— 「订单 20261005001 到哪了」「张三那单发了没」。
// 这类问题的共同点是**先有一个线索、再要一份完整信息**，而不是「给我一页列表」。
//
// 只读、不带任何写能力：mcp 层拿到的是 OrderQueryReader 这个两方法的窄接口，
// 手里没有 ChangeStatus / RefundOrder，「AI 顺手改一单」不会在某次改动里悄悄变得可能。

import (
	"context"
	"errors"
	"strings"
	"time"

	orderdto "go_wp/internal/module/order/dto"
	orderenums "go_wp/internal/module/order/enums"
	ordermodel "go_wp/internal/module/order/model"
	"go_wp/pkg/utils"
)

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
