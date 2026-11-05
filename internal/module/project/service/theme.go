package projectservice

import (
	"context"
	"errors"
	json "go_wp/pkg/jsonx"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"go_wp/pkg/builder"
	projectdto "go_wp/internal/module/project/dto"
	projectenums "go_wp/internal/module/project/enums"
	projectmodel "go_wp/internal/module/project/model"
	"go_wp/pkg/logger"
	"go_wp/pkg/utils"
)

func (s *Service) findThemeForLocate(ctx context.Context, id, explicitProjectID string) (*projectmodel.ThemeEntity, error) {
	if pid := strings.TrimSpace(explicitProjectID); pid != "" {
		return s.model.GetTheme(ctx, pid, id)
	}
	projects, err := s.model.ListAll(ctx)
	if err != nil {
		return nil, err
	}
	for _, p := range projects {
		if ctx.Err() != nil {
			break
		}
		e, gerr := s.model.GetTheme(ctx, p.ID, id)
		if gerr == nil {
			return e, nil
		}
		if !errors.Is(gerr, gorm.ErrRecordNotFound) {
			return nil, gerr
		}
	}
	return nil, gorm.ErrRecordNotFound
}

func (s *Service) GetTheme(ctx context.Context, id string) (res *projectdto.ThemeResp, err error) {
	entity, err := s.findThemeForLocate(ctx, id, "")
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrThemeNotFound
	}
	if err != nil {
		return nil, err
	}
	return toThemeRespPtr(entity), nil
}

func (s *Service) CreateTheme(ctx context.Context, req *projectdto.ThemeCreateReq) (res *projectdto.ThemeResp, err error) {
	if req == nil || strings.TrimSpace(req.Name) == "" {
		return nil, ErrThemeNameRequired
	}
	if strings.TrimSpace(req.ProjectID) == "" {
		return nil, ErrThemeProjectIDEmpty
	}

	settings, err := normalizeSettings(req.Settings)
	if err != nil {
		return nil, ErrInvalidThemeSettings
	}
	name := strings.TrimSpace(req.Name)

	exists, err := s.model.ExistsByName(ctx, req.ProjectID, name)
	if err != nil {
		return nil, err
	}
	if exists {
		return nil, ErrThemeDuplicateName
	}

	count, err := s.model.CountThemes(ctx, req.ProjectID)
	if err != nil {
		return nil, err
	}
	isFirst := count == 0
	now := time.Now().UTC()
	entity := &projectmodel.ThemeEntity{
		ID: uuid.NewString(), ProjectID: req.ProjectID, Name: name,
		Settings: settings, IsActive: isFirst, CreatedAt: now, UpdatedAt: now,
	}
	if err = s.model.CreateTheme(ctx, entity); err != nil {
		return nil, err
	}
	res = toThemeRespPtr(entity)
	return res, nil
}

func (s *Service) UpdateTheme(ctx context.Context, req *projectdto.ThemeUpdateReq) (res *projectdto.ThemeResp, err error) {
	if req == nil || strings.TrimSpace(req.ID) == "" {
		return nil, ErrThemeNotFound
	}

	entity, err := s.findThemeForLocate(ctx, req.ID, "")
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrThemeNotFound
		}
		return nil, err
	}
	name := entity.Name
	if strings.TrimSpace(req.Name) != "" {
		name = strings.TrimSpace(req.Name)
	}

	if name != entity.Name {
		exists, err := s.model.ExistsByName(ctx, entity.ProjectID, name, entity.ID)
		if err != nil {
			return nil, err
		}
		if exists {
			return nil, ErrThemeDuplicateName
		}
	}
	settings := entity.Settings
	if len(req.Settings) > 0 {
		normalized, err := normalizeSettings(req.Settings)
		if err != nil {
			return nil, ErrInvalidThemeSettings
		}
		settings = normalized
	}
	if err = s.model.UpdateTheme(ctx, entity.ProjectID, entity.ID, name, settings, time.Now().UTC()); err != nil {
		return nil, err
	}
	entity.Name = name
	entity.Settings = settings
	return toThemeRespPtr(entity), nil
}

func (s *Service) ActivateTheme(ctx context.Context, req *projectdto.ThemeActivateReq) (err error) {
	if req == nil || strings.TrimSpace(req.ID) == "" {
		return ErrThemeNotFound
	}

	entity, err := s.findThemeForLocate(ctx, req.ID, "")
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrThemeNotFound
		}
		return err
	}

	if err = s.model.ActivateTheme(ctx, entity.ProjectID, entity.ID, time.Now().UTC()); err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrThemeNotFound
		}
		return err
	}
	return nil
}

