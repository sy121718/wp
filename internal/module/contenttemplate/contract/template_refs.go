package contenttemplatecontract

import (
	"context"
	"net/http"

	"gorm.io/gorm"

	contenttemplateenums "go_wp/internal/module/contenttemplate/enums"
	"go_wp/pkg/facing"
)

// TemplateReferenceRow 是「某实体仍引用某模板」的事务内事实。
//
// Kind 说明引用来源（page / instance / theme / template），EntityID 是引用方实体，
// Label 供失败片段取名，Detail 是槽位等可定位明细；它只用于删除保护，
// 不承载成品界面句。
type TemplateReferenceRow struct {
	TemplateID string
	Kind       string
	EntityID   string
	Label      string
	Detail     string
}

const (
	TemplateReferenceKindPage     = "page"
	TemplateReferenceKindInstance = "instance"
	TemplateReferenceKindTheme    = "theme"
	TemplateReferenceKindTemplate = "template"
)

// TemplateRefQuery 是引用反查的入参：模板 id 用于文档 / 绑定列反查，
// 版本 id 用于按模板版本精确反查（自动发布快照直接外键到版本）。
type TemplateRefQuery struct {
	TemplateIDs []string
	VersionIDs  []string
}

// TemplateReferenceTxReader 在调用方事务内反查模板引用。
//
// 实现方必须使用传入的 tx，并在读取前显式设置工程作用域；不得另开连接或事务。
type TemplateReferenceTxReader interface {
	ListTemplateReferencesTx(ctx context.Context, tx *gorm.DB, projectID string, query *TemplateRefQuery) ([]TemplateReferenceRow, error)
}

// TemplateExistenceTxReader 在写入方事务内确认模板 / 版本仍存在。
//
// 写入方与删除方共用工程内容写锁：持锁写入前用本端口复核，才能消除
// 「校验时存在、提交时已被删除」的 TOCTOU。
type TemplateExistenceTxReader interface {
	MissingTemplateIDsTx(ctx context.Context, tx *gorm.DB, projectID string, templateIDs []string) ([]string, error)
	MissingTemplateVersionIDsTx(ctx context.Context, tx *gorm.DB, projectID string, versionIDs []string) ([]string, error)
}

// ErrTemplateGone 返回 409 的引用失效事实：写者提交的文档仍引用已删除模板。
func ErrTemplateGone() error {
	return facing.WithStatus(facing.NewMessage(contenttemplateenums.ErrTemplateGone, ""), http.StatusConflict)
}
