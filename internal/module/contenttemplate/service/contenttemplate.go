package contenttemplateservice

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	json "go_wp/pkg/jsonx"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"go_wp/pkg/builder"
	blockcontract "go_wp/internal/module/block/contract"
	contenttemplatecontract "go_wp/internal/module/contenttemplate/contract"
	contenttemplatedto "go_wp/internal/module/contenttemplate/dto"
	contenttemplateenums "go_wp/internal/module/contenttemplate/enums"
	contenttemplatemodel "go_wp/internal/module/contenttemplate/model"
	"go_wp/internal/pipeline"
	"go_wp/pkg/logger"
)

func (s *Service) Create(ctx context.Context, req *contenttemplatedto.CreateReq) (res *contenttemplatedto.TemplateResp, err error) {
	if req == nil || !s.validEntityType(req.EntityType) || req.Name == "" {
		return nil, contenttemplateenums.Err(contenttemplateenums.ErrInvalidParam)
	}
	projectID, err := s.resolveProjectID(ctx, req.ProjectID)
	if err != nil {
		return nil, err
	}
	if !json.Valid(req.DraftDocument) {
		return nil, contenttemplateenums.Err(contenttemplateenums.ErrDataInvalid)
	}

	merged, err := pipeline.MergeActiveThemeIntoDocument(ctx, s.project, projectID, req.DraftDocument)
	if err != nil {
		return nil, err
	}

	doc, err := s.validateDocument(req.EntityType, merged)
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	role := strings.TrimSpace(req.TemplateRole)
	if role == "" {
		role = contenttemplatemodel.TemplateRoleDetail
	}
	if !contenttemplatemodel.IsValidTemplateRole(role) {
		return nil, contenttemplateenums.Err(contenttemplateenums.ErrInvalidParam)
	}
	e := &contenttemplatemodel.TemplateEntity{
		ID: uuid.NewString(), ProjectID: projectID, Name: req.Name, EntityType: req.EntityType,
		TemplateRole:  role,
		DraftDocument: doc, DraftVersion: 1, CreatedAt: now, UpdatedAt: now,
	}

	verID := uuid.NewString()
	e.CurrentVersionID = &verID
	if err = s.m.Transaction(ctx, func(tx *gorm.DB) error {
		// 文档里的结构模板绑定与删除共用工程内容写锁，锁内确认模板仍存在。
		if lerr := s.LockAndCheckTemplateRefsTx(ctx, tx, projectID, doc); lerr != nil {
			return lerr
		}
		return s.m.CreateWithVersionTx(ctx, tx, e, &contenttemplatemodel.VersionEntity{
			ID: verID, TemplateID: e.ID, Version: 1, Document: doc,
			SourceHash: hashDocument(doc), CreatedBy: systemCreator, CreatedAt: now,
		})
	}); err != nil {
		return nil, err
	}
	return toResp(e), nil
}

func (s *Service) Update(ctx context.Context, req *contenttemplatedto.UpdateReq) (res *contenttemplatedto.TemplateResp, err error) {
	if req == nil || req.ID == "" {
		return nil, contenttemplateenums.Err(contenttemplateenums.ErrInvalidParam)
	}

	e, err := s.locateTemplate(ctx, req.ID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, contenttemplateenums.Err(contenttemplateenums.ErrNotFound)
		}
		return nil, err
	}
	if !json.Valid(req.DraftDocument) {
		return nil, contenttemplateenums.Err(contenttemplateenums.ErrDataInvalid)
	}
	merged, err := pipeline.MergeActiveThemeIntoDocument(ctx, s.project, e.ProjectID, req.DraftDocument)
	if err != nil {
		return nil, err
	}
	doc, err := s.validateDocument(e.EntityType, merged)
	if err != nil {
		return nil, err
	}
	e.DraftDocument = doc
	e.DraftVersion++
	e.UpdatedAt = time.Now().UTC()
	verID := uuid.NewString()
	e.CurrentVersionID = &verID

	if err = s.m.SaveWithVersion(ctx, e.ProjectID, &contenttemplatemodel.VersionEntity{
		ID: verID, TemplateID: e.ID, Version: e.DraftVersion, Document: doc,
		SourceHash: hashDocument(doc), CreatedBy: systemCreator, CreatedAt: e.UpdatedAt,
	}, e); err != nil {
		if errors.Is(err, contenttemplatemodel.ErrDraftVersionConflict) {
			return nil, contenttemplateenums.DraftVersionConflict()
		}
		return nil, err
	}

	s.notifyTemplateChanged(ctx, e.ID)
	return toResp(e), nil
}

