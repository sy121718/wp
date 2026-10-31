package mailservice

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	json "go_wp/pkg/jsonx"
	"strconv"
	"strings"
	"time"

	"gorm.io/gorm"

	maildto "go_wp/internal/module/mail/dto"
	mailmodel "go_wp/internal/module/mail/model"
	"go_wp/pkg/queue"
)

const (
	TrackOpenPath        = "/_t/o/"
	TrackClickPath       = "/_t/c/"
	TrackUnsubscribePath = "/_t/u/"
)

const trackTokenMaxAge = 180 * 24 * time.Hour

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

func (s *Service) trackSignature(b64 string) string {
	mac := hmac.New(sha256.New, []byte(s.cipherSecret))
	mac.Write([]byte(b64))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))[:22]
}

func (s *Service) InjectTracking(html string, p maildto.TrackPayload) (string, error) {
	if strings.TrimSpace(html) == "" {
		return html, nil
	}
	out, err := s.rewriteLinks(html, p)
	if err != nil {
		return "", err
	}

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

func (s *Service) RecordTrackEvent(_ context.Context, p maildto.TrackPayload, eventType, ip, ua string) {
	if !queue.IsInited() {
		return
	}
	_ = trackTask.Enqueue(TrackEventPayload{
		CampaignID: p.CampaignID, ContactID: p.ContactID, LogID: p.LogID,
		EventType: eventType, URL: p.URL, IP: ip, UserAgent: ua,
	})
}

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

	s.RecordTrackEvent(ctx, p, mailmodel.EventTypeUnsubscribe, ip, ua)
	return contact.Email, nil
}

const TaskMailTrackEvent = "mail:track_event"

type TrackEventPayload struct {
	CampaignID uint64 `json:"campaign_id"`
	ContactID  uint64 `json:"contact_id"`
	LogID      uint64 `json:"log_id"`
	EventType  string `json:"event_type"`
	URL        string `json:"url"`
	IP         string `json:"ip"`
	UserAgent  string `json:"user_agent"`
}

var trackTask = queue.NewTask(TaskMailTrackEvent, queue.WithQueue("default"), queue.WithMaxRetry(2))

func RegisterMailTrackTaskHandler(db *gorm.DB) {
	queue.Register(TaskMailTrackEvent, handleTrackEvent(db))
}

func handleTrackEvent(db *gorm.DB) queue.Handler {
	return func(ctx context.Context, raw []byte) error {
		var p TrackEventPayload
		if err := json.Unmarshal(raw, &p); err != nil {
			return err
		}
		if p.ContactID == 0 || strings.TrimSpace(p.EventType) == "" {
			return nil
		}
		m := mailmodel.NewMailModel(db)
		e := &mailmodel.MailCampaignEventEntity{
			CampaignID: p.CampaignID,
			ContactID:  p.ContactID,
			EventType:  p.EventType,
		}
		if strings.TrimSpace(p.URL) != "" {
			u := p.URL
			e.URL = &u
		}
		if strings.TrimSpace(p.IP) != "" {
			ip := p.IP
			e.IP = &ip
		}
		if ua := p.UserAgent; ua != "" {
			if len(ua) > 255 {
				ua = ua[:255]
			}
			e.UserAgent = &ua
		}

		if err := m.Transaction(ctx, func(tx *gorm.DB) error {
			if cerr := m.CreateEventTx(ctx, tx, e); cerr != nil {
				return cerr
			}

			return m.UpdateContactFieldsTx(ctx, tx, p.ContactID, map[string]any{"last_activity_at": time.Now()})
		}); err != nil {
			return err
		}

		svc := NewService(m)
		switch p.EventType {
		case mailmodel.EventTypeOpen:
			svc.OnEmailOpened(ctx, p.ContactID)
		case mailmodel.EventTypeClick:
			svc.OnEmailClicked(ctx, p.ContactID)
		}
		return nil
	}
}
