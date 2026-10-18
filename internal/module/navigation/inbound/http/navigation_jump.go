package navigationhttp

// navigation_jump.go — 导航菜单页写动作的**出口**：整页提示（shell.RenderJump，
// 对应 ThinkPHP 的 success() / error()）。
//
// 取代原先的 303 + `?err=` / `?done=` 回列表页：那条通道要求读侧再判一次「这条提示是不是
// 本仓给的」（navigationNoticeTexts / navigationPageErr / navigationPageDone 就是那套），
// 而查询参数不是可信边界。文案改走响应体之后，读侧判定整批删除（见 navigation_err.go）。
//
// 三条边界（同 shell.RenderJump 的注释）：
//   · 文案是**事实**（key + 中文兜底 + `%s` 参数），不是拼好的句子 —— 控制器不取词，
//     取词由提示页注入的 tf 做（见 adminui.Prepare）。事实的形状是本文件的 `navText`；
//     错误事实的判定统一在 navigation_err.go 的 navigationErrMsg（pkg/facing 判据 + 归口）；
//   · 回跳地址由 backref.BackPath 从**表单 action 的 query** 按白名单读回（服务端自己拼，
//     不读隐藏域里的整串 URL）；
//   · 结论不进 URL —— 成功 / 失败只体现在提示页的响应体里。
//
// 注意这条**不适用于删除链**：行内删除与批量删除已迁到 JSON 端点
// （navigation_delete_write.go），那里的原因是 navigationErrText 的成品句、超限是
// `key|max|count` 协议；来源批量加入同理（navigation_add_source_write.go，逐条原因随回执
// 返回）。同级上移 / 下移也已迁走（同一条 JSON 端点，action="move"）。页面这一侧只剩
// update / panel。

import (
	"github.com/gin-gonic/gin"

	"go_wp/internal/backref"
	navigationenums "go_wp/internal/module/navigation/enums"
	"go_wp/internal/shell"
)

// navigationListPath 导航菜单列表页（回跳目标）。
const navigationListPath = "/admin/navigations"

// navListBack 列表页回跳地址：从**本次请求的 query** 读回筛选上下文。
//
// 上下文随表单 action 的 query 一起提交（`action="/admin/navigations/update?project=…&kind=…`），
// 服务端按调用点显式列出的键读回来 —— 不再由 Go 拼 ?err= / ?done=。
func navListBack(c *gin.Context) string {
	return backref.BackPath(c, navigationListPath, "project", "kind")
}

// navListBackMenu 同 navListBack，额外保留 `?menu=`（编辑 / 面板抽屉保存后要重新展开的那一项）。
func navListBackMenu(c *gin.Context) string {
	return backref.BackPath(c, navigationListPath, "project", "kind", "menu")
}

// —— 回跳链接文字与成功回执（key + 中文兜底，**不需要 gin.Context**）——
//
// 「这段文案属于哪个语言」由提示页取词时决定，与控制器无关 —— 这正是本批的形状要求：
// 控制器曾经在这里调 shell.TranslateFor 把 key 提前翻成一句当前语言的成品串，而语言是
// **请求级**的，每多一处就多一次「这句话属于哪个语言」的判断。
var (
	// navListBackTitle 列表页回跳链接文字（复用页面标题词条，不新增全站词条）。
	navListBackTitle = navText{Key: navigationenums.Heading, Fallback: "导航菜单"}

	// navDoneNotice 写动作成功回执（复用同一句话，不按动作细分；词条见迁移 590）。
	navDoneNotice = navText{Key: navigationenums.ActionDone, Fallback: "操作已完成"}
)

// navJump 提示页出口：成功 1 秒后自动跳，失败不自动跳（运营要看清楚原因）。
func navJump(c *gin.Context, ok bool, msg navText, back string, backText navText) {
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

// navJumpList 回列表页的提示页（保留工程 / 位置筛选）。
func navJumpList(c *gin.Context, ok bool, msg navText) {
	navJump(c, ok, msg, navListBack(c), navListBackTitle)
}

// navJumpListMenu 回列表页并重新展开某项抽屉的提示页（编辑 / 面板设置共用）。
func navJumpListMenu(c *gin.Context, ok bool, msg navText) {
	navJump(c, ok, msg, navListBackMenu(c), navListBackTitle)
}
