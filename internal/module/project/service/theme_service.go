package projectservice

// theme_service.go — 站点前端主题:多套并存,单套激活;页面挂接主题。
//
// 本文件只放主题域的跨用例共享面：业务错误哨兵（errors.Is 判型）与实体 ↔ 响应转换。
// 用例按能力域拆分：
//   - theme_crud.go    主题增删改与激活（含跨工程定位 findThemeForLocate）
//   - theme_list.go    主题列举与激活态读取
//   - theme_default.go 默认主题的幂等补齐

import (
	"errors"

	projectdto "go_wp/internal/module/project/dto"
	projectenums "go_wp/internal/module/project/enums"
	projectmodel "go_wp/internal/module/project/model"
	"go_wp/pkg/utils"
)

// theme 业务错误哨兵（errors.Is 判型；文案统一取 projectenums，不硬编码）。
var (
	ErrThemeNameRequired    = errors.New(projectenums.ErrThemeNameRequired)
	ErrThemeNotFound        = errors.New(projectenums.ErrThemeNotFound)
	ErrThemeIsActive        = errors.New(projectenums.ErrThemeIsActive)
	ErrThemeDuplicateName   = errors.New(projectenums.ErrThemeDuplicateName)
	ErrThemeProjectIDEmpty  = errors.New(projectenums.ErrThemeProjectIDEmpty)
	ErrInvalidThemeSettings = errors.New(projectenums.ErrInvalidThemeSettings)
	// ErrThemeProjectRequired 主题入口无法确定工程作用域（0 个工程；DB-009 第三批）。
	ErrThemeProjectRequired = errors.New(projectenums.ErrProjectRequired)
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
