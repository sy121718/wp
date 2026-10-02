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
	"fmt"
	"net/http"
	"strconv"
	"strings"

	cartcontract "go_wp/internal/module/cart/contract"
	cartdto "go_wp/internal/module/cart/dto"
	cartenums "go_wp/internal/module/cart/enums"
	orderdto "go_wp/internal/module/order/dto"
	orderenums "go_wp/internal/module/order/enums"

	// 槽位键名的唯一来源：片段与后台页都从这里取，避免各处手写字符串
	//（键名写错不会报错，只会静默不生效，是最难查的一类）。
	pageenums "go_wp/internal/module/page/enums"
	rfenums "go_wp/internal/module/runtimefragment/enums"
	"go_wp/internal/templates"
)

// cartService 购物车依赖（装配期注入）。
//
// 装配自检（审计 CQ-019）：判为 required-port —— 实现由 routes.go 在同一个函数里
// 构造（cartservice.NewService）后立即注入，本进程内恒定可得，不存在「合法地不接」的
// 部署形态；为空只可能是有人删掉了注入行。
// nil 分支保留给单测，其表现是购物车六个能力一律渲染「暂不可用」文案 ——
// 那是把装配缺陷伪装成服务故障，不能当生产降级路径。
var cartService cartcontract.CartService

// SetCartProvider 注入购物车能力（装配期调用；**必须注入**，理由见字段注释）。
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
	Labels       cartViewLabels
	Items        []*cartdto.CartItem
	LineCount    int
	ItemCount    int
	TotalLabel   string
	Empty        bool
	// CheckoutURL 「去结算」的目标（槽位 checkout 的线上路径）。
	//
	// 空 = 这个站还没指定结算页 → 模板**不输出**该按钮。不猜路径：
	// 猜错的链接（指向一个不存在的页面）比没有按钮难查得多。
	CheckoutURL string
}

// checkoutFragmentData 结算结果片段的模板数据。
type checkoutFragmentData struct {
	Paid          bool
	OrderNo       string
	TotalLabel    string
	Email         string
	AccountMailed bool
	Labels        checkoutViewLabels
	AccountNote   string
	// PaymentError 非空表示订单建好了但钱没收到：页面要把它当成「待付款」而不是失败。
	PaymentError string
	// OrdersURL 「我的订单」目标（槽位 orders）；空 = 没配 → 不输出。
	OrdersURL string
	// ShopURL 「继续购物」目标（槽位 shop）；空 = 没配 → 不输出。
	ShopURL string
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
		return renderCartNotice(r, msgCartUnavailable)
	}
	snap, err := cartService.View(ctx, &cartdto.CartViewReq{
		ProjectID: cartProjectID(r),
		Cookie:    cartCookieValue(r),
	})
	if err != nil {
		return renderCartNotice(r, cartUserMessage(r, err))
	}
	return templates.RenderFragment("cart_view", cartFragmentOf(r, snap))
}

// renderCartAdd 加入购物车（同变体累加）。
func renderCartAdd(ctx context.Context, r *Request) (string, error) {
	if cartService == nil {
		return renderCartNotice(r, msgCartUnavailable)
	}
	quantity, qerr := cartAddQuantity(r)
	if qerr != nil {
		return renderCartNotice(r, cartUserMessage(r, qerr))
	}
	snap, err := cartService.Add(ctx, &cartdto.CartAddReq{
		ProjectID: cartProjectID(r),
		VariantID: cartVariantID(r),
		Quantity:  quantity,
		Cookie:    cartCookieValue(r),
	})
	if err != nil {
		return renderCartNotice(r, cartUserMessage(r, err))
	}
	safeSetCartCookie(r, snap.Cookie)
	return templates.RenderFragment("cart_view", cartFragmentOf(r, snap))
}

