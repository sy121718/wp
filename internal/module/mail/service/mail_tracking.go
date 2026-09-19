package mailservice

// mail_tracking.go — 营销追踪：打开像素 / 点击跳转 / 一键退订（issue #38 P1）。
//
// ## token 用签名而不是数据库表

// 每个 token 是一段自包含的载荷 + HMAC 签名，不落库：
//
//	payload = base64url(logID:campaignID:contactID[:目标URL])
//	token   = payload + "." + base64url(HMAC-SHA256(payload, secret))[:22]
//
// 好处是不必为每封邮件写一行 token 记录（群发一万封就是一万行），点击路径上也不必查库；
// 代价是 token 不能单独撤销 —— 所以**退订与抑制名单始终以数据库为准**，
// 端点每次都查抑制名单，token 有效期问题不会导致「退订了还继续发」。
//
// ## 点击目标只从 token 里解，绝不接受请求参数
//
// 这是防**开放重定向**的关键：若做成 `/_t/c?u=<任意URL>`，攻击者就能用「可信域名 + 任意跳转」
// 伪装钓鱼链接。目标 URL 在发信时就签进 token，端点只还原签名过的那个值。
//
// ## 事件记录一律异步

// 追踪端点站在访客点击路径上，同步写库会把它变成整条链路最慢的一环。
// 端点只做「验签 → 入队 → 返回」，记录交给 worker。

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"strconv"
	"strings"
	"time"

	"gorm.io/gorm"

	"go_wp/pkg/queue"

	maildto "go_wp/internal/module/mail/dto"
	mailmodel "go_wp/internal/module/mail/model"
)

// 追踪端点路径（公开路由，见 inbound/http/mail_tracking.go）。
const (
	TrackOpenPath        = "/_t/o/"
	TrackClickPath       = "/_t/c/"
	TrackUnsubscribePath = "/_t/u/"
)

// trackTokenMaxAge token 有效期（退订与抑制仍以数据库为准，这里只是限制链接可被复用的时间）。
const trackTokenMaxAge = 180 * 24 * time.Hour

// SignTrackToken 签发追踪 token（发信时调用）。
func (s *Service) SignTrackToken(p maildto.TrackPayload) (string, error) {
	if strings.TrimSpace(s.cipherSecret) == "" {
		return "", errors.New("未配置加密密钥，无法签发追踪 token")
	}
	parts := []string{
		strconv.FormatUint(p.LogID, 10),
		strconv.FormatUint(p.CampaignID, 10),
		strconv.FormatUint(p.ContactID, 10),
		strconv.FormatInt(time.Now().Add(trackTokenMaxAge).Unix(), 10),
	}
	payload := strings.Join(parts, ":")
	if p.URL != "" {
		payload += ":" + p.URL
	}
	b64 := base64.RawURLEncoding.EncodeToString([]byte(payload))
	return b64 + "." + s.trackSignature(b64), nil
}

// ParseTrackToken 校验并还原载荷。签名不符 / 结构不对 / 过期一律返回错误。
func (s *Service) ParseTrackToken(token string) (p maildto.TrackPayload, err error) {
	if strings.TrimSpace(s.cipherSecret) == "" {
		return p, errors.New("未配置加密密钥，无法校验追踪 token")
	}
	dot := strings.LastIndex(token, ".")
	if dot <= 0 {
		return p, errors.New("追踪 token 格式不正确")
	}
	b64, sig := token[:dot], token[dot+1:]
	if !hmac.Equal([]byte(sig), []byte(s.trackSignature(b64))) {
		return p, errors.New("追踪 token 签名校验失败")
	}
	raw, derr := base64.RawURLEncoding.DecodeString(b64)
	if derr != nil {
		return p, errors.New("追踪 token 解码失败")
	}
	segs := strings.SplitN(string(raw), ":", 5)
	if len(segs) < 4 {
		return p, errors.New("追踪 token 载荷不完整")
	}
	p.LogID, _ = strconv.ParseUint(segs[0], 10, 64)
	p.CampaignID, _ = strconv.ParseUint(segs[1], 10, 64)
	p.ContactID, _ = strconv.ParseUint(segs[2], 10, 64)
	exp, _ := strconv.ParseInt(segs[3], 10, 64)
	if exp > 0 && time.Now().Unix() > exp {
		return p, errors.New("追踪链接已过期")
	}
	if len(segs) == 5 {
		p.URL = segs[4]
	}
	return p, nil
}

// trackSignature HMAC-SHA256(payload) 的 base64url 前 22 字符。
func (s *Service) trackSignature(b64 string) string {
	mac := hmac.New(sha256.New, []byte(s.cipherSecret))
	mac.Write([]byte(b64))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))[:22]
}

