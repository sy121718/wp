package ordermcp

// coupon_tools.go — 优惠券的读与写。
//
// 两张口径必须在工具描述里说清，否则模型一定会填错：
//  1. percent 的 DiscountValue 是**减免的百分比**（1..100，100 = 全免），不是「打几折」；
//     90 是「减 90%」而不是「打九折」。service 生成的 DiscountLabel 就是这个口径。
//  2. fixed 的 DiscountValue 单位是**分**（与订单金额同口径），门槛 MinSubtotal 也是分。
//
// 另外 update 走的是 CouponSaveReq（**整份覆盖**），不是「只改传进来的字段」——
// 想改一个名字也必须把其余字段原样带回来，所以描述里强制要求先 coupon_get。

import (
	"context"
	"fmt"
	"strings"

	"go_wp/internal/mcp"
	orderdto "go_wp/internal/module/order/dto"
	"go_wp/internal/permission"
	"go_wp/pkg/utils"
)

const (
	couponDefaultLimit = 10
	couponMaxLimit     = 50
)

// CouponWriteReq 优惠券的建/改入参。
//
// 除 ProjectID 外全部必填 —— 与 service 的覆盖语义一致：
// 漏一个字段不是「不改它」，而是「把它清成零值」（门槛变 0、次数变不限）。
type CouponWriteReq struct {
	ProjectID     string `json:"projectId"`
	Name          string `json:"name"`
	DiscountType  string `json:"discountType"`
	DiscountValue int64  `json:"discountValue"`
	MinSubtotal   int64  `json:"minSubtotal,omitempty"`
	MaxUses       int    `json:"maxUses,omitempty"`
	PerUserLimit  int    `json:"perUserLimit,omitempty"`
	StartsAt      string `json:"startsAt,omitempty"`
	EndsAt        string `json:"endsAt,omitempty"`
	Status        int    `json:"status"`
	Remark        string `json:"remark,omitempty"`
}

// CouponUpdateReq 改动已有券：多一个 couponId，且券码不可改。
type CouponUpdateReq struct {
	CouponID uint64 `json:"couponId"`
	CouponWriteReq
}

// CouponUpdateRaw 是 UpdateArgs 的落地形状（内嵌结构体的 json 标签在扁平复用时不直观，
// 这里显式展开，避免 mcp 的 schema 反射与实际入参对不上）。
type CouponUpdateRaw struct {
	CouponID      uint64 `json:"couponId"`
	ProjectID     string `json:"projectId"`
	Name          string `json:"name"`
	DiscountType  string `json:"discountType"`
	DiscountValue int64  `json:"discountValue"`
	MinSubtotal   int64  `json:"minSubtotal,omitempty"`
	MaxUses       int    `json:"maxUses,omitempty"`
	PerUserLimit  int    `json:"perUserLimit,omitempty"`
	StartsAt      string `json:"startsAt,omitempty"`
	EndsAt        string `json:"endsAt,omitempty"`
	Status        int    `json:"status"`
	Remark        string `json:"remark,omitempty"`
}

type CouponListArgs struct {
	ProjectID string `json:"projectId"`
	Keyword   string `json:"keyword,omitempty"`
	Status    string `json:"status,omitempty"`
	PageSize  int    `json:"pageSize,omitempty"`
	Offset    int    `json:"offset,omitempty"`
}

// CouponDeleteArgs 删券只吃 id。
type CouponDeleteArgs struct {
	CouponID uint64 `json:"couponId"`
}

// CouponReader / CouponWriter 是本模块对优惠券的窄接口。
//
// 刻意不把 CouponValidate（试算）与 ListRedemptions（核销记录）放进来：
// 试算是下单链路的一部分，核销记录是审计材料，都不是「改券」这件事的一部分。
type CouponReader interface {
	GetCoupon(ctx context.Context, couponID uint64) (*orderdto.CouponResp, error)
	ListCoupons(ctx context.Context, req *orderdto.CouponListReq) (*orderdto.CouponListResp, error)
}

type CouponWriter interface {
	CreateCoupon(ctx context.Context, req *orderdto.CouponSaveReq) (*orderdto.CouponResp, error)
	UpdateCoupon(ctx context.Context, req *orderdto.CouponSaveReq) (*orderdto.CouponResp, error)
	DeleteCoupon(ctx context.Context, couponID uint64) error
}

