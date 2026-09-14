package orderservice

// coupon_release.go — 取消订单时释放优惠码核销（与建单 redeem 对称）。

import (
	"context"

	"gorm.io/gorm"

	"go_wp/pkg/logger"
)

// releaseCouponForOrder 取消路径释放券：删核销明细 + 减 used_count。
//
// 失败只记日志：订单状态已是取消，券计数偏差可由后台对账发现，不应阻断取消主链。
func (s *Service) releaseCouponForOrder(ctx context.Context, orderID uint64) {
	if s.coupons == nil || orderID == 0 {
		return
	}
	err := s.coupons.Transaction(ctx, func(tx *gorm.DB) error {
		_, rerr := s.coupons.ReleaseRedemptionByOrderTx(ctx, tx, orderID)
		return rerr
	})
	if err != nil {
		logger.Scene("order").With("order_id", orderID).
			Warn("取消订单后释放优惠码失败（需人工核对 used_count）: " + err.Error())
	}
}
