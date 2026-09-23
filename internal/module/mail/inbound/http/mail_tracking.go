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
	mailenums "go_wp/internal/module/mail/enums"
	mailmodel "go_wp/internal/module/mail/model"
	r "go_wp/pkg/response"
)

// 访客面文案的**中文兜底**（词条缺失 / i18n 未初始化时 pkg/i18n 落到这里）。
//
// 它们与迁移 410 的词条是同一句话的两份：词条是真相来源（后台可改），这里是兜底 ——
// 与 shell.PageInternalText 的 (key, "系统内部错误，请稍后重试") 同一取舍。
// 为什么不能兜底成 key：后台页面上出现裸 key 有人会来报，而这张页面只有收件人看见。
const (
	mailTrackLinkInvalidText       = "链接无效或已过期"
	mailUnsubscribeLinkInvalidText = "退订链接无效或已过期"
	mailUnsubscribeDoneTitleText   = "已退订"
	mailUnsubscribeDoneBodyText    = "%s 不会再收到我们的营销邮件。"
	mailUnsubscribeDoneNoteText    = "事务类邮件（如密码重置、订单通知）不受影响。"
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
		//
		// 形态不变（400 + 一句短句）：这是**访客**端点，没有后台壳也没有可回归的列表页，
		// 303 + ?err= 或 JSON 都无处可去。收口的是文案来源 —— 按请求语言取词条，
		// 英文收件人不再拿到一句中文。
		mailVisitorLog(c, err, "邮件点击链路处理失败")
		c.String(http.StatusBadRequest,
			mailVisitorText(c, mailenums.ErrTrackLinkInvalid, mailTrackLinkInvalidText))
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
		// 失败原因（token 无效 / 联系人查不到 / 抑制名单写失败）一律不外发：
		// 对收件人说「联系人不存在」既没有用处，也把系统内部结构讲给了外部 ——
		// service 那边的原文正是中文业务句与驱动原文两种都有。
		// 但原文必须进日志（这一条按 Error 记）：退订写库失败是真需要有人看的故障。
		mailVisitorLogError(c, err, "邮件退订链路处理失败")
		c.String(http.StatusBadRequest,
			mailVisitorText(c, mailenums.ErrUnsubscribeLinkInvalid, mailUnsubscribeLinkInvalidText))
		return
	}
	c.Header("Content-Type", "text/html; charset=utf-8")
	c.String(http.StatusOK, unsubscribeDonePage(c, email))
}

// unsubscribeDonePage 退订成功页：一张自带样式的整页 HTML（不依赖后台壳、不查库、不带脚本）。
//
// 三句文案与 lang 属性都按当前请求语言取 —— 这是收件人这次点击唯一的反馈，
// 固定写 zh-CN 会让浏览器按中文断行 / 选字体 / 朗读（英文收件人看到的是全中文页面）。
//
// 为什么用 strings.ReplaceAll 而不是 fmt.Sprintf 填邮箱：这里只是「把邮箱放进一句话」，
// 不需要 Go 的格式协议 —— 词条若被写进 %d 之类协议外占位符，Sprintf 会把
// "%!d(MISSING)" 摆到收件人面前（与 adminBulkTextOf 的 HasStringPlaceholdersOnly 同一顾虑，
// 这里的取法更省：连协议都不需要，直接换字面）。
//
// 三处文案都经 html.EscapeString：**词条是后台可编辑的数据**（sys_i18n），
// 直接拼进 HTML 就是一条存储型注入面；邮箱来自数据库，同理。
func unsubscribeDonePage(c *gin.Context, email string) string {
	lang := r.RequestLanguage(c)
	title := html.EscapeString(mailVisitorText(c, mailenums.MsgUnsubscribeDoneTitle, mailUnsubscribeDoneTitleText))
	body := strings.ReplaceAll(
		html.EscapeString(mailVisitorText(c, mailenums.MsgUnsubscribeDoneBody, mailUnsubscribeDoneBodyText)),
		"%s", html.EscapeString(email))
	note := html.EscapeString(mailVisitorText(c, mailenums.MsgUnsubscribeDoneNote, mailUnsubscribeDoneNoteText))

	return "<!doctype html><html lang=\"" + html.EscapeString(lang) + "\"><head><meta charset=\"utf-8\">" +
		"<meta name=\"viewport\" content=\"width=device-width,initial-scale=1\">" +
		"<title>" + title + "</title></head><body style=\"font-family:system-ui,sans-serif;max-width:520px;margin:80px auto;padding:0 20px;line-height:1.7\">" +
		"<h2>" + title + "</h2><p>" + body + "</p>" +
		"<p>" + note + "</p>" +
		"</body></html>"
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
