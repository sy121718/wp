package runtimefragment

// cart.go — 购物车与访客结算片段（访问面写路径）。
//
// 为什么购物车必须走片段：站点是**已编译的静态产物**，同一份 HTML 发给所有访客，
// 而「车里有什么」每个人都不同 —— 静态页面给不了这个，只有访问面的片段端点能给。
//
// 为什么匿名可用：访客在登录之前就要能加购。放进会话等于「没账号不能逛」，
// 把加购变成了一道注册门槛。购物车状态因此放在**客户端签名 cookie** 里
// （见 cart 模块的 cart_cookie.go），服务端不持久化任何购物车状态。
//
// 这类「有副作用 + anonymous」的写能力凭什么不是 CSRF 缺口（判定条件见 endpoint.go）：
//   · 状态是客户端签名 cookie —— 攻击者既无法预置受害者浏览器里的购物车
//     （跨域写不了别家的 cookie），也无法用表单字段伪造一辆车
//     （结算只读 cookie 里的车，不认表单里的商品）；
//   · SameSite=Lax 让跨站 POST **不携带** cookie，攻击者构造的请求拿到的是一辆空车；
//   · 金额与库存都在服务端现算现扣，客户端能影响的只有「买哪个变体、几件」——
//     那本来就是访客该有的自由。
//
// 六个能力：cartSummary（角标计数）/ cartView（购物车）/ cartAdd / cartSetQty /
// cartClear / checkout（结算并支付）。除 checkout 外都只动 cookie，不动数据库。

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"

	cartcontract "go_wp/internal/module/cart/contract"
	cartdto "go_wp/internal/module/cart/dto"
	cartenums "go_wp/internal/module/cart/enums"
	orderdto "go_wp/internal/module/order/dto"
	orderenums "go_wp/internal/module/order/enums"
	"go_wp/internal/templates"
)

// cartService 购物车依赖（装配期注入；nil = 未接入，能力给出明确提示而不是空壳）。
var cartService cartcontract.CartService

// SetCartProvider 注入购物车能力（装配期调用；传 nil 表示未接入）。
func SetCartProvider(svc cartcontract.CartService) { cartService = svc }

func init() {
	Register(Spec{Type: "cartSummary", Method: "GET", Auth: AuthAnonymous, Render: renderCartSummary})
	Register(Spec{Type: "cartView", Method: "GET", Auth: AuthAnonymous, Render: renderCartView})
	Register(Spec{Type: "cartAdd", Method: "POST", Auth: AuthAnonymous, Render: renderCartAdd})
	Register(Spec{Type: "cartSetQty", Method: "POST", Auth: AuthAnonymous, Render: renderCartSetQty})
	Register(Spec{Type: "cartClear", Method: "POST", Auth: AuthAnonymous, Render: renderCartClear})
	Register(Spec{Type: "checkout", Method: "POST", Auth: AuthAnonymous, Render: renderCheckout})
}

// cartFragmentData 购物车片段的模板数据。
type cartFragmentData struct {
	// FragmentType 自身能力名（模板写成 data-fragment 属性，与 loginPanel / cartSummary 同口径）。
	FragmentType string
	ProjectID    string
	Items        []*cartdto.CartItem
	LineCount    int
	ItemCount    int
	TotalLabel   string
	Empty        bool
}

// checkoutFragmentData 结算结果片段的模板数据。
type checkoutFragmentData struct {
	Paid          bool
	OrderNo       string
	TotalLabel    string
	Email         string
	AccountMailed bool
	// PaymentError 非空表示订单建好了但钱没收到：页面要把它当成「待付款」而不是失败。
	PaymentError string
}

// renderCartSummary 购物车角标计数。
//
// **永不报错**：它是页头的一枚角标，为它渲染一段错误提示比显示 0 更糟
// （页面看起来坏了）。工程未配置、cookie 损坏、服务未接入一律落到 0。
func renderCartSummary(ctx context.Context, r *Request) (string, error) {
	count := 0
	if cartService != nil {
		snap, err := cartService.View(ctx, &cartdto.CartViewReq{
			ProjectID: cartProjectID(r),
			Cookie:    cartCookieValue(r),
		})
		if err == nil && snap != nil {
			count = snap.ItemCount
		}
	}
	return templates.RenderFragment("cart_summary", struct{ Count string }{Count: strconv.Itoa(count)})
}

// renderCartView 渲染购物车。
func renderCartView(ctx context.Context, r *Request) (string, error) {
	if cartService == nil {
		return renderCartNotice(msgCartUnavailable)
	}
	snap, err := cartService.View(ctx, &cartdto.CartViewReq{
		ProjectID: cartProjectID(r),
		Cookie:    cartCookieValue(r),
	})
	if err != nil {
		return renderCartNotice(cartUserMessage(err))
	}
	return templates.RenderFragment("cart_view", cartFragmentOf(r, snap))
}

