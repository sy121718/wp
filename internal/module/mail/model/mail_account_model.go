package model

import (
	"context"
	"database/sql/driver"
	"errors"
	json "go_wp/pkg/jsonx"
	"go_wp/pkg/utils"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type JSONMap map[string]any

func (j JSONMap) Value() (driver.Value, error) {
	if j == nil {
		return nil, nil
	}
	return json.Marshal(j)
}

func (j *JSONMap) Scan(value any) error {
	if value == nil {
		*j = nil
		return nil
	}
	bytes, ok := value.([]byte)
	if !ok {
		return errors.New("JSONMap Scan: 类型不是 []byte")
	}
	return json.Unmarshal(bytes, j)
}

const (
	AccountPurposeTransactional = "transactional"
	AccountPurposeMarketing     = "marketing"
)

const (
	AccountStatusDisabled = 0
	AccountStatusEnabled  = 1
)

const (
	TemplateStatusDisabled = 0
	TemplateStatusEnabled  = 1
)

const (
	SuppressionReasonUnsubscribe = "unsubscribe"
	SuppressionReasonHardBounce  = "hard_bounce"
	SuppressionReasonComplaint   = "complaint"
	SuppressionReasonManual      = "manual"
)

const (
	LogStatusPending    = "pending"
	LogStatusSent       = "sent"
	LogStatusFailed     = "failed"
	LogStatusSuppressed = "suppressed"
)

type MailAccountEntity struct {
	ID             uint64     `gorm:"column:id;primaryKey"`
	Name           string     `gorm:"column:name"`
	Purpose        string     `gorm:"column:purpose"`
	IsDefault      bool       `gorm:"column:is_default"`
	FromName       *string    `gorm:"column:from_name"`
	FromEmail      string     `gorm:"column:from_email"`
	ReplyTo        *string    `gorm:"column:reply_to"`
	Provider       string     `gorm:"column:provider"`
	Host           *string    `gorm:"column:host"`
	Port           *int       `gorm:"column:port"`
	Username       *string    `gorm:"column:username"`
	PasswordCipher *string    `gorm:"column:password_cipher"`
	Encryption     string     `gorm:"column:encryption"`
	RatePerHour    int        `gorm:"column:rate_per_hour"`
	Status         int        `gorm:"column:status"`
	LastCheckAt    *time.Time `gorm:"column:last_check_at"`
	LastCheckError *string    `gorm:"column:last_check_error"`
	CreateTime     *time.Time `gorm:"column:create_time;autoCreateTime"`
	UpdateTime     *time.Time `gorm:"column:update_time"`
}

func (MailAccountEntity) TableName() string { return "mail_accounts" }

type MailTemplateEntity struct {
	ID          uint64 `gorm:"column:id;primaryKey"`
	TemplateKey string `gorm:"column:template_key"`
	Locale      string `gorm:"column:locale"`
	Name        string `gorm:"column:name"`
	Subject     string `gorm:"column:subject"`
	BodyHTML    string `gorm:"column:body_html"`
	BodyText    string `gorm:"column:body_text"`

	Variables StringArray `gorm:"column:variables;type:jsonb"`

	BodyJSON JSONMap `gorm:"column:body_json;type:jsonb"`

	BodyCompiledHTML string     `gorm:"column:body_compiled_html"`
	BodyCompiledText string     `gorm:"column:body_compiled_text"`
	Status           int        `gorm:"column:status"`
	CreateTime       *time.Time `gorm:"column:create_time;autoCreateTime"`
	UpdateTime       *time.Time `gorm:"column:update_time"`
}

func (MailTemplateEntity) TableName() string { return "mail_templates" }

type MailLogEntity struct {
	ID          uint64  `gorm:"column:id;primaryKey"`
	AccountID   *uint64 `gorm:"column:account_id"`
	TemplateKey *string `gorm:"column:template_key"`
	ToEmail     string  `gorm:"column:to_email"`

	CampaignID    *uint64    `gorm:"column:campaign_id"`
	ContactID     *uint64    `gorm:"column:contact_id"`
	Subject       *string    `gorm:"column:subject"`
	Status        string     `gorm:"column:status"`
	Provider      *string    `gorm:"column:provider"`
	ProviderMsgID *string    `gorm:"column:provider_msg_id"`
	ErrorKind     *string    `gorm:"column:error_kind"`
	ErrorMessage  *string    `gorm:"column:error_message"`
	RetryCount    int        `gorm:"column:retry_count"`
	SentAt        *time.Time `gorm:"column:sent_at"`
	CreateTime    *time.Time `gorm:"column:create_time;autoCreateTime"`
}

func (MailLogEntity) TableName() string { return "mail_logs" }

type MailSuppressionEntity struct {
	ID         uint64     `gorm:"column:id;primaryKey"`
	Email      string     `gorm:"column:email"`
	Reason     string     `gorm:"column:reason"`
	Source     *string    `gorm:"column:source"`
	Note       *string    `gorm:"column:note"`
	CreateTime *time.Time `gorm:"column:create_time;autoCreateTime"`
}

func (MailSuppressionEntity) TableName() string { return "mail_suppressions" }

type MailModel struct{ db *gorm.DB }

func NewMailModel(db *gorm.DB) *MailModel { return &MailModel{db: db} }

func (m *MailModel) tx(ctx context.Context) *gorm.DB { return m.db.WithContext(ctx) }

func (m *MailModel) txOr(ctx context.Context, tx *gorm.DB) *gorm.DB {
	if tx != nil {
		return tx.WithContext(ctx)
	}
	return m.tx(ctx)
}

func (m *MailModel) Transaction(ctx context.Context, fn func(tx *gorm.DB) error) error {
	return m.db.WithContext(ctx).Transaction(fn)
}

func (m *MailModel) CreateAccount(ctx context.Context, e *MailAccountEntity) (err error) {
	return m.tx(ctx).Create(e).Error
}

func (m *MailModel) GetAccount(ctx context.Context, id uint64) (e *MailAccountEntity, err error) {
	e = &MailAccountEntity{}
	err = m.tx(ctx).Where("id = ?", id).First(e).Error
	return e, err
}

func (m *MailModel) ListAccounts(ctx context.Context, purpose string, onlyEnabled bool) (list []*MailAccountEntity, err error) {
	q := m.tx(ctx).Model(&MailAccountEntity{})
	if p := strings.TrimSpace(purpose); p != "" {
		q = q.Where("purpose = ?", p)
	}
	if onlyEnabled {
		q = q.Where("status = ?", AccountStatusEnabled)
	}
	err = q.Order("is_default DESC, id ASC").Find(&list).Error
	return list, err
}

func (m *MailModel) ListAccountsPage(ctx context.Context, purpose string, onlyEnabled bool, page, limit int) (list []*MailAccountEntity, total int64, current int, err error) {
	q := m.tx(ctx).Model(&MailAccountEntity{})
	if p := strings.TrimSpace(purpose); p != "" {
		q = q.Where("purpose = ?", p)
	}
	if onlyEnabled {
		q = q.Where("status = ?", AccountStatusEnabled)
	}
	if err = q.Count(&total).Error; err != nil {
		return nil, 0, 0, err
	}
	current = mailPageWithinTotal(page, limit, total)
	if limit < 1 {
		limit = 20
	}
	err = q.Order("is_default DESC, id ASC").Offset(utils.PageOffset(current, limit)).Limit(limit).Find(&list).Error
	return list, total, current, err
}

func mailPageWithinTotal(page, limit int, total int64) int {
	if limit < 1 {
		limit = 20
	}
	if page < 1 {
		page = 1
	}
	pages := (total + int64(limit) - 1) / int64(limit)
	if pages < 1 {
		pages = 1
	}
	if int64(page) > pages {
		return int(pages)
	}
	return page
}

func (m *MailModel) DefaultAccount(ctx context.Context, purpose string) (e *MailAccountEntity, err error) {
	e = &MailAccountEntity{}
	err = m.tx(ctx).Where("purpose = ? AND status = ? AND is_default", purpose, AccountStatusEnabled).First(e).Error
	return e, err
}

func (m *MailModel) UpdateAccountFields(ctx context.Context, id uint64, fields map[string]any) (err error) {
	if len(fields) == 0 {
		return nil
	}
	return m.tx(ctx).Model(&MailAccountEntity{}).Where("id = ?", id).Updates(fields).Error
}

func (m *MailModel) ClearDefaultAccounts(ctx context.Context, tx *gorm.DB, purpose string) (err error) {
	return m.txOr(ctx, tx).Model(&MailAccountEntity{}).
		Where("purpose = ? AND is_default", purpose).Update("is_default", false).Error
}

func (m *MailModel) MarkAccountDefaultTx(ctx context.Context, tx *gorm.DB, id uint64) (err error) {
	return m.txOr(ctx, tx).Model(&MailAccountEntity{}).
		Where("id = ?", id).Updates(map[string]any{"is_default": true, "update_time": time.Now()}).Error
}

func (m *MailModel) DeleteAccount(ctx context.Context, id uint64) (err error) {
	return m.tx(ctx).Where("id = ?", id).Delete(&MailAccountEntity{}).Error
}

func (m *MailModel) AccountsByIDsTx(ctx context.Context, tx *gorm.DB, ids []uint64) (list []*MailAccountEntity, err error) {
	if len(ids) == 0 {
		return nil, nil
	}
	err = m.txOr(ctx, tx).Model(&MailAccountEntity{}).
		Where("id IN ?", ids).
		Order("id ASC").
		Clauses(clause.Locking{Strength: "UPDATE"}).
		Find(&list).Error
	return list, err
}

func (m *MailModel) DeleteAccountsByIDsTx(ctx context.Context, tx *gorm.DB, ids []uint64) (deleted int64, err error) {
	if len(ids) == 0 {
		return 0, nil
	}
	res := m.txOr(ctx, tx).Where("id IN ?", ids).Delete(&MailAccountEntity{})
	return res.RowsAffected, res.Error
}

func (m *MailModel) TemplatesByIDsTx(ctx context.Context, tx *gorm.DB, ids []uint64) (list []*MailTemplateEntity, err error) {
	if len(ids) == 0 {
		return nil, nil
	}
	err = m.txOr(ctx, tx).Model(&MailTemplateEntity{}).
		Where("id IN ?", ids).
		Order("id ASC").
		Clauses(clause.Locking{Strength: "UPDATE"}).
		Find(&list).Error
	return list, err
}

func (m *MailModel) DeleteTemplatesByIDsTx(ctx context.Context, tx *gorm.DB, ids []uint64) (deleted int64, err error) {
	if len(ids) == 0 {
		return 0, nil
	}
	res := m.txOr(ctx, tx).Where("id IN ?", ids).Delete(&MailTemplateEntity{})
	return res.RowsAffected, res.Error
}

func (m *MailModel) GetTemplate(ctx context.Context, key, locale string) (e *MailTemplateEntity, err error) {
	e = &MailTemplateEntity{}
	err = m.tx(ctx).Where("template_key = ? AND locale = ?", strings.TrimSpace(key), strings.TrimSpace(locale)).First(e).Error
	return e, err
}

func (m *MailModel) GetTemplateByID(ctx context.Context, id uint64) (e *MailTemplateEntity, err error) {
	e = &MailTemplateEntity{}
	err = m.tx(ctx).Where("id = ?", id).First(e).Error
	return e, err
}

func (m *MailModel) ListTemplates(ctx context.Context, key string) (list []*MailTemplateEntity, err error) {
	q := m.tx(ctx).Model(&MailTemplateEntity{})
	if k := strings.TrimSpace(key); k != "" {
		q = q.Where("template_key = ?", k)
	}
	err = q.Order("template_key ASC, locale ASC").Find(&list).Error
	return list, err
}

func (m *MailModel) ListTemplatesPage(ctx context.Context, key string, page, limit int) (list []*MailTemplateEntity, total int64, current int, err error) {
	q := m.tx(ctx).Model(&MailTemplateEntity{})
	if k := strings.TrimSpace(key); k != "" {
		q = q.Where("template_key = ?", k)
	}
	if err = q.Count(&total).Error; err != nil {
		return nil, 0, 0, err
	}
	current = mailPageWithinTotal(page, limit, total)
	if limit < 1 {
		limit = 20
	}
	err = q.Order("template_key ASC, locale ASC, id ASC").Offset(utils.PageOffset(current, limit)).Limit(limit).Find(&list).Error
	return list, total, current, err
}

func (m *MailModel) UpsertTemplate(ctx context.Context, e *MailTemplateEntity) (err error) {
	return m.tx(ctx).Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "template_key"}, {Name: "locale"}},
		DoUpdates: clause.AssignmentColumns([]string{
			"name", "subject", "body_html", "body_text",

			"body_json", "body_compiled_html", "body_compiled_text",
			"variables", "status", "update_time",
		}),
	}).Create(e).Error
}

