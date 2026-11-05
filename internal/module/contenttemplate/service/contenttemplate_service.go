package contenttemplateservice

import (
	"context"
	"errors"
	"strings"

	"gorm.io/gorm"

	"go_wp/pkg/builder"
	"go_wp/pkg/builder/core"
	contenttemplatecontract "go_wp/internal/module/contenttemplate/contract"
	contenttemplatedto "go_wp/internal/module/contenttemplate/dto"
	contenttemplateenums "go_wp/internal/module/contenttemplate/enums"
	contenttemplatemodel "go_wp/internal/module/contenttemplate/model"
	projectcontract "go_wp/internal/module/project/contract"
	"go_wp/internal/pipeline"
	"go_wp/pkg/utils"
)

const systemCreator = "00000000-0000-0000-0000-000000000000"

type Service struct {
	m        *contenttemplatemodel.Model
	project  projectcontract.ProjectService
	registry core.EntitySourceRegistry

	invalidator DependencyInvalidator

	impact contenttemplatecontract.TemplateImpactPort
}

func NewService(m *contenttemplatemodel.Model, project projectcontract.ProjectService,
	registry core.EntitySourceRegistry) *Service {
	return &Service{m: m, project: project, registry: registry}
}

type DependencyInvalidator interface {
	Invalidate(ctx context.Context, kind, key string)
}

func (s *Service) SetInvalidator(inv DependencyInvalidator) { s.invalidator = inv }

func (s *Service) notifyTemplateChanged(ctx context.Context, templateID string) {
	if s == nil || s.invalidator == nil || strings.TrimSpace(templateID) == "" {
		return
	}
	k := pipeline.ContentTemplateKey(templateID)
	s.invalidator.Invalidate(ctx, k.Kind, k.Key)
}

func (s *Service) validEntityType(entityType string) bool {
	if contenttemplatemodel.IsStructureTemplateType(entityType) {
		return true
	}
	return s.registry != nil && s.registry.IsValidType(entityType)
}

var _ contenttemplatecontract.ContentTemplateService = (*Service)(nil)

func structureSlotLabel(slot string) string {
	switch slot {
	case builder.SlotHeader:
		return "页眉"
	case builder.SlotFooter:
		return "页脚"
	default:
		return "结构槽位 " + slot
	}
}

func toResp(e *contenttemplatemodel.TemplateEntity) *contenttemplatedto.TemplateResp {
	return &contenttemplatedto.TemplateResp{
		ID:            e.ID,
		Name:          e.Name,
		EntityType:    e.EntityType,
		TemplateRole:  e.TemplateRole,
		IsDefault:     e.IsDefault,
		DraftVersion:  e.DraftVersion,
		DraftDocument: e.DraftDocument,
		UpdatedAt:     utils.NewJSONTime(e.UpdatedAt),
	}
}

func (s *Service) fanoutProjectIDs(ctx context.Context) ([]string, error) {
	if s == nil || s.project == nil {
		return nil, contenttemplateenums.Err(contenttemplateenums.ErrProjectRequired)
	}
	list, err := s.project.List(ctx)
	if err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(list))
	for i := range list {
		if id := strings.TrimSpace(list[i].ID); id != "" {
			ids = append(ids, id)
		}
	}
	if len(ids) == 0 {
		return nil, contenttemplateenums.Err(contenttemplateenums.ErrProjectRequired)
	}
	return ids, nil
}

func (s *Service) locateTemplate(ctx context.Context, id string) (*contenttemplatemodel.TemplateEntity, error) {
	ids, err := s.fanoutProjectIDs(ctx)
	if err != nil {
		return nil, err
	}
	var lastErr error = gorm.ErrRecordNotFound
	for _, projectID := range ids {
		if ctx.Err() != nil {
			break
		}
		e, gerr := s.m.Get(ctx, projectID, id)
		if gerr == nil {
			return e, nil
		}
		lastErr = gerr
		if !errors.Is(gerr, gorm.ErrRecordNotFound) {
			return nil, gerr
		}
	}
	return nil, lastErr
}

func (s *Service) resolveProjectID(ctx context.Context, explicit string) (string, error) {
	if id := strings.TrimSpace(explicit); id != "" {
		if s.project != nil {
			ok, err := s.project.Exists(ctx, id)
			if err != nil {
				return "", err
			}
			if !ok {
				return "", contenttemplateenums.Err(contenttemplateenums.ErrProjectNotFound)
			}
		}
		return id, nil
	}
	if s.project == nil {
		return "", contenttemplateenums.Err(contenttemplateenums.ErrProjectRequired)
	}
	list, err := s.project.List(ctx)
	if err != nil {
		return "", err
	}
	if len(list) != 1 {
		return "", contenttemplateenums.Err(contenttemplateenums.ErrProjectRequired)
	}
	return list[0].ID, nil
}
