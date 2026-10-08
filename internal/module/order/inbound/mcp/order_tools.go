package ordermcp

// 两张口径必须在工具描述里说清，否则模型一定会填错：
//  1. percent 的 DiscountValue 是**减免的百分比**（1..100，100 = 全免），不是「打几折」；
//     90 是「减 90%」而不是「打九折」。service 生成的 DiscountLabel 就是这个口径。
//  2. fixed 的 DiscountValue 单位是**分**（与订单金额同口径），门槛 MinSubtotal 也是分。
//
// 另外 update 走的是 CouponSaveReq（**整份覆盖**），不是「只改传进来的字段」——
// 想改一个名字也必须把其余字段原样带回来，所以描述里强制要求先 coupon_get。

// 与 order_tools.go 分开成两个文件与两个装配函数：它们的依赖接口不同
//（OrderRangeSummaryReader vs OrderOverviewReader），而工具层的依赖必须收窄到
// 「它真正需要的那几条只读方法」—— 合成一个函数会让只想给趋势的模块也被迫拿到榜单。

// 为什么补这一对：模块此前只有四个聚合（orders_summary / daily / top_products /
// status_counts），它们回答的都是「一共/每天/谁最好/各状态多少」；而用户最常问的
// 「订单 20261005001 到哪了」「张三那单发了没」——**一个线索指向一单**——没有任何入口。
// 底层一直是齐的（model 的 List 支持单号 / 客户名 / 邮箱关键词与时间窗，service 的
// GetOrderDetailByNo 连商品行与状态流水都取好了），缺的只是工具层的门。
//
// 两个工具都是只读：依赖收窄到 OrderQueryReader（两个方法），手里没有 ChangeStatus ——
// 「AI 顺手把订单改成已发货」不会在某次改动里悄悄变得可能。

// 这三个动作都只吃 orderId：工程作用域由 service 自己探测（`locateOrderProject` /
// `resolveOrderProject` 会逐工程找订单归属），**不要**让调用方传 projectId ——
// 传错工程时 orders 表的 FORCE 策略会让加锁读静默匹配 0 行，
// 症状是「订单不存在」，而订单明明在。
//
// 操作人身份从 ctx 注入（`mcp.UserIDFrom`），不从参数读：
// 模型会照用户口述的名字填，而状态流转日志是审计材料。

// 与 inbound/http 同构：一个模块的「对外能力」按消费者分目录，http 服务后台页面与
// 浏览器接口，mcp 服务 AI（进程内助手与外部 /mcp 两类消费者，见 docs/17 D2）。
// 装配期由上层汇总成一个注册表，运行期只读。

// 只开这一条写路径，理由见 contract/order_write.go：建单与后台手工建单页是同一件事，
// 而发货 / 退款 / 改单是对既有订单的不可逆或涉资金动作，留在人手里。
//
// 与库存的 stock_change 相比，这个工具多一层「金额不由调用方决定」的约束：
// dto 里 Subtotal / Total 是服务端算的，DiscountTotal 在无券时被忽略，
// 有券时以服务端试算为准。工具层因此**给不出价格** —— 用户说「按 99 元卖给他」
// 时，正确做法是先用 product_update 改价（或直接告诉用户改价在商品页），
// 而不是在这里塞一个金额。这一点写进了工具说明，否则模型会试着填 DiscountTotal
// 去凑数 —— 那在有券时被忽略、无券时也被忽略，用户看到的却是「已经按 99 元下单了」。

// 这条链路的每一步都有**不可逆的钱货后果**，描述里逐条写明：
//   - approve 带 autoReceive 时是「同意 + 当场入库 + 当场退款」一步到底；
//   - receive 是「货已到、入库 + 退款」；
//   - reject 是终态（客户的申请被驳回），remark 是给客户看的理由。
//
// 操作人身份从 ctx 注入（mcp.UserIDFrom），不从参数读 —— 审阅人是审计材料。

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"go_wp/internal/mcp"
	"go_wp/internal/module/order/contract"
	"go_wp/internal/module/order/dto"
	"go_wp/internal/module/order/enums"
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

// CouponUpdateRaw 把更新入参展平。mcp 的 schema 反射读不到内嵌结构体上的 json 标签，
// 展平后入参才和 schema 对得上。
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

// dailyDetailDays 按天趋势在正文里逐日列出的上限（天）。
//
// 超过就不列了：366 天的明细会把上下文吃光，而模型真正要的信息（总单数、峰值日）
// 已经在汇总句里。需要逐日明细的消费者读 Data（结构化数据不受这条限制）。
const dailyDetailDays = 14

// OverviewTools 返回订单模块的概览聚合工具集（趋势 / 热销榜 / 状态计数）。
func OverviewTools(overview ordercontract.OrderOverviewReader) ([]mcp.Tool, error) {
	if overview == nil {
		return nil, errors.New("ordermcp: 订单概览聚合依赖缺失（装配期接线错误）")
	}
	return []mcp.Tool{
		ordersDaily(overview),
		ordersTopProducts(overview),
		ordersStatusCounts(overview),
	}, nil
}

// ordersDailyArgs orders_daily 的入参。
type ordersDailyArgs struct {
	ProjectID string `json:"projectId"`
	From      string `json:"from"`
	To        string `json:"to"`
}

func ordersDaily(overview ordercontract.OrderOverviewReader) mcp.Tool {
	return mcp.New("orders_daily", "订单按天趋势",
		"按站点工程与日期区间统计**每一天**的订单：当天的订单数、计入消费的订单数、净销售额。"+
			"没有订单的那天也会返回一条 0 记录，所以「哪几天没单」「哪天最多」可以直接看结果。"+
			"时间区间为含当天的闭区间，跨度上限 366 天；金额字段为整数分，另有展示文案字段。",
		permission.OrderList,
		mcp.Object("订单按天趋势参数", map[string]mcp.Schema{
			"projectId": mcp.String("站点工程 id（uuid）"),
			"from":      mcp.String("起始日，格式 YYYY-MM-DD，含当天"),
			"to":        mcp.String("结束日，格式 YYYY-MM-DD，含当天；不得早于起始日"),
		}, "projectId", "from", "to"),
		func(ctx context.Context, args ordersDailyArgs) (mcp.Result, error) {
			res, err := overview.DailySeries(ctx, &orderdto.OrderDailySeriesReq{
				ProjectID: args.ProjectID, From: args.From, To: args.To,
			})
			if err != nil {
				return mcp.Result{}, err
			}
			return mcp.Result{Text: dailyText(res), Data: res}, nil
		})
}

// dailyText 把逐日数据写成模型能直接引用的几句（汇总 + 峰值 + 有条件展开的明细）。
func dailyText(res *orderdto.OrderDailySeriesResp) string {
	var orders, paid, net int64
	peakDay, peakOrders := "", int64(-1)
	for _, p := range res.Points {
		orders += p.OrderCount
		paid += p.PaidOrderCount
		net += p.NetSales
		if p.OrderCount > peakOrders {
			peakDay, peakOrders = p.Day, p.OrderCount
		}
	}
	var b strings.Builder
	fmt.Fprintf(&b, "订单按天趋势（%s ~ %s，含当天，共 %d 天）：订单 %d 单，其中计入消费 %d 单；净销售额 %d 分。",
		res.From, res.To, len(res.Points), orders, paid, net)
	if peakDay != "" && peakOrders > 0 {
		fmt.Fprintf(&b, " 峰值日 %s（%d 单）。", peakDay, peakOrders)
	}
	if len(res.Points) <= dailyDetailDays {
		parts := make([]string, 0, len(res.Points))
		for _, p := range res.Points {
			parts = append(parts, fmt.Sprintf("%s %d单/%d分", p.Day, p.OrderCount, p.NetSales))
		}
		fmt.Fprintf(&b, " 逐日：%s。", strings.Join(parts, "；"))
	} else {
		fmt.Fprintf(&b, " 逐日明细见结构化数据（共 %d 天）。", len(res.Points))
	}
	return b.String()
}