// CouponTools 装配优惠券工具。
func CouponTools(r CouponReader, w CouponWriter, store mcp.IdempotencyStore) ([]mcp.Tool, error) {
	if r == nil {
		return nil, fmt.Errorf("优惠券读取器不能为空")
	}
	if w == nil {
		return nil, fmt.Errorf("优惠券写入器不能为空")
	}
	if store == nil {
		return nil, fmt.Errorf("幂等存储不能为空")
	}
	limitProp := mcp.Integer(fmt.Sprintf("最多返回多少张（默认 %d，上限 %d）", couponDefaultLimit, couponMaxLimit))

	list := mcp.New("coupon_list", "查优惠券",
		"按券码或名称模糊查本站的优惠券。返回每张券的 id、券码、力度、门槛、已用次数与**当前是否在生效**。\n"+
			"「当前是否在生效」是算出来的（过期 / 未开始 / 已用尽都会标出来）—— "+
			"只看启用状态会以为一张已经过期的券还在跑。\n"+
			"改券之前先用 coupon_get 取回完整字段。",
		permission.OrderCouponList,
		mcp.Object("查优惠券", map[string]mcp.Schema{
			"projectId": mcp.String("工程 id"),
			"keyword":   mcp.String("按券码或名称模糊匹配，可省略"),
			"status": mcp.Enum("按状态筛：enabled 生效中 / disabled 已停用 / expired 已过期 / exhausted 已用尽；省略=全部",
				"enabled", "disabled", "expired", "exhausted"),
			"pageSize": limitProp,
			"offset":   mcp.Integer("跳过多少条（翻页用，默认 0）"),
		}, "projectId"),
		func(ctx context.Context, args CouponListArgs) (mcp.Result, error) {
			projectID := strings.TrimSpace(args.ProjectID)
			if projectID == "" {
				return mcp.Result{}, &mcp.ArgsError{Msg: "projectId 不能为空"}
			}
			req := &orderdto.CouponListReq{
				ProjectID: projectID,
				Keyword:   strings.TrimSpace(args.Keyword),
				Status:    strings.TrimSpace(args.Status),
				Limit:     clampCouponLimit(args.PageSize),
				Offset:    clampOffset(args.Offset),
			}
			res, err := r.ListCoupons(ctx, req)
			if err != nil {
				return mcp.Result{}, err
			}
			return mcp.Result{Text: couponListText(res)}, nil
		})

	get := mcp.New("coupon_get", "看一张券的完整字段",
		"按 id 取回优惠券的全部字段。**改券之前必须跑这一步** —— "+
			"保存接口是整份覆盖（只发要改的那个字段会把其余字段清成零值：门槛变 0、次数变不限），"+
			"所以要先拿到现状，改完再把整套字段一起发回。",
		permission.OrderCouponGet,
		mcp.Object("取优惠券", map[string]mcp.Schema{
			"couponId": mcp.Integer("优惠券 id（从 coupon_list 拿）"),
		}, "couponId"),
		func(ctx context.Context, args CouponDeleteArgs) (mcp.Result, error) {
			if args.CouponID == 0 {
				return mcp.Result{}, &mcp.ArgsError{Msg: "couponId 不能为空"}
			}
			res, err := r.GetCoupon(ctx, args.CouponID)
			if err != nil {
				return mcp.Result{}, err
			}
			return mcp.Result{Text: couponDetailText(res)}, nil
		})

	saveProps := func() map[string]mcp.Schema {
		return map[string]mcp.Schema{
			"projectId": mcp.String("工程 id"),
			"name":      mcp.String("券的名称（给人看的，例如「双十一满减」）"),
			"discountType": mcp.Enum("力度口径：percent 按百分比减 / fixed 减固定金额。**必填**",
				"percent", "fixed"),
			"discountValue": mcp.Integer("力度值。percent 时填**要减掉的百分比**（1..100，" +
				"100 表示全免；注意 90 是「减 90%」不是「打九折」）；fixed 时填**分**（减 5 元填 500）"),
			"minSubtotal":  mcp.Integer("使用门槛（分）：订单小计低于它不能用。0 或不填=无门槛"),
			"maxUses":      mcp.Integer("这张券总共能用多少次。0 或不填=不限"),
			"perUserLimit": mcp.Integer("每人能用多少次。0 或不填=不限"),
			"startsAt":     mcp.String("生效开始时间，两种写法都行：2026-11-01 或 2026-11-01 09:00:00。省略=立即生效"),
			"endsAt":       mcp.String("生效结束时间，写法同上。省略=不过期。**必须晚于开始时间**，否则这张券永远不可能生效"),
			"status":       mcp.Integer("1 启用 / 0 停用。停用不删（历史核销记录还要读它）"),
			"remark":       mcp.String("内部备注，可省略"),
		}
	}

	create := mcp.NewWrite("coupon_create", "新建优惠券",
		"建一张优惠券。券码由服务端按工程规则生成，不用你填。\n"+
			"**力度口径容易填错，先确认再动手**：\n"+
			"* `discountType=percent` + `discountValue=90` 是「**减 90%**」（几乎全免），不是「打九折」。\n"+
			"* `discountType=fixed` + `discountValue=500` 是「减 5 元」—— 单位是**分**。\n"+
			"* 门槛 `minSubtotal` 同样是分。\n"+
			"建之前把口径和用户念一遍（「减多少、满多少能用、有效期是什么时候」），"+
			"券建出来就挂在活动上了。",
		permission.OrderCouponCreate,
		mcp.Object("新建优惠券", saveProps(), "projectId", "name", "discountType", "discountValue"),
		store,
		func(ctx context.Context, args CouponWriteReq) (mcp.Result, error) {
			res, err := w.CreateCoupon(ctx, couponSaveReq(args))
			if err != nil {
				return mcp.Result{}, err
			}
			return mcp.Result{Text: "优惠券已创建：\n" + couponDetailText(res) +
				"\n券码是它对外唯一的名字，建后不可改（改码等于换一张券，历史核销记录会指向一个查不到的码）。"}, nil
		})

	update := mcp.NewWrite("coupon_update", "改优惠券",
		"改一张已有的券。**这是整份覆盖，不是增量修改** —— "+
			"只发 name 会把门槛清成 0、次数清成不限、备注清空。\n"+
			"正确步骤：先用 `coupon_get` 取回当前全部字段，改掉要改的那一两个，"+
			"**再整套一起发回来**。\n"+
			"券码建后不可改，这里也不接受。\n"+
			"已经有人用过的券，改门槛或力度会影响后续订单的算价 —— 说清改了什么再动手。",
		permission.OrderCouponUpdate,
		mcp.Object("改优惠券", saveProps(), "couponId", "projectId", "name", "discountType", "discountValue"),
		store,
		func(ctx context.Context, args CouponUpdateRaw) (mcp.Result, error) {
			req := couponSaveReq(CouponWriteReq{
				ProjectID: args.ProjectID, Name: args.Name,
				DiscountType: args.DiscountType, DiscountValue: args.DiscountValue,
				MinSubtotal: args.MinSubtotal, MaxUses: args.MaxUses, PerUserLimit: args.PerUserLimit,
				StartsAt: args.StartsAt, EndsAt: args.EndsAt, Status: args.Status, Remark: args.Remark,
			})
			req.ID = args.CouponID
			res, err := w.UpdateCoupon(ctx, req)
			if err != nil {
				return mcp.Result{}, err
			}
			return mcp.Result{Text: "优惠券已更新：\n" + couponDetailText(res)}, nil
		})

	del := mcp.NewWrite("coupon_delete", "删除优惠券",
		"**删除**一张券，不可撤销，历史核销记录会失去它的名字。\n"+
			"大多数时候你要的是**停用**（`coupon_update` 传 status=0）—— "+
			"停用后不能再被使用，但券和它的核销记录都还在，能对账。\n"+
			"只有明显建错、从没被用过的券才该删。先 `coupon_get` 看清 usedCount。",
		permission.OrderCouponDelete,
		mcp.Object("删除优惠券", map[string]mcp.Schema{
			"couponId": mcp.Integer("要删除的优惠券 id"),
		}, "couponId"),
		store,
		func(ctx context.Context, args CouponDeleteArgs) (mcp.Result, error) {
			if args.CouponID == 0 {
				return mcp.Result{}, &mcp.ArgsError{Msg: "couponId 不能为空"}
			}
			if err := w.DeleteCoupon(ctx, args.CouponID); err != nil {
				return mcp.Result{}, err
			}
			return mcp.Result{Text: fmt.Sprintf("优惠券 id=%d 已删除。", args.CouponID)}, nil
		})

	return []mcp.Tool{list, get, create, update, del}, nil
}

