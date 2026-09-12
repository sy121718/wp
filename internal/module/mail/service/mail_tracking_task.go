package mailservice

// mail_tracking_task.go — 追踪事件的异步落库（issue #38 P1）。
//
// 追踪端点站在访客点击路径上，必须极快：验签之后只入队，记录交给 worker。

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"gorm.io/gorm"

	mailmodel "go_wp/internal/module/mail/model"
	"go_wp/pkg/queue"
)

// TaskMailTrackEvent 追踪事件任务类型。
const TaskMailTrackEvent = "mail:track_event"

// TrackEventPayload 事件载荷。
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

// RegisterMailTrackTaskHandler 注册事件落库 handler（装配期调用）。
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
		if err := m.CreateEvent(ctx, e); err != nil {
			return err
		}
		// 联系人活跃时间：退订等状态变更已在端点里做过，这里只更新「最近活跃」。
		_ = m.UpdateContactFields(ctx, p.ContactID, map[string]any{"last_activity_at": time.Now()})
		return nil
	}
}
