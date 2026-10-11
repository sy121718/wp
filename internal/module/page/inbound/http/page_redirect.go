package pagehttp

// 页面挂在 authorizedAPI 组下（/api/page/redirect）而不是 /admin/*：page 模块在
// 分发给各模块注册）。这样挂的代价是 URL 少一层「后台感」，换来的是三层链
// （Session / CSRF / Casbin）与 API 完全一致，不需要改动 routes.go。
//
// 交互约定：GET 渲染整页；三个 POST 都是原生表单提交（带 csrf_token 隐藏域），
// 完成后 302 回本页（PRG，防重复提交）。失败也走 PRG，但把错误以 **i18n key**
// 放进 query（不是中文文案）：这样同一个 URL 在英文界面下显示英文提示。

import (
	"errors"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"go_wp/internal/middleware/builtin"
	pagedto "go_wp/internal/module/page/dto"
	pageenums "go_wp/internal/module/page/enums"
	pageservice "go_wp/internal/module/page/service"
	"go_wp/internal/shell"
	"go_wp/internal/templates"
	"go_wp/pkg/logger"
	"go_wp/pkg/response"
)

// redirectPagePath 管理页路径（与 page_router.go 里的挂载点、迁移 218 的权限点
// api_path 三处必须一致；改一处必须改三处）。
const redirectPagePath = "/api/page/redirect"

// RedirectPage 重定向管理页：列出当前工程的全部 301 并给出增删与合并入口。
func (h *Handle) RedirectPage(c *gin.Context) {
	res, err := h.svc.ListRedirects(c.Request.Context(), &pagedto.RedirectListReq{ProjectID: c.Query("project")})
	if err != nil {
		logger.Scene("page").Error(err, "重定向列表读取失败")
		// 同页渲染 + 500：失败不该看起来像成功（200 会让「列表是空的」和「读不到」
		// 长得一样）。no-store：失败页不该被缓存，重试必须真的回到服务端 ——
		// 与 navigation 的整页 500 同口径。
		c.Header("Cache-Control", "no-store")
		c.HTML(http.StatusInternalServerError, "admin/page/page_redirects.html", redirectPageData(c, nil, redirectErrKey(err)))
		return
	}
	c.HTML(http.StatusOK, "admin/page/page_redirects.html", redirectPageData(c, res, ""))
}

// RedirectCreate 新增重定向。
//
// 失败**留在本页并回填输入**（redirectCreateFailure）：新建表单在抽屉里，一个业务错误
// 把用户已填的源 / 目标路径清掉，比少一句错误文案贵得多 —— 这是「写表单失败时原地
// 留住输入」的例外（同 content 的文章保存，见 internal/templates/CLAUDE.md）。
// 成功走整页提示（shell.RenderJump）：结论不再经 302 + ?ok= 回带。
func (h *Handle) RedirectCreate(c *gin.Context) {
	var req pagedto.RedirectCreateReq
	if err := c.ShouldBind(&req); err != nil {
		h.redirectCreateFailure(c, &req, pageenums.RedirectErrInvalid)
		return
	}
	if _, err := h.svc.CreateRedirect(c.Request.Context(), &req); err != nil {
		h.redirectCreateFailure(c, &req, redirectErrKey(err))
		return
	}
	redirectJump(c, true, redirectOkMsg(pageenums.RedirectResultCreated))
}

func (h *Handle) redirectCreateFailure(c *gin.Context, req *pagedto.RedirectCreateReq, errKey string) {
	res, err := h.svc.ListRedirects(c.Request.Context(), &pagedto.RedirectListReq{ProjectID: req.ProjectID})
	if err != nil {
		logger.Scene("page").Error(err, "重定向创建失败后列表读取失败")
	}
	data := redirectPageData(c, res, errKey)
	if res == nil && req.ProjectID != "" {
		data["SelectedProject"] = req.ProjectID
	}
	data["CreateSource"] = req.SourcePath
	data["CreateTarget"] = req.TargetPath
	data["CreateFailed"] = true
	c.HTML(http.StatusOK, "admin/page/page_redirects.html", data)
}

// RedirectDelete 删除一条重定向。结论走整页提示（成功 1 秒后回本页）。
func (h *Handle) RedirectDelete(c *gin.Context) {
	var req pagedto.RedirectDeleteReq
	if err := c.ShouldBind(&req); err != nil {
		redirectJump(c, false, redirectErrMsg(pageenums.RedirectErrInvalid))
		return
	}
	if err := h.svc.DeleteRedirect(c.Request.Context(), &req); err != nil {
		redirectJump(c, false, redirectErrMsg(redirectErrKey(err)))
		return
	}
	redirectJump(c, true, redirectOkMsg(pageenums.RedirectResultDeleted))
}

