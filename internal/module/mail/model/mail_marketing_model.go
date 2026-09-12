package model

// mail_marketing_model.go — 联系人 / 群发活动 / 事件（issue #37 营销域）。
//
// 并发友好的点（用户要求把 Go 的并发优势用上）：
//   · BatchUpsertContacts —— 导入用批量 upsert（一条语句一批，而不是逐行 insert）；
//   · IncrCampaignCounts —— 计数用原子递增，多个投递 worker 同时回写不会互相覆盖；
//   · ListSubscribedByTags —— 投递目标分批取（游标式），大名单不必一次读进内存。

import (
	"context"
	"database/sql/driver"
	"fmt"
	"strings"
	"time"

	"gorm.io/gorm"
)

// 联系人来源。
const (
	ContactSourceSystemUser = "system_user"
	ContactSourceImport     = "import"
	ContactSourceManual     = "manual"
	ContactSourceSubscribe  = "subscribe"
)

// 联系人同意状态 —— **只有 subscribed 允许发营销**。
const (
	ContactStatusPending      = "pending"
	ContactStatusSubscribed   = "subscribed"
	ContactStatusUnsubscribed = "unsubscribed"
	ContactStatusBounced      = "bounced"
	ContactStatusComplained   = "complained"
)

// 活动状态。
const (
	CampaignStatusDraft     = "draft"
	CampaignStatusScheduled = "scheduled"
	CampaignStatusSending   = "sending"
	CampaignStatusSent      = "sent"
	CampaignStatusFailed    = "failed"
	CampaignStatusCanceled  = "canceled"
)

// 事件类型。
const (
	EventTypeOpen        = "open"
	EventTypeClick       = "click"
	EventTypeBounce      = "bounce"
	EventTypeUnsubscribe = "unsubscribe"
	EventTypeComplaint   = "complaint"
)

// StringArray 是 PG text[] 的 Go 映射，自己实现 driver.Valuer / sql.Scanner。
//
// 为什么不用现成方案（两种都**实测**失败了）：
//
//	· 裸 []string：GORM 经 pgx 编码成元组字面量 ('a','b')，PG 报 malformed array literal (22P02)；
//	· pgtype.FlatArray[string]：在 pgx **原生**路径下可用，但 GORM 走 database/sql 路径，
//	  它不被识别、仍编码成 record，PG 报 "column tags is of type text[] but expression is of
//	  type record" (42804)。
//
// 所以本类型直接产出 / 解析 PG 数组字面量，不依赖 driver 的编码约定。
type StringArray []string

// Value 实现 driver.Valuer：nil 与空切片都写 '{}'（列是 NOT NULL DEFAULT '{}'）。
func (a StringArray) Value() (driver.Value, error) {
	if a == nil {
		return "{}", nil
	}
	return encodePGTextArray(a), nil
}

// Scan 实现 sql.Scanner：接受 PG 返回的数组字面量文本。
func (a *StringArray) Scan(src any) error {
	if src == nil {
		*a = StringArray{}
		return nil
	}
	var raw string
	switch v := src.(type) {
	case string:
		raw = v
	case []byte:
		raw = string(v)
	default:
		return fmt.Errorf("StringArray: 不支持的来源类型 %T", src)
	}
	parsed, err := decodePGTextArray(raw)
	if err != nil {
		return err
	}
	*a = parsed
	return nil
}

// encodePGTextArray 编码成 PG 数组字面量 {"a","b"}。
//
// 元素一律加引号并转义反斜杠与双引号 —— 标签里出现逗号、空格、引号都不会破坏结构。
func encodePGTextArray(values []string) string {
	if len(values) == 0 {
		return "{}"
	}
	parts := make([]string, 0, len(values))
	for _, v := range values {
		esc := strings.ReplaceAll(v, "\\", "\\\\")
		esc = strings.ReplaceAll(esc, "\"", "\\\"")
		parts = append(parts, "\""+esc+"\"")
	}
	return "{" + strings.Join(parts, ",") + "}"
}

