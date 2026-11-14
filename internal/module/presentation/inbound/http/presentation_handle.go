package presentationhttp

import (
	"net/http"

	"github.com/gin-gonic/gin"

	presentationcontract "go_wp/internal/module/presentation/contract"
	presentationdto "go_wp/internal/module/presentation/dto"
	presentationenums "go_wp/internal/module/presentation/enums"
	"go_wp/pkg/binding"
	"go_wp/pkg/response"
)

type Handle struct {
	svc presentationcontract.PresentationService
}

func NewHandle(svc presentationcontract.PresentationService) *Handle { return &Handle{svc: svc} }

func (h *Handle) CreateInstance(c *gin.Context) {
	var req presentationdto.CreateInstanceReq
	if err := c.ShouldBindJSON(&req); err != nil {
		response.ParamError(c)
		return
	}
	res, err := h.svc.CreateInstance(c.Request.Context(), &req)
	if err != nil {
		response.ErrorAuto(c, http.StatusBadRequest, "presentation", err)
		return
	}
	response.SuccessWithMessage(c, presentationenums.MsgCreateSuccess, res)
}

func (h *Handle) Rebuild(c *gin.Context) {
	var req presentationdto.RebuildReq
	if err := c.ShouldBindJSON(&req); err != nil {
		response.ParamError(c)
		return
	}
	res, err := h.svc.Rebuild(c.Request.Context(), &req)
	if err != nil {
		response.ErrorAuto(c, http.StatusBadRequest, "presentation", err)
		return
	}
	response.SuccessWithMessage(c, presentationenums.MsgRebuildSuccess, res)
}

// GetByEntity / Get / List 是 GET 读端点：参数在查询串上，不在 body。
// 用 ParseForm 会把 GET 绑成零值（带 required 的 400、列表静默空集），
// 这里用显式查询入口声明唯一输入协议。
func (h *Handle) GetByEntity(c *gin.Context) {
	req, ok := binding.Query[presentationdto.GetByEntityReq](c)
	if !ok {
		return
	}
	res, err := h.svc.GetByEntity(c.Request.Context(), req)
	if err != nil {
		response.ErrorAuto(c, http.StatusNotFound, "presentation", err)
		return
	}
	response.SuccessWithMessage(c, presentationenums.MsgDetailSuccess, res)
}

func (h *Handle) Preview(c *gin.Context) {
	req, err := binding.ParseForm[presentationdto.PreviewInstanceReq](c)
	if err != nil {
		response.ParamError(c)
		return
	}
	res, err := h.svc.PreviewInstance(c.Request.Context(), req)
	if err != nil {
		response.ErrorAuto(c, http.StatusBadRequest, "presentation", err)
		return
	}
	response.SuccessWithMessage(c, presentationenums.MsgPreviewSuccess, res)
}

func (h *Handle) UpdateURL(c *gin.Context) {
	req, err := binding.ParseForm[presentationdto.UpdateURLReq](c)
	if err != nil {
		response.ParamError(c)
		return
	}
	res, err := h.svc.UpdateURL(c.Request.Context(), req)
	if err != nil {
		response.ErrorAuto(c, http.StatusBadRequest, "presentation", err)
		return
	}
	response.SuccessWithMessage(c, presentationenums.MsgUpdateURLSuccess, res)
}

func (h *Handle) Get(c *gin.Context) {
	req, ok := binding.Query[presentationdto.GetReq](c)
	if !ok {
		return
	}
	res, err := h.svc.Get(c.Request.Context(), req)
	if err != nil {
		response.ErrorAuto(c, http.StatusNotFound, "presentation", err)
		return
	}
	response.SuccessWithMessage(c, presentationenums.MsgDetailSuccess, res)
}

func (h *Handle) List(c *gin.Context) {
	req, ok := binding.Query[presentationdto.ListReq](c)
	if !ok {
		return
	}
	list, err := h.svc.List(c.Request.Context(), req)
	if err != nil {
		response.ErrorAuto(c, http.StatusBadRequest, "presentation", err)
		return
	}
	response.SuccessWithMessage(c, presentationenums.MsgListSuccess, list)
}

func (h *Handle) Delete(c *gin.Context) {
	req, err := binding.ParseForm[presentationdto.DeleteReq](c)
	if err != nil {
		response.ParamError(c)
		return
	}
	if err := h.svc.Delete(c.Request.Context(), req); err != nil {
		response.ErrorAuto(c, http.StatusBadRequest, "presentation", err)
		return
	}
	response.SuccessWithMessage(c, presentationenums.MsgDeleteSuccess, nil)
}
