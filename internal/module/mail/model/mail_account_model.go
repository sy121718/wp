package model

// mail_account_model.go — 发信账号 / 模板 / 发送日志 / 抑制名单（issue #37 事务邮件基座）。
//
// 与 pkg 里单表 model 的差别：邮件域有 7 张表、且经常要在**一次业务动作里跨表操作**
// （发一封信要读账号、查抑制名单、写日志），所以本 model 只暴露**具名方法**，
// 不暴露 DB(ctx) 句柄 —— 句柄是内部细节，service 本来就禁止碰它。
//
// 方法形状按**并发友好**设计（用户要求把 Go 的并发优势用上）：
//   · SuppressedEmails 批量查抑制名单（一条 SQL 判断一批，而不是每封一次查询）；
//   · IncrCampaignCounts 用 gorm.Expr 原子递增（并发投递时不会互相覆盖）；
//   · CreateLogsInBatches 批量落日志（群发后一次性写入，减少往返）。

import (
	"context"
	"database/sql/driver"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// JSONMap 可序列化的 JSON 字段（与其它模块同名类型是**有意重复**：跨模块不得 import 对方 model）。
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

// 账号用途（分类标记，不是使用限制 —— 配好 SMTP 的账号事务与营销都能用）。
const (
	AccountPurposeTransactional = "transactional"
	AccountPurposeMarketing     = "marketing"
)

// 账号启用状态。
const (
	AccountStatusDisabled = 0
	AccountStatusEnabled  = 1
)

// 模板启用状态。
const (
	TemplateStatusDisabled = 0
	TemplateStatusEnabled  = 1
)

// 抑制原因。
const (
	SuppressionReasonUnsubscribe = "unsubscribe"
	SuppressionReasonHardBounce  = "hard_bounce"
	SuppressionReasonComplaint   = "complaint"
	SuppressionReasonManual      = "manual"
)

// 发送日志状态。
const (
	LogStatusPending    = "pending"
	LogStatusSent       = "sent"
	LogStatusFailed     = "failed"
	LogStatusSuppressed = "suppressed"
)

// MailAccountEntity 对应 mail_accounts 表。
type MailAccountEntity struct {
	ID             uint64     `gorm:"column:id;primaryKey"`
	Name           string     `gorm:"column:name;type:varchar(64)"`
	Purpose        string     `gorm:"column:purpose;type:varchar(16)"`
	IsDefault      bool       `gorm:"column:is_default"`
	FromName       *string    `gorm:"column:from_name;type:varchar(100)"`
	FromEmail      string     `gorm:"column:from_email;type:varchar(254)"`
	ReplyTo        *string    `gorm:"column:reply_to;type:varchar(254)"`
	Provider       string     `gorm:"column:provider;type:varchar(32)"`
	Host           *string    `gorm:"column:host;type:varchar(255)"`
	Port           *int       `gorm:"column:port"`
	Username       *string    `gorm:"column:username;type:varchar(255)"`
	PasswordCipher *string    `gorm:"column:password_cipher"`
	Encryption     string     `gorm:"column:encryption;type:varchar(16)"`
	RatePerHour    int        `gorm:"column:rate_per_hour"`
	Status         int        `gorm:"column:status;type:smallint"`
	LastCheckAt    *time.Time `gorm:"column:last_check_at;type:timestamp(3)"`
	LastCheckError *string    `gorm:"column:last_check_error"`
	CreateTime     *time.Time `gorm:"column:create_time;type:timestamp(3);autoCreateTime"`
	UpdateTime     *time.Time `gorm:"column:update_time;type:timestamp(3)"`
}

// TableName 表名。
func (MailAccountEntity) TableName() string { return "mail_accounts" }

// MailTemplateEntity 对应 mail_templates 表。
type MailTemplateEntity struct {
	ID          uint64     `gorm:"column:id;primaryKey"`
	TemplateKey string     `gorm:"column:template_key;type:varchar(64)"`
	Locale      string     `gorm:"column:locale;type:varchar(16)"`
	Name        string     `gorm:"column:name;type:varchar(100)"`
	Subject     string     `gorm:"column:subject;type:varchar(255)"`
	BodyHTML    string     `gorm:"column:body_html"`
	BodyText    string     `gorm:"column:body_text"`
	Variables   JSONMap    `gorm:"column:variables;type:jsonb"`
	Status      int        `gorm:"column:status;type:smallint"`
	CreateTime  *time.Time `gorm:"column:create_time;type:timestamp(3);autoCreateTime"`
	UpdateTime  *time.Time `gorm:"column:update_time;type:timestamp(3)"`
}

// TableName 表名。
func (MailTemplateEntity) TableName() string { return "mail_templates" }

// MailLogEntity 对应 mail_logs 表。
type MailLogEntity struct {
	ID            uint64     `gorm:"column:id;primaryKey"`
	AccountID     *uint64    `gorm:"column:account_id"`
	TemplateKey   *string    `gorm:"column:template_key;type:varchar(64)"`
	ToEmail       string     `gorm:"column:to_email;type:varchar(254)"`
	Subject       *string    `gorm:"column:subject;type:varchar(255)"`
	Status        string     `gorm:"column:status;type:varchar(16)"`
	Provider      *string    `gorm:"column:provider;type:varchar(32)"`
	ProviderMsgID *string    `gorm:"column:provider_msg_id;type:varchar(191)"`
	ErrorKind     *string    `gorm:"column:error_kind;type:varchar(16)"`
	ErrorMessage  *string    `gorm:"column:error_message"`
	RetryCount    int        `gorm:"column:retry_count"`
	SentAt        *time.Time `gorm:"column:sent_at;type:timestamp(3)"`
	CreateTime    *time.Time `gorm:"column:create_time;type:timestamp(3);autoCreateTime"`
}

// TableName 表名。
func (MailLogEntity) TableName() string { return "mail_logs" }

// MailSuppressionEntity 对应 mail_suppressions 表。
type MailSuppressionEntity struct {
	ID         uint64     `gorm:"column:id;primaryKey"`
	Email      string     `gorm:"column:email;type:varchar(254)"`
	Reason     string     `gorm:"column:reason;type:varchar(16)"`
	Source     *string    `gorm:"column:source;type:varchar(64)"`
	Note       *string    `gorm:"column:note;type:varchar(255)"`
	CreateTime *time.Time `gorm:"column:create_time;type:timestamp(3);autoCreateTime"`
}

// TableName 表名。
func (MailSuppressionEntity) TableName() string { return "mail_suppressions" }

// MailModel 邮件域的表访问单元（管本模块 7 张表）。
type MailModel struct{ db *gorm.DB }

// NewMailModel 构造。
func NewMailModel(db *gorm.DB) *MailModel { return &MailModel{db: db} }

// tx 内部句柄（不导出：本 model 多表，对外只给具名方法）。
func (m *MailModel) tx(ctx context.Context) *gorm.DB { return m.db.WithContext(ctx) }

// Transaction 透传事务：跨表编排由 service 决定边界。
func (m *MailModel) Transaction(ctx context.Context, fn func(tx *gorm.DB) error) error {
	return m.db.WithContext(ctx).Transaction(fn)
}

// ---- 账号 ----

// CreateAccount 新建发信账号。
func (m *MailModel) CreateAccount(ctx context.Context, e *MailAccountEntity) (err error) {
	return m.tx(ctx).Create(e).Error
}

// GetAccount 按主键取账号。
func (m *MailModel) GetAccount(ctx context.Context, id uint64) (e *MailAccountEntity, err error) {
	e = &MailAccountEntity{}
	err = m.tx(ctx).Where("id = ?", id).First(e).Error
	return e, err
}

// ListAccounts 列出账号（可按用途过滤；purpose 为空表示全部）。
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

// DefaultAccount 取某用途的默认账号（不存在返回 gorm.ErrRecordNotFound）。
func (m *MailModel) DefaultAccount(ctx context.Context, purpose string) (e *MailAccountEntity, err error) {
	e = &MailAccountEntity{}
	err = m.tx(ctx).Where("purpose = ? AND status = ? AND is_default", purpose, AccountStatusEnabled).First(e).Error
	return e, err
}

// UpdateAccountFields 按主键更新指定列。
func (m *MailModel) UpdateAccountFields(ctx context.Context, id uint64, fields map[string]any) (err error) {
	if len(fields) == 0 {
		return nil
	}
	return m.tx(ctx).Model(&MailAccountEntity{}).Where("id = ?", id).Updates(fields).Error
}

// ClearDefaultAccounts 清掉某用途下的默认标记（切换默认账号时先清后设，同一事务内）。
func (m *MailModel) ClearDefaultAccounts(ctx context.Context, tx *gorm.DB, purpose string) (err error) {
	return tx.WithContext(ctx).Model(&MailAccountEntity{}).
		Where("purpose = ? AND is_default", purpose).Update("is_default", false).Error
}

// DeleteAccount 删除账号。
func (m *MailModel) DeleteAccount(ctx context.Context, id uint64) (err error) {
	return m.tx(ctx).Where("id = ?", id).Delete(&MailAccountEntity{}).Error
}

// ---- 模板 ----

// GetTemplate 按 key + 语言取模板（locale 为空表示通用兜底）。
func (m *MailModel) GetTemplate(ctx context.Context, key, locale string) (e *MailTemplateEntity, err error) {
	e = &MailTemplateEntity{}
	err = m.tx(ctx).Where("template_key = ? AND locale = ?", strings.TrimSpace(key), strings.TrimSpace(locale)).First(e).Error
	return e, err
}

// ListTemplates 列出模板（key 为空表示全部）。
func (m *MailModel) ListTemplates(ctx context.Context, key string) (list []*MailTemplateEntity, err error) {
	q := m.tx(ctx).Model(&MailTemplateEntity{})
	if k := strings.TrimSpace(key); k != "" {
		q = q.Where("template_key = ?", k)
	}
	err = q.Order("template_key ASC, locale ASC").Find(&list).Error
	return list, err
}

// UpsertTemplate 新建或覆盖模板（key + locale 唯一）。
func (m *MailModel) UpsertTemplate(ctx context.Context, e *MailTemplateEntity) (err error) {
	return m.tx(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "template_key"}, {Name: "locale"}},
		DoUpdates: clause.AssignmentColumns([]string{"name", "subject", "body_html", "body_text", "variables", "status", "update_time"}),
	}).Create(e).Error
}