func (s *Service) DeleteTheme(ctx context.Context, id string) (err error) {
	if strings.TrimSpace(id) == "" {
		return ErrThemeNotFound
	}
	entity, err := s.findThemeForLocate(ctx, id, "")
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrThemeNotFound
		}
		return err
	}
	if entity.IsActive {
		return ErrThemeIsActive
	}

	rows, err := s.model.DeleteTheme(ctx, entity.ProjectID, id)
	if err != nil {
		return err
	}
	if rows == 0 {

		return ErrThemeIsActive
	}
	return nil
}

func (s *Service) ensureDefaultTheme(ctx context.Context, projectID string) (created bool, err error) {
	count, err := s.model.CountThemes(ctx, projectID)
	if err != nil {
		return false, err
	}
	if count > 0 {
		return false, nil
	}
	raw, err := json.Marshal(builder.DefaultThemeSettings())
	if err != nil {
		return false, err
	}
	if _, err = s.CreateTheme(ctx, &projectdto.ThemeCreateReq{
		ProjectID: projectID,
		Name:      builder.DefaultThemeName,
		Settings:  raw,
	}); err != nil {
		return false, err
	}
	return true, nil
}

func (s *Service) EnsureDefaultThemes(ctx context.Context) (fixed int, err error) {
	projects, err := s.model.ListAll(ctx)
	if err != nil {
		return 0, err
	}
	for _, p := range projects {
		id := p.ID
		created, cerr := s.ensureDefaultTheme(ctx, id)
		if cerr != nil {

			logger.Scene("project").With("project", id).Error(cerr, "默认主题补齐失败")
			continue
		}
		if created {
			fixed++
		}
	}
	return fixed, nil
}

func (s *Service) ListThemes(ctx context.Context, projectID string) (res []projectdto.ThemeResp, err error) {
	entities, err := s.model.ListThemes(ctx, projectID)
	if err != nil {
		return nil, err
	}
	res = make([]projectdto.ThemeResp, 0, len(entities))
	for i := range entities {
		res = append(res, toThemeResp(&entities[i]))
	}
	return res, nil
}

func (s *Service) ListThemesByBlockID(ctx context.Context, blockID string) (res []projectdto.ThemeResp, err error) {

	projects, err := s.model.ListAll(ctx)
	if err != nil {
		return nil, err
	}
	if len(projects) == 0 {
		return nil, ErrThemeProjectRequired
	}
	var entities []projectmodel.ThemeEntity
	for _, p := range projects {
		if ctx.Err() != nil {
			break
		}
		hit, herr := s.model.ListThemesByBlockID(ctx, p.ID, blockID)
		if herr != nil {
			return nil, herr
		}
		entities = append(entities, hit...)
	}
	res = make([]projectdto.ThemeResp, 0, len(entities))
	for i := range entities {
		res = append(res, toThemeResp(&entities[i]))
	}
	return res, nil
}

func (s *Service) GetActiveTheme(ctx context.Context, projectID string) (res *projectdto.ThemeResp, err error) {
	entity, err := s.model.GetActiveTheme(ctx, projectID)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return toThemeRespPtr(entity), nil
}

var (
	ErrThemeNameRequired    = projectenums.Err(projectenums.ErrThemeNameRequired)
	ErrThemeNotFound        = projectenums.Err(projectenums.ErrThemeNotFound)
	ErrThemeIsActive        = projectenums.Err(projectenums.ErrThemeIsActive)
	ErrThemeDuplicateName   = projectenums.Err(projectenums.ErrThemeDuplicateName)
	ErrThemeProjectIDEmpty  = projectenums.Err(projectenums.ErrThemeProjectIDEmpty)
	ErrInvalidThemeSettings = projectenums.Err(projectenums.ErrInvalidThemeSettings)

	ErrThemeProjectRequired = projectenums.Err(projectenums.ErrProjectRequired)
)

func toThemeResp(e *projectmodel.ThemeEntity) projectdto.ThemeResp {
	return projectdto.ThemeResp{
		ID: e.ID, ProjectID: e.ProjectID, Name: e.Name, Settings: e.Settings,
		IsActive: e.IsActive, CreatedAt: utils.NewJSONTime(e.CreatedAt), UpdatedAt: utils.NewJSONTime(e.UpdatedAt),
	}
}

func toThemeRespPtr(e *projectmodel.ThemeEntity) *projectdto.ThemeResp {
	r := toThemeResp(e)
	return &r
}