// ordersTopProductsArgs orders_top_products 的入参。
type ordersTopProductsArgs struct {
	ProjectID string `json:"projectId"`
	From      string `json:"from"`
	To        string `json:"to"`
	Limit     int    `json:"limit"`
}

func ordersTopProducts(overview ordercontract.OrderOverviewReader) mcp.Tool {
	return mcp.New("orders_top_products", "热销商品榜",
		"按站点工程与日期区间统计卖得最好的商品（按销量降序）。只统计已付款/已发货/已完成的订单，"+
			"已取消与待付款的不上榜。商品名与 SKU 是下单当时的快照；金额为该商品的行实付合计（整数分），"+
			"**不含退款分摊**（订单级退款摊不到商品级）。",
		permission.OrderList,
		// limit 上限与 service 的 MaxTopProductLimit 一致（50）：模型填更大值会被截到上限，
		// 而不是把一次对话变成一次全表排序。
		mcp.Object("热销商品榜参数", map[string]mcp.Schema{
			"projectId": mcp.String("站点工程 id（uuid）"),
			"from":      mcp.String("起始日，格式 YYYY-MM-DD，含当天"),
			"to":        mcp.String("结束日，格式 YYYY-MM-DD，含当天；不得早于起始日"),
			"limit":     mcp.Integer("返回条数，1~50；不传按 5 条"),
		}, "projectId", "from", "to"),
		func(ctx context.Context, args ordersTopProductsArgs) (mcp.Result, error) {
			res, err := overview.TopProducts(ctx, &orderdto.OrderTopProductsReq{
				ProjectID: args.ProjectID, From: args.From, To: args.To, Limit: args.Limit,
			})
			if err != nil {
				return mcp.Result{}, err
			}
			return mcp.Result{Text: topProductsText(res), Data: res}, nil
		})
}

func topProductsText(res *orderdto.OrderTopProductsResp) string {
	var b strings.Builder
	fmt.Fprintf(&b, "热销商品榜（%s ~ %s，含当天，按销量降序，取前 %d 名）：", res.From, res.To, res.Limit)
	if len(res.Items) == 0 {
		b.WriteString("区间内没有任何已付款的订单。")
		return b.String()
	}
	parts := make([]string, 0, len(res.Items))
	for _, it := range res.Items {
		// productId 必须带上：榜单是模型最常接着追问的对象（「第一名那个商品的详情」），
		// 而商品类工具的唯一入口是 id。只给名字时它只能再调一次 product_find 去猜，
		// 名字有重名或含特殊字符时还会猜错。
		parts = append(parts, fmt.Sprintf("%d) %s（SKU %s，productId=%s）%d 件 / %s",
			it.Rank, it.ProductName, it.SKU, it.ProductID, it.Quantity, it.AmountLabel))
	}
	fmt.Fprintf(&b, "%s。", strings.Join(parts, "；"))
	return b.String()
}

// ordersStatusCountsArgs orders_status_counts 的入参（只有工程，没有时间窗）。
type ordersStatusCountsArgs struct {
	ProjectID string `json:"projectId"`
}

// statusLabelText 状态名 → 中文短标签（「已完成」而不是 "completed"）。
//
// 用 order 模块自己的映射而不是在工具里再写一份：那份映射是词条 key 的唯一来源，
// 两处各写一份时，加一个状态只改一边，另一边就会把英文枚举值直接甩给用户。
func statusLabelText(status string) string {
	_, fallback := orderenums.OrderStatusLabel(status)
	return fallback
}

func ordersStatusCounts(overview ordercontract.OrderOverviewReader) mcp.Tool {
	return mcp.New("orders_status_counts", "订单状态计数",
		"统计当前各状态的订单条数（**不带时间区间**：回答「现在有多少单等着处理」），"+
			"并给出两个已解释过的口径：待付款、待发货（已付款未发货）。",
		permission.OrderList,
		mcp.Object("订单状态计数参数", map[string]mcp.Schema{
			"projectId": mcp.String("站点工程 id（uuid）"),
		}, "projectId"),
		func(ctx context.Context, args ordersStatusCountsArgs) (mcp.Result, error) {
			res, err := overview.StatusCounts(ctx, &orderdto.OrderStatusCountsReq{ProjectID: args.ProjectID})
			if err != nil {
				return mcp.Result{}, err
			}
			// 逐状态列全：`Counts` 之前只进了 Data，而 **Data 不会回灌给模型**
			//（ai_session.go 只取 Result.Text）。于是模型只看得见三个数，
			// 用户问「已完成多少单」「这个月取消几单」时它答不出来 —— 而这几个数
			// 明明就在它手上的结构体里。工具名承诺的是「统计当前各状态」。
			lines := make([]string, 0, len(res.Counts))
			for status, n := range res.Counts {
				lines = append(lines, statusLabelText(status)+" "+strconv.FormatInt(n, 10))
			}
			// 状态顺序由地图迭代决定，必须先排序：同一份数据两次调用给出不同顺序，
			// 模型会把「顺序变了」读成「情况变了」。
			sort.Strings(lines)
			head := fmt.Sprintf("当前订单共 %d 笔。待付款 %d 笔、待发货 %d 笔。",
				res.TotalCount, res.PendingCount, res.ShipPendingCount)
			if len(lines) == 0 {
				return mcp.Result{Text: head, Data: res}, nil
			}
			return mcp.Result{
				Text: head + "\n各状态明细：" + strings.Join(lines, "；") + "。",
				Data: res,
			}, nil
		})
}

// QueryTools 返回订单模块的「按线索查订单」工具集。
func QueryTools(query ordercontract.OrderQueryReader) ([]mcp.Tool, error) {
	if query == nil {
		return nil, errors.New("ordermcp: 订单查询依赖缺失（装配期接线错误）")
	}
	return []mcp.Tool{orderFind(query), orderGet(query)}, nil
}

// orderFindArgs order_find 的入参。
type orderFindArgs struct {
	ProjectID string `json:"projectId"`
	Keyword   string `json:"keyword"`
	Status    string `json:"status"`
	From      string `json:"from"`
	To        string `json:"to"`
	Limit     int    `json:"limit"`
}

func orderFind(query ordercontract.OrderQueryReader) mcp.Tool {
	return mcp.New("order_find", "按线索查订单",
		"按线索查订单，用于回答「订单 20261005001 到哪了」「张三那单发了没」「这周有几单没发货」。\n"+
			"keyword 会同时匹配**商户单号 / 客户邮箱 / 客户姓名**三者，所以用户给的任何一条线索都能直接用，不必先猜是哪种。\n"+
			"时间窗（from/to，格式 YYYY-MM-DD，按 UTC 日界、含当天）与 status 都是可选的：不给就不筛那一条。\n"+
			"拿到结果后要细节就用 order_get（按单号取，含商品行与状态流水）。",
		permission.OrderList,
		mcp.Object("按线索查订单参数", map[string]mcp.Schema{
			"projectId": mcp.String("站点工程 id（uuid）"),
			"keyword":   mcp.String("线索：商户单号 / 客户邮箱 / 客户姓名，任一命中即算（可选）"),
			"status": mcp.Enum("只看某个状态（可选；不传则全部状态）",
				"pending", "paid", "shipped", "completed", "cancelled", "refunded"),
			"from":  mcp.String("下单起始日，格式 YYYY-MM-DD，含当天（可选，与 to 成对使用）"),
			"to":    mcp.String("下单结束日，格式 YYYY-MM-DD，含当天（可选，与 from 成对使用）"),
			"limit": mcp.Integer("最多返回几单，默认 10，上限 50"),
		}, "projectId"),
		func(ctx context.Context, args orderFindArgs) (mcp.Result, error) {
			res, err := query.FindOrders(ctx, &orderdto.FindOrderReq{
				ProjectID: args.ProjectID,
				Keyword:   args.Keyword,
				Status:    args.Status,
				From:      args.From,
				To:        args.To,
				Limit:     args.Limit,
			})
			if err != nil {
				return mcp.Result{}, err
			}
			return mcp.Result{Text: findOrdersText(res), Data: res}, nil
		})
}