func clampCouponLimit(v int) int {
	if v <= 0 {
		return couponDefaultLimit
	}
	if v > couponMaxLimit {
		return couponMaxLimit
	}
	return v
}

func clampOffset(v int) int {
	if v < 0 {
		return 0
	}
	return v
}

func couponSaveReq(a CouponWriteReq) *orderdto.CouponSaveReq {
	return &orderdto.CouponSaveReq{
		ProjectID:     strings.TrimSpace(a.ProjectID),
		Name:          strings.TrimSpace(a.Name),
		DiscountType:  strings.TrimSpace(a.DiscountType),
		DiscountValue: a.DiscountValue,
		MinSubtotal:   a.MinSubtotal,
		MaxUses:       a.MaxUses,
		PerUserLimit:  a.PerUserLimit,
		StartsAt:      strings.TrimSpace(a.StartsAt),
		EndsAt:        strings.TrimSpace(a.EndsAt),
		Status:        a.Status,
		Remark:        strings.TrimSpace(a.Remark),
	}
}

func couponListText(res *orderdto.CouponListResp) string {
	if res == nil || len(res.List) == 0 {
		return "没有符合条件的优惠券。"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "共 %d 张，本页 %d 张：\n", res.Total, len(res.List))
	for i, c := range res.List {
		fmt.Fprintf(&b, "%d. id=%d 券码 %s「%s」%s · 门槛 %s · 已用 %d/%s · 当前 %s\n",
			i+1, c.ID, c.Code, c.Name, firstNonEmpty(c.DiscountLabel, couponValueText(c)),
			firstNonEmpty(c.MinSubtotalLabel, centsLabel(c.MinSubtotal)),
			c.UsedCount, usesText(c.MaxUses), couponStateText(c.State))
	}
	return b.String()
}

