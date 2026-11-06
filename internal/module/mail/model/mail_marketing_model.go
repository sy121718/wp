package model

import (
	"context"
	"database/sql/driver"
	"fmt"
	json "go_wp/pkg/jsonx"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	ContactSourceSystemUser = "system_user"
	ContactSourceImport     = "import"
	ContactSourceManual     = "manual"
	ContactSourceSubscribe  = "subscribe"
)

const (
	ContactStatusPending      = "pending"
	ContactStatusSubscribed   = "subscribed"
	ContactStatusUnsubscribed = "unsubscribed"
	ContactStatusBounced      = "bounced"
	ContactStatusComplained   = "complained"
)

const (
	CampaignStatusDraft     = "draft"
	CampaignStatusScheduled = "scheduled"
	CampaignStatusSending   = "sending"
	CampaignStatusSent      = "sent"
	CampaignStatusFailed    = "failed"
	CampaignStatusCanceled  = "canceled"
)

const (
	EventTypeOpen        = "open"
	EventTypeClick       = "click"
	EventTypeBounce      = "bounce"
	EventTypeUnsubscribe = "unsubscribe"
	EventTypeComplaint   = "complaint"

	EventTypeReply = "reply"
)

type StringArray []string

func (a StringArray) Value() (driver.Value, error) {
	if a == nil {
		return "{}", nil
	}
	return encodePGTextArray(a), nil
}

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

type MailContactEntity struct {
	ID            uint64     `gorm:"column:id;primaryKey"`
	Email         string     `gorm:"column:email"`
	Name          *string    `gorm:"column:name"`
	UserID        *uint64    `gorm:"column:user_id"`
	Source        string     `gorm:"column:source"`
	Status        string     `gorm:"column:status"`
	SubscribedAt  *time.Time `gorm:"column:subscribed_at"`
	ConsentSource *string    `gorm:"column:consent_source"`

	Tags           StringArray `gorm:"column:tags;type:text[]"`
	Attributes     JSONMap     `gorm:"column:attributes;type:jsonb"`
	LastActivityAt *time.Time  `gorm:"column:last_activity_at"`
	CreateTime     *time.Time  `gorm:"column:create_time;autoCreateTime"`
	UpdateTime     *time.Time  `gorm:"column:update_time"`
}

func (MailContactEntity) TableName() string { return "mail_contacts" }

type MailCampaignEntity struct {
	ID          uint64      `gorm:"column:id;primaryKey"`
	Name        string      `gorm:"column:name"`
	AccountID   uint64      `gorm:"column:account_id"`
	TemplateID  uint64      `gorm:"column:template_id"`
	TargetTags  StringArray `gorm:"column:target_tags;type:text[]"`
	Subject     string      `gorm:"column:subject"`
	Variables   JSONMap     `gorm:"column:variables;type:jsonb"`
	Status      string      `gorm:"column:status"`
	ScheduledAt *time.Time  `gorm:"column:scheduled_at"`
	StartedAt   *time.Time  `gorm:"column:started_at"`
	FinishedAt  *time.Time  `gorm:"column:finished_at"`
	TotalCount  int         `gorm:"column:total_count"`
	SentCount   int         `gorm:"column:sent_count"`
	FailedCount int         `gorm:"column:failed_count"`
	CreateBy    uint64      `gorm:"column:create_by"`
	CreateTime  *time.Time  `gorm:"column:create_time;autoCreateTime"`
	UpdateTime  *time.Time  `gorm:"column:update_time"`
}

func (MailCampaignEntity) TableName() string { return "mail_campaigns" }

type MailCampaignEventEntity struct {
	ID         uint64     `gorm:"column:id;primaryKey"`
	CampaignID uint64     `gorm:"column:campaign_id"`
	ContactID  uint64     `gorm:"column:contact_id"`
	EventType  string     `gorm:"column:event_type"`
	URL        *string    `gorm:"column:url"`
	IP         *string    `gorm:"column:ip"`
	UserAgent  *string    `gorm:"column:user_agent"`
	CreateTime *time.Time `gorm:"column:create_time;autoCreateTime"`
}

func (MailCampaignEventEntity) TableName() string { return "mail_campaign_events" }

type ContactFilter struct {
	Keyword string

	Status string

	Tags   []string
	Offset int
	Limit  int
}

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

