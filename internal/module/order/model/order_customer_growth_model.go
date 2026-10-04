package model

// order_customer_growth_model.go — 区间内的客户增长事实（新客 / 复购 / 回头客）。
//
// 为什么归订单模块：口径是「谁在这段时间下了单、他是不是第一次来」，事实全在 orders 表；
// 客户模块手里只有账号本身（注册时间、状态），读不到订单。口径来源是 Laravel CRM 的
// Reports/NewCustomersAnalysisService + RepurchaseAnalysisService —— **只搬口径不搬实现**：
// 那边按邮箱判人，这里一律按 user_id（同一个人换邮箱在那边会变成两个客户）。
//
// 三条口径（docs/17 §4.4）：
//   - 新客：**首次下单**落在区间内（不看注册时间；注册了但从没下单的不计入）。
//   - 复购：区间内下单 ≥ 2 单（同一区间内，不是历史累计）。
//   - 回头客：首次下单在区间之前（即「老客」）。
//
// 三条都只统计**计入消费的订单**（paidStatuses）：把取消/退款的单算进来，
// 「新客」会变成「注册完又反悔的人」，而那不是增长。

import (
	"context"
	"strings"
	"time"

	"gorm.io/gorm"

	"go_wp/pkg/rls"
)

// orderCustomerCTEs 区间客户口径的**唯一**定义（两个 CTE）。
//
// 抽成常量而不是让两个查询各写一份：客户概览页的「新客 12」与客户列表按「新客」筛出来的
// 条数必须相等（docs/17 §P7 的对账闸门）。各写一份 SQL 时，改一处滤条件（比如把
// `>=` 改成 `>`）不会让另一处变红，只会让两个页面的数字悄悄差一个人 ——
// 而差一个人是最难被发现的那种错。
//
// 参数顺序（按 ? 出现顺序）：ranged 的 project_id / from / to / 状态名单，
// 再是 firsts 的 project_id / 状态名单。
const orderCustomerCTEs = `WITH ranged AS (
    SELECT o.user_id, COUNT(*) AS order_count
      FROM orders o
     WHERE o.project_id = ?
       AND o.create_time >= ?
       AND o.create_time < ?
       AND o.status = ANY(string_to_array(?, ',')::text[])
       AND o.user_id IS NOT NULL
     GROUP BY o.user_id
),
firsts AS (
    SELECT o.user_id, MIN(o.create_time) AS first_at
      FROM orders o
     WHERE o.project_id = ?
       AND o.status = ANY(string_to_array(?, ',')::text[])
       AND o.user_id IS NOT NULL
     GROUP BY o.user_id
)`

// 三个分段条件（只有这三个，白名单常量拼进 SQL，不接受任何调用方字符串）。
//
// ranged 里 order_count 已经限定在区间内，firsts 里 first_at 是不限区间的首单时刻：
//   - new：首单落在区间内；
//   - returning：首单在区间之前（老客）；
//   - repurchasing：区间内下单 ≥ customerRepurchaseMinOrders。
//
// new / returning 条件各需要一个区间参数（上界）。repurchasing 的次数门槛不在这里
// 另写字面量：orderCustomerGrowthSQL 用 customerRepurchaseWhere 拼同一段条件。
const (
	customerSegmentWhereNew          = "f.first_at >= ?"
	customerSegmentWhereReturning    = "f.first_at < ?"
	customerSegmentWhereRepurchasing = "r.order_count >= 2"
)

