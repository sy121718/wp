package cartservice

// cart_shipping.go — 结算运费的**完整取值链**（站点级基础运费 → 满额免运费 → 会员免运费）。
//
// 边界只有一条：**只影响运费，不动商品金额**。商品小计由订单域按商品域真源现算
//（cart 里的价格不作数，见 cart_checkout.go），站点策略与会员权益都碰不到它。
//
// 取值链（顺序固定，任一环节命中「免」即 0）：
//
//	① 站点基础运费      —— projects.settings 的 shippingBaseFee（分，0 = 这个站点不收运费）；
//	② 满额免运费        —— shippingFreeThreshold > 0 且商品小计 ≥ 门槛 → 0；
//	③ 会员免运费        —— free_shipping 权益为真时把剩下的置 0。
//
// **全模块只有 shippingTotalOf 一个函数给运费定价**：把「该收多少运费」的判断散到
// 第二处的那天，两处就会开始分叉（一处改了阈值、另一处没改），而分叉的表现是
// 「购物车显示免运费、订单里收了运费」这种只有客户能发现的不一致。
//
// 两个端口都可缺：
//
//	membershipcontract.Reader   未注入 = 会员权益未开启（运费与接入前逐字一致）；
//	projectcontract.ShippingPolicyReader
//	                            未注入 = 站点没配运费（base 恒 0 ⇒ 收不到钱）。
//
// 为什么运费不从请求里取：结算入参里凡是从表单取的东西都是客户端可伪造的
//（参见 renderCheckout 的取值方式）。运费是收银台上的一笔钱，谁传谁就能免单 ——
// 所以本文件的两个输入（站点规则、商品小计）都由服务端自己读。

import (
	"context"
	"strings"

	membershipcontract "go_wp/internal/module/membership/contract"
	membershipdto "go_wp/internal/module/membership/dto"
	productcontract "go_wp/internal/module/product/contract"
	projectcontract "go_wp/internal/module/project/contract"
	"go_wp/pkg/logger"
)

// SetMembershipReader 注入会员身份读取端口（装配期调用；**可缺** = 免运费未开启）。
//
// 收窄到 Reader 而不是 MembershipService：购物车只需要「这个访客免不免运费」，
// 拿不到等级 CRUD、归属写入或重算能力（越权防护靠接口形状，不靠调用方自觉）。
func (s *Service) SetMembershipReader(reader membershipcontract.Reader) {
	if s == nil {
		return
	}
	s.membership = reader
}

// SetShippingPolicyReader 注入站点运费规则读取端口（装配期调用；**可缺** = 站点不收运费）。
//
// 收窄到 ShippingPolicyReader（一条只读方法）：结算只需要「这个工程的运费规则」，
// 拿不到工程 CRUD、站点设置写入或主题能力 —— cart 也**不允许** import project 的
// service/model，这条端口是唯一通道（与 product 的 VariantSnapshotPort 同一形状）。
func (s *Service) SetShippingPolicyReader(reader projectcontract.ShippingPolicyReader) {
	if s == nil {
		return
	}
	s.shippingPolicy = reader
}

// shippingPolicyOf 读该工程的运费规则（**结算写路径上唯一读站点设置的地方**）。
//
// 三种形态：
//   - 端口未注入 / 工程为空 → 零值（不收运费），**不打日志**：那是合法的部署形态
//     （站点本来就没配运费），每次结算记一条 Warn 只会把日志淹掉，反而掩盖真故障；
//   - 读失败（基础设施）→ 零值 + Error 日志；
//   - 读到规则 → 原样返回（合法性由 project 侧归一，见 ShippingPolicyReader 的注释）。
//
// 「读失败按 0」是刻意的失效方向，理由见 shippingTotalOf 的注释。
func (s *Service) shippingPolicyOf(ctx context.Context, projectID string) projectcontract.ShippingPolicy {
	if s.shippingPolicy == nil || strings.TrimSpace(projectID) == "" {
		return projectcontract.ShippingPolicy{}
	}
	policy, err := s.shippingPolicy.ShippingPolicyOf(ctx, projectID)
	if err != nil {
		logger.Scene("cart").With("projectId", projectID).Error(err,
			"读站点运费规则失败，本单按不收运费结算（不拒单）")
		return projectcontract.ShippingPolicy{}
	}
	return policy
}

