package model

// order_spent_model.go — 「按工程批量取每个访客的累计消费额」（BIZ-3 会员升级口径的批量只读聚合）。
//
// 为什么这条聚合归订单模块：**消费口径由订单模块独占解释** —— 哪些状态算钱进来了、
// 订单金额怎么取，都是订单域的事实。membership 模块读不到 users 表、也不该读 orders 表
// （表隔离），所以「谁是本工程的会员候选」这个问题只能由订单侧回答，
// 经 membershipcontract.PurchaseSource 交给它做「消费额 ≥ 门槛」的分档。
//
// 口径与客户页订单摘要（SummaryByUser）**完全同源**：
//   · 计入消费的状态 = paidStatuses（名单只在 order_model.go 声明一次）；
//   · 工程作用域 = rls.InProjectScope（同一形态，策略为行级，作用域落在事务上）。
//
// 「同源」不是风格问题：两边各写一份状态名单的失败模式是「某次顺手加了个状态之后，
// 客户页的累计消费变了、会员等级没变」，两边都不报错、只在有人对账时才发现。

import (
	"context"
	"strings"

	"gorm.io/gorm"

	"go_wp/pkg/rls"
)

// orderNetTotalSQLExpr 订单的**净消费额**表达式：订单金额 − 该单已实际收货的退款额，下界 0。
//
// 这个常量是**两处共用的唯一真源**：客户页订单摘要（SummaryByUser，见 order_model.go）
// 与会员候选聚合（SpentTotalsByProject，见本文件）必须算同一个数 —— 会员按净额分档、
// 客户页按总额显示，是两个数字静默分叉（两边都不报错、只在有人对账时才发现）。
// 任何一处复制它的文本都会立刻产生第二份口径；「改一次两处同时生效」是这个常量存在的全部意义。
//
// 形状是**相关标量子查询**（不是 JOIN，也不是 LATERAL）：退款要按**订单**粒度扣，
// 直接 join 退货表会让一笔订单的多张退货单把订单行乘起来，再 SUM(o.total) 就把金额重复计了。
// 它引用外层的订单别名 **o**，所以拼进 SQL 的外层必须是 `FROM orders o`。
//
// 它内部含**一个 ? 参数**（退货状态名单 ReturnedStatuses）：调用方的实参顺序要按 SQL 文本里
// ? 的出现顺序排 —— 改这个表达式的形状会同时改变两处的参数顺序，两边都要跟着看。
//
// 只扣「已经实际收货」（ReturnedStatuses = received / completed）的退款：申请中与已同意待收货的
// 钱还没退出去，扣了会让会员等级凭空下降。全额退货的单会转成 refunded、本来就不在 paidStatuses
// 里，在状态过滤阶段就被排除，不会再被这里扣一次（两处口径不重叠）。
//
// GREATEST(..., 0) 的夹取与 SpentTotalsByProject 的既有行为一致：净额不会被脏数据
// （退款额超过订单金额）算成负数，但**净额为 0 的行仍然会出现在结果里** —— 这是既有行为，
// 不要顺手改成过滤掉。
const orderNetTotalSQLExpr = "GREATEST(o.total - COALESCE((SELECT SUM(r.refund_amount) " +
	"FROM order_returns r WHERE r.order_id = o.id AND r.project_id = o.project_id " +
	"AND r.status = ANY(string_to_array(?, ',')::text[])), 0), 0)"

// spentTotalsSelect 消费额聚合的取数列（净额口径，见 orderNetTotalSQLExpr）。
//
// 拼串只发生在**列表达式**上：净额表达式含子查询，GORM 没有「把表达式当列」的链式写法，
// 而它是本模块净额语义的唯一真源（KPI / 客户摘要 / 会员分档三处共用）。
// FROM / WHERE / GROUP BY 与参数绑定全部归 GORM。
//
// 实参只有**一个**：退货状态名单，对应 orderNetTotalSQLExpr 里唯一那处 ?。
// 「计入消费的订单状态」不在这里 —— 它在 WHERE 的 o.status 条件上（见 SpentTotalsByProject）。
const spentTotalsSelect = "o.user_id, COALESCE(SUM(" + orderNetTotalSQLExpr + "), 0) AS amount"

// SpentTotalsByProject 返回本工程「有可计入消费」的访客 → 累计消费额（**分**）。
//
// 一条 GROUP BY 聚合，而不是对每个客户各调一次 SummaryByUser：那是 N+1 ——
// 会员日结要的是全量候选清单，几千个客户就是几千次往返，而这里一次扫描就够。
//
// 只返回有可计入消费的用户：全部订单都取消 / 退款的客户不该出现在候选里
// （金额为 0 的行在调用方那边也定不出档，多送过去只是让重算多做一轮无用功）。
// 早退不判 projectID 是否为空：空工程在本仓是**查不到行**（uuid 不匹配），
// 返回空 map 是正确结论；把它当参数错误会让调用方多写一条不必要的分支。
//
// **口径（BIZ-09）：消费额是净额 —— 订单金额减去已经实际收货的退款额。**
//
//   - 只扣「已经实际收货」（ReturnedStatuses = received / completed）的退款：
//     申请中与已同意待收货的钱还没退出去，扣了会让会员等级凭空下降；
//   - 全额退货的单会转成 refunded 状态、本来就不在 paidStatuses 里 —— 它在 WHERE 阶段
//     就被排除了，不会再被这里扣一次（两处口径不重叠）；
//   - 因此**部分退货会降档**：消费额按实际净消费算，退款回去的钱不再计入。
//     这是产品口径上的选择，理由是它必须与「全额退货不计入」自洽 ——
//     否则会出现「全退不计、退一半全计」的阶梯，退得越多反而等级越高。
//     代价是已发出的等级会随退款回落，这正是「按净消费分档」的应有之义。
//
// 与客户页订单摘要（SummaryByUser 的 total_amount）用**同一个表达式常量**
// （orderNetTotalSQLExpr）：那边是同一件事的单客户视图，两处口径必须逐字一致 ——
// 它们共享一个常量，改一处即两处生效；跨路径一致性另有 feature 用例钉住。
func (m *OrderModel) SpentTotalsByProject(ctx context.Context, projectID string) (totals map[uint64]int64, err error) {
	var rows []struct {
		UserID uint64 `gorm:"column:user_id"`
		Amount int64  `gorm:"column:amount"`
	}
	err = rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {
		q := tx.Table(OrderEntity{}.TableName() + " AS o")
		// 经 selectExpr 而不是 Select：它先校验 ? 个数与实参个数相等（本处最初就是
		// 1 个 ? 配 2 个实参，GORM 静默把实参当列名拼出了坏 SQL）。
		return selectExpr(q, spentTotalsSelect, strings.Join(ReturnedStatuses, ",")).
			Where("o.project_id = ?", projectID).
			Where("o.user_id IS NOT NULL").
			Where("o.status = ANY(string_to_array(?, ',')::text[])", strings.Join(paidStatuses, ",")).
			Group("o.user_id").
			Scan(&rows).Error
	})
	if err != nil {
		return nil, err
	}
	totals = make(map[uint64]int64, len(rows))
	for _, r := range rows {
		if r.UserID == 0 {
			// 0 不是有效账号 id（users.id 从 1 起），它在 map 里只会变成一个
			// 永远定不出档的候选；挡在这里比让调用方逐个判更省事。
			continue
		}
		totals[r.UserID] = r.Amount
	}
	return totals, nil
}