// renderCartAdd 加入购物车（同变体累加）。
func renderCartAdd(ctx context.Context, r *Request) (string, error) {
	if cartService == nil {
		return renderCartNotice(msgCartUnavailable)
	}
	quantity, qerr := cartAddQuantity(r)
	if qerr != nil {
		return renderCartNotice(cartUserMessage(qerr))
	}
	snap, err := cartService.Add(ctx, &cartdto.CartAddReq{
		ProjectID: cartProjectID(r),
		VariantID: cartVariantID(r),
		Quantity:  quantity,
		Cookie:    cartCookieValue(r),
	})
	if err != nil {
		return renderCartNotice(cartUserMessage(err))
	}
	safeSetCartCookie(r, snap.Cookie)
	return templates.RenderFragment("cart_view", cartFragmentOf(r, snap))
}

// renderCartSetQty 设置数量（0 = 移除）。
func renderCartSetQty(ctx context.Context, r *Request) (string, error) {
	if cartService == nil {
		return renderCartNotice(msgCartUnavailable)
	}
	// 这个能力**不接受缺省值**：数量输入框没填出一个数字时静默按 1 件处理，
	// 会把「我没想改数量」变成一次真实的改单。
	quantity, qerr := cartSetQuantity(r)
	if qerr != nil {
		return renderCartNotice(cartUserMessage(qerr))
	}
	snap, err := cartService.SetQuantity(ctx, &cartdto.CartSetQuantityReq{
		ProjectID: cartProjectID(r),
		VariantID: cartVariantID(r),
		Quantity:  quantity,
		Cookie:    cartCookieValue(r),
	})
	if err != nil {
		return renderCartNotice(cartUserMessage(err))
	}
	safeSetCartCookie(r, snap.Cookie)
	return templates.RenderFragment("cart_view", cartFragmentOf(r, snap))
}

// renderCartClear 清空购物车。
func renderCartClear(ctx context.Context, r *Request) (string, error) {
	if cartService == nil {
		return renderCartNotice(msgCartUnavailable)
	}
	snap, err := cartService.Clear(ctx, &cartdto.CartViewReq{
		ProjectID: cartProjectID(r),
		Cookie:    cartCookieValue(r),
	})
	if err != nil {
		return renderCartNotice(cartUserMessage(err))
	}
	safeSetCartCookie(r, snap.Cookie)
	return templates.RenderFragment("cart_view", cartFragmentOf(r, snap))
}

// renderCheckout 结算：购物车 → 订单 → 支付 → 落账。
//
// 收货信息由页面表单提供（页面作者自己画表单，引擎不硬编码一套结算页外观）：
// email / name / phone / province / city / district / address / zip /
// remark / requestId / locale，账单地址用 bill* 前缀（缺省与收货地址相同）。
func renderCheckout(ctx context.Context, r *Request) (string, error) {
	if cartService == nil {
		return renderCartNotice(msgCheckoutUnavailable)
	}
	res, err := cartService.Checkout(ctx, &cartdto.CartCheckoutReq{
		ProjectID: cartProjectID(r),
		Cookie:    cartCookieValue(r),
		Email:     paramOf(r, "email"),
		Name:      paramOf(r, "name"),
		Phone:     paramOf(r, "phone"),
		Shipping: orderdto.OrderAddress{
			// 收货人与电话缺省沿用联系人：绝大多数订单两者一致，
			// 让访客为「我要寄给别人」再填一遍是把他当成了异常情况。
			Name:     firstNonEmpty(paramOf(r, "shipName"), paramOf(r, "name")),
			Phone:    firstNonEmpty(paramOf(r, "shipPhone"), paramOf(r, "phone")),
			Province: paramOf(r, "province"),
			City:     paramOf(r, "city"),
			District: paramOf(r, "district"),
			Address:  paramOf(r, "address"),
			Zip:      paramOf(r, "zip"),
		},
		Billing: orderdto.OrderAddress{
			Name:     paramOf(r, "billName"),
			Phone:    paramOf(r, "billPhone"),
			Province: paramOf(r, "billProvince"),
			City:     paramOf(r, "billCity"),
			District: paramOf(r, "billDistrict"),
			Address:  paramOf(r, "billAddress"),
			Zip:      paramOf(r, "billZip"),
		},
		Remark:    paramOf(r, "remark"),
		RequestID: paramOf(r, "requestId"),
		Locale:    paramOf(r, "locale"),
		Tracking:  trackCookiesOf(r),
		IPAddress: r.IP,
		UserAgent: r.UserAgent,
	})
	if err != nil {
		// 订单没建出来：把模块给的原因原样透出（「库存不足」必须让访客看到），
		// 但只认白名单里的文案，其余收口到通用提示。
		return renderCartNotice(cartUserMessage(err))
	}
	safeSetCartCookie(r, res.Cookie)
	data := checkoutFragmentData{
		Paid:          res.Paid,
		OrderNo:       res.OrderNo,
		TotalLabel:    res.TotalLabel,
		Email:         res.Email,
		AccountMailed: res.AccountMailed,
		PaymentError:  res.PaymentError,
	}
	return templates.RenderFragment("checkout_result", data)
}

