package model

// order_sales_overview_model.go — 「销售概览」的取数（订单数 / 明细行数 / 件数 / 销售额 / 下单客户数）。
//
// 口径来源是 Laravel CRM 的 Reports/SalesProductSummary + UnitsOrdered（那边 sales dashboard
// 的那排卡片）—— **只搬口径不搬实现**：那边把「订单商品行数」与「销售件数」拆成两个接口
// 分两次拿，两次之间落的单会让「平均每单 1.8 件」这种派生值自相矛盾；这里一条查询一次取回。
//
// **全部走 GORM 链式，不写裸 SQL**（本仓 model 层规则：查询用 GORM 表达，
// 聚合表达式放进 Select 的字符串参数里，FROM / JOIN / WHERE 由 GORM 拼）。
// 表名从不写字面量：一律走 `XxxEntity{}.TableName()`，表改名时跟着走。

import (
	"context"
	"strings"
	"time"

	"gorm.io/gorm"

	"go_wp/pkg/rls"
)

// 销售概览用的表与联接（表名取自 Entity，不写字面量）。
//
// 用 var 而不是 const：`OrderEntity{}.TableName()` 是函数调用，不是常量表达式。
var (
	orderSalesFrom = OrderEntity{}.TableName() + " AS o"
	orderSalesJoin = "LEFT JOIN " + OrderItemEntity{}.TableName() + " i ON i.order_id = o.id"
)

// orderSalesOverviewSelect 一条查询取回销售概览的五个数。
//
// **为什么 LEFT JOIN 而不是 INNER**：`orders` 里存在没有明细行的订单（建单时商品
// 全部下架、或历史脏数据）。用 INNER 时这些订单会**从订单数里消失**，页面上
// 「订单数 12 / 明细 0 行」变成「订单数 11」—— 少一单是查不出来的那种错。
// COUNT(DISTINCT o.id) 同时挡住了 JOIN 造成的行放大（一单 5 行明细不会算成 5 单）。
//
// **金额口径是行实付合计**（Σ order_items.line_total），**不含退款分摊** ——
// 与热销榜同口径、与「净销售额」（订单级减已收货退款）不同名，
// 避免运营以为其中一个算错。
//
// **下单客户数按 user_id 去重**（游客单 user_id 为空，不计入）：与客户增长那套
// 口径同一个判据，所以卡片上的「下单客户 8 人」与客户区块的「下单客户」会相等。
const orderSalesOverviewSelect = `COUNT(DISTINCT o.id) AS order_count,
       COUNT(i.id) AS item_rows,
       COALESCE(SUM(i.quantity), 0) AS units,
       COALESCE(SUM(i.line_total), 0) AS sales,
       COUNT(DISTINCT o.user_id) AS customers`

// orderSalesMonthlyInnerSelect 月度趋势的**内层**投影（逐行：月桶 + 金额 + 客户类型）。
//
// 月桶用 `::date` 而不是 `date_trunc(...)` 直接返回：`date_trunc` 返回**无时区
// timestamp**，驱动按本地时区贴位置会让桶键整体偏一个时区（AGENTS.md
// 「桶键读回来必须还是同一个时刻」）。`::date` 的语义就是「某一天」，
// 驱动给 UTC 零点，`.Format` 出来就是 2026-10-01。
//
// 新客 / 回头客 / 游客的拆分见 orderSalesMonthlyOuterSelect —— 客户类型要在
// **内层子查询**里算一次（orderSalesCustomerKind），外层只按它分组聚合：
// 把 CASE 表达式在三段聚合里各抄一遍，改一处漏两处时三段之和就不再等于总量。
const orderSalesMonthlyInnerSelect = `(date_trunc('month', o.create_time AT TIME ZONE 'UTC'))::date AS ` + orderSalesMonthColumn + `,
       o.id AS order_id,
       o.user_id AS user_id,
       i.line_total AS line_total,
       ` + orderSalesCustomerKind + ` AS customer_kind`

