package orderservice

// order_membership_discount.go — 会员折扣并入建单金额链路（BIZ-3 消费侧接入）。
//
// 口径（已拍板，不是这里能改的）：**会员折扣与券各自独立计账、相加扣减**。
// 落到 orders 上是两列：
//
//	discount_total           —— 券折扣（既有语义**一个字节都不动**）
//	membership_discount_total —— 会员折扣（迁移 462 已加好，本文件开始真正写入）
//
// 为什么不并进 discount_total：SEC-001 有一条既有判据「无优惠码时折扣恒为 0」
// （见 resolveCoupon 的注释）。把会员折扣算进同一列，那条判据就会在每次会员下单时
// 出现常规例外，审计与回归测试都要重新解释「这一列到底代表什么」。
//
// 三条边界：
//   · **只读**：Reader.Resolve 是只读路径，本文件不写库、不建归属行；
//   · **失败不阻断下单**：会员服务读不到时降级为「不打折 + 日志」，不是拒单
//     （会员是附加能力，订单是主体 —— 与访客开号失败不阻断下单同一取舍）；
//   · **绝不放大到负数**：折扣合计夹在小计之内（见 resolveMembershipDiscount）。

import (
	"context"
	"strings"

	membershipcontract "go_wp/internal/module/membership/contract"
	membershipdto "go_wp/internal/module/membership/dto"
	"go_wp/pkg/logger"
)

// SetMembershipReader 注入会员身份读取端口（装配期调用；**可缺**）。
//
// 可缺的语义是「会员折扣功能未开启」：未注入时折扣恒为 0，订单金额与本批接入前
// **逐字一致**。这与 runtimefragment 的 sitePageResolver 同一口径 ——
// 缺能力就按「没有这项能力」算，而不是报错或静默算成别的数。
func (s *Service) SetMembershipReader(reader membershipcontract.Reader) {
	if s == nil {
		return
	}
	s.membership = reader
}

// membershipDiscountAmount 纯函数：按扣减百分比算会员折扣额（分）。
//
// 三条规则：
//  1. 百分比按 kind=discount 的取值域 1..100 解释（20 = 打八折），
//     越界值一律按边界夹住 —— 迁移 462 的 CHECK 已经在库上拦过一次，
//     这里是「万一有人绕过应用层写库」时的第二道，免得负折扣被算成加价；
//  2. 基数 = **小计**（与券同基数）：两套折扣各自独立计账，谁也不以对方的结果为基数
//     —— 那会让「先加券再打折」与「先打折再加券」得到不同金额，而运营在后台
//     看到的是两个独立开关，没有理由认为它们有先后；
//  3. 上界 = 小计扣掉券之后**还剩多少**（remaining）：相加扣减下两道折扣可能合计超过小计
//     （券 100% + 会员 50%），不夹住就会出现「应付为负」（负数总额没有意义，
//     而且它会顺着 total 传到支付金额上）。
func membershipDiscountAmount(subtotal, remaining, percent int64) int64 {
	if percent <= 0 || subtotal <= 0 || remaining <= 0 {
		return 0
	}
	if percent > 100 {
		percent = 100
	}
	amount := subtotal * percent / 100
	if amount > remaining {
		amount = remaining
	}
	if amount < 0 {
		return 0
	}
	return amount
}

// resolveMembershipDiscount 取该访客在本工程的会员折扣额（分）。
//
// 返回值的三种形态都是**明确的结论**，不是「失败与 0 混在一起」：
//   - 未注入端口 / 无账号 / 无折扣权益 → 0（正常路径，不打日志）；
//   - 解析失败（默认等级缺失、库不可用）→ 0 **加一条 Warn**（降级方向是照收钱，
//     不是免单：一次数据库抖动不该让全站订单集体打折，那正是「错误被解释成优惠」的资损形态）；
//   - 有折扣权益 → 按小计算出的金额，并夹在小计减去券之后的余额内。
//
// userID 为 nil / 0 时**不查库**直接返回 0：访客还没有账号就没有会员身份，
// 为它跑一次解析只是白花一次数据库往返（AGENTS.md 不变量 1 的取舍）。
func (s *Service) resolveMembershipDiscount(ctx context.Context, projectID string, userID *uint64, subtotal, couponDiscount int64) (discount int64) {
	if s.membership == nil || userID == nil || *userID == 0 {
		return 0
	}
	if strings.TrimSpace(projectID) == "" {
		return 0
	}
	member, err := s.membership.Resolve(ctx, &membershipdto.ResolveReq{
		ProjectID: projectID,
		UserID:    *userID,
	})
	if err != nil {
		logger.Scene("order").
			With("projectId", projectID).With("userId", *userID).
			Warn("会员身份解析失败，本单按无会员折扣建单（折扣降级为 0，不拒单）")
		return 0
	}
	if member == nil {
		return 0
	}
	remaining := subtotal - couponDiscount
	if remaining < 0 {
		remaining = 0
	}
	return membershipDiscountAmount(subtotal, remaining, member.DiscountPercent)
}
