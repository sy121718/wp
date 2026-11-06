package projectcontract

import (
	"context"

	"gorm.io/gorm"
)

// TemplateRef 是主题设置里仍引用某个模板的事务内事实。
type TemplateRef struct {
	TemplateID string
	ThemeID    string
	ThemeName  string
	Slots      []string
}

// TemplateRefTxReader 在调用方事务内反查主题设置的结构模板绑定。
//
// 实现必须使用传入的 tx 并显式设置工程作用域，供 contenttemplate 的删除事务
// 在持锁后复核引用；不得另开连接或事务。
type TemplateRefTxReader interface {
	ListThemeTemplateRefsTx(ctx context.Context, tx *gorm.DB, projectID string, templateIDs []string) ([]TemplateRef, error)
}
