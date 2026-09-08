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
func SetupThemeRoutes(rg *gin.RouterGroup, db *gorm.DB) {
	model := projectmodel.NewProjectModel(db)
	svc := service.NewService(model)
	h := &ThemeHandle{svc: svc}

	g := rg.Group("/theme", builtin.SessionAuthMiddleware())
	g.GET("/list", h.List)
	g.POST("/create", h.Create)
	g.POST("/update", h.Update)
	g.POST("/activate", h.Activate)
	g.POST("/delete", h.Delete)
	g.GET("/active", h.Active)
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
		response.ParamError(c, err.Error())
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
		response.ParamError(c, err.Error())
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
		response.ParamError(c, err.Error())
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
		response.ParamError(c, err.Error())
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