// decodePGTextArray 解析 PG 数组字面量（处理引号、反斜杠转义、空数组）。
func decodePGTextArray(raw string) (StringArray, error) {
	s := strings.TrimSpace(raw)
	if s == "" || s == "{}" {
		return StringArray{}, nil
	}
	if !strings.HasPrefix(s, "{") || !strings.HasSuffix(s, "}") {
		return nil, fmt.Errorf("StringArray: 非法数组字面量 %q", raw)
	}
	body := s[1 : len(s)-1]
	out := StringArray{}
	var cur strings.Builder
	inQuote, escaped, started := false, false, false
	for _, r := range body {
		switch {
		case escaped:
			cur.WriteRune(r)
			escaped = false
		case r == '\\':
			escaped = true
		case r == '"':
			inQuote = !inQuote
			started = true
		case r == ',' && !inQuote:
			out = append(out, cur.String())
			cur.Reset()
			started = false
		default:
			cur.WriteRune(r)
			started = true
		}
	}
	if inQuote {
		return nil, fmt.Errorf("StringArray: 数组字面量引号未闭合: %q", raw)
	}
	if started || cur.Len() > 0 {
		out = append(out, cur.String())
	}
	return out, nil
}

// MailContactEntity 对应 mail_contacts 表。
type MailContactEntity struct {
	ID            uint64     `gorm:"column:id;primaryKey"`
	Email         string     `gorm:"column:email;type:varchar(254)"`
	Name          *string    `gorm:"column:name;type:varchar(128)"`
	UserID        *uint64    `gorm:"column:user_id"`
	Source        string     `gorm:"column:source;type:varchar(16)"`
	Status        string     `gorm:"column:status;type:varchar(16)"`
	SubscribedAt  *time.Time `gorm:"column:subscribed_at;type:timestamp(3)"`
	ConsentSource *string    `gorm:"column:consent_source;type:varchar(64)"`
	// Tags PG 原生 text[]，用 StringArray 映射（裸 []string 会被 pgx 编码成元组字面量，
	// 实测报 22P02；见 StringArray 的注释）。零值需注意：列是 NOT NULL DEFAULT '{}'，
	// nil 会写成 NULL 触发约束错误，所以写入前统一兜底成空数组。
	Tags           StringArray `gorm:"column:tags;type:text[]"`
	Attributes     JSONMap     `gorm:"column:attributes;type:jsonb"`
	LastActivityAt *time.Time  `gorm:"column:last_activity_at;type:timestamp(3)"`
	CreateTime     *time.Time  `gorm:"column:create_time;type:timestamp(3);autoCreateTime"`
	UpdateTime     *time.Time  `gorm:"column:update_time;type:timestamp(3)"`
}

// TableName 表名。
func (MailContactEntity) TableName() string { return "mail_contacts" }

// MailCampaignEntity 对应 mail_campaigns 表。
type MailCampaignEntity struct {
	ID          uint64      `gorm:"column:id;primaryKey"`
	Name        string      `gorm:"column:name;type:varchar(150)"`
	AccountID   uint64      `gorm:"column:account_id"`
	TemplateID  uint64      `gorm:"column:template_id"`
	TargetTags  StringArray `gorm:"column:target_tags;type:text[]"`
	Subject     string      `gorm:"column:subject;type:varchar(255)"`
	Variables   JSONMap     `gorm:"column:variables;type:jsonb"`
	Status      string      `gorm:"column:status;type:varchar(16)"`
	ScheduledAt *time.Time  `gorm:"column:scheduled_at;type:timestamp(3)"`
	StartedAt   *time.Time  `gorm:"column:started_at;type:timestamp(3)"`
	FinishedAt  *time.Time  `gorm:"column:finished_at;type:timestamp(3)"`
	TotalCount  int         `gorm:"column:total_count"`
	SentCount   int         `gorm:"column:sent_count"`
	FailedCount int         `gorm:"column:failed_count"`
	CreateBy    uint64      `gorm:"column:create_by"`
	CreateTime  *time.Time  `gorm:"column:create_time;type:timestamp(3);autoCreateTime"`
	UpdateTime  *time.Time  `gorm:"column:update_time;type:timestamp(3)"`
}

// TableName 表名。
func (MailCampaignEntity) TableName() string { return "mail_campaigns" }

