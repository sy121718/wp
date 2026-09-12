package runtimefragment

// cart_test.go — 购物车片段的 HTTP 层测试（包内）。
//
// 覆盖的是**协议层**而不是业务：响应 cookie 有没有按属性写出去、业务错误有没有变成
// 500、追踪 cookie 与客户端 IP 有没有传进 service。业务不变量在
// public/test/cart/feature/（真实 PostgreSQL）里覆盖。
//
// 为什么这一层必须单独测：cookie 属性写错（少了 HttpOnly / SameSite）不会有任何报错，
// 页面看起来完全正常，只是防线没了 —— 这类缺陷只有断言响应头才看得见。

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	cartcontract "go_wp/internal/module/cart/contract"
	cartdto "go_wp/internal/module/cart/dto"
	cartenums "go_wp/internal/module/cart/enums"
)

// fakeCart 购物车替身：记录入参、返回可控结果。
type fakeCart struct {
	addReq       *cartdto.CartAddReq
	setQtyReq    *cartdto.CartSetQuantityReq
	checkoutReq  *cartdto.CartCheckoutReq
	snap         *cartdto.CartSnapshot
	checkoutResp *cartdto.CheckoutResp
	err          error
}

func (f *fakeCart) Add(_ context.Context, req *cartdto.CartAddReq) (*cartdto.CartSnapshot, error) {
	f.addReq = req
	if f.err != nil {
		return nil, f.err
	}
	return f.snap, nil
}

func (f *fakeCart) SetQuantity(_ context.Context, req *cartdto.CartSetQuantityReq) (*cartdto.CartSnapshot, error) {
	f.setQtyReq = req
	if f.err != nil {
		return nil, f.err
	}
	return f.snap, nil
}

func (f *fakeCart) Clear(_ context.Context, _ *cartdto.CartViewReq) (*cartdto.CartSnapshot, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.snap, nil
}

func (f *fakeCart) View(_ context.Context, _ *cartdto.CartViewReq) (*cartdto.CartSnapshot, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.snap, nil
}

func (f *fakeCart) Checkout(_ context.Context, req *cartdto.CartCheckoutReq) (*cartdto.CheckoutResp, error) {
	f.checkoutReq = req
	if f.err != nil {
		return nil, f.err
	}
	return f.checkoutResp, nil
}

// newFragmentCtx 构造一次片段端点调用（路由参数、表单、cookie 齐备）。
func newFragmentCtx(t *testing.T, method, typeName string, form url.Values, cookies map[string]string) (*gin.Context, *httptest.ResponseRecorder) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	if method == http.MethodPost {
		c.Request = httptest.NewRequest(method, "/_fragments/"+typeName, strings.NewReader(form.Encode()))
		c.Request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	} else {
		c.Request = httptest.NewRequest(method, "/_fragments/"+typeName+"?"+form.Encode(), nil)
	}
	for name, value := range cookies {
		c.Request.AddCookie(&http.Cookie{Name: name, Value: value})
	}
	c.Request.Header.Set("User-Agent", "FragmentTest/1.0")
	c.Params = gin.Params{{Key: "type", Value: typeName}}
	return c, w
}

// findResponseCookie 从响应头里取指定名字的 Set-Cookie。
func findResponseCookie(w *httptest.ResponseRecorder, name string) *http.Cookie {
	for _, ck := range w.Result().Cookies() {
		if ck.Name == name {
			return ck
		}
	}
	return nil
}

