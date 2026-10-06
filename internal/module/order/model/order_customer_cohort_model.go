package model

// order_customer_cohort_model.go — 群组留存（按首单月份分群 + 留存矩阵）。
//
// 口径来源 Laravel CRM 的 Reports/CohortAnalysisService —— **只搬口径不搬实现**，
// 而且那一条里有**两处实现细节我不搬，理由都写在下面**（不写清楚的话，下一个人
// 会以为这里漏抄了）。
//
// 搬过来的口径（docs/17 §4.4）：分群 = **首单落在查询区间内**的客户，按**首单所在自然月**
// 分组；矩阵的取值 = 该群在第 N 个月**有购买**的人数。
//
// 不搬的第一处 —— **分母用本群人数，不是全体人数**：CRM 在算群内留存率时用的是本群
// 总人数（`$cohort['total_customers']`），但最后输出 `cohort_table.retention_rates`
// 时换成了全体客户数（`$totalCustomers = count($customers)`）。同一个文件里两个分母，
// 后果是每个群的留存率都被同一批人稀释：越早的群数字越小，看起来像「留存一月不如一月」，
// 实际只是群大小不同。这里一律用本群人数。
//
// 不搬的第二处 —— **不要求连续活跃**：CRM 用「活跃客户队列」逐月衰减
// （上月没买的从队列里移除），于是第 3 个月留存 = 1、2、3 月**都**买过的人。
// 那不是行业口径下的留存矩阵（标准口径只问「第 3 个月有没有来」），并且会让
// 第 3 个月的留存率显著偏低，而运营会把它读成「三个月后只剩这么点人」。
// 这里按标准口径：每个格只看**那一个月**有没有下单，与中间月份无关。

import (
	"context"
	"strings"
	"time"

	"gorm.io/gorm"

	"go_wp/pkg/rls"
)

// orderCohortScope 分群与群大小（两条子查询，sizes 依赖 cohort）。
//
// 分群条件 `first_at >= ? AND first_at < ?` 与客户概览的「新客」**是同一个判据**
// （见 order_customer_growth_model.go 的 customerSegmentWhereNew）：本表所有群的
// 人数之和必须等于概览页的新客数。这是可对账的（测试钉住），也是唯一能发现
// 「两处口径分叉」的方式 —— 各写一份时把 `>=` 改成 `>` 不会让任何一处变红。
//
// **为什么是子查询而不是 WITH**：本仓不准写裸 SQL，必须走 GORM 链式，而 GORM 没有
// CTE 构造；`Table("(?) AS x", sub)` 展开成子查询在语义上与 CTE 等价（都只被引用一次）。
// 代价是 sizes 内层会把 cohort 的子查询再展开一遍 —— PG 12+ 会 inline 掉，
// 而口径仍然只有本函数一处定义。
func orderCohortScope(tx *gorm.DB, projectID string, from, to time.Time) (cohort, sizes *gorm.DB) {
	statuses := strings.Join(paidStatuses, ",")
	firsts := tx.Table(OrderEntity{}.TableName()+" AS o").
		Select("o.user_id, MIN(o.create_time) AS first_at").
		Where("o.project_id = ?", projectID).
		Where("o.status = ANY(string_to_array(?, ',')::text[])", statuses).
		Where("o.user_id IS NOT NULL").
		Group("o.user_id")
	cohort = tx.Table("(?) AS f", firsts).
		// 月份桶必须**贴回 UTC**（两遍 AT TIME ZONE）：`date_trunc` 返回的是无时区
		// timestamp，驱动读成 time.Time 时按本机时区贴位置 —— 于是「2026-10-01 00:00 UTC」
		// 会被读成「2026-10-01 00:00 +08」= 09-30 16:00 UTC，群归属整整偏一个月。
		// 这与 page_views / orders 的按天按小时桶是同一条判据（AGENTS.md「聚合口径」）。
		Select("f.user_id, (date_trunc('month', f.first_at AT TIME ZONE 'UTC') AT TIME ZONE 'UTC') AS cohort_month").
		Where("f.first_at >= ?", from).
		Where("f.first_at < ?", to)
	sizes = tx.Table("(?) AS c", cohort).
		Select("c.cohort_month, COUNT(*) AS cohort_size").
		Group("c.cohort_month")
	return cohort, sizes
}