// MailCampaignEventEntity 对应 mail_campaign_events 表。
type MailCampaignEventEntity struct {
	ID         uint64     `gorm:"column:id;primaryKey"`
	CampaignID uint64     `gorm:"column:campaign_id"`
	ContactID  uint64     `gorm:"column:contact_id"`
	EventType  string     `gorm:"column:event_type;type:varchar(16)"`
	URL        *string    `gorm:"column:url;type:varchar(1000)"`
	IP         *string    `gorm:"column:ip;type:varchar(50)"`
	UserAgent  *string    `gorm:"column:user_agent;type:varchar(255)"`
	CreateTime *time.Time `gorm:"column:create_time;type:timestamp(3);autoCreateTime"`
}

// TableName 表名。
func (MailCampaignEventEntity) TableName() string { return "mail_campaign_events" }

// ContactFilter 联系人筛选。
type ContactFilter struct {
	Keyword string
	// Status 空表示不过滤；营销投递固定用 ContactStatusSubscribed。
	Status string
	// Tags 同时包含（AND 语义）：用于「vip 且 华南」这种交叉筛选。
	Tags   []string
	Offset int
	Limit  int
}

// arrayLiteral 把字符串切片转成 PG 数组字面量 `{"a","b"}`。
//
// 为什么手写字面量而不引 pq.Array：本项目尚未依赖 lib/pq，而 pgx 对带 cast 的
// 字面量参数处理得最直白（`tags @> ?::text[]`），少一个依赖、行为可预期。
// 元素里的引号与反斜杠按 PG 规则转义，避免注入。
func arrayLiteral(values []string) string {
	if len(values) == 0 {
		return "{}"
	}
	parts := make([]string, 0, len(values))
	for _, v := range values {
		escaped := strings.ReplaceAll(v, "\\", "\\\\")
		escaped = strings.ReplaceAll(escaped, "\"", "\\\"")
		parts = append(parts, "\""+escaped+"\"")
	}
	return "{" + strings.Join(parts, ",") + "}"
}

// ---- 联系人 ----

// CreateContact 新建联系人。
func (m *MailModel) CreateContact(ctx context.Context, e *MailContactEntity) (err error) {
	if e.Tags == nil {
		e.Tags = StringArray{}
	}
	return m.tx(ctx).Create(e).Error
}

// GetContact 按主键取联系人。
func (m *MailModel) GetContact(ctx context.Context, id uint64) (e *MailContactEntity, err error) {
	e = &MailContactEntity{}
	err = m.tx(ctx).Where("id = ?", id).First(e).Error
	return e, err
}

// GetContactByEmail 按邮箱取（大小写不敏感）；找不到返回 gorm.ErrRecordNotFound。
func (m *MailModel) GetContactByEmail(ctx context.Context, email string) (e *MailContactEntity, err error) {
	addr := strings.TrimSpace(email)
	if addr == "" {
		return nil, gorm.ErrRecordNotFound
	}
	e = &MailContactEntity{}
	err = m.tx(ctx).Where("lower(email) = lower(?)", addr).First(e).Error
	return e, err
}

// ExistingContactIDs **批量**查已存在联系人的 id（导入时先查一次，避免 ON CONFLICT）。
//
// 为什么不用 ON CONFLICT：mail_contacts 的唯一索引是**表达式索引** lower(email)，
// 而 ON CONFLICT (email) 匹配不到它，PG 直接报 42P10（实测）。
// 先查后分两批写还有个额外好处：**能准确区分新增与更新**，导入报告不再是估算。
func (m *MailModel) ExistingContactIDs(ctx context.Context, emails []string) (ids map[string]uint64, err error) {
	ids = map[string]uint64{}
	if len(emails) == 0 {
		return ids, nil
	}
	lowered := make([]string, 0, len(emails))
	for _, e := range emails {
		if v := strings.TrimSpace(e); v != "" {
			lowered = append(lowered, strings.ToLower(v))
		}
	}
	if len(lowered) == 0 {
		return ids, nil
	}
	var rows []struct {
		ID    uint64
		Email string
	}
	err = m.tx(ctx).Model(&MailContactEntity{}).
		Select("id, lower(email) AS email").
		Where("lower(email) IN ?", lowered).
		Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	for _, r := range rows {
		ids[strings.ToLower(r.Email)] = r.ID
	}
	return ids, nil
}

