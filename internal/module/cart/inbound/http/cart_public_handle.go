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

type CartPublicHandle struct{ svc cartcontract.CartService }

func NewCartPublicHandle(svc cartcontract.CartService) *CartPublicHandle {
	return &CartPublicHandle{svc: svc}
}

func (h *CartPublicHandle) View(c *gin.Context) {
	req, err := binding.ParseQuery[cartdto.CartViewReq](c)
	if err != nil {
		response.ParamError(c)
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
	req, err := binding.ParseForm[cartdto.CartAddReq](c)
	if err != nil {
		response.ParamError(c)
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
	req, err := binding.ParseForm[cartdto.CartSetQuantityReq](c)
	if err != nil {
		response.ParamError(c)
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
	req, err := binding.ParseForm[cartdto.CartViewReq](c)
	if err != nil {
		response.ParamError(c)
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
	req, err := binding.ParseForm[cartdto.CartCheckoutReq](c)
	if err != nil {
		response.ParamError(c)
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

func writeCartCookie(c *gin.Context, value string) {
	if strings.TrimSpace(value) == "" {
		return
	}
	cookie := &http.Cookie{Name: cartcontract.CartCookieName, Value: value, Path: "/", MaxAge: cartcontract.CartCookieMaxAgeSeconds, HttpOnly: true, Secure: sitehttps.Enabled(), SameSite: http.SameSiteLaxMode}

	c.Writer.Header().Add("Set-Cookie", cookie.String())
}

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
