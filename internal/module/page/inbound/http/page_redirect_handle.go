package pagehttp

// page_redirect_handle.go — 重定向管理页与增删改接口（审计 SEO-025）。
//
// 页面挂在 authorizdedAPI 组下（/api/page/redirect）而不是 /admin/*：page 模块在
// routes.go 里只拿到 authorizedAPI 这一个已装配好的组（页面路由由 dashboard 模块
// 注册到引擎根）。这样挂的代价是 URL 少一层「后台感」，换来的是三层链
// （Session / CSRF / Casbin）与 API 完全一致，不需要改动 routes.go。
//
// 交互约定：GET 渲染整页；三个 POST 都是原生表单提交（带 csrf_token 隐藏域），
// 完成后 302 回本页（PRG，防重复提交）。失败也走 PRG，但把错误以 **i18n key**
// 放进 query（不是中文文案）：这样同一个 URL 在英文界面下显示英文提示。

import (
	"errors"
	"net/http"
	"net/url"
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
