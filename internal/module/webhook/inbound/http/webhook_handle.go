package webhookhttp

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"go_wp/internal/module/webhook/contract"
	"go_wp/internal/module/webhook/dto"
	"go_wp/internal/module/webhook/enums"
	"go_wp/pkg/i18n"
	"go_wp/pkg/response"
)

// Handle webhook 模块 HTTP 处理器。
type Handle struct {
	svc webhookcontract.EndpointService
}

// NewHandle 构造。
func NewHandle(svc webhookcontract.EndpointService) *Handle { return &Handle{svc: svc} }

// EndpointList 端点列表（eventType 为空即全部，含已停用的）。
func (h *Handle) EndpointList(c *gin.Context) {
	list, err := h.svc.ListEndpoints(c.Request.Context(), c.Query("eventType"))
	if err != nil {
		response.ErrorAuto(c, http.StatusBadRequest, "webhook", err)
		return
	}
	response.Success(c, list)
}

// EndpointSave 新建 / 更新端点（id 为 0 即新建）。
//
// 密钥只在这一刻以明文进来，service 立即加密落库；此后再没有任何接口能读回它。
func (h *Handle) EndpointSave(c *gin.Context) {
	var req webhookdto.SaveEndpointReq
	if err := c.ShouldBind(&req); err != nil {
		response.ErrorWithMessage(c, http.StatusBadRequest, webhookenums.ErrInvalidParam)
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

// EndpointDelete 删除端点（投递日志随之级联删除）。
func (h *Handle) EndpointDelete(c *gin.Context) {
	id := parseID(c.Query("id"))
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

// EndpointStatus 启停端点。
func (h *Handle) EndpointStatus(c *gin.Context) {
	var req webhookdto.SetEndpointStatusReq
	if err := c.ShouldBind(&req); err != nil || req.ID == 0 {
		response.ErrorWithMessage(c, http.StatusBadRequest, webhookenums.ErrInvalidParam)
		return
	}
	if err := h.svc.SetEndpointStatus(c.Request.Context(), req.ID, req.Status); err != nil {
		response.ErrorAuto(c, http.StatusBadRequest, "webhook", err)
		return
	}
	response.SuccessWithMessage(c, webhookenums.MsgStatusSuccess, nil)
}

// DeliveryList 投递日志（排障）。
func (h *Handle) DeliveryList(c *gin.Context) {
	var req webhookdto.DeliveryListReq
	// GET 用 ShouldBindQuery：ShouldBind 对 GET 走 form 绑定，query 参数会被漏掉。
	if err := c.ShouldBindQuery(&req); err != nil {
		response.ErrorWithMessage(c, http.StatusBadRequest, webhookenums.ErrInvalidParam)
		return
	}
	res, err := h.svc.ListDeliveries(c.Request.Context(), &req)
	if err != nil {
		response.ErrorAuto(c, http.StatusBadRequest, "webhook", err)
		return
	}
	// last_error 落库时是「key + 参数」编码（见 enums/webhook_delivery_err.go），出口按语言还原。
	webhookDeliveryTexts(response.RequestLanguage(c), res.Items)
	response.Success(c, res)
}

// DeliveryRetry 重投一次失败的投递。
func (h *Handle) DeliveryRetry(c *gin.Context) {
	id := parseID(c.Query("id"))
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

// parseID 解析查询里的十进制 id，非法或为零一律返回 0（由调用方判定为参数错误）。
func parseID(raw string) uint64 {
	id, err := strconv.ParseUint(raw, 10, 64)
	if err != nil {
		return 0
	}
	return id
}

// webhookDeliveryTexts 把投递日志里的 last_error 编码按请求语言还原（原地改写）。
//
// 历史行不受影响：FormatDeliveryErr 对中文原文 / 出站客户端原文原样返回。
func webhookDeliveryTexts(lang string, items []*webhookdto.DeliveryItem) {
	tr := i18n.TranslateFunc(lang)
	for _, it := range items {
		if it == nil {
			continue
		}
		it.LastError = webhookenums.FormatDeliveryErr(tr, it.LastError)
	}
}
