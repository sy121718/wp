package membershipmcp

// membership_write_tools.go — 会员的写工具：手工设级、取消手工锁定、等级增改删。
//
// 这一组里最需要说清的是**手工锁定的语义**：AssignManual 把归属的 source 写成 manual，
// 之后日结的自动重算不再碰它 —— 也就是说「设成 VIP」不是设一次、而是**接管**这个会员，
// 他会一直停在那一级，哪怕消费额早就够下一档。反向的 UnlockManual 也不立刻改级别，
// 只把归属交还自动重算（等级要等下一次日结才变），所以解锁不会造成一次可见的跳变。
//
// 这两句话必须出现在工具描述里。它们不是实现细节：用户说「把他设成 VIP」时，
// 心里想的通常是「给他个 VIP 待遇」，而这个动作的实际后果是「以后自动升级不再管他」。
// 描述里不写，模型不会主动说，用户也不会想到要问。

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"go_wp/internal/mcp"
	membershipcontract "go_wp/internal/module/membership/contract"
	membershipdto "go_wp/internal/module/membership/dto"
	membershipenums "go_wp/internal/module/membership/enums"
	"go_wp/internal/permission"
)

// WriteTools 返回会员模块的写工具集。
func WriteTools(w membershipcontract.Writer) ([]mcp.Tool, error) {
	if w == nil {
		return nil, errors.New("membershipmcp: 会员写依赖缺失（装配期接线错误）")
	}
	return []mcp.Tool{
		memberAssignSet(w),
		memberAssignUnlock(w),
		memberTierCreate(w),
		memberTierUpdate(w),
		memberTierDelete(w),
	}, nil
}

// —— 归属 ——

type memberAssignSetArgs struct {
	ProjectID string `json:"projectId"`
	UserID    uint64 `json:"userId"`
	TierID    int64  `json:"tierId"`
}

func memberAssignSet(w membershipcontract.Writer) mcp.Tool {
	return mcp.NewWrite("member_assign_set", "手工指定会员等级",
		"把某个客户手工指定到某个会员等级（如「把张三设成 VIP」）。\n"+
			"**这不是一次性的等级调整，而是接管**：指定之后这名会员的等级不再随消费额自动重算，"+
			"会一直停在所设的等级上，哪怕他后来消费够了更高的门槛。要交还自动管理，"+
			"用 member_assign_unlock。\n"+
			"所以执行前要把这句话讲给用户，并确认他确实想要「不再自动升级」——"+
			"如果他只是想让这个人享受 VIP 待遇一次，那是别的事，不要用这个工具。\n"+
			"userId 是**客户 id**（用 customer_find 拿），tierId 用 member_tiers 拿，"+
			"两者必须属于同一个工程。",
		permission.MembershipAssignSet,
		mcp.Object("手工指定会员等级参数", map[string]mcp.Schema{
			"projectId": mcp.String("站点工程 id（uuid，必填）"),
			"userId":    mcp.Integer("客户 id（用 customer_find 拿到的 id，不是登录账号名）"),
			"tierId":    mcp.Integer("会员等级 id（用 member_tiers 拿到的 id）"),
		}, "projectId", "userId", "tierId"),
		nil,
		func(ctx context.Context, args memberAssignSetArgs) (mcp.Result, error) {
			res, err := w.AssignManual(ctx, &membershipdto.AssignManualReq{
				ProjectID: strings.TrimSpace(args.ProjectID),
				UserID:    args.UserID,
				TierID:    args.TierID,
			})
			if err != nil {
				return mcp.Result{}, err
			}
			return mcp.Result{Text: assignedText(res)}, nil
		})
}

// assignedText 手工设级的回执。
//
// 必须复述「已接管」这一后果：回执是用户唯一能看到实际发生了什么的地方，
// 而界面上「客户 2 → 金卡」这行字与自动升级的结果长得一模一样。
func assignedText(res *membershipdto.AssignmentResp) string {
	if res == nil {
		return "已手工指定会员等级。"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "已把客户 %d 手工指定为「%s」（tierId=%d）。", res.UserID, strings.TrimSpace(res.TierName), res.TierID)
	b.WriteString("\n注意：这已**接管**该会员的等级 —— 之后不再随消费额自动重算；" +
		"消费够了更高门槛也不会自动升上去。要交还自动管理用 member_assign_unlock。")
	return b.String()
}