// renderCartSetQty 设置数量（0 = 移除）。
func renderCartSetQty(ctx context.Context, r *Request) (string, error) {
	if cartService == nil {
		return renderCartNotice(r, msgCartUnavailable)
	}
	// 这个能力**不接受缺省值**：数量输入框没填出一个数字时静默按 1 件处理，
	// 会把「我没想改数量」变成一次真实的改单。
	quantity, qerr := cartSetQuantity(r)
	if qerr != nil {
		return renderCartNotice(r, cartUserMessage(r, qerr))
	}
	snap, err := cartService.SetQuantity(ctx, &cartdto.CartSetQuantityReq{
		ProjectID: cartProjectID(r),
		VariantID: cartVariantID(r),
		Quantity:  quantity,
		Cookie:    cartCookieValue(r),
	})
	if err != nil {
		return renderCartNotice(r, cartUserMessage(r, err))
	}
	safeSetCartCookie(r, snap.Cookie)
	return templates.RenderFragment("cart_view", cartFragmentOf(r, snap))
}

// renderCartClear 清空购物车。
func renderCartClear(ctx context.Context, r *Request) (string, error) {
	if cartService == nil {
		return renderCartNotice(r, msgCartUnavailable)
	}
	snap, err := cartService.Clear(ctx, &cartdto.CartViewReq{
		ProjectID: cartProjectID(r),
		Cookie:    cartCookieValue(r),
	})
	if err != nil {
		return renderCartNotice(r, cartUserMessage(r, err))
	}
	safeSetCartCookie(r, snap.Cookie)
	return templates.RenderFragment("cart_view", cartFragmentOf(r, snap))
}

