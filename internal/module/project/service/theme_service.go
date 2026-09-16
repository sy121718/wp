package projectservice

// theme_service.go — 站点前端主题:多套并存,单套激活;页面挂接主题。

import (
	"context"
	"errors"
	"strings"
	"time"

	"encoding/json"
	"github.com/google/uuid"
	"go_wp/internal/builder"
	projectdto "go_wp/internal/module/project/dto"
	projectenums "go_wp/internal/module/project/enums"
	projectmodel "go_wp/internal/module/project/model"
	"go_wp/pkg/logger"
	"go_wp/pkg/utils"
	"gorm.io/gorm"
)

// theme 业务错误哨兵（errors.Is 判型；文案统一取 projectenums，不硬编码）。
var (
	ErrThemeNameRequired    = errors.New(projectenums.ErrThemeNameRequired)
	ErrThemeNotFound        = errors.New(projectenums.ErrThemeNotFound)
	ErrThemeIsActive        = errors.New(projectenums.ErrThemeIsActive)
	ErrThemeDuplicateName   = errors.New(projectenums.ErrThemeDuplicateName)
	ErrThemeProjectIDEmpty  = errors.New(projectenums.ErrThemeProjectIDEmpty)
	ErrInvalidThemeSettings = errors.New(projectenums.ErrInvalidThemeSettings)
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
		return nil, ErrThemeProjectIDEmpty
	}
	// settings 必须是 JSON 对象：空值兜底 {}，null/数组/字符串等拒绝（复用工程侧校验）。
	settings, err := normalizeSettings(req.Settings)
	if err != nil {
		return nil, ErrInvalidThemeSettings
	}
	name := strings.TrimSpace(req.Name)
	// 名称查重：model 层参数化精确查询（大小写不敏感），消除 ListThemes 拉全量。
	exists, err := s.model.ExistsByName(ctx, req.ProjectID, name)
	if err != nil {
		return nil, err
	}
	if exists {
		return nil, ErrThemeDuplicateName
	}
	// 首建自动激活：工程首个主题 is_active=true。
	// 并发首建竞态由部分唯一索引 uq_themes_project_active（迁移 037）兜底——
	// 同工程仅允许一个 is_active=true，多请求并发首建时仅一个成功，其余触发唯一约束错误。
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

// ensureDefaultTheme 工程一套主题都没有时，建一套默认主题（后台设计语言色值）并激活。
//
// 为什么建站就要有主题：继承链是「主题 → 页面 → 组件」，起点不存在时页面拿不到任何令牌、
// 组件只能落到各自的内置 fallback —— 用户还没开始配，前台就已经是一套不属于自己的配色。
// 已有主题（哪怕没激活）时不动：用户自己的选择优先，本函数只补空。
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

// EnsureDefaultThemes 给尚无任何主题的工程补上默认主题（幂等，启动时调用）。
//
// 覆盖存量：本能力上线前建的工程没有主题，页面也就一直没有主题可继承。
// 逐工程补齐而不是批量 SQL —— 主题创建要走名称查重与首建激活的既有逻辑。
func (s *Service) EnsureDefaultThemes(ctx context.Context) (fixed int, err error) {
	projects, err := s.model.ListAll(ctx)
	if err != nil {
		return 0, err
	}
	for _, p := range projects {
		id := p.ID
		created, cerr := s.ensureDefaultTheme(ctx, id)
		if cerr != nil {
			// 单个工程失败不阻断其余：补齐是兜底动作，不是建站主链路。
			logger.Scene("project").With("project", id).Error(cerr, "默认主题补齐失败")
			continue
		}
		if created {
			fixed++
		}
	}
	return fixed, nil
}

// UpdateTheme 更新主题名称与设置(颜色/字体/页眉页脚引用)。
func (s *Service) UpdateTheme(ctx context.Context, req *projectdto.ThemeUpdateReq) (res *projectdto.ThemeResp, err error) {
	if req == nil || strings.TrimSpace(req.ID) == "" {
		return nil, ErrThemeNotFound
	}
	entity, err := s.model.GetTheme(ctx, req.ID)
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
	// 名称查重（排除自身，大小写不敏感）。
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
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrThemeNotFound
		}
		return err
	}
	// 事务内第二步「激活目标」影响 0 行 = 目标在 GetTheme 之后被并发删除，
	// 此时必须回滚（否则全工程落入无激活主题），并映射成「主题不存在」。
	if err = s.model.ActivateTheme(ctx, entity.ProjectID, entity.ID, time.Now().UTC()); err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrThemeNotFound
		}
		return err
	}
	return nil
}

// DeleteTheme 删除主题(激活态拒绝)。
func (s *Service) DeleteTheme(ctx context.Context, id string) (err error) {
	if strings.TrimSpace(id) == "" {
		return ErrThemeNotFound
	}
	entity, err := s.model.GetTheme(ctx, id)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrThemeNotFound
		}
		return err
	}
	if entity.IsActive {
		return ErrThemeIsActive
	}
	// 原子删除：仅当仍为非激活态时删除（WHERE is_active=false），规避 GetTheme 后并发激活的 TOCTOU。
	rows, err := s.model.DeleteTheme(ctx, id)
	if err != nil {
		return err
	}
	if rows == 0 {
		// GetTheme 后被并发激活：部分唯一索引保证单激活，此处按激活态拒绝。
		return ErrThemeIsActive
	}
	return nil
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
		IsActive: e.IsActive, CreatedAt: utils.NewJSONTime(e.CreatedAt), UpdatedAt: utils.NewJSONTime(e.UpdatedAt),
	}
}

func toThemeRespPtr(e *projectmodel.ThemeEntity) *projectdto.ThemeResp {
	r := toThemeResp(e)
	return &r
}
