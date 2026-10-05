package ordermcp

// order_write_tools.go — 后台代客建单（写）。
//
// 只开这一条写路径，理由见 contract/order_write.go：建单与后台手工建单页是同一件事，
// 而发货 / 退款 / 改单是对既有订单的不可逆或涉资金动作，留在人手里。
//
// 与库存的 stock_change 相比，这个工具多一层「金额不由调用方决定」的约束：
// dto 里 Subtotal / Total 是服务端算的，DiscountTotal 在无券时被忽略，
// 有券时以服务端试算为准。工具层因此**给不出价格** —— 用户说「按 99 元卖给他」
// 时，正确做法是先用 product_update 改价（或直接告诉用户改价在商品页），
// 而不是在这里塞一个金额。这一点写进了工具说明，否则模型会试着填 DiscountTotal
// 去凑数 —— 那在有券时被忽略、无券时也被忽略，用户看到的却是「已经按 99 元下单了」。

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"go_wp/internal/mcp"
	ordercontract "go_wp/internal/module/order/contract"
	orderdto "go_wp/internal/module/order/dto"
	"go_wp/internal/permission"
)

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
