package pagehttp

// page_redirect_handle.go — 重定向管理页与增删改接口（审计 SEO-025）。
//
// 页面挂在 authorizedAPI 组下（/api/page/redirect）而不是 /admin/*：page 模块在
// routes.go 里只拿到 authorizedAPI 这一个已装配好的组（页面路由组由 assembly 创建、
// 分发给各模块注册）。这样挂的代价是 URL 少一层「后台感」，换来的是三层链
// （Session / CSRF / Casbin）与 API 完全一致，不需要改动 routes.go。
//
// 交互约定：GET 渲染整页；三个 POST 都是原生表单提交（带 csrf_token 隐藏域），
// 完成后 302 回本页（PRG，防重复提交）。失败也走 PRG，但把错误以 **i18n key**
// 放进 query（不是中文文案）：这样同一个 URL 在英文界面下显示英文提示。

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"go_wp/internal/middleware/builtin"
	pagedto "go_wp/internal/module/page/dto"
	pageenums "go_wp/internal/module/page/enums"
	pageservice "go_wp/internal/module/page/service"
	"go_wp/internal/templates"
	"go_wp/pkg/logger"
	"go_wp/pkg/response"

	"github.com/gin-gonic/gin"
)

// redirectPagePath 管理页路径（与 page_router.go 里的挂载点、迁移 218 的权限点
// api_path 三处必须一致；改一处必须改三处）。
const redirectPagePath = "/api/page/redirect"

// redirectBulkOkCode 批量删除成功枚举（与 created / deleted / merged 并列；
// 它不带词条 key，文案由服务端按 dn / sk 两个计数拼装）。
const redirectBulkOkCode = "bulk"

// RedirectPage 重定向管理页：列出当前工程的全部 301 并给出增删与合并入口。
func (h *Handle) RedirectPage(c *gin.Context) {
	res, err := h.svc.ListRedirects(c.Request.Context(), &pagedto.RedirectListReq{ProjectID: c.Query("project")})
	if err != nil {
		logger.Scene("page").Error(err, "重定向列表读取失败")
		c.HTML(http.StatusInternalServerError, "admin/page_redirects.html", redirectPageData(c, nil, redirectErrKey(err)))
		return
	}
	c.HTML(http.StatusOK, "admin/page_redirects.html", redirectPageData(c, res, redirectErrKeyFromCode(c.Query("err"))))
}

// RedirectCreate 新增重定向。
func (h *Handle) RedirectCreate(c *gin.Context) {
	var req pagedto.RedirectCreateReq
	if err := c.ShouldBind(&req); err != nil {
		redirectPageBack(c, "", "", pageenums.ErrInvalidParam)
		return
	}
	if _, err := h.svc.CreateRedirect(c.Request.Context(), &req); err != nil {
		redirectPageBack(c, req.ProjectID, "", redirectErrKey(err))
		return
	}
	redirectPageBack(c, req.ProjectID, "created", "")
}

// RedirectDelete 删除一条重定向。
func (h *Handle) RedirectDelete(c *gin.Context) {
	var req pagedto.RedirectDeleteReq
	if err := c.ShouldBind(&req); err != nil {
		redirectPageBack(c, "", "", pageenums.ErrInvalidParam)
		return
	}
	if err := h.svc.DeleteRedirect(c.Request.Context(), &req); err != nil {
		redirectPageBack(c, req.ProjectID, "", redirectErrKey(err))
		return
	}
	redirectPageBack(c, req.ProjectID, "deleted", "")
}

// RedirectMerge 把一条重定向的跳转链合并为直达。
func (h *Handle) RedirectMerge(c *gin.Context) {
	var req pagedto.RedirectMergeReq
	if err := c.ShouldBind(&req); err != nil {
		redirectPageBack(c, "", "", pageenums.ErrInvalidParam)
		return
	}
	if _, err := h.svc.MergeRedirectChain(c.Request.Context(), &req); err != nil {
		redirectPageBack(c, req.ProjectID, "", redirectErrKey(err))
		return
	}
	redirectPageBack(c, req.ProjectID, "merged", "")
}

// redirectBulkDeleteReq 批量删除的表单形状：一个工程 + 一组源路径。
// 本页一次只显示一个工程的重定向，故 project 是标量而不是每行各带一个。
type redirectBulkDeleteReq struct {
	ProjectID string   `form:"project" binding:"required"`
	Paths     []string `form:"paths" binding:"required"`
}

