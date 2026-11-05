package projecthttp

import (
	"github.com/gin-gonic/gin"
	"go_wp/pkg/response"

	"go_wp/internal/adminui"
	"go_wp/internal/backref"
	projectenums "go_wp/internal/module/project/enums"
	"go_wp/internal/shell"
)

type projectMsg = adminui.Msg

var (
	themesBackKeys        = []string{"project"}
	siteSettingsBackKeys  = []string{"project"}
	themeSettingsBackKeys = []string{"id"}
)

func themesBack(c *gin.Context) string {
	return backref.BackPath(c, "/admin/themes", themesBackKeys...)
}

func siteSettingsBack(c *gin.Context) string {
	return backref.BackPath(c, "/admin/settings", siteSettingsBackKeys...)
}

func themeSettingsBack(c *gin.Context) string {
	return backref.BackPath(c, "/admin/themes/settings", themeSettingsBackKeys...)
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

func projectJump(c *gin.Context, ok bool, msg projectMsg, back string, backText projectMsg) {
	j := shell.Jump{
		MsgKey:      msg.Key,
		Msg:         msg.Fallback,
		MsgArgs:     msg.Args,
		Back:        back,
		BackTextKey: backText.Key,
		BackText:    backText.Fallback,
	}
	if ok {
		j.OK = true
		j.Seconds = 1
	}
	shell.RenderJump(c, j)
}

var (
	siteSettingsSavedMsg = projectMsg{Key: projectenums.MsgSiteSettingsSaved, Fallback: "站点设置已保存"}

	localesSavedMsg = projectMsg{Key: projectenums.SiteLocalesSaved, Fallback: siteLocalesSavedFallback}

	themeSettingsSavedMsg = projectMsg{Key: projectenums.ThemeSettingsOKSaved, Fallback: themeSettingsSavedTextFallback}

	themeCreatedMsg   = projectMsg{Key: projectenums.ThemeOKCreated, Fallback: "主题已创建"}
	themeActivatedMsg = projectMsg{Key: projectenums.ThemeOKActivated, Fallback: "主题已激活，该主题下页面已标记待重建"}
	themeDeletedMsg   = projectMsg{Key: projectenums.ThemeOKDeleted, Fallback: "主题已删除"}
)