// BatchInsertContacts 批量新增联系人（导入的新增那一批）。
func (m *MailModel) BatchInsertContacts(ctx context.Context, list []*MailContactEntity, batchSize int) (err error) {
	if len(list) == 0 {
		return nil
	}
	for _, e := range list {
		if e.Tags == nil {
			e.Tags = StringArray{}
		}
	}
	if batchSize <= 0 {
		batchSize = 200
	}
	return m.tx(ctx).CreateInBatches(list, batchSize).Error
}

// UpdateContactsByEmails 按邮箱批量更新若干列（导入的已存在那一批）。
//
// **不碰 status / subscribed_at / consent_source**：同意状态不能被一次导入悄悄改写
// （原来退订的人，导入不该把他变回订阅）。
func (m *MailModel) UpdateContactsByEmails(ctx context.Context, emails []string, fields map[string]any, at time.Time) (err error) {
	if len(emails) == 0 || len(fields) == 0 {
		return nil
	}
	lowered := make([]string, 0, len(emails))
	for _, e := range emails {
		if v := strings.TrimSpace(e); v != "" {
			lowered = append(lowered, strings.ToLower(v))
		}
	}
	if len(lowered) == 0 {
		return nil
	}
	fields["update_time"] = at
	return m.tx(ctx).Model(&MailContactEntity{}).Where("lower(email) IN ?", lowered).Updates(fields).Error
}

// ListContacts 按筛选分页取联系人。
func (m *MailModel) ListContacts(ctx context.Context, f ContactFilter) (list []*MailContactEntity, total int64, err error) {
	q := m.tx(ctx).Model(&MailContactEntity{})
	if kw := strings.TrimSpace(f.Keyword); kw != "" {
		like := "%" + kw + "%"
		q = q.Where("email ILIKE ? OR name ILIKE ?", like, like)
	}
	if s := strings.TrimSpace(f.Status); s != "" {
		q = q.Where("status = ?", s)
	}
	if len(f.Tags) > 0 {
		q = q.Where("tags @> ?::text[]", arrayLiteral(f.Tags))
	}
	if err = q.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	limit := f.Limit
	if limit <= 0 {
		limit = 20
	}
	err = q.Order("id DESC").Offset(f.Offset).Limit(limit).Find(&list).Error
	return list, total, err
}

// ListSubscribedByTags **投递目标**：只取已订阅、且命中标签的人。
//
// 用 id 游标分批（afterID）而不是 OFFSET：群发时一边取一边发、
// 期间可能有新联系人进来，OFFSET 会漏人或重复；游标严格按主键推进，天然稳定。
func (m *MailModel) ListSubscribedByTags(ctx context.Context, tags []string, afterID uint64, limit int) (list []*MailContactEntity, err error) {
	q := m.tx(ctx).Where("status = ?", ContactStatusSubscribed)
	if len(tags) > 0 {
		q = q.Where("tags @> ?::text[]", arrayLiteral(tags))
	}
	if afterID > 0 {
		q = q.Where("id > ?", afterID)
	}
	if limit <= 0 {
		limit = 500
	}
	err = q.Order("id ASC").Limit(limit).Find(&list).Error
	return list, err
}

// CountSubscribedByTags 统计投递目标人数（活动开始前算 total_count）。
func (m *MailModel) CountSubscribedByTags(ctx context.Context, tags []string) (count int64, err error) {
	q := m.tx(ctx).Model(&MailContactEntity{}).Where("status = ?", ContactStatusSubscribed)
	if len(tags) > 0 {
		q = q.Where("tags @> ?::text[]", arrayLiteral(tags))
	}
	err = q.Count(&count).Error
	return count, err
}

// UpdateContactFields 按主键更新指定列。
func (m *MailModel) UpdateContactFields(ctx context.Context, id uint64, fields map[string]any) (err error) {
	if len(fields) == 0 {
		return nil
	}
	return m.tx(ctx).Model(&MailContactEntity{}).Where("id = ?", id).Updates(fields).Error
}

