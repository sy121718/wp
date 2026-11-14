package pluginhttp

import (
	ginbinding "github.com/gin-gonic/gin/binding"
	"io"

	plugincontract "go_wp/internal/module/plugin/contract"
	plugindto "go_wp/internal/module/plugin/dto"
	pluginenums "go_wp/internal/module/plugin/enums"
	"go_wp/pkg/logger"
	pageview "go_wp/pkg/view"

	"github.com/gin-gonic/gin"
	"go_wp/pkg/response"
)

type pluginsPageData struct {
	Title   string
	Menu    string
	Plugins []*plugincontract.PluginResp

	Error string

	ArtifactPatrol *plugincontract.PatrolResp
}

type pluginPageHandle struct {
	plugins plugincontract.PluginService
}

func emptyPatrol() *plugincontract.PatrolResp {
	return &plugincontract.PatrolResp{
		OrphanSchemas:  []plugincontract.SchemaInfo{},
		MissingSchemas: []string{},
		OrphanStorage:  []string{},
		MissingStorage: []string{},
	}
}

func (h *pluginPageHandle) PluginsPage(c *gin.Context) {

	data := &pluginsPageData{
		Title:          pluginenums.TitlePlugins,
		Menu:           "plugins",
		ArtifactPatrol: emptyPatrol(),
	}

	if h.plugins != nil {
		list, err := h.plugins.List(c.Request.Context())
		if err != nil {
			data.Error = response.MsgText(c, pluginNoticeListFailed)
		} else {
			data.Plugins = list
		}

		if patrol, perr := h.plugins.PatrolArtifacts(c.Request.Context()); perr != nil {
			logger.Scene("plugin").Error(perr, "插件产物巡检失败")
		} else if patrol != nil {
			data.ArtifactPatrol = patrol
		}
	}

	pageview.Render(c, data)
}

const pluginsUploadMax = 52 << 20

func (h *pluginPageHandle) PluginsInstall(c *gin.Context) {
	if h.plugins == nil {
		pluginPageJump(c, false, pluginNoticeModuleUnwired)
		return
	}
	// 上传是明确的 multipart 文件表单：先按 FormMultipart 绑定成 DTO，
	// 让写端点契约能对账线上控件名；再取文件流做大小上限校验。
	var form plugindto.InstallFormReq
	if err := c.ShouldBindWith(&form, ginbinding.FormMultipart); err != nil || form.File == nil {
		pluginPageJump(c, false, pluginInstallFailMsg(c, pluginNoticeNoFile))
		return
	}
	// 消费绑定到的真实文件头；不再二次 FormFile，避免两处各解析一次请求体。
	file, err := form.File.Open()
	if err != nil {
		pluginPageJump(c, false, pluginInstallFailMsg(c, pluginNoticeNoFile))
		return
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, pluginsUploadMax+1))
	if err != nil || len(data) == 0 || len(data) > pluginsUploadMax {
		pluginPageJump(c, false, pluginInstallFailMsg(c, pluginNoticeUnreadable))
		return
	}
	if _, err := h.plugins.Install(c.Request.Context(), data); err != nil {
		logger.Scene("plugin").Error(err, "插件安装失败")
		pluginPageJump(c, false, pluginInstallFailMsg(c, pluginErrMsg(c, err)))
		return
	}
	pluginPageJump(c, true, pluginMsg{Key: pluginenums.MsgInstallSuccess, Fallback: "插件安装成功"})
}
