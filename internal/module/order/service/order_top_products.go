package orderservice

// order_top_products.go — 热销商品榜（概览页榜单 / AI 工具）。
//
// 这一层只做三件事：归一化窗口、把 limit 收进合法区间、给每行写名次。
// 排序规则（销量优先、金额与商品 id 收尾）全在 model 的 SQL 里 —— 名次必须是那条
// ORDER BY 的产物，在这里重排一次就等于把排序规则写了第二遍。

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

// defaultTopProducts 概览页默认看几条。
//
// 定成 5 是因为榜单块的高度固定：条数一多卡片就被撑开，页面整体版式随之跳动。
// AI 工具可以显式传更大值（上限见 model.MaxTopProductLimit）。
const defaultTopProducts = 5

// TopProducts 取区间内销量最高的若干商品（含名次，已按销量降序）。
func (s *Service) TopProducts(ctx context.Context, req *orderdto.OrderTopProductsReq) (res *orderdto.OrderTopProductsResp, err error) {
	if req == nil || strings.TrimSpace(req.ProjectID) == "" {
		return nil, errors.New(orderenums.ErrProjectRequired)
	}
	from, to, err := normalizeRangeWindow(req.From, req.To, time.Now())
	if err != nil {
		return nil, err
	}
	// limit 在这里 clamp 而不是报错：调用方多半是页面或模型，它填 1000 的意思是
	// 「尽量多」，不是「我要一个 1000 行的答案」（model 里还会再 clamp 一次作为兜底）。
	limit := req.Limit
	if limit <= 0 {
		limit = defaultTopProducts
	}
	if limit > ordermodel.MaxTopProductLimit {
		limit = ordermodel.MaxTopProductLimit
	}
	rows, err := s.orders.TopProductsByRange(ctx, req.ProjectID, from, to, limit)
	if err != nil {
		return nil, err
	}
	items := make([]orderdto.OrderTopProductItemDTO, 0, len(rows))
	for i, row := range rows {
		items = append(items, orderdto.OrderTopProductItemDTO{
			Rank:        i + 1,
			ProductID:   row.ProductID,
			ProductName: row.ProductName,
			SKU:         row.SKU,
			Quantity:    row.Quantity,
			Amount:      row.Amount,
			AmountLabel: centsToYuanLabel(row.Amount),
		})
	}
	return &orderdto.OrderTopProductsResp{
		ProjectID: req.ProjectID,
		From:      from.Format(utils.LayoutDay),
		To:        to.AddDate(0, 0, -1).Format(utils.LayoutDay),
		Limit:     limit,
		Items:     items,
	}, nil
}
