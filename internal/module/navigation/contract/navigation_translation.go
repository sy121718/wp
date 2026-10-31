package navigationcontract

import (
	"context"
	"time"

	"gorm.io/gorm"
)

type PageStalePort interface {
	MarkMenuStaleTx(ctx context.Context, tx *gorm.DB, projectID, kind string, at time.Time) (n int64, err error)
}

type PresentationStalePort interface {
	MarkInstanceMenuStaleTx(ctx context.Context, tx *gorm.DB, projectID, kind string, at time.Time) (n int64, err error)
}

type MenuItemStalePort interface {
	MarkNavigationStaleTx(ctx context.Context, tx *gorm.DB, projectID, navigationID string) (n int64, err error)
}

type MenuItemPresentationStalePort interface {
	MarkInstanceNavigationStaleTx(ctx context.Context, tx *gorm.DB, projectID, navigationID string, at time.Time) (n int64, err error)
}

type MenuStaleEnqueuePort interface {
	EnqueueMenuStaleTx(ctx context.Context, tx *gorm.DB, projectID, kind string) (inputVersion int64, err error)
}

type TranslationWriteResult struct {
	Written int `json:"written"`

	ChangedKinds []string `json:"changedKinds"`
}

type TranslationWriteService interface {
	WriteTranslations(ctx context.Context, req *TranslationWriteReq) (*TranslationWriteResult, error)
}

type TranslationWriteReq struct {
	ProjectID string

	Lang string

	Rows []TranslationWriteRow
}

type TranslationWriteRow struct {
	Context string

	Hash string

	Target string
}
