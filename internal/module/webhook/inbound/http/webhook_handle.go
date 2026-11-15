package webhookhttp

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	webhookcontract "go_wp/internal/module/webhook/contract"
	webhookdto "go_wp/internal/module/webhook/dto"
	webhookenums "go_wp/internal/module/webhook/enums"
	"go_wp/pkg/binding"
	"go_wp/pkg/facing"
	"go_wp/pkg/response"
)

type Handle struct {
	svc webhookcontract.EndpointService
}

func NewHandle(svc webhookcontract.EndpointService) *Handle { return &Handle{svc: svc} }

func (h *Handle) EndpointList(c *gin.Context) {
	list, err := h.svc.ListEndpoints(c.Request.Context(), c.Query("eventType"))
	if err != nil {
		response.ErrorAuto(c, http.StatusBadRequest, "webhook", err)
		return
	}
	response.Success(c, list)
}

func (h *Handle) EndpointSave(c *gin.Context) {
	var req webhookdto.SaveEndpointReq
	if err := c.ShouldBindJSON(&req); err != nil {
		response.ParamError(c)
		return
	}
	var (
		item *webhookdto.EndpointItem
		err  error
	)
	if req.ID > 0 {
		item, err = h.svc.UpdateEndpoint(c.Request.Context(), &req)
	} else {
		item, err = h.svc.CreateEndpoint(c.Request.Context(), &req)
	}
	if err != nil {
		response.ErrorAuto(c, http.StatusBadRequest, "webhook", err)
		return
	}
	response.SuccessWithMessage(c, webhookenums.MsgSaveSuccess, item)
}

func (h *Handle) EndpointDelete(c *gin.Context) {
	var query webhookdto.EndpointIDQuery
	if err := c.ShouldBindQuery(&query); err != nil {
		response.ParamError(c)
		return
	}
	id := parseID(query.ID)
	if id == 0 {
		response.ErrorWithMessage(c, http.StatusBadRequest, webhookenums.ErrInvalidParam)
		return
	}
	if err := h.svc.DeleteEndpoint(c.Request.Context(), id); err != nil {
		response.ErrorAuto(c, http.StatusBadRequest, "webhook", err)
		return
	}
	response.SuccessWithMessage(c, webhookenums.MsgDeleteSuccess, nil)
}

func (h *Handle) EndpointStatus(c *gin.Context) {
	var req webhookdto.SetEndpointStatusReq
	if err := c.ShouldBindJSON(&req); err != nil {
		response.ParamError(c)
		return
	}
	if req.ID == 0 {
		response.ErrorWithMessage(c, http.StatusBadRequest, webhookenums.ErrInvalidParam)
		return
	}
	if err := h.svc.SetEndpointStatus(c.Request.Context(), req.ID, req.Status); err != nil {
		response.ErrorAuto(c, http.StatusBadRequest, "webhook", err)
		return
	}
	response.SuccessWithMessage(c, webhookenums.MsgStatusSuccess, nil)
}

func (h *Handle) DeliveryList(c *gin.Context) {

	req, err := binding.ParseQuery[webhookdto.DeliveryListReq](c)
	if err != nil {
		response.ParamError(c)
		return
	}
	res, err := h.svc.ListDeliveries(c.Request.Context(), req)
	if err != nil {
		response.ErrorAuto(c, http.StatusBadRequest, "webhook", err)
		return
	}

	webhookDeliveryTexts(c, res.Items)
	response.Success(c, res)
}

func (h *Handle) DeliveryRetry(c *gin.Context) {
	var query webhookdto.EndpointIDQuery
	if err := c.ShouldBindQuery(&query); err != nil {
		response.ParamError(c)
		return
	}
	id := parseID(query.ID)
	if id == 0 {
		response.ErrorWithMessage(c, http.StatusBadRequest, webhookenums.ErrInvalidParam)
		return
	}
	if err := h.svc.RetryDelivery(c.Request.Context(), id); err != nil {
		response.ErrorAuto(c, http.StatusBadRequest, "webhook", err)
		return
	}
	response.SuccessWithMessage(c, webhookenums.MsgRetryQueued, nil)
}

func parseID(raw string) uint64 {
	id, err := strconv.ParseUint(raw, 10, 64)
	if err != nil {
		return 0
	}
	return id
}

// webhookDeliveryTexts 给投递记录的错误说明取词。
//
// 这是 JSON 出口（response.Success 直接把 DeliveryItem 交给调用方），
// 因此取词走 pkg/response 的正式口，不在这里拿 i18n 包自建闭包。
func webhookDeliveryTexts(c *gin.Context, items []*webhookdto.DeliveryItem) {
	tr := func(key, fallback string) string {
		return response.MsgText(c, facing.Msg{Key: key, Fallback: fallback})
	}
	for _, it := range items {
		if it == nil {
			continue
		}
		it.LastError = webhookenums.FormatDeliveryErr(tr, it.LastError)
	}
}
