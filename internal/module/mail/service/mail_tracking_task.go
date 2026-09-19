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
		// 事件行与联系人「最近活跃」**同事务**：分开提交时「事件记了、活跃时间没更新」
		// 会让报表与列表对不上（AGENTS.md「写操作的事务与回滚」）。
		if err := m.Transaction(ctx, func(tx *gorm.DB) error {
			if cerr := m.CreateEventTx(ctx, tx, e); cerr != nil {
				return cerr
			}
			// 联系人活跃时间：退订等状态变更已在端点里做过，这里只更新「最近活跃」。
			return m.UpdateContactFieldsTx(ctx, tx, p.ContactID, map[string]any{"last_activity_at": time.Now()})
		}); err != nil {
			return err
		}

		// 触发自动化（#38 P3）：打开 / 点击是**逐条**触发的 —— 事件量级远小于导入，
		// 且「打开了邮件」这类信号的价值就在于实时（等一分钟再发下一条就没意义了）。
		// 触发失败不影响事件落库（fireTrigger 内部吞错）。
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
