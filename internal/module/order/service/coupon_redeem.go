package orderservice

// coupon_redeem.go — 优惠码核销（与建单同事务，BIZ-1）。
//
// 核销的口径只有一条：**券的每一次使用都对应一张真实订单**。
// 因此它没有独立入口，只能在建单事务里发生（见 CreateOrderReq.CouponCode）：
//
//   · 券用尽 → 整个事务回滚，订单也不会落库。访客看到的是「券用完了」，
//     而不是「下单成功但优惠没生效」—— 后者要靠客诉才能发现；
//   · 同一单重复核销 → 命中唯一键 (coupon_id, order_id)，幂等返回且**不重复计数**。
//     重试与并发重放都会走到这条路径上，把重复当错误会让重试永远失败。

import (
	"context"
	"errors"
	"time"

	"gorm.io/gorm"

	orderenums "go_wp/internal/module/order/enums"
	ordermodel "go_wp/internal/module/order/model"
)

// redeemCouponTx 事务内核销一次券。
//
// e 为 nil（这一单没用券）时直接返回，调用方不必在事务里写 if。
func (s *Service) redeemCouponTx(ctx context.Context, tx *gorm.DB, e *ordermodel.CouponEntity, orderID uint64, orderNo string, discount int64, userID *uint64, now time.Time) (err error) {
	if e == nil {
		return nil
	}
	inserted, ierr := s.coupons.InsertRedemptionTx(ctx, tx, &ordermodel.CouponRedemptionEntity{
		CouponID:       e.ID,
		ProjectID:      e.ProjectID,
		Code:           e.Code,
		OrderID:        orderID,
		OrderNo:        orderNo,
		DiscountAmount: discount,
		UserID:         userID,
		CreateTime:     now,
	})
	if ierr != nil {
		return ierr
	}
	if !inserted {
		// 这一单此前已经核销过这张券：幂等命中。不递增计数 ——
		// 否则重试一次就把可用次数多算掉一次，而次数是真金白银。
		return nil
	}
	ok, uerr := s.coupons.IncrementUsedTx(ctx, tx, e.ID)
	if uerr != nil {
		return uerr
	}
	if !ok {
		// 守卫没通过 = 用尽（并发抢最后一次）。返回错误让**整个事务回滚**：
		// 明细行与订单头一起消失，不留下「券记了一笔但单没下成」的半截状态。
		return errors.New(orderenums.ErrCouponExhausted)
	}
	return nil
}
