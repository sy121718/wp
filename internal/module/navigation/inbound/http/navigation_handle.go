package navigationhttp

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	ginbinding "github.com/gin-gonic/gin/binding"

	navigationcontract "go_wp/internal/module/navigation/contract"
	navigationdto "go_wp/internal/module/navigation/dto"
	navigationenums "go_wp/internal/module/navigation/enums"
	"go_wp/pkg/binding"
	"go_wp/pkg/logger"
	"go_wp/pkg/response"
)

type Handle struct {
	svc navigationcontract.NavigationService

	writerSvc navigationcontract.TranslationWriteService
}

func NewHandle(svc navigationcontract.NavigationService) *Handle {
	return &Handle{svc: svc}
}

func (h *Handle) SetTranslationWriteService(s navigationcontract.TranslationWriteService) {
	h.writerSvc = s
}

func (h *Handle) Create(c *gin.Context) {
	body, raw, ok := navigationWriteBody(c)
	if !ok {
		return
	}
	action, hasAction, shapeOK := navigationWriteAction(raw)
	if !shapeOK {
		response.ParamError(c)
		return
	}
	if hasAction {
		if action != navigationAddSourceAction {
			response.ParamError(c)
			return
		}
		h.createFromSource(c, raw)
		return
	}
	if navigationDeleteHasKey(raw, "ids") {

		response.ParamError(c)
		return
	}
	req := &navigationdto.CreateReq{}
	if err := ginbinding.JSON.BindBody(body, req); err != nil {
		response.ParamError(c)
		return
	}
	res, err := h.svc.Create(c.Request.Context(), req)
	if err != nil {
		response.ErrorWithMessage(c, navigationErrorStatus(err), navigationErrText(c, err))
		return
	}
	response.SuccessWithMessage(c, navigationenums.MsgCreateSuccess, res)
}

func (h *Handle) Update(c *gin.Context) {
	env, ok := binding.JSONLimited[navigationdto.UpdateEnvelope](c, 1<<20)
	if !ok {
		return
	}

	if err := env.Validate(); err != nil {
		response.ParamError(c)
		return
	}
	switch env.Route() {
	case navigationdto.RouteTranslations:
		h.updateTranslations(c, env)
	case navigationdto.RouteMove:
		h.moveNavigationEnvelope(c, env)
	case navigationdto.RouteLegacy:
		req, err := env.DecodeLegacy()
		if err != nil {
			response.ParamError(c)
			return
		}
		res, err := h.svc.Update(c.Request.Context(), req)
		if err != nil {
			response.ErrorWithMessage(c, navigationErrorStatus(err), navigationErrText(c, err))
			return
		}
		response.SuccessWithMessage(c, navigationenums.MsgUpdateSuccess, res)
	default:

		response.ParamError(c)
	}
}

func (h *Handle) updateTranslations(c *gin.Context, env *navigationdto.UpdateEnvelope) {
	req, err := env.DecodeTranslation()
	if err != nil {
		response.ParamError(c)
		return
	}
	if h.writerSvc == nil {

		logger.Scene("navigation").Warn("导航译文写服务未装配，拒绝译文保存")
		response.ErrorStatusAuto(c, "navigation", navigationenums.ErrTranslationWriteUnavailableError())
		return
	}
	rows := make([]navigationcontract.TranslationWriteRow, 0, len(req.Rows))
	for _, row := range req.Rows {
		rows = append(rows, navigationcontract.TranslationWriteRow{
			Context: row.Context, Hash: row.Hash, Target: row.Target,
		})
	}
	result, err := h.writerSvc.WriteTranslations(c.Request.Context(), &navigationcontract.TranslationWriteReq{
		ProjectID: req.ProjectID, Lang: req.Lang, Rows: rows,
	})
	if err != nil {

		response.ErrorStatusAuto(c, "navigation", err)
		return
	}
	if result == nil {

		logger.Scene("navigation").Warn("导航译文写服务返回空回执，按失败处理")
		response.ErrorStatusAuto(c, "navigation", navigationenums.ErrTranslationWriteUnavailableError())
		return
	}

	response.SuccessMsg(c, navigationenums.SavedMsg(strconv.Itoa(result.Written)), result)
}

func (h *Handle) Get(c *gin.Context) {
	req, ok := binding.Form[navigationdto.GetReq](c)
	if !ok {
		return
	}
	res, err := h.svc.Get(c.Request.Context(), req)
	if err != nil {
		response.ErrorWithMessage(c, navigationErrorStatus(err), navigationErrText(c, err))
		return
	}
	response.SuccessWithMessage(c, navigationenums.MsgDetailSuccess, res)
}

func (h *Handle) List(c *gin.Context) {
	req, ok := binding.Form[navigationdto.ListReq](c)
	if !ok {
		return
	}
	list, err := h.svc.List(c.Request.Context(), req)
	if err != nil {
		response.ErrorWithMessage(c, navigationErrorStatus(err), navigationErrText(c, err))
		return
	}
	response.SuccessWithMessage(c, navigationenums.MsgListSuccess, list)
}

func (h *Handle) Delete(c *gin.Context) {
	body, raw, ok := navigationWriteBody(c)
	if !ok {
		return
	}

	hasID := navigationDeleteHasKey(raw, "id")
	hasIDs := navigationDeleteHasKey(raw, "ids")
	if hasID && hasIDs {
		response.ParamError(c)
		return
	}
	if hasIDs {
		if !navigationDeleteShapeOK(raw, navigationDeleteBulkKeys) {
			response.ParamError(c)
			return
		}
		h.deleteBulk(c, raw)
		return
	}
	if !hasID || !navigationDeleteShapeOK(raw, navigationDeleteSingleKeys) {
		response.ParamError(c)
		return
	}
	req := &navigationdto.DeleteReq{}

	if err := ginbinding.JSON.BindBody(body, req); err != nil {
		response.ParamError(c)
		return
	}
	if err := h.svc.Delete(c.Request.Context(), req); err != nil {
		response.ErrorWithMessage(c, navigationErrorStatus(err), navigationErrText(c, err))
		return
	}
	response.SuccessWithMessage(c, navigationenums.MsgDeleteSuccess, nil)
}

func navigationErrorStatus(err error) int {
	fact, business := navigationFact(err)
	if !business {
		return http.StatusInternalServerError
	}

	key, _, _ := strings.Cut(fact.Key, "|")
	switch key {
	case navigationenums.ErrNotFound:
		return http.StatusNotFound

	case navigationenums.ErrStaleVersion:
		return http.StatusConflict
	case navigationenums.ErrInvalidParam,
		navigationenums.ErrInvalidKind,
		navigationenums.ErrPathTaken,

		navigationenums.ErrSortTied:
		return http.StatusBadRequest
	default:
		return http.StatusInternalServerError
	}
}
