package projecthttp

import (
	"github.com/gin-gonic/gin"
	"go_wp/pkg/response"
	view "go_wp/pkg/view"

	"go_wp/internal/adminui"
	projectenums "go_wp/internal/module/project/enums"
	"go_wp/pkg/backpath"
)

// projectErrScene 只进日志与失败事件归类；对用户可见的是消息事实本身。
const projectErrScene = "project"

type projectMsg = adminui.Msg

var (
	themesBackKeys        = []string{"project"}
	siteSettingsBackKeys  = []string{"project"}
	themeSettingsBackKeys = []string{"id"}
)

func themesBack(c *gin.Context) string {
	return backpath.FromRequest(c.Request.URL.Query(), "/admin/themes", "/admin", "project", themesBackKeys...)
}

func siteSettingsBack(c *gin.Context) string {
	return backpath.FromRequest(c.Request.URL.Query(), "/admin/settings", "/admin", "project", siteSettingsBackKeys...)
}

func themeSettingsBack(c *gin.Context) string {
	return backpath.FromRequest(c.Request.URL.Query(), "/admin/themes/settings", "/admin", "project", themeSettingsBackKeys...)
}

var (
	themesBackMsg        = projectMsg{Key: themePageMsgTitle, Fallback: "主题管理"}
	siteSettingsBackMsg  = projectMsg{Key: siteSettingsMsgTitle, Fallback: "站点设置"}
	themeSettingsBackMsg = projectMsg{Key: themeSettingsMsgTitle, Fallback: "主题设置"}
)

func projectMsgOf(key, fallback string) projectMsg {
	return projectMsg{Key: key, Fallback: fallback}
}

func projectTr(c *gin.Context) func(key, fallback string) string {
	return func(key, fallback string) string { return response.ArgsText(c, key, fallback, nil) }
}

func projectJump(c *gin.Context, ok bool, msg projectMsg, _ string, _ projectMsg) {
	if ok {
		view.SuccessWith(c, view.SuccessFact{Key: msg.Key, Fallback: msg.Fallback, Args: msg.Args})
		return
	}
	view.FailMessage(c, projectErrScene, msg.Key, msg.Fallback, msg.Args...)
}

var (
	siteSettingsSavedMsg = projectMsg{Key: projectenums.MsgSiteSettingsSaved, Fallback: "站点设置已保存"}

	localesSavedMsg = projectMsg{Key: projectenums.SiteLocalesSaved, Fallback: siteLocalesSavedFallback}

	themeSettingsSavedMsg = projectMsg{Key: projectenums.ThemeSettingsOKSaved, Fallback: themeSettingsSavedTextFallback}

	themeCreatedMsg   = projectMsg{Key: projectenums.ThemeOKCreated, Fallback: "主题已创建"}
	themeActivatedMsg = projectMsg{Key: projectenums.ThemeOKActivated, Fallback: "主题已激活，该主题下页面已标记待重建"}
	themeDeletedMsg   = projectMsg{Key: projectenums.ThemeOKDeleted, Fallback: "主题已删除"}
)