func couponDetailText(c *orderdto.CouponResp) string {
	if c == nil {
		return "没有这张券。"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "* 券码 %s（建后不可改）\n", c.Code)
	fmt.Fprintf(&b, "* 名称：%s\n", emptyAsDash(c.Name))
	fmt.Fprintf(&b, "* 力度：%s\n", firstNonEmpty(c.DiscountLabel, couponValueText(c)))
	fmt.Fprintf(&b, "* 门槛：%s\n", firstNonEmpty(c.MinSubtotalLabel, centsLabel(c.MinSubtotal)))
	fmt.Fprintf(&b, "* 用量：已用 %d，总上限 %s，每人上限 %s\n",
		c.UsedCount, usesText(c.MaxUses), usesText(c.PerUserLimit))
	fmt.Fprintf(&b, "* 有效期：%s ~ %s（%s）\n", timeText(c.StartsAt), timeText(c.EndsAt), emptyAsDash(c.TimeZone))
	fmt.Fprintf(&b, "* 当前状态：%s（%s）\n", couponStateText(c.State), emptyAsDash(c.StatusLabel))
	if strings.TrimSpace(c.Remark) != "" {
		fmt.Fprintf(&b, "* 备注：%s\n", c.Remark)
	}
	fmt.Fprintf(&b, "* id=%d，工程 %s", c.ID, c.ProjectID)
	return b.String()
}

// couponValueText 是 DiscountLabel 缺席时的兜底。
//
// 口径与 service 的 couponDiscountLabel 一致：percent 是**减免的百分比**。
// 这里必须说「减 90%」而不是「打九折」—— 两者差 10 倍。
func couponValueText(c *orderdto.CouponResp) string {
	if c.DiscountType == "percent" {
		return fmt.Sprintf("减 %d%%", c.DiscountValue)
	}
	return fmt.Sprintf("减 %s", centsLabel(c.DiscountValue))
}

// timeText 时间窗的文本。nil 表示「不限」，与空串不是一回事 ——
// 券的「生效到什么时候」和「随时可用」对外表现完全不同。
func timeText(t *utils.JSONTime) string {
	if t == nil || t.IsZero() {
		return "不限"
	}
	return t.String()
}

func centsLabel(cents int64) string {
	return fmt.Sprintf("%.2f 元", float64(cents)/100)
}

func usesText(n int) string {
	if n <= 0 {
		return "不限"
	}
	return fmt.Sprintf("%d 次", n)
}

// couponStateText 把展示口径状态翻成中文。
//
// 用 State 而不是 Status：status=1 只说明「运营没手动停用」，
// 一张已经过期或用尽的券看 status 是看不出问题的。
func couponStateText(state string) string {
	switch strings.TrimSpace(state) {
	case "enabled":
		return "生效中"
	case "disabled":
		return "已停用"
	case "expired":
		return "已过期"
	case "not_started":
		return "未开始"
	case "exhausted":
		return "已用尽"
	}
	return emptyAsDash(state)
}