func (m *MailModel) CreateLog(ctx context.Context, e *MailLogEntity) (err error) {
	return m.tx(ctx).Create(e).Error
}

func (m *MailModel) DeleteLogsBefore(ctx context.Context, cutoff time.Time, limit int) (int64, error) {
	if limit < 1 {
		return 0, nil
	}

	sub := m.tx(ctx).Model(&MailLogEntity{}).
		Select("id").Where("create_time < ?", cutoff).Order("id").Limit(limit)
	res := m.tx(ctx).Model(&MailLogEntity{}).Where("id IN (?)", sub).Delete(&MailLogEntity{})
	return res.RowsAffected, res.Error
}

func (m *MailModel) DeleteNodeLogsBefore(ctx context.Context, cutoff time.Time, limit int) (int64, error) {
	if limit < 1 {
		return 0, nil
	}
	sub := m.tx(ctx).Model(&MailAutomationNodeLogEntity{}).
		Select("id").Where("create_time < ?", cutoff).Order("id").Limit(limit)
	res := m.tx(ctx).Model(&MailAutomationNodeLogEntity{}).
		Where("id IN (?)", sub).Delete(&MailAutomationNodeLogEntity{})
	return res.RowsAffected, res.Error
}

func (m *MailModel) CreateLogsInBatches(ctx context.Context, list []*MailLogEntity, batchSize int) (err error) {
	if len(list) == 0 {
		return nil
	}
	if batchSize <= 0 {
		batchSize = 200
	}
	return m.tx(ctx).CreateInBatches(list, batchSize).Error
}

