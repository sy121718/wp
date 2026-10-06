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

// orderBucketSelect 按桶聚合的三个列表达式（天/小时逐字共用）。
//
// **把它抽成常量正是为了让「两个粒度只有 date_trunc 不同」变成编译期事实**：
// 两份 SQL 各写一遍这三个列时，某次只改其中一份就会让「区间汇总 = 各桶之和」
// 在其中一个粒度下失效，而失效的表现是图上柱子加起来对不上 KPI（不报错）。
//
// 拼串只发生在列表达式上（净额表达式含子查询，见 orderNetTotalSQLExpr 的说明）；
// FROM / WHERE / GROUP BY / ORDER BY 与参数绑定全部归 GORM。
//
// 参数顺序（按 ? 在文本里出现的顺序）：paid_order_count 的状态名单、
// orderNetTotalSQLExpr 内部的退货状态名单、net_sales 的 FILTER 状态名单。
const orderBucketSelect = "COUNT(*) AS order_count, " +
	"COUNT(*) FILTER (WHERE o.status = ANY(string_to_array(?, ',')::text[])) AS paid_order_count, " +
	"COALESCE(SUM(" + orderNetTotalSQLExpr + ") FILTER (WHERE o.status = ANY(string_to_array(?, ',')::text[])), 0) AS net_sales"

// orderDailyBucketSelect 天的桶键 + 三个聚合列。
//
// 日的口径是 **UTC**（date_trunc 前先 AT TIME ZONE 'UTC'）：全站的按天聚合都是 UTC 的 day 桶
// （analytics 的按天统计就是），混用本地时区会让同一页面上两根柱子错开一个时区。
//
// 桶键的列名统一叫 `day`（小时粒度也叫 day，见 HourlyByRange 的说明）—— GORM 的
// `Group` 不接受 `Group("1")`（会被转义成 `"1"` 而 PG 报 column "1" does not exist），
// 所以必须按输出列名分组；两处同名才能共用同一个常量。
const orderDailyBucketSelect = "date_trunc('day', o.create_time AT TIME ZONE 'UTC')::date AS day, " + orderBucketSelect

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
		return tx.Table(OrderEntity{}.TableName()+" AS o").
			Select(orderDailyBucketSelect,
				strings.Join(paidStatuses, ","),
				strings.Join(ReturnedStatuses, ","),
				strings.Join(paidStatuses, ",")).
			Where("o.project_id = ?", projectID).
			Where("o.create_time >= ?", from).
			Where("o.create_time < ?", to).
			Group("day").
			Order("day").
			Scan(&rows).Error
	})
	return rows, err
}

// orderHourlySQL 区间内按小时聚合。
//
// 与 orderDailySQL 逐项同口径（状态名单、净额表达式、半开区间），只有 date_trunc 的
// 单位不同 —— 两份 SQL 的差异必须**只有这一处**，任何一处口径分叉都会让「区间汇总 =
// 各桶之和」这条不变量在某个粒度下失效，而失效的表现是图上柱子加起来对不上 KPI。
//
// 小时的桶**也是 UTC**：按天用 UTC 而按小时用本地时区，会让「今日」这根柱子归属到
// 昨天，且两个粒度切换时同一天的形状对不上。
//
// **`AT TIME ZONE 'UTC'` 必须写两遍**（第二遍不能省）：第一遍把 timestamptz 折成
// 「UTC 挂钟时间的 timestamp」（无时区），而驱动把无时区 timestamp 读成 `time.Time`
// 时会**按本地时区贴位置** —— 在本机（+08）读出来是 `2026-10-05T01:00+08`，
// `fillHourlyPoints` 拿它 `.Format(LayoutHour)` 得到 `2026-10-04T17:00`，
// 与循环生成的 `2026-10-05T01:00` 对不上 → **那一小时的订单静默变 0**。
// 第二遍把它标回 timestamptz，驱动给出的才是正确时刻。
//
// 对照：`orderDailySQL` 用 `::date`，返回 `date` 类型 —— 语义就是「某一天」，
// 驱动给 UTC 零点，`.Format(LayoutDay)` 正确。判据：**返回 `timestamp` 的聚合列
// 一定要贴回 UTC；`date` 不用**。这条由 `TestDayAndHourBucketsReturnUTCWakeClock`
// 钉住（两种粒度都断言读回来的桶键 UTC 挂钟 == 插入时刻的那个小时/那一天，
// 去掉第二遍、或把 `::date` 顺手改成 `date_trunc`，该测试都会变红）。
	const orderHourlyBucketSelect = "(date_trunc('hour', o.create_time AT TIME ZONE 'UTC') AT TIME ZONE 'UTC') AS day, " + orderBucketSelect

// HourlyByRange 取区间 [from, to) 内按小时聚合的订单数据。
//
// 复用 OrderDailyPoint：桶的语义从「某天」变成「某小时」，其余字段含义完全一致。
// 为此另开一个只有 gorm column 不同的类型，只会让调用方多一层无意义的转换。
func (m *OrderModel) HourlyByRange(ctx context.Context, projectID string, from, to time.Time) (rows []OrderDailyPoint, err error) {
	if strings.TrimSpace(projectID) == "" {
		return nil, ErrProjectRequired
	}
	if from.IsZero() || to.IsZero() {
		return nil, ErrRangeRequired
	}
	err = rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {
		return tx.Table(OrderEntity{}.TableName()+" AS o").
			Select(orderHourlyBucketSelect,
				strings.Join(paidStatuses, ","),
				strings.Join(ReturnedStatuses, ","),
				strings.Join(paidStatuses, ",")).
			Where("o.project_id = ?", projectID).
			Where("o.create_time >= ?", from).
			Where("o.create_time < ?", to).
			Group("day").
			Order("day").
			Scan(&rows).Error
	})
	return rows, err
}
