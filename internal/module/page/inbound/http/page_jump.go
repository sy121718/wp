package pagehttp

// page_jump.go — page 后台页写动作的**出口**：整页提示（shell.RenderJump，
// 对应 ThinkPHP 的 success() / error()）。
//
// 取代原先的 302/303 + `?err=` / `?done=` / `?ok=` 回列表页：那条通道要求读侧再判一次
// 「这条提示是不是本仓给的」（pagePageErr / pagePageDone / pageNoticeTexts /
// redirectOkKey / redirectErrKeyFromCode / siteSlotQueryText 就是那套），而查询参数
// 不是可信边界。文案改走响应体之后，那套判定整批删除（见 page_err.go 的文件头）。
//
// 三条边界（同 shell.RenderJump 的注释）：
//   · 文案必须**已过本模块白名单 / 已归口**（pageFacingOrInternal / pageErrPageText /
//     pageLangErrText / siteSlotFacingError / redirectErrText 的产物）—— 原文只进日志，
//     换个页面呈现不等于可以把 err.Error() 铺上去；
//   · 回跳地址由 shell.BackPath 从**表单 action 的 query** 按白名单读回（服务端自己拼，
//     不读隐藏域里的整串 URL）；
//   · 结论不进 URL —— 成功 / 失败只体现在提示页的响应体里。

import (
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	pageenums "go_wp/internal/module/page/enums"
	projectenums "go_wp/internal/module/project/enums"
	"go_wp/internal/shell"
)

// 各页回跳筛选键。
//
// 同一份键表服务两条路径：**渲染时**拼进表单 action 的 query、**POST 回来时**由
// shell.BackPath 读回。两处分叉的表现是「写完跳回去筛选静默丢了」—— 页面不报错、
// 日志也干净，所以键表必须是同一份（不要在两处各写一遍字面量）。
var (
	pageListBackKeys        = []string{"project"}
	siteSlotBackKeys        = []string{"project"}
	redirectBackKeys        = []string{"project"}
	translationBackKeys     = []string{"pageId", "lang"}
	translationMissBackKeys = []string{"project"}
)

// 后台页面路径（回跳目标）。redirectPagePath 在 pages_handle.go 里（与挂载点 /
// 权限点三处一致），这里不重复。
const (
	pageListPath            = "/admin/pages"
	pageSiteSlotsPath       = "/admin/site-slots"
	pageTranslationsPath    = "/admin/pages/translations"
	pageTranslationMissPath = "/admin/page-translation-misses"
)

// —— 回跳地址（每页一个，键表与上面同一份）——

func pageListBack(c *gin.Context) string {
	return shell.BackPath(c, pageListPath, pageListBackKeys...)
}

func siteSlotBack(c *gin.Context) string {
	return shell.BackPath(c, pageSiteSlotsPath, siteSlotBackKeys...)
}

func redirectBack(c *gin.Context) string {
	return shell.BackPath(c, redirectPagePath, redirectBackKeys...)
}

func translationBack(c *gin.Context) string {
	return shell.BackPath(c, pageTranslationsPath, translationBackKeys...)
}

func translationMissBack(c *gin.Context) string {
	return shell.BackPath(c, pageTranslationMissPath, translationMissBackKeys...)
}

// —— 回跳链接文字（复用各页标题词条，不新增全站词条）——

func pageListBackText(c *gin.Context) string {
	return shell.TranslateFor(c)(pageenums.MsgPagesTitle, "页面管理")
}

func siteSlotBackText(c *gin.Context) string {
	return shell.TranslateFor(c)("admin.site_slots.title", "系统页面")
}

func redirectBackText(c *gin.Context) string {
	return shell.TranslateFor(c)("admin.redirect.title", "重定向管理")
}

func translationBackText(c *gin.Context) string {
	return shell.TranslateFor(c)(pageenums.MsgPageTranslationsTitle, "多语言")
}

