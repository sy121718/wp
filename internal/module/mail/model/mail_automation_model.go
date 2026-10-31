package model

import (
	"context"
	"strings"
	"time"

	"gorm.io/gorm"
)

const (
	AutomationStatusDraft  = "draft"
	AutomationStatusActive = "active"
	AutomationStatusPaused = "paused"
)

const (
	TriggerManual = "manual"

	TriggerContactCreated = "contact_created"

	TriggerContactSubscribed = "contact_subscribed"

	TriggerEmailOpened  = "email_opened"
	TriggerEmailClicked = "email_clicked"

	TriggerTagAdded = "tag_added"
)

const (
	TriggerFrequencyEveryTime = "every_time"

	TriggerFrequencyOnce = "once"
)

const (
	RunStatusRunning   = "running"
	RunStatusWaiting   = "waiting"
	RunStatusCompleted = "completed"
	RunStatusFailed    = "failed"
	RunStatusStopped   = "stopped"
)

const (
	NodeStatusOK      = "ok"
	NodeStatusSkipped = "skipped"
	NodeStatusWaiting = "waiting"
	NodeStatusFailed  = "failed"
)

type MailAutomationEntity struct {
	ID            uint64  `gorm:"column:id;primaryKey"`
	Name          string  `gorm:"column:name"`
	Description   *string `gorm:"column:description"`
	TriggerType   string  `gorm:"column:trigger_type"`
	TriggerParams JSONMap `gorm:"column:trigger_params;type:jsonb"`

	TriggerFrequency string `gorm:"column:trigger_frequency"`

	ExitOnReply bool `gorm:"column:exit_on_reply"`

	Definition JSONMap    `gorm:"column:definition;type:jsonb"`
	Status     string     `gorm:"column:status"`
	Version    int        `gorm:"column:version"`
	CreateBy   uint64     `gorm:"column:create_by"`
	CreateTime *time.Time `gorm:"column:create_time;autoCreateTime"`
	UpdateTime *time.Time `gorm:"column:update_time"`
}

func (MailAutomationEntity) TableName() string { return "mail_automations" }

type MailAutomationRunEntity struct {
	ID                uint64     `gorm:"column:id;primaryKey"`
	AutomationID      uint64     `gorm:"column:automation_id"`
	AutomationVersion int        `gorm:"column:automation_version"`
	ContactID         uint64     `gorm:"column:contact_id"`
	Status            string     `gorm:"column:status"`
	CurrentNode       *string    `gorm:"column:current_node"`
	NextRunAt         *time.Time `gorm:"column:next_run_at"`
	ErrorMessage      *string    `gorm:"column:error_message"`
	TriggerEvent      *string    `gorm:"column:trigger_event"`
	StartedAt         *time.Time `gorm:"column:started_at"`
	FinishedAt        *time.Time `gorm:"column:finished_at"`
	CreateTime        *time.Time `gorm:"column:create_time;autoCreateTime"`
	UpdateTime        *time.Time `gorm:"column:update_time"`
}

func (MailAutomationRunEntity) TableName() string { return "mail_automation_runs" }

type MailAutomationNodeLogEntity struct {
	ID         uint64     `gorm:"column:id;primaryKey"`
	RunID      uint64     `gorm:"column:run_id"`
	NodeKey    string     `gorm:"column:node_key"`
	NodeType   string     `gorm:"column:node_type"`
	Status     string     `gorm:"column:status"`
	Detail     *string    `gorm:"column:detail"`
	CreateTime *time.Time `gorm:"column:create_time;autoCreateTime"`
}

func (MailAutomationNodeLogEntity) TableName() string { return "mail_automation_node_logs" }

func (m *MailModel) CreateAutomation(ctx context.Context, e *MailAutomationEntity) (err error) {
	return m.tx(ctx).Create(e).Error
}

func (m *MailModel) GetAutomation(ctx context.Context, id uint64) (e *MailAutomationEntity, err error) {
	e = &MailAutomationEntity{}
	err = m.tx(ctx).Where("id = ?", id).First(e).Error
	return e, err
}