// RedirectBulkDelete 批量删除重定向（POST /api/page/redirect/bulk-delete）。
//
// 逐条走同一条单条删除路径（同一个 svc.DeleteRedirect：解除访问面激活 + 清占用账）：
// 「目标不存在 / 访问面不可用」这类失败只计入跳过数，整批不中断 —— 整批回滚会让
// 用户以为「一条都没删」，然后反复重试。
//
// 结果按「已删除 N 条 / 跳过 M 条」回带。计数走独立的 dn / sk 参数、由服务端 ParseInt
// 后重新拼文案：本页 ?err= 是**词条 key 白名单**（防手拼 URL 塞任意提示），
// 直接往 err 里塞整句会绕开那道防线。
func (h *Handle) RedirectBulkDelete(c *gin.Context) {
	var req redirectBulkDeleteReq
	if err := c.ShouldBind(&req); err != nil {
		redirectPageBack(c, "", "", pageenums.ErrInvalidParam)
		return
	}
	deleted, skipped := 0, 0
	for _, raw := range req.Paths {
		path := strings.TrimSpace(raw)
		if path == "" {
			continue
		}
		if err := h.svc.DeleteRedirect(c.Request.Context(), &pagedto.RedirectDeleteReq{
			ProjectID: req.ProjectID, Path: path,
		}); err != nil {
			logger.Scene("page").With("path", path).Error(err, "批量删除重定向失败")
			skipped++
			continue
		}
		deleted++
	}
	redirectBulkBack(c, req.ProjectID, deleted, skipped)
}

// redirectBulkBack 批量删除结果回跳：ok=bulk 标记 + dn/sk 两个计数。
func redirectBulkBack(c *gin.Context, projectID string, deleted, skipped int) {
	q := url.Values{}
	if p := strings.TrimSpace(projectID); p != "" {
		q.Set("project", p)
	}
	q.Set("ok", redirectBulkOkCode)
	q.Set("dn", strconv.Itoa(deleted))
	q.Set("sk", strconv.Itoa(skipped))
	c.Redirect(http.StatusFound, redirectPagePath+"?"+q.Encode())
}

// redirectBulkDeleteText 批量删除的结果文案（成功几条、跳过几条都要说清楚 ——
// 只报「操作完成」会把部分成功静默成全部成功，用户不会再去看剩下那几条）。
func redirectBulkDeleteText(deleted, skipped int) string {
	switch {
	case deleted == 0 && skipped == 0:
		return "没有勾选任何重定向，列表未改动。"
	case skipped == 0:
		return fmt.Sprintf("已删除 %d 条重定向。", deleted)
	case deleted == 0:
		return fmt.Sprintf("%d 条重定向都未能删除，列表未改动。", skipped)
	default:
		return fmt.Sprintf("已删除 %d 条，%d 条未能删除（可能已不存在或访问面不可用）。", deleted, skipped)
	}
}

// redirectBulkCounts 读取批量删除回带的计数（ok=bulk&dn=N&sk=M）。
//
// 计数一律经 Atoi 校验、非法即 0：文案由服务端重新拼装，所以 ok / err 的取值约束
// 没有被绕过 —— 手拼 URL 只能影响两个数字，塞不进自定义文案。
func redirectBulkCounts(c *gin.Context) (deleted, skipped int, bulk bool) {
	if strings.TrimSpace(c.Query("ok")) != redirectBulkOkCode {
		return 0, 0, false
	}
	deleted, _ = strconv.Atoi(strings.TrimSpace(c.Query("dn")))
	skipped, _ = strconv.Atoi(strings.TrimSpace(c.Query("sk")))
	return deleted, skipped, true
}

// redirectPageBack 302 回管理页（PRG）。ok 是成功枚举，errKey 是失败词条 key。
func redirectPageBack(c *gin.Context, projectID, ok, errKey string) {
	q := url.Values{}
	if p := strings.TrimSpace(projectID); p != "" {
		q.Set("project", p)
	}
	if ok != "" {
		q.Set("ok", ok)
	}
	if errKey != "" {
		q.Set("err", errKey)
	}
	target := redirectPagePath
	if encoded := q.Encode(); encoded != "" {
		target += "?" + encoded
	}
	c.Redirect(http.StatusFound, target)
}

