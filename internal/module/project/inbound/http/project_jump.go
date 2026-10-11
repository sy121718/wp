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
//   · 文案是**事实**（key + 中文兜底 + `%s` 参数），不是拼好的句子 —— 控制器不取词，
//     取词由提示页注入的 tf 做（见 adminui.Prepare）。事实的形状是本文件的 `projectMsg`；
//     错误事实的判定在 project_err.go 的 projectErrMsg（service 哨兵表 + 归口）；
//   · 回跳地址由 backref.BackPath 从**表单 action 的 query** 按调用点列出的键读回
//     （服务端自己拼，不读隐藏域里的整串 URL）；
//   · 结论不进 URL —— 成功 / 失败只体现在提示页的响应体里。

import (
	"github.com/gin-gonic/gin"

	"go_wp/internal/adminui"
	"go_wp/internal/backref"
	projectenums "go_wp/internal/module/project/enums"
	"go_wp/internal/shell"
)

// projectMsg 提示页的一句结论 —— 就是共享形状 adminui.Msg
// （key + 中文兜底 + `%s` 位置参数）。
//
// 用别名而不是新造 struct：六个模块各造一份同形类型就是六份契约，字段名与参数编码
// 迟早分叉，而症状是「某个页面的提示偶尔少一个数字」，既不报错也不记日志（同 page 的 pageMsg）。
type projectMsg = adminui.Msg

// 各页回跳筛选键。
//
// 同一份键表服务两条路径：**渲染时**拼进表单 action 的 query、**POST 回来时**由
// backref.BackPath 读回。两处分叉的表现是「写完跳回去筛选静默丢了」—— 页面不报错、
// 日志也干净，所以键表必须是同一份（不要在两处各写一遍字面量）。
var (
	themesBackKeys        = []string{"project"}
	siteSettingsBackKeys  = []string{"project"}
	themeSettingsBackKeys = []string{"id"}
)

// themesBack 主题管理页的回跳地址（保留当前工程筛选）。
func themesBack(c *gin.Context) string {
	return backref.BackPath(c, "/admin/themes", themesBackKeys...)
}

// siteSettingsBack 站点设置页的回跳地址（保留当前工程）。
func siteSettingsBack(c *gin.Context) string {
	return backref.BackPath(c, "/admin/settings", siteSettingsBackKeys...)
}

// themeSettingsBack 单主题设置页的回跳地址（保留主题 id）。
func themeSettingsBack(c *gin.Context) string {
	return backref.BackPath(c, "/admin/themes/settings", themeSettingsBackKeys...)
}

// —— 回跳链接文字（复用各页标题词条，不新增全站词条）——
//
// 全是静态的 key + 中文原文，所以是包级变量：**不需要 gin.Context** ——
// 「这段文案属于哪个语言」由提示页取词时决定，与控制器无关（这正是本批的形状要求）。
var (
	themesBackMsg        = projectMsg{Key: themePageMsgTitle, Fallback: "主题管理"}
	siteSettingsBackMsg  = projectMsg{Key: siteSettingsMsgTitle, Fallback: "站点设置"}
	themeSettingsBackMsg = projectMsg{Key: themeSettingsMsgTitle, Fallback: "主题设置"}
)

// projectMsgOf 一条文案的事实（key + 中文兜底）。调用点给的是**已经选定**的 key
// （表单校验、成功回执）；取词发生在渲染期（提示页的 tf）。
//
// 与 projectErrMsg 的分工：那个是**错误归口漏斗**（判哨兵 / 记日志 / 落归口 key）。
func projectMsgOf(key, fallback string) projectMsg {
	return projectMsg{Key: key, Fallback: fallback}
}

// projectTr Go 侧取词函数（本模块**拼装标签**用的那一档；页面出口的取词一律发生在渲染期）。
//
// 为什么还需要它：主题分组 / SEO 评分 / 结构模板下拉的**标签**由 Go 侧组装后写进渲染数据，
// 模板直接渲染（还没取词化，见报告）。取词入口是共享设施（adminui.TranslateArgs），
// 不再是 shell.TranslateFor —— shell 那条是正在退役的旧入口。
func projectTr(c *gin.Context) func(key, fallback string) string {
	return func(key, fallback string) string { return adminui.TranslateArgs(c, key, fallback, nil) }
}

// projectJump 页面写动作的统一出口：整页提示（成功 1 秒后自动跳，失败不自动跳）。
//
// 失败不自动跳（Seconds=0）：运营要看清楚原因。成功 1 秒后自动回目标页
// （与 sysconfig / plugin / content / block 同一取舍）。
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

// —— 成功回执（key + 中文兜底；复用既有词条，词条见迁移 058 / 187 / 447 / 451，主题 CRUD 三条在 590）——

var (
	// siteSettingsSavedMsg 站点设置保存成功。
	siteSettingsSavedMsg = projectMsg{Key: projectenums.MsgSiteSettingsSaved, Fallback: "站点设置已保存"}
	// localesSavedMsg 语言清单保存成功（词条在迁移 187）。
	localesSavedMsg = projectMsg{Key: siteLocalesSavedKey, Fallback: siteLocalesSavedFallback}
	// themeSettingsSavedMsg 主题设置保存成功（词条在迁移 451）。
	themeSettingsSavedMsg = projectMsg{Key: themeSettingsSavedTextKey, Fallback: themeSettingsSavedTextFallback}

	themeCreatedMsg   = projectMsg{Key: projectenums.ThemeOKCreated, Fallback: "主题已创建"}
	themeActivatedMsg = projectMsg{Key: projectenums.ThemeOKActivated, Fallback: "主题已激活，该主题下页面已标记待重建"}
	themeDeletedMsg   = projectMsg{Key: projectenums.ThemeOKDeleted, Fallback: "主题已删除"}
)
