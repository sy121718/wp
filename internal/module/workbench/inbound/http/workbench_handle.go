package workbenchhttp

import (
	"context"

	"go_wp/internal/middleware/builtin"
	json "go_wp/pkg/jsonx"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	navigationdto "go_wp/internal/module/navigation/dto"
	workbenchdto "go_wp/internal/module/workbench/dto"
	workbenchenums "go_wp/internal/module/workbench/enums"
	workbenchservice "go_wp/internal/module/workbench/service"
	"go_wp/pkg/binding"
	"go_wp/pkg/logger"
	"go_wp/pkg/response"
	seoscore "go_wp/pkg/seo"
)

type NavigationPickerPort interface {
	List(ctx context.Context, req *navigationdto.ListReq) (list []*navigationdto.NavigationResp, err error)
	Create(ctx context.Context, req *navigationdto.CreateReq) (res *navigationdto.NavigationResp, err error)
}

func (h *Handle) SetNavigationPicker(p NavigationPickerPort) {
	if h == nil {
		return
	}
	h.svc.SetNavigationPicker(p)
}

func (h *Handle) InspectorNavigationCreate(c *gin.Context) {
	if h == nil || h.svc == nil || h.svc.NavigationPicker() == nil {

		response.ErrorWithMessage(c, http.StatusServiceUnavailable, response.InternalText(c))
		return
	}
	// 检查器发起的是 urlencoded 表单（editor-app/host.js），只读 body、不合并 query。
	req, err := binding.ParseForm[workbenchdto.NavigationCreateReq](c)
	if err != nil {
		response.ParamError(c)
		return
	}
	projectID, title := strings.TrimSpace(req.ProjectID), strings.TrimSpace(req.Title)
	path, kind := strings.TrimSpace(req.Path), strings.TrimSpace(req.Kind)
	if projectID == "" || title == "" || path == "" || kind == "" {
		response.ParamError(c)
		return
	}
	created, err := h.svc.NavigationPicker().Create(c.Request.Context(), &navigationdto.CreateReq{
		ProjectID: projectID, Title: title, Path: path, Kind: kind,
	})
	if err != nil || created == nil {
		logger.Scene("workbench").With("user_id", builtin.CurrentUserID(c)).
			With("project_id", projectID).Error(err, "检查器新建菜单项失败")
		response.ErrorWithMessage(c, http.StatusBadRequest, response.MessageOf(c, "workbench", err))
		return
	}
	response.Success(c, gin.H{
		"id": created.ID, "title": created.Title, "path": created.Path,
		"kind": created.Kind, "label": workbenchservice.NavigationKindLabel(created.Kind, workbenchTrFunc(c)) + " · " + created.Title,
	})
}

func (h *Handle) SEOScore(c *gin.Context) {
	req, err := binding.ParseForm[workbenchdto.SEOScoreReq](c)
	if err != nil {
		response.ParamError(c)
		return
	}
	document := json.RawMessage(req.DraftDocument)
	if len(document) == 0 {
		response.ErrorWithMessage(c, http.StatusBadRequest, workbenchShortText(c, workbenchenums.ErrDraftDocumentEmpty))
		return
	}
	res, err := seoscore.ScoreDocument(document, req.URL, requestScoreLang(c))
	if err != nil {
		response.BadRequestForPage(c, "seo_score", err)
		return
	}
	response.Success(c, res)
}

func requestScoreLang(c *gin.Context) string {
	return response.RequestLanguage(c)
}