// orderSalesMonthlyOuterSelect 按客户类型拆分的月度聚合。
//
// **三段之和恒等于总量**（new + returning + guest = total），这是本页唯一一条
// 「拆开的数字必须能拼回去」的判据（AGENTS.md「这条要有会变红的判据」）：
// 判据是 TestMonthlyCustomerMixSumsToTotal。
//
// **为什么要 guest 这一段**：本仓按 `user_id` 认客户（游客单 user_id 为空），
// 而 laravel CRM 那边按邮箱分组、每个人都有邮箱，所以它只有 new / returning 两段。
// 本仓若也只留两段，游客单的销售额就**无处可去** —— 表现为两段之和小于总额，
// 而每一段单独看都对（这正是「同一张页面上的两个数字必须同源」要防的形状）。
const orderSalesMonthlyOuterSelect = orderSalesMonthColumn + `,
       COUNT(DISTINCT order_id) AS order_count,
       COALESCE(SUM(line_total), 0) AS sales,
       COUNT(DISTINCT user_id) AS customers,
       COUNT(DISTINCT CASE WHEN customer_kind = 'new' THEN order_id END) AS new_order_count,
       COUNT(DISTINCT CASE WHEN customer_kind = 'returning' THEN order_id END) AS returning_order_count,
       COUNT(DISTINCT CASE WHEN customer_kind = 'guest' THEN order_id END) AS guest_order_count,
       COALESCE(SUM(CASE WHEN customer_kind = 'new' THEN line_total ELSE 0 END), 0) AS new_sales,
       COALESCE(SUM(CASE WHEN customer_kind = 'returning' THEN line_total ELSE 0 END), 0) AS returning_sales,
       COALESCE(SUM(CASE WHEN customer_kind = 'guest' THEN line_total ELSE 0 END), 0) AS guest_sales,
       COUNT(DISTINCT CASE WHEN customer_kind = 'new' THEN user_id END) AS new_customers,
       COUNT(DISTINCT CASE WHEN customer_kind = 'returning' THEN user_id END) AS returning_customers`

// orderSalesCustomerKind 客户类型的判据（new / returning / guest）。
//
// **新客 = 这个客户的首单落在与当前订单同一个 UTC 月内**（口径搬自 laravel CRM 的
// MonthlyTrendService::calculateMonthStats：「首购时间是否在本月」）。
//
// 与那边的**唯一差别**：那边用「区间裁剪后的当月起止」判（查 10-05~11-20 时，
// 首购在 10-01 的人不算 10 月新客），本仓用**自然月**判。取自然月是因为前者会让
// 同一个客户的新老身份随**查询区间**漂移 —— 同一个人查长区间是新客、查短区间变回头客，
// 而「一个人是不是新客」与运营把日期框拉到哪里无关。
//
// `f.first_at IS NULL` 只可能是**游客单**（user_id 为空，LEFT JOIN 不命中）——
// 有账号的人一定有首单（当前这一单本身就是）。
const orderSalesCustomerKind = `CASE
           WHEN f.first_at IS NULL THEN 'guest'
           WHEN date_trunc('month', f.first_at AT TIME ZONE 'UTC') = date_trunc('month', o.create_time AT TIME ZONE 'UTC') THEN 'new'
           ELSE 'returning'
       END`

// orderSalesMonthColumn 月度趋势的桶列别名。
//
// 单独抽出来是因为 GROUP BY / ORDER BY 要用它：GORM 的 `Group("1")` 会被转义成
// `"1"`（PG 读成「名为 1 的列不存在」），而把整条 date_trunc 表达式在 Select 与
// Group 里各写一遍，是两处必须手工保持同步的事实。按输出列名分组就只剩一处。
const orderSalesMonthColumn = "month"

// OrderSalesFilter 销售概览的可选筛选（只有订单状态一档）。
//
// 零值 = 不限：Statuses 为空时用 paidStatuses（计入消费的那三个状态）。
// 白名单由调用方（service）负责，model 只执行 —— 状态名走参数绑定没有注入面，
// 但传一个不存在的状态名会静默返回全 0。
type OrderSalesFilter struct {
	// Statuses 计入统计的订单状态；为空时用 paidStatuses。
	Statuses []string
}

