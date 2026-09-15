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
)

// CustomerOrderSummaryOf 按「工程 + 客户」取订单聚合（数量 / 累计消费 / 最近一单）。
//
// 客户从来没有下过单是**正常结果**（HasOrders=false），不是错误：
// 刚注册的账号就长这样，把它当错误会让页面显示一句看不懂的提示。
func (s *Service) CustomerOrderSummaryOf(ctx context.Context, req *orderdto.CustomerOrderSummaryReq) (res *orderdto.CustomerOrderSummaryResp, err error) {
	if req == nil || strings.TrimSpace(req.ProjectID) == "" || req.UserID == 0 {
		return nil, errors.New(orderenums.ErrInvalidParam)
	}
	// 聚合三值与最近一单在**同一条 SQL** 里取回（model.SummaryByUser 的窗口函数查询）。
	// 拆成三条（聚合 / 列表里的分页计数 / 列表取一单）时，三条之间落的新单会让摘要
	// 自相矛盾；其中分页计数那条在摘要场景里连结果都用不上，白扫一遍全量行。
	row, err := s.orders.SummaryByUser(ctx, req.ProjectID, req.UserID)
	if err != nil {
		return nil, err
	}
	res = &orderdto.CustomerOrderSummaryResp{
		UserID:           req.UserID,
		ProjectID:        req.ProjectID,
		OrderCount:       row.OrderCount,
		PaidOrderCount:   row.PaidOrderCount,
		TotalAmount:      row.TotalAmount,
		TotalAmountLabel: centsToYuanLabel(row.TotalAmount),
	}
	// 最近一单为空 ⇔ 这个客户在该工程下一单都没有（查询挂单行哨兵，恒返回一行）。
	// HasOrders 由它推出，而不是从聚合数字猜：0 单 + 0 元与「查不到」在数字上无法区分。
	if row.LastOrderID != nil && row.LastOrderTime != nil {
		res.HasOrders = true
		res.LastOrderID = *row.LastOrderID
		res.LastOrderTime = row.LastOrderTime
		res.LastOrderTimeText = row.LastOrderTime.Local().Format("2006-01-02 15:04")
		if row.LastOrderNo != nil {
			res.LastOrderNo = *row.LastOrderNo
		}
		if row.LastOrderStatus != nil {
			res.LastOrderStatus = *row.LastOrderStatus
		}
	}
	return res, nil
}
