package navigationhttp

// navigation_jump.go — 导航菜单页写动作的**出口**：整页提示（shell.RenderJump，
// 对应 ThinkPHP 的 success() / error()）。
//
// 取代原先的 303 + `?err=` / `?done=` 回列表页：那条通道要求读侧再判一次「这条提示是不是
// 本仓给的」（navigationNoticeTexts / navigationPageErr / navigationPageDone 就是那套），
// 而查询参数不是可信边界。文案改走响应体之后，读侧判定整批删除（见 navigation_err.go）。
//
// 三条边界（同 shell.RenderJump 的注释）：
//   · 文案必须**已过本模块白名单 / 已归口**（navigationErrPageText / shell.BulkIDsFacingText /
//     navBulkFilled 的产物）—— 原文只进日志，换个页面呈现不等于可以把 err.Error() 铺上去；
//   · 回跳地址由 shell.BackPath 从**表单 action 的 query** 按白名单读回（服务端自己拼，
//     不读隐藏域里的整串 URL）；
//   · 结论不进 URL —— 成功 / 失败只体现在提示页的响应体里。

import (
	"github.com/gin-gonic/gin"

	"go_wp/internal/shell"
)

// navigationListPath 导航菜单列表页（回跳目标）。
const navigationListPath = "/admin/navigations"

// navListBack 列表页回跳地址：从**本次请求的 query** 读回筛选上下文。
//
// 上下文随表单 action 的 query 一起提交（`action="/admin/navigations/create?project=…&kind=…"`），
// 服务端按调用点显式列出的键读回来 —— 不再由 Go 拼 ?err= / ?done=。
func navListBack(c *gin.Context) string {
	return shell.BackPath(c, navigationListPath, "project", "kind")
}

// navListBackMenu 同 navListBack，额外保留 `?menu=`（编辑 / 面板抽屉保存后要重新展开的那一项）。
func navListBackMenu(c *gin.Context) string {
	return shell.BackPath(c, navigationListPath, "project", "kind", "menu")
}

// navListBackTitle 列表页回跳链接文字（复用页面标题词条，不新增全站词条）。
var navListBackTitle = navText{Key: "admin.navigations.heading", Fallback: "导航菜单"}

func navListBackText(c *gin.Context) string { return navTextOf(c, navListBackTitle) }

// navDoneNotice 写动作成功回执（复用同一句话，不按动作细分；词条见迁移 590）。
var navDoneNotice = navText{Key: "admin.navigations.actionDone", Fallback: "操作已完成"}

func navDoneText(c *gin.Context) string { return navTextOf(c, navDoneNotice) }

// navJump 提示页出口：成功 1 秒后自动跳，失败不自动跳（运营要看清楚原因）。
func navJump(c *gin.Context, ok bool, msg, back, backText string) {
	if ok {
		shell.RenderJump(c, shell.Jump{OK: true, Msg: msg, Back: back, BackText: backText, Seconds: 1})
		return
	}
	shell.RenderJump(c, shell.Jump{Msg: msg, Back: back, BackText: backText})
}

// navJumpList 回列表页的提示页（保留工程 / 位置筛选）。
func navJumpList(c *gin.Context, ok bool, msg string) {
	navJump(c, ok, msg, navListBack(c), navListBackText(c))
}

// navJumpListMenu 回列表页并重新展开某项抽屉的提示页（编辑 / 面板设置共用）。
func navJumpListMenu(c *gin.Context, ok bool, msg string) {
	navJump(c, ok, msg, navListBackMenu(c), navListBackText(c))
}
