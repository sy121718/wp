package contenttemplatecontract

import (
	"context"
	json "go_wp/pkg/jsonx"
	"gorm.io/gorm"
)

type LockedDraft struct {
	ID       string
	Version  int64
	Document json.RawMessage
}

type DraftDocumentWriterTx interface {
	LockDraftTx(context.Context, *gorm.DB, string, string) (*LockedDraft, error)
	SaveDraftDocumentTx(context.Context, *gorm.DB, string, string, int64, json.RawMessage) error
}