// redirectPageData 组装模板数据（i18n 与 CSRF 与后台页面同一手法：
// t 是模板取词函数，中文兜底写在模板里，真文案在 sys_i18n）。
func redirectPageData(c *gin.Context, res *pagedto.RedirectListResp, errKey string) gin.H {
	lang := response.RequestLanguage(c)
	t := templates.TranslateFunc(lang)
	token, err := builtin.GetCSRFToken(c)
	if err != nil {
		// token 拿不到时不阻断渲染：页面照常显示，提交会被 CSRF 中间件拒（与其它后台页一致）。
		token = ""
	}
	// 批量删除的结果：全成功走 Done，有跳过走 ErrText（整句，服务端按计数拼装）。
	// 跳过态用警告条 —— 与其它列表页「有跳过就进 ?err=」的口径一致，而本页的 ?err=
	// 是词条 key 白名单，装不下带数字的整句，故单开一个受控键。
	doneText, errText := "", ""
	if deleted, skipped, bulk := redirectBulkCounts(c); bulk {
		if skipped > 0 {
			errText = redirectBulkDeleteText(deleted, skipped)
		} else {
			doneText = redirectBulkDeleteText(deleted, skipped)
		}
	}
	// 全部键都预置默认值：Jet 模板读到**缺失**的键会在那一行中断渲染，
	// 而 HTTP 状态码仍是 200 —— 中断点之前的内容照常输出、之后的整块消失，
	// 排查成本极高（internal/templates/CLAUDE.md）。列表页尤其致命：看不到任何报错。
	data := gin.H{
		"lang":            lang,
		"t":               t,
		"csrf_token":      token,
		"Title":           t("admin.redirect.title", "重定向管理"),
		"PagePath":        redirectPagePath,
		"OkKey":           redirectOkKey(c.Query("ok")),
		"ErrKey":          errKey,
		"Done":            doneText,
		"ErrText":         errText,
		"Projects":        []pagedto.RedirectProjectOption{},
		"Items":           []pagedto.RedirectItem{},
		"SelectedProject": "",
		"Total":           0,
		"EffectiveCount":  0,
		"MultiHopCount":   0,
		"LoopCount":       0,
	}
	if res != nil {
		data["Projects"] = res.Projects
		data["SelectedProject"] = res.ProjectID
		data["Items"] = res.Items
		data["Total"] = res.Total
		data["EffectiveCount"] = res.EffectiveCount
		data["MultiHopCount"] = res.MultiHopCount
		data["LoopCount"] = res.LoopCount
	}
	return data
}

// redirectOkKey 成功枚举 → 词条 key（无法识别的值不显示提示，不猜）。
func redirectOkKey(code string) string {
	switch strings.TrimSpace(code) {
	case "created":
		return "admin.redirect.ok.created"
	case "deleted":
		return "admin.redirect.ok.deleted"
	case "merged":
		return "admin.redirect.ok.merged"
	default:
		return ""
	}
}

// redirectErrKeyFromCode 把 query 里带回来的 key 收敛到白名单内。
//
// 不能直接把 query 当词条 key 用：那样任意人都能构造 /api/page/redirect?err=xxx
// 让页面去查一条可控 key（轻则显示怪文案，重则成为探测词条表的入口）。
func redirectErrKeyFromCode(code string) string {
	switch strings.TrimSpace(code) {
	case "admin.redirect.err.occupied",
		"admin.redirect.err.target_missing",
		"admin.redirect.err.loop",
		"admin.redirect.err.not_found",
		"admin.redirect.err.unavailable",
		"admin.redirect.err.invalid",
		"admin.redirect.err.internal":
		return strings.TrimSpace(code)
	default:
		return ""
	}
}

// redirectErrKey 业务错误 → 词条 key。
func redirectErrKey(err error) string {
	switch {
	case err == nil:
		return ""
	case errors.Is(err, pageservice.ErrRedirectOccupied), errors.Is(err, pageservice.ErrPathOccupied):
		return "admin.redirect.err.occupied"
	case errors.Is(err, pageservice.ErrRedirectTargetMiss):
		return "admin.redirect.err.target_missing"
	case errors.Is(err, pageservice.ErrRedirectLoop):
		return "admin.redirect.err.loop"
	case errors.Is(err, pageservice.ErrRedirectNotFound):
		return "admin.redirect.err.not_found"
	case errors.Is(err, pageservice.ErrRedirectUnavailable):
		return "admin.redirect.err.unavailable"
	case errors.Is(err, pageservice.ErrInvalidParam), errors.Is(err, pageservice.ErrInvalidPath):
		return "admin.redirect.err.invalid"
	default:
		logger.Scene("page").Error(err, "重定向操作失败")
		return "admin.redirect.err.internal"
	}
}
