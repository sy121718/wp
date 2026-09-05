package pagehttp

import (
	"errors"
	"net/http"

	pagecontract "go_wp/internal/module/page/contract"
	pagedto "go_wp/internal/module/page/dto"
	pageenums "go_wp/internal/module/page/enums"
	pageservice "go_wp/internal/module/page/service"
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
		response.ParamError(c, pageenums.ErrInvalidParam)
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
		response.ParamError(c, pageenums.ErrInvalidParam)
		return
	}
	res, err := h.svc.Detail(c.Request.Context(), &req)
	if err != nil {
		response.ErrorWithMessage(c, pageErrorStatus(err), err.Error())
		return
	}
	response.SuccessWithMessage(c, pageenums.MsgPageDetail, res)
}

// List 列出全部页面摘要。
func (h *Handle) List(c *gin.Context) {
	res, err := h.svc.List(c.Request.Context(), c.Query("themeId"))
	if err != nil {
		response.ErrorWithMessage(c, http.StatusInternalServerError, err.Error())
		return
	}
	response.Success(c, res)
}

// SaveDraft 保存完整 Page AST 并追加不可变 Revision。
func (h *Handle) SaveDraft(c *gin.Context) {
	var req pagedto.SaveDraftReq
	if err := c.ShouldBindJSON(&req); err != nil {
		response.ParamError(c, pageenums.ErrInvalidParam)
		return
	}
	res, err := h.svc.SaveDraft(c.Request.Context(), &req)
	if err != nil {
		response.ErrorWithMessage(c, pageErrorStatus(err), err.Error())
		return
	}
	response.SuccessWithMessage(c, pageenums.MsgDraftSaved, res)
}

// Build 基于当前草稿构建并暂存产物。
func (h *Handle) Build(c *gin.Context) {
	var req pagedto.BuildReq
	if err := c.ShouldBindJSON(&req); err != nil {
		response.ParamError(c, pageenums.ErrInvalidParam)
		return
	}
	res, err := h.svc.Build(c.Request.Context(), &req)
	if err != nil {
		response.ErrorWithMessage(c, pageErrorStatus(err), err.Error())
		return
	}
	response.SuccessWithMessage(c, pageenums.MsgBuildReady, res)
}

// Publish 激活暂存产物。
func (h *Handle) Publish(c *gin.Context) {
	var req pagedto.PublishReq
	if err := c.ShouldBindJSON(&req); err != nil {
		response.ParamError(c, pageenums.ErrInvalidParam)
		return
	}
	res, err := h.svc.Publish(c.Request.Context(), &req)
	if err != nil {
		response.ErrorWithMessage(c, pageErrorStatus(err), err.Error())
		return
	}
	response.SuccessWithMessage(c, pageenums.MsgPublished, res)
}

// Rollback 回滚到历史产物。
func (h *Handle) Rollback(c *gin.Context) {
	var req pagedto.RollbackReq
	if err := c.ShouldBindJSON(&req); err != nil {
		response.ParamError(c, pageenums.ErrInvalidParam)
		return
	}
	res, err := h.svc.Rollback(c.Request.Context(), &req)
	if err != nil {
		response.ErrorWithMessage(c, pageErrorStatus(err), err.Error())
		return
	}
	response.SuccessWithMessage(c, pageenums.MsgRollbackDone, res)
}

// UpdateURL 修改访问路径，旧路径 301 或取消激活。
func (h *Handle) UpdateURL(c *gin.Context) {
	var req pagedto.UpdateURLReq
	if err := c.ShouldBindJSON(&req); err != nil {
		response.ParamError(c, pageenums.ErrInvalidParam)
		return
	}
	res, err := h.svc.UpdateURL(c.Request.Context(), &req)
	if err != nil {
		response.ErrorWithMessage(c, pageErrorStatus(err), err.Error())
		return
	}
	response.SuccessWithMessage(c, pageenums.MsgURLUpdated, res)
}

// Delete 软删页面并释放其全部路径占用。
func (h *Handle) Delete(c *gin.Context) {
	var req pagedto.DeleteReq
	if err := c.ShouldBindJSON(&req); err != nil {
		response.ParamError(c, pageenums.ErrInvalidParam)
		return
	}
	if err := h.svc.Delete(c.Request.Context(), &req); err != nil {
		response.ErrorWithMessage(c, pageErrorStatus(err), err.Error())
		return
	}
	response.SuccessWithMessage(c, pageenums.MsgPageDeleted, nil)
}

// ListRevisions 查询 Page 草稿修订历史。
func (h *Handle) ListRevisions(c *gin.Context) {
	var req pagedto.RevisionReq
	if err := c.ShouldBindQuery(&req); err != nil {
		response.ParamError(c, pageenums.ErrInvalidParam)
		return
	}
	res, err := h.svc.ListRevisions(c.Request.Context(), &req)
	if err != nil {
		response.ErrorWithMessage(c, pageErrorStatus(err), err.Error())
		return
	}
	response.SuccessWithMessage(c, pageenums.MsgRevisionsListed, res)
}

// pageErrorStatus 把 page service 返回的错误分类映射为 HTTP 状态码。
// 修复前按 err.Error() 中文文案 strings.Contains 匹配（文案改动即失效）；
// 修复后基于本包 sentinel error 精确 errors.Is 判定（文案与 pageenums 一致）。
// ErrInvalidParam（nil/空 ID 等参数错误）映射 400，与资源不存在（404）区分。
func pageErrorStatus(err error) int {
	switch {
	case errors.Is(err, pageservice.ErrNoStagedArtifact):
		return http.StatusConflict
	case errors.Is(err, pageservice.ErrPageNotFound),
		errors.Is(err, pageservice.ErrProjectNotFound),
		errors.Is(err, pageservice.ErrRollbackTargetMiss):
		return http.StatusNotFound
	case errors.Is(err, pageservice.ErrDraftVersionConflict),
		errors.Is(err, pageservice.ErrPathOccupied),
		errors.Is(err, pageservice.ErrRebuildRequired):
		return http.StatusConflict
	case errors.Is(err, pageservice.ErrInvalidParam),
		errors.Is(err, pageservice.ErrInvalidKind),
		errors.Is(err, pageservice.ErrInvalidDocument),
		errors.Is(err, pageservice.ErrInvalidPath):
		return http.StatusBadRequest
	default:
		return http.StatusInternalServerError
	}
}
