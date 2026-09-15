// Package presentationhttp presentation 模块 HTTP 入口（0-A2）。
package presentationhttp

import (
	"net/http"

	"github.com/gin-gonic/gin"

	presentationcontract "go_wp/internal/module/presentation/contract"
	presentationdto "go_wp/internal/module/presentation/dto"
	presentationenums "go_wp/internal/module/presentation/enums"
	"go_wp/pkg/response"
)

// Handle presentation 接口处理器。
type Handle struct {
	svc presentationcontract.PresentationService
}

// NewHandle 构造。
func NewHandle(svc presentationcontract.PresentationService) *Handle { return &Handle{svc: svc} }

// CreateInstance 创建自动发布实例。
func (h *Handle) CreateInstance(c *gin.Context) {
	req := &presentationdto.CreateInstanceReq{}
	if err := c.ShouldBindJSON(req); err != nil {
		response.ErrorWithMessage(c, http.StatusBadRequest, presentationenums.ErrInvalidParam)
		return
	}
	res, err := h.svc.CreateInstance(c.Request.Context(), req)
	if err != nil {
		response.ErrorAuto(c, http.StatusBadRequest, "presentation", err)
		return
	}
	response.SuccessWithMessage(c, presentationenums.MsgCreateSuccess, res)
}

// Rebuild 实体更新后重建。
func (h *Handle) Rebuild(c *gin.Context) {
	req := &presentationdto.RebuildReq{}
	if err := c.ShouldBindJSON(req); err != nil {
		response.ErrorWithMessage(c, http.StatusBadRequest, presentationenums.ErrInvalidParam)
		return
	}
	res, err := h.svc.Rebuild(c.Request.Context(), req)
	if err != nil {
		response.ErrorAuto(c, http.StatusBadRequest, "presentation", err)
		return
	}
	response.SuccessWithMessage(c, presentationenums.MsgRebuildSuccess, res)
}

// GetByEntity 按内容实体查询实例（后台「详情页模板」页读当前绑定）。
func (h *Handle) GetByEntity(c *gin.Context) {
	req := &presentationdto.GetByEntityReq{}
	if err := c.ShouldBind(req); err != nil {
		response.ErrorWithMessage(c, http.StatusBadRequest, presentationenums.ErrInvalidParam)
		return
	}
	res, err := h.svc.GetByEntity(c.Request.Context(), req)
	if err != nil {
		response.ErrorAuto(c, http.StatusNotFound, "presentation", err)
		return
	}
	response.SuccessWithMessage(c, presentationenums.MsgDetailSuccess, res)
}

// Preview 发布前预览模板渲染效果（issue #14 验收 3）：只读渲染，不落库不激活。
func (h *Handle) Preview(c *gin.Context) {
	req := &presentationdto.PreviewInstanceReq{}
	if err := c.ShouldBind(req); err != nil {
		response.ErrorWithMessage(c, http.StatusBadRequest, presentationenums.ErrInvalidParam)
		return
	}
	res, err := h.svc.PreviewInstance(c.Request.Context(), req)
	if err != nil {
		response.ErrorAuto(c, http.StatusBadRequest, "presentation", err)
		return
	}
	response.SuccessWithMessage(c, presentationenums.MsgPreviewSuccess, res)
}

// UpdateURL 修改已发布实例的线上路径（改 URL）：新路径构建激活后，
// 旧路径按 WithRedirect 登记 301 或取消激活。
//
// 用 ShouldBind 而非 ShouldBindJSON：后台入口走表单 POST（dashboard 的
// 详情页模板页 / 文章发布区块），JSON 与表单都要能绑。
func (h *Handle) UpdateURL(c *gin.Context) {
	req := &presentationdto.UpdateURLReq{}
	if err := c.ShouldBind(req); err != nil {
		response.ErrorWithMessage(c, http.StatusBadRequest, presentationenums.ErrInvalidParam)
		return
	}
	res, err := h.svc.UpdateURL(c.Request.Context(), req)
	if err != nil {
		response.ErrorAuto(c, http.StatusBadRequest, "presentation", err)
		return
	}
	response.SuccessWithMessage(c, presentationenums.MsgUpdateURLSuccess, res)
}

// Get 实例详情。
func (h *Handle) Get(c *gin.Context) {
	req := &presentationdto.GetReq{}
	if err := c.ShouldBind(req); err != nil {
		response.ErrorWithMessage(c, http.StatusBadRequest, presentationenums.ErrInvalidParam)
		return
	}
	res, err := h.svc.Get(c.Request.Context(), req)
	if err != nil {
		response.ErrorAuto(c, http.StatusNotFound, "presentation", err)
		return
	}
	response.SuccessWithMessage(c, presentationenums.MsgDetailSuccess, res)
}

// List 实例列表。
func (h *Handle) List(c *gin.Context) {
	req := &presentationdto.ListReq{}
	if err := c.ShouldBind(req); err != nil {
		response.ErrorWithMessage(c, http.StatusBadRequest, presentationenums.ErrInvalidParam)
		return
	}
	list, err := h.svc.List(c.Request.Context(), req)
	if err != nil {
		response.ErrorAuto(c, http.StatusBadRequest, "presentation", err)
		return
	}
	response.SuccessWithMessage(c, presentationenums.MsgListSuccess, list)
}

// Delete 删除实例。
func (h *Handle) Delete(c *gin.Context) {
	req := &presentationdto.DeleteReq{}
	if err := c.ShouldBind(req); err != nil {
		response.ErrorWithMessage(c, http.StatusBadRequest, presentationenums.ErrInvalidParam)
		return
	}
	if err := h.svc.Delete(c.Request.Context(), req); err != nil {
		response.ErrorAuto(c, http.StatusBadRequest, "presentation", err)
		return
	}
	response.SuccessWithMessage(c, presentationenums.MsgDeleteSuccess, nil)
}
