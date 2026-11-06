package contenttemplateservice

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	contenttemplatecontract "go_wp/internal/module/contenttemplate/contract"
	contenttemplateenums "go_wp/internal/module/contenttemplate/enums"
	contenttemplatemodel "go_wp/internal/module/contenttemplate/model"
	"go_wp/internal/pipeline"
	json "go_wp/pkg/jsonx"
	"go_wp/pkg/i18n"
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
	if err := s.LockAndCheckTemplateRefsTx(ctx, tx, projectID, raw); err != nil {
		return err
	}
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

// LockAndCheckTemplateRefsTx 在模板写事务内先取工程内容写锁，再确认文档新引用的
// 结构模板仍存在：写者与删除共用同一把锁，删除提交后不会再写出悬空绑定。
//
// 文档没有结构模板绑定时不加锁，也不触库。
func (s *Service) LockAndCheckTemplateRefsTx(ctx context.Context, tx *gorm.DB, projectID string, raw json.RawMessage) error {
	ids := TemplateIDsInDocument(raw)
	if len(ids) == 0 {
		return nil
	}
	if err := i18n.LockProjectContentWrite(ctx, tx, projectID); err != nil {
		return err
	}
	missing, err := s.m.MissingTemplateIDsTx(ctx, tx, projectID, ids)
	if err != nil {
		return err
	}
	if len(missing) > 0 {
		return contenttemplatecontract.ErrTemplateGone()
	}
	return nil
}

// TemplateIDsInDocument 返回模板文档 settings.structure 里绑定的结构模板 id。
func TemplateIDsInDocument(raw json.RawMessage) []string {
	if len(raw) == 0 {
		return nil
	}
	bindings, err := pipeline.ParseStructureBindings(raw)
	if err != nil {
		return nil
	}
	ids := make([]string, 0, len(bindings.SlotTemplates)+2)
	seen := map[string]bool{}
	for _, id := range bindings.TemplateBindings() {
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		ids = append(ids, id)
	}
	return ids
}