// 未接入时的提示文案（与 cartenums 的白名单文案不同：这些是**装配缺失**，
// 不是业务结果，页面作者看到它应该去接装配，而不是去改访客的输入）。
const (
	msgCartUnavailable     = "购物车功能尚未接入"
	msgCheckoutUnavailable = "结算功能尚未接入"
)

// renderCartNotice 渲染一段提示（业务错误与未接入都走这里）。
//
// 不返回 error：片段端点把 error 变成 500 + 一句「片段渲染失败」，
// 那对访客没有任何信息量。校验失败、库存不足这类结论要**看得见**。
func renderCartNotice(message string) (string, error) {
	if strings.TrimSpace(message) == "" {
		message = cartenums.ErrInternal
	}
	return templates.RenderFragment("cart_notice", struct{ Message string }{Message: message})
}

// cartUserMessage 把错误映射成可以原样给访客看的中文文案。
//
// 白名单来自两个模块（cart / order）：结算链路会穿过订单域，
// 而「库存不足」这种话必须原样透出 —— 否则访客看到的是「操作失败」，
// 既不知道发生了什么，也不知道能不能重试。
func cartUserMessage(err error) string {
	if err == nil {
		return ""
	}
	msg := strings.TrimSpace(err.Error())
	for _, list := range [][]string{cartenums.UserFacingMessages, orderenums.UserFacingMessages} {
		for _, m := range list {
			if m == msg {
				return msg
			}
		}
	}
	return cartenums.ErrInternal
}

// cartFragmentOf 把快照拍成模板数据。
func cartFragmentOf(r *Request, snap *cartdto.CartSnapshot) cartFragmentData {
	data := cartFragmentData{FragmentType: r.Type, ProjectID: cartProjectID(r)}
	if snap == nil {
		data.Empty = true
		return data
	}
	data.Items = snap.Items
	data.LineCount = snap.LineCount
	data.ItemCount = snap.ItemCount
	data.TotalLabel = snap.TotalLabel
	data.Empty = snap.Empty
	return data
}

// safeSetCartCookie 把新的购物车 cookie 值挂到响应上。
//
// 只在值非空时挂：空值意味着「这次没有变更」（只读路径），
// 而写一个空 cookie 会把购物车真的清掉 —— 那是最难查的一类 bug。
func safeSetCartCookie(r *Request, value string) {
	if r == nil || strings.TrimSpace(value) == "" {
		return
	}
	r.SetCookies = append(r.SetCookies, ResponseCookie{
		Name:     cartcontract.CartCookieName,
		Value:    value,
		MaxAge:   cartcontract.CartCookieMaxAgeSeconds,
		HTTPOnly: true,
		SameSite: http.SameSiteLaxMode,
		Path:     "/",
	})
}

// paramOf 取参数值（去空白；参数长度已由端点校验）。
func paramOf(r *Request, key string) string {
	if r == nil {
		return ""
	}
	return strings.TrimSpace(r.Params[key])
}

// cartProjectID 站点工程 id（页面作者在 hx-get 里拼进来的实例配置）。
func cartProjectID(r *Request) string { return paramOf(r, "projectId") }

// cartVariantID 变体 id。
func cartVariantID(r *Request) string { return paramOf(r, "variantId") }

// cartCookieValue 请求里的购物车 cookie 原始值。
func cartCookieValue(r *Request) string {
	if r == nil || len(r.Cookies) == 0 {
		return ""
	}
	return r.Cookies[cartcontract.CartCookieName]
}

// cartAddQuantity 加购数量，缺省 1 件（「加入购物车」按钮的默认语义）。
func cartAddQuantity(r *Request) (int, error) {
	raw := paramOf(r, "quantity")
	if raw == "" {
		return 1, nil
	}
	n, err := strconv.Atoi(raw)
	if err != nil {
		return 0, errors.New(cartenums.ErrQuantityInvalid)
	}
	return n, nil
}

// cartSetQuantity 设置数量，必须显式给出。
func cartSetQuantity(r *Request) (int, error) {
	raw := paramOf(r, "quantity")
	if raw == "" {
		return 0, errors.New(cartenums.ErrQuantityInvalid)
	}
	n, err := strconv.Atoi(raw)
	if err != nil {
		return 0, errors.New(cartenums.ErrQuantityInvalid)
	}
	return n, nil
}

// firstNonEmpty 取第一个非空值。
func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}
