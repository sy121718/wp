// Package carthttp 购物车模块的 HTTP 接入层：当前只有一条**支付回调**端点。
//
// 为什么本模块此前没有 inbound/http、现在也只有这一条：购物车的其余能力全部经访问面的
// /_fragments 端点暴露（它们要读客户端签名 cookie，参数也受片段协议约束）。
// 而支付回调是**通道服务端打过来**的通知：它没有会话、没有 cookie、带的是原始报文，
// 用片段端点去接会同时撞上「参数长度上限」与「上下文白名单」两条协议约束，
// 而且它本来就不是浏览器发起的请求。
//
// 越权防护在这条链路上只有一件事可做：**验签**（见 PaymentGateway.VerifyCallback）。
// 所以它直挂公开路由，不进 /api 的三层链 —— 通道不可能持有后台会话与 CSRF token。
package carthttp

import (
	"io"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"go_wp/pkg/logger"

	cartcontract "go_wp/internal/module/cart/contract"
	cartdto "go_wp/internal/module/cart/dto"
	cartenums "go_wp/internal/module/cart/enums"
	"go_wp/pkg/response"
)

// maxCallbackBody 回调报文大小上限。
//
// 回调是**未认证**的输入：不限长就等于把内存交给任何能访问这个路径的人。
const maxCallbackBody = 64 << 10

// CallbackHandle 支付回调处理器。
type CallbackHandle struct {
	svc cartcontract.CartService
}

// NewCallbackHandle 构造。
func NewCallbackHandle(svc cartcontract.CartService) *CallbackHandle {
	return &CallbackHandle{svc: svc}
}

// PaymentCallback 支付通道异步回调。
//
// 读的是**原始 body**（不是 ShouldBind 出来的结构体）：验签必须拿原始字节算。
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
		// 头名归一化成小写：HTTP/1.1 不区分大小写，各通道写法又不统一，
		// 让每个通道实现自己去猜大小写是最容易踩的坑之一。
		headers[strings.ToLower(k)] = v[0]
	}
	res, serr := h.svc.HandlePaymentCallback(c.Request.Context(), &cartdto.PaymentCallbackReq{
		// 工程 id 从 notify_url 的查询参数取（是我们自己拼进回调地址的）。
		ProjectID: strings.TrimSpace(c.Query("projectId")),
		Headers:   headers,
		RawBody:   raw,
	})
	if serr != nil {
		// 非 2xx 会让通道重试：验签失败与订单找不到都属于「重试也不会有结果」，
		// 所以用 4xx 明确告诉对方别再发了；网关侧一般会对 4xx 停止重试。
		//
		// 但**消息不能直接用 err.Error()**：通道是第三方，基础设施错误的原文会带表名、
		// 列名甚至 SQL 片段（审计 CQ-009 同一条判据 —— 那给运维看的东西不该出网）。
		// 只放行回调自身的四条业务 key，其余一律收口到 cart.err.internal，原文进日志。
		if msg := callbackErrorKey(serr.Error()); msg != "" {
			response.ErrorWithMessage(c, http.StatusBadRequest, msg)
		} else {
			logger.Scene("cart").Error(serr, "支付回调处理失败")
			response.ErrorWithMessage(c, http.StatusBadRequest, cartenums.ErrInternal)
		}
		return
	}
	response.SuccessWithMessage(c, "回调已接收", res)
}

// callbackErrorKey 回调对外只认这四条业务 key（命中返回 key，未命中返回空串）。
//
// 两种形态都认：整串等于 key，或以 "key：" 开头 —— service 用
// fmt.Errorf("%s：补充说明", key) 把 key 拼进整句话，补充说明只记日志、不回给通道。
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

// SetupCartRoutes 注册购物车的 HTTP 路由（当前只有支付回调）。
//
// 公开路由：通道是服务端到服务端调用，没有会话也没有 CSRF token，
// 唯一的来源证明是签名 —— 这写在契约（VerifyCallback）里，不在这条路由的装配上。
func SetupCartRoutes(router *gin.Engine, svc cartcontract.CartService) {
	if router == nil || svc == nil {
		return
	}
	h := NewCallbackHandle(svc)
	router.POST("/payment/callback", h.PaymentCallback)
}
