package projecthttp

// project_jump.go — project 域后台页写动作的**出口**：整页提示（shell.RenderJump，
// 对应 ThinkPHP 的 success() / error()）。
//
// 取代原先的 303 + `?err=` / `?ok=` / `?locales_saved=1` 回列表页 / 回本页：那条通道
// 要求读侧再判一次「这条提示是不是本仓给的」（projectPageErrKeys / projectErrTexts /
// projectPageErrText / themeSettingsSavedTexts / themeSettingsPageOK 就是那套），
// 而查询参数不是可信边界。文案改走响应体之后，读侧判定整批删除（见 project_err.go）。
//
// 三条边界（同 shell.RenderJump 的注释）：
//   · 文案必须**已过本模块白名单 / 已归口**（projectErrParam / projectText 的产物）——
//     原文只进日志，换个页面呈现不等于可以把 err.Error() 铺在页面上；
//   · 回跳地址由 shell.BackPath 从**表单 action 的 query** 按调用点列出的键读回
//     （服务端自己拼，不读隐藏域里的整串 URL）；
//   · 结论不进 URL —— 成功 / 失败只体现在提示页的响应体里。

import (
	"github.com/gin-gonic/gin"

	projectenums "go_wp/internal/module/project/enums"
	"go_wp/internal/shell"
)

// 各页回跳筛选键。
//
// 同一份键表服务两条路径：**渲染时**拼进表单 action 的 query、**POST 回来时**由
// shell.BackPath 读回。两处分叉的表现是「写完跳回去筛选静默丢了」—— 页面不报错、
// 日志也干净，所以键表必须是同一份（不要在两处各写一遍字面量）。
var (
	themesBackKeys        = []string{"project"}
	siteSettingsBackKeys  = []string{"project"}
	themeSettingsBackKeys = []string{"id"}
)

// themesBack 主题管理页的回跳地址（保留当前工程筛选）。
func themesBack(c *gin.Context) string {
	return shell.BackPath(c, "/admin/themes", themesBackKeys...)
}

// siteSettingsBack 站点设置页的回跳地址（保留当前工程）。
func siteSettingsBack(c *gin.Context) string {
	return shell.BackPath(c, "/admin/settings", siteSettingsBackKeys...)
}

// themeSettingsBack 单主题设置页的回跳地址（保留主题 id）。
func themeSettingsBack(c *gin.Context) string {
	return shell.BackPath(c, "/admin/themes/settings", themeSettingsBackKeys...)
}

// —— 回跳链接文字（复用各页标题词条，不新增全站词条）——

func themesBackText(c *gin.Context) string {
	return shell.TranslateFor(c)(themePageMsgTitle, "主题管理")
}

func siteSettingsBackText(c *gin.Context) string {
	return shell.TranslateFor(c)(siteSettingsMsgTitle, "站点设置")
}

func themeSettingsBackText(c *gin.Context) string {
	return shell.TranslateFor(c)(themeSettingsMsgTitle, "主题设置")
}

// projectText 取一条页面文案的当前语言文本（词条缺失回落中文兜底）。
//
// 与 projectErrParam 的分工：那个是**错误归口漏斗**（判哨兵 / 记日志 / 落归口 key），
// 这个只负责取词 —— 调用点给的是**已经选定**的 key（表单校验、成功回执）。
// 不走 response.TranslateMessage：那个没有兜底，词条缺失时会把裸 key 渲染到页面上。
func projectText(c *gin.Context, key, fallback string) string {
	return shell.TranslateFor(c)(key, fallback)
}

// projectJump 页面写动作的统一出口：整页提示（成功 1 秒后自动跳，失败不自动跳）。
//
// 失败不自动跳（Seconds=0）：运营要看清楚原因。成功 1 秒后自动回目标页
// （与 sysconfig / plugin / content / block 同一取舍）。
func projectJump(c *gin.Context, ok bool, msg, back, backText string) {
	if ok {
		shell.RenderJump(c, shell.Jump{OK: true, Msg: msg, Back: back, BackText: backText, Seconds: 1})
		return
	}
	shell.RenderJump(c, shell.Jump{Msg: msg, Back: back, BackText: backText})
}

// —— 成功回执（取当前语言，词条缺失回落中文兜底）——
//
// 复用既有词条（迁移 058 / 447 / 187 / 451），只有主题 CRUD 的三条是新增（迁移 590）。

// siteSettingsSavedText 站点设置保存成功。
func siteSettingsSavedText(c *gin.Context) string {
	return projectText(c, projectenums.MsgSiteSettingsSaved, "站点设置已保存")
}

// localesSavedText 语言清单保存成功（词条在迁移 187）。
func localesSavedText(c *gin.Context) string {
	return projectText(c, siteLocalesSavedKey, siteLocalesSavedFallback)
}

// themeSettingsSavedText 主题设置保存成功（词条在迁移 451）。
func themeSettingsSavedText(c *gin.Context) string {
	return projectText(c, themeSettingsSavedTextKey, themeSettingsSavedTextFallback)
}

// themeCreatedText / themeActivatedText / themeDeletedText 主题 CRUD 的成功回执
// （词条在迁移 590）。
func themeCreatedText(c *gin.Context) string {
	return projectText(c, projectenums.ThemeOKCreated, "主题已创建")
}

func themeActivatedText(c *gin.Context) string {
	return projectText(c, projectenums.ThemeOKActivated, "主题已激活，该主题下页面已标记待重建")
}

func themeDeletedText(c *gin.Context) string {
	return projectText(c, projectenums.ThemeOKDeleted, "主题已删除")
}
