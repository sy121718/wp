package projecthttp

// theme_http.go — 站点主题 REST:列表/新建/更新/激活/删除/取激活。

import (
	"errors"
	"net/http"

	"go_wp/internal/middleware/builtin"
	projectdto "go_wp/internal/module/project/dto"
	projectenums "go_wp/internal/module/project/enums"
	projectmodel "go_wp/internal/module/project/model"
	service "go_wp/internal/module/project/service"
	"go_wp/internal/permission"
	"go_wp/pkg/logger"
	"go_wp/pkg/response"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// ThemeHandle 主题 HTTP 处理器。
type ThemeHandle struct {
	svc *service.Service
}

// SetupThemeRoutes 注册主题路由（挂 /api 前缀之下——与 035 seed 权限点 /api/theme/* 一致，内部再分 /theme 组）。
func SetupThemeRoutes(rg *permission.RouteGroup, db *gorm.DB) {
	model := projectmodel.NewProjectModel(db)
	svc := service.NewService(model)
	h := &ThemeHandle{svc: svc}

	g := rg.Group("/theme", builtin.SessionAuthMiddleware())
	g.GET("/list", permission.ProjectThemeList, h.List)
	g.POST("/create", permission.ProjectThemeCreate, h.Create)
	g.POST("/update", permission.ProjectThemeUpdate, h.Update)
	g.POST("/activate", permission.ProjectThemeActivate, h.Activate)
	g.POST("/delete", permission.ProjectThemeDelete, h.Delete)
	g.GET("/active", permission.ProjectThemeActive, h.Active)
}

// themeError 将主题业务错误映射为响应状态码与文案：
// 业务哨兵 → 对应 enums 文案；其余（基础设施故障）→ 兜底文案 + 日志留原文，
// 不向客户端泄漏内部错误细节。
func themeError(c *gin.Context, err error) {
	status, message := http.StatusInternalServerError, projectenums.ErrThemeInternal
	switch {
	case errors.Is(err, service.ErrThemeNotFound):
		status, message = http.StatusNotFound, projectenums.ErrThemeNotFound
	case errors.Is(err, service.ErrThemeIsActive),
		errors.Is(err, service.ErrThemeNameRequired),
		errors.Is(err, service.ErrThemeDuplicateName),
		errors.Is(err, service.ErrThemeProjectIDEmpty),
		errors.Is(err, service.ErrInvalidThemeSettings):
		status, message = http.StatusBadRequest, err.Error()
	default:
		logger.Scene("theme").Error(err, "主题操作失败")
	}
	response.ErrorWithMessage(c, status, message)
}

// List 列出工程主题。
func (h *ThemeHandle) List(c *gin.Context) {
	projectID := c.Query("projectId")
	if projectID == "" {
		response.ParamError(c, "缺少 projectId")
		return
	}
	res, err := h.svc.ListThemes(c.Request.Context(), projectID)
	if err != nil {
		themeError(c, err)
		return
	}
	response.Success(c, res)
}

// Create 新建主题。
func (h *ThemeHandle) Create(c *gin.Context) {
	var req projectdto.ThemeCreateReq
	if err := c.ShouldBindJSON(&req); err != nil {
		paramBindFail(c, err)
		return
	}
	res, err := h.svc.CreateTheme(c.Request.Context(), &req)
	if err != nil {
		themeError(c, err)
		return
	}
	response.Success(c, res)
}

// Update 更新主题(设置/名称)。
func (h *ThemeHandle) Update(c *gin.Context) {
	var req projectdto.ThemeUpdateReq
	if err := c.ShouldBindJSON(&req); err != nil {
		paramBindFail(c, err)
		return
	}
	res, err := h.svc.UpdateTheme(c.Request.Context(), &req)
	if err != nil {
		themeError(c, err)
		return
	}
	response.Success(c, res)
}

// Activate 激活主题(整站前端切换)。
func (h *ThemeHandle) Activate(c *gin.Context) {
	var req projectdto.ThemeActivateReq
	if err := c.ShouldBindJSON(&req); err != nil {
		paramBindFail(c, err)
		return
	}
	if err := h.svc.ActivateTheme(c.Request.Context(), &req); err != nil {
		themeError(c, err)
		return
	}
	response.Success(c, nil)
}

// Delete 删除主题。
func (h *ThemeHandle) Delete(c *gin.Context) {
	var req projectdto.ThemeActivateReq
	if err := c.ShouldBindJSON(&req); err != nil {
		paramBindFail(c, err)
		return
	}
	if err := h.svc.DeleteTheme(c.Request.Context(), req.ID); err != nil {
		themeError(c, err)
		return
	}
	response.Success(c, nil)
}

// Active 取当前激活主题。
func (h *ThemeHandle) Active(c *gin.Context) {
	projectID := c.Query("projectId")
	if projectID == "" {
		response.ParamError(c, "缺少 projectId")
		return
	}
	res, err := h.svc.GetActiveTheme(c.Request.Context(), projectID)
	if err != nil {
		themeError(c, err)
		return
	}
	response.Success(c, res)
}

// paramBindFail 请求绑定失败的统一出口（400 + 受控文案）。
//
// 不把绑定错误原文拼进响应：gin 的绑定错误会带上 Go 结构体与字段名
// （如 `json: cannot unmarshal string into Go struct field ThemeCreateReq.name of type string`），
// 那是实现细节 —— 对外只说「参数不合法」，原文进日志供排障。
//
// 原先这里是 `response.ParamError(c, err.Error())`。它一直待在门禁盲区里：形态 ① 只认
// `*.ErrorWithMessage(` 与 `c.String(`，而 `ParamError` 是同一个包里的同族出口却不在判据里。
// 2026-09 第三批把 ParamError 纳入判据后，本模块这几处立刻被扫出来 —— 判据是**形状**，
// 不是字面量；同族出口漏一个就等于那一族都没管住。
func paramBindFail(c *gin.Context, err error) {
	if err != nil {
		// 绑定失败是客户端输入问题，按 warn 记（不污染错误日志）。
		logger.Scene("project").With("path", c.Request.URL.Path).
			With("detail", err.Error()).Warn("theme 接口请求绑定失败")
	}
	response.ParamError(c)
}