// findOrdersText 把订单列表写成模型能直接引用的几句。
//
// 每行都给**单号**：它是接着调 order_get 的唯一钥匙，只给客户名与金额时
// 模型只能再猜一次或干脆反问用户（而用户刚给的就是单号）。
func findOrdersText(res *orderdto.FindOrderResp) string {
	if res == nil {
		return "没有查到订单。"
	}
	var b strings.Builder
	scope := describeFindScope(res)
	if len(res.List) == 0 {
		fmt.Fprintf(&b, "没有符合条件的订单（%s）。", scope)
		return b.String()
	}
	fmt.Fprintf(&b, "找到 %d 单（共 %d 单符合条件；%s）：\n", len(res.List), res.Total, scope)
	for i, o := range res.List {
		fmt.Fprintf(&b, "%d. %s · %s · %s · %s · 下单 %s\n",
			i+1, o.OrderNo, orderStatusText(o.Status), customerText(o),
			moneyText(o.Total, o.Currency), o.CreateTime.Time().Format("2006-01-02 15:04"))
	}
	return strings.TrimRight(b.String(), "\n")
}

// describeFindScope 说清这次筛了什么 —— 模型要照这句话回答用户，
// 它必须是**实际生效**的条件而不是用户给的原串。
func describeFindScope(res *orderdto.FindOrderResp) string {
	parts := make([]string, 0, 3)
	if res.From != "" || res.To != "" {
		parts = append(parts, fmt.Sprintf("下单日期 %s ~ %s", dashIfEmpty(res.From), dashIfEmpty(res.To)))
	} else {
		parts = append(parts, "不限下单日期")
	}
	if res.Total > int64(len(res.List)) {
		parts = append(parts, fmt.Sprintf("只列出最近的 %d 单", len(res.List)))
	}
	return strings.Join(parts, "，")
}

// orderGetArgs order_get 的入参。
type orderGetArgs struct {
	ProjectID string `json:"projectId"`
	OrderNo   string `json:"orderNo"`
}

func orderGet(query ordercontract.OrderQueryReader) mcp.Tool {
	return mcp.New("order_get", "订单详情",
		"按**商户单号**取一单的完整信息：金额构成、收货人、商品行（含 SKU 与数量）、"+
			"以及状态流水（谁在什么时候把它推到哪一步）。\n"+
			"回答「这单到哪了」必须用它 —— 只看状态字段答不出「到哪了」，流水才带时间与操作人。\n"+
			"单号来自 order_find 的结果；用户直接给了单号时也可以直接调。",
		permission.OrderList,
		mcp.Object("订单详情参数", map[string]mcp.Schema{
			"projectId": mcp.String("站点工程 id（uuid）"),
			"orderNo":   mcp.String("商户单号（order_find 结果里的那串）"),
		}, "projectId", "orderNo"),
		func(ctx context.Context, args orderGetArgs) (mcp.Result, error) {
			res, err := query.GetOrderDetailByNo(ctx, &orderdto.GetOrderByNoReq{
				ProjectID: args.ProjectID,
				OrderNo:   args.OrderNo,
			})
			if err != nil {
				return mcp.Result{}, err
			}
			return mcp.Result{Text: orderDetailText(res), Data: res}, nil
		})
}

// orderDetailText 把订单详情写成模型能直接引用的几句。
func orderDetailText(res *orderdto.OrderDetailResp) string {
	if res == nil || res.Head == nil {
		return "没有查到这单。"
	}
	o := res.Head
	var b strings.Builder
	fmt.Fprintf(&b, "订单 %s：%s。客户 %s。\n", o.OrderNo, orderStatusText(o.Status), customerText(o))
	fmt.Fprintf(&b, "金额：商品小计 %s，折扣 -%s，运费 %s，税 %s，合计 %s。\n",
		moneyText(o.Subtotal, o.Currency), moneyText(o.DiscountTotal, o.Currency),
		moneyText(o.ShippingTotal, o.Currency), moneyText(o.TaxTotal, o.Currency),
		moneyText(o.Total, o.Currency))
	if addr := shipAddressText(o); addr != "" {
		fmt.Fprintf(&b, "收货：%s %s %s（%s）\n", emptyAsDash(o.ShipName), o.ShipPhone, addr, emptyAsDash(o.ShipZip))
	}
	if o.PaymentMethod != "" {
		fmt.Fprintf(&b, "支付方式：%s。\n", firstNonEmpty(o.PaymentMethodTitle, o.PaymentMethod))
	}
	if o.PaidAt != nil {
		fmt.Fprintf(&b, "付款时间：%s。\n", o.PaidAt.Time().Format("2006-01-02 15:04"))
	}
	if o.CancelReason != "" {
		fmt.Fprintf(&b, "取消原因：%s。\n", o.CancelReason)
	}
	if len(res.Items) == 0 {
		b.WriteString("商品行：没有记录。\n")
	} else {
		fmt.Fprintf(&b, "商品行（%d 项）：\n", len(res.Items))
		for i, it := range res.Items {
			name := it.ProductName
			if it.VariantLabel != "" {
				name += " / " + it.VariantLabel
			}
			fmt.Fprintf(&b, "%d. %s，SKU %s，单价 %s × %d = %s\n",
				i+1, name, emptyAsDash(it.SKU), moneyText(it.UnitPrice, o.Currency), it.Quantity,
				moneyText(it.LineTotal, o.Currency))
		}
	}
	if len(res.Logs) == 0 {
		b.WriteString("状态流水：没有记录（这一单从创建到现在没有发生过状态变更）。")
	} else {
		fmt.Fprintf(&b, "状态流水（%d 条，从早到晚）：\n", len(res.Logs))
		for i, lg := range res.Logs {
			fmt.Fprintf(&b, "%d. %s → %s，%s %s，%s\n",
				i+1, orderStatusText(lg.FromStatus), orderStatusText(lg.ToStatus),
				emptyAsDash(lg.OperatorName), operatorTypeText(lg.OperatorType),
				lg.CreateTime.Time().Format("2006-01-02 15:04"))
		}
	}
	return strings.TrimRight(b.String(), "\n")
}

// —— 文本小工具 ——

// orderStatusText 状态的中文说法；取不到就回原值（原值至少能让模型知道有个状态）。
func orderStatusText(status string) string {
	status = strings.TrimSpace(status)
	if status == "" {
		return "未知状态"
	}
	_, fallback := orderenums.OrderStatusLabel(status)
	if strings.TrimSpace(fallback) == "" {
		return status
	}
	return fallback
}

// operatorTypeText 操作人类型的中文说法（空值不写）。
func operatorTypeText(t string) string {
	switch strings.TrimSpace(t) {
	case "admin":
		return "管理员"
	case "customer", "visitor":
		return "客户"
	case "system":
		return "系统"
	default:
		return strings.TrimSpace(t)
	}
}