// ---- 日志 ----

// CreateLog 写一条发送日志。
func (m *MailModel) CreateLog(ctx context.Context, e *MailLogEntity) (err error) {
	return m.tx(ctx).Create(e).Error
}

// CreateLogsInBatches 批量落日志（群发后一次写入，减少往返）。
func (m *MailModel) CreateLogsInBatches(ctx context.Context, list []*MailLogEntity, batchSize int) (err error) {
	if len(list) == 0 {
		return nil
	}
	if batchSize <= 0 {
		batchSize = 200
	}
	return m.tx(ctx).CreateInBatches(list, batchSize).Error
}

// UpdateLogResult 回写发送结果（状态 / provider / 错误分类 / 消息 id）。
func (m *MailModel) UpdateLogResult(ctx context.Context, id uint64, fields map[string]any) (err error) {
	if len(fields) == 0 {
		return nil
	}
	return m.tx(ctx).Model(&MailLogEntity{}).Where("id = ?", id).Updates(fields).Error
}

// IncrLogRetry 原子递增重试次数（并发重试时不会互相覆盖）。
func (m *MailModel) IncrLogRetry(ctx context.Context, id uint64) (err error) {
	return m.tx(ctx).Model(&MailLogEntity{}).Where("id = ?", id).
		Update("retry_count", gorm.Expr("retry_count + 1")).Error
}

