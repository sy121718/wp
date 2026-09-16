package orderservice

// order_create_persist.go — 建单的写入阶段：订单事务 + 扣库存 + 失败补偿。
//
// 这一段动的是**真源**（订单表与库存），所以每一步的失败都要有明确后果：
// 订单事务失败 → 整单不存在（含券核销，同生共死）；
// 扣库存失败 → 订单已在库里，必须补偿成「已取消」并留痕，不能留一张待付款的僵尸单。

import (
	"context"
	"errors"
	"strings"
	"time"

	"gorm.io/gorm"

	ordercontract "go_wp/internal/module/order/contract"
	orderenums "go_wp/internal/module/order/enums"
	ordermodel "go_wp/internal/module/order/model"
)

// persistOrder ①：订单头 + 订单项 + 流转流水 + 券核销，一个事务。
//
// 券的核销与订单**同生共死**：核销失败（用尽 / 超每人限次）则订单一起回滚，
// 不会出现「券核销了但单没下成」或「单下了但券没用掉」两种半截状态。
//
// 回填 head.ID：订单项与流水都要挂这个 id，而它是 CreateTx 之后才有的。
func (s *Service) persistOrder(ctx context.Context, d *orderDraft) error {
	return s.orders.Transaction(ctx, func(tx *gorm.DB) error {
		if cerr := s.orders.CreateTx(ctx, tx, d.head); cerr != nil {
			return cerr
		}
		for _, it := range d.items {
			it.OrderID = d.head.ID
		}
		if cerr := s.items.CreateBatchTx(ctx, tx, d.items); cerr != nil {
			return cerr
		}
		if lerr := s.logs.CreateTx(ctx, tx, &ordermodel.OrderStatusLogEntity{
			OrderID:      d.head.ID,
			FromStatus:   "",
			ToStatus:     ordermodel.OrderStatusPending,
			OperatorType: operatorTypeOf(d.head.CreatedVia),
			OperatorID:   d.head.CreateBy,
			Remark:       "建单",
			CreateTime:   d.now,
		}); lerr != nil {
			return lerr
		}
		return s.redeemCouponTx(ctx, tx, d.appliedCoupon, d.head.ID, d.head.OrderNo, d.head.DiscountTotal, d.head.UserID, d.now)
	})
}

// deductStockOrCompensate ②③：扣库存（跨模块，落在订单事务之外）并在失败时补偿。
//
// 库存不足是业务常态（并发抢最后一件），不能把「没扣到库存」的单留成待付款；
// 补偿动作是**标记取消 + 记流水 + 交还券次数**，都不再向上冒错 ——
// 调用方已经要拿到「库存不足 / 服务不可用」这个结论了，再叠一个补偿错误
// 只会让原因看不出主次。
func (s *Service) deductStockOrCompensate(ctx context.Context, d *orderDraft) error {
	lines := make([]ordercontract.StockLine, 0, len(d.items))
	for _, it := range d.items {
		lines = append(lines, ordercontract.StockLine{
			ProductID: it.ProductID,
			VariantID: it.VariantID,
			SKUCode:   it.SKU,
			Quantity:  it.Quantity,
		})
	}
	dErr := s.stock.DeductStock(ctx, &ordercontract.StockDeduction{
		ProjectID:  d.projectID,
		ReasonCode: "sale_out",
		SourceType: "order",
		SourceRef:  d.head.OrderNo,
		Remark:     "订单出库",
		Lines:      lines,
	})
	if dErr == nil {
		return nil
	}

	// 区分「库存不足」与「库存服务不可用」：前者是客户看得到答案的业务结论，
	// 后者是运维要看的问题，两者在补偿原因与后续排查上完全不同。
	reason := orderenums.ErrStockInsufficient
	msg := strings.ToLower(dErr.Error())
	if !strings.Contains(msg, "不足") && !strings.Contains(msg, "insufficient") {
		reason = orderenums.ErrStockUnavailable
	}
	_ = s.markAutoCancelled(ctx, d.head.ProjectID, d.head.ID, reason)
	return errors.New(reason)
}

// markAutoCancelled 库存失败后的补偿：把订单置为已取消并记一条流转。
//
// 补偿本身的失败**不再向上冒**：调用方已经要拿到「库存不足」这个结论了，
// 再叠一个补偿错误只会让原因变得看不出主次；订单留在 pending 会被后续的人工处理看到。
func (s *Service) markAutoCancelled(ctx context.Context, projectID string, orderID uint64, reason string) error {
	now := time.Now()
	err := s.orders.Transaction(ctx, func(tx *gorm.DB) error {
		if uerr := s.orders.UpdateFieldsTx(ctx, tx, projectID, orderID, map[string]any{
			"status":        ordermodel.OrderStatusCancelled,
			"cancel_reason": reason,
			"update_time":   now,
		}); uerr != nil {
			return uerr
		}
		return s.logs.CreateTx(ctx, tx, &ordermodel.OrderStatusLogEntity{
			OrderID:      orderID,
			FromStatus:   ordermodel.OrderStatusPending,
			ToStatus:     ordermodel.OrderStatusCancelled,
			OperatorType: ordermodel.OperatorTypeSystem,
			Remark:       reason,
			CreateTime:   now,
		})
	})
	if err != nil {
		return err
	}
	// 库存未扣成功但券已在建单事务内核销 —— 必须把次数还回去。
	s.releaseCouponForOrder(ctx, orderID)
	return nil
}
