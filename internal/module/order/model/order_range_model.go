package model

// order_range_model.go — 「区间订单摘要」（概览页 KPI 与对外只读聚合的取数口）。
//
// 为什么这条聚合归订单模块：哪些状态算钱进来了、订单金额怎么取，都是订单域独占解释的事实。
// 概览页 / AI 工具 / 外部 MCP 都只是消费结论，不参与计算（表隔离：它们读不到 orders 表）。

import (
	"context"
	"errors"
	"strings"
	"time"

	"gorm.io/gorm"

	"go_wp/pkg/rls"
)

// orderRangeSummarySelect 区间订单摘要的三个聚合列。
//
// **为什么一条语句而不是三条**：三个数回答的是「同一批订单」的三件事实，拆开之后
// 两条查询之间落的新单会让「10 单 / 500 元」这种自相矛盾的组合漏到页面上，
// 而每一处单独看都对（与 SummaryByUser 收敛成一条的理由相同）。
//
// **为什么 SELECT 里还有一段拼串**：`orderNetTotalSQLExpr` 是一个**含子查询的口径表达式**
// （`GREATEST(o.total - COALESCE((SELECT SUM(r.refund_amount) ...), 0), 0)`），
// 它是本模块「净额」语义的唯一真源、被 KPI / 客户摘要 / 会员分档三处共用。
// GORM 没有「把表达式当列」的链式写法（`Select("expr")` 也是传字符串），
// 所以这里保留三处拼接：共享的净额表达式 + 两个 `FILTER (WHERE ...)` 名单。
// **拼的只有列表达式，FROM / WHERE / 参数绑定全部归 GORM** —— 这条分界线是本轮整改的判据。
//
// 时间窗是半开区间 [from, to)：上界不含。日期参数天生是「哪一天」（含当天），
// 而 timestamptz 带微秒 —— 用 `<= 当日 23:59:59` 会丢掉 23:59:59.5 的单，
// 且写不出「含当天」这个语义。调用方把上界算成**次日零点**传进来（service 负责）。
//
// 参数顺序（按 ? 在文本里出现的顺序）：paid_order_count 的状态名单、
// orderNetTotalSQLExpr 内部的退货状态名单、net_sales 的 FILTER 状态名单。
const orderRangeSummarySelect = "COUNT(*) AS order_count, " +
	"COUNT(*) FILTER (WHERE o.status = ANY(string_to_array(?, ',')::text[])) AS paid_order_count, " +
	"COALESCE(SUM(" + orderNetTotalSQLExpr + ") FILTER (WHERE o.status = ANY(string_to_array(?, ',')::text[])), 0) AS net_sales"

// OrderRangeSummaryRow 区间订单摘要的三个事实。
//
// OrderCount 是**全部状态**（含取消与退款）：它回答「区间内下了几单」，
// 与订单列表页不带状态筛选时的总数同源。若也按 paidStatuses 过滤，
// 失败模式是运营看到「这个月 8 单」而列表里有 11 单，两边都对不上且都不报错。
type OrderRangeSummaryRow struct {
	OrderCount     int64 `gorm:"column:order_count"`
	PaidOrderCount int64 `gorm:"column:paid_order_count"`
	// NetSales 计入消费口径的净销售额（分）：已付款 / 已发货 / 已完成，减去已实际收货的退款额。
	NetSales int64 `gorm:"column:net_sales"`
}

// ErrRangeRequired 缺少区间参数。
//
// 零值 time.Time 在 PG 里是 0001-01-01，`create_time >= 零值` **恒真** ——
// 一个漏传的 from 不会报错，它会静默变成「不限起点」，概览页于是显示全站累计数字
// 而看起来完全合理。区间聚合永远有时间范围，所以这里 fail closed。
var ErrRangeRequired = errors.New("order: 需要显式的区间参数")

// SummaryByRange 取区间 [from, to]（闭区间，与订单列表筛选同口径）内的订单摘要。
//
// projectID 必填：orders 带 FORCE 策略（迁移 215），不设作用域在非超级角色下
// 静默返回空集 —— 概览页会显示一片 0，同样「看起来合理」。
func (m *OrderModel) SummaryByRange(ctx context.Context, projectID string, from, to time.Time) (row OrderRangeSummaryRow, err error) {
	if strings.TrimSpace(projectID) == "" {
		return row, ErrProjectRequired
	}
	if from.IsZero() || to.IsZero() {
		return row, ErrRangeRequired
	}
	err = rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {
		return tx.Table(OrderEntity{}.TableName()+" AS o").
			Select(orderRangeSummarySelect,
				// 三个实参对应 orderRangeSummarySelect 里 ? 的出现顺序。
				strings.Join(paidStatuses, ","),
				strings.Join(ReturnedStatuses, ","),
				strings.Join(paidStatuses, ",")).
			Where("o.project_id = ?", projectID).
			Where("o.create_time >= ?", from).
			Where("o.create_time < ?", to).
			Scan(&row).Error
	})
	return row, err
}
