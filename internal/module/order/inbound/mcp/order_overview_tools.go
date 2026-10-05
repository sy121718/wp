package ordermcp

// order_overview_tools.go — 概览页三块只读聚合的工具（趋势 / 热销榜 / 状态计数）。
//
// 与 order_tools.go 分开成两个文件与两个装配函数：它们的依赖接口不同
//（OrderRangeSummaryReader vs OrderOverviewReader），而工具层的依赖必须收窄到
// 「它真正需要的那几条只读方法」—— 合成一个函数会让只想给趋势的模块也被迫拿到榜单。

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
	"go_wp/internal/permission"
)

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
		b.WriteString(" 逐日：" + strings.Join(parts, "；") + "。")
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
	b.WriteString(strings.Join(parts, "；") + "。")
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
			//（ai_session_chat.go 只取 Result.Text）。于是模型只看得见三个数，
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
