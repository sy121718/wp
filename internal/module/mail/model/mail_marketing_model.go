package model

// mail_marketing_model.go — 联系人 / 群发活动 / 事件（issue #37 营销域）。
//
// 并发友好的点（用户要求把 Go 的并发优势用上）：
//   · BatchUpsertContacts —— 导入用批量 upsert（一条语句一批，而不是逐行 insert）；
//   · IncrCampaignCounts —— 计数用原子递增，多个投递 worker 同时回写不会互相覆盖；
//   · ListSubscribedByTags —— 投递目标分批取（游标式），大名单不必一次读进内存。

import (
	"context"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
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
	// Tags PG 原生 text[]。GORM 经 pgx 映射 []string —— 注意**初始化成空切片而不是 nil**：
	// 列是 NOT NULL DEFAULT '{}'，传 nil 会写 NULL 触发约束错误。
	Tags           []string   `gorm:"column:tags;type:text[]"`
	Attributes     JSONMap    `gorm:"column:attributes;type:jsonb"`
	LastActivityAt *time.Time `gorm:"column:last_activity_at;type:timestamp(3)"`
	CreateTime     *time.Time `gorm:"column:create_time;type:timestamp(3)"`
	UpdateTime     *time.Time `gorm:"column:update_time;type:timestamp(3)"`
}

// TableName 表名。
func (MailContactEntity) TableName() string { return "mail_contacts" }

// MailCampaignEntity 对应 mail_campaigns 表。
type MailCampaignEntity struct {
	ID          uint64     `gorm:"column:id;primaryKey"`
	Name        string     `gorm:"column:name;type:varchar(150)"`
	AccountID   uint64     `gorm:"column:account_id"`
	TemplateID  uint64     `gorm:"column:template_id"`
	TargetTags  []string   `gorm:"column:target_tags;type:text[]"`
	Subject     string     `gorm:"column:subject;type:varchar(255)"`
	Variables   JSONMap    `gorm:"column:variables;type:jsonb"`
	Status      string     `gorm:"column:status;type:varchar(16)"`
	ScheduledAt *time.Time `gorm:"column:scheduled_at;type:timestamp(3)"`
	StartedAt   *time.Time `gorm:"column:started_at;type:timestamp(3)"`
	FinishedAt  *time.Time `gorm:"column:finished_at;type:timestamp(3)"`
	TotalCount  int        `gorm:"column:total_count"`
	SentCount   int        `gorm:"column:sent_count"`
	FailedCount int        `gorm:"column:failed_count"`
	CreateBy    uint64     `gorm:"column:create_by"`
	CreateTime  *time.Time `gorm:"column:create_time;type:timestamp(3)"`
	UpdateTime  *time.Time `gorm:"column:update_time;type:timestamp(3)"`
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
	CreateTime *time.Time `gorm:"column:create_time;type:timestamp(3)"`
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
		e.Tags = []string{}
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

// BatchUpsertContacts 批量新建或更新联系人（导入 / 拉系统客户走它）。
//
// 冲突键是邮箱：已存在则更新（不覆盖同意状态相关的列由 service 决定传什么）。
// 返回实际写入条数。批大小可调：一次语句写几百行，比逐行 insert 快一个量级。
func (m *MailModel) BatchUpsertContacts(ctx context.Context, list []*MailContactEntity, batchSize int, updateColumns []string) (err error) {
	if len(list) == 0 {
		return nil
	}
	for _, e := range list {
		if e.Tags == nil {
			e.Tags = []string{}
		}
	}
	if batchSize <= 0 {
		batchSize = 200
	}
	cl := clause.OnConflict{
		Columns: []clause.Column{{Name: "email"}},
	}
	if len(updateColumns) > 0 {
		cl.DoUpdates = clause.AssignmentColumns(append(updateColumns, "update_time"))
	} else {
		cl.DoNothing = true
	}

	return m.tx(ctx).Clauses(cl).CreateInBatches(list, batchSize).Error
}

// ListContacts 按筛选分页取联系人。
func (m *MailModel) ListContacts(ctx context.Context, f ContactFilter) (list []*MailContactEntity, total int64, err error) {
	q := m.tx(ctx)
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
		e.TargetTags = []string{}
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
	q := m.tx(ctx)
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
