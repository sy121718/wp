package pluginhttp

import (
	"io"
	"net/http"

	"github.com/gin-gonic/gin"

	plugincontract "go_wp/internal/module/plugin/contract"
	plugindto "go_wp/internal/module/plugin/dto"
	pluginenums "go_wp/internal/module/plugin/enums"
	"go_wp/pkg/binding"
	"go_wp/pkg/response"
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

func (h *Handle) Toggle(c *gin.Context) {
	req, ok := binding.Form[plugindto.ToggleReq](c)
	if !ok {
		return
	}
	if err := h.svc.Toggle(c.Request.Context(), req); err != nil {

		response.ErrorAuto(c, http.StatusBadRequest, "plugin", err)
		return
	}
	response.SuccessWithMessage(c, pluginenums.MsgToggleSuccess, nil)
}

func (h *Handle) Uninstall(c *gin.Context) {
	req, ok := binding.Form[plugindto.UninstallReq](c)
	if !ok {
		return
	}
	if err := h.svc.Uninstall(c.Request.Context(), req); err != nil {
		response.ErrorAuto(c, http.StatusInternalServerError, "plugin", err)
		return
	}
	response.SuccessWithMessage(c, pluginenums.MsgUninstallSuccess, nil)
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
