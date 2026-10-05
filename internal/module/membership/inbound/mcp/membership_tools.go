package membershipmcp

// membership_tools.go — 会员模块的只读工具。
//
// 为什么补这一对：用户问「我们有几个会员等级」「金卡要花多少钱才能升」
// 「这个等级里有多少人」时，答案在 membership_tiers / membership_assignments 里，
// 而在此之前 AI 侧看不到任何会员数据 —— 它会说「我没有查会员的工具」。
//
// 只读：依赖收窄到 QueryReader（两个方法），手里没有 SaveTier、没有 Recalc ——
// 「AI 顺手把升级门槛改了」不会在某次改动里变得可能，而门槛一改会牵动
// 后续所有人的升降级。
//
// 门槛金额的单位是**分**（与订单金额同口径，见 TierResp 注释）：正文里换算成
// 元再展示，并标明口径 —— 直接念「50000」会让运营以为门槛是五万。

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"go_wp/internal/mcp"
	membershipcontract "go_wp/internal/module/membership/contract"
	membershipdto "go_wp/internal/module/membership/dto"
	"go_wp/internal/permission"
	"go_wp/pkg/utils"
)

const (
	memberDefaultLimit = 15
	memberMaxLimit     = 50
)

// Tools 返回会员模块的只读工具集。
func Tools(reader membershipcontract.QueryReader) ([]mcp.Tool, error) {
	if reader == nil {
		return nil, errors.New("membershipmcp: 会员查询依赖缺失（装配期接线错误）")
	}
	return []mcp.Tool{tierList(reader), memberFind(reader)}, nil
}

// —— 等级 ——

type tierListArgs struct {
	ProjectID string `json:"projectId"`
}

func tierList(reader membershipcontract.QueryReader) mcp.Tool {
	return mcp.New("member_tiers", "会员等级清单",
		"列出站点工程的会员等级，用于回答「有几个会员等级」「金卡门槛是多少」「每个等级有什么权益」。\n"+
			"门槛金额以**元**给出（库里存的是分），并注明是否按累计消费额自动升级。\n"+
			"要看某个等级里有哪些人，用 member_find 带上 tierId。",
		permission.MembershipTierList,
		mcp.Object("列会员等级参数", map[string]mcp.Schema{
			"projectId": mcp.String("站点工程 id（uuid）"),
		}, "projectId"),
		func(ctx context.Context, args tierListArgs) (mcp.Result, error) {
			if strings.TrimSpace(args.ProjectID) == "" {
				return mcp.Result{}, &mcp.ArgsError{Msg: "projectId 必填；先用 site_projects 查站点工程 id"}
			}
			list, err := reader.ListTiers(ctx, &membershipdto.ListTiersReq{ProjectID: args.ProjectID})
			if err != nil {
				return mcp.Result{}, err
			}
			return mcp.Result{Text: tierListText(list), Data: list}, nil
		})
}

func tierListText(list []*membershipdto.TierResp) string {
	if len(list) == 0 {
		return "这个工程还没有会员等级。"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "共 %d 个会员等级（由低到高）：\n", len(list))
	for i, t := range list {
		fmt.Fprintf(&b, "%d. id=%d「%s」· %s%s\n",
			i+1, t.ID, emptyAsDash(t.Name), thresholdText(t), defaultSuffix(t.IsDefault))
		if len(t.Entitlements) > 0 {
			fmt.Fprintf(&b, "   权益：%s\n", entitlementsText(t.Entitlements))
		}
	}
	return strings.TrimRight(b.String(), "\n")
}

// thresholdText 把分换算成元。
//
// 门槛是**累计消费额**的门槛（库里的单位是分）：直接念原始数字会让运营
// 把 50000 当成五万元。0 是合法门槛（默认等级的常见取值），所以按数值分支
// 而不是按零值判空。
func thresholdText(t *membershipdto.TierResp) string {
	if t.ThresholdAmount <= 0 {
		return "无门槛（默认等级）"
	}
	return fmt.Sprintf("累计消费满 %.2f 元可升级", float64(t.ThresholdAmount)/100)
}

func defaultSuffix(isDefault bool) string {
	if isDefault {
		return " · **默认等级**"
	}
	return ""
}

func entitlementsText(list []membershipdto.EntitlementResp) string {
	parts := make([]string, 0, len(list))
	for _, e := range list {
		parts = append(parts, strings.TrimSpace(e.Kind))
	}
	return strings.Join(parts, "、")
}

// —— 归属 ——

type memberFindArgs struct {
	ProjectID string `json:"projectId"`
	TierID    int64  `json:"tierId"`
	Source    string `json:"source"`
	UserID    uint64 `json:"userId"`
	Limit     int    `json:"limit"`
}

