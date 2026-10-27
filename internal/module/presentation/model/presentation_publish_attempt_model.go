package presentationmodel

// presentation_publish_attempt_model.go — 发布尝试明细的写入与游标读（迁移 671）。
//
// 为什么是独立表而不是扩 publication_receipts：那张表是**恢复用的补偿台账**
// （pending → committed / rolled_back，只覆盖「访问面切换」那一段，成功后只留终态），
// 它的列集合回答不了「谁在什么时候因为什么发了 / 没发出去」。两者服务不同的读者：
// 回执服务启动恢复，本表服务人。
//
// 为什么记失败：发布失败时事务整体回滚，presentation_artifacts 里连行都没有 ——
// 「昨晚发了三次为什么线上还是旧版」在改前没有任何数据可答。所以本表的写入点必须在
// 事务**之外**（见 presentation_render.go 的 recordPublishAttempt），宁可极端情况下丢
// 最后一条（进程被 kill），也要保证失败记录一定留下。

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

// 触发源取值（与迁移 671 的 ck_publish_attempts_trigger 逐字一致）。
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

// 结局取值。
const (
	PublishOutcomeSuccess = "success"
	PublishOutcomeFailed  = "failed"
)

// PublishAttemptEntity 对应 presentation_publish_attempts 表。
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

// LangArray 本条尝试涉及的语言（PG text[] 列）。
//
// 与 page / mail 模块同名的类型是**有意重复**：跨模块不得 import 对方的 model（AGENTS.md
// 分层判据）。为什么不用裸 []string 或 pgtype.FlatArray —— 那两个在本仓实测都失败，
// 结论见 internal/module/page/model/page_langs_model.go 的注释（pgx 元组字面量 22P02 /
// GORM 走 database/sql 路径不识别 pgtype → 42804）。所以直接产出与解析 PG 数组字面量。
type LangArray []string

// Value 实现 driver.Valuer：nil 与空切片都写 '{}'（列是 NOT NULL DEFAULT '{}'）。
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

// Scan 实现 sql.Scanner：接受 PG 返回的数组字面量文本。
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

// PublishAttemptDB 绑定本表。
func (m *Model) PublishAttemptDB(ctx context.Context) *gorm.DB {
	return m.db.WithContext(ctx).Model(&PublishAttemptEntity{})
}

// RecordPublishAttempt 写一条发布尝试（工程作用域内）。
//
// ctx 必须已带工程 scope（rls.InProjectScope）：本表在 671 里开了 FORCE RLS，
// 缺 scope 的写入会被策略拒绝 —— 这正是要的（fail closed，而不是写进别人的工程）。
func (m *Model) RecordPublishAttempt(ctx context.Context, e *PublishAttemptEntity) error {
	return rls.InProjectScope(ctx, m.db, e.ProjectID, func(tx *gorm.DB) error {
		return tx.Create(e).Error
	})
}

// PublishAttemptCursor 倒序游标：(create_time, id) 二元组。
//
// 为什么不用 OFFSET：本表只增，翻页期间上层持续写入会让 OFFSET 跳行/重行。
// 取 (create_time DESC, id DESC) 的严格小于条件，插入的新行只会出现在第一页。
type PublishAttemptCursor struct {
	CreatedAt time.Time
	ID        int64
}

// Valid 游标是否可用（零值 = 从第一页开始）。
func (c PublishAttemptCursor) Valid() bool { return !c.CreatedAt.IsZero() }

// ListPublishAttempts 按**实体**倒序取一页（limit 上限由调用方定）。
//
// 按实体而不是按实例：一个商品可以是两张页面（detail 详情页 + archive 归档页），
// 运营问的是「这个商品发布过几次」，把两张页面的尝试拆到两个抽屉里没人看得懂。
// 索引 idx_publish_attempts_entity 就是为这条查询建的。
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

// CountPublishAttempts 该实体的尝试总数（页面显示「共 N 次」）。
func (m *Model) CountPublishAttempts(ctx context.Context, projectID, entityType, entityID string) (total int64, err error) {
	err = rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {
		return tx.Model(&PublishAttemptEntity{}).
			Where("entity_type = ? AND entity_id = ?", entityType, entityID).Count(&total).Error
	})
	return total, err
}
