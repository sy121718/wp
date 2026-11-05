package pagehttp

import (
	"errors"
	"go_wp/pkg/backpath"
	"strings"

	"github.com/gin-gonic/gin"

	"go_wp/internal/middleware/builtin"
	pagedto "go_wp/internal/module/page/dto"
	pageenums "go_wp/internal/module/page/enums"
	pageservice "go_wp/internal/module/page/service"
	"go_wp/pkg/binding"
	"go_wp/pkg/logger"
	pageview "go_wp/pkg/view"
)

const redirectPagePath = "/api/page/redirect"

func (h *Handle) RedirectPage(c *gin.Context) {
	res, err := h.svc.ListRedirects(c.Request.Context(), &pagedto.RedirectListReq{ProjectID: c.Query("project")})
	if err != nil {

		pageview.Fail(c, err)
		return
	}
	pageview.RenderTemplate(c, RedirectPageTemplate, redirectPageData(c, res, ""))
}

func (h *Handle) RedirectCreate(c *gin.Context) {

	req, bindErr := binding.ParseForm[pagedto.RedirectCreateReq](c)
	if bindErr != nil {
		h.redirectCreateFailure(c, req, pageenums.RedirectErrInvalid)
		return
	}
	if _, err := h.svc.CreateRedirect(c.Request.Context(), req); err != nil {
		h.redirectCreateFailure(c, req, redirectErrKey(err))
		return
	}
	redirectJump(c, true, redirectOkMsg(pageenums.RedirectResultCreated))
}

func (h *Handle) redirectCreateFailure(c *gin.Context, req *pagedto.RedirectCreateReq, errKey string) {
	res, err := h.svc.ListRedirects(c.Request.Context(), &pagedto.RedirectListReq{ProjectID: req.ProjectID})
	if err != nil {
		logger.Scene("page").Error(err, "重定向创建失败后列表读取失败")
	}
	view := redirectPageData(c, res, errKey)
	if res == nil && req.ProjectID != "" {
		view.SelectedProject = req.ProjectID
	}
	view.CreateSource = req.SourcePath
	view.CreateTarget = req.TargetPath
	view.CreateFailed = true
	pageview.RenderTemplate(c, RedirectPageTemplate, view)
}

func (h *Handle) RedirectDelete(c *gin.Context) {
	req, bindErr := binding.ParseForm[pagedto.RedirectDeleteReq](c)
	if bindErr != nil {
		redirectJump(c, false, redirectErrMsg(pageenums.RedirectErrInvalid))
		return
	}
	if err := h.svc.DeleteRedirect(c.Request.Context(), req); err != nil {
		redirectJump(c, false, redirectErrMsg(redirectErrKey(err)))
		return
	}
	redirectJump(c, true, redirectOkMsg(pageenums.RedirectResultDeleted))
}

func (h *Handle) RedirectMerge(c *gin.Context) {
	req, bindErr := binding.ParseForm[pagedto.RedirectMergeReq](c)
	if bindErr != nil {
		redirectJump(c, false, redirectErrMsg(pageenums.RedirectErrInvalid))
		return
	}
	if _, err := h.svc.MergeRedirectChain(c.Request.Context(), req); err != nil {
		redirectJump(c, false, redirectErrMsg(redirectErrKey(err)))
		return
	}
	redirectJump(c, true, redirectOkMsg(pageenums.RedirectResultMerged))
}

type redirectBulkDeleteReq struct {
	ProjectID string   `form:"project" binding:"required"`
	Paths     []string `form:"paths" binding:"required"`
}

func (h *Handle) RedirectBulkDelete(c *gin.Context) {
	req, err := binding.ParseForm[redirectBulkDeleteReq](c)
	if err != nil {
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

func redirectPageData(c *gin.Context, res *pagedto.RedirectListResp, errKey string) *RedirectPageResp {
	token, err := builtin.GetCSRFToken(c)
	if err != nil {

		token = ""
	}

	_ = token
	view := &RedirectPageResp{
		PagePath:        redirectPagePath,
		ErrKey:          errKey,
		Projects:        []pagedto.RedirectProjectOptionResp{},
		Items:           []pagedto.RedirectItemResp{},
		SelectedProject: "",
	}
	if res != nil {
		view.Projects = res.Projects
		view.SelectedProject = res.ProjectID
		view.Items = res.Items
		view.Total = res.Total
		view.EffectiveCount = res.EffectiveCount
		view.MultiHopCount = res.MultiHopCount
		view.LoopCount = res.LoopCount
	}
	return view
}

func setRedirectCounts(data gin.H, multiHop, loop int) {
	data["MultiHopCount"] = multiHop
	data["LoopCount"] = loop
}

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

var redirectBackKeys = []string{"project"}

func redirectBack(c *gin.Context) string {
	return backpath.FromRequest(c.Request.URL.Query(), redirectPagePath, "/admin", "page", redirectBackKeys...)
}

var redirectBackMsg = pageMsg{Key: pageenums.RedirectTitle, Fallback: "重定向管理"}

func redirectJump(c *gin.Context, ok bool, msg pageMsg) {
	pageJump(c, ok, msg, redirectBack(c), redirectBackMsg)
}

func redirectOkMsg(code string) pageMsg {
	key, fallback := pageenums.RedirectResultLabel(strings.TrimSpace(code))
	return pageMsg{Key: key, Fallback: fallback}
}

func redirectErrMsg(key string) pageMsg {
	if strings.TrimSpace(key) == "" {
		return pageMsg{}
	}
	return pageMsg{Key: strings.TrimSpace(key), Fallback: "操作未完成"}
}
