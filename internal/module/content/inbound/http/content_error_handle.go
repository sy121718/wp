package contenthttp

// content_error_handle.go — 文章后台页写动作的**出口归口**（错误文案三件套 + 整页提示）。
//
// 三件套（AGENTS.md §响应与错误处理，与 admin 的 admin_err.go / block 的 block_err.go 同形）：
//
//	① **判定** —— 只有一条：adminui.PageErrorFact（pkg/response.IsBusinessError）。
//	   本模块原先自持三张「可透出白名单」并逐条比对，那是**第二份真相**：service 换了措辞、
//	   或某个 key 改由别的模块提供，白名单就静默失配，而两种症状都不报错（有时把内部错误
//	   当业务文案透出、有时把业务文案吞成归口）。三张表现在只剩一件事 ——
//	   **key → 中文兜底**（缺词条时页面显示什么），不再是可透出性判据。
//	   （表在 content_page.go：articleFallbacks / articlePublishFallbacks /
//	   articleImportFallbacks；存在价值只剩「同一个 key 在不同场景各说各的」。）
//	② 归口文案 —— 未命中时给 adminui.InternalMsg()（可翻译 key + 中文兜底）；
//	③ 结构化日志 —— 原文只进日志，带 user_id（见 content_page.go 的 articleInternalMsg）。
//
// **控制器不取词**：本文件的出口收的是**事实**（adminui.Msg：key + 中文兜底 + `%s` 参数），
// 不是拼好的句子 —— 「这段文案属于哪个语言」由模板取词时决定，与控制器无关。所以
// 出口的 msg / backText 都是 adminui.Msg，回跳链接文字则是包级常量（静态文案不需要 c）。
//
// 传输通道：写动作的结论由 shell.RenderJump 渲染成整页提示（对应 ThinkPHP 的
// success() / error()），**不再**经 302 / 303 + `?err=` / `?ok=` / `?done=` 回带列表页或编辑页。
//
// **读侧（?err= / ?ok= / ?done= 的受控文案集合与判定）已整批删除**：那条通道要求读侧
// 再判一次「这条提示是不是本仓给的」（articleQueryText / articleDoneTexts / articlePageDone
// 就是那套），而查询参数不是可信边界。文案走响应体之后，那套判定随之不需要了。
//
// 本文件只放**写侧出口**：回跳地址（shell.BackPath / backpath.WithParams）与提示页渲染。
// 错误归口（articleFacingErrMsg / articlePublishErrMsg / articleImportErrMsg 等）
// 仍留在各自的用例文件里，与它们的兜底表同处一份。

import (
	"strings"

	"github.com/gin-gonic/gin"

	"go_wp/internal/shell"
	"go_wp/pkg/backpath"
)

// articleJumpListBack 回文章列表页的回跳地址：从**本次请求的 query** 读回筛选上下文。
//
// 上下文随表单 action 的 query 一起提交（`action="/admin/articles/bulk-delete?keyword=…&limit=…"`），
// 服务端按调用点显式列出的键读回来 —— 不再由 Go 拼 ?err= / ?ok= / ?done=。
func articleJumpListBack(c *gin.Context) string {
	return shell.BackPath(c, articleListPath, "keyword", "limit")
}

// articleJumpEditBack 回文章编辑页的回跳地址：id 来自本次提交的表单。
//
// 编辑页的写动作（保存 / 发布 / 重建 / 改 URL / 导入）都把 id 放在表单隐藏域里，
// 不是查询参数 —— 所以用 backpath.WithParams 拼，与 BackPath 共用同一份参数编码。
func articleJumpEditBack(c *gin.Context, id string) string {
	return backpath.WithParams(articleEditPagePath, map[string]string{"id": strings.TrimSpace(id)})
}

// —— 回跳链接文字（复用页面标题词条，不新增全站词条）——
//
// 全是静态的 key + 中文原文，所以是包级变量：**不需要 gin.Context** ——
// 「这段文案属于哪个语言」由模板取词时决定，与控制器无关（这正是本批的形状要求）。
var (
	articleJumpListMsg = articleMsg{Key: articlePageTitle, Fallback: "文章"}
	articleJumpEditMsg = articleMsg{Key: articleEditTitle, Fallback: "编辑文章"}
)

// articlePageJump 页面写动作的统一出口：整页提示。
//
// 取代原先的 302 / 303 + `?err=` / `?ok=` / `?done=`：那条通道要求读侧再判一次
// 「这条提示是不是本仓给的」，而查询参数不是可信边界。现在文案走响应体，读侧判定随之删除。
//
// msg / backText 都是**事实**（adminui.Msg）而不是拼好的句子：判定与兜底已在
// articleFacingErrMsg 那一层做完，取词留给模板注入的 tf（见 adminui.Prepare）。
//
// 失败不自动跳转（Seconds=0）：运营要看清楚原因。成功 1 秒后自动回目标页
// （与 sysconfig / plugin / block 同一取舍）。
//
// 例外（2026-10 起已取消）：保存文章的编辑页表单改走 htmx 之后，失败不再经这里，
// 也不再重渲整页 —— 页面没离开过，正文还在框里，结论由 toast 给。
// 这个出口只剩「列表 / 批量 / 发布 / 重建」这类**不带用户输入**的写动作在用。
func articlePageJump(c *gin.Context, ok bool, msg articleMsg, back string, backText articleMsg) {
	seconds := 0
	if ok {
		seconds = 1
	}
	shell.RenderJump(c, shell.Jump{
		OK:          ok,
		MsgKey:      msg.Key,
		Msg:         msg.Fallback,
		MsgArgs:     msg.Args,
		Back:        back,
		BackTextKey: backText.Key,
		BackText:    backText.Fallback,
		Seconds:     seconds,
	})
}

// articleListJump 回文章列表页的提示页出口（新建 / 删除 / 批量删除共用）。
func articleListJump(c *gin.Context, ok bool, msg articleMsg) {
	articlePageJump(c, ok, msg, articleJumpListBack(c), articleJumpListMsg)
}

// articleEditJump 回文章编辑页的提示页出口（发布 / 重建 / 改 URL / 导入共用）。
func articleEditJump(c *gin.Context, ok bool, id string, msg articleMsg) {
	articlePageJump(c, ok, msg, articleJumpEditBack(c, id), articleJumpEditMsg)
}