func (s *Service) Activate(ctx context.Context, req *contenttemplatedto.ActivateReq) (res *contenttemplatedto.TemplateResp, err error) {
	if req == nil || strings.TrimSpace(req.ID) == "" {
		return nil, contenttemplateenums.Err(contenttemplateenums.ErrInvalidParam)
	}
	e, err := s.locateTemplate(ctx, req.ID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, contenttemplateenums.Err(contenttemplateenums.ErrNotFound)
		}
		return nil, err
	}
	now := time.Now().UTC()
	if err = s.m.Transaction(ctx, func(tx *gorm.DB) error {
		return s.m.SetDefaultTx(tx, e.ProjectID, e.EntityType, e.ID, now)
	}); err != nil {
		return nil, err
	}
	e.IsDefault = true
	e.UpdatedAt = now
	s.notifyTemplateChanged(ctx, e.ID)
	return toResp(e), nil
}

// Delete 是单条删除：与批量删除共用同一条事务规则（工程内容锁、锁内重验引用、
// 整批原子写入），单条失败时主消息取该条的具体业务原因。
func (s *Service) Delete(ctx context.Context, req *contenttemplatedto.DeleteReq) (err error) {
	if req == nil || strings.TrimSpace(req.ID) == "" {
		return contenttemplateenums.Err(contenttemplateenums.ErrInvalidParam)
	}
	return s.BulkDelete(ctx, &contenttemplatedto.BulkDeleteReq{IDs: []string{req.ID}})
}

func hashDocument(doc []byte) string {
	sum := sha256.Sum256(doc)
	return hex.EncodeToString(sum[:])
}

func (s *Service) validateDocument(entityType string, raw json.RawMessage) (json.RawMessage, error) {
	return s.validateDocumentMode(entityType, raw, true)
}

func (s *Service) validateDocumentStrict(entityType string, raw json.RawMessage) (json.RawMessage, error) {
	return s.validateDocumentMode(entityType, raw, false)
}

func (s *Service) validateDocumentMode(entityType string, raw json.RawMessage, tolerant bool) (json.RawMessage, error) {
	page, err := builder.ParsePage(raw)
	if err != nil {
		return nil, contenttemplateenums.Err(contenttemplateenums.ErrDataInvalid)
	}
	if tolerant {
		if _, err = builder.ValidatePageTolerant(page); err != nil {
			return nil, contenttemplateenums.Err(contenttemplateenums.ErrDataInvalid)
		}
	} else if err = builder.ValidatePage(page); err != nil {
		return nil, contenttemplateenums.Err(contenttemplateenums.ErrDataInvalid)
	}
	if contenttemplatemodel.IsStructureTemplateType(entityType) {

		refs, rerr := builder.CollectFieldRefs(page)
		if rerr != nil {
			return nil, contenttemplateenums.Err(contenttemplateenums.ErrDataInvalid)
		}
		if len(refs) > 0 {
			return nil, contenttemplateenums.Err(contenttemplateenums.ErrFieldBindingInvalid)
		}
	} else if err = builder.ValidateFieldRefs(page, entityType, s.registry); err != nil {
		return nil, contenttemplateenums.ErrDetail(contenttemplateenums.ErrFieldBindingInvalid, err.Error())
	}
	doc, err := json.Marshal(page)
	if err != nil {
		return nil, contenttemplateenums.Err(contenttemplateenums.ErrDataInvalid)
	}
	return doc, nil
}

