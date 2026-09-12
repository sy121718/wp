// mail_tracking.go — 营销追踪端点（公开路由，issue #38 P1）。
//
// 三个端点在**访问面**：无鉴权、无登录态、只做受控的事。
//
//	GET /_t/o/{token}.gif  打开追踪：返回 1×1 透明 GIF，事件异步入队
//	GET /_t/c/{token}      点击追踪：验签 → 记录 → 302 跳转到**签名里的那个 URL**
//	GET /_t/u/{token}      一键退订：写抑制名单 + 改状态，返回一个确认页
//
// 点击跳转的目标**只从 token 里解**，绝不接受请求参数 —— 否则这个端点就是
// 「可信域名 + 任意跳转」的开放重定向，会被用来伪装钓鱼链接。
package mailhttp

import (
	"html"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	mailcontract "go_wp/internal/module/mail/contract"
	mailmodel "go_wp/internal/module/mail/model"
)

// trackingHandle 追踪端点处理器。
type trackingHandle struct {
	svc mailcontract.TrackingService
}

// NewTrackingHandle 构造。
func NewTrackingHandle(svc mailcontract.TrackingService) *trackingHandle {
	return &trackingHandle{svc: svc}
}

// 1×1 透明 GIF（43 字节，标准最小透明像素）。
var transparentGIF = []byte{
	0x47, 0x49, 0x46, 0x38, 0x39, 0x61, 0x01, 0x00, 0x01, 0x00, 0x80, 0x00, 0x00,
	0xff, 0xff, 0xff, 0x00, 0x00, 0x00, 0x21, 0xf9, 0x04, 0x01, 0x00, 0x00, 0x00,
	0x00, 0x2c, 0x00, 0x00, 0x00, 0x00, 0x01, 0x00, 0x01, 0x00, 0x00, 0x02, 0x02,
	0x44, 0x01, 0x00, 0x3b,
}

// Open 打开追踪：无论 token 是否有效都返回像素。
//
// 无效 token 也返回 200 + 像素，不返回错误页 —— 收件人打开邮件时不该看到一个报错图，
// 而且返回差异会向外部泄露「这个 token 是否有效」。
func (h *trackingHandle) Open(c *gin.Context) {
	token := strings.TrimSuffix(c.Param("token"), ".gif")
	if p, err := h.svc.ParseTrackToken(token); err == nil {
		h.svc.RecordTrackEvent(c.Request.Context(), p, mailmodel.EventTypeOpen, c.ClientIP(), c.Request.UserAgent())
	}
	c.Header("Cache-Control", "no-store, no-cache, must-revalidate, private")
	c.Data(http.StatusOK, "image/gif", transparentGIF)
}

// Click 点击追踪：验签 → 记录 → 302 跳转。
func (h *trackingHandle) Click(c *gin.Context) {
	p, err := h.svc.ParseTrackToken(c.Param("token"))
	if err != nil || strings.TrimSpace(p.URL) == "" {
		// 无效 token 不跳转（跳去任意地方等于开放重定向），回一个中性提示。
		c.String(http.StatusBadRequest, "链接无效或已过期")
		return
	}
	h.svc.RecordTrackEvent(c.Request.Context(), p, mailmodel.EventTypeClick, c.ClientIP(), c.Request.UserAgent())
	c.Redirect(http.StatusFound, p.URL)
}

// Unsubscribe 一键退订（无需登录）。
//
// 反垃圾邮件法要求退订足够简单，所以这里不校验登录态与 csrf；
// 安全性由「签名 token 只能由我们签发」保证，且退订是幂等的。
func (h *trackingHandle) Unsubscribe(c *gin.Context) {
	email, err := h.svc.UnsubscribeByToken(c.Request.Context(), c.Param("token"), c.ClientIP(), c.Request.UserAgent())
	if err != nil {
		c.String(http.StatusBadRequest, "退订链接无效或已过期")
		return
	}
	c.Header("Content-Type", "text/html; charset=utf-8")
	// 用双引号字符串而不是反引号：正文里要插入邮箱（用户可控），必须转义。
	c.String(http.StatusOK, "<!doctype html><html lang=\"zh-CN\"><head><meta charset=\"utf-8\">"+
		"<meta name=\"viewport\" content=\"width=device-width,initial-scale=1\">"+
		"<title>已退订</title></head><body style=\"font-family:system-ui,sans-serif;max-width:520px;margin:80px auto;padding:0 20px;line-height:1.7\">"+
		"<h2>已退订</h2><p>"+html.EscapeString(email)+" 不会再收到我们的营销邮件。</p>"+
		"<p>事务类邮件（如密码重置、订单通知）不受影响。</p>"+
		"</body></html>")
}

// SetupTrackingRoutes 挂载追踪路由（公开）。
func SetupTrackingRoutes(router *gin.Engine, svc mailcontract.TrackingService) {
	if router == nil || svc == nil {
		return
	}
	h := NewTrackingHandle(svc)
	router.GET("/_t/o/:token", h.Open)
	router.GET("/_t/c/:token", h.Click)
	router.GET("/_t/u/:token", h.Unsubscribe)
}
