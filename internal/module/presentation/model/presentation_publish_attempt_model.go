package presentationmodel

import (
	"context"
	"database/sql/driver"
	"fmt"
	"strings"
	"time"

	"gorm.io/gorm"

	"go_wp/pkg/rls"
)

const tableNamePublishAttempts = "presentation_publish_attempts"

const (
	PublishTriggerCreate   = "create"
	PublishTriggerManual   = "manual"
	PublishTriggerSwitch   = "template_switch"
	PublishTriggerURL      = "update_url"
	PublishTriggerRollback = "rollback"
	PublishTriggerOverride = "override"
	PublishTriggerAuto     = "auto_rebuild"
	PublishTriggerDepends  = "dependency_rebuild"
)

const (
	PublishOutcomeSuccess = "success"
	PublishOutcomeFailed  = "failed"
)

type PublishAttemptEntity struct {
	ID              int64     `gorm:"column:id;primaryKey"`
	ProjectID       string    `gorm:"column:project_id;not null"`
	InstanceID      string    `gorm:"column:instance_id;not null"`
	EntityType      string    `gorm:"column:entity_type;not null"`
	EntityID        string    `gorm:"column:entity_id;not null"`
	Trigger         string    `gorm:"column:trigger;not null"`
	Outcome         string    `gorm:"column:outcome;not null"`
	ErrorText       string    `gorm:"column:error_text;not null"`
	Langs           LangArray `gorm:"column:langs;not null"`
	Attempts        int       `gorm:"column:attempts;not null"`
	ArtifactVersion int64     `gorm:"column:artifact_version;not null"`
	ArtifactHash    string    `gorm:"column:artifact_hash;not null"`
	OperatorID      int64     `gorm:"column:operator_id;not null"`
	OperatorName    string    `gorm:"column:operator_name;not null"`
	DurationMs      int64     `gorm:"column:duration_ms;not null"`
	CreatedAt       time.Time `gorm:"column:create_time;not null"`
}

func (PublishAttemptEntity) TableName() string { return tableNamePublishAttempts }

type LangArray []string

func (a LangArray) Value() (driver.Value, error) {
	if len(a) == 0 {
		return "{}", nil
	}
	parts := make([]string, 0, len(a))
	for _, v := range a {
		esc := strings.ReplaceAll(v, "\\", "\\\\")
		esc = strings.ReplaceAll(esc, "\"", "\\\"")
		parts = append(parts, "\""+esc+"\"")
	}
	return "{" + strings.Join(parts, ",") + "}", nil
}

func (a *LangArray) Scan(src any) error {
	if src == nil {
		*a = LangArray{}
		return nil
	}
	var raw string
	switch v := src.(type) {
	case string:
		raw = v
	case []byte:
		raw = string(v)
	default:
		return fmt.Errorf("LangArray: 不支持的来源类型 %T", src)
	}
	s := strings.TrimSpace(raw)
	if s == "" || s == "{}" {
		*a = LangArray{}
		return nil
	}
	if !strings.HasPrefix(s, "{") || !strings.HasSuffix(s, "}") {
		return fmt.Errorf("LangArray: 非法数组字面量 %q", raw)
	}
	body := s[1 : len(s)-1]
	out := LangArray{}
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
		return fmt.Errorf("LangArray: 数组字面量引号未闭合: %q", raw)
	}
	if started || cur.Len() > 0 {
		out = append(out, cur.String())
	}
	*a = out
	return nil
}

func (m *Model) PublishAttemptDB(ctx context.Context) *gorm.DB {
	return m.db.WithContext(ctx).Model(&PublishAttemptEntity{})
}

func (m *Model) RecordPublishAttempt(ctx context.Context, e *PublishAttemptEntity) error {
	return rls.InProjectScope(ctx, m.db, e.ProjectID, func(tx *gorm.DB) error {
		return tx.Create(e).Error
	})
}

type PublishAttemptCursor struct {
	CreatedAt time.Time
	ID        int64
}

func (c PublishAttemptCursor) Valid() bool { return !c.CreatedAt.IsZero() }

func (m *Model) ListPublishAttempts(ctx context.Context, projectID, entityType, entityID string,
	limit int) (list []PublishAttemptEntity, err error) {
	list = []PublishAttemptEntity{}
	err = rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {
		return tx.Model(&PublishAttemptEntity{}).
			Where("entity_type = ? AND entity_id = ?", entityType, entityID).
			Order("create_time DESC, id DESC").Limit(limit).Find(&list).Error
	})
	return list, err
}

func (m *Model) CountPublishAttempts(ctx context.Context, projectID, entityType, entityID string) (total int64, err error) {
	err = rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {
		return tx.Model(&PublishAttemptEntity{}).
			Where("entity_type = ? AND entity_id = ?", entityType, entityID).Count(&total).Error
	})
	return total, err
}
