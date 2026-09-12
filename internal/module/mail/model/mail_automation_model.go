package model

// mail_automation_model.go — 自动化流程的表访问单元（issue #38 P3）。
//
// 三张表的生命周期完全不同，方法也按各自的使用方式设计：
//   · 定义：整体读写（编辑器一次保存整张图）；
//   · 实例：并发争抢（同一人同流程只能有一个进行中的，靠部分唯一索引兜底）；
//   · 日志：只写只读、量大。

import (
	"context"
	"strings"
	"time"
)

// 流程状态。
const (
	AutomationStatusDraft  = "draft"
	AutomationStatusActive = "active"
	AutomationStatusPaused = "paused"
)

// 触发方式。
const (
	// TriggerManual 手工把联系人加进流程。
	TriggerManual = "manual"
	// TriggerContactCreated 新联系人产生（导入 / 拉系统用户 / 注册）。
	TriggerContactCreated = "contact_created"
	// TriggerContactSubscribed 联系人变为已订阅。
	TriggerContactSubscribed = "contact_subscribed"
	// TriggerEmailOpened / TriggerEmailClicked 营销邮件的打开 / 点击事件。
	TriggerEmailOpened  = "email_opened"
	TriggerEmailClicked = "email_clicked"
	// TriggerTagAdded 联系人被打上某个标签。
	TriggerTagAdded = "tag_added"
)

// 实例状态。
const (
	RunStatusRunning   = "running"
	RunStatusWaiting   = "waiting"
	RunStatusCompleted = "completed"
	RunStatusFailed    = "failed"
	RunStatusStopped   = "stopped"
)

// 节点日志状态。
const (
	NodeStatusOK      = "ok"
	NodeStatusSkipped = "skipped"
	NodeStatusWaiting = "waiting"
	NodeStatusFailed  = "failed"
)

// MailAutomationEntity 对应 mail_automations 表。
type MailAutomationEntity struct {
	ID            uint64  `gorm:"column:id;primaryKey"`
	Name          string  `gorm:"column:name;type:varchar(150)"`
	Description   *string `gorm:"column:description;type:varchar(500)"`
	TriggerType   string  `gorm:"column:trigger_type;type:varchar(32)"`
	TriggerParams JSONMap `gorm:"column:trigger_params;type:jsonb"`
	// Definition 图定义。用 JSONB 是正当的「自由形状」用法：节点形状各异，
	// 且编辑器整体读写整张图，拆成关联表只会让读写更碎。
	Definition JSONMap    `gorm:"column:definition;type:jsonb"`
	Status     string     `gorm:"column:status;type:varchar(16)"`
	Version    int        `gorm:"column:version"`
	CreateBy   uint64     `gorm:"column:create_by"`
	CreateTime *time.Time `gorm:"column:create_time;type:timestamp(3);autoCreateTime"`
	UpdateTime *time.Time `gorm:"column:update_time;type:timestamp(3)"`
}

// TableName 表名。
func (MailAutomationEntity) TableName() string { return "mail_automations" }

// MailAutomationRunEntity 对应 mail_automation_runs 表。
type MailAutomationRunEntity struct {
	ID                uint64     `gorm:"column:id;primaryKey"`
	AutomationID      uint64     `gorm:"column:automation_id"`
	AutomationVersion int        `gorm:"column:automation_version"`
	ContactID         uint64     `gorm:"column:contact_id"`
	Status            string     `gorm:"column:status;type:varchar(16)"`
	CurrentNode       *string    `gorm:"column:current_node;type:varchar(64)"`
	NextRunAt         *time.Time `gorm:"column:next_run_at;type:timestamp(3)"`
	ErrorMessage      *string    `gorm:"column:error_message"`
	TriggerEvent      *string    `gorm:"column:trigger_event;type:varchar(32)"`
	StartedAt         *time.Time `gorm:"column:started_at;type:timestamp(3)"`
	FinishedAt        *time.Time `gorm:"column:finished_at;type:timestamp(3)"`
	CreateTime        *time.Time `gorm:"column:create_time;type:timestamp(3);autoCreateTime"`
	UpdateTime        *time.Time `gorm:"column:update_time;type:timestamp(3)"`
}

// TableName 表名。
func (MailAutomationRunEntity) TableName() string { return "mail_automation_runs" }

// MailAutomationNodeLogEntity 对应 mail_automation_node_logs 表。
type MailAutomationNodeLogEntity struct {
	ID         uint64     `gorm:"column:id;primaryKey"`
	RunID      uint64     `gorm:"column:run_id"`
	NodeKey    string     `gorm:"column:node_key;type:varchar(64)"`
	NodeType   string     `gorm:"column:node_type;type:varchar(32)"`
	Status     string     `gorm:"column:status;type:varchar(16)"`
	Detail     *string    `gorm:"column:detail"`
	CreateTime *time.Time `gorm:"column:create_time;type:timestamp(3);autoCreateTime"`
}

// TableName 表名。
func (MailAutomationNodeLogEntity) TableName() string { return "mail_automation_node_logs" }

