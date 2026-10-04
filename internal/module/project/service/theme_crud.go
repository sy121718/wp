package projectservice

// theme_crud.go — 主题的增删改与激活（含跨工程定位口径）。
//
// findThemeForLocate 是这四个入口共用的定位辅助（DB-009 第三批）：themes 带 FORCE 策略，
// 而契约方法签名里没有工程参数，故按主键 id 逐工程独立作用域探测，命中即返回。

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

// findThemeForLocate 按 id 定位主题：逐工程独立作用域探测（DB-009 第三批）。
//
// 为什么不能再靠「唯一工程」：themes 带 FORCE 策略，作用域必须是一个具体 uuid，而主题
// 契约方法（GetTheme/UpdateTheme/ActivateTheme/DeleteTheme）签名里没有工程参数（后台页面
// 直接依赖该契约）。原来的 soleProjectID 只在「工程表恰好一个工程」时猜得出作用域，
// 多工程部署下会退到「不限工程」形态 —— 那是换非超级角色后的静默「主题不存在」，
// 也是本批要消灭的默认路径。
//
// 现在改成逐工程探测：themes.id 是主键（跨工程不会重复命中），每个工程各自一次独立
// 作用域的事务，命中即返回。多工程部署下这些入口不再退化，也不存在「不限工程」分支。
// 调用方若本来就持有工程 id（从 URL / 会话 / 页面设置里带出来），应当显式传入
// explicitProjectID —— 那就只需一次查询，属性校验也不会跨工程碰运气。
// 工程表为空时返回 gorm.ErrRecordNotFound（没有工程就没有主题），由调用方映射成
// 「主题不存在」；连工程表都读不出来时上抛基础设施错误，不吞成业务错误。
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

// GetTheme 按 ID 取单个主题。
func (s *Service) GetTheme(ctx context.Context, id string) (res *projectdto.ThemeResp, err error) {
	entity, err := s.findThemeForLocate(ctx, id, "")
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

// UpdateTheme 更新主题名称与设置(颜色/字体/页眉页脚引用)。
func (s *Service) UpdateTheme(ctx context.Context, req *projectdto.ThemeUpdateReq) (res *projectdto.ThemeResp, err error) {
	if req == nil || strings.TrimSpace(req.ID) == "" {
		return nil, ErrThemeNotFound
	}
	// 显式工程（请求带来时）优先；没有则逐工程探测定位（见 findThemeForLocate）。
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
	if err = s.model.UpdateTheme(ctx, entity.ProjectID, entity.ID, name, settings, time.Now().UTC()); err != nil {
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
	// 显式工程（请求带来时）优先；没有则逐工程探测定位（见 findThemeForLocate）。
	entity, err := s.findThemeForLocate(ctx, req.ID, "")
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
	// 原子删除：仅当仍为非激活态时删除（WHERE is_active=false），规避 GetTheme 后并发激活的 TOCTOU。
	rows, err := s.model.DeleteTheme(ctx, entity.ProjectID, id)
	if err != nil {
		return err
	}
	if rows == 0 {
		// GetTheme 后被并发激活：部分唯一索引保证单激活，此处按激活态拒绝。
		return ErrThemeIsActive
	}
	return nil
}
