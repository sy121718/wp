package model

// order_customer_rfm_model.go — 客户 RFM 分层（R 最近下单 / F 窗口内频次 / M 窗口内消费额）。
//
// 口径出自 Laravel CRM 的 Reports/RfmAnalysisService，**只搬口径不搬实现**：
//   - 那边按邮箱判人，这里一律按 user_id（同一个人换邮箱在那边会变成两个客户）；
//   - 那边的 M 是欧元尺度的**固定阈值**（≥300 / 180-299 / …），这里一律用
//     **本项目数据的五分位** —— docs/17 §4.4 明确要求金额阈值按本项目客单价重定。
//     固定阈值在客单价差一个数量级的站点上会把所有人打成同一档，
//     而那种结果看起来完全正常（「大多数客户都是 m1，说明客单价低」）。
//
// 三段各自打分（1-5），总分 3-15，分段与 CRM 同口径：
// total >= 12 为 vip，>= 8 为 potential，其余 low_value。
//
// R 与 F/M 的窗口**不一致，且必须不一致**（照搬 CRM）：
//   - R 取**全历史**最后下单时刻到窗口结束的天数 —— 「他多久没来了」与区间无关；
//   - F / M 只算窗口内的单 —— 「这段时间他来了几次、花了多少」。
//   把 R 也限制在窗口内，会变成「窗口内没下单的人 R 无穷大」，
//   而那正是这个指标要区分的东西。
//
// 只取**窗口内下过单**的人（freq > 0）：从未下单的账号不是这份报表的对象
//（他们该出现在客户列表，而不是 RFM 分层里）。

import (
	"context"
	"strings"
	"time"

	"gorm.io/gorm"

	"go_wp/pkg/rls"
)

// rfmScoredCTE RFM 的打分主体（两个 CTE）。
//
// **五分位而不是固定阈值**：NTILE(5) 让每一档在**本项目的实际分布**上落位，
// 客单价变了分档跟着变。这也是 docs/17 要求「金额阈值按本项目客单价重定」的落地方式。
//
// NTILE 的 ORDER BY 都带 user_id 兜底：分数相同的行之间若没有稳定次序，
// PostgreSQL 每次可以给出不同的分档 —— 表现是「同一个人刷新两次分数不一样」，
// 而那不会有任何报错。
//
// R 取 `6 - NTILE(...)`：NTILE 在 `ORDER BY last_at DESC` 下给最近的人 1，
// 而 RFM 的语义是「越近分越高」，所以要翻过来。
//
// 参数顺序（**按 ? 在 SQL 文本里的出现顺序，不是按子句顺序**）：
// ① ② F 的窗口起止 → ③ 净额表达式里的退货状态名单 → ④ ⑤ M 的窗口起止
// → ⑥ project_id → ⑦ 订单状态名单。用 orderCustomerRfmArgs 统一构造，
// 调用方不要手写这个切片：参数错位不报错，只是数字全错。
const rfmScoredCTE = `WITH agg AS (
    SELECT o.user_id,
           MAX(o.create_time) AS last_at,
           COUNT(*) FILTER (WHERE o.create_time >= ? AND o.create_time < ?) AS freq,
           COALESCE(SUM(` + orderNetTotalSQLExpr + `) FILTER (WHERE o.create_time >= ? AND o.create_time < ?), 0) AS monetary
      FROM orders o
     WHERE o.project_id = ?
       AND o.status = ANY(string_to_array(?, ',')::text[])
       AND o.user_id IS NOT NULL
     GROUP BY o.user_id
),
scored AS (
    SELECT a.user_id, a.last_at, a.freq, a.monetary,
           6 - NTILE(5) OVER (ORDER BY a.last_at DESC, a.user_id) AS r_score,
           NTILE(5) OVER (ORDER BY a.freq ASC, a.user_id) AS f_score,
           NTILE(5) OVER (ORDER BY a.monetary ASC, a.user_id) AS m_score
      FROM agg a
     WHERE a.freq > 0
)`

// rfmSegmentExpr 总分 → 分段（与 CRM 的 classifySegment 同阈值）。
const rfmSegmentExpr = `CASE WHEN (r_score + f_score + m_score) >= 12 THEN 'vip'
                             WHEN (r_score + f_score + m_score) >= 8 THEN 'potential'
                             ELSE 'low_value' END`

