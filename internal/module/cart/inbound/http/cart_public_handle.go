package carthttp

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	cartcontract "go_wp/internal/module/cart/contract"
	cartdto "go_wp/internal/module/cart/dto"
	usercontract "go_wp/internal/module/user/contract"
	"go_wp/pkg/binding"
	"go_wp/pkg/response"
	"go_wp/pkg/sitehttps"
)

// CartPublicHandle 只适配访客输入与可信 cookie，不生成视图。
type CartPublicHandle struct{ svc cartcontract.CartService }

func NewCartPublicHandle(svc cartcontract.CartService) *CartPublicHandle {
	return &CartPublicHandle{svc: svc}
}

func (h *CartPublicHandle) View(c *gin.Context) {
	req, ok := binding.Query[cartdto.CartViewReq](c)
	if !ok {
		return
	}
	req.Cookie = requestCookie(c, cartcontract.CartCookieName)
	data, err := h.svc.View(c.Request.Context(), req)
	if err != nil {
		response.ErrorStatusAuto(c, "cart", err)
		return
	}
	response.Success(c, data)
}
func (h *CartPublicHandle) Add(c *gin.Context) {
	req, ok := binding.JSON[cartdto.CartAddReq](c)
	if !ok {
		return
	}
	req.Cookie = requestCookie(c, cartcontract.CartCookieName)
	data, err := h.svc.Add(c.Request.Context(), req)
	if err != nil {
		response.ErrorStatusAuto(c, "cart", err)
		return
	}
	writeCartCookie(c, data.Cookie)
	response.Success(c, data)
}
func (h *CartPublicHandle) SetQuantity(c *gin.Context) {
	req, ok := binding.JSON[cartdto.CartSetQuantityReq](c)
	if !ok {
		return
	}
	req.Cookie = requestCookie(c, cartcontract.CartCookieName)
	data, err := h.svc.SetQuantity(c.Request.Context(), req)
	if err != nil {
		response.ErrorStatusAuto(c, "cart", err)
		return
	}
	writeCartCookie(c, data.Cookie)
	response.Success(c, data)
}
func (h *CartPublicHandle) Clear(c *gin.Context) {
	req, ok := binding.JSON[cartdto.CartViewReq](c)
	if !ok {
		return
	}
	req.Cookie = requestCookie(c, cartcontract.CartCookieName)
	data, err := h.svc.Clear(c.Request.Context(), req)
	if err != nil {
		response.ErrorStatusAuto(c, "cart", err)
		return
	}
	writeCartCookie(c, data.Cookie)
	response.Success(c, data)
}
func (h *CartPublicHandle) Checkout(c *gin.Context) {
	req, ok := binding.JSON[cartdto.CartCheckoutReq](c)
	if !ok {
		return
	}
	checkoutRequestMetadata(c, req)
	data, err := h.svc.Checkout(c.Request.Context(), req)
	if err != nil {
		response.ErrorStatusAuto(c, "cart", err)
		return
	}
	writeCartCookie(c, data.Cookie)
	response.Success(c, data)
}

func requestCookie(c *gin.Context, name string) string { value, _ := c.Cookie(name); return value }

// writeCartCookie 的安全属性只取部署配置，不信请求转发头。
func writeCartCookie(c *gin.Context, value string) {
	if strings.TrimSpace(value) == "" {
		return
	}
	cookie := &http.Cookie{Name: cartcontract.CartCookieName, Value: value, Path: "/", MaxAge: cartcontract.CartCookieMaxAgeSeconds, HttpOnly: true, Secure: sitehttps.Enabled(), SameSite: http.SameSiteLaxMode}
	// 追加响应头，保留同一次请求里访客会话写入的 cookie。
	c.Writer.Header().Add("Set-Cookie", cookie.String())
}

// checkoutRequestMetadata 只读可信请求上下文，客户端同名字段被 DTO 屏蔽。
func checkoutRequestMetadata(c *gin.Context, req *cartdto.CartCheckoutReq) {
	req.Cookie = requestCookie(c, cartcontract.CartCookieName)
	req.Tracking = cartdto.TrackCookies{Current: requestCookie(c, "gw_src"), First: requestCookie(c, "gw_first"), Session: requestCookie(c, "gw_sess"), Trail: requestCookie(c, "gw_trail"), Visitor: requestCookie(c, "gw_vis")}
	req.UserID = nil
	if raw, exists := c.Get(usercontract.VisitorContextKey); exists {
		if id, ok := raw.(uint64); ok && id != 0 {
			req.UserID = &id
		}
	}
	req.IPAddress = c.ClientIP()
	req.UserAgent = c.Request.UserAgent()
}
