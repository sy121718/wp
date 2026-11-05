package ordermcp

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"go_wp/internal/mcp"
	ordercontract "go_wp/internal/module/order/contract"
	orderdto "go_wp/internal/module/order/dto"
	orderenums "go_wp/internal/module/order/enums"
	"go_wp/pkg/utils"
)

const (
	couponDefaultLimit = 10
	couponMaxLimit     = 50
)

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

type CouponUpdateReq struct {
	CouponID uint64 `json:"couponId"`
	CouponWriteReq
}

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

type CouponDeleteArgs struct {
	CouponID uint64 `json:"couponId"`
}

type CouponReader interface {
	GetCoupon(ctx context.Context, couponID uint64) (*orderdto.CouponResp, error)
	ListCoupons(ctx context.Context, req *orderdto.CouponListReq) (*orderdto.CouponListResp, error)
}

type CouponWriter interface {
	CreateCoupon(ctx context.Context, req *orderdto.CouponSaveReq) (*orderdto.CouponResp, error)
	UpdateCoupon(ctx context.Context, req *orderdto.CouponSaveReq) (*orderdto.CouponResp, error)
	DeleteCoupon(ctx context.Context, couponID uint64) error
}

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
		orderenums.OrderCouponList,
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

			limit, offset := utils.NormalizeLimitOffset(args.PageSize, args.Offset,
				couponDefaultLimit, couponMaxLimit)
			req := &orderdto.CouponListReq{
				ProjectID: projectID,
				Keyword:   strings.TrimSpace(args.Keyword),
				Status:    strings.TrimSpace(args.Status),
				Limit:     limit,
				Offset:    offset,
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
		orderenums.OrderCouponGet,
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
		orderenums.OrderCouponCreate,
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
		orderenums.OrderCouponUpdate,
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
		orderenums.OrderCouponDelete,
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

func couponValueText(c *orderdto.CouponResp) string {
	if c.DiscountType == "percent" {
		return fmt.Sprintf("减 %d%%", c.DiscountValue)
	}
	return fmt.Sprintf("减 %s", centsLabel(c.DiscountValue))
}

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

const dailyDetailDays = 14

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
		orderenums.OrderList,
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
		orderenums.OrderList,

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

		parts = append(parts, fmt.Sprintf("%d) %s（SKU %s，productId=%s）%d 件 / %s",
			it.Rank, it.ProductName, it.SKU, it.ProductID, it.Quantity, it.AmountLabel))
	}
	fmt.Fprintf(&b, "%s。", strings.Join(parts, "；"))
	return b.String()
}

type ordersStatusCountsArgs struct {
	ProjectID string `json:"projectId"`
}

func statusLabelText(status string) string {
	_, fallback := orderenums.OrderStatusLabel(status)
	return fallback
}

func ordersStatusCounts(overview ordercontract.OrderOverviewReader) mcp.Tool {
	return mcp.New("orders_status_counts", "订单状态计数",
		"统计当前各状态的订单条数（**不带时间区间**：回答「现在有多少单等着处理」），"+
			"并给出两个已解释过的口径：待付款、待发货（已付款未发货）。",
		orderenums.OrderList,
		mcp.Object("订单状态计数参数", map[string]mcp.Schema{
			"projectId": mcp.String("站点工程 id（uuid）"),
		}, "projectId"),
		func(ctx context.Context, args ordersStatusCountsArgs) (mcp.Result, error) {
			res, err := overview.StatusCounts(ctx, &orderdto.OrderStatusCountsReq{ProjectID: args.ProjectID})
			if err != nil {
				return mcp.Result{}, err
			}

			lines := make([]string, 0, len(res.Counts))
			for status, n := range res.Counts {
				lines = append(lines, statusLabelText(status)+" "+strconv.FormatInt(n, 10))
			}

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

func QueryTools(query ordercontract.OrderQueryReader) ([]mcp.Tool, error) {
	if query == nil {
		return nil, errors.New("ordermcp: 订单查询依赖缺失（装配期接线错误）")
	}
	return []mcp.Tool{orderFind(query), orderGet(query)}, nil
}

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
		orderenums.OrderList,
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
		orderenums.OrderList,
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

func shipAddressText(o *orderdto.OrderResp) string {
	parts := make([]string, 0, 4)
	for _, p := range []string{o.ShipProvince, o.ShipCity, o.ShipDistrict, o.ShipAddress} {
		if s := strings.TrimSpace(p); s != "" {
			parts = append(parts, s)
		}
	}
	return strings.Join(parts, "")
}

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

func StatusWriteTools(w statusWriter) ([]mcp.Tool, error) {
	if w == nil {
		return nil, errors.New("ordermcp: 订单状态写依赖缺失（装配期接线错误）")
	}
	return []mcp.Tool{orderStatus(w), orderCancel(w), orderNote(w)}, nil
}

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
		orderenums.OrderStatus,
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
		orderenums.OrderCancel,
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
		orderenums.OrderNote,
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

func Tools(summary ordercontract.OrderRangeSummaryReader) ([]mcp.Tool, error) {
	if summary == nil {
		return nil, errors.New("ordermcp: 订单区间摘要依赖缺失（装配期接线错误）")
	}
	return []mcp.Tool{ordersSummary(summary)}, nil
}

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

		orderenums.OrderList,
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

			return mcp.Result{
				Text: fmt.Sprintf("订单区间摘要（%s ~ %s，含当天）：订单 %d 单，其中计入消费 %d 单；净销售额 %s（%d 分）。",
					res.From, res.To, res.OrderCount, res.PaidOrderCount, res.NetSalesLabel, res.NetSales),
				Data: res,
			}, nil
		})
}

