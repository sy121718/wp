package mailservice

// mail_contact.go — 联系人的查询与状态维护（导入逻辑见 mail_contact_import.go）。

import (
	"context"
	"errors"
	"strings"
	"time"

	mailcontract "go_wp/internal/module/mail/contract"
	maildto "go_wp/internal/module/mail/dto"
	mailenums "go_wp/internal/module/mail/enums"
	mailmodel "go_wp/internal/module/mail/model"
)

// 编译期断言：本模块服务满足对外契约。
var _ mailcontract.MailService = (*Service)(nil)

// ListContacts 联系人列表（分页）。
func (s *Service) ListContacts(ctx context.Context, req *maildto.ContactFilterReq) (res *maildto.ContactListResp, err error) {
	if req == nil {
		req = &maildto.ContactFilterReq{}
	}
	page, size := req.Page, req.PageSize
	if page <= 0 {
		page = 1
	}
	if size <= 0 || size > 200 {
		size = 20
	}
	list, total, err := s.m.ListContacts(ctx, mailmodel.ContactFilter{
		Keyword: req.Keyword,
		Status:  req.Status,
		Tags:    req.Tags,
		Offset:  (page - 1) * size,
		Limit:   size,
	})
	if err != nil {
		return nil, err
	}
	res = &maildto.ContactListResp{Items: make([]maildto.ContactItem, 0, len(list)), Total: total}
	for _, e := range list {
		res.Items = append(res.Items, contactItemOf(e))
	}
	return res, nil
}

// UpdateContactStatus 后台手工改同意状态（订阅 / 退订）。
//
// 这是**人工留痕**操作，与自动回写（退信 / 投诉）走的路径不同，但落库字段一致：
// 改成 subscribed 会写 subscribed_at，改成 unsubscribed 同时进抑制名单 ——
// 后台点了退订却还能发出去，是比不点退订更糟的结果。
func (s *Service) UpdateContactStatus(ctx context.Context, req *maildto.UpdateContactStatusReq) (err error) {
	if req == nil || req.ID == 0 {
		return errors.New(mailenums.ErrInvalidParam)
	}
	switch req.Status {
	case mailmodel.ContactStatusSubscribed, mailmodel.ContactStatusPending,
		mailmodel.ContactStatusUnsubscribed, mailmodel.ContactStatusBounced,
		mailmodel.ContactStatusComplained:
	default:
		return errors.New(mailenums.ErrInvalidParam)
	}
	row, err := s.m.GetContact(ctx, req.ID)
	if err != nil {
		return errors.New(mailenums.ErrContactNotFound)
	}
	now := time.Now()
	fields := map[string]any{"status": req.Status, "update_time": now}
	if req.Status == mailmodel.ContactStatusSubscribed {
		fields["subscribed_at"] = now
		if src := strings.TrimSpace(req.Note); src != "" {
			fields["consent_source"] = src
		}
	}
	if err = s.m.UpdateContactFields(ctx, req.ID, fields); err != nil {
		return err
	}
	// 变为已订阅时才触发自动化（#38 P3）。判断「原来不是订阅」而不是「现在是订阅」——
	// 否则重复保存一次订阅状态就会给人再塞进一条欢迎流程。触发失败不影响状态变更。
	if req.Status == mailmodel.ContactStatusSubscribed && row.Status != mailmodel.ContactStatusSubscribed {
		s.OnContactSubscribed(ctx, req.ID)
	}

	// 退订 / 投诉 / 硬退信一律进抑制名单（发送前必查），避免换个活动又发出去。
	switch req.Status {
	case mailmodel.ContactStatusUnsubscribed:
		return s.m.AddSuppression(ctx, &mailmodel.MailSuppressionEntity{
			Email: row.Email, Reason: mailmodel.SuppressionReasonUnsubscribe, Source: strPtr("manual"),
			Note: strPtr(req.Note),
		})
	case mailmodel.ContactStatusComplained:
		return s.m.AddSuppression(ctx, &mailmodel.MailSuppressionEntity{
			Email: row.Email, Reason: mailmodel.SuppressionReasonComplaint, Source: strPtr("manual"),
		})
	case mailmodel.ContactStatusBounced:
		return s.m.AddSuppression(ctx, &mailmodel.MailSuppressionEntity{
			Email: row.Email, Reason: mailmodel.SuppressionReasonHardBounce, Source: strPtr("manual"),
		})
	}
	return nil
}

func contactItemOf(e *mailmodel.MailContactEntity) maildto.ContactItem {
	item := maildto.ContactItem{
		ID:     e.ID,
		Email:  e.Email,
		Source: e.Source,
		Status: e.Status,
		Tags:   e.Tags,
	}
	if e.Name != nil {
		item.Name = *e.Name
	}
	if e.UserID != nil {
		item.UserID = *e.UserID
	}
	if e.ConsentSource != nil {
		item.ConsentSource = *e.ConsentSource
	}
	if e.SubscribedAt != nil {
		item.SubscribedAt = e.SubscribedAt.Format(time.RFC3339)
	}
	if e.CreateTime != nil {
		item.CreateTime = e.CreateTime.Format(time.RFC3339)
	}
	return item
}
