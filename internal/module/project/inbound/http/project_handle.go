package projecthttp

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"

	projectcontract "go_wp/internal/module/project/contract"
	projectdto "go_wp/internal/module/project/dto"
	projectenums "go_wp/internal/module/project/enums"
	projectservice "go_wp/internal/module/project/service"
	"go_wp/pkg/binding"
	"go_wp/pkg/logger"
	"go_wp/pkg/response"
)

type Handle struct {
	svc projectcontract.ProjectService
}

func NewHandle(svc projectcontract.ProjectService) *Handle {
	return &Handle{svc: svc}
}

func (h *Handle) Create(c *gin.Context) {
	req, ok := binding.JSON[projectdto.CreateReq](c)
	if !ok {
		return
	}
	res, err := h.svc.Create(c.Request.Context(), req)
	if err != nil {
		projectError(c, err)
		return
	}
	response.SuccessWithMessage(c, projectenums.MsgProjectCreated, res)
}

func (h *Handle) List(c *gin.Context) {
	res, err := h.svc.List(c.Request.Context())
	if err != nil {
		projectError(c, err)
		return
	}
	response.Success(c, res)
}

func (h *Handle) Detail(c *gin.Context) {
	req, ok := binding.Query[projectdto.DetailReq](c)
	if !ok {
		return
	}
	res, err := h.svc.Detail(c.Request.Context(), req)
	if err != nil {
		projectError(c, err)
		return
	}
	response.Success(c, res)
}

func (h *Handle) Update(c *gin.Context) {
	req, ok := binding.JSON[projectdto.UpdateReq](c)
	if !ok {
		return
	}
	res, err := h.svc.Update(c.Request.Context(), req)
	if err != nil {
		projectError(c, err)
		return
	}
	response.SuccessWithMessage(c, projectenums.MsgProjectUpdated, res)
}

func projectError(c *gin.Context, err error) {
	status, message := http.StatusInternalServerError, projectenums.ErrProjectInternal
	switch {
	case errors.Is(err, projectservice.ErrProjectNotFound):
		status, message = http.StatusNotFound, projectenums.ErrProjectNotFound
	case errors.Is(err, projectservice.ErrInvalidName),
		errors.Is(err, projectservice.ErrInvalidSettings),
		errors.Is(err, projectservice.ErrInvalidParam):
		status, message = http.StatusBadRequest, err.Error()
	default:
		logger.Scene("project").Error(err, "工程操作失败")
	}
	response.ErrorWithMessage(c, status, message)
}

type ThemeHandle struct {
	svc *projectservice.Service
}

func themeError(c *gin.Context, err error) {
	status, key := projectErrStatusText(err, projectenums.ErrThemeInternal)
	if status == http.StatusInternalServerError {
		logger.Scene("theme").Error(err, "主题操作失败")
	}
	response.ErrorWithMessage(c, status, key)
}

func (h *ThemeHandle) List(c *gin.Context) {
	projectID := c.Query("projectId")
	if projectID == "" {
		response.ParamError(c, projectenums.ErrThemeProjectIDEmpty)
		return
	}
	res, err := h.svc.ListThemes(c.Request.Context(), projectID)
	if err != nil {
		themeError(c, err)
		return
	}
	response.Success(c, res)
}

func (h *ThemeHandle) Create(c *gin.Context) {
	req, ok := binding.JSON[projectdto.ThemeCreateReq](c)
	if !ok {
		return
	}
	res, err := h.svc.CreateTheme(c.Request.Context(), req)
	if err != nil {
		themeError(c, err)
		return
	}
	response.Success(c, res)
}

func (h *ThemeHandle) Update(c *gin.Context) {
	req, ok := binding.JSON[projectdto.ThemeUpdateReq](c)
	if !ok {
		return
	}
	res, err := h.svc.UpdateTheme(c.Request.Context(), req)
	if err != nil {
		themeError(c, err)
		return
	}
	response.Success(c, res)
}

func (h *ThemeHandle) Activate(c *gin.Context) {
	req, ok := binding.JSON[projectdto.ThemeActivateReq](c)
	if !ok {
		return
	}
	if err := h.svc.ActivateTheme(c.Request.Context(), req); err != nil {
		themeError(c, err)
		return
	}
	response.Success(c, nil)
}

func (h *ThemeHandle) Delete(c *gin.Context) {
	req, ok := binding.JSON[projectdto.ThemeActivateReq](c)
	if !ok {
		return
	}
	if err := h.svc.DeleteTheme(c.Request.Context(), req.ID); err != nil {
		themeError(c, err)
		return
	}
	response.Success(c, nil)
}

func (h *ThemeHandle) Active(c *gin.Context) {
	projectID := c.Query("projectId")
	if projectID == "" {
		response.ParamError(c, projectenums.ErrThemeProjectIDEmpty)
		return
	}
	res, err := h.svc.GetActiveTheme(c.Request.Context(), projectID)
	if err != nil {
		themeError(c, err)
		return
	}
	response.Success(c, res)
}

func paramBindFail(c *gin.Context, err error) {
	if err != nil {

		logger.Scene("project").With("path", c.Request.URL.Path).
			With("detail", err.Error()).Warn("theme 接口请求绑定失败")
	}
	response.ParamError(c)
}
