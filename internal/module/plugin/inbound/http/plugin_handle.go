package pluginhttp

import (
	"io"
	"net/http"

	"github.com/gin-gonic/gin"
	ginbinding "github.com/gin-gonic/gin/binding"

	plugincontract "go_wp/internal/module/plugin/contract"
	plugindto "go_wp/internal/module/plugin/dto"
	pluginenums "go_wp/internal/module/plugin/enums"
	"go_wp/pkg/binding"
	"go_wp/pkg/response"
	pageview "go_wp/pkg/view"
)

type Handle struct {
	svc plugincontract.PluginService
}

func NewHandle(svc plugincontract.PluginService) *Handle { return &Handle{svc: svc} }

const uploadMaxBytes = 52 << 20

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

func (h *Handle) List(c *gin.Context) {
	list, err := h.svc.List(c.Request.Context())
	if err != nil {
		response.ErrorAuto(c, http.StatusInternalServerError, "plugin", err)
		return
	}
	response.SuccessWithMessage(c, pluginenums.MsgListSuccess, list)
}

// Toggle 接收标准表单，成功后由 Jet 返回统一成功片段。
func (h *Handle) Toggle(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	var req plugindto.ToggleReq
	if err := c.ShouldBindWith(&req, ginbinding.FormPost); err != nil {
		pageview.FailStatus(c, http.StatusBadRequest, "plugin", err)
		return
	}
	if err := h.svc.Toggle(c.Request.Context(), &req); err != nil {
		pageview.FailScene(c, "plugin", err)
		return
	}
	pageview.Success(c)
}

// Uninstall 接收标准表单；删除文件失败按服务端错误渲染，不暴露内部原文。
func (h *Handle) Uninstall(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	var req plugindto.UninstallReq
	if err := c.ShouldBindWith(&req, ginbinding.FormPost); err != nil {
		pageview.FailStatus(c, http.StatusBadRequest, "plugin", err)
		return
	}
	if err := h.svc.Uninstall(c.Request.Context(), &req); err != nil {
		pageview.FailScene(c, "plugin", err)
		return
	}
	pageview.Success(c)
}

func (h *Handle) Detail(c *gin.Context) {
	req, ok := binding.Form[plugindto.DetailReq](c)
	if !ok {
		return
	}
	res, err := h.svc.Detail(c.Request.Context(), req)
	if err != nil {
		response.ErrorWithMessage(c, http.StatusNotFound, pluginenums.ErrPluginNotFound)
		return
	}
	response.SuccessWithMessage(c, pluginenums.MsgDetailSuccess, res)
}
