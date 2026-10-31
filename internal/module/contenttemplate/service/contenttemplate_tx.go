package contenttemplateservice

import (
	"context"
	"errors"
	"github.com/google/uuid"
	contenttemplatecontract "go_wp/internal/module/contenttemplate/contract"
	contenttemplateenums "go_wp/internal/module/contenttemplate/enums"
	contenttemplatemodel "go_wp/internal/module/contenttemplate/model"
	json "go_wp/pkg/jsonx"
	"gorm.io/gorm"
	"time"
)

var _ contenttemplatecontract.DraftDocumentWriterTx = (*Service)(nil)

func (s *Service) LockDraftTx(ctx context.Context, tx *gorm.DB, projectID, id string) (*contenttemplatecontract.LockedDraft, error) {
	e, err := s.m.LockDraftTx(ctx, tx, projectID, id)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, contenttemplateenums.Err(contenttemplateenums.ErrNotFound)
	}
	if err != nil {
		return nil, err
	}
	return &contenttemplatecontract.LockedDraft{ID: e.ID, Version: e.DraftVersion, Document: e.DraftDocument}, nil
}

func (s *Service) SaveDraftDocumentTx(ctx context.Context, tx *gorm.DB, projectID, id string, expectedVersion int64, raw json.RawMessage) error {
	e, err := s.m.LockDraftTx(ctx, tx, projectID, id)
	if err != nil {
		return err
	}
	if e.DraftVersion != expectedVersion {
		return contenttemplateenums.DraftVersionConflict()
	}
	doc, err := s.validateDocument(e.EntityType, raw)
	if err != nil {
		return err
	}
	e.DraftDocument = doc
	e.DraftVersion++
	e.UpdatedAt = time.Now().UTC()
	versionID := uuid.NewString()
	e.CurrentVersionID = &versionID
	err = s.m.SaveWithVersionTx(ctx, tx, projectID, expectedVersion, &contenttemplatemodel.VersionEntity{
		ID: versionID, TemplateID: e.ID, Version: e.DraftVersion, Document: doc, SourceHash: hashDocument(doc), CreatedBy: systemCreator, CreatedAt: e.UpdatedAt,
	}, e)
	if errors.Is(err, contenttemplatemodel.ErrDraftVersionConflict) {
		return contenttemplateenums.DraftVersionConflict()
	}
	return err
}
