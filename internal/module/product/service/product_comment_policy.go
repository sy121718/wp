package productservice

// product_comment_policy.go — 商品评论的差异化规则（comment 模块 EntityPolicy 端口的实现）。
//
// 规则：**商品评论必须买过**。
//
// 为什么实现在 product 而不是 comment：comment 模块不认识商品、也不认识订单 ——
// 它只定义「能不能评」这个问题与实际存储。谁拥有这个实体，谁负责回答关于它自己的
// 产品规则（同型先例：datarule 的域白名单由拥有该表的实体声明）。
//
// 依赖方向：product → order（经 productcontract.PurchaseChecker 只读端口）。
// 反向不存在：order 不认识 comment，也不认识「评论」这件事。

import (
	"context"
	"strings"

	commentcontract "go_wp/internal/module/comment/contract"
	productcontract "go_wp/internal/module/product/contract"
	productenums "go_wp/internal/module/product/enums"
	"go_wp/pkg/logger"
)

// 编译期断言：本实现满足 comment 模块声明的差异化规则端口。
var _ commentcontract.EntityPolicy = (*Service)(nil)

// SetPurchaseChecker 注入购买事实只读端口（装配期调用；可缺）。
//
// 未注入时**放行**而不是拒绝，判据见 commentcontract.EntityPolicy 的注释：
// 这是产品策略不是安全边界（评论提交的安全边界是「必须登录」，那条在 comment
// service 里无条件执行），且未注入即拒绝会把「装配漏了一行」表现成
// 「整站商品评论发不出去」—— 运营看到的现象与「这个站不让评论」完全一样，
// 排查成本极高。装配层在未注入时留一条 Warn（见 routers 的装配段）。
func (s *Service) SetPurchaseChecker(port productcontract.PurchaseChecker) {
	s.purchases = port
}

// AllowComment 判定某访客能否对某实体发表评论（commentcontract.EntityPolicy）。
//
// 只对**商品**行使规则：其它实体类型（文章等）一律放行 —— 本模块不认识它们，
// 也就不该替它们立规矩。将来文章要自己的规则，由 content 侧实现同一个端口。
func (s *Service) AllowComment(ctx context.Context, req *commentcontract.PolicyReq) (denial *commentcontract.PolicyDenial, err error) {
	if req == nil || strings.TrimSpace(req.EntityType) != productcontract.EntityTypeProduct {
		return nil, nil
	}
	if s.purchases == nil {
		// 未注入 = 规则未启用（放行）。见 SetPurchaseChecker 的注释。
		return nil, nil
	}
	purchased, cerr := s.purchases.HasPurchasedProduct(ctx, req.ProjectID, req.UserID, req.EntityID)
	if cerr != nil {
		// 判定失败 **≠** 「没买过」。这里选择放行 + 记 Error 日志，而不是拒绝：
		//   - 拒绝的失败模式是「一次数据库抖动让所有商品评论都发不出去」，且访客
		//     看到的是一句误导性的「你没买过」—— 比放行更难查、更难解释；
		//   - 放行仍有兜底：评论一律落 pending，审核队列把关内容
		//    （这条规则本身的定位就是产品策略，不是安全边界）。
		logger.Scene("product").Error(cerr, "商品评论的购买事实判定失败（本次放行，待审核队列把关）")
		return nil, nil
	}
	if purchased {
		return nil, nil
	}
	return &commentcontract.PolicyDenial{
		Message:  productenums.ErrCommentPurchaseRequired,
		Fallback: "购买过该商品才能发表评论",
	}, nil
}