// orderCustomerRfmArgs 按 rfmScoredCTE 的 ? 顺序构造参数（见该常量的参数顺序注释）。
func orderCustomerRfmArgs(projectID string, from, to time.Time) []any {
	return []any{
		from, to, // ① F 的窗口
		strings.Join(ReturnedStatuses, ","), // ③ 净额表达式里的退货状态
		from, to,                            // ④ M 的窗口
		projectID,                       // ⑥
		strings.Join(paidStatuses, ","), // ⑦
	}
}

// RFM 分段名（与 CRM 的 classifySegment 同阈值口径）。
const (
	RfmSegmentVip       = "vip"
	RfmSegmentPotential = "potential"
	RfmSegmentLowValue  = "low_value"
)

// IsKnownRfmSegment 分段名的白名单判定（三个值，其余一律拒）。
//
// 抽成函数而不是在三处各写一遍字符串比较：新增一个分段时漏改一处，
// 表现是「那个分段在列表页筛不出来」，而没有任何报错。
func IsKnownRfmSegment(segment string) bool {
	switch segment {
	case RfmSegmentVip, RfmSegmentPotential, RfmSegmentLowValue:
		return true
	}
	return false
}

// OrderCustomerRfmRow 一个客户的 RFM 明细。
type OrderCustomerRfmRow struct {
	UserID     int64     `gorm:"column:user_id"`
	LastAt     time.Time `gorm:"column:last_at"`
	Freq       int64     `gorm:"column:freq"`
	Monetary   int64     `gorm:"column:monetary"`
	RScore     int       `gorm:"column:r_score"`
	FScore     int       `gorm:"column:f_score"`
	MScore     int       `gorm:"column:m_score"`
	TotalScore int       `gorm:"column:total_score"`
	Segment    string    `gorm:"column:segment"`
}

// OrderCustomerRfmSegmentRow 分段计数（报表头部的三格）。
type OrderCustomerRfmSegmentRow struct {
	Customers int64 `gorm:"column:customers"`
	Vip       int64 `gorm:"column:vip"`
	Potential int64 `gorm:"column:potential"`
	LowValue  int64 `gorm:"column:low_value"`
}

// rfmRowColumns 明细查询的列清单（统计与明细共用同一套打分列定义）。
const rfmRowColumns = ` user_id, last_at, freq, monetary, r_score, f_score, m_score,
       (r_score + f_score + m_score) AS total_score,
       ` + rfmSegmentExpr + ` AS segment`

// CustomerRfmSummary 分段计数（一次全量聚合，不受分页影响）。
func (m *OrderModel) CustomerRfmSummary(ctx context.Context, projectID string, from, to time.Time) (row OrderCustomerRfmSegmentRow, err error) {
	if strings.TrimSpace(projectID) == "" {
		return row, ErrProjectRequired
	}
	if from.IsZero() || to.IsZero() {
		return row, ErrRangeRequired
	}
	sql := rfmScoredCTE + `
SELECT COUNT(*) AS customers,
       COUNT(*) FILTER (WHERE segment = 'vip') AS vip,
       COUNT(*) FILTER (WHERE segment = 'potential') AS potential,
       COUNT(*) FILTER (WHERE segment = 'low_value') AS low_value
  FROM (SELECT ` + rfmRowColumns + ` FROM scored) s`
	err = rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {
		return tx.Raw(sql, orderCustomerRfmArgs(projectID, from, to)...).Scan(&row).Error
	})
	return row, err
}

