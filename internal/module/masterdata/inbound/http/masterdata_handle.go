package masterdatahttp

import (
	"net/http"

	"github.com/gin-gonic/gin"

	masterdatacontract "go_wp/internal/module/masterdata/contract"
	masterdatadto "go_wp/internal/module/masterdata/dto"
	masterdataenums "go_wp/internal/module/masterdata/enums"
	"go_wp/pkg/binding"
	"go_wp/pkg/response"
)

type Handle struct {
	svc masterdatacontract.MasterDataService
}

func NewHandle(svc masterdatacontract.MasterDataService) *Handle { return &Handle{svc: svc} }

func (h *Handle) ListChanges(c *gin.Context) {
	req, ok := binding.Query[masterdatadto.ListChangeReq](c)
	if !ok {
		return
	}
	list, err := h.svc.ListChanges(c.Request.Context(), req)
	if err != nil {
		response.ErrorAuto(c, http.StatusBadRequest, "masterdata", err)
		return
	}
	response.SuccessWithMessage(c, masterdataenums.MsgListSuccess, list)
}

func (h *Handle) CountChanges(c *gin.Context) {
	req, ok := binding.Query[masterdatadto.ListChangeReq](c)
	if !ok {
		return
	}
	total, err := h.svc.CountChanges(c.Request.Context(), req)
	if err != nil {
		response.ErrorAuto(c, http.StatusBadRequest, "masterdata", err)
		return
	}
	response.SuccessWithMessage(c, masterdataenums.MsgListSuccess, gin.H{"total": total})
}

func (h *Handle) ListEntities(c *gin.Context) {
	req, ok := binding.Query[masterdatadto.ListEntityReq](c)
	if !ok {
		return
	}
	list, err := h.svc.ListEntities(c.Request.Context(), req)
	if err != nil {
		response.ErrorAuto(c, http.StatusBadRequest, "masterdata", err)
		return
	}
	response.SuccessWithMessage(c, masterdataenums.MsgListSuccess, list)
}

func (h *Handle) EntityTimeline(c *gin.Context) {
	req, ok := binding.Query[masterdatadto.EntityTimelineReq](c)
	if !ok {
		return
	}
	res, err := h.svc.EntityTimeline(c.Request.Context(), req)
	if err != nil {
		response.ErrorAuto(c, http.StatusBadRequest, "masterdata", err)
		return
	}
	response.SuccessWithMessage(c, masterdataenums.MsgListSuccess, res)
}