func memberFind(reader membershipcontract.QueryReader) mcp.Tool {
	return mcp.New("member_find", "查会员归属",
		"查「谁是哪一级会员」，用于回答「某个等级里有多少人」「这个人是不是会员」"+
			"「哪些人是手工设的等级」。\n"+
			"线索可以给等级 id、客户 id、来源（自动 / 手工），任一条都能单独用；都不给就列最近的一批。\n"+
			"等级名与门槛用 member_tiers 查（本工具只给归属关系与等级名）。\n"+
			"客户资料（邮箱 / 注册时间 / 最后登录）用 customer_get 按这里的 userId 查。",
		permission.MembershipAssignList,
		mcp.Object("查会员归属参数", map[string]mcp.Schema{
			"projectId": mcp.String("站点工程 id（uuid）"),
			"tierId":    mcp.Integer("限定某个等级 id（可选；从 member_tiers 拿）"),
			"source":    mcp.Enum("限定等级来源（可选；不传则不筛）", "auto", "manual"),
			"userId":    mcp.Integer("限定某个客户 id（可选；从 customer_find 拿）"),
			"limit":     mcp.Integer("最多返回几条，默认 15，上限 50"),
		}, "projectId"),
		func(ctx context.Context, args memberFindArgs) (mcp.Result, error) {
			if strings.TrimSpace(args.ProjectID) == "" {
				return mcp.Result{}, &mcp.ArgsError{Msg: "projectId 必填；先用 site_projects 查站点工程 id"}
			}
			list, err := reader.ListAssignments(ctx, &membershipdto.ListAssignmentsReq{
				ProjectID: args.ProjectID,
				TierID:    args.TierID,
				Source:    args.Source,
				UserID:    args.UserID,
				Page:      1,
				Size:      clampLimit(args.Limit),
			})
			if err != nil {
				return mcp.Result{}, err
			}
			return mcp.Result{Text: assignmentListText(list, args), Data: list}, nil
		})
}

func assignmentListText(list []*membershipdto.AssignmentResp, args memberFindArgs) string {
	scope := describeScope(args)
	if len(list) == 0 {
		return fmt.Sprintf("没有符合条件的会员归属记录（%s）。可能这一档还没有人，"+
			"也可能筛选条件写得太窄 —— 等级 id 用 member_tiers 确认。", scope)
	}
	var b strings.Builder
	fmt.Fprintf(&b, "查到 %d 条会员归属（%s）：\n", len(list), scope)
	for i, a := range list {
		fmt.Fprintf(&b, "%d. 客户 id=%d · %s · 来源 %s · 生效 %s\n",
			i+1, a.UserID, emptyAsDash(a.TierName), sourceText(a.Source), timeText(a.AssignedAt))
	}
	return strings.TrimRight(b.String(), "\n")
}

func describeScope(args memberFindArgs) string {
	parts := make([]string, 0, 3)
	if args.TierID > 0 {
		parts = append(parts, fmt.Sprintf("等级 id=%d", args.TierID))
	}
	if args.Source != "" {
		parts = append(parts, "来源 "+args.Source)
	}
	if args.UserID > 0 {
		parts = append(parts, fmt.Sprintf("客户 id=%d", args.UserID))
	}
	if len(parts) == 0 {
		return "全部"
	}
	return strings.Join(parts, "、")
}

// sourceText 归属来源。
//
// auto（按累计消费自动升降）与 manual（后台手工设定）要分开说：运营看到
// 「这个人怎么是金卡」时，下一步动作完全不同 —— 前者去看消费额，后者去问是谁设的。
func sourceText(source string) string {
	switch strings.ToLower(strings.TrimSpace(source)) {
	case "auto":
		return "自动（按累计消费）"
	case "manual":
		return "手工设定"
	case "":
		return "未标注"
	default:
		return source
	}
}

// timeText 归属生效时刻。
//
// 用 utils.JSONTime 的 String（它自己处理零值 → 空串）而不是 fmt %v 直接打：
// 零值的时间戳在正文里会显示成 0001-01-01，读起来像一个真实日期。
func timeText(t utils.JSONTime) string {
	if t.IsZero() {
		return "-"
	}
	return t.String()
}

func clampLimit(n int) int {
	if n <= 0 {
		return memberDefaultLimit
	}
	if n > memberMaxLimit {
		return memberMaxLimit
	}
	return n
}

func emptyAsDash(s string) string {
	if strings.TrimSpace(s) == "" {
		return "-"
	}
	return s
}