// renderCheckout 结算：购物车 → 订单 → 支付 → 落账。
//
// 收货信息由页面表单提供（页面作者自己画表单，引擎不硬编码一套结算页外观）：
// email / name / phone / country / province / city / district / address / zip /
// remark / requestId / locale，账单地址用 bill* 前缀（缺省与收货地址相同）。
func renderCheckout(ctx context.Context, r *Request) (string, error) {
	if cartService == nil {
		return renderCartNotice(r, msgCheckoutUnavailable)
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
			Name:  firstNonEmpty(paramOf(r, "shipName"), paramOf(r, "name")),
			Phone: firstNonEmpty(paramOf(r, "shipPhone"), paramOf(r, "phone")),
			// 国家/地区代码：只做形状约束（orderdto.NormalizeCountryCode，与后台代客建单页
			// 共用同一份规则 —— 两条入口各写一份必然漂移，而漂移只在写库那一刻暴露），
			// 认不出的一律丢弃成空串。它不像运费那样是收银台上的钱：只是地址的一行，
			// 不影响金额与库存。
			Country:  orderdto.NormalizeCountryCode(paramOf(r, "country")),
			Province: paramOf(r, "province"),
			City:     paramOf(r, "city"),
			District: paramOf(r, "district"),
			Address:  paramOf(r, "address"),
			Zip:      paramOf(r, "zip"),
		},
		Billing: orderdto.OrderAddress{
			Name:     paramOf(r, "billName"),
			Phone:    paramOf(r, "billPhone"),
			Country:  orderdto.NormalizeCountryCode(paramOf(r, "billCountry")),
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
		// 访客身份（BIZ-3）：契约把 UserID 列在「由 inbound 覆盖写入，客户端不可伪造」
		// 那一段，而它此前**没有任何调用方写入** —— 于是结算链路上的会员折扣与免运费
		// 永远解析不出身份（静默地按「非会员」结算，不报错）。身份只从**已解析的会话**
		// 取（r.UserID 由访客身份中间件写入），不认表单里的任何 user 字段。
		UserID: visitorIDPtrOf(r),
		// 运费刻意**不从表单取**：结算表单的字段全部来自请求参数（上面逐个 paramOf），
		// 而运费是收银台上的一笔钱 —— 让客户端填它等于让客人自己免单。
		// 基准运费由服务端提供（当前系统无站点级运费策略，故为 0）；
		// 会员的 free_shipping 权益在 cart 侧作用在这一笔上（见 cart 模块的 cart_shipping.go）。
	})
	if err != nil {
		// 订单没建出来：把模块给的原因原样透出（「库存不足」必须让访客看到），
		// 但只认白名单里的文案，其余收口到通用提示。
		return renderCartNotice(r, cartUserMessage(r, err))
	}
	safeSetCartCookie(r, res.Cookie)
	slots := cartSitePages(r, cartProjectID(r))
	labels := checkoutViewLabelsOf(r)
	data := checkoutFragmentData{
		Paid:          res.Paid,
		OrderNo:       res.OrderNo,
		TotalLabel:    res.TotalLabel,
		Email:         res.Email,
		AccountMailed: res.AccountMailed,
		PaymentError:  res.PaymentError,
		OrdersURL:     slots[pageenums.SiteSlotOrders],
		ShopURL:       slots[pageenums.SiteSlotShop],
		Labels:        labels,
	}
	if res.AccountMailed && res.Email != "" {
		data.AccountNote = fmt.Sprintf(labels.AccountMailed, res.Email)
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
func renderCartNotice(r *Request, message string) (string, error) {
	if strings.TrimSpace(message) == "" {
		message = cartenums.ErrInternal
	}
	switch message {
	case msgCartUnavailable:
		message = r.tr(rfenums.CartUnavailable, msgCartUnavailable)
	case msgCheckoutUnavailable:
		message = r.tr(rfenums.CheckoutUnavailable, msgCheckoutUnavailable)
	}
	return templates.RenderFragment("cart_notice", struct{ Message string }{Message: message})
}

// cartUserMessage 把错误映射成可以原样给访客看的中文文案。
//
// 白名单来自两个模块（cart / order）：结算链路会穿过订单域，
// 而「库存不足」这种话必须原样透出 —— 否则访客看到的是「操作失败」，
// 既不知道发生了什么，也不知道能不能重试。
func cartUserMessage(r *Request, err error) string {
	if err == nil {
		return ""
	}
	msg := strings.TrimSpace(err.Error())
	for _, list := range [][]string{cartenums.UserFacingMessages, orderenums.UserFacingMessages} {
		for _, m := range list {
			if m == msg {
				return fragmentUserMessage(r, msg)
			}
		}
	}
	return fragmentUserMessage(r, cartenums.ErrInternal)
}

// cartFragmentOf 把快照拍成模板数据。
func cartFragmentOf(r *Request, snap *cartdto.CartSnapshot) cartFragmentData {
	projectID := cartProjectID(r)
	data := cartFragmentData{
		FragmentType: r.Type,
		ProjectID:    projectID,
		Labels:       cartViewLabelsOf(r),
		CheckoutURL:  cartSitePages(r, projectID)[pageenums.SiteSlotCheckout],
	}
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

// cartSitePages 解析本工程在当前语言下的系统页面槽位路径（BIZ-1）。
//
// 语言取自请求参数 lang，缺省为站点默认语言：页面作者手写的 hx-get 通常只带 projectId，
// 单语言站点下这与本站语义完全一致；多语言站点要在 hx-get 里带上 lang 才能拿到
// 本语言的路径 —— 这一点写进文档，不做「从 Referer 猜语言」那种启发式：
// 猜错时链接会在某个语言下静默指向另一语言，比缺 lang 更难查。
//
// 未接入解析器或解析失败一律返回 nil（模板据此不输出链接）。
func cartSitePages(r *Request, projectID string) map[string]string {
	if r == nil || r.SitePagesOf == nil {
		return nil
	}
	lang := r.Lang
	if lang == "" {
		lang = strings.TrimSpace(paramOf(r, fragmentLangParam))
	}
	return r.SitePagesOf(projectID, lang)
}

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

// visitorIDPtrOf 本次请求的访客身份，未登录时返回 nil。
//
// 与 orders.go 的 visitorIDOf 同源（都从 Request.UserID 解析，都由访客身份中间件写入），
// 区别只在类型：结算契约里 UserID 是 *uint64（「未登录」必须是可表达的状态 ——
// 0 是无效账号 id，用它表示未登录会让「没登录」与「登录到 id=0」看起来一样）。
func visitorIDPtrOf(r *Request) *uint64 {
	id, ok := visitorIDOf(r)
	if !ok {
		return nil
	}
	return &id
}
