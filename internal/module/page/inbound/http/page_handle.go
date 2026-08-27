package pagehttp

import (
	"net/http"
	"strings"

	pagecontract "go_wp/internal/module/page/contract"
	pagedto "go_wp/internal/module/page/dto"
	pageenums "go_wp/internal/module/page/enums"
	"go_wp/pkg/response"

	"github.com/gin-gonic/gin"
)

// Handle page 模块 HTTP 处理器。
type Handle struct {
	svc pagecontract.PageService
}

// NewHandle 创建 page HTTP 处理器。
func NewHandle(svc pagecontract.PageService) *Handle {
	return &Handle{svc: svc}
}

// Create 创建 Page、初始 Draft 和初始 Revision。
func (h *Handle) Create(c *gin.Context) {
	var req pagedto.CreateReq
	if err := c.ShouldBindJSON(&req); err != nil {
		response.ParamError(c, pageenums.ErrInvalidDocument)
		return
	}
	res, err := h.svc.Create(c.Request.Context(), &req)
	if err != nil {
		response.ErrorWithMessage(c, pageErrorStatus(err), err.Error())
		return
	}
	response.SuccessWithMessage(c, pageenums.MsgPageCreated, res)
}

// Detail 查询 Page 当前草稿。
func (h *Handle) Detail(c *gin.Context) {
	var req pagedto.DetailReq
	if err := c.ShouldBindQuery(&req); err != nil {
		response.ParamError(c, pageenums.ErrPageNotFound)
		return
	}
	res, err := h.svc.Detail(c.Request.Context(), &req)
	if err != nil {
		response.ErrorWithMessage(c, pageErrorStatus(err), err.Error())
		return
	}
	response.SuccessWithMessage(c, pageenums.MsgPageDetail, res)
}

// SaveDraft 保存完整 Page AST 并追加不可变 Revision。
func (h *Handle) SaveDraft(c *gin.Context) {
	var req pagedto.SaveDraftReq
	if err := c.ShouldBindJSON(&req); err != nil {
		response.ParamError(c, pageenums.ErrInvalidDocument)
		return
	}
	res, err := h.svc.SaveDraft(c.Request.Context(), &req)
	if err != nil {
		response.ErrorWithMessage(c, pageErrorStatus(err), err.Error())
		return
	}
	response.SuccessWithMessage(c, pageenums.MsgDraftSaved, res)
}

// ListRevisions 查询 Page 草稿修订历史。
func (h *Handle) ListRevisions(c *gin.Context) {
	var req pagedto.RevisionReq
	if err := c.ShouldBindQuery(&req); err != nil {
		response.ParamError(c, pageenums.ErrPageNotFound)
		return
	}
	res, err := h.svc.ListRevisions(c.Request.Context(), &req)
	if err != nil {
		response.ErrorWithMessage(c, pageErrorStatus(err), err.Error())
		return
	}
	response.SuccessWithMessage(c, pageenums.MsgRevisionsListed, res)
}

func pageErrorStatus(err error) int {
	message := err.Error()
	switch {
	case strings.Contains(message, pageenums.ErrPageNotFound), strings.Contains(message, pageenums.ErrProjectNotFound):
		return http.StatusNotFound
	case strings.Contains(message, pageenums.ErrDraftVersionConflict), strings.Contains(message, pageenums.ErrPathOccupied):
		return http.StatusConflict
	case strings.Contains(message, pageenums.ErrInvalidKind), strings.Contains(message, pageenums.ErrInvalidDocument), strings.Contains(message, pageenums.ErrInvalidPath):
		return http.StatusBadRequest
	default:
		return http.StatusInternalServerError
	}
}
