package orderservice

// order_expire.go — 待付款订单超时自动取消（TX-001）。
//
// 建单成功即扣库存，待付款单若长期不支付也不取消，可用量会被一直占住。
// 定时扫描超过 TTL 的 pending 单，复用 CancelOrder（归还库存 + 释放券核销）。

import (
	"context"
	"errors"
	"time"

	orderdto "go_wp/internal/module/order/dto"
	orderenums "go_wp/internal/module/order/enums"
	ordermodel "go_wp/internal/module/order/model"
	"go_wp/pkg/logger"
)

const (
	// pendingOrderTTL 待付款超时阈值：超过此时间未支付则自动取消。
	pendingOrderTTL = 30 * time.Minute
	// pendingOrderExpiryInterval 后台扫描间隔。
	pendingOrderExpiryInterval = 15 * time.Minute
	// pendingOrderExpiryBatch 单次扫描最多处理条数，避免一次拖住进程太久。
	pendingOrderExpiryBatch = 100
)

// ExpirePendingOrders 取消创建时间早于 cutoff 的待付款订单，返回成功取消条数。
//
// 逐工程扫描（DB-009 第三批）：orders 带 FORCE 策略，不带作用域的「全表扫描」在换
// 非超级角色后静默返回空集 —— 定时任务照样每 15 分钟跑一次，一单都不会取消，
// 库存被一直占住而日志里没有任何异常。工程清单由 model 从 projects 表取（那里是
// 隔离主体、不受策略约束），逐个工程各设一次作用域。
// 连工程都取不到时显式失败：静默返回 0 会把「读不到工程表」伪装成「没有超时订单」。
func (s *Service) ExpirePendingOrders(ctx context.Context, olderThan time.Duration, batchSize int) (expired int, err error) {
	if olderThan <= 0 {
		olderThan = pendingOrderTTL
	}
	if batchSize <= 0 {
		batchSize = pendingOrderExpiryBatch
	}
	projectIDs, perr := s.orders.ListAllProjectIDs(ctx)
	if perr != nil {
		return 0, perr
	}
	if len(projectIDs) == 0 {
		return 0, errors.New(orderenums.ErrProjectRequired)
	}
	cutoff := time.Now().Add(-olderThan)
	for _, projectID := range projectIDs {
		if ctx.Err() != nil {
			break
		}
		list, lerr := s.orders.ListPendingCreatedBefore(ctx, projectID, cutoff, batchSize)
		if lerr != nil {
			return expired, lerr
		}
		for _, o := range list {
			if ctx.Err() != nil {
				return expired, nil
			}
			_, cerr := s.CancelOrder(ctx, &orderdto.CancelOrderReq{
				OrderID:      o.ID,
				ProjectID:    projectID,
				Reason:       "待付款超时自动取消",
				OperatorType: ordermodel.OperatorTypeSystem,
			})
			if cerr != nil {
				logger.Scene("order").With("order_id", o.ID).With("order_no", o.OrderNo).
					Warn("待付款超时取消失败（下轮重试）: " + cerr.Error())
				continue
			}
			expired++
		}
	}
	if expired > 0 {
		logger.Scene("order").With("expired", expired).Info("待付款超时订单已自动取消")
	}
	return expired, nil
}

// StartPendingOrderExpiryScheduler 启动后台定时扫描（进程内 goroutine，失败不 panic）。
func StartPendingOrderExpiryScheduler(svc *Service) {
	if svc == nil {
		return
	}
	go func() {
		run := func() {
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
			defer cancel()
			_, _ = svc.ExpirePendingOrders(ctx, pendingOrderTTL, pendingOrderExpiryBatch)
		}
		run()
		ticker := time.NewTicker(pendingOrderExpiryInterval)
		defer ticker.Stop()
		for range ticker.C {
			run()
		}
	}()
}
