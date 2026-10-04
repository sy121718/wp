package mailservice

// mail_contact.go — 联系人的查询与状态维护（导入逻辑见 mail_contact_import.go）。

import (
	"context"
	"errors"
	"strings"
	"time"

	"gorm.io/gorm"

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
	if req == nil || req.ID == 0 || !contactStatusValid(req.Status) {
		return errors.New(mailenums.ErrInvalidParam)
	}
	row, err := s.m.GetContact(ctx, req.ID)
	if err != nil {
		return errors.New(mailenums.ErrContactNotFound)
	}
	// 状态变更与抑制名单是同一件事的两面，**必须同事务**（AGENTS.md「写操作的事务与回滚」）：
	// 分开提交时第二步失败会留下「后台点了退订、地址却不在抑制名单里」——换一个活动照样会发出去，
	// 这比「没点退订」更糟（联系人以为已经退订了）。退订 / 投诉 / 硬退信三种终态都进名单。
	if err = s.m.Transaction(ctx, func(tx *gorm.DB) error {
		return s.applyContactStatusTx(ctx, tx, req.ID, row.Email, req.Status, req.Note, time.Now())
	}); err != nil {
		return err
	}
	// 变为已订阅时才触发自动化（#38 P3）。判断「原来不是订阅」而不是「现在是订阅」——
	// 否则重复保存一次订阅状态就会给人再塞进一条欢迎流程。触发失败不影响状态变更。
	//
	// 触发**留在事务外**：它要建实例、可能入队发信（外部副作用），不属于本次状态写入的原子范围。
	s.fireContactSubscribedIfChanged(ctx, req.ID, row.Status, req.Status)
	return nil
}

// —— 同意状态的共享实现 ——
//
// 手工改状态（UpdateContactStatus）、编辑抽屉保存（UpdateContact）都要写「状态 + 抑制名单」，
// 但它们的入口、校验与回执完全不同。共用的部分抽在这里，**不是**为了让文件短一点：
// 两份实现的第一天就等价，第二次改（比如加一种终态）时必然分叉 ——
// 分叉的后果是某条路径点了退订却没进抑制名单，换一个活动照样发出去。

// contactStatusValid 手工可设的同意状态白名单（单条抽屉 / 批量 / 编辑抽屉三条入口共用一份）。
//
// bounced / complained 也在白名单里：单条抽屉允许人工把它们改回去（投递反馈是事实，
// 但事实可能已经过时 —— 换过邮件服务商之后原地址可能又能收了）。批量路径另有限制，
// 见 MailContactsBulkStatus。
func contactStatusValid(status string) bool {
	switch status {
	case mailmodel.ContactStatusSubscribed, mailmodel.ContactStatusPending,
		mailmodel.ContactStatusUnsubscribed, mailmodel.ContactStatusBounced,
		mailmodel.ContactStatusComplained:
		return true
	}
	return false
}

// contactStatusFields 状态变更要写的列（不含邮箱等身份列）。
func contactStatusFields(status, note string, at time.Time) map[string]any {
	fields := map[string]any{"status": status, "update_time": at}
	if status == mailmodel.ContactStatusSubscribed {
		fields["subscribed_at"] = at
		if src := strings.TrimSpace(note); src != "" {
			fields["consent_source"] = src
		}
	}
	return fields
}

// suppressionReasonForStatus 状态 → 抑制名单原因；不需要进名单的状态返回 false。
func suppressionReasonForStatus(status string) (string, bool) {
	switch status {
	case mailmodel.ContactStatusUnsubscribed:
		return mailmodel.SuppressionReasonUnsubscribe, true
	case mailmodel.ContactStatusComplained:
		return mailmodel.SuppressionReasonComplaint, true
	case mailmodel.ContactStatusBounced:
		return mailmodel.SuppressionReasonHardBounce, true
	}
	return "", false
}

// contactStatusForSuppression 抑制名单原因 → 联系人应当呈现的状态。
//
// 用于「这个地址已经在抑制名单里」的那条分支（新建联系人或改邮箱时命中）：
// 列表上写着「已订阅」而发信时被名单拦下，运营会以为系统坏了 —— 状态必须与事实一致。
// manual（人工加进名单的）按退订处理：名单的语义就是「不要再发」。
func contactStatusForSuppression(reason string) string {
	switch reason {
	case mailmodel.SuppressionReasonComplaint:
		return mailmodel.ContactStatusComplained
	case mailmodel.SuppressionReasonHardBounce:
		return mailmodel.ContactStatusBounced
	}
	return mailmodel.ContactStatusUnsubscribed
}

// applyContactStatusTx 写状态字段 + 终态进抑制名单，**调用方事务内**。
//
// 抑制记录的 Note 只在退订时写：投诉 / 硬退信是投递反馈带来的终态，
// 那句「来源备注」是人工退订留痕用的（与既有实现逐字一致）。
func (s *Service) applyContactStatusTx(ctx context.Context, tx *gorm.DB, contactID uint64, email, status, note string, at time.Time) error {
	if err := s.m.UpdateContactFieldsTx(ctx, tx, contactID, contactStatusFields(status, note, at)); err != nil {
		return err
	}
	reason, need := suppressionReasonForStatus(status)
	if !need {
		return nil
	}
	sup := &mailmodel.MailSuppressionEntity{
		Email: email, Reason: reason, Source: strPtr("manual"),
	}
	if reason == mailmodel.SuppressionReasonUnsubscribe {
		sup.Note = strPtr(note)
	}
	return s.m.AddSuppressionTx(ctx, tx, sup)
}

// fireContactSubscribedIfChanged 从非订阅变为订阅时触发自动化，**在事务外调用**。
//
// 判据是「原来不是订阅」而不是「现在是订阅」——否则重复保存一次订阅状态
// 就会给人再塞进一条欢迎流程（与 UpdateContactStatus 的既有口径一致）。
func (s *Service) fireContactSubscribedIfChanged(ctx context.Context, contactID uint64, was, now string) {
	if now != mailmodel.ContactStatusSubscribed || was == mailmodel.ContactStatusSubscribed {
		return
	}
	s.OnContactSubscribed(ctx, contactID)
}

// isContactEmailConflict 判断是不是「邮箱唯一索引撞车」。
//
// 双判据（与 ai_provider_crud.go / product_crud.go 同款）：生产连接开了 gorm 的
// TranslateError 时拿到 gorm.ErrDuplicatedKey，未翻译的连接（部分测试 fixture）拿到的是
// PG 原始 23505 文本。两条都认，否则并发撞车时运营看到的是 SQLSTATE 原文。
func isContactEmailConflict(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, gorm.ErrDuplicatedKey) {
		return true
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "23505") || strings.Contains(msg, "duplicate key value")
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