// customerText 客户标识：姓名与邮箱哪些有就写哪些，都没有写「游客」。
func customerText(o *orderdto.OrderResp) string {
	name := strings.TrimSpace(o.CustomerName)
	email := strings.TrimSpace(o.CustomerEmail)
	switch {
	case name != "" && email != "":
		return name + "（" + email + "）"
	case name != "":
		return name
	case email != "":
		return email
	default:
		return "游客（未留信息）"
	}
}

// shipAddressText 收货地址拼接（省市区 + 详址），全空回空串。
func shipAddressText(o *orderdto.OrderResp) string {
	parts := make([]string, 0, 4)
	for _, p := range []string{o.ShipProvince, o.ShipCity, o.ShipDistrict, o.ShipAddress} {
		if s := strings.TrimSpace(p); s != "" {
			parts = append(parts, s)
		}
	}
	return strings.Join(parts, "")
}

// moneyText 金额输出：分转元 + 币种。
//
// **币种从订单上取**（o.Currency），不写死人民币：工具不知道站点用哪种货币，
// 凭空写 ¥ 会在别的站点上给出错误答案，而用户无法察觉（数字是对的）。
// 币种为空时只给数字与单位「分」。
func moneyText(cents int64, currency string) string {
	yuan := fmt.Sprintf("%.2f", float64(cents)/100)
	if c := strings.TrimSpace(currency); c != "" {
		return c + " " + yuan
	}
	return strconv.FormatInt(cents, 10) + " 分"
}

func emptyAsDash(s string) string {
	if strings.TrimSpace(s) == "" {
		return "—"
	}
	return s
}

