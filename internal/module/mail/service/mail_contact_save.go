package mailservice

// mail_contact_save.go — 联系人的新建 / 编辑 / 删除。
//
// 为什么单独一个文件：这三件事与「列表 + 改状态」（mail_contact.go）、
// 「批量打标签」（mail_contact_tag.go）的入口不同、失败形态不同，但共享同一组边界：
// 邮箱归一化、抑制名单连带、状态与抑制名单同事务。边界写在下面的注释里，
// 复制到别处就会分叉。

import (
	"context"
	"errors"
	"strings"
	"time"

	"gorm.io/gorm"

	maildto "go_wp/internal/module/mail/dto"
	mailenums "go_wp/internal/module/mail/enums"
	mailmodel "go_wp/internal/module/mail/model"
)

// CreateContact 新建联系人（ID = 0 的保存语义）。
//
// 三条边界在这里定死：
//
//	· 邮箱一律走 normalizeEmail —— 非法地址进了库也发不出去，还会让日后的导入报告里
//	  冒出「这个地址格式非法」而没人知道是谁塞进来的；
//	· 重复邮箱返回**可读错误**（ErrContactEmailExists）：唯一索引是表达式索引 lower(email)，
//	  撞车时 PG 抛的是 23505 原文（带索引名），既不能给运营看，也不能当 500。
//	  先查一次是为了给出可读文案，事务内再兜一次并发撞车（isContactEmailConflict）；
//	· 状态缺省 pending：没有同意证据的人不进可发名单（与导入路径同一口径）。
func (s *Service) CreateContact(ctx context.Context, req *maildto.SaveContactReq) (id uint64, err error) {
	if req == nil {
		return 0, errors.New(mailenums.ErrInvalidParam)
	}
	email, err := normalizeContactEmail(req.Email)
	if err != nil {
		return 0, err
	}
	status := strings.TrimSpace(req.Status)
	if status == "" {
		status = mailmodel.ContactStatusPending
	} else if !contactStatusValid(status) {
		return 0, errors.New(mailenums.ErrInvalidParam)
	}
	if _, gerr := s.m.GetContactByEmail(ctx, email); gerr == nil {
		return 0, errors.New(mailenums.ErrContactEmailExists)
	} else if !errors.Is(gerr, gorm.ErrRecordNotFound) {
		return 0, gerr
	}
	tags := normalizeTagList(req.Tags)
	e := &mailmodel.MailContactEntity{
		Email:  email,
		Source: contactSourceOr(req.Source),
		Status: status,
		Tags:   mailmodel.StringArray(tags),
	}
	if name := strings.TrimSpace(req.Name); name != "" {
		e.Name = strPtr(name)
	}
	if src := strings.TrimSpace(req.ConsentSource); src != "" {
		e.ConsentSource = strPtr(src)
	}

	// 新建 + 状态副作用（订阅态写 subscribed_at / 终态进抑制名单）是两处写，必须同事务。
	// 抑制名单的判断也在事务内：这个地址已经在名单里，却把状态写成「已订阅」，
	// 列表说的就是假话 —— 发信时照样被名单拦下（见 applyContactStatusTx）。
	now := time.Now()
	if err = s.m.Transaction(ctx, func(tx *gorm.DB) error {
		sup, serr := s.m.FindSuppressionByEmailTx(ctx, tx, email)
		if serr == nil {
			status = contactStatusForSuppression(sup.Reason)
		} else if !errors.Is(serr, gorm.ErrRecordNotFound) {
			return serr
		}
		e.Status = status
		// 判据是**最终状态**（上面那步可能把 subscribed 降级成抑制终态），不是提交上来的状态：
		// 命中抑制名单的人不该因为「没写同意来源」被拒 —— 他本来就不进可发名单。
		// 这条底线与导入路径的 ConsentDeclared 是同一件事的两个入口：那边靠勾选强制，
		// 这边只能由服务端强制（页面文案承诺过，不执行就是文案在撒谎）。
		if status == mailmodel.ContactStatusSubscribed && strings.TrimSpace(req.ConsentSource) == "" {
			return errors.New(mailenums.ErrConsentSourceRequired)
		}
		if cerr := s.m.CreateContact(ctx, e); cerr != nil {
			if isContactEmailConflict(cerr) {
				return errors.New(mailenums.ErrContactEmailExists)
			}
			return cerr
		}
		id = e.ID
		return s.applyContactStatusTx(ctx, tx, e.ID, email, status, req.ConsentSource, now)
	}); err != nil {
		return 0, err
	}

	// 两个触发**都在事务外**：建实例、可能入队发信是外部副作用，不属于本次写入的原子范围。
	// 「新建即订阅」按「从非订阅变为订阅」处理（此前状态视为不存在）——否则手工加进来的
	// 订阅者永远拿不到欢迎流程，而同一批人在导入路径里是能拿到的。
	s.fireContactSubscribedIfChanged(ctx, id, "", status)
	s.fireContactTagsAdded(ctx, id, nil, tags)
	return id, nil
}