// orderCustomerGrowthSQL 一条 SQL 取回区间内客户增长的五个数。
//
// **为什么一条而不是五条**：五个数描述的是同一批订单（区间内下单的人），
// 拆开之后两条之间落的新单会让「下单客户 40 人 / 新客 12 人 / 回头客 30 人」
// 这种自相矛盾的组合漏到页面上 —— 与区间订单摘要收敛成一条是同一个理由。
//
// **两个 CTE 的分工**：
//   - ranged：区间内每个客户下了几单（只算计入消费的、排除 user_id 为空的游客单）；
//   - firsts：每个客户的**首单时刻**（不限区间，扫该工程全部消费单）——
//     「是不是新客」只有把历史一起看了才能回答。
//
// 第二个 CTE 是全表范围内按客户分组，是本条里唯一可能贵的部分；它只需要
// (project_id, user_id, create_time) 上的索引即可走 index-only scan，
// 不需要读订单其它列。客户数到达十万级时应当改成按客户维度的预聚合表。
//
// **复购率的分子分母都在这里出**：分子 = 新客里复购的 + 老客下单的（老客只要下单
// 就算「回来的」，不必再复购一次），分母 = 区间内下单客户数。这个式子写在 service 里
// 一处，页面与 AI 都不重算（两个消费方各算一次必然分叉）。
//
// 参数顺序（按 ? 出现顺序）：见 orderCustomerCTEs，随后三个 FILTER 的区间上界各一次。
//
// `customerRepurchaseWhere(0)` 在包初始化时求值（纯函数，回落默认门槛）：
// 「什么算复购」因此与列表筛选共用同一个定义，而不是两处各写一个 2。
var orderCustomerGrowthSQL = orderCustomerCTEs + `
SELECT COUNT(*) AS ordering_customers,
       COUNT(*) FILTER (WHERE f.first_at >= ?) AS new_customers,
       COUNT(*) FILTER (WHERE f.first_at < ? AND ` + customerRepurchaseWhere(0) + `) AS new_repurchasers,
       COUNT(*) FILTER (WHERE ` + customerRepurchaseWhere(0) + `) AS repurchasers,
       COUNT(*) FILTER (WHERE f.first_at < ?) AS returning_customers
  FROM ranged r
  JOIN firsts f ON f.user_id = r.user_id`

// OrderCustomerGrowthRow 区间内的客户增长事实。
//
// 五个数满足一组恒等式（测试钉住）：NewCustomers + ReturningCustomers == OrderingCustomers
// （每个人要么首单在区间内、要么在区间之前），且 NewRepurchasers <= Repurchasers。
type OrderCustomerGrowthRow struct {
	// OrderingCustomers 区间内下过消费单的客户数（分母）。
	OrderingCustomers int64 `gorm:"column:ordering_customers"`
	// NewCustomers 首单落在区间内的客户数。
	NewCustomers int64 `gorm:"column:new_customers"`
	// NewRepurchasers 新客里在区间内下了 ≥2 单的（复购率的分子之一）。
	NewRepurchasers int64 `gorm:"column:new_repurchasers"`
	// Repurchasers 区间内下了 ≥2 单的客户总数（不分新老）。
	Repurchasers int64 `gorm:"column:repurchasers"`
	// ReturningCustomers 首单在区间之前、区间内又下单的客户数（复购率的分子之二）。
	ReturningCustomers int64 `gorm:"column:returning_customers"`
}

// CustomerGrowthByRange 取区间 [from, to) 内的客户增长事实。
//
// projectID 必填、区间必填：orders 带 FORCE 策略，不设作用域在非超级角色下静默返回空集 ——
// 概览页会显示一片 0，而 0 会被当成真实统计（「这段时间一个客户都没有」）。
func (m *OrderModel) CustomerGrowthByRange(ctx context.Context, projectID string, from, to time.Time) (row OrderCustomerGrowthRow, err error) {
	if strings.TrimSpace(projectID) == "" {
		return row, ErrProjectRequired
	}
	if from.IsZero() || to.IsZero() {
		return row, ErrRangeRequired
	}
	statuses := strings.Join(paidStatuses, ",")
	err = rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {
		return tx.Raw(orderCustomerGrowthSQL,
			projectID, from, to, statuses,
			projectID, statuses,
			from, from, from).Scan(&row).Error
	})
	return row, err
}
