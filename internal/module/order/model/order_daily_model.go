package model

// order_daily_model.go — 区间内「按天」的订单聚合（概览页销售趋势柱图）。
//
// 与 order_range_model.go 的关系：那边回答「这段区间一共多少」，这边回答「每天各多少」。
// 两条 SQL 共用同一套状态口径（paidStatuses）、同一个净额表达式（orderNetTotalSQLExpr）
// 与同一个半开区间约定 —— 分开写是因为「一天一行」与「一行汇总」的 GROUP BY 形状不同，
// 而不是因为口径要另算一份。

import (
	"context"
	"strings"
	"time"

	"gorm.io/gorm"

	"go_wp/pkg/rls"
)

// orderDailySQL 区间内按天聚合。
//
// **没有任何订单的那天不会出现**：这里只负责「有数据的那些天」，补齐空天由调用方做。
// 在 SQL 里补零要 generate_series，而那会让「哪些天属于这个区间」这件事同时存在于
// SQL 与 Go 两处 —— 时区与边界一改就分叉，且分叉表现为「图上少一天/多一天」，
// 不会报错。
//
// 日的口径是 **UTC**（date_trunc 前先 AT TIME ZONE 'UTC'）：全站的按天聚合都是 UTC 的 day 桶
// （analytics 的按天统计就是），混用本地时区会让同一页面上两根柱子错开一个时区。
//
// 参数顺序（按 ? 在文本里出现的顺序）：paid_order_count 的状态名单、
// orderNetTotalSQLExpr 内部的退货状态名单、net_sales 的 FILTER 状态名单、project_id、from、to。
const orderDailySQL = `SELECT date_trunc('day', o.create_time AT TIME ZONE 'UTC')::date AS day,
       COUNT(*) AS order_count,
       COUNT(*) FILTER (WHERE o.status = ANY(string_to_array(?, ',')::text[])) AS paid_order_count,
       COALESCE(SUM(` + orderNetTotalSQLExpr + `) FILTER (WHERE o.status = ANY(string_to_array(?, ',')::text[])), 0) AS net_sales
  FROM orders o
 WHERE o.project_id = ?
   AND o.create_time >= ?
   AND o.create_time < ?
 GROUP BY 1
 ORDER BY 1`

// OrderDailyPoint 某一天的订单事实。
//
// 三个数与区间汇总（OrderRangeSummaryRow）同口径：把它们按天相加，结果应当等于
// 同区间的汇总 —— 这条不变量由测试钉住（两处口径一旦分叉，图上柱子加起来对不上 KPI）。
type OrderDailyPoint struct {
	Day time.Time `gorm:"column:day"`
	// OrderCount 当天创建的全部订单（含取消与退款）。
	OrderCount int64 `gorm:"column:order_count"`
	// PaidOrderCount 当天创建、且计入消费口径的订单数。
	PaidOrderCount int64 `gorm:"column:paid_order_count"`
	// NetSales 当天的净销售额（分）。
	NetSales int64 `gorm:"column:net_sales"`
}

// DailyByRange 取区间 [from, to) 内按天聚合的订单数据。
//
// 只回有订单的那些天（见 orderDailySQL）；projectID 必填（orders 带 FORCE 策略，
// 不设作用域在非超级角色下静默回空集）。
func (m *OrderModel) DailyByRange(ctx context.Context, projectID string, from, to time.Time) (rows []OrderDailyPoint, err error) {
	if strings.TrimSpace(projectID) == "" {
		return nil, ErrProjectRequired
	}
	if from.IsZero() || to.IsZero() {
		return nil, ErrRangeRequired
	}
	err = rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {
		return tx.Raw(orderDailySQL,
			strings.Join(paidStatuses, ","),
			strings.Join(ReturnedStatuses, ","),
			strings.Join(paidStatuses, ","),
			projectID, from, to).Scan(&rows).Error
	})
	return rows, err
}
