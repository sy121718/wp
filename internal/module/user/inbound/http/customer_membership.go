package userhttp

// customer_membership.go — 客户详情页的会员等级展示（BIZ-3 消费侧接入）。
//
// **只加展示、不加领域**：本文件没有任何写方法 —— 不指定等级、不解锁、不重算。
// 那些是 membership 模块自己的后台页（/admin/membership/*）的职责，各带各的权限点；
// 在客户页上顺手给一个「改他的等级」按钮，等于把两个权限点合成了一个。
//
// 端口经装配期注入，形态照既有的 CustomerAdminPort / CustomerOrderSummaryReader：
//	Reader       —— 读一条会员身份（收窄的只读接口，拿不到等级 CRUD 与归属写入）；
//	FacingTexter —— 把会员模块的业务错误转成一句话（客户页拿不到它的 enums 白名单，
//	                没有这条出口就只能直出 err.Error() 或一律通用提示）。
//
// 两处「不炸页」的兜底：
//   · 端口未注入 / 没选工程 / 解析失败一律给**可见文案**，且渲染键**始终存在**
//     （Jet 的 `{{if .X}}` 遇到缺键会中断整页渲染 → 500，见 internal/templates/CLAUDE.md）；
//   · 等级名等字段用 isset 兜底，缺键不炸（渲染键恒设零值，模板侧仍有判断）。

import (
	"context"

	"github.com/gin-gonic/gin"

	membershipcontract "go_wp/internal/module/membership/contract"
	membershipdto "go_wp/internal/module/membership/dto"
	"go_wp/internal/web/shell"
	"go_wp/pkg/response"
)

// customerMembershipView 客户详情页上「会员等级」那一块的渲染数据。
//
// 零值即「什么都没有」：没有等级、没有权益、没有降级说明 —— 页面据此渲染空块而不是报错。
type customerMembershipView struct {
	// HasMembership 解析出了会员身份（含默认等级兜底）。
	HasMembership bool
	// TierName 生效的等级名。
	TierName string
	// IsDefaultTier 本次是默认等级兜底（这个客户还没有归属行）。
	//
	// 与「他是这个等级的会员」必须能分辨：运营看这一块是要判断「该不该给他手工指定等级」。
	IsDefaultTier bool
	// FreeShipping 该等级免运费。
	FreeShipping bool
	// DiscountPercent 折扣扣减百分比（0 = 无折扣权益）。
	DiscountPercent int64
	// Notice 降级说明（非空时模板只显示它，不显示等级）。三种形态：
	// 端口未接入 / 没选工程 / 解析失败。
	Notice string
}

// SetMembershipDisplay 注入会员展示所需的两个端口（装配期调用）。
//
// 两个一起注入而不是两个 setter：它们服务的是同一块展示，只接一半的中间态
// （读得到等级、错误却直出原文）没有任何部署理由，而分开注入一定会有人只接一半。
func (h *customerPageHandle) SetMembershipDisplay(reader membershipcontract.Reader, texter membershipcontract.FacingTexter) {
	if h == nil {
		return
	}
	h.membership = reader
	h.membershipFacing = texter
}

// membershipView 取该客户在某工程下的会员展示数据。
//
// 客户 id 为 0 或工程为空即返回**带说明的空视图**：详情页是在某个工程上算的
// （消费额与等级都按工程算），没有工程就没有可展示的等级 —— 这是正常状态，
// 不是错误（客户管理页在没有站点工程时也长这样）。
func (h *customerPageHandle) membershipView(ctx context.Context, c *gin.Context, projectID string, userID uint64) customerMembershipView {
	tr := shell.TranslateFor(c)
	if h.membership == nil {
		// 装配缺失：说清是「会员模块没接」，而不是让运营以为这个客户没有等级。
		return customerMembershipView{Notice: tr(customerMembershipUnavailableLabel.key, customerMembershipUnavailableLabel.fallback)}
	}
	if userID == 0 || projectID == "" {
		return customerMembershipView{Notice: tr(customerMembershipNoProjectLabel.key, customerMembershipNoProjectLabel.fallback)}
	}
	member, err := h.membership.Resolve(ctx, &membershipdto.ResolveReq{ProjectID: projectID, UserID: userID})
	if err != nil || member == nil {
		// 原文只进日志（经 FacingTexter 归口）：客户页拿不到会员模块的 enums 白名单，
		// 直出 err.Error() 会把 PostgreSQL 原文漏到页面上。
		return customerMembershipView{Notice: h.membershipFacingText(c, err)}
	}
	return customerMembershipView{
		HasMembership:   true,
		TierName:        member.TierName,
		IsDefaultTier:   member.IsDefaultTier,
		FreeShipping:    member.FreeShipping,
		DiscountPercent: member.DiscountPercent,
	}
}

// membershipFacingText 会员模块错误的展示文案（拿不到出口时用本地兜底）。
func (h *customerPageHandle) membershipFacingText(c *gin.Context, err error) string {
	tr := shell.TranslateFor(c)
	fallback := tr(customerMembershipFailedLabel.key, customerMembershipFailedLabel.fallback)
	if h.membershipFacing == nil || err == nil {
		return fallback
	}
	if text := h.membershipFacing.FacingText(response.RequestLanguage(c), err); text != "" {
		return text
	}
	return fallback
}

// applyCustomerMembership 把会员视图写进详情页渲染数据。
//
// 为什么是「往已有的 gin.H 里写键」而不是给 customerDetailPageData 加参数：
// 那个组装函数有 6 处调用点，其中 4 处在渲染测试里 —— 为一块展示改签名会把测试
// 一起卷进来（而它本身没有变化），换来的只是少写一行传参。
//
// **渲染键恒设**（包括空值）是硬要求：Jet 的 `{{if .X}}` 遇到缺失的键会中断整页渲染
// → 500 → htmx 不 swap（用户在浏览器里看不到任何反应）。
func applyCustomerMembership(data gin.H, view customerMembershipView) {
	data["MembershipNotice"] = view.Notice
	data["HasMembership"] = view.HasMembership
	data["MembershipTierName"] = view.TierName
	data["MembershipIsDefault"] = view.IsDefaultTier
	data["MembershipFreeShipping"] = view.FreeShipping
	data["MembershipDiscountPercent"] = view.DiscountPercent
}

// 三个降级说明（i18n key + 中文兜底，与页面其余展示标签同形态）。
var (
	customerMembershipUnavailableLabel = userLabel{"admin.customer_detail.membership.unavailable", "会员模块尚未接入，这里看不到等级。"}
	customerMembershipNoProjectLabel   = userLabel{"admin.customer_detail.membership.no_project", "还没有站点工程：会员等级按工程计算，选定工程后才能看到。"}
	customerMembershipFailedLabel      = userLabel{"admin.customer_detail.membership.failed", "会员等级暂时读不出来 —— 客户资料本身不受影响。"}
)