// UpdateContactStatusByEmail 按邮箱改状态（退订 / 退信回写用，幂等）。
func (m *MailModel) UpdateContactStatusByEmail(ctx context.Context, email, status string, at time.Time) (err error) {
	fields := map[string]any{"status": status, "update_time": at}
	if status == ContactStatusSubscribed {
		fields["subscribed_at"] = at
	}
	return m.tx(ctx).Model(&MailContactEntity{}).Where("lower(email) = lower(?)", strings.TrimSpace(email)).Updates(fields).Error
}

// DeleteContact 删除联系人。
func (m *MailModel) DeleteContact(ctx context.Context, id uint64) (err error) {
	return m.tx(ctx).Where("id = ?", id).Delete(&MailContactEntity{}).Error
}

// ---- 活动 ----

// CreateCampaign 新建活动。
func (m *MailModel) CreateCampaign(ctx context.Context, e *MailCampaignEntity) (err error) {
	if e.TargetTags == nil {
		e.TargetTags = StringArray{}
	}
	return m.tx(ctx).Create(e).Error
}

// GetCampaign 按主键取活动。
func (m *MailModel) GetCampaign(ctx context.Context, id uint64) (e *MailCampaignEntity, err error) {
	e = &MailCampaignEntity{}
	err = m.tx(ctx).Where("id = ?", id).First(e).Error
	return e, err
}

// ListCampaigns 列活动（状态可空）。
func (m *MailModel) ListCampaigns(ctx context.Context, status string, offset, limit int) (list []*MailCampaignEntity, total int64, err error) {
	q := m.tx(ctx).Model(&MailCampaignEntity{})
	if s := strings.TrimSpace(status); s != "" {
		q = q.Where("status = ?", s)
	}
	if err = q.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	if limit <= 0 {
		limit = 20
	}
	err = q.Order("id DESC").Offset(offset).Limit(limit).Find(&list).Error
	return list, total, err
}

// UpdateCampaignFields 按主键更新指定列。
func (m *MailModel) UpdateCampaignFields(ctx context.Context, id uint64, fields map[string]any) (err error) {
	if len(fields) == 0 {
		return nil
	}
	return m.tx(ctx).Model(&MailCampaignEntity{}).Where("id = ?", id).Updates(fields).Error
}

// IncrCampaignCounts **原子**递增活动的送达 / 失败计数。
//
// 并发投递时多个 worker 会同时回写同一个活动行：读-改-写在并发下必然丢计数，
// 所以计数在 SQL 里做（sent_count = sent_count + n），不经过 Go 侧。
func (m *MailModel) IncrCampaignCounts(ctx context.Context, id uint64, sent, failed int) (err error) {
	if sent == 0 && failed == 0 {
		return nil
	}
	return m.tx(ctx).Model(&MailCampaignEntity{}).Where("id = ?", id).
		Updates(map[string]any{
			"sent_count":   gorm.Expr("sent_count + ?", sent),
			"failed_count": gorm.Expr("failed_count + ?", failed),
		}).Error
}

// ---- 事件 ----

// CreateEvent 写一条事件。
func (m *MailModel) CreateEvent(ctx context.Context, e *MailCampaignEventEntity) (err error) {
	return m.tx(ctx).Create(e).Error
}

// BatchCreateEvents 批量写事件（追踪端点批量落事件用，减少往返）。
func (m *MailModel) BatchCreateEvents(ctx context.Context, list []*MailCampaignEventEntity, batchSize int) (err error) {
	if len(list) == 0 {
		return nil
	}
	if batchSize <= 0 {
		batchSize = 200
	}
	return m.tx(ctx).CreateInBatches(list, batchSize).Error
}

// CountEventsByType 统计某活动各类事件数（报表用）。
func (m *MailModel) CountEventsByType(ctx context.Context, campaignID uint64) (counts map[string]int64, err error) {
	type row struct {
		EventType string
		Total     int64
	}
	var rows []row
	err = m.tx(ctx).Model(&MailCampaignEventEntity{}).
		Select("event_type, COUNT(*) AS total").
		Where("campaign_id = ?", campaignID).
		Group("event_type").Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	counts = map[string]int64{}
	for _, r := range rows {
		counts[r.EventType] = r.Total
	}
	return counts, nil
}