func dashIfEmpty(s string) string {
	if strings.TrimSpace(s) == "" {
		return "—"
	}
	return s
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

type statusWriter interface {
	ChangeStatus(ctx context.Context, req *orderdto.ChangeStatusReq) error
	CancelOrder(ctx context.Context, req *orderdto.CancelOrderReq) (*orderdto.CancelOrderResp, error)
	UpdateOrderNote(ctx context.Context, req *orderdto.UpdateOrderNoteReq) (*orderdto.OrderResp, error)
}

// StatusWriteTools 返回订单状态工具的写工具集（3 个）。
func StatusWriteTools(w statusWriter) ([]mcp.Tool, error) {
	if w == nil {
		return nil, errors.New("ordermcp: 订单状态写依赖缺失（装配期接线错误）")
	}
	return []mcp.Tool{orderStatus(w), orderCancel(w), orderNote(w)}, nil
}

// orderStatusArgs 只留 orderId / toStatus / remark —— 操作人由 ctx 注入。
type orderStatusArgs struct {
	OrderID  uint64 `json:"orderId"`
	ToStatus string `json:"toStatus"`
	Remark   string `json:"remark"`
}

func orderStatus(w statusWriter) mcp.Tool {
	return mcp.NewWrite("order_status", "改订单状态",
		"把订单推到下一个状态。可选目标：`paid` 已付款 / `shipped` 已发货 / "+
			"`completed` 已完成 / `pending` 待付款。\n"+
			"**取消和退款不走这里** —— 取消要归还库存、退款要记流水号，各有专门的入口："+
			"取消用 `order_cancel`，退款在后台订单页操作。传这两个值会被服务端拒绝。\n"+
			"服务端只接受合法的流转（比如没付款的订单不能直接发 `shipped`），"+
			"被拒时回执会说明当前状态能走到哪里 —— 别绕过它硬试。\n"+
			"改之前先 `order_get` 看清当前状态与金额，报给用户确认后再改。",
		permission.OrderStatus,
		mcp.Object("改订单状态参数", map[string]mcp.Schema{
			"orderId":  mcp.Integer("订单 id（用 order_find 拿，不是订单号）"),
			"toStatus": mcp.Enum("目标状态", "pending", "paid", "shipped", "completed"),
			"remark":   mcp.String("流转备注（可选，会写进状态日志）"),
		}, "orderId", "toStatus"),
		nil,
		func(ctx context.Context, args orderStatusArgs) (mcp.Result, error) {
			to := strings.TrimSpace(args.ToStatus)
			req := &orderdto.ChangeStatusReq{
				OrderID:      args.OrderID,
				ToStatus:     to,
				Remark:       strings.TrimSpace(args.Remark),
				OperatorType: "admin",
				OperatorID:   uint64(mcp.UserIDFrom(ctx)),
			}
			if err := w.ChangeStatus(ctx, req); err != nil {
				return mcp.Result{}, err
			}
			_, label := orderenums.OrderStatusLabel(to)
			note := ""
			switch to {
			case "paid":
				note = "付款时间已记下（对账要用）。"
			case "shipped":
				note = "完成时间已清空 —— 发货后订单回到「进行中」。"
			case "completed":
				note = "完成时间已记下（时效统计要用）。"
			}
			return mcp.Result{Text: fmt.Sprintf("订单 %d 已改为「%s」。%s"+
				"流转记录已写进订单状态日志。", args.OrderID, label, note)}, nil
		})
}

type orderCancelArgs struct {
	OrderID uint64 `json:"orderId"`
	Reason  string `json:"reason"`
}

func orderCancel(w statusWriter) mcp.Tool {
	return mcp.NewWrite("order_cancel", "取消订单",
		"取消一笔订单。**它会归还库存**（同一事务里，库存回不来则整笔回滚、订单保持原状）"+
			"并释放已核销的优惠券。\n"+
			"**必须给原因** —— 取消原因会记进状态日志，事后对账要看。\n"+
			"已付款的订单取消前先 `order_get` 确认金额，并把「钱怎么退」跟用户讲清楚："+
			"这个动作只把订单置为已取消，**退款要走后台的退款入口**。\n"+
			"取消不可撤销（要恢复只能重新建单）。",
		permission.OrderCancel,
		mcp.Object("取消订单参数", map[string]mcp.Schema{
			"orderId": mcp.Integer("订单 id（用 order_find 拿，不是订单号）"),
			"reason":  mcp.String("取消原因（必填，写进状态日志）"),
		}, "orderId", "reason"),
		nil,
		func(ctx context.Context, args orderCancelArgs) (mcp.Result, error) {
			req := &orderdto.CancelOrderReq{
				OrderID:      args.OrderID,
				Reason:       strings.TrimSpace(args.Reason),
				OperatorType: "admin",
				OperatorID:   uint64(mcp.UserIDFrom(ctx)),
			}
			if _, err := w.CancelOrder(ctx, req); err != nil {
				return mcp.Result{}, err
			}
			return mcp.Result{Text: fmt.Sprintf(
				"订单 %d 已取消（原因：%s）。占用的库存已归还、优惠券已释放。"+
					"如果这笔单已经收过款，退款要另外去后台订单页做 —— 这一步只改了订单状态。",
				args.OrderID, strings.TrimSpace(args.Reason))}, nil
		})
}

type orderNoteArgs struct {
	OrderID   uint64 `json:"orderId"`
	AdminNote string `json:"adminNote"`
}

func orderNote(w statusWriter) mcp.Tool {
	return mcp.NewWrite("order_note", "写订单后台备注",
		"改订单的后台备注（只有内部可见，客户看不到）。**整段覆盖**，不是追加 —— "+
			"要保留原有的备注，先用 `order_get` 读出当前内容，把新旧拼在一起再写。\n"+
			"备注**不是状态流转**：它不改变订单处在哪一步，所以不写状态日志。\n"+
			"把它当便签用（「客户要求周五前发出」「电话确认过地址」），"+
			"别把状态变更的想法写在这里 —— 那样它不会真的发生，只留下一句失效的话。",
		permission.OrderNote,
		mcp.Object("写订单备注参数", map[string]mcp.Schema{
			"orderId":   mcp.Integer("订单 id（用 order_find 拿，不是订单号）"),
			"adminNote": mcp.String("完整的新备注内容（会整段替换原备注；传空串 = 清空备注）"),
		}, "orderId", "adminNote"),
		nil,
		func(ctx context.Context, args orderNoteArgs) (mcp.Result, error) {
			req := &orderdto.UpdateOrderNoteReq{
				OrderID:      args.OrderID,
				AdminNote:    args.AdminNote,
				OperatorType: "admin",
				OperatorID:   uint64(mcp.UserIDFrom(ctx)),
			}
			res, err := w.UpdateOrderNote(ctx, req)
			if err != nil {
				return mcp.Result{}, err
			}
			if res == nil {
				return mcp.Result{Text: fmt.Sprintf("订单 %d 的备注已更新。", args.OrderID)}, nil
			}
			now := strings.TrimSpace(res.AdminNote)
			if now == "" {
				return mcp.Result{Text: fmt.Sprintf("订单 %s 的备注已清空。", res.OrderNo)}, nil
			}
			return mcp.Result{Text: fmt.Sprintf("订单 %s 的备注已更新，现在是：%s", res.OrderNo, now)}, nil
		})
}

// Tools 返回订单模块的工具集。
//
// 依赖收窄到 OrderRangeSummaryReader（一条只读方法）而不是整个 OrderService：
// 工具层手里握着 ChangeStatus / RefundOrder 时，「AI 顺手改个订单状态」会从
// 「需要显式加一个工具」退化成「随手就能写」—— 能力边界该由接口形状决定，
// 而不是靠下次写代码时记得住。
func Tools(summary ordercontract.OrderRangeSummaryReader) ([]mcp.Tool, error) {
	if summary == nil {
		return nil, errors.New("ordermcp: 订单区间摘要依赖缺失（装配期接线错误）")
	}
	return []mcp.Tool{ordersSummary(summary)}, nil
}

// ordersSummaryArgs orders_summary 的入参，字段名与 schema 一一对应。
//
// 用结构体而不是 map：字段名拼错是编译错误，而 map 是运行期「少取一个字段」——
// 后者表现为一路零值算出来的、看起来正常的数字。
type ordersSummaryArgs struct {
	ProjectID string `json:"projectId"`
	From      string `json:"from"`
	To        string `json:"to"`
}

func ordersSummary(summary ordercontract.OrderRangeSummaryReader) mcp.Tool {
	return mcp.New("orders_summary", "订单区间摘要",
		"按站点工程与日期区间统计订单：区间内创建的订单数、计入消费的订单数、净销售额"+
			"（净销售额 = 计入消费的订单金额 − 已实际收货的退款额；返回值为整数分，另有展示文案字段）。"+
			"时间区间为含当天的闭区间，跨度上限 366 天。",
		// 复用订单列表的权限点：工具的可见性必须与「这个人本来就看不看得到订单」一致，
		// 另造一个「order:ai_read」只会让同一份数据出现第二种授权口径。
		permission.OrderList,
		mcp.Object("订单区间摘要参数", map[string]mcp.Schema{
			"projectId": mcp.String("站点工程 id（uuid）"),
			"from":      mcp.String("起始日，格式 YYYY-MM-DD，含当天"),
			"to":        mcp.String("结束日，格式 YYYY-MM-DD，含当天；不得早于起始日"),
		}, "projectId", "from", "to"),
		func(ctx context.Context, args ordersSummaryArgs) (mcp.Result, error) {
			res, err := summary.SummaryByRange(ctx, &orderdto.OrderRangeSummaryReq{
				ProjectID: args.ProjectID,
				From:      args.From,
				To:        args.To,
			})
			if err != nil {
				return mcp.Result{}, err
			}
			// 正文给模型读，Data 给渲染层用：模型不该去解析自己的表格，
			// 而渲染层也不该去正则匹配一句中文。
			return mcp.Result{
				Text: fmt.Sprintf("订单区间摘要（%s ~ %s，含当天）：订单 %d 单，其中计入消费 %d 单；净销售额 %s（%d 分）。",
					res.From, res.To, res.OrderCount, res.PaidOrderCount, res.NetSalesLabel, res.NetSales),
				Data: res,
			}, nil
		})
}

const (
	// orderCreateMaxItems 与 service 的 maxOrderItems 同量级。
	//
	// 工具层先拦一道不是为了替代 service 的校验：是为了在**调用之前**给出
	// 「分几次做」这种可执行的建议，而 service 的报错只有一句「商品项超限」。
	orderCreateMaxItems = 100
)

// WriteTools 订单模块的写工具集。
func WriteTools(w ordercontract.OrderWriter, store mcp.IdempotencyStore) ([]mcp.Tool, error) {
	if w == nil {
		return nil, errors.New("ordermcp: 订单写工具依赖缺失（装配期接线错误）")
	}
	return []mcp.Tool{orderCreate(w, store)}, nil
}

// orderCreateArgs order_create 的业务入参。
type orderCreateArgs struct {
	ProjectID             string            `json:"projectId"`
	CustomerEmail         string            `json:"customerEmail"`
	CustomerName          string            `json:"customerName"`
	CustomerPhone         string            `json:"customerPhone"`
	Items                 []orderCreateItem `json:"items"`
	Shipping              *orderCreateAddr  `json:"shipping"`
	ShippingTotal         int64             `json:"shippingTotal"`
	CouponCode            string            `json:"couponCode"`
	Remark                string            `json:"remark"`
	AdminNote             string            `json:"adminNote"`
	Locale                string            `json:"locale"`
	ProvisionGuestAccount *bool             `json:"provisionGuestAccount"`
}

type orderCreateItem struct {
	VariantID string `json:"variantId"`
	Quantity  int    `json:"quantity"`
}

type orderCreateAddr struct {
	Name     string `json:"name"`
	Phone    string `json:"phone"`
	Province string `json:"province"`
	City     string `json:"city"`
	District string `json:"district"`
	Address  string `json:"address"`
	Zip      string `json:"zip"`
	Country  string `json:"country"`
}

func orderCreate(w ordercontract.OrderWriter, store mcp.IdempotencyStore) mcp.Tool {
	return mcp.NewWrite("order_create", "代客建单",
		"为一位客户手工建一笔订单（与后台「新建订单」是同一件事，来源记为后台代建）。\n"+
			"**金额不由你决定**：总价由服务端按商品价 + 运费 − 券计算，你给不了商品单价，\n"+
			"DiscountTotal 这类字段也不在参数里 —— 用户要改价请先在商品页改价，或者给一张券。\n"+
			"**会真的扣库存**：任一行库存不足则整单失败、什么都不落。所以建单前先把变体与数量确认清楚。\n"+
			"items 里每行的 variantId 是**变体 id**（不是商品 id）：先用 product_find / product_get\n"+
			"找到商品，再用 stock_find 拿变体 id（库存行上带它）。\n"+
			"provisionGuestAccount 默认 false（不开号）：这个邮箱没有账号时只落单、不发任何邮件；\n"+
			"要开号必须显式传 true，那时系统会建号并把初始密码寄到订单邮箱。\n"+
			"建单是交易凭证的产生动作：请与用户逐项确认客户邮箱、商品、数量、收货地址后再执行。",
		permission.OrderCreate,
		mcp.Object("代客建单参数", map[string]mcp.Schema{
			"projectId":     mcp.String("站点工程 id（uuid，必填）"),
			"customerEmail": mcp.String("客户邮箱（必填）。它同时是订单的联系方式与开号依据"),
			"customerName":  mcp.String("客户姓名（可选）"),
			"customerPhone": mcp.String("客户电话（可选）"),
			"items": mcp.Array("订单商品行（必填，1-100 行）", mcp.Object("一行", map[string]mcp.Schema{
				"variantId": mcp.String("变体 id（必填，不是商品 id）"),
				"quantity":  mcp.Integer("数量（必填，正整数；每项上限 100000）"),
			}, "variantId", "quantity")),
			"shipping": mcp.Object("收货地址（可选，但实物订单应当给）", map[string]mcp.Schema{
				"name":     mcp.String("收件人"),
				"phone":    mcp.String("电话"),
				"province": mcp.String("省"),
				"city":     mcp.String("市"),
				"district": mcp.String("区/县"),
				"address":  mcp.String("详细地址"),
				"zip":      mcp.String("邮编"),
				"country":  mcp.String("国家/地区代码（ISO 3166-1 alpha-2，如 CN）"),
			}),
			"shippingTotal": mcp.Integer("运费（**最小货币单位**，如分。可选，默认 0）。这是唯一一个由你给出的金额字段，理由是运费策略不属于商品域；给之前先问清用户运费多少。"),
			"couponCode":    mcp.String("优惠码（可选）。给了它就按服务端试算的折扣算，不会有第二种算法。"),
			"remark":        mcp.String("订单备注（可选，客户可见）"),
			"adminNote":     mcp.String("后台备注（可选，仅后台可见）"),
			"locale":        mcp.String("客户语言（可选，如 zh-CN）。决定开号时初始密码邮件用哪套模板"),
			"provisionGuestAccount": mcp.Boolean("是否给这个邮箱开号并寄初始密码（可选，默认 false = 不开号）。" +
				"只有用户明确说要给客户开账号时才传 true。"),
		}, "projectId", "customerEmail", "items"),
		store,
		func(ctx context.Context, args orderCreateArgs) (mcp.Result, error) {
			if len(args.Items) == 0 {
				return mcp.Result{}, &mcp.ArgsError{Msg: "items 至少要有一行"}
			}
			if len(args.Items) > orderCreateMaxItems {
				return mcp.Result{}, &mcp.ArgsError{Msg: fmt.Sprintf("一次最多 %d 行商品（收到 %d）；分几笔建单",
					orderCreateMaxItems, len(args.Items))}
			}
			items := make([]orderdto.OrderItemReq, 0, len(args.Items))
			for i, it := range args.Items {
				if strings.TrimSpace(it.VariantID) == "" {
					return mcp.Result{}, &mcp.ArgsError{Msg: fmt.Sprintf("第 %d 行缺 variantId（要变体 id，不是商品 id）", i+1)}
				}
				if it.Quantity <= 0 {
					return mcp.Result{}, &mcp.ArgsError{Msg: fmt.Sprintf("第 %d 行：数量必须是正整数", i+1)}
				}
				items = append(items, orderdto.OrderItemReq{VariantID: it.VariantID, Quantity: it.Quantity})
			}
			if args.ShippingTotal < 0 {
				return mcp.Result{}, &mcp.ArgsError{Msg: "运费不能为负"}
			}
			req := &orderdto.CreateOrderReq{
				ProjectID:     args.ProjectID,
				CustomerEmail: strings.TrimSpace(args.CustomerEmail),
				CustomerName:  args.CustomerName,
				CustomerPhone: args.CustomerPhone,
				Items:         items,
				ShippingTotal: args.ShippingTotal,
				CouponCode:    args.CouponCode,
				Remark:        args.Remark,
				AdminNote:     args.AdminNote,
				Locale:        args.Locale,
			}
			if args.Shipping != nil {
				req.Shipping = orderdto.OrderAddress{
					Name:     args.Shipping.Name,
					Phone:    args.Shipping.Phone,
					Province: args.Shipping.Province,
					City:     args.Shipping.City,
					District: args.Shipping.District,
					Address:  args.Shipping.Address,
					Zip:      args.Shipping.Zip,
					Country:  args.Shipping.Country,
				}
			}
			// 开号是**显式**动作：三态里只接受 true / false，不让 nil 漏进 service。
			// nil 的语义是「调用方没表态，保持既有前台行为（下单即开户）」——
			// 那是前台 checkout 链路的语义，工具这里是后台代客建单，
			// 沿用 nil 会让每次代客建单都顺手替客户开一个账号。
			provision := false
			if args.ProvisionGuestAccount != nil {
				provision = *args.ProvisionGuestAccount
			}
			req.ProvisionGuestAccount = &provision

			res, err := w.CreateAdminOrder(ctx, req)
			if err != nil {
				return mcp.Result{}, err
			}
			return mcp.Result{Text: orderCreateText(res), Data: res}, nil
		},
	)
}

// orderCreateText 把建单结果写成模型能直接引用的几句。
//
// 必须给**订单号**：用户拿到它才能去后台找到这一单（内部 id 在界面上不出现）。
// Duplicated 为真时要说清「这单不是新落的」—— 否则用户会以为重复下单了，
// 而实际情况多半是他自己点了两次、系统正确地去重了。
func orderCreateText(res *orderdto.CreateOrderResp) string {
	if res == nil {
		return "建单请求已提交，但没有返回订单信息 —— 请调 order_find 按邮箱或订单号确认。"
	}
	var b strings.Builder
	if res.Duplicated {
		fmt.Fprintf(&b, "这笔单**已经存在**（命中了同一次请求的幂等键），没有重复创建：\n")
	} else {
		fmt.Fprintf(&b, "已建单：\n")
	}
	fmt.Fprintf(&b, "- 订单号 %s（内部 id %d）\n", res.OrderNo, res.ID)
	fmt.Fprintf(&b, "- 状态 %s，金额 %s\n", res.Status, moneyText(res.Total, res.Currency))
	if res.AccountMailed {
		// 开号成功必须说：不提示的话，客户不知道自己已经有了账号，
		// 下次回来还会去走一遍注册。
		b.WriteString("- 已为该邮箱新建客户账号，并把初始密码寄到了订单邮箱（请告诉用户去收信，包括垃圾箱）\n")
	}
	if !res.Duplicated {
		b.WriteString("库存已按订单行扣减。要查发货状态或推进流程，去订单页做 —— 本工具只能建单。")
	}
	return strings.TrimRight(b.String(), "\n")
}

const (
	returnDefaultLimit = 10
	returnMaxLimit     = 50
)

// ReturnListArgs 退货列表筛选。
type ReturnListArgs struct {
	ProjectID string `json:"projectId"`
	Keyword   string `json:"keyword,omitempty"`
	Status    string `json:"status,omitempty"`
	OrderID   uint64 `json:"orderId,omitempty"`
	PageSize  int    `json:"pageSize,omitempty"`
	Offset    int    `json:"offset,omitempty"`
}

// ReturnGetArgs 看单条退货申请。
type ReturnGetArgs struct {
	ReturnID uint64 `json:"returnId"`
}

// ReturnReviewArgs 审阅（同意 / 驳回）。
type ReturnReviewArgs struct {
	ReturnID uint64 `json:"returnId"`
	Remark   string `json:"remark,omitempty"`
	// AutoReceive 同意后立刻完成「入库 + 退款」。见工具描述：这是一步到底，不是加速确认。
	AutoReceive   bool   `json:"autoReceive,omitempty"`
	WarehouseID   string `json:"warehouseId,omitempty"`
	TransactionID string `json:"transactionId,omitempty"`
}

// ReturnReceiveArgs 确认收货（入库 + 退款）。
type ReturnReceiveArgs struct {
	ReturnID      uint64 `json:"returnId"`
	WarehouseID   string `json:"warehouseId,omitempty"`
	TransactionID string `json:"transactionId,omitempty"`
	Remark        string `json:"remark,omitempty"`
}

// ReturnReader / ReturnWriter 是本模块对退货的窄接口。
//
// 刻意不含 ReturnableOfOrder（那是前台「这单还能不能退」的判断）与
// CancelReturn（撤销是**客户**的动作，后台代客户撤销要另想清楚身份问题）。
// 也不含 RefundReturn 之类的内部步骤：入库与退款由 receive 一次性编排，
// 单独暴露它们等于允许「只退款不收货」这种账实不符的状态。
type ReturnReader interface {
	ListReturns(ctx context.Context, req *orderdto.ReturnListReq) (*orderdto.ReturnListResp, error)
	GetReturn(ctx context.Context, returnID uint64) (*orderdto.ReturnDetailResp, error)
}

type ReturnWriter interface {
	ApproveReturn(ctx context.Context, req *orderdto.ReturnReviewReq) (*orderdto.ReturnResp, error)
	RejectReturn(ctx context.Context, req *orderdto.ReturnReviewReq) (*orderdto.ReturnResp, error)
	ReceiveReturn(ctx context.Context, req *orderdto.ReturnReceiveReq) (*orderdto.ReturnResp, error)
}

// ReturnTools 装配退货工具。
func ReturnTools(r ReturnReader, w ReturnWriter, store mcp.IdempotencyStore) ([]mcp.Tool, error) {
	if r == nil {
		return nil, fmt.Errorf("退货读取器不能为空")
	}
	if w == nil {
		return nil, fmt.Errorf("退货写入器不能为空")
	}
	if store == nil {
		return nil, fmt.Errorf("幂等存储不能为空")
	}

	list := mcp.New("return_list", "查退货申请",
		"列出退货 / 退款申请。默认按提交时间倒序。\n"+
			"**`status=pending` 是「等我们处理」的那一批** —— 客户已经提交，钱和货都还没动。\n"+
			"看单条详情用 return_get（列表不带商品明细与退款额拆解）。",
		permission.OrderReturnList,
		mcp.Object("查退货申请", map[string]mcp.Schema{
			"projectId": mcp.String("工程 id"),
			"status": mcp.Enum("按状态筛：pending 待处理 / approved 已同意（待收货）/ rejected 已驳回 / received 已收货 / refunded 已退款 / cancelled 已撤销；省略=全部",
				"pending", "approved", "rejected", "received", "refunded", "cancelled"),
			"keyword":  mcp.String("按退货单号 / 订单号 / 客户模糊匹配，可省略"),
			"orderId":  mcp.Integer("只看某一单的退货申请，可省略"),
			"pageSize": mcp.Integer(fmt.Sprintf("最多返回多少条（默认 %d，上限 %d）", returnDefaultLimit, returnMaxLimit)),
			"offset":   mcp.Integer("跳过多少条（翻页用，默认 0）"),
		}, "projectId"),
		func(ctx context.Context, args ReturnListArgs) (mcp.Result, error) {
			projectID := strings.TrimSpace(args.ProjectID)
			if projectID == "" {
				return mcp.Result{}, &mcp.ArgsError{Msg: "projectId 不能为空"}
			}
			res, err := r.ListReturns(ctx, &orderdto.ReturnListReq{
				ProjectID: projectID,
				Keyword:   strings.TrimSpace(args.Keyword),
				Status:    strings.TrimSpace(args.Status),
				OrderID:   args.OrderID,
				Limit:     clampReturnLimit(args.PageSize),
				Offset:    clampOffset(args.Offset),
			})
			if err != nil {
				return mcp.Result{}, err
			}
			return mcp.Result{Text: returnListText(res)}, nil
		})

	get := mcp.New("return_get", "看一条退货申请的详情",
		"取回退货申请的全部信息：客户、订单、申请理由、**逐条商品的退款额**、"+
			"以及审阅与收货的时间线。\n"+
			"同意或驳回之前先跑这一步 —— 退款额是按明细算出来的，不是整单金额。",
		permission.OrderReturnGet,
		mcp.Object("取退货详情", map[string]mcp.Schema{
			"returnId": mcp.Integer("退货申请 id（从 return_list 拿）"),
		}, "returnId"),
		func(ctx context.Context, args ReturnGetArgs) (mcp.Result, error) {
			if args.ReturnID == 0 {
				return mcp.Result{}, &mcp.ArgsError{Msg: "returnId 不能为空"}
			}
			res, err := r.GetReturn(ctx, args.ReturnID)
			if err != nil {
				return mcp.Result{}, err
			}
			return mcp.Result{Text: returnDetailText(res)}, nil
		})

	approve := mcp.NewWrite("return_approve", "同意退货",
		"同意客户的退货申请。**这一步会不会动钱和货，取决于 autoReceive**：\n"+
			"* `autoReceive=false`（默认）：只把状态改成「已同意」，等仓库确认收到货再走 return_receive。"+
			"货还没到手时用这个。\n"+
			"* `autoReceive=true`：**当场完成「入库 + 退款」一步到底** —— "+
			"库存加回去、钱退给客户。只在前台已经把货拿回来了、或者你确实要立刻退款的场景才用。\n"+
			"退款额是按申请明细逐条算的，不是整单金额；同意之前用 return_get 看清数字。"+
			"同意之后状态就是「已同意」，不能再改回待处理。",
		permission.OrderReturnApprove,
		mcp.Object("同意退货", map[string]mcp.Schema{
			"returnId":    mcp.Integer("退货申请 id"),
			"autoReceive": mcp.Boolean("true = 立刻完成入库与退款（一步到底）；false 或不填 = 只同意，等收货再走 return_receive"),
			"warehouseId": mcp.String("入库仓库 id，省略 = 该工程默认仓"),
			"remark":      mcp.String("给内部看的审阅备注，可省略"),
		}, "returnId"),
		store,
		func(ctx context.Context, args ReturnReviewArgs) (mcp.Result, error) {
			req := &orderdto.ReturnReviewReq{
				ReturnID:    args.ReturnID,
				Remark:      strings.TrimSpace(args.Remark),
				AutoReceive: args.AutoReceive,
				WarehouseID: strings.TrimSpace(args.WarehouseID),
			}
			fillReturnOperator(ctx, req)
			res, err := w.ApproveReturn(ctx, req)
			if err != nil {
				return mcp.Result{}, err
			}
			tail := "已停在「已同意」—— 等仓库点确认收货后再跑 return_receive，那时才入库与退款。"
			if args.AutoReceive {
				tail = "**入库与退款已经执行完毕**（库存已加回、钱已退给客户）。"
			}
			return mcp.Result{Text: "退货申请已同意：\n" + returnRespText(res) + "\n" + tail}, nil
		})

	reject := mcp.NewWrite("return_reject", "驳回退货申请",
		"驳回客户的退货申请。**这是终态**，客户看到的是「被拒绝」，之后要走也是重新提交一张新的申请。\n"+
			"`remark` 是**给客户看的理由**，不是内部备注 —— 空着等于让客户收到一个没有解释的拒绝。"+
			"驳回之前先 return_get 看清理由与明细，并把理由跟用户核对一遍。\n"+
			"驳回不动库存也不退款。",
		permission.OrderReturnReject,
		mcp.Object("驳回退货", map[string]mcp.Schema{
			"returnId": mcp.Integer("退货申请 id"),
			"remark":   mcp.String("驳回理由（会展示给客户，**必填**）"),
		}, "returnId", "remark"),
		store,
		func(ctx context.Context, args ReturnReviewArgs) (mcp.Result, error) {
			remark := strings.TrimSpace(args.Remark)
			if remark == "" {
				return mcp.Result{}, &mcp.ArgsError{Msg: "驳回必须给出理由（remark），客户会看到它"}
			}
			req := &orderdto.ReturnReviewReq{ReturnID: args.ReturnID, Remark: remark}
			fillReturnOperator(ctx, req)
			res, err := w.RejectReturn(ctx, req)
			if err != nil {
				return mcp.Result{}, err
			}
			return mcp.Result{Text: "退货申请已驳回：\n" + returnRespText(res) +
				"\n客户会看到理由：" + remark}, nil
		})

	receive := mcp.NewWrite("return_receive", "确认收货并退货",
		"确认已经收到退回来的货，并**当场执行两件事：库存加回、退款给客户**。"+
			"两步各自幂等，重复调用不会重复退款。\n"+
			"只在货真的在手上时用。申请得先是「已同意」状态（没同意过先走 return_approve）。\n"+
			"退款额按申请明细逐条算，不是整单金额 —— 部分退货只退那几件的钱。",
		permission.OrderReturnReceive,
		mcp.Object("确认收货", map[string]mcp.Schema{
			"returnId":      mcp.Integer("退货申请 id"),
			"warehouseId":   mcp.String("入库仓库 id，省略 = 该工程默认仓"),
			"transactionId": mcp.String("退款流水号，可省略（省略时由服务端生成）"),
			"remark":        mcp.String("备注，可省略"),
		}, "returnId"),
		store,
		func(ctx context.Context, args ReturnReceiveArgs) (mcp.Result, error) {
			req := &orderdto.ReturnReceiveReq{
				ReturnID:      args.ReturnID,
				WarehouseID:   strings.TrimSpace(args.WarehouseID),
				TransactionID: strings.TrimSpace(args.TransactionID),
				Remark:        strings.TrimSpace(args.Remark),
			}
			fillReturnReceiveOperator(ctx, req)
			res, err := w.ReceiveReturn(ctx, req)
			if err != nil {
				return mcp.Result{}, err
			}
			return mcp.Result{Text: "已确认收货：\n" + returnRespText(res) +
				"\n**库存已加回、钱已退给客户。**"}, nil
		})

	return []mcp.Tool{list, get, approve, reject, receive}, nil
}

// fillReturnOperator 把操作人从 ctx 注入。
//
// 不从参数读：审阅人是审计材料，模型会照用户口述的名字填，而那个名字未必是登录身份。
func fillReturnOperator(ctx context.Context, req *orderdto.ReturnReviewReq) {
	if uid := mcp.UserIDFrom(ctx); uid > 0 {
		req.OperatorType = "admin"
		req.OperatorID = uint64(uid)
	}
}

func fillReturnReceiveOperator(ctx context.Context, req *orderdto.ReturnReceiveReq) {
	if uid := mcp.UserIDFrom(ctx); uid > 0 {
		req.OperatorType = "admin"
		req.OperatorID = uint64(uid)
	}
}

func clampReturnLimit(v int) int {
	if v <= 0 {
		return returnDefaultLimit
	}
	if v > returnMaxLimit {
		return returnMaxLimit
	}
	return v
}

// returnRespText 写操作回执用的简版：只有退货单本身，没有订单快照与明细。
func returnRespText(r *orderdto.ReturnResp) string {
	if r == nil {
		return "（服务端没有回传退货单）"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "* 退货单号 %s（id=%d），对应订单 %s\n",
		emptyAsDash(r.ReturnNo), r.ID, emptyAsDash(r.OrderNo))
	fmt.Fprintf(&b, "* 客户：%s %s\n", emptyAsDash(r.CustomerName), emptyAsDash(r.CustomerEmail))
	fmt.Fprintf(&b, "* 状态：%s\n", returnStatusText(r.Status, r.StatusLabel))
	fmt.Fprintf(&b, "* 退款额：%s", firstNonEmpty(r.RefundLabel, centsLabel(r.RefundAmount)))
	return b.String()
}

func returnListText(res *orderdto.ReturnListResp) string {
	if res == nil || len(res.List) == 0 {
		return "没有符合条件的退货申请。"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "共 %d 条，本页 %d 条：\n", res.Total, len(res.List))
	for i, r := range res.List {
		fmt.Fprintf(&b, "%d. id=%d %s · 订单 %s · 客户 %s · 退款 %s · %s · 提交 %s\n",
			i+1, r.ID, emptyAsDash(r.ReturnNo), emptyAsDash(r.OrderNo),
			firstNonEmpty(r.CustomerEmail, r.CustomerName, "-"),
			firstNonEmpty(r.RefundLabel, centsLabel(r.RefundAmount)),
			returnStatusText(r.Status, r.StatusLabel), r.CreateTime.String())
	}
	return b.String()
}

// returnDetailText 详情视图：ReturnDetailResp 是 {Return *ReturnResp, Order *OrderResp} 的组合，
// 明细挂在 Return.Items 上（不是外层）。
func returnDetailText(d *orderdto.ReturnDetailResp) string {
	if d == nil || d.Return == nil {
		return "没有这条退货申请。"
	}
	r := d.Return
	var b strings.Builder
	b.WriteString(returnRespText(r))
	b.WriteString("\n")
	fmt.Fprintf(&b, "* 申请理由：%s\n", emptyAsDash(r.Reason))
	if len(r.Items) > 0 {
		b.WriteString("* 退货明细（退款额是按这些逐条算的，不是整单金额）：\n")
		for _, it := range r.Items {
			fmt.Fprintf(&b, "  - %s %s ×%d，退 %s\n",
				emptyAsDash(it.ProductName), emptyAsDash(it.VariantLabel), it.Quantity,
				firstNonEmpty(it.RefundLabel, centsLabel(it.RefundAmount)))
		}
	}
	if d.Order != nil {
		fmt.Fprintf(&b, "* 订单：%s，状态 %s，金额 %s\n",
			emptyAsDash(d.Order.OrderNo), orderStatusText(d.Order.Status),
			moneyText(d.Order.Total, d.Order.Currency))
	}
	if strings.TrimSpace(r.AdminNote) != "" {
		fmt.Fprintf(&b, "* 审阅备注：%s\n", r.AdminNote)
	}
	if r.ReviewedAt != nil {
		fmt.Fprintf(&b, "* 审阅于 %s（%s）\n", r.ReviewedAt.String(), emptyAsDash(r.ReviewerName))
	}
	if r.ReceivedAt != nil {
		fmt.Fprintf(&b, "* 收货于 %s\n", r.ReceivedAt.String())
	}
	if r.RefundedAt != nil {
		fmt.Fprintf(&b, "* 退款于 %s（流水 %s）", r.RefundedAt.String(), emptyAsDash(r.TransactionID))
	}
	return b.String()
}

// returnStatusText 状态的中文说法。
//
// 优先用 service 给的口径标签；它缺席时兜底 —— 但**不能只回裸状态码**：
// 「已同意」与「已收货」在下游是两件不同的事（一个还没退钱，一个钱已退）。
func returnStatusText(status, label string) string {
	if s := strings.TrimSpace(label); s != "" {
		return s
	}
	switch strings.TrimSpace(status) {
	case "pending":
		return "待处理"
	case "approved":
		return "已同意（待收货）"
	case "rejected":
		return "已驳回"
	case "received":
		return "已收货（待退款）"
	case "refunded":
		return "已退款"
	case "cancelled":
		return "已撤销"
	}
	return emptyAsDash(status)
}