// TestCartAddWritesHttpOnlyLaxCookie 加购成功必须把新的购物车 cookie 按属性写出去。
func TestCartAddWritesHttpOnlyLaxCookie(t *testing.T) {
	fake := &fakeCart{snap: &cartdto.CartSnapshot{
		Cookie: "signed-payload.signature", ItemCount: 2, LineCount: 1, TotalLabel: "¥60.00",
		Items: []*cartdto.CartItem{{VariantID: "v1", ProductName: "商品", Quantity: 2}},
	}}
	SetCartProvider(fake)
	defer SetCartProvider(nil)

	form := url.Values{"projectId": {"p1"}, "variantId": {"v1"}, "quantity": {"2"}}
	c, w := newFragmentCtx(t, http.MethodPost, "cartAdd", form, map[string]string{cartcontract.CartCookieName: "old.cookie"})
	FragmentEndpoint(c)

	if w.Code != http.StatusOK {
		t.Fatalf("状态码应为 200，实际 %d（%s）", w.Code, w.Body.String())
	}
	ck := findResponseCookie(w, cartcontract.CartCookieName)
	if ck == nil {
		t.Fatal("加购成功后必须写出新的购物车 cookie —— 没有它购物车等于没变")
	}
	if ck.Value != "signed-payload.signature" {
		t.Fatalf("写出的 cookie 值应为 service 返回的那个，实际 %q", ck.Value)
	}
	if !ck.HttpOnly {
		t.Fatal("购物车 cookie 必须 HttpOnly：没有脚本需要读它，放开就等于把「买了什么」交给任何注入脚本")
	}
	if ck.SameSite != http.SameSiteLaxMode {
		t.Fatalf("购物车 cookie 必须是 SameSite=Lax（匿名写路径的 CSRF 防线），实际 %v", ck.SameSite)
	}
	if ck.MaxAge <= 0 {
		t.Fatal("购物车 cookie 应带 Max-Age（否则关掉浏览器车就没了）")
	}
	if fake.addReq == nil || fake.addReq.Cookie != "old.cookie" {
		t.Fatalf("请求里的旧购物车 cookie 应原样交给 service，实际 %+v", fake.addReq)
	}
	if !strings.Contains(w.Body.String(), "sky-cart") {
		t.Fatalf("响应应是购物车片段 HTML，实际 %s", w.Body.String())
	}
}

// TestCartBusinessErrorRendersNoticeNot500 业务错误（比如库存不足）要看得见，而不是 500。
func TestCartBusinessErrorRendersNoticeNot500(t *testing.T) {
	SetCartProvider(&fakeCart{err: errors.New(cartenums.ErrOutOfStock)})
	defer SetCartProvider(nil)

	form := url.Values{"projectId": {"p1"}, "variantId": {"v1"}, "quantity": {"5"}}
	c, w := newFragmentCtx(t, http.MethodPost, "cartAdd", form, nil)
	FragmentEndpoint(c)

	if w.Code != http.StatusOK {
		t.Fatalf("业务失败不该是 500（端点把 error 变成「片段渲染失败」，对访客没有信息量），实际 %d", w.Code)
	}
	body := w.Body.String()
	if !strings.Contains(body, cartenums.ErrOutOfStock) {
		t.Fatalf("库存不足的原因必须原样透出，实际 %s", body)
	}
	if findResponseCookie(w, cartcontract.CartCookieName) != nil {
		t.Fatal("失败的请求绝不能写购物车 cookie（否则页面与服务端各说各话）")
	}
}

// TestFragmentErrorCollapsesToGenericMessage 白名单外的错误收口到通用文案，不泄漏内部信息。
func TestFragmentErrorCollapsesToGenericMessage(t *testing.T) {
	SetCartProvider(&fakeCart{err: errors.New("pq: relation \"orders\" does not exist")})
	defer SetCartProvider(nil)

	form := url.Values{"projectId": {"p1"}, "variantId": {"v1"}, "quantity": {"1"}}
	c, w := newFragmentCtx(t, http.MethodPost, "cartAdd", form, nil)
	FragmentEndpoint(c)

	body := w.Body.String()
	if strings.Contains(body, "does not exist") || strings.Contains(body, "orders") {
		t.Fatalf("内部错误原文不能出现在访客看到的 HTML 里: %s", body)
	}
	if !strings.Contains(body, cartenums.ErrInternal) {
		t.Fatalf("白名单外的错误应收口到通用文案，实际 %s", body)
	}
}

