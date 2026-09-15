// Package pluginhttp 插件模块 HTTP 入口。
package pluginhttp

import (
	"io"
	"net/http"

	"github.com/gin-gonic/gin"

	plugincontract "go_wp/internal/module/plugin/contract"
	plugindto "go_wp/internal/module/plugin/dto"
	pluginenums "go_wp/internal/module/plugin/enums"
	"go_wp/pkg/response"
)

// Handle 插件接口处理器。
type Handle struct {
	svc plugincontract.PluginService
}

// NewHandle 构造。
func NewHandle(svc plugincontract.PluginService) *Handle { return &Handle{svc: svc} }

// uploadMaxBytes 上传体积上限（与安装防线 zipMaxBytes 对齐 + multipart 开销）。
const uploadMaxBytes = 52 << 20

// Install 上传安装插件（multipart zip；HTMX 表单与 JSON 客户端均可）。
func (h *Handle) Install(c *gin.Context) {
	file, _, err := c.Request.FormFile("file")
	if err != nil {
		response.ErrorWithMessage(c, http.StatusBadRequest, pluginenums.ErrInstallParse)
		return
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, uploadMaxBytes+1))
	if err != nil || len(data) == 0 || len(data) > uploadMaxBytes {
		response.ErrorWithMessage(c, http.StatusBadRequest, pluginenums.ErrInstallParse)
		return
	}
	res, err := h.svc.Install(c.Request.Context(), data)
	if err != nil {
		response.ErrorAuto(c, http.StatusBadRequest, "plugin", err)
		return
	}
	response.SuccessWithMessage(c, pluginenums.MsgInstallSuccess, res)
}

// List 插件列表。
func (h *Handle) List(c *gin.Context) {
	list, err := h.svc.List(c.Request.Context())
	if err != nil {
		response.ErrorAuto(c, http.StatusInternalServerError, "plugin", err)
		return
	}
	response.SuccessWithMessage(c, pluginenums.MsgListSuccess, list)
}

// Toggle 启停插件。
func (h *Handle) Toggle(c *gin.Context) {
	req := &plugindto.ToggleReq{}
	if err := c.ShouldBind(req); err != nil {
		response.ErrorWithMessage(c, http.StatusBadRequest, pluginenums.ErrInvalidParam)
		return
	}
	if err := h.svc.Toggle(c.Request.Context(), req); err != nil {
		// 错误消息统一来自模块 enums（含"插件不存在"等业务语义），
		// 业务失败按 400 返回，其余系统错误 500。
		response.ErrorAuto(c, http.StatusBadRequest, "plugin", err)
		return
	}
	response.SuccessWithMessage(c, pluginenums.MsgToggleSuccess, nil)
}

// Uninstall 卸载插件。
func (h *Handle) Uninstall(c *gin.Context) {
	req := &plugindto.UninstallReq{}
	if err := c.ShouldBind(req); err != nil {
		response.ErrorWithMessage(c, http.StatusBadRequest, pluginenums.ErrInvalidParam)
		return
	}
	if err := h.svc.Uninstall(c.Request.Context(), req); err != nil {
		response.ErrorAuto(c, http.StatusInternalServerError, "plugin", err)
		return
	}
	response.SuccessWithMessage(c, pluginenums.MsgUninstallSuccess, nil)
}

// Detail 插件详情。
func (h *Handle) Detail(c *gin.Context) {
	req := &plugindto.DetailReq{}
	if err := c.ShouldBind(req); err != nil {
		response.ErrorWithMessage(c, http.StatusBadRequest, pluginenums.ErrInvalidParam)
		return
	}
	res, err := h.svc.Detail(c.Request.Context(), req)
	if err != nil {
		response.ErrorWithMessage(c, http.StatusNotFound, pluginenums.ErrPluginNotFound)
		return
	}
	response.SuccessWithMessage(c, pluginenums.MsgDetailSuccess, res)
}