// orderCohortActivitySelect 每（群 × 月份）一格的取数列。
//
// **为什么把 sizes 一起带出来**：分母（群人数）与分子（当月活跃人数）必须来自
// 同一次执行。分两次查的话，两次之间落到某个群的注册会改变分母，
// 于是同一行里的百分比与人数对不上，而页面上看不出哪一个是错的。
const orderCohortActivitySelect = "a.cohort_month, a.activity_month, " +
	"COUNT(DISTINCT a.user_id) AS active_customers, s.cohort_size"

// OrderCohortRetentionRow 群组留存矩阵的一个格。
//
// CohortMonth / ActivityMonth 都是**月首的 timestamp**（`date_trunc('month', …)` 的结果），
// 不是日期：把两个时刻相减得到相差几个月是可以直接做的（都是月首、且 UTC）。
type OrderCohortRetentionRow struct {
	// CohortMonth 该客户首单所在的自然月（月首）。
	CohortMonth time.Time `gorm:"column:cohort_month"`
	// ActivityMonth 有购买行为的那个自然月（月首）。
	ActivityMonth time.Time `gorm:"column:activity_month"`
	// ActiveCustomers 该群在 ActivityMonth 当月下过消费单的人数。
	ActiveCustomers int64 `gorm:"column:active_customers"`
	// CohortSize 该群的总人数（分母，每个格都一样）。
	CohortSize int64 `gorm:"column:cohort_size"`
}

// CohortRetention 取区间 [from, to) 内首单的客户的群组留存明细（稀疏格）。
//
// projectID 必填、区间必填：orders 带 FORCE 策略，不设作用域在非超级角色下静默返回空集 ——
// 页面会显示「这段时间没有新客户」，而那是错的结论（真实情况是没设作用域）。
func (m *OrderModel) CohortRetention(ctx context.Context, projectID string, from, to time.Time) (rows []OrderCohortRetentionRow, err error) {
	if strings.TrimSpace(projectID) == "" {
		return nil, ErrProjectRequired
	}
	if from.IsZero() || to.IsZero() {
		return nil, ErrRangeRequired
	}
	statuses := strings.Join(paidStatuses, ",")
	err = rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {
		cohort, sizes := orderCohortScope(tx, projectID, from, to)
		active := tx.Table("(?) AS c", cohort).
			// activity_month 同样要贴回 UTC（理由见上面 cohort_month）。
			// 两处必须用同一个口径：一个是群月份、一个是活跃月份，两者相减得到「第 N 个月」，
			// 口径不一致时留存矩阵会整体错位一格。
			Select("c.cohort_month, (date_trunc('month', o.create_time AT TIME ZONE 'UTC') AT TIME ZONE 'UTC') AS activity_month, o.user_id").
			Joins("JOIN "+OrderEntity{}.TableName()+" AS o ON o.user_id = c.user_id").
			Where("o.project_id = ?", projectID).
			Where("o.status = ANY(string_to_array(?, ',')::text[])", statuses)
		// 结果**稀疏**（没有活跃客户的月份不出行）：铺成矩阵是 service 的事 ——
		// 让 SQL 生成完整矩阵需要与「观察窗终点」对齐的 generate_series，
		// 而那个终点是展示决定（看几个月），不是数据事实。
		return tx.Table("(?) AS a", active).
			Joins("JOIN (?) AS s ON s.cohort_month = a.cohort_month", sizes).
			Select(orderCohortActivitySelect).
			Group("a.cohort_month, a.activity_month, s.cohort_size").
			Order("a.cohort_month, a.activity_month").
			Scan(&rows).Error
	})
	return rows, err
}
