package projectservice

// theme_service.go — 站点前端主题:多套并存,单套激活;页面挂接主题。

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	projectdto "go_wp/internal/module/project/dto"
	projectmodel "go_wp/internal/module/project/model"
)

var (
	ErrThemeNameRequired = errors.New("主题名称不能为空")
	ErrThemeNotFound     = errors.New("主题不存在")
	ErrThemeIsActive     = errors.New("激活主题不可删除，请先切换到其他主题")
)

// ListThemes 列出工程全部主题。
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

// ListThemesByBlockID 列出页眉/页脚槽位绑定了指定全局块的全部主题。
func (s *Service) ListThemesByBlockID(ctx context.Context, blockID string) (res []projectdto.ThemeResp, err error) {
	entities, err := s.model.ListThemesByBlockID(ctx, blockID)
	if err != nil {
		return nil, err
	}
	res = make([]projectdto.ThemeResp, 0, len(entities))
	for i := range entities {
		res = append(res, toThemeResp(&entities[i]))
	}
	return res, nil
}

// GetTheme 按 ID 取单个主题。
func (s *Service) GetTheme(ctx context.Context, id string) (res *projectdto.ThemeResp, err error) {
	entity, err := s.model.GetTheme(ctx, id)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrThemeNotFound
	}
	if err != nil {
		return nil, err // 基础设施故障原样上抛，不吞成业务错误
	}
	return toThemeRespPtr(entity), nil
}

// CreateTheme 新建主题(名称防重;首个主题自动激活)。
func (s *Service) CreateTheme(ctx context.Context, req *projectdto.ThemeCreateReq) (res *projectdto.ThemeResp, err error) {
	if req == nil || strings.TrimSpace(req.Name) == "" {
		return nil, ErrThemeNameRequired
	}
	if strings.TrimSpace(req.ProjectID) == "" {
		return nil, errors.New("工程 ID 不能为空")
	}
	// settings 必须是 JSON 对象：空值兜底 {}，null/数组/字符串等拒绝（复用工程侧校验）。
	settings, err := normalizeSettings(req.Settings)
	if err != nil {
		return nil, errors.New("无效的主题设置")
	}
	existing, err := s.model.ListThemes(ctx, req.ProjectID)
	if err != nil {
		return nil, err
	}
	for _, e := range existing {
		if strings.EqualFold(e.Name, strings.TrimSpace(req.Name)) {
			return nil, errors.New("同名主题已存在")
		}
	}
	isFirst := len(existing) == 0
	now := time.Now().UTC()
	entity := &projectmodel.ThemeEntity{
		ID: uuid.NewString(), ProjectID: req.ProjectID, Name: strings.TrimSpace(req.Name),
		Settings: settings, IsActive: isFirst, CreatedAt: now, UpdatedAt: now,
	}
	if err = s.model.CreateTheme(ctx, entity); err != nil {
		return nil, err
	}
	res = toThemeRespPtr(entity)
	return res, nil
}

// UpdateTheme 更新主题名称与设置(颜色/字体/页眉页脚引用)。
func (s *Service) UpdateTheme(ctx context.Context, req *projectdto.ThemeUpdateReq) (res *projectdto.ThemeResp, err error) {
	if req == nil || strings.TrimSpace(req.ID) == "" {
		return nil, ErrThemeNotFound
	}
	entity, err := s.model.GetTheme(ctx, req.ID)
	if err != nil {
		return nil, ErrThemeNotFound
	}
	name := entity.Name
	if strings.TrimSpace(req.Name) != "" {
		name = strings.TrimSpace(req.Name)
	}
	settings := entity.Settings
	if len(req.Settings) > 0 {
		normalized, err := normalizeSettings(req.Settings)
		if err != nil {
			return nil, errors.New("无效的主题设置")
		}
		settings = normalized
	}
	if err = s.model.UpdateTheme(ctx, entity.ID, name, settings, time.Now().UTC()); err != nil {
		return nil, err
	}
	entity.Name = name
	entity.Settings = settings
	return toThemeRespPtr(entity), nil
}

// ActivateTheme 激活主题(整站前端切换)。
func (s *Service) ActivateTheme(ctx context.Context, req *projectdto.ThemeActivateReq) (err error) {
	if req == nil || strings.TrimSpace(req.ID) == "" {
		return ErrThemeNotFound
	}
	entity, err := s.model.GetTheme(ctx, req.ID)
	if err != nil {
		return ErrThemeNotFound
	}
	return s.model.ActivateTheme(ctx, entity.ProjectID, entity.ID, time.Now().UTC())
}

// DeleteTheme 删除主题(激活态拒绝)。
func (s *Service) DeleteTheme(ctx context.Context, id string) (err error) {
	if strings.TrimSpace(id) == "" {
		return ErrThemeNotFound
	}
	entity, err := s.model.GetTheme(ctx, id)
	if err != nil {
		return ErrThemeNotFound
	}
	if entity.IsActive {
		return ErrThemeIsActive
	}
	return s.model.DeleteTheme(ctx, id)
}

// GetActiveTheme 取工程当前激活主题。
// 工程尚无主题属合法状态：返回 (nil, nil)，调用方以 theme == nil 判断。
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

func toThemeResp(e *projectmodel.ThemeEntity) projectdto.ThemeResp {
	return projectdto.ThemeResp{
		ID: e.ID, ProjectID: e.ProjectID, Name: e.Name, Settings: e.Settings,
		IsActive: e.IsActive, CreatedAt: e.CreatedAt, UpdatedAt: e.UpdatedAt,
	}
}

func toThemeRespPtr(e *projectmodel.ThemeEntity) *projectdto.ThemeResp {
	r := toThemeResp(e)
	return &r
}