// InjectTracking 给渲染好的邮件正文注入打开像素，并把链接改写成点击追踪链接。
//
// 只处理 **HTML 正文**：纯文本正文没有图片也没有超链接，注入无处可放（这也是为什么
// 追踪数据天然偏向「会渲染 HTML 的客户端」，纯文本用户的行为统计不到）。
func (s *Service) InjectTracking(html string, p maildto.TrackPayload) (string, error) {
	if strings.TrimSpace(html) == "" {
		return html, nil
	}
	out, err := s.rewriteLinks(html, p)
	if err != nil {
		return "", err
	}
	// 打开像素：1×1 透明 GIF 的 data URI 会让客户端不发起请求，所以走端点。
	open, oerr := s.SignTrackToken(maildto.TrackPayload{LogID: p.LogID, CampaignID: p.CampaignID, ContactID: p.ContactID})
	if oerr != nil {
		return "", oerr
	}
	pixel := "<img src=\"" + TrackOpenPath + open + ".gif\" width=\"1\" height=\"1\" alt=\"\" style=\"display:none\">"
	if idx := strings.LastIndex(strings.ToLower(out), "</body>"); idx >= 0 {
		return out[:idx] + pixel + out[idx:], nil
	}
	return out + pixel, nil
}

// rewriteLinks 把 <a href="..."> 改写成点击追踪链接。
//
// 用朴素扫描而不是 HTML 解析器：邮件正文是我们自己渲染出来的受控 HTML，
// 结构简单可预期；引第三方解析器会把整棵 DOM 拉进来，收益不成比例。
// 只处理双引号与单引号两种 href 写法，跳过 mailto/tel/锚点/已带追踪前缀的链接。
func (s *Service) rewriteLinks(html string, p maildto.TrackPayload) (string, error) {
	const marker = `href="`
	var b strings.Builder
	rest := html
	for {
		idx := strings.Index(rest, marker)
		if idx < 0 {
			b.WriteString(rest)
			break
		}
		b.WriteString(rest[:idx+len(marker)])
		rest = rest[idx+len(marker):]
		end := strings.Index(rest, `"`)
		if end < 0 {
			b.WriteString(rest)
			break
		}
		href := rest[:end]
		rest = rest[end:]
		if !shouldTrackLink(href) {
			b.WriteString(href)
			continue
		}
		token, err := s.SignTrackToken(maildto.TrackPayload{
			LogID: p.LogID, CampaignID: p.CampaignID, ContactID: p.ContactID, URL: href,
		})
		if err != nil {
			return "", err
		}
		b.WriteString(TrackClickPath + token)
	}
	return b.String(), nil
}

// shouldTrackLink 判断链接是否值得追踪。
func shouldTrackLink(href string) bool {
	h := strings.TrimSpace(href)
	if h == "" || strings.HasPrefix(h, "#") || strings.HasPrefix(h, "{") {
		return false
	}
	low := strings.ToLower(h)
	for _, skip := range []string{"mailto:", "tel:", "sms:", "cid:"} {
		if strings.HasPrefix(low, skip) {
			return false
		}
	}
	if strings.HasPrefix(low, TrackClickPath) || strings.HasPrefix(low, TrackOpenPath) {
		return false
	}
	return strings.HasPrefix(low, "http://") || strings.HasPrefix(low, "https://")
}

// RecordTrackEvent 记录追踪事件（**异步**：端点只入队，不落库）。
func (s *Service) RecordTrackEvent(_ context.Context, p maildto.TrackPayload, eventType, ip, ua string) {
	if !queue.IsInited() {
		return
	}
	_ = trackTask.Enqueue(TrackEventPayload{
		CampaignID: p.CampaignID, ContactID: p.ContactID, LogID: p.LogID,
		EventType: eventType, URL: p.URL, IP: ip, UserAgent: ua,
	})
}

// UnsubscribeByToken 一键退订（无需登录）。
//
// 退订是**反垃圾邮件法的要求**，所以：
//
//	· 不需要登录、不需要 csrf —— 收件人在邮件客户端里点一下就该生效；
//	· 幂等 —— 重复点是同一个结果，不报错；
//	· 同时写抑制名单与联系人状态 —— 只改一边的话，换个活动又会被发出去。
func (s *Service) UnsubscribeByToken(ctx context.Context, token, ip, ua string) (email string, err error) {
	p, err := s.ParseTrackToken(token)
	if err != nil {
		return "", err
	}
	var contact *mailmodel.MailContactEntity
	if p.ContactID > 0 {
		contact, err = s.m.GetContact(ctx, p.ContactID)
		if err != nil {
			return "", errors.New("联系人不存在")
		}
	}
	if contact == nil {
		return "", errors.New("退订链接缺少联系人信息")
	}
	now := time.Now()
	// 状态与抑制名单**同事务**：只改一边的话，换个活动又会被发出去 ——
	// 「点了退订却还能被发出去」比不点退订更糟（本文件头也是这个口径）。
	if uerr := s.m.Transaction(ctx, func(tx *gorm.DB) error {
		if uerr := s.m.UpdateContactFieldsTx(ctx, tx, contact.ID, map[string]any{
			"status": mailmodel.ContactStatusUnsubscribed, "update_time": now,
		}); uerr != nil {
			return uerr
		}
		return s.m.AddSuppressionTx(ctx, tx, &mailmodel.MailSuppressionEntity{
			Email: contact.Email, Reason: mailmodel.SuppressionReasonUnsubscribe, Source: strPtr("email"),
		})
	}); uerr != nil {
		return "", uerr
	}
	// 事件记录留在事务外：它只是入队（访客点击路径不等待落库），不是本次退订的一部分。
	s.RecordTrackEvent(ctx, p, mailmodel.EventTypeUnsubscribe, ip, ua)
	return contact.Email, nil
}