func (s *Service) ListBlockSourceRefs(ctx context.Context, blockID string) (out []blockcontract.BlockUsage, err error) {
	ids, err := s.fanoutProjectIDs(ctx)
	if err != nil {
		return nil, err
	}
	for _, projectID := range ids {
		if ctx.Err() != nil {
			break
		}
		rows, rerr := s.m.ListBlockDocumentRefs(ctx, projectID, blockID)
		if rerr != nil {
			return nil, rerr
		}
		for i := range rows {
			detail := ""
			if rows[i].FromVersion {
				detail = fmt.Sprintf("version %d", rows[i].Version)
			}
			out = append(out, blockcontract.BlockUsage{
				Kind: blockcontract.UsageKindContentTemplate, ProjectID: projectID,
				EntityID: rows[i].ID, Label: rows[i].Name, Detail: detail,
			})
		}
	}
	return out, nil
}

func (s *Service) Get(ctx context.Context, req *contenttemplatedto.GetReq) (res *contenttemplatedto.TemplateResp, err error) {
	if req == nil || req.ID == "" {
		return nil, contenttemplateenums.Err(contenttemplateenums.ErrInvalidParam)
	}
	e, err := s.locateTemplate(ctx, req.ID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, contenttemplateenums.Err(contenttemplateenums.ErrNotFound)
		}
		return nil, err
	}
	return toResp(e), nil
}

func (s *Service) GetScoped(ctx context.Context, projectID, id string) (res *contenttemplatedto.TemplateResp, err error) {
	e, err := s.m.Get(ctx, projectID, id)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, contenttemplateenums.Err(contenttemplateenums.ErrNotFound)
		}
		return nil, err
	}
	return toResp(e), nil
}

func (s *Service) List(ctx context.Context, req *contenttemplatedto.ListReq) (list []*contenttemplatedto.TemplateResp, err error) {
	if req == nil {
		req = &contenttemplatedto.ListReq{}
	}
	if req.EntityType != "" && !s.validEntityType(req.EntityType) {
		return nil, contenttemplateenums.Err(contenttemplateenums.ErrInvalidType)
	}

	projectID, err := s.resolveProjectID(ctx, req.ProjectID)
	if err != nil {
		return nil, err
	}
	rows, err := s.m.List(ctx, projectID, req.EntityType)
	if err != nil {
		return nil, err
	}
	out := make([]*contenttemplatedto.TemplateResp, 0, len(rows))
	for _, r := range rows {
		out = append(out, toResp(r))
	}
	return out, nil
}

func (s *Service) ResolveTemplate(ctx context.Context, entityType string) (res *contenttemplatecontract.ResolvedTemplate, err error) {
	projectID, err := s.resolveProjectID(ctx, "")
	if err != nil {
		return nil, err
	}
	return s.ResolveTemplateScoped(ctx, projectID, entityType)
}

func (s *Service) ResolveTemplateScoped(ctx context.Context, projectID, entityType string) (res *contenttemplatecontract.ResolvedTemplate, err error) {
	if entityType == "" || !s.validEntityType(entityType) {
		return nil, contenttemplateenums.Err(contenttemplateenums.ErrInvalidType)
	}

	return s.ResolveTemplateByRoleScoped(ctx, projectID, entityType, contenttemplatemodel.TemplateRoleDetail)
}

func (s *Service) ResolveTemplateByRole(ctx context.Context, entityType, role string) (res *contenttemplatecontract.ResolvedTemplate, err error) {
	projectID, err := s.resolveProjectID(ctx, "")
	if err != nil {
		return nil, err
	}
	return s.ResolveTemplateByRoleScoped(ctx, projectID, entityType, role)
}

func (s *Service) ResolveTemplateByRoleScoped(ctx context.Context, projectID, entityType, role string) (res *contenttemplatecontract.ResolvedTemplate, err error) {
	if entityType == "" || !s.validEntityType(entityType) {
		return nil, contenttemplateenums.Err(contenttemplateenums.ErrInvalidType)
	}
	if role != "" && !contenttemplatemodel.IsValidTemplateRole(role) {
		return nil, contenttemplateenums.Err(contenttemplateenums.ErrInvalidParam)
	}
	rows, err := s.m.ListByRole(ctx, projectID, entityType, role)
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, contenttemplateenums.Err(contenttemplateenums.ErrNotFound)
	}
	tpl := rows[0]
	if !tpl.IsDefault {
		logger.Scene("contenttemplate").With("entityType", entityType).With("templateId", tpl.ID).
			Warn("未标记默认模板，回落到最新更新的模板")
	}
	ver, err := s.m.LatestVersion(ctx, tpl.ID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, contenttemplateenums.Err(contenttemplateenums.ErrNotFound)
		}
		return nil, err
	}
	return s.resolvedTemplateFromVersion(tpl, ver)
}

