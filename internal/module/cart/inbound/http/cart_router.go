package carthttp

import (
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"go_wp/internal/middleware/builtin"
	"go_wp/internal/permission"

	"go_wp/pkg/logger"

	cartcontract "go_wp/internal/module/cart/contract"
	cartdto "go_wp/internal/module/cart/dto"
	cartenums "go_wp/internal/module/cart/enums"
	"go_wp/pkg/response"
)

const maxCallbackBody = 64 << 10

type CallbackHandle struct {
	svc cartcontract.CartService
}

func NewCallbackHandle(svc cartcontract.CartService) *CallbackHandle {
	return &CallbackHandle{svc: svc}
}

func (h *CallbackHandle) PaymentCallback(c *gin.Context) {
	raw, err := io.ReadAll(io.LimitReader(c.Request.Body, maxCallbackBody))
	if err != nil {
		response.ErrorWithMessage(c, http.StatusBadRequest, cartenums.ErrInvalidParam)
		return
	}
	headers := make(map[string]string, len(c.Request.Header))
	for k, v := range c.Request.Header {
		if len(v) == 0 {
			continue
		}

		headers[strings.ToLower(k)] = v[0]
	}
	res, serr := h.svc.HandlePaymentCallback(c.Request.Context(), &cartdto.PaymentCallbackReq{

		ProjectID: strings.TrimSpace(c.Query("projectId")),
		Headers:   headers,
		RawBody:   raw,
	})
	if serr != nil {

		if msg := callbackErrorKey(serr.Error()); msg != "" {
			response.ErrorWithMessage(c, http.StatusBadRequest, msg)
		} else {
			logger.Scene("cart").Error(serr, "支付回调处理失败")
			response.ErrorWithMessage(c, http.StatusBadRequest, cartenums.ErrInternal)
		}
		return
	}
	response.SuccessWithMessage(c, cartenums.MsgCallbackReceived, res)
}

func callbackErrorKey(raw string) string {
	msg := strings.TrimSpace(raw)
	for _, key := range []string{
		cartenums.ErrCallbackSignature,
		cartenums.ErrCallbackOrderMissing,
		cartenums.ErrCallbackAmountMismatch,
		cartenums.ErrPaymentFailed,
	} {
		if msg == key || strings.HasPrefix(msg, key+"：") || strings.HasPrefix(msg, key+":") {
			return key
		}
	}
	return ""
}

func SetupCartRoutes(router *gin.Engine, svc cartcontract.CartService) {
	if router == nil || svc == nil {
		return
	}
	h := NewCallbackHandle(svc)
	router.POST("/payment/callback", h.PaymentCallback)
}

func SetupCartPublicRoutes(router *gin.Engine, svc cartcontract.CartService, middleware ...gin.HandlerFunc) {
	if router == nil || svc == nil {
		return
	}
	g := permission.NewRouteGroup(router.Group("/api/site/cart", middleware...))
	g.Use(builtin.RequestRateLimitMiddleware(90, time.Minute))
	h := NewCartPublicHandle(svc)
	g.GET("", permission.Exempt, h.View)
	g.POST("/add", permission.Exempt, h.Add)
	g.POST("/quantity", permission.Exempt, h.SetQuantity)
	g.POST("/clear", permission.Exempt, h.Clear)
	g.POST("/checkout", permission.Exempt, h.Checkout)
}