type memberAssignUnlockArgs struct {
	ProjectID string `json:"projectId"`
	UserID    uint64 `json:"userId"`
}

func memberAssignUnlock(w membershipcontract.Writer) mcp.Tool {
	return mcp.NewWrite("member_assign_unlock", "取消手工锁定会员等级",
		"把某个客户交还给自动管理：等级重新由消费额决定。\n"+
			"**等级不会立刻变** —— 只把归属标记成交还，实际等级要等下一次日结重算才调整。"+
			"这是有意的：解锁本身不该造成一次看得见的等级跳变，运营可以先看一眼再让它跑。\n"+
			"回答里要说清这一点，否则用户会以为按钮没生效。\n"+
			"如果客户当前等级本来就是自动的，工具会如实告诉你，那不是失败。",
		permission.MembershipAssignUnlock,
		mcp.Object("取消手工锁定参数", map[string]mcp.Schema{
			"projectId": mcp.String("站点工程 id（uuid，必填）"),
			"userId":    mcp.Integer("客户 id（用 customer_find / member_find 拿到的 id）"),
		}, "projectId", "userId"),
		nil,
		func(ctx context.Context, args memberAssignUnlockArgs) (mcp.Result, error) {
			if err := w.UnlockManual(ctx, &membershipdto.UnlockManualReq{
				ProjectID: strings.TrimSpace(args.ProjectID),
				UserID:    args.UserID,
			}); err != nil {
				return mcp.Result{}, err
			}
			return mcp.Result{Text: unlockedText(args.UserID)}, nil
		})
}

// unlockedText 解锁回执。
//
// UnlockManual 只回 error，不回等级名（它**故意不动 tier_id**，见契约注释）——
// 所以这里不能报「当前等级是 X」，那会是一句我们其实没读到的话。
func unlockedText(userID uint64) string {
	return fmt.Sprintf("客户 %d 已交还自动管理。\n"+
		"等级要等下一次日结按消费额重算才会调整 —— 现在没变是正常的，不是没生效。", userID)
}

// —— 等级 ——

type tierEntitlementArg struct {
	Kind     string `json:"kind"`
	ValueInt int64  `json:"valueInt"`
}

type tierCreateArgs struct {
	ProjectID      string               `json:"projectId"`
	Name           string               `json:"name"`
	SortOrder      int                  `json:"sortOrder"`
	ThresholdCents int64                `json:"thresholdCents"`
	IsDefault      bool                 `json:"isDefault"`
	Remark         string               `json:"remark"`
	Entitlements   []tierEntitlementArg `json:"entitlements"`
}

