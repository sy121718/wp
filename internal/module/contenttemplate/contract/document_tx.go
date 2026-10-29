package contenttemplatecontract

import (
	"context"
	json "go_wp/pkg/jsonx"
	"gorm.io/gorm"
)

// LockedDraft 是外部控制面事务已锁定的模板事实，不暴露本模块 model。
type LockedDraft struct {
	ID       string
	Version  int64
	Document json.RawMessage
}

// DraftDocumentWriterTx 让有限源转换与页面 revision 同事务提交。
// 调用方须先 LockDraftTx；写入仍由模板 service 验证并产生不可变版本。
type DraftDocumentWriterTx interface {
	LockDraftTx(context.Context, *gorm.DB, string, string) (*LockedDraft, error)
	SaveDraftDocumentTx(context.Context, *gorm.DB, string, string, int64, json.RawMessage) error
}
