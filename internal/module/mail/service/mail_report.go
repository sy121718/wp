package mailservice

// mail_report.go — 活动报表（issue #38 P1）。

import (
	"context"
	"errors"
	"math"
	"time"

	maildto "go_wp/internal/module/mail/dto"
	mailenums "go_wp/internal/module/mail/enums"
	mailmodel "go_wp/internal/module/mail/model"
)

// CampaignReport 生成活动报表。
//
// 两条口径必须分清楚，否则数字会互相矛盾：
//
//	· **去重人数**（opened / clicked）—— 率的分母分子都用它；
//	· **事件次数**（openEvents / clickEvents）—— 用于人均次数这类指标。
//
// 用次数当分子会算出超过 100% 的打开率。
func (s *Service) CampaignReport(ctx context.Context, campaignID uint64, page, pageSize int) (res *maildto.CampaignReport, err error) {
	if campaignID == 0 {
		return nil, errors.New(mailenums.ErrInvalidParam)
	}
	c, err := s.m.GetCampaign(ctx, campaignID)
	if err != nil {
		return nil, errors.New(mailenums.ErrCampaignNotFound)
	}
	if page <= 0 {
		page = 1
	}
	if pageSize <= 0 || pageSize > 200 {
		pageSize = 20
	}

	res = &maildto.CampaignReport{
		Campaign: *campaignItemOf(c),
		Sent:     c.SentCount,
		Failed:   c.FailedCount,
		Target:   c.TotalCount,
	}

	types := []struct {
		eventType string
		dst       *int64
	}{
		{mailmodel.EventTypeOpen, &res.Opened},
		{mailmodel.EventTypeClick, &res.Clicked},
		{mailmodel.EventTypeUnsubscribe, &res.Unsubscribed},
		{mailmodel.EventTypeComplaint, &res.Complained},
		{mailmodel.EventTypeBounce, &res.Bounced},
	}
	for _, t := range types {
		if n, cerr := s.m.CountDistinctContactsByEvent(ctx, campaignID, t.eventType); cerr == nil {
			*t.dst = n
		}
	}
	counts, _ := s.m.CountEventsByType(ctx, campaignID)
	res.OpenEvents = counts[mailmodel.EventTypeOpen]
	res.ClickEvents = counts[mailmodel.EventTypeClick]

	// 率的分母用「送达人数」= 目标 - 失败。用目标人数当分母会把失败的人也当成没打开，
	// 让打开率凭空偏低（退信的人本来就没机会打开）。
	delivered := res.Target - res.Failed
	if delivered > 0 {
		res.OpenRate = round1(float64(res.Opened) / float64(delivered) * 100)
		res.ClickRate = round1(float64(res.Clicked) / float64(delivered) * 100)
	}

	links, lerr := s.m.ClickRanking(ctx, campaignID, 20)
	if lerr == nil {
		res.Links = make([]maildto.LinkStat, 0, len(links))
		for _, l := range links {
			res.Links = append(res.Links, maildto.LinkStat{URL: l.URL, Total: l.Total, Contacts: l.Contacts})
		}
	}

	rows, total, rerr := s.m.ListRecipients(ctx, campaignID, (page-1)*pageSize, pageSize)
	if rerr == nil {
		res.Total = total
		res.Recipients = make([]maildto.RecipientItem, 0, len(rows))
		for _, r := range rows {
			item := maildto.RecipientItem{
				ContactID: r.ContactID,
				Email:     r.Email,
				Status:    r.Status,
				Opened:    r.Opened,
				Clicked:   r.Clicked,
			}
			if r.Name != nil {
				item.Name = *r.Name
			}
			if r.SentAt != nil {
				item.SentAt = r.SentAt.Format(time.RFC3339)
			}
			if r.ErrorKind != nil {
				item.ErrorKind = *r.ErrorKind
			}
			res.Recipients = append(res.Recipients, item)
		}
	}
	return res, nil
}

func round1(v float64) float64 {
	return math.Round(v*10) / 10
}
