package runtimefragment

// membership.go — 会员身份片段（BIZ-3 消费侧接入）。
//
// 为什么会员信息必须走运行时片段，而不是烘进静态产物：
// 等级与权益是**每个访客各自的事实**，而产物按语言维度编译
//（internal/pipeline/artifact.go 的 Manifest.Lang）—— 没有访客级维度，
// 「静态产物对所有人是同一份字节」。把「会员价 / 等级名」烘进去，等于让所有访客
// 都看到第一位会员的等级（AGENTS.md 不变量 1）。
//
// 两个能力，形态与 accountPanel 同口径（GET + anonymous）：
//
//	membershipBadge —— 一枚等级角标（页头小字），数据只有等级名；
//	membershipPanel —— 完整面板（等级 + 权益），给「我的会员」区块用。
//
// 为什么 anonymous 而不是 session：**不是**因为「谁都能看」，而是因为未登录必须拿到
// 一句引导。AuthSession 会直接回 401，而 HTMX 默认不替换 401 响应的目标节点 ——
// 访客会看到一个毫无变化的面板，完全不知道发生了什么（同 ordersList / accountPanel 的取舍）。
// 真正的门槛在数据侧：没有身份就没有会员身份，未登录一律只渲染引导文案。
//
// 两个端口都可缺（装配期注入）：
//
//	Reader      —— 解析一条会员身份（收窄的只读接口，拿不到等级 CRUD 与归属写入）；
//	FacingTexter —— 把本模块的业务错误转成可展示的一句话。**必须有它**：
//	                 片段层只依赖 contract 与不可变 dto，拿不到 membership 的 enums 白名单，
//	                 没有这个出口就只能直出 err.Error()（把 PostgreSQL 原文漏到页面上）
//	                 或一律通用提示（吞掉「这个工程还没配默认等级」这类可行动差异）。
//
// 任一端口未注入或解析失败一律渲染**可见文案**，绝不 500：
// 片段端点把 error 变成 500 + 一句「片段渲染失败」，对访客没有任何信息量
// （同 renderCartNotice 的取舍）。

import (
	"context"
	"fmt"
	"strings"

	membershipcontract "go_wp/internal/module/membership/contract"
	membershipdto "go_wp/internal/module/membership/dto"
	rfenums "go_wp/internal/module/runtimefragment/enums"
	"go_wp/internal/templates"
)

// 两个能力名（同时是模板里的 data-fragment 值）。
const (
	membershipFragmentBadge = "membershipBadge"
	membershipFragmentPanel = "membershipPanel"
)

// membershipReader 会员身份读取端口（装配期注入一次；可缺）。
//
// 收窄到 Reader：片段层拿不到等级 CRUD、归属写入与重算 —— 越权防护靠接口形状，
// 不靠调用方自觉（同 cartProvider / sitePageResolver 的手法）。
var membershipReader membershipcontract.Reader

// SetMembershipReader 注入会员身份读取端口（装配期调用）。
func SetMembershipReader(reader membershipcontract.Reader) { membershipReader = reader }

// membershipFacing 会员业务错误的文案出口（装配期注入一次；可缺）。
//
// 可缺时的降级是「用本地通用文案」而不是直出 err.Error()：内部错误原文
// （表名 / 约束名 / SQLSTATE）不是可以出现在访客页面上的东西。
var membershipFacing membershipcontract.FacingTexter

// SetMembershipFacingTexter 注入会员文案出口（装配期调用）。
func SetMembershipFacingTexter(texter membershipcontract.FacingTexter) { membershipFacing = texter }

func init() {
	Register(Spec{Type: membershipFragmentBadge, Method: "GET", Auth: AuthAnonymous, Render: renderMembershipBadge})
	Register(Spec{Type: membershipFragmentPanel, Method: "GET", Auth: AuthAnonymous, Render: renderMembershipPanel})
}

// membershipFragmentData 两个会员片段共用的模板数据。
//
// 共用一个结构而不是两份：它们的差异只有「渲染多少字段」，字段集完全重叠 ——
// 拆开只会让「新增一条权益展示」变成要改两处。
type membershipFragmentData struct {
	// Fragment 能力名（模板写成 data-fragment 属性，与 loginPanel / cartSummary 同口径）。
	Fragment string
	// NeedLogin 未登录：渲染引导 + 登录链接，而不是一个空白的面板。
	NeedLogin bool
	// Unavailable 端口未注入（装配缺失）：渲染可见提示。
	//
	// 它和 NeedLogin 必须分开：前者要运维去接装配，后者要访客去登录 ——
	// 两者页面上长得一样的话，装配缺陷会被当成「这站要登录」而被忽略很久。
	Unavailable bool
	// ProjectMissing 页面作者没给 projectId：等级按工程定义，没有工程就没有可查的范围。
	ProjectMissing bool
	// Failed 会员身份读不出来（默认等级缺失 / 库不可用）：渲染可见文案，不 500。
	Failed bool
	// Notice 上面任意一种降级形态的一句话（已按请求语言取词）。
	Notice string

	// TierName 生效的等级名；回退到默认等级时也照常给出。
	TierName string
	// IsDefaultTier 本次结果是**默认等级兜底**（这个访客还没有归属行）。
	//
	// 必须让访客看得出区别：「你是这个等级的会员」与「你还没成为会员，等级是兜底值」
	// 是两件事 —— 后者才该显示「消费满 X 升级」之类的引导。
	IsDefaultTier bool
	// FreeShipping 该等级是否免运费。
	FreeShipping bool
	// DiscountPercent 折扣扣减百分比（0 = 无折扣权益）。
	DiscountPercent int64
	// FreeShippingText / DiscountText 两条权益的展示文案（无该权益时为空）。
	FreeShippingText string
	DiscountText     string

	Labels membershipLabels
}