func memberTierCreate(w membershipcontract.Writer) mcp.Tool {
	return mcp.NewWrite("member_tier_create", "新建会员等级",
		"给某个工程新增一个会员等级（如「黄金会员」），可一次带上权益。\n"+
			"sortOrder 是**等级高低，越大越高**：解析「消费额落在哪一档」时按它降序取第一个够门槛的档。"+
			"所以它不只是显示顺序 —— 排错了会让高消费客户被解析成低等级。\n"+
			"thresholdCents 的单位是**分**（与订单金额同口径）：说「满 1000 元」时要传 100000，"+
			"别直接传 1000（那会被读成 10 元）。留 0 表示「无需消费即可进入此档」，通常只用在默认等级上。\n"+
			"isDefault 是默认等级：每工程**只能有一个**，新客户没有消费记录时落在它上面；"+
			"设了一个新的默认，原来那个会被自动取消。\n"+
			"权益两种：kind=free_shipping 时 valueInt 取 1（免运费）；"+
			"kind=discount 时 valueInt 取 1..100 表示**扣减百分比**（20 = 打八折，不是「20% 折扣」）。",
		permission.MembershipTierCreate,
		mcp.Object("新建会员等级参数", map[string]mcp.Schema{
			"projectId":      mcp.String("站点工程 id（uuid，必填）"),
			"name":           mcp.String("等级名称（如「黄金会员」）"),
			"sortOrder":      mcp.Integer("等级高低，**越大越高**（决定消费额落在哪一档，不只是显示顺序）"),
			"thresholdCents": mcp.Integer("升级门槛，单位**分**（1000 元 = 100000；0 = 无需消费即可进入，通常只给默认等级）"),
			"isDefault":      mcp.Boolean("是否设为默认等级（每工程只能有一个；新客户无消费记录时落在这里）"),
			"remark":         mcp.String("备注（可选，给运营看的内部说明）"),
			"entitlements": mcp.Array("权益清单（可选）：kind=free_shipping 时 valueInt 取 1；"+
				"kind=discount 时 valueInt 取 1..100 表示扣减百分比（20 = 打八折）",
				mcp.Object("一条权益", map[string]mcp.Schema{
					"kind":     mcp.Enum("权益类型", membershipenums.KindFreeShipping, membershipenums.KindDiscount),
					"valueInt": mcp.Integer("取值：free_shipping 用 1；discount 用 1..100（扣减百分比）"),
				}, "kind", "valueInt")),
		}, "projectId", "name", "sortOrder"),
		nil,
		func(ctx context.Context, args tierCreateArgs) (mcp.Result, error) {
			ents, err := toEntitlements(args.Entitlements)
			if err != nil {
				return mcp.Result{}, err
			}
			res, err := w.CreateTier(ctx, &membershipdto.CreateTierReq{
				ProjectID:       strings.TrimSpace(args.ProjectID),
				Name:            strings.TrimSpace(args.Name),
				SortOrder:       args.SortOrder,
				ThresholdAmount: args.ThresholdCents,
				IsDefault:       args.IsDefault,
				Remark:          strings.TrimSpace(args.Remark),
				Entitlements:    ents,
			})
			if err != nil {
				return mcp.Result{}, err
			}
			return mcp.Result{Text: tierCreatedText(res)}, nil
		})
}

// toEntitlements 工具入参 → dto；顺带把取值范围的错话在这里说清。
//
// service 也会判这两条，但它的错是枚举 key（用户看不懂）。
func toEntitlements(items []tierEntitlementArg) ([]membershipdto.EntitlementReq, error) {
	out := make([]membershipdto.EntitlementReq, 0, len(items))
	for _, it := range items {
		kind := strings.TrimSpace(it.Kind)
		switch kind {
		case membershipenums.KindFreeShipping:
			if it.ValueInt != 0 && it.ValueInt != 1 {
				return nil, &mcp.ArgsError{Msg: "free_shipping 的 valueInt 只能是 1（免运费）或 0（不免）"}
			}
		case membershipenums.KindDiscount:
			if it.ValueInt < 1 || it.ValueInt > 100 {
				return nil, &mcp.ArgsError{Msg: "discount 的 valueInt 是**扣减百分比**，取 1..100（20 表示打八折）"}
			}
		default:
			return nil, &mcp.ArgsError{Msg: "权益 kind 只能是 free_shipping 或 discount"}
		}
		out = append(out, membershipdto.EntitlementReq{Kind: kind, ValueInt: it.ValueInt})
	}
	return out, nil
}

func tierCreatedText(res *membershipdto.TierResp) string {
	if res == nil {
		return "已新建会员等级。"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "已新建会员等级「%s」（id=%d，高低排序 %d）。", strings.TrimSpace(res.Name), res.ID, res.SortOrder)
	b.WriteString("\n")
	b.WriteString(thresholdText(res))
	if res.IsDefault {
		b.WriteString("\n它已设为**默认等级**：新客户没有消费记录时落在这里。")
	}
	if line := entitlementsText(res.Entitlements); line != "" {
		b.WriteString("\n权益：" + line)
	}
	return b.String()
}

type tierUpdateArgs struct {
	ProjectID      string `json:"projectId"`
	TierID         int64  `json:"tierId"`
	Name           string `json:"name"`
	SortOrder      *int   `json:"sortOrder"`
	ThresholdCents *int64 `json:"thresholdCents"`
	IsDefault      *bool  `json:"isDefault"`
	Remark         string `json:"remark"`
}