// ---- 定义 ----

// CreateAutomation 新建流程。
func (m *MailModel) CreateAutomation(ctx context.Context, e *MailAutomationEntity) (err error) {
	return m.tx(ctx).Create(e).Error
}

// GetAutomation 按主键取流程。
func (m *MailModel) GetAutomation(ctx context.Context, id uint64) (e *MailAutomationEntity, err error) {
	e = &MailAutomationEntity{}
	err = m.tx(ctx).Where("id = ?", id).First(e).Error
	return e, err
}

// ListAutomations 列流程（状态可空）。
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

// ListActiveAutomations 取所有启用中的流程（事件触发时用来匹配）。
func (m *MailModel) ListActiveAutomations(ctx context.Context) (list []*MailAutomationEntity, err error) {
	err = m.tx(ctx).Where("status = ?", AutomationStatusActive).Order("id ASC").Find(&list).Error
	return list, err
}

// UpdateAutomationFields 按主键更新指定列。
func (m *MailModel) UpdateAutomationFields(ctx context.Context, id uint64, fields map[string]any) (err error) {
	if len(fields) == 0 {
		return nil
	}
	return m.tx(ctx).Model(&MailAutomationEntity{}).Where("id = ?", id).Updates(fields).Error
}

// DeleteAutomation 删除流程（进行中的实例由 service 先停止）。
func (m *MailModel) DeleteAutomation(ctx context.Context, id uint64) (err error) {
	return m.tx(ctx).Where("id = ?", id).Delete(&MailAutomationEntity{}).Error
}

// ---- 实例 ----

// CreateRun 建实例。
//
// 同一人同流程只允许一个进行中的实例（部分唯一索引兜底）：反复触发（开一次邮件、点一次链接）
// 若各自建实例，同一个人会收到多轮自动化邮件。冲突时返回 gorm.ErrDuplicatedKey 由 service 当作「已在流程中」。
func (m *MailModel) CreateRun(ctx context.Context, e *MailAutomationRunEntity) (err error) {
	return m.tx(ctx).Create(e).Error
}

// GetRun 按主键取实例。
func (m *MailModel) GetRun(ctx context.Context, id uint64) (e *MailAutomationRunEntity, err error) {
	e = &MailAutomationRunEntity{}
	err = m.tx(ctx).Where("id = ?", id).First(e).Error
	return e, err
}

// ActiveRun 取某人某流程的进行中实例（不存在返回 gorm.ErrRecordNotFound）。
func (m *MailModel) ActiveRun(ctx context.Context, automationID, contactID uint64) (e *MailAutomationRunEntity, err error) {
	e = &MailAutomationRunEntity{}
	err = m.tx(ctx).
		Where("automation_id = ? AND contact_id = ? AND status IN ?", automationID, contactID,
			[]string{RunStatusRunning, RunStatusWaiting}).First(e).Error
	return e, err
}

// UpdateRunFields 按主键更新实例。
func (m *MailModel) UpdateRunFields(ctx context.Context, id uint64, fields map[string]any) (err error) {
	if len(fields) == 0 {
		return nil
	}
	return m.tx(ctx).Model(&MailAutomationRunEntity{}).Where("id = ?", id).Updates(fields).Error
}

// ListRuns 列某流程的实例（状态可空）。
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

// DueRuns 取到点该唤醒的等待中实例（延时调度用）。
func (m *MailModel) DueRuns(ctx context.Context, now time.Time, limit int) (list []*MailAutomationRunEntity, err error) {
	if limit <= 0 {
		limit = 200
	}
	err = m.tx(ctx).
		Where("status = ? AND next_run_at IS NOT NULL AND next_run_at <= ?", RunStatusWaiting, now).
		Order("next_run_at ASC").Limit(limit).Find(&list).Error
	return list, err
}

// CountRunsByStatus 按状态统计实例数（后台概览）。
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

// StopRunsOfAutomation 停止某流程所有进行中的实例（删流程前调用，避免留下孤儿实例）。
func (m *MailModel) StopRunsOfAutomation(ctx context.Context, automationID uint64, at time.Time) (err error) {
	return m.tx(ctx).Model(&MailAutomationRunEntity{}).
		Where("automation_id = ? AND status IN ?", automationID, []string{RunStatusRunning, RunStatusWaiting}).
		Updates(map[string]any{"status": RunStatusStopped, "finished_at": at, "update_time": at}).Error
}

// ---- 节点日志 ----

// CreateNodeLog 写一条节点日志。
func (m *MailModel) CreateNodeLog(ctx context.Context, e *MailAutomationNodeLogEntity) (err error) {
	return m.tx(ctx).Create(e).Error
}

// ListNodeLogs 列某实例的节点日志（按执行顺序）。
func (m *MailModel) ListNodeLogs(ctx context.Context, runID uint64, limit int) (list []*MailAutomationNodeLogEntity, err error) {
	if limit <= 0 {
		limit = 100
	}
	err = m.tx(ctx).Where("run_id = ?", runID).Order("id ASC").Limit(limit).Find(&list).Error
	return list, err
}