// statusesText 生效的状态名单（逗号串）。
func (f OrderSalesFilter) statusesText() string {
	if len(f.Statuses) == 0 {
		return strings.Join(paidStatuses, ",")
	}
	return strings.Join(f.Statuses, ",")
}

// OrderSalesOverviewRow 销售概览的五个原始事实（一条查询的结果行）。
//
// 五个都是**计数或求和**，没有一个派生值（AOV / ACV / 平均每件）：
// 派生值全部在 service 算一次，页面与 AI 都不重算 —— 两个消费方各算一次必然分叉。
type OrderSalesOverviewRow struct {
	// OrderCount 区间内的订单数（含没有明细行的订单）。
	OrderCount int64 `gorm:"column:order_count"`
	// ItemRows 明细行数（一单多行各算一次）。
	ItemRows int64 `gorm:"column:item_rows"`
	// Units 售出件数（Σ quantity）。
	Units int64 `gorm:"column:units"`
	// Sales 行实付合计（分，不含退款分摊）。
	Sales int64 `gorm:"column:sales"`
	// Customers 下单客户数（按 user_id 去重，不含游客单）。
	Customers int64 `gorm:"column:customers"`
}

// OrderSalesMonthlyRow 月度趋势的一个桶。
type OrderSalesMonthlyRow struct {
	// Month 桶键，UTC 月的第一天（如 2026-10-01）。
	Month time.Time `gorm:"column:month"`
	// OrderCount 该月的订单数。
	OrderCount int64 `gorm:"column:order_count"`
	// Sales 该月的行实付合计（分）。
	Sales int64 `gorm:"column:sales"`
	// Customers 该月的下单客户数（按 user_id 去重，不含游客单）。
	Customers int64 `gorm:"column:customers"`

	// ── 按客户类型的拆分（三段之和恒等于上面的总量）──────────────────
	//
	// 「新客」= 该客户的**首单**落在这个月（自然月，见 orderSalesCustomerKind）。
	// 「游客」= 没有账号的订单（user_id 为空），它既不是新客也不是回头客，
	// 但它的钱必须出现在某一段里 —— 否则两段之和小于总量。

	// NewOrderCount 该月新客订单数。
	NewOrderCount int64 `gorm:"column:new_order_count"`
	// ReturningOrderCount 该月回头客订单数。
	ReturningOrderCount int64 `gorm:"column:returning_order_count"`
	// GuestOrderCount 该月游客订单数。
	GuestOrderCount int64 `gorm:"column:guest_order_count"`
	// NewSales 该月新客订单的行实付合计（分）。
	NewSales int64 `gorm:"column:new_sales"`
	// ReturningSales 该月回头客订单的行实付合计（分）。
	ReturningSales int64 `gorm:"column:returning_sales"`
	// GuestSales 该月游客订单的行实付合计（分）。
	GuestSales int64 `gorm:"column:guest_sales"`
	// NewCustomers 该月下过单的新客数。
	NewCustomers int64 `gorm:"column:new_customers"`
	// ReturningCustomers 该月下过单的回头客数。
	ReturningCustomers int64 `gorm:"column:returning_customers"`
}

// salesScope 应用销售概览与月度趋势**共用**的筛选条件。
//
// 抽成一个函数而不是让两个查询各写一份：卡片上的「销售额」与趋势图上那条线必须同源
// （AGENTS.md「同一张页面上的两个数字必须同源」）。各写一份 WHERE 的失败模式是
// 卡片排除了取消单、趋势图忘了排除 —— 两个数字摆在同一个页面上互相矛盾。
func salesScope(q *gorm.DB, projectID string, from, to time.Time, f OrderSalesFilter) *gorm.DB {
	return q.
		Where("o.project_id = ?", projectID).
		Where("o.create_time >= ?", from).
		Where("o.create_time < ?", to).
		Where("o.status = ANY(string_to_array(?, ',')::text[])", f.statusesText())
}

