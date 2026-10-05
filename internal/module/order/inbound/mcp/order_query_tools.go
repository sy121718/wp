package ordermcp

// order_query_tools.go — 「按线索找订单」的两个只读工具。
//
// 为什么补这一对：模块此前只有四个聚合（orders_summary / daily / top_products /
// status_counts），它们回答的都是「一共/每天/谁最好/各状态多少」；而用户最常问的
// 「订单 20261005001 到哪了」「张三那单发了没」——**一个线索指向一单**——没有任何入口。
// 底层一直是齐的（model 的 List 支持单号 / 客户名 / 邮箱关键词与时间窗，service 的
// GetOrderDetailByNo 连商品行与状态流水都取好了），缺的只是工具层的门。
//
// 两个工具都是只读：依赖收窄到 OrderQueryReader（两个方法），手里没有 ChangeStatus ——
// 「AI 顺手把订单改成已发货」不会在某次改动里悄悄变得可能。

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"go_wp/internal/mcp"
	ordercontract "go_wp/internal/module/order/contract"
	orderdto "go_wp/internal/module/order/dto"
	orderenums "go_wp/internal/module/order/enums"
	"go_wp/internal/permission"
)

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
