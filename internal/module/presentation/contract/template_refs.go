package presentationcontract

import (
	"context"

	"gorm.io/gorm"
)

// TemplateRef 是自动发布实例侧仍引用某个模板 / 模板版本的事务内事实。
type TemplateRef struct {
	TemplateID string
	InstanceID string
	EntityType string
	EntityID   string
	URLPath    string
	Slots      []string
}

// TemplateRefTxReader 在调用方事务内反查实例绑定、独立文档与快照的模板版本外键。
//
// 实现必须使用传入的 tx 并显式设置工程作用域，供 contenttemplate 的删除事务
// 在持锁后复核引用；不得另开连接或事务。
type TemplateRefTxReader interface {
	ListInstanceTemplateRefsTx(ctx context.Context, tx *gorm.DB, projectID string, templateIDs, versionIDs []string) ([]TemplateRef, error)
}
