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

// orderItemScope 订单行聚合的公共筛选条件（热销榜与商品销售总量共用）。
//
// 抽成一个作用域函数而不是各写一份：「哪些单计入消费」是一条口径，两份 WHERE 各自维护
// 的失败模式是榜单排除了取消单、总量忘了排除 —— 两个数字摆在同一个页面上互相矛盾，
// 而每一处单独看都对。
//
// `o` 别名由调用方在 Table(...) 里给出（见 SalesOverviewByRange 的同类写法）。
func orderItemScope(q *gorm.DB, projectID string, from, to time.Time) *gorm.DB {
	return q.
		Where("o.project_id = ?", projectID).
		Where("o.create_time >= ?", from).
		Where("o.create_time < ?", to).
		Where("o.status = ANY(string_to_array(?, ',')::text[])", strings.Join(paidStatuses, ","))
}

// orderSoldQuantitySelect 区间内售出的商品总件数与贡献订单数。
//
// 顺带把贡献订单数一起取：运营看到「38 件」的下一个问题是「几个单贡献的」，
// 分两次查会在两次查询之间落进新单而互相矛盾（与区间摘要一次取三个数同理）。
const orderSoldQuantitySelect = "COALESCE(SUM(i.quantity), 0) AS quantity, COUNT(DISTINCT i.order_id) AS order_count"

// orderTopProductsSelect 榜单的取数列。
//
// **商品名与 SKU 取订单行上的快照**（order_items.product_name / sku），不是关联商品表现取：
// 表隔离下订单模块读不到商品表；而且即便读得到，展示的也该是「下单那一刻它叫什么」——
// 商品改名之后，历史区间的报表不该跟着变（对账时那会变成「上个月的报表被人改了」）。
//
// **金额口径是行实付合计**（line_total），**不含退款分摊**：退款是订单级的，摊到商品级
// 需要按退货单逐行匹配，本批不做。所以它与「净销售额」在有大额退款时会不一样 ——
// 这就是两处标签必须分别叫「销售额」与「净销售额」的原因（同名会让运营以为其中一个算错了）。
const orderTopProductsSelect = "i.product_id, i.product_name, i.sku, " +
	"SUM(i.quantity) AS quantity, COALESCE(SUM(i.line_total), 0) AS amount"

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
		return orderItemScope(orderSalesItemsJoin(tx), projectID, from, to).
			Select(orderSoldQuantitySelect).
			Scan(&row).Error
	})
	return row, nil
}

// orderSalesItemsJoin 明细表 JOIN 订单表（热销榜与商品销售总量共用的 FROM 骨架）。
//
// `i` 是本模块内的表（order_items），`o` 是同模块的 orders —— 同模块 JOIN 在
// 本仓 model 层规则里是允许的（禁止的是跨模块）。JOIN 条件用 TableName() 拼，
// 表名有真源。
func orderSalesItemsJoin(tx *gorm.DB) *gorm.DB {
	return tx.Table(OrderItemEntity{}.TableName() + " AS i").
		Joins("JOIN " + OrderEntity{}.TableName() + " AS o ON o.id = i.order_id")
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
		return orderItemScope(orderSalesItemsJoin(tx), projectID, from, to).
			Select(orderTopProductsSelect).
			Group("i.product_id, i.product_name, i.sku").
			// 排序用 quantity DESC 主序、amount DESC 次序、product_id 收尾：前两个相等时若没有
			// 第三个键，同一次查询在两台机器上可能给出不同顺序 —— 榜单会随机抖动，测试也会偶发。
			Order("quantity DESC, amount DESC, i.product_id").
			Limit(limit).
			Scan(&rows).Error
	})
	return rows, nil
}