func translationMissBackText(c *gin.Context) string {
	return shell.TranslateFor(c)("admin.page.translation_misses.title", "内容缺译报告")
}

// —— 出口 ——

// pageJump 渲染整页提示：成功 1 秒后自动回跳，失败不自动跳（运营要看清楚原因）。
func pageJump(c *gin.Context, ok bool, msg, back, backText string) {
	if ok {
		shell.RenderJump(c, shell.Jump{OK: true, Msg: msg, Back: back, BackText: backText, Seconds: 1})
		return
	}
	shell.RenderJump(c, shell.Jump{Msg: msg, Back: back, BackText: backText})
}

// pageListJump 回页面列表的提示页出口（页面 / 工程写动作、排定与语言面板共用）。
func pageListJump(c *gin.Context, ok bool, msg string) {
	pageJump(c, ok, msg, pageListBack(c), pageListBackText(c))
}

// siteSlotJump 回系统页面槽位页的提示页出口。
func siteSlotJump(c *gin.Context, ok bool, msg string) {
	pageJump(c, ok, msg, siteSlotBack(c), siteSlotBackText(c))
}

// redirectJump 回重定向管理页的提示页出口。
func redirectJump(c *gin.Context, ok bool, msg string) {
	pageJump(c, ok, msg, redirectBack(c), redirectBackText(c))
}

// translationJump 回翻译工作台的提示页出口（保存成功）。
func translationJump(c *gin.Context, ok bool, msg string) {
	pageJump(c, ok, msg, translationBack(c), translationBackText(c))
}

// translationMissJump 回缺译报告的提示页出口（取消某语言）。
func translationMissJump(c *gin.Context, ok bool, msg string) {
	pageJump(c, ok, msg, translationMissBack(c), translationMissBackText(c))
}

// —— 写动作的成功 / 失败文案（复用既有词条，缺词条回落中文）——

// pageProjectCreatedText 新建站点工程的成功回执（复用 project 模块的 MsgProjectCreated 词条）。
func pageProjectCreatedText(c *gin.Context) string {
	return shell.TranslateFor(c)(projectenums.MsgProjectCreated, "站点工程创建成功")
}

// pageCreatedText 新建页面的成功回执。
func pageCreatedText(c *gin.Context) string {
	return shell.TranslateFor(c)(pageenums.MsgPageCreated, "页面创建成功")
}

// pageDeletedText 单条删除页面的成功回执（与批量删除同一句话，只是计数为 1）。
func pageDeletedText(c *gin.Context) string {
	return pagesBulkDeleteResult(c, 1, 0)
}

// pageTranslationsSavedText 译文保存成功回执（与工作台原来的 saved 提示逐字一致）。
func pageTranslationsSavedText(c *gin.Context, written int) string {
	tr := shell.TranslateFor(c)
	msg := tr("admin.page_translations.saved_lead", "已保存 ") +
		strconv.Itoa(written) + tr("admin.page_translations.saved_mid", " 条译文。")
	if written > 0 {
		msg += tr("admin.page_translations.saved_tail", "译文变更已标记全站待重建（下次构建生效）。")
	}
	return msg
}

// redirectOkText 重定向写动作的成功回执（key 与模板时代同一批，不新增词条）。
func redirectOkText(c *gin.Context, code string) string {
	switch strings.TrimSpace(code) {
	case "created":
		return shell.TranslateFor(c)("admin.redirect.ok.created", "重定向已新增。")
	case "deleted":
		return shell.TranslateFor(c)("admin.redirect.ok.deleted", "重定向已删除。")
	case "merged":
		return shell.TranslateFor(c)("admin.redirect.ok.merged", "重定向链已合并为直达。")
	default:
		return ""
	}
}

// redirectErrText 重定向写动作的失败回执：把业务错误 key 翻成当前语言（未命中落「操作未完成」）。
func redirectErrText(c *gin.Context, key string) string {
	if strings.TrimSpace(key) == "" {
		return ""
	}
	return shell.TranslateFor(c)(key, "操作未完成")
}
