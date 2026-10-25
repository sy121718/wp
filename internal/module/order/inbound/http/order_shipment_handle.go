// 发货与承运商的 API 出口（迁移 624 / 625，S2）。
//
// 四个出口 + 三个承运商入口，全部是「绑定 → 调用例 → JSON」：
// 业务判定（状态、超量、缺仓、模板校验）都在 service，控制器不重复判一遍。
package orderhttp

import (
	"net/http"

	"github.com/gin-gonic/gin"

	orderdto "go_wp/internal/module/order/dto"
	"go_wp/pkg/binding"
	"go_wp/pkg/response"
)

// 失败出口**不新开一个本模块的 helper**：那会被 scripts/check-shell-migration.sh 判成
// 「自造失败出口」（它的判据是 inbound/http 里 ^func …Fail… 的个数），而出口的终局归属
// 是 pkg/response。每个方法里直接调 response.ErrorAuto（与 order_handle.go 逐字同一句）。

// ShipOrder 发货（POST /api/order/shipment/create）。
//
// 请求体带 requestId 时是**幂等**的：同一 (工程, request_id) 只会产生一张发货单，
// 重复提交直接返回那张单（不改库存、不改状态）—— 双击 / 重试不会发出两批货。
func (h *Handle) ShipOrder(c *gin.Context) {
	req, ok := binding.JSON[orderdto.ShipOrderReq](c)
	if !ok {
		return
	}
	// 审计字段**无条件覆盖**（不看客户端传了什么）：能被伪造的审计等于没有审计。
	// 与「只在非零时覆盖」的差别在会话缺失时 —— 那种情况下客户端传的 id 会原样落库。
	{
		id, name := operatorFromContext(c)
		req.OperatorID, req.OperatorName = int64(id), name
	}
	res, err := h.svc.ShipOrder(c.Request.Context(), req)
	if err != nil {
		response.ErrorAuto(c, http.StatusBadRequest, "order", err)
		return
	}
	response.Success(c, res)
}

// ListShipments 某订单的发货记录（GET /api/order/shipment/list?orderId=）。
func (h *Handle) ListShipments(c *gin.Context) {
	req, ok := binding.Query[orderdto.ListShipmentReq](c)
	if !ok {
		return
	}
	list, err := h.svc.ListShipments(c.Request.Context(), req)
	if err != nil {
		response.ErrorAuto(c, http.StatusBadRequest, "order", err)
		return
	}
	response.Success(c, list)
}

// ListPendingShip 待发货列表（GET /api/order/shipment/pending?projectId=）。
//
// 读模型自己算好每个数字（件数 / 已发 / 剩余），列表页只渲染 —— 模板不做减法。
func (h *Handle) ListPendingShip(c *gin.Context) {
	req, ok := binding.Query[orderdto.PendingShipReq](c)
	if !ok {
		return
	}
	rows, err := h.svc.ListPendingShip(c.Request.Context(), req.ProjectID)
	if err != nil {
		response.ErrorAuto(c, http.StatusBadRequest, "order", err)
		return
	}
	response.Success(c, rows)
}

// ShipForm 发货表单数据（GET /api/order/shipment/form?orderId=）。
func (h *Handle) ShipForm(c *gin.Context) {
	req, ok := binding.Query[orderdto.ListShipmentReq](c)
	if !ok {
		return
	}
	form, err := h.svc.BuildShipForm(c.Request.Context(), req)
	if err != nil {
		response.ErrorAuto(c, http.StatusBadRequest, "order", err)
		return
	}
	response.Success(c, form)
}

// ListCarriers 承运商列表（GET /api/order/carrier/list?projectId=&includeDisabled=）。
func (h *Handle) ListCarriers(c *gin.Context) {
	req, ok := binding.Query[orderdto.ListCarrierReq](c)
	if !ok {
		return
	}
	list, err := h.svc.ListCarriers(c.Request.Context(), req)
	if err != nil {
		response.ErrorAuto(c, http.StatusBadRequest, "order", err)
		return
	}
	response.Success(c, list)
}

// CreateCarrier 新建承运商（POST /api/order/carrier/create）。
func (h *Handle) CreateCarrier(c *gin.Context) {
	req, ok := binding.JSON[orderdto.CarrierCreateReq](c)
	if !ok {
		return
	}
	res, err := h.svc.CreateCarrier(c.Request.Context(), req)
	if err != nil {
		response.ErrorAuto(c, http.StatusBadRequest, "order", err)
		return
	}
	response.Success(c, res)
}

// UpdateCarrier 修改承运商（POST /api/order/carrier/update）。
func (h *Handle) UpdateCarrier(c *gin.Context) {
	req, ok := binding.JSON[orderdto.CarrierUpdateReq](c)
	if !ok {
		return
	}
	res, err := h.svc.UpdateCarrier(c.Request.Context(), req)
	if err != nil {
		response.ErrorAuto(c, http.StatusBadRequest, "order", err)
		return
	}
	response.Success(c, res)
}