func (s *Service) ResolveTemplateByID(ctx context.Context, templateID string) (res *contenttemplatecontract.ResolvedTemplate, err error) {
	if strings.TrimSpace(templateID) == "" {
		return nil, contenttemplateenums.Err(contenttemplateenums.ErrInvalidParam)
	}

	tpl, err := s.locateTemplate(ctx, templateID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, contenttemplateenums.Err(contenttemplateenums.ErrNotFound)
		}
		return nil, err
	}
	ver, err := s.m.LatestVersion(ctx, tpl.ID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, contenttemplateenums.Err(contenttemplateenums.ErrNotFound)
		}
		return nil, err
	}
	return s.resolvedTemplateFromVersion(tpl, ver)
}

func (s *Service) ResolveTemplateByIDScoped(ctx context.Context, projectID, templateID string) (res *contenttemplatecontract.ResolvedTemplate, err error) {
	if strings.TrimSpace(templateID) == "" {
		return nil, contenttemplateenums.Err(contenttemplateenums.ErrInvalidParam)
	}
	tpl, err := s.m.Get(ctx, projectID, templateID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, contenttemplateenums.Err(contenttemplateenums.ErrNotFound)
		}
		return nil, err
	}
	ver, err := s.m.LatestVersion(ctx, tpl.ID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, contenttemplateenums.Err(contenttemplateenums.ErrNotFound)
		}
		return nil, err
	}
	return s.resolvedTemplateFromVersion(tpl, ver)
}

func (s *Service) resolvedTemplateFromVersion(tpl *contenttemplatemodel.TemplateEntity, ver *contenttemplatemodel.VersionEntity) (*contenttemplatecontract.ResolvedTemplate, error) {
	doc, err := s.validateDocumentStrict(tpl.EntityType, ver.Document)
	if err != nil {
		return nil, err
	}
	return &contenttemplatecontract.ResolvedTemplate{
		TemplateID:   tpl.ID,
		TemplateName: tpl.Name,
		VersionID:    ver.ID,
		Version:      ver.Version,
		EntityType:   tpl.EntityType,
		TemplateRole: tpl.TemplateRole,
		Document:     doc,
	}, nil
}

func (s *Service) SetImpactPort(p contenttemplatecontract.TemplateImpactPort) { s.impact = p }

func (s *Service) Impact(ctx context.Context, req *contenttemplatedto.ImpactReq) (res *contenttemplatedto.ImpactResp, err error) {
	if req == nil {
		req = &contenttemplatedto.ImpactReq{}
	}
	projectID, err := s.resolveProjectID(ctx, req.ProjectID)
	if err != nil {
		return nil, err
	}
	if s.impact == nil {

		return &contenttemplatedto.ImpactResp{Available: false, References: []contenttemplatedto.TemplateReference{}}, nil
	}
	ids, lerr := s.templateIDsOf(ctx, projectID)
	if lerr != nil {
		return nil, lerr
	}
	refs, unparsable, ierr := s.impact.ListTemplateReferences(ctx, projectID, ids)
	if ierr != nil {
		return nil, ierr
	}
	if refs == nil {
		refs = []contenttemplatedto.TemplateReference{}
	}
	return &contenttemplatedto.ImpactResp{Available: true, References: refs, Unparsable: unparsable}, nil
}

func (s *Service) templateIDsOf(ctx context.Context, projectID string) ([]string, error) {
	rows, err := s.m.List(ctx, projectID, "")
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(rows))
	for _, r := range rows {
		if r == nil {
			continue
		}
		if id := strings.TrimSpace(r.ID); id != "" {
			out = append(out, id)
		}
	}
	return out, nil
}
