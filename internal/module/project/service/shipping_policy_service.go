package projectservice

// shipping_policy_service.go — 站点运费规则的读取（projectcontract.ShippingPolicyReader 的实现）。
//
// 读的是 projects.settings 这一列（与 GA4 / GSC / Head-Body 同源），不新增存储。
// 形状校验走 projectdto.NormalizeShippingPolicy —— 与后台保存**同一份判据**：
// 两边各写一份的结果是「后台存进去了、结算时按不收运费处理」，不报错、不记日志。

import (
	"context"
	"errors"
	"strings"

	projectcontract "go_wp/internal/module/project/contract"
	projectdto "go_wp/internal/module/project/dto"
	"go_wp/pkg/logger"

	"gorm.io/gorm"
)

// 编译期断言：本 service 实现站点运费规则读取端口（装配层据此注入给 cart）。
var _ projectcontract.ShippingPolicyReader = (*Service)(nil)

// ShippingPolicyOf 读该工程当前的站点运费规则（分）。
//
// 两种「读不出规则」的情形都在这里就地收敛成零值（= 不收运费），而不是把它们
// 变成 error 让调用方各自解释：
//
//	工程不存在      —— 结算链路本身会在建单时被订单域拒掉（工程不存在则商品也取不到），
//	                   运费的正确答案是「不知道」，不该在这里制造第二个失败点；
//	存储值非法      —— 只有绕过后台表单写入才可能出现（表单保存时已拒绝非法值），
//	                   按 0 走 + 一条 Error 日志：运营能在日志里找到那个工程 id，
//	                   而结算不会因为一个坏配置整店停摆。
//
// error 只留给基础设施故障（读库失败）：那是调用方必须自己决定失效方向的情形
// （cart 侧按「不收运费」降级，见 cart_shipping.go）。
func (s *Service) ShippingPolicyOf(ctx context.Context, projectID string) (policy projectdto.ShippingPolicy, err error) {
	id := strings.TrimSpace(projectID)
	if id == "" {
		return projectdto.ShippingPolicy{}, nil
	}
	e, gerr := s.model.GetByID(ctx, id)
	if errors.Is(gerr, gorm.ErrRecordNotFound) {
		logger.Scene("project").With("project", id).
			Warn("读站点运费规则时工程不存在，本次结算按不收运费处理")
		return projectdto.ShippingPolicy{}, nil
	}
	if gerr != nil {
		return projectdto.ShippingPolicy{}, gerr
	}
	fields := projectdto.ParseSiteSettings(e.Settings)
	policy, nerr := projectdto.NormalizeShippingPolicy(fields.ShippingBaseFee, fields.ShippingFreeThreshold)
	if nerr != nil {
		// 原文进日志（带工程 id 便于定位到具体哪一行配置），对外按零值继续。
		logger.Scene("project").With("project", id).Error(nerr,
			"站点运费配置非法（负数或超上限），本次结算按不收运费处理；请到站点设置页改正")
		return projectdto.ShippingPolicy{}, nil
	}
	return policy, nil
}