// UpdateContact 编辑联系人（ID > 0 的保存语义）。
//
// 邮箱是抑制名单的**关联键**（mail_suppressions 只有 email，没有 contact_id），
// 所以「改邮箱」这件事必须显式表态，本实现选的是：
//
//	· **不搬抑制记录** —— 把「这个地址说过不要再发」的记录跟着搬到新地址，等于让退订者
//	  换个地址继续收，正是反垃圾邮件规则要禁止的事（与「后台点了退订却还能被发出去」同一根）；
//	· 反过来，新地址若已经在抑制名单里，就把这个联系人落到名单原因对应的**终态** ——
//	  否则列表上写着「已订阅」，发信时照样被名单拦下，运营会以为系统坏了；
//	· 旧地址的抑制记录原样留在名单里：那是**地址**的历史事实，与哪一行联系人无关。
//
// 状态若不改（表单没提交或与原值相同）不写状态列；要改则复用 UpdateContactStatus 的
// 内部实现（applyContactStatusTx），状态字段与抑制名单永远同一事务。
func (s *Service) UpdateContact(ctx context.Context, req *maildto.SaveContactReq) (err error) {
	if req == nil || req.ID == 0 {
		return errors.New(mailenums.ErrInvalidParam)
	}
	email, err := normalizeContactEmail(req.Email)
	if err != nil {
		return err
	}
	status := strings.TrimSpace(req.Status)
	if status != "" && !contactStatusValid(status) {
		return errors.New(mailenums.ErrInvalidParam)
	}
	row, err := s.m.GetContact(ctx, req.ID)
	if err != nil {
		return errors.New(mailenums.ErrContactNotFound)
	}
	emailChanged := !strings.EqualFold(strings.TrimSpace(row.Email), email)
	if emailChanged {
		other, gerr := s.m.GetContactByEmail(ctx, email)
		switch {
		case gerr == nil && other.ID != req.ID:
			return errors.New(mailenums.ErrContactEmailExists)
		case gerr != nil && !errors.Is(gerr, gorm.ErrRecordNotFound):
			return gerr
		}
	}
	tags := normalizeTagList(req.Tags)
	now := time.Now()
	fields := map[string]any{
		"email": email,
		// 姓名留空 = 清空（表单预填了原值，用户删掉它就是要清掉）。
		"name":        strPtr(strings.TrimSpace(req.Name)),
		"tags":        mailmodel.StringArray(tags),
		"update_time": now,
	}
	// 来源留空时保持原值：来源是事实记录，不该因为这次没在表单里填就被抹掉。
	if src := strings.TrimSpace(req.Source); src != "" {
		fields["source"] = src
	}
	if cs := strings.TrimSpace(req.ConsentSource); cs != "" {
		fields["consent_source"] = cs
	}

	nextStatus := status
	if err = s.m.Transaction(ctx, func(tx *gorm.DB) error {
		if emailChanged {
			sup, serr := s.m.FindSuppressionByEmailTx(ctx, tx, email)
			switch {
			case serr == nil:
				nextStatus = contactStatusForSuppression(sup.Reason)
			case errors.Is(serr, gorm.ErrRecordNotFound):
				if nextStatus == "" {
					nextStatus = row.Status
				}
			default:
				return serr
			}
		}
		// 置为「已订阅」必须写清同意来源（合规留痕）。判据取**最终**状态与**最终**来源：
		//   · 最终状态：表单没提交状态时就是库里原状态；改邮箱撞抑制名单时上面已把它降级成终态，
		//     那种情况不该要求来源（他进不了可发名单）；
		//   · 最终来源：本次提交的来源，没提交则沿用库里已有的 —— 否则「只改姓名」会被无谓挡住。
		finalStatus := nextStatus
		if finalStatus == "" {
			finalStatus = row.Status
		}
		finalConsent := strings.TrimSpace(req.ConsentSource)
		if finalConsent == "" && row.ConsentSource != nil {
			finalConsent = strings.TrimSpace(*row.ConsentSource)
		}
		if finalStatus == mailmodel.ContactStatusSubscribed && finalConsent == "" {
			return errors.New(mailenums.ErrConsentSourceRequired)
		}
		if uerr := s.m.UpdateContactFieldsTx(ctx, tx, req.ID, fields); uerr != nil {
			if isContactEmailConflict(uerr) {
				return errors.New(mailenums.ErrContactEmailExists)
			}
			return uerr
		}
		if nextStatus != "" && nextStatus != row.Status {
			return s.applyContactStatusTx(ctx, tx, req.ID, email, nextStatus, req.ConsentSource, now)
		}
		return nil
	}); err != nil {
		return err
	}

	if nextStatus == "" {
		nextStatus = row.Status
	}
	s.fireContactSubscribedIfChanged(ctx, req.ID, row.Status, nextStatus)
	s.fireContactTagsAdded(ctx, req.ID, row.Tags, tags)
	return nil
}