// SalesOverviewByRange 取区间 [from, to) 内的销售概览事实。
//
// projectID 必填、区间必填：orders 带 FORCE 策略，不设作用域在非超级角色下静默返回空集 ——
// 页面会显示一片 0，而 0 会被当成真实统计（「这段时间一单都没有」）。
func (m *OrderModel) SalesOverviewByRange(ctx context.Context, projectID string, from, to time.Time, f OrderSalesFilter) (row OrderSalesOverviewRow, err error) {
	if strings.TrimSpace(projectID) == "" {
		return row, ErrProjectRequired
	}
	if from.IsZero() || to.IsZero() {
		return row, ErrRangeRequired
	}
	err = rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {
		q := tx.Table(orderSalesFrom).Joins(orderSalesJoin).Select(orderSalesOverviewSelect)
		return salesScope(q, projectID, from, to, f).Scan(&row).Error
	})
	return row, err
}

// SalesMonthlyByRange 取区间 [from, to) 内按 UTC 月聚合的销售趋势（只回出现过的月）。
//
// 补零由 service 做：调用方要的是「区间内每个月一个点」，而「区间里有哪几个月」
// 依赖时区与边界，正是最容易分叉的一步（与逐日序列同一个理由）。
//
// 排序显式写出来：不写 ORDER BY 时 PG 的行序不保证，趋势图的折线会随机抖动，
// 测试也会偶发。
//
// **首单子查询不受区间裁剪**（`firsts` 只带 project_id，没有 from/to）：判新客看的是
// 「这个人的第一次下单」，区间一裁，区间内的第一单就会被误判成新客，
// 于是「近半年」与「近一个月」里的同一个客户会得到两种身份。
// 别名用 `f0` 而不是 `o`：外层 FROM 已经是 `orders AS o`，同名会让 PG 报
// `table name "o" specified more than once`（子查询不构成独立的名字空间）。
func (m *OrderModel) SalesMonthlyByRange(ctx context.Context, projectID string, from, to time.Time, f OrderSalesFilter) (rows []OrderSalesMonthlyRow, err error) {
	if strings.TrimSpace(projectID) == "" {
		return nil, ErrProjectRequired
	}
	if from.IsZero() || to.IsZero() {
		return nil, ErrRangeRequired
	}
	err = rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {
		// 每个客户在本工程**全历史**的首单时刻；游客单（user_id 为空）不进这张表，
		// 它们在下面按 LEFT JOIN 未命中的那一支归到 guest。
		firsts := tx.Table(OrderEntity{}.TableName()+" AS f0").
			Select("f0.user_id AS user_id, MIN(f0.create_time) AS first_at").
			Where("f0.project_id = ?", projectID).
			// user_id 是 bigint 且可空（游客单为 NULL）：只能判 IS NOT NULL，
			// 不能写成 `<> ''` —— bigint 与空串比较是**运行期类型错**（SQLSTATE 22P02），
			// 而且只有真跑到这条查询才会暴露（编译与单测都过）。
			Where("f0.user_id IS NOT NULL").
			Group("f0.user_id")

		// 内层：逐行算出「月桶 + 金额 + 客户类型」，客户类型只在这里算**一次**。
		inner := salesScope(
			tx.Table(orderSalesFrom).
				Joins(orderSalesJoin).
				Joins("LEFT JOIN (?) AS f ON f.user_id = o.user_id", firsts).
				Select(orderSalesMonthlyInnerSelect),
			projectID, from, to, f,
		)

		// 外层：按桶聚合。Table("(?) AS t", inner) 是 GORM 表达 FROM 子查询的写法；
		// WHERE 留在内层（口径与卡片共用 salesScope），外层只负责聚合与排序。
		return tx.Table("(?) AS t", inner).
			Select(orderSalesMonthlyOuterSelect).
			// 按**输出列名**分组，不写 `Group("1")`：GORM 会把裸数字转义成 `"1"`，
			// 而 PG 读成「名为 1 的列不存在」（实测 SQLSTATE 42703）。
			Group(orderSalesMonthColumn).
			Order(orderSalesMonthColumn).
			Scan(&rows).Error
	})
	return rows, err
}
