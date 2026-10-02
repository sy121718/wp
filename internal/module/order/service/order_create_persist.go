package orderservice

// order_create_persist.go — 建单的写入阶段：订单 + 扣库存，**同一个事务**。
//
// 一次建单有四处数据库写入：订单头、订单项、状态流水、券核销（含券 usedCount），
// 外加跨模块的库存扣减。它们必须同生共死：
//   · 任一步失败 → **整单不存在**，不留「有单没扣库存」的待付款僵尸单，
//     也不留「券核销了但没下单」；
//   · 库存不足 / 库存服务不可用都是业务结论，直接把错误返回给调用方，
//     不再走「先建单、再补偿成已取消」——同库跨模块不再用补偿，只传事务句柄
//     （DeductStockTx，先例 masterdata.RecordChangesTx）。

import (
	"context"
	"errors"
	"strings"

	"gorm.io/gorm"

	ordercontract "go_wp/internal/module/order/contract"
	orderenums "go_wp/internal/module/order/enums"
	ordermodel "go_wp/internal/module/order/model"
)

// persistOrderTx 建单写入的**事务内实现**（tx 由调用方负责提交 / 回滚）。
//
// 回填 head.ID：订单项与流水都要挂这个 id，而它是 CreateTx 之后才有的。
func (s *Service) persistOrderTx(ctx context.Context, tx *gorm.DB, d *orderDraft) error {
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
	if rerr := s.redeemCouponTx(ctx, tx, d.appliedCoupon, d.head.ID, d.head.OrderNo, d.head.DiscountTotal, d.head.UserID, d.now); rerr != nil {
		return rerr
	}
	// 扣库存：同库跨模块，把**订单事务的句柄**传给库存的 …Tx 方法。
	// 不足 / 不可用都让整个事务回滚 —— 订单、项、流水、券核销一起消失。
	return s.deductStockTx(ctx, tx, d)
}

// deductStockTx 建单出库（在订单事务内）：库存变动落在调用方的事务里，失败原样反馈。
//
// 区分「库存不足」与「库存服务不可用」：前者是客户看得到答案的业务结论，
// 后者是运维要看的问题，两者在错误文案与后续排查上完全不同。
func (s *Service) deductStockTx(ctx context.Context, tx *gorm.DB, d *orderDraft) error {
	lines := make([]ordercontract.StockLine, 0, len(d.items))
	for _, it := range d.items {
		lines = append(lines, ordercontract.StockLine{
			ProductID: it.ProductID,
			VariantID: it.VariantID,
			SKUCode:   it.SKU,
			Quantity:  it.Quantity,
		})
	}
	dErr := s.stock.DeductStockTx(ctx, tx, &ordercontract.StockDeduction{
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
	reason := orderenums.ErrStockInsufficient
	msg := strings.ToLower(dErr.Error())
	if !strings.Contains(msg, "不足") && !strings.Contains(msg, "insufficient") {
		reason = orderenums.ErrStockUnavailable
	}
	return errors.New(reason)
}