func (m *MailModel) UpdateLogResult(ctx context.Context, id uint64, fields map[string]any) (err error) {
	return m.UpdateLogResultTx(ctx, nil, id, fields)
}

func (m *MailModel) UpdateLogResultTx(ctx context.Context, tx *gorm.DB, id uint64, fields map[string]any) (err error) {
	if len(fields) == 0 {
		return nil
	}
	return m.txOr(ctx, tx).Model(&MailLogEntity{}).Where("id = ?", id).Updates(fields).Error
}

func (m *MailModel) IncrLogRetry(ctx context.Context, id uint64) (err error) {
	return m.IncrLogRetryTx(ctx, nil, id)
}

func (m *MailModel) IncrLogRetryTx(ctx context.Context, tx *gorm.DB, id uint64) (err error) {
	return m.txOr(ctx, tx).Model(&MailLogEntity{}).Where("id = ?", id).
		Update("retry_count", gorm.Expr("retry_count + 1")).Error
}

func (m *MailModel) ListLogs(ctx context.Context, toEmail, status string, offset, limit int) (list []*MailLogEntity, total int64, err error) {
	q := m.tx(ctx).Model(&MailLogEntity{})
	if e := strings.TrimSpace(toEmail); e != "" {
		q = q.Where("lower(to_email) = lower(?)", e)
	}
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

func (m *MailModel) IsSuppressed(ctx context.Context, email string) (suppressed bool, err error) {
	var count int64
	err = m.tx(ctx).Model(&MailSuppressionEntity{}).
		Where("lower(email) = lower(?)", strings.TrimSpace(email)).Count(&count).Error
	return count > 0, err
}

func (m *MailModel) SuppressedEmails(ctx context.Context, emails []string) (blocked map[string]bool, err error) {
	return m.SuppressedEmailsTx(ctx, nil, emails)
}

func (m *MailModel) SuppressedEmailsTx(ctx context.Context, tx *gorm.DB, emails []string) (blocked map[string]bool, err error) {
	blocked = map[string]bool{}
	if len(emails) == 0 {
		return blocked, nil
	}
	lowered := make([]string, 0, len(emails))
	for _, e := range emails {
		if v := strings.TrimSpace(e); v != "" {
			lowered = append(lowered, strings.ToLower(v))
		}
	}
	if len(lowered) == 0 {
		return blocked, nil
	}
	var rows []string
	err = m.txOr(ctx, tx).Model(&MailSuppressionEntity{}).
		Where("lower(email) IN ?", lowered).
		Pluck("lower(email)", &rows).Error
	if err != nil {
		return nil, err
	}
	for _, e := range rows {
		blocked[e] = true
	}
	return blocked, nil
}

func (m *MailModel) AddSuppression(ctx context.Context, e *MailSuppressionEntity) (err error) {
	return m.AddSuppressionTx(ctx, nil, e)
}

func (m *MailModel) AddSuppressionTx(ctx context.Context, tx *gorm.DB, e *MailSuppressionEntity) (err error) {
	return m.txOr(ctx, tx).Clauses(clause.OnConflict{DoNothing: true}).Create(e).Error
}

func (m *MailModel) FindSuppressionByEmailTx(ctx context.Context, tx *gorm.DB, email string) (e *MailSuppressionEntity, err error) {
	addr := strings.TrimSpace(email)
	if addr == "" {
		return nil, gorm.ErrRecordNotFound
	}
	e = &MailSuppressionEntity{}
	err = m.txOr(ctx, tx).Where("lower(email) = lower(?)", addr).First(e).Error
	return e, err
}

func (m *MailModel) ListSuppressions(ctx context.Context, reason string, offset, limit int) (list []*MailSuppressionEntity, total int64, err error) {
	q := m.tx(ctx).Model(&MailSuppressionEntity{})
	if r := strings.TrimSpace(reason); r != "" {
		q = q.Where("reason = ?", r)
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

func (m *MailModel) DeleteSuppression(ctx context.Context, id uint64) (err error) {
	return m.tx(ctx).Where("id = ?", id).Delete(&MailSuppressionEntity{}).Error
}

func (m *MailModel) GetLog(ctx context.Context, id uint64) (e *MailLogEntity, err error) {
	e = &MailLogEntity{}
	err = m.tx(ctx).Where("id = ?", id).First(e).Error
	return e, err
}

func (m *MailModel) DeleteTemplate(ctx context.Context, key, locale string) (err error) {
	return m.tx(ctx).
		Where("template_key = ? AND locale = ?", strings.TrimSpace(key), strings.TrimSpace(locale)).
		Delete(&MailTemplateEntity{}).Error
}
