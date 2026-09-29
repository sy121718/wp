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

// SpentTotalsByProject 返回本工程「有可计入消费」的访客 → 累计消费额（**分**）。
//
// 一条 GROUP BY 聚合，而不是对每个客户各调一次 SummaryByUser：那是 N+1 ——
// 会员日结要的是全量候选清单，几千个客户就是几千次往返，而这里一次扫描就够。
//
// 只返回有可计入消费的用户：全部订单都取消 / 退款的客户不该出现在候选里
// （金额为 0 的行在调用方那边也定不出档，多送过去只是让重算多做一轮无用功）。
// 早退不判 projectID 是否为空：空工程在本仓是**查不到行**（uuid 不匹配），
// 返回空 map 是正确结论；把它当参数错误会让调用方多写一条不必要的分支。
func (m *OrderModel) SpentTotalsByProject(ctx context.Context, projectID string) (totals map[uint64]int64, err error) {
	var rows []struct {
		UserID uint64 `gorm:"column:user_id"`
		Amount int64  `gorm:"column:amount"`
	}
	err = rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {
		return tx.Model(&OrderEntity{}).
			Select("user_id, COALESCE(SUM(total), 0) AS amount").
			Where("project_id = ?", projectID).
			// user_id 可空（访客下单还没开号、后台代客建单都可能为空）：
			// 没有账号就没有会员身份，这些订单不构成任何人的消费额。
			Where("user_id IS NOT NULL").
			// 状态名单由 paidStatuses 拼出（只声明一次，见 order_model.go）。
			Where("status = ANY(string_to_array(?, ',')::text[])", strings.Join(paidStatuses, ",")).
			Group("user_id").
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
