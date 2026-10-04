package model

// order_top_product_model.go — 区间内「卖得最好的商品」聚合（概览页热销榜 / AI 工具）。
//
// 为什么归订单模块而不是商品模块：榜单的事实是「谁被买走多少」，来源是订单行；
// 商品模块手里只有商品本身，读不到 order_items（表隔离）。商品模块将来要展示自己的销量，
// 也应当经 contract 问订单模块要，而不是自己去 join。

import (
	"context"
	"strings"
	"time"

	"gorm.io/gorm"

	"go_wp/pkg/rls"
)

// orderItemScopeSQL 订单行聚合的公共筛选条件（热销榜与商品销售总量共用）。
//
// 抽成常量而不是各写一份：「哪些单计入消费」是一条口径，两份 WHERE 各自维护的失败模式
// 是榜单排除了取消单、总量忘了排除 —— 两个数字摆在同一个页面上互相矛盾，而每一处单独看都对。
//
// 参数顺序（按 ? 在文本里出现的顺序）：project_id、from、to、状态名单。
const orderItemScopeSQL = `
  WHERE o.project_id = ?
    AND o.create_time >= ?
    AND o.create_time < ?
    AND o.status = ANY(string_to_array(?, ',')::text[])`

// orderTopProductsSQL 区间内按销量排序的商品榜。
//
// **商品名与 SKU 取订单行上的快照**（order_items.product_name / sku），不是关联商品表现取：
// 表隔离下订单模块读不到商品表；而且即便读得到，展示的也该是「下单那一刻它叫什么」——
// 商品改名之后，历史区间的报表不该跟着变（对账时那会变成「上个月的报表被人改了」）。
//
// **金额口径是行实付合计**（line_total），**不含退款分摊**：退款是订单级的，摊到商品级
// 需要按退货单逐行匹配，本批不做。所以它与「净销售额」在有大额退款时会不一样 ——
// 这就是两处标签必须分别叫「销售额」与「净销售额」的原因（同名会让运营以为其中一个算错了）。
//
// **只统计计入消费的订单**（paidStatuses）：把取消/退款的单算进榜单会让「热销」
// 变成「下单最多的」（用户点了付款又取消也上榜），而那不是运营想看的。
//
// 排序用 quantity DESC 主序、amount DESC 次序、product_id 收尾：前两个相等时若没有
// 第三个键，同一次查询在两台机器上可能给出不同顺序 —— 榜单会随机抖动，测试也会偶发。
//
// 参数顺序：project_id、from、to、状态名单、limit（末位那个是本条自己的）。
const orderTopProductsSQL = `SELECT i.product_id,
       i.product_name,
       i.sku,
       SUM(i.quantity) AS quantity,
       COALESCE(SUM(i.line_total), 0) AS amount
  FROM order_items i
  JOIN orders o ON o.id = i.order_id` + orderItemScopeSQL + `
 GROUP BY i.product_id, i.product_name, i.sku
 ORDER BY quantity DESC, amount DESC, i.product_id
 LIMIT ?`

// orderSoldQuantitySQL 区间内售出的商品总件数（Σ order_items.quantity）。
//
// 与榜单**同一个筛选条件**（orderItemScopeSQL）：这里回答「一共卖了多少件」，
// 榜单回答「哪些商品卖得多」，两者必须自洽。
//
// 顺带把贡献订单数一起回：运营看到「38 件」的下一个问题是「几个单贡献的」，
// 分两次查会在两次查询之间落进新单而互相矛盾（与区间摘要用一条 SQL 取三个数同理）。
//
// 参数顺序：project_id、from、to、状态名单。
const orderSoldQuantitySQL = `SELECT COALESCE(SUM(i.quantity), 0) AS quantity,
       COUNT(DISTINCT i.order_id) AS order_count
  FROM order_items i
  JOIN orders o ON o.id = i.order_id` + orderItemScopeSQL

// MaxTopProductLimit 榜单最多能取多少行。
//
// 有上限是防「把整个商品目录拉出来排序」：概览页只要前几名，而 AI 工具的参数是
// 模型填的 —— 没有上限时它可能填一个很大的值，把一次对话变成一次全表排序。
const MaxTopProductLimit = 50

// OrderTopProductRow 榜单里的一行（已按销量降序）。
type OrderTopProductRow struct {
	ProductID   string `gorm:"column:product_id"`
	ProductName string `gorm:"column:product_name"`
	SKU         string `gorm:"column:sku"`
	// Quantity 区间内该商品被买走的总件数（同商品不同变体合并：榜单按商品看）。
	Quantity int64 `gorm:"column:quantity"`
	// Amount 区间内该商品的行实付合计（分，不含退款分摊）。
	Amount int64 `gorm:"column:amount"`
}

// OrderSoldQuantityRow 区间内的商品销售总量。
type OrderSoldQuantityRow struct {
	// Quantity 区间内售出的商品总件数（只算计入消费的订单）。
	Quantity int64 `gorm:"column:quantity"`
	// OrderCount 贡献这些件数的订单数（去重后的订单数，会小于等于明细行数）。
	OrderCount int64 `gorm:"column:order_count"`
}

// SoldQuantityByRange 取区间 [from, to) 内售出的商品总件数。
//
// 与 TopProductsByRange 同一个作用域与同一个筛选条件；两者的差别只在 SELECT 与是否分组。
func (m *OrderModel) SoldQuantityByRange(ctx context.Context, projectID string, from, to time.Time) (row OrderSoldQuantityRow, err error) {
	if strings.TrimSpace(projectID) == "" {
		return row, ErrProjectRequired
	}
	if from.IsZero() || to.IsZero() {
		return row, ErrRangeRequired
	}
	err = rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {
		return tx.Raw(orderSoldQuantitySQL,
			projectID, from, to,
			strings.Join(paidStatuses, ",")).Scan(&row).Error
	})
	return row, err
}

// TopProductsByRange 取区间 [from, to) 内销量最高的若干商品。
//
// limit <= 0 时用 MaxTopProductLimit；超过上限时**截到上限**而不是报错 ——
// 调用方多半是模型或页面，它填 1000 的意图是「尽量多」，不是「我要一个 1000 行的答案」。
func (m *OrderModel) TopProductsByRange(ctx context.Context, projectID string, from, to time.Time, limit int) (rows []OrderTopProductRow, err error) {
	if strings.TrimSpace(projectID) == "" {
		return nil, ErrProjectRequired
	}
	if from.IsZero() || to.IsZero() {
		return nil, ErrRangeRequired
	}
	if limit <= 0 || limit > MaxTopProductLimit {
		limit = MaxTopProductLimit
	}
	err = rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {
		return tx.Raw(orderTopProductsSQL,
			projectID, from, to,
			strings.Join(paidStatuses, ","),
			limit).Scan(&rows).Error
	})
	return rows, err
}