// cartSubtotalOf 结算路径现算商品小计（分）—— 「满额免运费」门槛判定的输入。
//
// 为什么结算里要算这一次：门槛判的是「这一单买了多少钱」，那个数字只有读商品域真源
// 才知道（cookie 里的价格不作数，见 cart_cookie.go）。口径与订单域建单时逐字一致
// （下架、跨工程、数量非正的行都不计入），所以两边对「多少钱」的看法不会分叉。
//
// **这不是「在别处重算金额」**：运费金额只在 shippingTotalOf 里算一处，这里给的是
// **商品**金额；订单域建单时会再按商品域真源算一遍并落快照 —— 那一份才是权威口径，
// cart 这一份只用来决定「门槛是否已达标」。
//
// 读不到商品事实时返回 0：门槛恒 > 0，于是 0 一定「不达门槛」= 照收基础运费，
// 落在失效方向安全的那一侧（宁愿照收，也不凭空免掉）。而真走到这一步时，订单域
// 建单也会因同一次读取失败而拒绝整单，运费算多少都不改变结局。
func (s *Service) cartSubtotalOf(ctx context.Context, projectID string, lines []cartPayloadLine) int64 {
	if s.product == nil || len(lines) == 0 {
		return 0
	}
	ids := make([]string, 0, len(lines))
	for _, l := range lines {
		ids = append(ids, l.VariantID)
	}
	snaps, err := s.product.VariantSnapshots(ctx, ids, projectID)
	if err != nil {
		return 0
	}
	byID := make(map[string]*productcontract.VariantSnapshot, len(snaps))
	for _, sn := range snaps {
		if sn != nil {
			byID[sn.VariantID] = sn
		}
	}
	var subtotal int64
	for _, l := range lines {
		if l.Quantity <= 0 {
			continue
		}
		sn := byID[l.VariantID]
		// 下架 / 跨工程 / 拿不到价格的行不计入小计：与 snapshotOf 对下架商品的处理
		// 同口径 —— 不计入小计的那一行，也不该把客户推过免运费门槛。
		if sn == nil || !sn.Enabled || sn.Price <= 0 {
			continue
		}
		if sn.ProjectID != "" && sn.ProjectID != projectID {
			continue
		}
		subtotal += sn.Price * int64(l.Quantity)
	}
	return subtotal
}

// shippingTotalOf 定出本单该收的运费（分）—— **全模块唯一给运费定价的函数**。
//
// 五种形态都是明确结论：
//   - 站点基础运费 <= 0 → 0（这个站点不收运费，**不读库也不问会员** ——
//     「0 减 0 还是 0」没有结论价值，而结算路径上每一次多余往返都是访客在等）；
//   - 小计已达门槛（门槛 > 0）→ 0（**不再问会员**：已经免了，再解析一次身份
//     只是多一次往返）；
//   - 端口未注入 / 无账号 → 照收基础运费（正常路径，不打日志）；
//   - 有 free_shipping 权益 → 0；
//   - 解析失败 → **照收基础运费** + 一条 Warn。
//
// 失效方向（两条，判据是同一条：宁可按高收，不可凭空免）：
//
//	① 会员身份读不到 → 照收。把「会员服务读不到」解释成「免运费」等于一次数据库抖动
//	   让全站订单免运费（少收钱且不可追溯）；照收的最坏后果是会员少享受一次权益，
//	   客服可补。与折扣侧同一判据（见 order 的 resolveMembershipDiscount）。
//	② 站点运费规则读不到（端口未注入 / 读库失败）→ **按 0（不收运费）**。
//	   这一条方向与①相反，理由是输入不同：① 里 base 是**已知**的，② 里 base 根本不知道。
//	   「不知道」时编造一个金额（无论是 0 还是某个默认值）都没有依据 —— 唯一有依据的
//	   缺省值是 0，因为 shippingBaseFee 的语义就是「0 = 不收运费」，而系统在接入站点策略
//	   之前的行为也正是 0（读不到 = 与接入前逐字一致）。
//	   反过来（读不到就拒单 / 按某个非 0 值收）会把一次装配缺陷或数据库抖动升级成
//	   「全店无法结算」或「凭空多收一笔钱」，两者都比少收一笔运费严重且不可解释。
func (s *Service) shippingTotalOf(
	ctx context.Context,
	projectID string,
	userID *uint64,
	subtotal int64,
	policy projectcontract.ShippingPolicy,
) int64 {
	base := policy.BaseFeeCents
	if base <= 0 {
		return 0
	}
	// ② 满额免运费：门槛 > 0 才算「启用」（0 = 不启用，不能解释成「0 元就免」）。
	if policy.FreeThresholdCents > 0 && subtotal >= policy.FreeThresholdCents {
		return 0
	}
	if s.membership == nil || userID == nil || *userID == 0 {
		return base
	}
	if strings.TrimSpace(projectID) == "" {
		return base
	}
	member, err := s.membership.Resolve(ctx, &membershipdto.ResolveReq{
		ProjectID: projectID,
		UserID:    *userID,
	})
	if err != nil {
		logger.Scene("cart").
			With("projectId", projectID).With("userId", *userID).
			Warn("会员身份解析失败，本单按基准运费结算（不免运费，也不拒单）")
		return base
	}
	if member == nil || !member.FreeShipping {
		return base
	}
	return 0
}
