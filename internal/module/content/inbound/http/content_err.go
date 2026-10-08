package contenthttp

// content_err.go — 文章后台页写动作的**出口归口**（错误文案三件套 + 整页提示）。
//
// 三件套（AGENTS.md §响应与错误处理，与 admin 的 admin_err.go / block 的 block_err.go 同形）：
//
//	① 白名单 —— articleFacingMessages / articlePublishFacingMessages / articleImportFacingMessages
//	   （三张表分别在 content_page.go，键是 enums 常量或 i18n key）；
//	② 归口文案 —— 未命中时返回 shell.PageInternalText(c)（可翻译 key + 中文兜底）；
//	③ 结构化日志 —— 原文只进日志，带 user_id（见 content_page.go 的 articleInternalText）。
//
// 传输通道：写动作的结论由 shell.RenderJump 渲染成整页提示（对应 ThinkPHP 的
// success() / error()），**不再**经 302 / 303 + `?err=` / `?ok=` / `?done=` 回带列表页或编辑页。
//
// **读侧（?err= / ?ok= / ?done= 的受控文案集合与判定）已整批删除**：那条通道要求读侧
// 再判一次「这条提示是不是本仓给的」（articleQueryText / articleDoneTexts / articlePageDone
// 就是那套），而查询参数不是可信边界。文案走响应体之后，那套判定随之不需要了。
//
// 本文件只放**写侧出口**：回跳地址（shell.BackPath / shell.WithParams）与提示页渲染。
// 错误归口（articleFacingError / articlePublishFacingError / articleImportFacingError 等）
// 仍留在各自的用例文件里，与它们的白名单同处一份。

import (
	"strings"

	"github.com/gin-gonic/gin"

	"go_wp/internal/shell"
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
// 不是查询参数 —— 所以用 shell.WithParams 拼，与 BackPath 共用同一份参数编码。
func articleJumpEditBack(c *gin.Context, id string) string {
	return shell.WithParams(articleEditPagePath, map[string]string{"id": strings.TrimSpace(id)})
}

// articleJumpListText 列表页回跳链接的文字（复用页面标题词条，不新增全站词条）。
func articleJumpListText(c *gin.Context) string {
	return shell.TranslateFor(c)(articlePageTitle, "文章")
}

// articleJumpEditText 编辑页回跳链接的文字（复用页面标题词条）。
func articleJumpEditText(c *gin.Context) string {
	return shell.TranslateFor(c)(articleEditTitle, "编辑文章")
}

// articlePageJump 页面写动作的统一出口：整页提示。
//
// 取代原先的 302 / 303 + `?err=` / `?ok=` / `?done=`：那条通道要求读侧再判一次
// 「这条提示是不是本仓给的」，而查询参数不是可信边界。现在文案走响应体，读侧判定随之删除。
//
// msg 必须**已过本模块白名单 / 已归口**（articleFacingError / articleFacingText /
// articlePublishTextOf / articleFacingOrInternal 的产物），原文只进日志 ——
// 换个页面呈现不等于可以把 err.Error() 铺在页面上。
//
// 失败不自动跳转（Seconds=0）：运营要看清楚原因。成功 1 秒后自动回目标页
// （与 sysconfig / plugin / block 同一取舍）。
//
// 例外：**保存文章失败**不走这里，仍由 articleUpdateFailure 原地重渲编辑页回填本次输入
// （见 internal/templates/CLAUDE.md「写表单失败时原地留住输入」）—— 整页提示会把
// 用户刚写的正文整屏清掉，那是比错误文案贵得多的损失。
func articlePageJump(c *gin.Context, ok bool, msg, back, backText string) {
	if ok {
		shell.RenderJump(c, shell.Jump{OK: true, Msg: msg, Back: back, BackText: backText, Seconds: 1})
		return
	}
	shell.RenderJump(c, shell.Jump{Msg: msg, Back: back, BackText: backText})
}

// articleListJump 回文章列表页的提示页出口（新建 / 删除 / 批量删除共用）。
func articleListJump(c *gin.Context, ok bool, msg string) {
	articlePageJump(c, ok, msg, articleJumpListBack(c), articleJumpListText(c))
}

// articleEditJump 回文章编辑页的提示页出口（发布 / 重建 / 改 URL / 导入共用）。
func articleEditJump(c *gin.Context, ok bool, id, msg string) {
	articlePageJump(c, ok, msg, articleJumpEditBack(c, id), articleJumpEditText(c))
}