func memberTierUpdate(w membershipcontract.Writer) mcp.Tool {
	return mcp.NewWrite("member_tier_update", "修改会员等级",
		"改一个会员等级的名称、高低排序、升级门槛、默认标记或备注。\n"+
			"只传要改的那几项，没传的保持不变。\n"+
			"改 sortOrder 与 thresholdYuan 会影响**实时解析**：下一个请求进来时，"+
			"会员就会被分到新的档上（自动管理的会员不用等到日结）。"+
			"所以调低门槛或调高排序之前，先跟用户说清会影响哪些人。\n"+
			"tierId 用 member_tiers 拿到的 id。改权益用不着这个工具 —— 见描述末尾。",
		permission.MembershipTierUpdate,
		mcp.Object("修改会员等级参数", map[string]mcp.Schema{
			"projectId":      mcp.String("站点工程 id（uuid，必填）"),
			"tierId":         mcp.Integer("要改的等级 id（用 member_tiers 拿到的 id）"),
			"name":           mcp.String("新的等级名称（可选）"),
			"sortOrder":      mcp.Integer("新的高低排序，越大越高（可选；改它会影响会员被分到哪一档）"),
			"thresholdCents": mcp.Integer("新的升级门槛，单位**分**（可选；1000 元要传 100000）"),
			"isDefault":      mcp.Boolean("是否设为默认等级（可选；每工程只能有一个，设新的会取消旧的）"),
			"remark":         mcp.String("新的备注（可选）"),
		}, "projectId", "tierId"),
		nil,
		func(ctx context.Context, args tierUpdateArgs) (mcp.Result, error) {
			req := &membershipdto.UpdateTierReq{
				ProjectID: strings.TrimSpace(args.ProjectID),
				TierID:    args.TierID,
				SortOrder: args.SortOrder,
				IsDefault: args.IsDefault,
			}
			if name := strings.TrimSpace(args.Name); name != "" {
				req.Name = &name
			}
			req.ThresholdAmount = args.ThresholdCents
			if remark := strings.TrimSpace(args.Remark); remark != "" {
				req.Remark = &remark
			}
			if req.Name == nil && req.SortOrder == nil && req.ThresholdAmount == nil &&
				req.IsDefault == nil && req.Remark == nil {
				return mcp.Result{}, &mcp.ArgsError{
					Msg: "至少要改一项：name / sortOrder / thresholdCents / isDefault / remark 五个全空的话这次调用没有意义。"}
			}
			res, err := w.UpdateTier(ctx, req)
			if err != nil {
				return mcp.Result{}, err
			}
			return mcp.Result{Text: tierUpdatedText(res)}, nil
		})
}

func tierUpdatedText(res *membershipdto.TierResp) string {
	if res == nil {
		return "已更新会员等级。"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "已更新会员等级「%s」：", strings.TrimSpace(res.Name))
	b.WriteString(thresholdText(res))
	if res.IsDefault {
		b.WriteString("；它是**默认等级**")
	}
	return b.String()
}

type tierDeleteArgs struct {
	ProjectID string `json:"projectId"`
	TierID    int64  `json:"tierId"`
}

func memberTierDelete(w membershipcontract.Writer) mcp.Tool {
	return mcp.NewWrite("member_tier_delete", "删除会员等级",
		"删除一个会员等级（软删，记录保留可恢复）。\n"+
			"**还挂着会员的等级删不掉**：如果这个等级下还有归属（自动或手工），调用会被拒。"+
			"先让用户把那些会员改到别的等级（member_assign_set）或解锁交还自动管理，"+
			"再删。这条限制是有意的 —— 直接删会让那批会员的 tier_id 指向一个不存在的档。\n"+
			"默认等级通常也删不掉（新客户必须有地方可落），真删之前先设一个新的默认。",
		permission.MembershipTierDelete,
		mcp.Object("删除会员等级参数", map[string]mcp.Schema{
			"projectId": mcp.String("站点工程 id（uuid，必填）"),
			"tierId":    mcp.Integer("要删的等级 id（用 member_tiers 拿到的 id）"),
		}, "projectId", "tierId"),
		nil,
		func(ctx context.Context, args tierDeleteArgs) (mcp.Result, error) {
			if err := w.DeleteTier(ctx, &membershipdto.DeleteTierReq{
				ProjectID: strings.TrimSpace(args.ProjectID),
				TierID:    args.TierID,
			}); err != nil {
				return mcp.Result{}, err
			}
			return mcp.Result{Text: fmt.Sprintf("已删除会员等级（tierId=%d）。它是软删，记录还在，需要时可以恢复。", args.TierID)}, nil
		})
}
