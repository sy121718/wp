package orderservice

// order_customer_summary.go — 按客户聚合订单事实（客户管理页的「订单摘要」块）。
//
// 为什么放在订单模块而不是让后台页面自己拼：订单表是本模块的私有数据，
// 页面直接 join 会绕过「订单状态口径」这一层（哪些状态算消费、金额单位怎么换算），
// 而这些东西正是订单模块唯一的解释权所在。
//
// 这是**只读**能力，且收窄到一条方法（见 ordercontract.CustomerOrderSummaryReader）：
// 后台客户页需要的是「这个客户下过几单、花了多少、最后一次是什么时候」，
// 不是订单列表、更不是订单写能力。

import (
	"context"
	"errors"
	"strings"

	orderdto "go_wp/internal/module/order/dto"
	orderenums "go_wp/internal/module/order/enums"
	ordermodel "go_wp/internal/module/order/model"
)

// CustomerOrderSummaryOf 按「工程 + 客户」取订单聚合（数量 / 累计消费 / 最近一单）。
//
// 客户从来没有下过单是**正常结果**（HasOrders=false），不是错误：
// 刚注册的账号就长这样，把它当错误会让页面显示一句看不懂的提示。
func (s *Service) CustomerOrderSummaryOf(ctx context.Context, req *orderdto.CustomerOrderSummaryReq) (res *orderdto.CustomerOrderSummaryResp, err error) {
	if req == nil || strings.TrimSpace(req.ProjectID) == "" || req.UserID == 0 {
		return nil, errors.New(orderenums.ErrInvalidParam)
	}
	agg, err := s.orders.AggregateByUser(ctx, req.ProjectID, req.UserID)
	if err != nil {
		return nil, err
	}
	res = &orderdto.CustomerOrderSummaryResp{
		UserID:           req.UserID,
		ProjectID:        req.ProjectID,
		OrderCount:       agg.OrderCount,
		PaidOrderCount:   agg.PaidOrderCount,
		TotalAmount:      agg.TotalAmount,
		TotalAmountLabel: centsToYuanLabel(agg.TotalAmount),
	}
	// 最近一单：复用列表查询（排序恒为 id DESC，取一条即最新）。
	// 不为它单独加一个 model 方法 —— 那就是把同一条查询抄第二遍。
	uid := req.UserID
	latest, _, lerr := s.orders.List(ctx, ordermodel.OrderFilter{
		ProjectID: req.ProjectID,
		UserID:    &uid,
		Limit:     1,
	})
	if lerr != nil {
		return nil, lerr
	}
	if len(latest) > 0 {
		head := latest[0]
		res.HasOrders = true
		res.LastOrderID = head.ID
		res.LastOrderNo = head.OrderNo
		res.LastOrderStatus = head.Status
		res.LastOrderTime = &head.CreateTime
		res.LastOrderTimeText = head.CreateTime.Local().Format("2006-01-02 15:04")
	}
	return res, nil
}