// ListLogs 按条件列日志（收件人 / 状态可空）。
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

// ---- 抑制名单 ----

// IsSuppressed 单个地址是否在抑制名单里。
func (m *MailModel) IsSuppressed(ctx context.Context, email string) (suppressed bool, err error) {
	var count int64
	err = m.tx(ctx).Model(&MailSuppressionEntity{}).
		Where("lower(email) = lower(?)", strings.TrimSpace(email)).Count(&count).Error
	return count > 0, err
}

// SuppressedEmails **批量**查抑制名单：一次 SQL 判定一批地址。
//
// 这是并发投递路径上的关键点：群发时若每个收件人查一次库，
// N 封邮件就是 N 次往返；批量查询把「一批地址是否被抑制」压成一条 SQL，
// worker 并发起来才不会把数据库打满。
func (m *MailModel) SuppressedEmails(ctx context.Context, emails []string) (blocked map[string]bool, err error) {
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
	err = m.tx(ctx).Model(&MailSuppressionEntity{}).
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

// AddSuppression 加入抑制名单（已存在则忽略，不报错 —— 退订是幂等动作）。
func (m *MailModel) AddSuppression(ctx context.Context, e *MailSuppressionEntity) (err error) {
	return m.tx(ctx).Clauses(clause.OnConflict{DoNothing: true}).Create(e).Error
}

// ListSuppressions 列抑制名单。
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

// DeleteSuppression 从抑制名单移除（误判时人工放行）。
func (m *MailModel) DeleteSuppression(ctx context.Context, id uint64) (err error) {
	return m.tx(ctx).Where("id = ?", id).Delete(&MailSuppressionEntity{}).Error
}