// membershipLabels 两个会员片段的固定文案（由 Go 预翻译后注入 jet）。
//
// 取词一律 `r.tr(rfenums.Xxx, "中文兜底")`：词条缺失时回落中文，
// 而不是在页面上显示裸 key。
type membershipLabels struct {
	Title          string
	Guest          string
	Unavailable    string
	ProjectMissing string
	Failed         string
	DefaultHint    string
	Discount       string // 带一个 %d：扣减百分比
	FreeShipping   string
	NoBenefit      string
}

func membershipLabelsOf(r *Request) membershipLabels {
	return membershipLabels{
		Title:          r.tr(rfenums.MembershipTitle, "我的会员"),
		Guest:          r.tr(rfenums.MembershipGuest, "登录后可以查看你的会员等级与专属权益。"),
		Unavailable:    r.tr(rfenums.MembershipUnavailable, "会员信息暂时不可用。"),
		ProjectMissing: r.tr(rfenums.MembershipProjectMissing, "这个页面还没指定站点工程，会员信息无法显示。"),
		Failed:         r.tr(rfenums.MembershipFailed, "会员信息暂时读不出来 —— 稍后刷新页面再试。"),
		DefaultHint:    r.tr(rfenums.MembershipDefaultHint, "还不是会员：消费累积到门槛后会自动升级。"),
		Discount:       r.tr(rfenums.MembershipDiscount, "%d%% 折扣"),
		FreeShipping:   r.tr(rfenums.MembershipFreeShipping, "免运费"),
		NoBenefit:      r.tr(rfenums.MembershipNoBenefit, "这个等级暂无专属权益。"),
	}
}

// renderMembershipBadge 等级角标。
func renderMembershipBadge(ctx context.Context, r *Request) (string, error) {
	return renderMembershipFragment(ctx, r, membershipFragmentBadge, "membership_badge")
}

// renderMembershipPanel 会员面板（等级 + 权益）。
func renderMembershipPanel(ctx context.Context, r *Request) (string, error) {
	return renderMembershipFragment(ctx, r, membershipFragmentPanel, "membership_panel")
}

// renderMembershipFragment 两个形态共用的取数与降级逻辑。
//
// 降级的四条分支都在这里定案（模板只负责显示），顺序即优先级：
// 端口未注入 → 未给工程 → 未登录 → 解析失败。
// 把「未给工程」排在「未登录」之前是刻意的：工程缺失是页面作者的配置问题，
// 让访客去登录也修不好它。
func renderMembershipFragment(ctx context.Context, r *Request, capability, templateName string) (string, error) {
	data := membershipFragmentData{Fragment: capability, Labels: membershipLabelsOf(r)}

	projectID := strings.TrimSpace(paramOf(r, "projectId"))
	userID, loggedIn := visitorIDOf(r)

	switch {
	case membershipReader == nil:
		data.Unavailable = true
		data.Notice = data.Labels.Unavailable
	case projectID == "":
		data.ProjectMissing = true
		data.Notice = data.Labels.ProjectMissing
	case !loggedIn:
		// 未登录：给引导，**不是** 401（见文件头）。
		data.NeedLogin = true
		data.Notice = data.Labels.Guest
	default:
		member, err := membershipReader.Resolve(ctx, &membershipdto.ResolveReq{
			ProjectID: projectID,
			UserID:    userID,
		})
		if err != nil || member == nil {
			// 读不出来（默认等级缺失 / 库不可用）：可见文案 + 原文只进日志。
			// 文案走 membership 的契约出口 —— 片段层拿不到它的 enums 白名单，
			// 没有这一层就只能直出 err.Error()。
			data.Failed = true
			data.Notice = membershipFacingText(r, err, data.Labels.Failed)
			break
		}
		data.TierName = member.TierName
		data.IsDefaultTier = member.IsDefaultTier
		data.FreeShipping = member.FreeShipping
		data.DiscountPercent = member.DiscountPercent
		if member.FreeShipping {
			data.FreeShippingText = data.Labels.FreeShipping
		}
		if member.DiscountPercent > 0 {
			data.DiscountText = fmt.Sprintf(data.Labels.Discount, member.DiscountPercent)
		}
	}

	return templates.RenderFragment(templateName, data)
}

// membershipFacingText 把会员模块的错误转成一句可展示文案（拿不到出口时用本地兜底）。
//
// err 为 nil 时直接给兜底文案：走到这里说明 member 也是 nil（契约承诺「不存在返回业务错误」，
// 但片段层不押注在调用方一定这么做 —— 一个 nil 会员静默渲染成空面板，
// 访客会以为是自己的会员没了）。
func membershipFacingText(r *Request, err error, fallback string) string {
	if membershipFacing == nil || err == nil {
		return fallback
	}
	if text := strings.TrimSpace(membershipFacing.FacingText(r.Lang, err)); text != "" {
		return text
	}
	return fallback
}