// RedirectMerge 把一条重定向的跳转链合并为直达。结论走整页提示。
func (h *Handle) RedirectMerge(c *gin.Context) {
	var req pagedto.RedirectMergeReq
	if err := c.ShouldBind(&req); err != nil {
		redirectJump(c, false, redirectErrMsg(pageenums.RedirectErrInvalid))
		return
	}
	if _, err := h.svc.MergeRedirectChain(c.Request.Context(), &req); err != nil {
		redirectJump(c, false, redirectErrMsg(redirectErrKey(err)))
		return
	}
	redirectJump(c, true, redirectOkMsg(pageenums.RedirectResultMerged))
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
// 结果按「已删除 N 条 / 跳过 M 条」渲染进整页提示（有跳过走失败档，不自动跳）：
// 结论文案由服务端按计数重新拼装，不进 URL。
func (h *Handle) RedirectBulkDelete(c *gin.Context) {
	var req redirectBulkDeleteReq
	if err := c.ShouldBind(&req); err != nil {
		redirectJump(c, false, redirectErrMsg(pageenums.RedirectErrInvalid))
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
	redirectJump(c, skipped == 0 && deleted > 0, outcomeMsg(pageenums.RedirectBulkDeleteOutcome(deleted, skipped)))
}

// redirectPageData 组装模板数据（i18n 与 CSRF 与后台页面同一手法：
// t 是模板取词函数，中文兜底写在模板里，真文案在 sys_i18n）。
//
// 写动作的结论不在本页回显（走 shell.RenderJump，见 page_jump.go）：ErrKey 只剩
// 「新建失败后原地回填输入」这一条来源（redirectCreateFailure），其余读侧键已删。
func redirectPageData(c *gin.Context, res *pagedto.RedirectListResp, errKey string) gin.H {
	lang := response.RequestLanguage(c)
	t := templates.TranslateFunc(lang)
	token, err := builtin.GetCSRFToken(c)
	if err != nil {
		// token 拿不到时不阻断渲染：页面照常显示，提交会被 CSRF 中间件拒（与其它后台页一致）。
		token = ""
	}
	// 全部键都预置默认值：Jet 缺键会导致渲染器返回 500 通用错误页，
	// 列表与失败回填所需的键必须由同一装配入口提供。
	data := gin.H{
		"lang":            lang,
		"t":               t,
		"csrf_token":      token,
		"Title":           t(pageenums.RedirectTitle, "重定向管理"),
		"PagePath":        redirectPagePath,
		"ErrKey":          errKey,
		"CreateSource":    "",
		"CreateTarget":    "",
		"CreateFailed":    false,
		"Projects":        []pagedto.RedirectProjectOption{},
		"Items":           []pagedto.RedirectItem{},
		"SelectedProject": "",
		"Total":           0,
		"EffectiveCount":  0,
	}
	// 计数与分档成对写（setRedirectCounts）：多跳 / 成环的档位跟计数走，
	// 分两处手写迟早漏一处 —— 那时页面上会同时出现「计数 3」和中性色。
	setRedirectCounts(data, 0, 0)
	if res != nil {
		data["Projects"] = res.Projects
		data["SelectedProject"] = res.ProjectID
		data["Items"] = res.Items
		data["Total"] = res.Total
		data["EffectiveCount"] = res.EffectiveCount
		setRedirectCounts(data, res.MultiHopCount, res.LoopCount)
	}
	return data
}

// setRedirectCounts 写重定向统计的计数事实（徽标分档由模板按计数选，见
// internal/templates/admin/page/page_redirects.html）：有就是异常 —— 多跳 warn、成环 danger，
// 计数为 0 则是中性档。
func setRedirectCounts(data gin.H, multiHop, loop int) {
	data["MultiHopCount"] = multiHop
	data["LoopCount"] = loop
}

// redirectErrKey 业务错误 → 词条 key。
func redirectErrKey(err error) string {
	switch {
	case err == nil:
		return ""
	case errors.Is(err, pageservice.ErrRedirectOccupied), errors.Is(err, pageservice.ErrPathOccupied):
		return pageenums.RedirectErrOccupied
	case errors.Is(err, pageservice.ErrRedirectTargetMiss):
		return pageenums.RedirectErrTargetMissing
	case errors.Is(err, pageservice.ErrRedirectLoop):
		return pageenums.RedirectErrLoop
	case errors.Is(err, pageservice.ErrRedirectNotFound):
		return pageenums.RedirectErrNotFound
	case errors.Is(err, pageservice.ErrRedirectUnavailable):
		return pageenums.RedirectErrUnavailable
	case errors.Is(err, pageservice.ErrInvalidParam), errors.Is(err, pageservice.ErrInvalidPath):
		return pageenums.RedirectErrInvalid
	default:
		logger.Scene("page").Error(err, "重定向操作失败")
		return pageenums.RedirectErrInternal
	}
}

// —— 本功能自己的回跳与回执（不借 page_jump.go 的共享出口）——

// redirectBackKeys 回跳筛选键：渲染时拼进表单 action 的 query、POST 回来时由
// shell.BackPath 读回 —— 两处必须同一份，否则「写完跳回去筛选静默丢了」。
var redirectBackKeys = []string{"project"}

func redirectBack(c *gin.Context) string {
	return shell.BackPath(c, redirectPagePath, redirectBackKeys...)
}

// redirectBackMsg 回跳链接文字（复用本页标题词条，不新增全站词条）。
var redirectBackMsg = pageMsg{Key: pageenums.RedirectTitle, Fallback: "重定向管理"}

// redirectJump 回重定向管理页的提示页出口。
func redirectJump(c *gin.Context, ok bool, msg pageMsg) {
	pageJump(c, ok, msg, redirectBack(c), redirectBackMsg)
}

// redirectOkMsg 重定向写动作的成功回执（key 与模板时代同一批，不新增词条）。
//
// 值 → 文案的映射归 pageenums.RedirectResultLabel（值域与词条 key 都属本模块 enums），
// 这里只把 (key, 兜底) 转成本包的事实形状；调用点传的是 pageenums.RedirectResult* 常量。
func redirectOkMsg(code string) pageMsg {
	key, fallback := pageenums.RedirectResultLabel(strings.TrimSpace(code))
	return pageMsg{Key: key, Fallback: fallback}
}

// redirectErrMsg 重定向写动作的失败回执：业务错误 key（未命中落「操作未完成」）。
func redirectErrMsg(key string) pageMsg {
	if strings.TrimSpace(key) == "" {
		return pageMsg{}
	}
	return pageMsg{Key: strings.TrimSpace(key), Fallback: "操作未完成"}
}