func (m *MailModel) ListAutomations(ctx context.Context, status string, offset, limit int) (list []*MailAutomationEntity, total int64, err error) {
	q := m.tx(ctx).Model(&MailAutomationEntity{})
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

func (m *MailModel) ListActiveAutomations(ctx context.Context) (list []*MailAutomationEntity, err error) {
	err = m.tx(ctx).Where("status = ?", AutomationStatusActive).Order("id ASC").Find(&list).Error
	return list, err
}

func (m *MailModel) UpdateAutomationFields(ctx context.Context, id uint64, fields map[string]any) (err error) {
	if len(fields) == 0 {
		return nil
	}
	return m.tx(ctx).Model(&MailAutomationEntity{}).Where("id = ?", id).Updates(fields).Error
}

func (m *MailModel) DeleteAutomation(ctx context.Context, id uint64) (err error) {
	return m.DeleteAutomationTx(ctx, nil, id)
}

func (m *MailModel) DeleteAutomationTx(ctx context.Context, tx *gorm.DB, id uint64) (err error) {
	return m.txOr(ctx, tx).Where("id = ?", id).Delete(&MailAutomationEntity{}).Error
}

func (m *MailModel) CreateRun(ctx context.Context, e *MailAutomationRunEntity) (err error) {
	return m.tx(ctx).Create(e).Error
}

func (m *MailModel) GetRun(ctx context.Context, id uint64) (e *MailAutomationRunEntity, err error) {
	e = &MailAutomationRunEntity{}
	err = m.tx(ctx).Where("id = ?", id).First(e).Error
	return e, err
}

func (m *MailModel) ActiveRun(ctx context.Context, automationID, contactID uint64) (e *MailAutomationRunEntity, err error) {
	e = &MailAutomationRunEntity{}
	err = m.tx(ctx).
		Where("automation_id = ? AND contact_id = ? AND status IN ?", automationID, contactID,
			[]string{RunStatusRunning, RunStatusWaiting}).First(e).Error
	return e, err
}

func (m *MailModel) HasAnyRun(ctx context.Context, automationID, contactID uint64) (exists bool, err error) {
	var id uint64
	res := m.tx(ctx).Model(&MailAutomationRunEntity{}).
		Select("id").
		Where("automation_id = ? AND contact_id = ?", automationID, contactID).
		Limit(1).Scan(&id)
	if res.Error != nil {
		return false, res.Error
	}
	return id != 0, nil
}

func (m *MailModel) ListActiveRunsOfContact(ctx context.Context, contactID uint64) (list []*MailAutomationRunEntity, err error) {
	err = m.tx(ctx).
		Where("contact_id = ? AND status IN ?", contactID,
			[]string{RunStatusRunning, RunStatusWaiting}).
		Order("id ASC").Find(&list).Error
	return list, err
}

func (m *MailModel) UpdateRunFields(ctx context.Context, id uint64, fields map[string]any) (err error) {
	if len(fields) == 0 {
		return nil
	}
	return m.tx(ctx).Model(&MailAutomationRunEntity{}).Where("id = ?", id).Updates(fields).Error
}

func (m *MailModel) ListRuns(ctx context.Context, automationID uint64, status string, offset, limit int) (list []*MailAutomationRunEntity, total int64, err error) {
	q := m.tx(ctx).Model(&MailAutomationRunEntity{})
	if automationID > 0 {
		q = q.Where("automation_id = ?", automationID)
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

func (m *MailModel) DueRuns(ctx context.Context, now time.Time, limit int) (list []*MailAutomationRunEntity, err error) {
	if limit <= 0 {
		limit = 200
	}
	err = m.tx(ctx).
		Where("status = ? AND next_run_at IS NOT NULL AND next_run_at <= ?", RunStatusWaiting, now).
		Order("next_run_at ASC").Limit(limit).Find(&list).Error
	return list, err
}

func (m *MailModel) CountRunsByStatus(ctx context.Context, automationID uint64) (counts map[string]int64, err error) {
	type row struct {
		Status string
		Total  int64
	}
	var rows []row
	q := m.tx(ctx).Model(&MailAutomationRunEntity{}).Select("status, COUNT(*) AS total")
	if automationID > 0 {
		q = q.Where("automation_id = ?", automationID)
	}
	if err = q.Group("status").Scan(&rows).Error; err != nil {
		return nil, err
	}
	counts = map[string]int64{}
	for _, r := range rows {
		counts[r.Status] = r.Total
	}
	return counts, nil
}

func (m *MailModel) StopRunsOfAutomation(ctx context.Context, automationID uint64, at time.Time) (err error) {
	return m.StopRunsOfAutomationTx(ctx, nil, automationID, at)
}

func (m *MailModel) StopRunsOfAutomationTx(ctx context.Context, tx *gorm.DB, automationID uint64, at time.Time) (err error) {
	return m.txOr(ctx, tx).Model(&MailAutomationRunEntity{}).
		Where("automation_id = ? AND status IN ?", automationID, []string{RunStatusRunning, RunStatusWaiting}).
		Updates(map[string]any{"status": RunStatusStopped, "finished_at": at, "update_time": at}).Error
}

type RunRow struct {
	MailAutomationRunEntity
	Email          string  `gorm:"column:email"`
	Name           *string `gorm:"column:contact_name"`
	AutomationName string  `gorm:"column:automation_name"`
}

func (m *MailModel) ListRunRows(ctx context.Context, automationID uint64, status string, offset, limit int) (list []RunRow, total int64, err error) {

	applyFilter := func(q *gorm.DB) *gorm.DB {
		if automationID > 0 {
			q = q.Where("r.automation_id = ?", automationID)
		}
		if s := strings.TrimSpace(status); s != "" {
			q = q.Where("r.status = ?", s)
		}
		return q
	}
	if err = applyFilter(m.tx(ctx).Table("mail_automation_runs AS r")).Count(&total).Error; err != nil {
		return nil, 0, err
	}
	if limit <= 0 {
		limit = 20
	}
	q := m.tx(ctx).Table("mail_automation_runs AS r").
		Select("r.*, c.email AS email, c.name AS contact_name, a.name AS automation_name").
		Joins("LEFT JOIN mail_contacts c ON c.id = r.contact_id").
		Joins("LEFT JOIN mail_automations a ON a.id = r.automation_id")
	err = applyFilter(q).Order("r.id DESC").Offset(offset).Limit(limit).Scan(&list).Error
	return list, total, err
}

func (m *MailModel) RunRowByID(ctx context.Context, runID uint64) (row RunRow, err error) {
	err = m.tx(ctx).Table("mail_automation_runs AS r").
		Select("r.*, c.email AS email, c.name AS contact_name, a.name AS automation_name").
		Joins("LEFT JOIN mail_contacts c ON c.id = r.contact_id").
		Joins("LEFT JOIN mail_automations a ON a.id = r.automation_id").
		Where("r.id = ?", runID).Limit(1).Scan(&row).Error
	return row, err
}

func (m *MailModel) ListRunsOfContact(ctx context.Context, contactID uint64, limit int) (list []RunRow, err error) {
	if limit <= 0 {
		limit = 20
	}
	err = m.tx(ctx).Table("mail_automation_runs AS r").
		Select("r.*, c.email AS email, c.name AS contact_name, a.name AS automation_name").
		Joins("LEFT JOIN mail_contacts c ON c.id = r.contact_id").
		Joins("LEFT JOIN mail_automations a ON a.id = r.automation_id").
		Where("r.contact_id = ?", contactID).
		Order("r.id DESC").Limit(limit).Scan(&list).Error
	return list, err
}

func (m *MailModel) CreateNodeLog(ctx context.Context, e *MailAutomationNodeLogEntity) (err error) {
	return m.tx(ctx).Create(e).Error
}

func (m *MailModel) NodeLogExists(ctx context.Context, runID uint64, nodeKey string) (done bool, err error) {
	var count int64
	err = m.tx(ctx).Model(&MailAutomationNodeLogEntity{}).
		Where("run_id = ? AND node_key = ? AND status = ?", runID, nodeKey, NodeStatusOK).
		Count(&count).Error
	return count > 0, err
}

func (m *MailModel) ContactHasEvent(ctx context.Context, contactID uint64, eventType string) (has bool, err error) {
	var count int64
	err = m.tx(ctx).Model(&MailCampaignEventEntity{}).
		Where("contact_id = ? AND event_type = ?", contactID, eventType).
		Count(&count).Error
	return count > 0, err
}

func (m *MailModel) GetContactTags(ctx context.Context, contactID uint64) (tags StringArray, err error) {
	var row struct{ Tags StringArray }
	err = m.tx(ctx).Model(&MailContactEntity{}).Select("tags").Where("id = ?", contactID).Scan(&row).Error
	return row.Tags, err
}

func (m *MailModel) ListNodeLogs(ctx context.Context, runID uint64, limit int) (list []*MailAutomationNodeLogEntity, err error) {
	if limit <= 0 {
		limit = 100
	}
	err = m.tx(ctx).Where("run_id = ?", runID).Order("id ASC").Limit(limit).Find(&list).Error
	return list, err
}
