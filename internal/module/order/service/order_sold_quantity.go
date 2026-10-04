package orderservice

// order_sold_quantity.go — 区间商品销售总量（概览页 KPI 与只读聚合）。
//
// 这一层只做两件事：归一化窗口、把 model 的行摊成 DTO。
// 口径（哪些单计入消费、商品件数怎么数）全在 model 的 orderSoldQuantitySQL 里。

import (
	"context"
	"errors"
	"strings"
	"time"

	orderdto "go_wp/internal/module/order/dto"
	orderenums "go_wp/internal/module/order/enums"
	"go_wp/pkg/utils"
)

// SoldQuantityByRange 取区间内的商品销售总量（件数）。
//
// 与 SummaryByRange 分开而不是塞进它：那条 SQL 只碰 orders 表，本条要 join order_items。
// 合成一条的代价是概览页每次取 KPI 都得付一次 join，而「件数」这一格多数时候不看；
// 两条查询各自可解释、各自可用索引，比一条又长又贵的合并查询更划算。
func (s *Service) SoldQuantityByRange(ctx context.Context, req *orderdto.OrderSoldQuantityReq) (res *orderdto.OrderSoldQuantityResp, err error) {
	if req == nil || strings.TrimSpace(req.ProjectID) == "" {
		return nil, errors.New(orderenums.ErrProjectRequired)
	}
	from, to, err := normalizeRangeWindow(req.From, req.To, time.Now())
	if err != nil {
		return nil, err
	}
	row, err := s.orders.SoldQuantityByRange(ctx, req.ProjectID, from, to)
	if err != nil {
		return nil, err
	}
	return &orderdto.OrderSoldQuantityResp{
		ProjectID: req.ProjectID,
		From:      from.Format(utils.LayoutDay),
		// to 是半开上界（次日零点），减一天才是用户看到的「结束日」（同 SummaryByRange）。
		To:         to.AddDate(0, 0, -1).Format(utils.LayoutDay),
		Quantity:   row.Quantity,
		OrderCount: row.OrderCount,
	}, nil
}