// CustomerRfmList 客户 RFM 明细（按总分降序分页；segment 非空时只看那一段）。
//
// segment 只接受白名单里的三个值（由 service 判定）；这里再断言一次是为了让
// 「拼进来的字符串」在 model 层也不可能出现 —— SQL 片段拼接是本文件唯一
// 直接拼字符串的地方，把入口收窄到三个常量是它的安全边界。
//
// 总数用 COUNT(*) OVER ()：与分页窗口来自同一次执行。RFM 的打分是相对的（五分位），
// 两次执行之间来了新客户会改变分档 —— 拆成两条查询时，「共 12 人」与列出来的 12 行
// 可能属于两套不同的分档。
func (m *OrderModel) CustomerRfmList(ctx context.Context, projectID string, from, to time.Time, segment string, limit, offset int) (rows []OrderCustomerRfmRow, total int64, err error) {
	if strings.TrimSpace(projectID) == "" {
		return nil, 0, ErrProjectRequired
	}
	if from.IsZero() || to.IsZero() {
		return nil, 0, ErrRangeRequired
	}
	if segment != "" && !IsKnownRfmSegment(segment) {
		return nil, 0, ErrSegmentUnknown
	}
	if limit <= 0 {
		return nil, 0, ErrRangeRequired
	}
	sql := rfmScoredCTE + `
SELECT` + rfmRowColumns + `, COUNT(*) OVER () AS total
  FROM scored`
	args := orderCustomerRfmArgs(projectID, from, to)
	if segment != "" {
		// 分段是**结果列**，不能在 WHERE 里直接引用（SQL 不允许）—— 包一层子查询。
		sql = rfmScoredCTE + `
SELECT *, COUNT(*) OVER () AS total
  FROM (SELECT ` + rfmRowColumns + ` FROM scored) s
 WHERE segment = ?`
		args = append(args, segment)
	}
	sql += ` ORDER BY total_score DESC, user_id LIMIT ? OFFSET ?`
	args = append(args, limit, offset)

	// 显式列全部字段（不用嵌入 struct）：GORM 对嵌入字段的列名推断依赖命名策略，
	// 在这里多一层猜测没有收益，而猜错的后果是某一列永远是零值。
	type listRow struct {
		UserID     int64     `gorm:"column:user_id"`
		LastAt     time.Time `gorm:"column:last_at"`
		Freq       int64     `gorm:"column:freq"`
		Monetary   int64     `gorm:"column:monetary"`
		RScore     int       `gorm:"column:r_score"`
		FScore     int       `gorm:"column:f_score"`
		MScore     int       `gorm:"column:m_score"`
		TotalScore int       `gorm:"column:total_score"`
		Segment    string    `gorm:"column:segment"`
		Total      int64     `gorm:"column:total"`
	}
	var raw []listRow
	err = rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {
		return tx.Raw(sql, args...).Scan(&raw).Error
	})
	if err != nil {
		return nil, 0, err
	}
	rows = make([]OrderCustomerRfmRow, 0, len(raw))
	for _, r := range raw {
		rows = append(rows, OrderCustomerRfmRow{
			UserID: r.UserID, LastAt: r.LastAt, Freq: r.Freq, Monetary: r.Monetary,
			RScore: r.RScore, FScore: r.FScore, MScore: r.MScore,
			TotalScore: r.TotalScore, Segment: r.Segment,
		})
		total = r.Total
	}
	return rows, total, nil
}

// CustomerRfmSegmentIDs 取某个 RFM 分段内的 user_id（客户列表按「RFM 分段」筛用）。
//
// 与 CustomerRfmList 的关系：**同一条 rfmScoredCTE、同一套打分列定义**，区别只在这里
// 只取 id（列表要的是「哪些人是 vip」，不是他们的分数）。
// 不另写一条打分 SQL 的理由：RFM 是五分位（相对分），两处各算一遍会在数据变动的
// 边界上给出不同分档 —— 表现是「RFM 页说他是 vip，用 vip 筛客户列表却查不到他」，
// 而两边各自的页面看起来都对。
//
// 排序与 CustomerRfmList 一致（总分降序 + user_id 兜底）：没有稳定排序时
// PostgreSQL 每次可以给出不同的前 N 行，翻页会漏人（而不报错）。
func (m *OrderModel) CustomerRfmSegmentIDs(ctx context.Context, projectID string, from, to time.Time, segment string, limit, offset int) (ids []int64, total int64, err error) {
	if strings.TrimSpace(projectID) == "" {
		return nil, 0, ErrProjectRequired
	}
	if from.IsZero() || to.IsZero() {
		return nil, 0, ErrRangeRequired
	}
	if !IsKnownRfmSegment(segment) {
		return nil, 0, ErrSegmentUnknown
	}
	if limit <= 0 {
		return nil, 0, ErrRangeRequired
	}
	// 分段是**结果列**，不能在 WHERE 里直接引用（SQL 不允许）—— 包一层子查询（同 CustomerRfmList）。
	const sql = rfmScoredCTE + `
SELECT user_id, COUNT(*) OVER () AS total
  FROM (SELECT ` + rfmRowColumns + ` FROM scored) s
 WHERE segment = ?
 ORDER BY total_score DESC, user_id LIMIT ? OFFSET ?`
	type idRow struct {
		UserID int64 `gorm:"column:user_id"`
		Total  int64 `gorm:"column:total"`
	}
	var raw []idRow
	args := append(orderCustomerRfmArgs(projectID, from, to), segment, limit, offset)
	err = rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {
		return tx.Raw(sql, args...).Scan(&raw).Error
	})
	if err != nil {
		return nil, 0, err
	}
	ids = make([]int64, 0, len(raw))
	for _, r := range raw {
		ids = append(ids, r.UserID)
		total = r.Total // 每行都带全量计数，取最后一行的即可（空结果时保持 0）
	}
	return ids, total, nil
}