func (m *MailModel) CreateContact(ctx context.Context, e *MailContactEntity) (err error) {
	if e.Tags == nil {
		e.Tags = StringArray{}
	}
	return m.tx(ctx).Create(e).Error
}

func (m *MailModel) GetContact(ctx context.Context, id uint64) (e *MailContactEntity, err error) {
	e = &MailContactEntity{}
	err = m.tx(ctx).Where("id = ?", id).First(e).Error
	return e, err
}

func (m *MailModel) GetContactByEmail(ctx context.Context, email string) (e *MailContactEntity, err error) {
	addr := strings.TrimSpace(email)
	if addr == "" {
		return nil, gorm.ErrRecordNotFound
	}
	e = &MailContactEntity{}
	err = m.tx(ctx).Where("lower(email) = lower(?)", addr).First(e).Error
	return e, err
}

func (m *MailModel) ExistingContactIDs(ctx context.Context, emails []string) (ids map[string]uint64, err error) {
	return m.ExistingContactIDsTx(ctx, nil, emails)
}

func (m *MailModel) ExistingContactIDsTx(ctx context.Context, tx *gorm.DB, emails []string) (ids map[string]uint64, err error) {
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
	err = m.txOr(ctx, tx).Model(&MailContactEntity{}).
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

// ExistingContactIDsForUpdateTx 按邮箱锁定已存在的联系人（id 升序）：
// 导入的旧标签差集与随后的更新必须共用这把锁，避免并发导入互相覆盖或重复触发 tag_added。
func (m *MailModel) ExistingContactIDsForUpdateTx(ctx context.Context, tx *gorm.DB, emails []string) (ids map[string]uint64, err error) {
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
	err = m.txOr(ctx, tx).Model(&MailContactEntity{}).
		Select("id, lower(email) AS email").
		Where("lower(email) IN ?", lowered).
		Order("id ASC").
		Clauses(clause.Locking{Strength: "UPDATE"}).
		Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	for _, r := range rows {
		ids[strings.ToLower(r.Email)] = r.ID
	}
	return ids, nil
}

const contactTagsChunkSize = 2000

func (m *MailModel) TagsByContactIDs(ctx context.Context, ids []uint64) (tags map[uint64][]string, err error) {
	return m.TagsByContactIDsTx(ctx, nil, ids)
}

func (m *MailModel) TagsByContactIDsTx(ctx context.Context, tx *gorm.DB, ids []uint64) (tags map[uint64][]string, err error) {
	tags = make(map[uint64][]string, len(ids))
	if len(ids) == 0 {
		return tags, nil
	}
	uniq := make([]uint64, 0, len(ids))
	seen := make(map[uint64]struct{}, len(ids))
	for _, id := range ids {
		if id == 0 {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		uniq = append(uniq, id)
	}
	for start := 0; start < len(uniq); start += contactTagsChunkSize {
		end := start + contactTagsChunkSize
		if end > len(uniq) {
			end = len(uniq)
		}
		var rows []struct {
			ID   uint64
			Tags StringArray
		}
		if qerr := m.txOr(ctx, tx).Model(&MailContactEntity{}).
			Select("id", "tags").
			Where("id IN ?", uniq[start:end]).
			Scan(&rows).Error; qerr != nil {
			return nil, qerr
		}
		for _, r := range rows {
			tags[r.ID] = r.Tags
		}
	}
	return tags, nil
}

func (m *MailModel) BatchInsertContacts(ctx context.Context, list []*MailContactEntity, batchSize int) (err error) {
	return m.BatchInsertContactsTx(ctx, nil, list, batchSize)
}

func (m *MailModel) BatchInsertContactsTx(ctx context.Context, tx *gorm.DB, list []*MailContactEntity, batchSize int) (err error) {
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
	return m.txOr(ctx, tx).CreateInBatches(list, batchSize).Error
}

type ContactImportUpdate struct {
	Email string   `json:"email"`
	Name  *string  `json:"name,omitempty"`
	Tags  []string `json:"tags,omitempty"`
}

func (m *MailModel) BatchUpdateContactImportFields(ctx context.Context, rows []ContactImportUpdate, source string, at time.Time) (emails []string, err error) {
	return m.BatchUpdateContactImportFieldsTx(ctx, nil, rows, source, at)
}

func (m *MailModel) BatchUpdateContactImportFieldsTx(ctx context.Context, tx *gorm.DB, rows []ContactImportUpdate, source string, at time.Time) (emails []string, err error) {
	if len(rows) == 0 {
		return nil, nil
	}
	payload, err := json.Marshal(rows)
	if err != nil {
		return nil, err
	}
	const q = `UPDATE mail_contacts AS c SET
    name        = COALESCE(v.name, c.name),
    tags        = COALESCE(v.tags, c.tags),
    source      = v.source,
    update_time = v.at
FROM (
    SELECT r->>'email' AS email,
           NULLIF(r->>'name', '') AS name,
           CASE WHEN jsonb_typeof(r->'tags') = 'array' AND jsonb_array_length(r->'tags') > 0
                THEN ARRAY(SELECT jsonb_array_elements_text(r->'tags'))::text[]
                ELSE NULL END AS tags,
           ?::text AS source,
           ?::timestamptz AS at
    FROM jsonb_array_elements(?::jsonb) AS r
) AS v
WHERE lower(c.email) = lower(v.email)
RETURNING lower(c.email)`
	if err = m.txOr(ctx, tx).Raw(q, source, at, string(payload)).Scan(&emails).Error; err != nil {
		return nil, err
	}
	return emails, nil
}

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

func (m *MailModel) CountSubscribedByTags(ctx context.Context, tags []string) (count int64, err error) {
	q := m.tx(ctx).Model(&MailContactEntity{}).Where("status = ?", ContactStatusSubscribed)
	if len(tags) > 0 {
		q = q.Where("tags @> ?::text[]", arrayLiteral(tags))
	}
	err = q.Count(&count).Error
	return count, err
}

func (m *MailModel) UpdateContactFields(ctx context.Context, id uint64, fields map[string]any) (err error) {
	return m.UpdateContactFieldsTx(ctx, nil, id, fields)
}

func (m *MailModel) UpdateContactFieldsTx(ctx context.Context, tx *gorm.DB, id uint64, fields map[string]any) (err error) {
	if len(fields) == 0 {
		return nil
	}
	return m.txOr(ctx, tx).Model(&MailContactEntity{}).Where("id = ?", id).Updates(fields).Error
}

func (m *MailModel) UpdateContactStatusByEmail(ctx context.Context, email, status string, at time.Time) (err error) {
	return m.UpdateContactStatusByEmailTx(ctx, nil, email, status, at)
}

func (m *MailModel) UpdateContactStatusByEmailTx(ctx context.Context, tx *gorm.DB, email, status string, at time.Time) (err error) {
	fields := map[string]any{"status": status, "update_time": at}
	if status == ContactStatusSubscribed {
		fields["subscribed_at"] = at
	}
	return m.txOr(ctx, tx).Model(&MailContactEntity{}).Where("lower(email) = lower(?)", strings.TrimSpace(email)).Updates(fields).Error
}

func (m *MailModel) LockContactTagsTx(ctx context.Context, tx *gorm.DB, contactID uint64) (tags StringArray, err error) {
	var row struct{ Tags StringArray }
	err = m.txOr(ctx, tx).Model(&MailContactEntity{}).
		Select("tags").Where("id = ?", contactID).
		Clauses(clause.Locking{Strength: "UPDATE"}).
		First(&row).Error
	if err != nil {
		return nil, err
	}
	return row.Tags, nil
}

// ContactsByIDsTx 按 id 升序锁定并取回联系人：批量写需要稳定顺序加锁，避免并发死锁。
func (m *MailModel) ContactsByIDsTx(ctx context.Context, tx *gorm.DB, ids []uint64) (list []*MailContactEntity, err error) {
	if len(ids) == 0 {
		return nil, nil
	}
	err = m.txOr(ctx, tx).Model(&MailContactEntity{}).
		Where("id IN ?", ids).
		Order("id ASC").
		Clauses(clause.Locking{Strength: "UPDATE"}).
		Find(&list).Error
	return list, err
}

func (m *MailModel) DeleteContact(ctx context.Context, id uint64) (err error) {
	return m.tx(ctx).Where("id = ?", id).Delete(&MailContactEntity{}).Error
}

func (m *MailModel) DeleteContactsByIDsTx(ctx context.Context, tx *gorm.DB, ids []uint64) (deleted int64, err error) {
	uniq := make([]uint64, 0, len(ids))
	seen := make(map[uint64]struct{}, len(ids))
	for _, id := range ids {
		if id == 0 {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		uniq = append(uniq, id)
	}
	if len(uniq) == 0 {
		return 0, nil
	}
	res := m.txOr(ctx, tx).Where("id IN ?", uniq).Delete(&MailContactEntity{})
	return res.RowsAffected, res.Error
}

func (m *MailModel) ListContactTags(ctx context.Context) (tags []string, err error) {

	err = m.tx(ctx).
		Table(MailContactEntity{}.TableName() + ", unnest(tags) AS t").
		Select("DISTINCT btrim(t) AS tag").
		Where("btrim(t) <> ''").
		Order("tag ASC").
		Scan(&tags).Error
	return tags, err
}

func (m *MailModel) CreateCampaign(ctx context.Context, e *MailCampaignEntity) (err error) {
	if e.TargetTags == nil {
		e.TargetTags = StringArray{}
	}
	return m.tx(ctx).Create(e).Error
}

func (m *MailModel) GetCampaign(ctx context.Context, id uint64) (e *MailCampaignEntity, err error) {
	e = &MailCampaignEntity{}
	err = m.tx(ctx).Where("id = ?", id).First(e).Error
	return e, err
}

func (m *MailModel) DeleteCampaign(ctx context.Context, id uint64) (err error) {
	return m.tx(ctx).Where("id = ?", id).Delete(&MailCampaignEntity{}).Error
}

func (m *MailModel) CampaignsByIDsTx(ctx context.Context, tx *gorm.DB, ids []uint64) (list []*MailCampaignEntity, err error) {
	if len(ids) == 0 {
		return nil, nil
	}
	err = m.txOr(ctx, tx).Model(&MailCampaignEntity{}).
		Where("id IN ?", ids).
		Order("id ASC").
		Clauses(clause.Locking{Strength: "UPDATE"}).
		Find(&list).Error
	return list, err
}

func (m *MailModel) DeleteCampaignsByIDsTx(ctx context.Context, tx *gorm.DB, ids []uint64) (deleted int64, err error) {
	if len(ids) == 0 {
		return 0, nil
	}
	res := m.txOr(ctx, tx).Where("id IN ?", ids).Delete(&MailCampaignEntity{})
	return res.RowsAffected, res.Error
}

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

func (m *MailModel) UpdateCampaignFields(ctx context.Context, id uint64, fields map[string]any) (err error) {
	if len(fields) == 0 {
		return nil
	}
	return m.tx(ctx).Model(&MailCampaignEntity{}).Where("id = ?", id).Updates(fields).Error
}

func (m *MailModel) IncrCampaignCounts(ctx context.Context, id uint64, sent, failed int) (err error) {
	return m.IncrCampaignCountsTx(ctx, nil, id, sent, failed)
}

func (m *MailModel) IncrCampaignCountsTx(ctx context.Context, tx *gorm.DB, id uint64, sent, failed int) (err error) {
	if sent == 0 && failed == 0 {
		return nil
	}
	return m.txOr(ctx, tx).Model(&MailCampaignEntity{}).Where("id = ?", id).
		Updates(map[string]any{
			"sent_count":   gorm.Expr("sent_count + ?", sent),
			"failed_count": gorm.Expr("failed_count + ?", failed),
		}).Error
}

func (m *MailModel) CreateEvent(ctx context.Context, e *MailCampaignEventEntity) (err error) {
	return m.CreateEventTx(ctx, nil, e)
}

func (m *MailModel) CreateEventTx(ctx context.Context, tx *gorm.DB, e *MailCampaignEventEntity) (err error) {
	return m.txOr(ctx, tx).Create(e).Error
}

func (m *MailModel) ListCampaignsWithExpiredEvents(ctx context.Context, cutoff time.Time, limit int) (ids []uint64, err error) {
	if limit < 1 {
		return nil, nil
	}
	err = m.tx(ctx).Model(&MailCampaignEventEntity{}).
		Select("campaign_id").
		Group("campaign_id").
		Having("MAX(create_time) < ?", cutoff).
		Order("campaign_id").
		Limit(limit).
		Pluck("campaign_id", &ids).Error
	return ids, err
}

func (m *MailModel) SetCampaignEventTotalsIfUnset(ctx context.Context, campaignID uint64, openCount, clickCount int64) (int64, error) {
	if campaignID == 0 {
		return 0, nil
	}

	res := m.tx(ctx).Model(&MailCampaignEntity{}).
		Where("id = ? AND open_count = 0 AND click_count = 0", campaignID).
		UpdateColumns(map[string]any{"open_count": openCount, "click_count": clickCount})
	return res.RowsAffected, res.Error
}

func (m *MailModel) DeleteEventsBefore(ctx context.Context, cutoff time.Time, limit int) (int64, error) {
	if limit < 1 {
		return 0, nil
	}
	sub := m.tx(ctx).Model(&MailCampaignEventEntity{}).
		Select("id").Where("create_time < ?", cutoff).Order("id").Limit(limit)
	res := m.tx(ctx).Model(&MailCampaignEventEntity{}).
		Where("id IN (?)", sub).Delete(&MailCampaignEventEntity{})
	return res.RowsAffected, res.Error
}

func (m *MailModel) BatchCreateEvents(ctx context.Context, list []*MailCampaignEventEntity, batchSize int) (err error) {
	if len(list) == 0 {
		return nil
	}
	if batchSize <= 0 {
		batchSize = 200
	}
	return m.tx(ctx).CreateInBatches(list, batchSize).Error
}

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

func (m *MailModel) CountDistinctContactsByEvent(ctx context.Context, campaignID uint64, eventType string) (count int64, err error) {
	err = m.tx(ctx).Model(&MailCampaignEventEntity{}).
		Where("campaign_id = ? AND event_type = ?", campaignID, eventType).
		Distinct("contact_id").Count(&count).Error
	return count, err
}

type ClickStat struct {
	URL   string
	Total int64

	Contacts int64
}

func (m *MailModel) ClickRanking(ctx context.Context, campaignID uint64, limit int) (list []ClickStat, err error) {
	if limit <= 0 {
		limit = 20
	}
	err = m.tx(ctx).Model(&MailCampaignEventEntity{}).
		Select("url, COUNT(*) AS total, COUNT(DISTINCT contact_id) AS contacts").
		Where("campaign_id = ? AND event_type = ? AND url IS NOT NULL AND url <> ''", campaignID, EventTypeClick).
		Group("url").Order("contacts DESC, total DESC").Limit(limit).Scan(&list).Error
	return list, err
}

type RecipientRow struct {
	ContactID uint64
	Email     string
	Name      *string
	Status    string
	SentAt    *time.Time
	ErrorKind *string
	Opened    int64
	Clicked   int64
}

func (m *MailModel) ListRecipients(ctx context.Context, campaignID uint64, offset, limit int) (list []RecipientRow, total int64, err error) {
	base := m.tx(ctx).Table("mail_logs AS l").Where("l.campaign_id = ?", campaignID)
	if err = base.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	if limit <= 0 {
		limit = 20
	}
	err = m.tx(ctx).Table("mail_logs AS l").
		Select("l.contact_id AS contact_id, l.to_email AS email, c.name AS name, l.status AS status, "+
			"l.sent_at AS sent_at, l.error_kind AS error_kind, "+
			"COALESCE(o.total, 0) AS opened, COALESCE(k.total, 0) AS clicked").
		Joins("LEFT JOIN mail_contacts c ON c.id = l.contact_id").
		Joins("LEFT JOIN (SELECT contact_id, COUNT(*) AS total FROM mail_campaign_events WHERE campaign_id = ? AND event_type = 'open' GROUP BY contact_id) o ON o.contact_id = l.contact_id", campaignID).
		Joins("LEFT JOIN (SELECT contact_id, COUNT(*) AS total FROM mail_campaign_events WHERE campaign_id = ? AND event_type = 'click' GROUP BY contact_id) k ON k.contact_id = l.contact_id", campaignID).
		Where("l.campaign_id = ?", campaignID).
		Order("l.id DESC").Offset(offset).Limit(limit).Scan(&list).Error
	return list, total, err
}