// DeleteContacts 删除联系人（单条与批量同一条路径）。
//
// **只删 mail_contacts 行，绝不碰 mail_suppressions**：抑制名单是地址级的合规事实
// （这个地址说过「不要再发」）。删联系人时顺手删掉抑制记录，下次导入名单就会把退订者
// 复活 —— 一个退订过的人重新收到群发，是这类系统里最贵的缺陷（投诉 + 域名声誉）。
// 误判需要放行时走抑制名单页的单独入口（DeleteSuppression），不是这里。
//
// 返回实际删除行数：调用方据此如实说明「点选的 id 里有几个已经不存在了」。
func (s *Service) DeleteContacts(ctx context.Context, req *maildto.DeleteContactsReq) (deleted int64, err error) {
	if req == nil || len(req.IDs) == 0 {
		return 0, errors.New(mailenums.ErrInvalidParam)
	}
	// 批删是一条语句（DeleteContactsByIDsTx）而不是循环单条：循环会让「删了 3 个、
	// 第 4 个报错」留下半截状态，调用方只能把整批当失败，重试时前 3 条已不存在。
	if err = s.m.Transaction(ctx, func(tx *gorm.DB) error {
		var derr error
		deleted, derr = s.m.DeleteContactsByIDsTx(ctx, tx, req.IDs)
		return derr
	}); err != nil {
		return 0, err
	}
	return deleted, nil
}

// normalizeContactEmail 归一化并校验邮箱，失败时给出可读原因。
//
// 空与非法分开报：空是「忘了填」，非法是「填错了」——合成一句会让运营反复试同一个错。
func normalizeContactEmail(raw string) (string, error) {
	email, ok := normalizeEmail(raw)
	if ok {
		return email, nil
	}
	if strings.TrimSpace(raw) == "" {
		return "", errors.New(mailenums.ErrEmailRequired)
	}
	return "", errors.New(mailenums.ErrEmailInvalid)
}

// contactSourceOr 来源缺省（手工新建的联系人来源就是 manual）。
func contactSourceOr(raw string) string {
	if v := strings.TrimSpace(raw); v != "" {
		return v
	}
	return mailmodel.ContactSourceManual
}

// fireContactTagsAdded 对本次**真正新增**的标签触发 tag_added（事务外）。
//
// 口径与导入路径一致（见 mail_contact_import.go 的 tagAdds / OnTagsAddedBatch）：
// 导入会触发、这里不触发的话，同一个动作走两条入口只有一条会发信 —— 静默不一致，
// 而运营的预期是「打上这个标签的人就会进那条流程」。
// 差集用 diffTags（精确匹配、大小写敏感），与按标签筛人群的口径相同。
func (s *Service) fireContactTagsAdded(ctx context.Context, contactID uint64, before, after []string) {
	for _, tag := range diffTags(before, after) {
		s.OnTagsAddedBatch(ctx, tag, []uint64{contactID})
	}
}