// TestCheckoutPassesTrackingCookiesAndClientInfo 追踪 cookie、IP、UA 要传进结算请求。
func TestCheckoutPassesTrackingCookiesAndClientInfo(t *testing.T) {
	fake := &fakeCart{checkoutResp: &cartdto.CheckoutResp{
		OrderID: 7, OrderNo: "GWP20260101ABCDEF", Paid: true, TotalLabel: "¥30.00",
		Email: "buyer@example.com", Cookie: "empty.cart",
	}}
	SetCartProvider(fake)
	defer SetCartProvider(nil)

	form := url.Values{
		"projectId": {"p1"}, "email": {"buyer@example.com"}, "name": {"张三"},
		"phone": {"13800000000"}, "address": {"科技园一号"}, "city": {"深圳市"},
		"requestId": {"req-9"},
	}
	cookies := map[string]string{
		cartcontract.CartCookieName: "cart.cookie",
		trackCookieCurrent:          "t=utm&s=google",
		trackCookieFirst:            "t=referral&s=partner.example",
		trackCookieSession:          "e=/home&p=2",
		trackCookieTrail:            "%5B%5B%22%2Fp%2F1%22%2C%22%E5%95%86%E5%93%81%22%2C1700000000%5D%5D",
		trackCookieVisitor:          "n=2",
	}
	c, w := newFragmentCtx(t, http.MethodPost, "checkout", form, cookies)
	FragmentEndpoint(c)

	if w.Code != http.StatusOK {
		t.Fatalf("状态码应为 200，实际 %d（%s）", w.Code, w.Body.String())
	}
	req := fake.checkoutReq
	if req == nil {
		t.Fatal("结算请求没有到达 service")
	}
	if req.Cookie != "cart.cookie" {
		t.Fatalf("购物车 cookie 未传入: %q", req.Cookie)
	}
	if req.Tracking.Current != "t=utm&s=google" || req.Tracking.First != "t=referral&s=partner.example" {
		t.Fatalf("追踪 cookie 未传入: %+v", req.Tracking)
	}
	if req.Tracking.Session != "e=/home&p=2" || req.Tracking.Visitor != "n=2" {
		t.Fatalf("会话 / 访客 cookie 未传入: %+v", req.Tracking)
	}
	if req.Tracking.Trail != cookies[trackCookieTrail] {
		t.Fatalf("轨迹 cookie 未传入: %q", req.Tracking.Trail)
	}
	if req.IPAddress == "" {
		t.Fatal("客户端 IP 应传进结算请求（订单表有这一列）")
	}
	if req.UserAgent != "FragmentTest/1.0" {
		t.Fatalf("UA 应取请求头，实际 %q", req.UserAgent)
	}
	if req.Shipping.Address != "科技园一号" || req.Shipping.City != "深圳市" {
		t.Fatalf("收货地址未绑定: %+v", req.Shipping)
	}
	// 收货人缺省沿用联系人（表单没填 shipName 时）。
	if req.Shipping.Name != "张三" || req.Shipping.Phone != "13800000000" {
		t.Fatalf("收货人 / 电话应缺省沿用联系人: %+v", req.Shipping)
	}
	ck := findResponseCookie(w, cartcontract.CartCookieName)
	if ck == nil || ck.Value != "empty.cart" {
		t.Fatalf("结算成功后应写出空购物车 cookie，实际 %+v", ck)
	}
}

// TestCartSummaryNeverFails 角标计数在最坏情况下也要渲染出数字（不能把页头搞坏）。
func TestCartSummaryNeverFails(t *testing.T) {
	SetCartProvider(&fakeCart{err: errors.New("db down")})
	defer SetCartProvider(nil)

	c, w := newFragmentCtx(t, http.MethodGet, "cartSummary", url.Values{"projectId": {"p1"}}, nil)
	FragmentEndpoint(c)

	if w.Code != http.StatusOK {
		t.Fatalf("角标计数应恒为 200，实际 %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), ">0<") {
		t.Fatalf("读不到购物车时角标应显示 0，实际 %s", w.Body.String())
	}
}

// TestCartSetQtyRequiresExplicitQuantity 改数量必须显式给值（缺省静默按 1 件是改单）。
func TestCartSetQtyRequiresExplicitQuantity(t *testing.T) {
	SetCartProvider(&fakeCart{snap: &cartdto.CartSnapshot{Cookie: "x.y"}})
	defer SetCartProvider(nil)

	form := url.Values{"projectId": {"p1"}, "variantId": {"v1"}}
	c, w := newFragmentCtx(t, http.MethodPost, "cartSetQty", form, nil)
	FragmentEndpoint(c)

	if w.Code != http.StatusOK {
		t.Fatalf("参数错误应渲染提示而非 500，实际 %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), cartenums.ErrQuantityInvalid) {
		t.Fatalf("缺数量时应给出参数错误提示，实际 %s", w.Body.String())
	}
	if findResponseCookie(w, cartcontract.CartCookieName) != nil {
		t.Fatal("参数错误时不该写购物车 cookie")
	}
}

// TestCartMethodMismatchRejected GET 能力不接受 POST（反之亦然）。
func TestCartMethodMismatchRejected(t *testing.T) {
	SetCartProvider(&fakeCart{})
	defer SetCartProvider(nil)

	c, w := newFragmentCtx(t, http.MethodPost, "cartView", url.Values{}, nil)
	FragmentEndpoint(c)
	if w.Code != http.StatusMethodNotAllowed {
		t.Fatalf("cartView 是 GET 能力，POST 应被拒，实际 %d", w.Code)
	}
}

// 编译期断言：替身实现契约（契约变了这里先失败，而不是等到装配期）。
var _ cartcontract.CartService = (*fakeCart)(nil)
