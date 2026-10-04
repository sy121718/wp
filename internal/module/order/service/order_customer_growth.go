package orderservice

// order_customer_growth.go — 区间客户增长（客户概览 / 概览页「新客户」KPI）。
//
// 这一层只做三件事：归一化窗口、算复购率、把分/百分比格式化成展示串。
// 口径（谁是新人、什么算复购）全在 model 的 orderCustomerGrowthSQL 里，这里不复制第二份。

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	orderdto "go_wp/internal/module/order/dto"
	orderenums "go_wp/internal/module/order/enums"
	"go_wp/pkg/utils"
)

// CustomerGrowthByRange 取区间内的客户增长事实。
func (s *Service) CustomerGrowthByRange(ctx context.Context, req *orderdto.CustomerGrowthReq) (res *orderdto.CustomerGrowthResp, err error) {
	if req == nil || strings.TrimSpace(req.ProjectID) == "" {
		return nil, errors.New(orderenums.ErrProjectRequired)
	}
	from, to, err := normalizeRangeWindow(req.From, req.To, time.Now())
	if err != nil {
		return nil, err
	}
	row, err := s.orders.CustomerGrowthByRange(ctx, req.ProjectID, from, to)
	if err != nil {
		return nil, err
	}
	rate := repurchaseRatePct(row.NewRepurchasers, row.ReturningCustomers, row.OrderingCustomers)
	return &orderdto.CustomerGrowthResp{
		ProjectID: req.ProjectID,
		From:      from.Format(utils.LayoutDay),
		// to 是半开上界（次日零点），减一天才是用户看到的「结束日」（同 SummaryByRange）。
		To:                  to.AddDate(0, 0, -1).Format(utils.LayoutDay),
		OrderingCustomers:   row.OrderingCustomers,
		NewCustomers:        row.NewCustomers,
		ReturningCustomers:  row.ReturningCustomers,
		Repurchasers:        row.Repurchasers,
		NewRepurchasers:     row.NewRepurchasers,
		RepurchaseRatePct:   rate,
		RepurchaseRateLabel: fmt.Sprintf("%.1f%%", rate),
	}, nil
}

// repurchaseRatePct 复购率（百分比，一位小数）。
//
// 分子 = 新客里在区间内复购的 + 区间内下单的老客。老客**只要下单**就算「回来的」，
// 不必再复购一次 —— 他在区间之前已经下过单了，这次下单本身就是「回来」。
// 分母 = 区间内下单的客户数。
//
// 分母为 0 时返回 0 而不是 NaN：那意味着「这段时间没有客户下过单」，与「复购率 0%」
// 在页面上是同一件事（没东西可看），而 NaN 会让模板渲染出「NaN%」并且**没有任何报错**。
func repurchaseRatePct(newRepurchasers, returning, ordering int64) float64 {
	if ordering <= 0 {
		return 0
	}
	return math.Round(float64(newRepurchasers+returning)*1000/float64(ordering)) / 10
}
