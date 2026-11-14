package pluginhttp

import (
	"io"
	"mime/multipart"
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
	// 上传是明确的 multipart 文件表单：与页面端点 /admin/plugins/install 共用
	// InstallFormReq，避免同一动作两条入口各解析一次请求体。
	var form plugindto.InstallFormReq
	if err := c.ShouldBindWith(&form, ginbinding.FormMultipart); err != nil || form.File == nil {
		response.ErrorWithMessage(c, http.StatusBadRequest, pluginenums.ErrInstallParse)
		return
	}
	data, ok := readPluginUpload(form.File)
	if !ok {
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

// readPluginUpload 读出上传文件字节并做大小/空文件校验。
// 空文件、读取失败、超限都返回 false，由调用方给统一的安装解析失败出口。
func readPluginUpload(header *multipart.FileHeader) ([]byte, bool) {
	file, err := header.Open()
	if err != nil {
		return nil, false
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, uploadMaxBytes+1))
	if err != nil || len(data) == 0 || len(data) > uploadMaxBytes {
		return nil, false
	}
	return data, true
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
	req, err := binding.ParseForm[plugindto.ToggleReq](c)
	if err != nil {
		pageview.FailStatus(c, http.StatusBadRequest, "plugin", err)
		return
	}
	if err := h.svc.Toggle(c.Request.Context(), req); err != nil {
		pageview.FailScene(c, "plugin", err)
		return
	}
	pageview.Success(c)
}

// Uninstall 接收标准表单；删除文件失败按服务端错误渲染，不暴露内部原文。
func (h *Handle) Uninstall(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	req, err := binding.ParseForm[plugindto.UninstallReq](c)
	if err != nil {
		pageview.FailStatus(c, http.StatusBadRequest, "plugin", err)
		return
	}
	if err := h.svc.Uninstall(c.Request.Context(), req); err != nil {
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
