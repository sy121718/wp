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
	req, ok := binding.JSON[presentationdto.CreateInstanceReq](c)
	if !ok {
		return
	}
	res, err := h.svc.CreateInstance(c.Request.Context(), req)
	if err != nil {
		response.ErrorAuto(c, http.StatusBadRequest, "presentation", err)
		return
	}
	response.SuccessWithMessage(c, presentationenums.MsgCreateSuccess, res)
}

func (h *Handle) Rebuild(c *gin.Context) {
	req, ok := binding.JSON[presentationdto.RebuildReq](c)
	if !ok {
		return
	}
	res, err := h.svc.Rebuild(c.Request.Context(), req)
	if err != nil {
		response.ErrorAuto(c, http.StatusBadRequest, "presentation", err)
		return
	}
	response.SuccessWithMessage(c, presentationenums.MsgRebuildSuccess, res)
}

func (h *Handle) GetByEntity(c *gin.Context) {
	req, ok := binding.Form[presentationdto.GetByEntityReq](c)
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
	req, ok := binding.Form[presentationdto.PreviewInstanceReq](c)
	if !ok {
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
	req, ok := binding.Form[presentationdto.UpdateURLReq](c)
	if !ok {
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
	req, ok := binding.Form[presentationdto.GetReq](c)
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
	req, ok := binding.Form[presentationdto.ListReq](c)
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
	req, ok := binding.Form[presentationdto.DeleteReq](c)
	if !ok {
		return
	}
	if err := h.svc.Delete(c.Request.Context(), req); err != nil {
		response.ErrorAuto(c, http.StatusBadRequest, "presentation", err)
		return
	}
	response.SuccessWithMessage(c, presentationenums.MsgDeleteSuccess, nil)
}
