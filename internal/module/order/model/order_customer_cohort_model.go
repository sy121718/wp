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

// orderCohortCTEs 分群与群大小（三条 CTE）。
//
// 分群条件 `first_at >= ? AND first_at < ?` 与客户概览的「新客」**是同一个判据**
// （见 order_customer_growth_model.go 的 customerSegmentWhereNew）：本表所有群的
// 人数之和必须等于概览页的新客数。这是可对账的（测试钉住），也是唯一能发现
// 「两处口径分叉」的方式 —— 各写一份时把 `>=` 改成 `>` 不会让任何一处变红。
//
// 参数顺序（按 ? 出现顺序）：firsts 的 project_id / 状态名单，再是 cohort 的区间下界、上界。
const orderCohortCTEs = `WITH firsts AS (
    SELECT o.user_id, MIN(o.create_time) AS first_at
      FROM orders o
     WHERE o.project_id = ?
       AND o.status = ANY(string_to_array(?, ',')::text[])
       AND o.user_id IS NOT NULL
     GROUP BY o.user_id
),
cohort AS (
    SELECT f.user_id, date_trunc('month', f.first_at) AS cohort_month
      FROM firsts f
     WHERE f.first_at >= ?
       AND f.first_at < ?
),
sizes AS (
    SELECT c.cohort_month, COUNT(*) AS cohort_size
      FROM cohort c
     GROUP BY c.cohort_month
)`

// orderCohortRetentionSQL 每（群 × 月份）一格一行：该月活跃人数 + 本群人数。
//
// **为什么把 sizes 一起带出来**：分母（群人数）与分子（当月活跃人数）必须来自
// 同一次执行。分两次查的话，两次之间落到某个群的注册会改变分母，
// 于是同一行里的百分比与人数对不上，而页面上看不出哪一个是错的。
//
// 结果**稀疏**（没有活跃客户的月份不出行）：铺成矩阵是 service 的事 ——
// 让 SQL 生成完整矩阵需要与「观察窗终点」对齐的 generate_series，
// 而那个终点是展示决定（看几个月），不是数据事实。
//
// 参数顺序（按 ? 出现顺序）：见 orderCohortCTEs，随后是 active 的 project_id / 状态名单。
const orderCohortRetentionSQL = orderCohortCTEs + `,
active AS (
    SELECT c.cohort_month,
           date_trunc('month', o.create_time) AS activity_month,
           o.user_id
      FROM cohort c
      JOIN orders o ON o.user_id = c.user_id
     WHERE o.project_id = ?
       AND o.status = ANY(string_to_array(?, ',')::text[])
)
SELECT a.cohort_month,
       a.activity_month,
       COUNT(DISTINCT a.user_id) AS active_customers,
       s.cohort_size
  FROM active a
  JOIN sizes s ON s.cohort_month = a.cohort_month
 GROUP BY a.cohort_month, a.activity_month, s.cohort_size
 ORDER BY a.cohort_month, a.activity_month`

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
		return tx.Raw(orderCohortRetentionSQL,
			projectID, statuses,
			from, to,
			projectID, statuses).Scan(&rows).Error
	})
	return rows, err
}
