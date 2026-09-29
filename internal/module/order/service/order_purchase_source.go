package orderservice

// order_purchase_source.go — 订单侧的「消费额批量只读端口」（membership 模块的会员升级素材）。
//
// 契约在 **membership 侧**（membershipcontract.PurchaseSource），实现放在订单侧：
// 「什么算消费」这个问题只有订单域能回答（哪些状态算钱进来了、金额取哪一列），
// 而 membership 模块读不到 users 表、也不该读 orders 表（AGENTS.md 的表隔离）。
//
// 形状是**批量**的（一次给全工程的映射），不是逐用户查询：membership 的日结重算
// 要的是「谁该升级」，它无从枚举 userID —— 只有订单侧能给出这份清单。
// 逐用户调 CustomerOrderSummaryOf 在这里是错的：那是 N+1
//（几千个客户 = 几千次往返 + 几千个作用域事务）。
//
// 这是一个**只读**端口：接口里只有一条返回映射的方法，没有写入能力。

import (
	"context"
	"errors"
	"strings"

	membershipcontract "go_wp/internal/module/membership/contract"
	orderenums "go_wp/internal/module/order/enums"
)

// 编译期断言：装配层用它把本 service 作为 PurchaseSource 注入 membership。
//
// 分一条断言而不是并进 ordercontract.OrderService：这份能力**不是**订单对外的业务契约
// （客户页 / 购物车 / 片段层都不该拿到「全站消费额清单」），它只服务会员重算这一条路径。
// 装配点用断言取，缺实现时在启动时炸掉，而不是等到日结跑出「扫到 0 个人」。
var _ membershipcontract.PurchaseSource = (*Service)(nil)

// SpentTotalsByUser 返回本工程「有可计入消费」的访客 → 累计消费额（**分**）。
//
// 口径（哪些订单状态计入、是否减去退款）由订单侧定义并负责 —— 这里是**唯一的出口**：
// model.SpentTotalsByProject 一条聚合取回，与客户页的订单摘要（SummaryByUser）
// 共用同一份 paidStatuses 与同一个 rls.InProjectScope 形态，两边不可能分叉。
//
// 工程 id 为空即参数错误（打回给调用方），不返回空 map：「没给工程」与「这个工程
// 一分钱消费都没有」在排障时方向完全相反，用一个空 map 表达两件事会让后者被前者掩盖。
func (s *Service) SpentTotalsByUser(ctx context.Context, projectID string) (totals map[uint64]int64, err error) {
	pid := strings.TrimSpace(projectID)
	if pid == "" {
		return nil, errors.New(orderenums.ErrProjectRequired)
	}
	return s.orders.SpentTotalsByProject(ctx, pid)
}