const (
	orderCreateMaxItems = 100
)

func WriteTools(w ordercontract.OrderWriter, store mcp.IdempotencyStore) ([]mcp.Tool, error) {
	if w == nil {
		return nil, errors.New("ordermcp: 订单写工具依赖缺失（装配期接线错误）")
	}
	return []mcp.Tool{orderCreate(w, store)}, nil
}

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
		orderenums.OrderCreate,
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

type ReturnListArgs struct {
	ProjectID string `json:"projectId"`
	Keyword   string `json:"keyword,omitempty"`
	Status    string `json:"status,omitempty"`
	OrderID   uint64 `json:"orderId,omitempty"`
	PageSize  int    `json:"pageSize,omitempty"`
	Offset    int    `json:"offset,omitempty"`
}

type ReturnGetArgs struct {
	ReturnID uint64 `json:"returnId"`
}

type ReturnReviewArgs struct {
	ReturnID uint64 `json:"returnId"`
	Remark   string `json:"remark,omitempty"`

	AutoReceive   bool   `json:"autoReceive,omitempty"`
	WarehouseID   string `json:"warehouseId,omitempty"`
	TransactionID string `json:"transactionId,omitempty"`
}

type ReturnReceiveArgs struct {
	ReturnID      uint64 `json:"returnId"`
	WarehouseID   string `json:"warehouseId,omitempty"`
	TransactionID string `json:"transactionId,omitempty"`
	Remark        string `json:"remark,omitempty"`
}

type ReturnReader interface {
	ListReturns(ctx context.Context, req *orderdto.ReturnListReq) (*orderdto.ReturnListResp, error)
	GetReturn(ctx context.Context, returnID uint64) (*orderdto.ReturnDetailResp, error)
}

type ReturnWriter interface {
	ApproveReturn(ctx context.Context, req *orderdto.ReturnReviewReq) (*orderdto.ReturnResp, error)
	RejectReturn(ctx context.Context, req *orderdto.ReturnReviewReq) (*orderdto.ReturnResp, error)
	ReceiveReturn(ctx context.Context, req *orderdto.ReturnReceiveReq) (*orderdto.ReturnResp, error)
}

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
		orderenums.OrderReturnList,
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
			limit, offset := utils.NormalizeLimitOffset(args.PageSize, args.Offset,
				returnDefaultLimit, returnMaxLimit)
			res, err := r.ListReturns(ctx, &orderdto.ReturnListReq{
				ProjectID: projectID,
				Keyword:   strings.TrimSpace(args.Keyword),
				Status:    strings.TrimSpace(args.Status),
				OrderID:   args.OrderID,
				Limit:     limit,
				Offset:    offset,
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
		orderenums.OrderReturnGet,
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
		orderenums.OrderReturnApprove,
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
		orderenums.OrderReturnReject,
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
		orderenums.OrderReturnReceive,
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
