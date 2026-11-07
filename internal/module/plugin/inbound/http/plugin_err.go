package pluginhttp

import (
	view "go_wp/pkg/view"
	"strings"

	"github.com/gin-gonic/gin"
	pluginenums "go_wp/internal/module/plugin/enums"
	"go_wp/pkg/facing"
	"go_wp/pkg/response"
)

const (
	pluginErrScene = "plugin"

	pluginPagePath = "/admin/plugins"
)

type pluginMsg = facing.Msg

var (
	pluginTextInvalidParam    = pluginMsg{Key: pluginenums.ErrInvalidParam, Fallback: "参数不完整，请检查后重试"}
	pluginTextPluginNotFound  = pluginMsg{Key: pluginenums.ErrPluginNotFound, Fallback: "插件不存在"}
	pluginTextInstallParse    = pluginMsg{Key: pluginenums.ErrInstallParse, Fallback: "插件包解析失败"}
	pluginTextUnsafePackage   = pluginMsg{Key: pluginenums.ErrUnsafePackage, Fallback: "插件包包含不安全内容，已拒绝"}
	pluginTextVersionRegress  = pluginMsg{Key: pluginenums.ErrVersionRegress, Fallback: "插件版本不能回退"}
	pluginTextStorageFailure  = pluginMsg{Key: pluginenums.ErrStorageFailure, Fallback: "插件包存储失败"}
	pluginTextToggleFailed    = pluginMsg{Key: pluginenums.ErrToggleFailed, Fallback: "插件状态更新失败"}
	pluginTextUninstallFailed = pluginMsg{Key: pluginenums.ErrUninstallFailed, Fallback: "插件卸载失败"}
	pluginTextMigrationFailed = pluginMsg{Key: pluginenums.ErrMigrationFailed, Fallback: "插件数据层迁移失败"}
)

var (
	pluginNoticeModuleUnwired = pluginMsg{Key: pluginenums.ErrModuleUnwired, Fallback: "插件模块未装配，该操作无法执行"}
	pluginNoticeNoFile        = pluginMsg{Key: pluginenums.ErrInstallNoFile, Fallback: "没有收到插件包文件"}
	pluginNoticeUnreadable    = pluginMsg{Key: pluginenums.ErrPackageUnreadable, Fallback: "插件包读取失败，或文件超过 52MB 上限"}

	pluginNoticeListFailed = pluginMsg{Key: pluginenums.ErrListFailed, Fallback: "插件列表加载失败"}

	pluginReselectHint = pluginMsg{Key: pluginenums.NoticeInstallReselect, Fallback: "浏览器不会重传已选文件，请重新选择文件后再提交"}
)

var pluginFallbacks = []pluginMsg{
	pluginTextInvalidParam,
	pluginTextPluginNotFound,
	pluginTextInstallParse,
	pluginTextUnsafePackage,
	pluginTextVersionRegress,
	pluginTextStorageFailure,
	pluginTextToggleFailed,
	pluginTextUninstallFailed,
	pluginTextMigrationFailed,
	pluginNoticeModuleUnwired,
	pluginNoticeNoFile,
	pluginNoticeUnreadable,
	pluginNoticeListFailed,
	pluginReselectHint,
}

func pluginFacingFallback(key string) string {
	key = strings.TrimSpace(key)
	for _, m := range pluginFallbacks {
		if m.Key == key {
			return m.Fallback
		}
	}
	return ""
}

func pluginInstallFailMsg(c *gin.Context, m pluginMsg) pluginMsg {
	return pluginMsg{Fallback: response.MsgText(c, m) + "；" + response.MsgText(c, pluginReselectHint)}
}

func pluginErrMsg(c *gin.Context, err error) pluginMsg {
	if err == nil {
		return pluginMsg{}
	}
	fact := view.PageErrorFact(c, pluginErrScene, err)
	if !fact.Business {

		return pluginMsg{Key: fact.Key, Fallback: fact.Fallback}
	}
	fallback := pluginFacingFallback(fact.Key)
	if fallback == "" {
		fallback = fact.Fallback
	}
	return pluginMsg{Key: fact.Key, Fallback: fallback}
}

func pluginErrText(c *gin.Context, err error) string {
	return response.MsgText(c, pluginErrMsg(c, err))
}

func pluginPageJump(c *gin.Context, ok bool, msg pluginMsg) {
	// 回跳由发起页面声明；这里只交结果事实。
	if ok {
		view.SuccessWith(c, view.SuccessFact{Key: msg.Key, Fallback: msg.Fallback, Args: msg.Args})
		return
	}
	view.FailMessage(c, pluginErrScene, msg.Key, msg.Fallback, msg.Args...)
}
