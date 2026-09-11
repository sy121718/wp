// product_pricing_handle.go — 定价工具 HTTP 入口（issue #13）。
//
// 与标签 / 分类接口同形：JSON 入参、pkg/response 出参、消息取 enums。
// 本层只做绑定、覆盖操作人与转发：规则校验、算价、落库与留痕一律在 service。
package producthttp

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"go_wp/internal/middleware/builtin"
	productdto "go_wp/internal/module/product/dto"
	productenums "go_wp/internal/module/product/enums"
	"go_wp/pkg/response"
)

// ListPricingRuleTypes 内置定价规则类型清单（后台下拉与参数说明的唯一来源）。
func (h *Handle) ListPricingRuleTypes(c *gin.Context) {
	response.SuccessWithMessage(c, productenums.MsgListSuccess, h.svc.ListPricingRuleTypes(c.Request.Context()))
}

// ListPricingRoundingOptions 尾数处理清单。
func (h *Handle) ListPricingRoundingOptions(c *gin.Context) {
	response.SuccessWithMessage(c, productenums.MsgListSuccess, h.svc.ListPricingRoundingOptions(c.Request.Context()))
}

// PreviewPricing 按规则试算（不落库、不留痕）。
func (h *Handle) PreviewPricing(c *gin.Context) {
	req := &productdto.PricingPreviewReq{}
	if err := c.ShouldBindJSON(req); err != nil {
		response.ErrorWithMessage(c, http.StatusBadRequest, productenums.ErrInvalidParam)
		return
	}
	res, err := h.svc.PreviewPricing(c.Request.Context(), req)
	if err != nil {
		response.ErrorWithMessage(c, http.StatusBadRequest, err.Error())
		return
	}
	response.SuccessWithMessage(c, productenums.MsgPricingPreviewSuccess, res)
}

// ApplyPricing 应用调价（算价 → 写回 product_variants.price → 留痕）。
func (h *Handle) ApplyPricing(c *gin.Context) {
	req := &productdto.PricingApplyReq{}
	if err := c.ShouldBindJSON(req); err != nil {
		response.ErrorWithMessage(c, http.StatusBadRequest, productenums.ErrInvalidParam)
		return
	}
	// 操作人取自会话，客户端传什么都不作数（留痕的操作人不可伪造）。
	req.OperatorID = operatorFromContext(c)
	res, err := h.svc.ApplyPricing(c.Request.Context(), req)
	if err != nil {
		response.ErrorWithMessage(c, http.StatusBadRequest, err.Error())
		return
	}
	response.SuccessWithMessage(c, productenums.MsgPricingApplySuccess, res)
}

// ListPriceAdjustments 调价留痕列表（按工程）。
func (h *Handle) ListPriceAdjustments(c *gin.Context) {
	req := &productdto.ListPriceAdjustmentReq{}
	if err := c.ShouldBindQuery(req); err != nil {
		response.ErrorWithMessage(c, http.StatusBadRequest, productenums.ErrInvalidParam)
		return
	}
	list, err := h.svc.ListPriceAdjustments(c.Request.Context(), req)
	if err != nil {
		response.ErrorWithMessage(c, http.StatusBadRequest, err.Error())
		return
	}
	response.SuccessWithMessage(c, productenums.MsgListSuccess, list)
}

// GetPriceAdjustment 单批次留痕详情（含逐变体「原价 → 新价」明细）。
func (h *Handle) GetPriceAdjustment(c *gin.Context) {
	req := &productdto.GetPriceAdjustmentReq{}
	if err := c.ShouldBindQuery(req); err != nil {
		response.ErrorWithMessage(c, http.StatusBadRequest, productenums.ErrInvalidParam)
		return
	}
	res, err := h.svc.GetPriceAdjustment(c.Request.Context(), req)
	if err != nil {
		response.ErrorWithMessage(c, http.StatusNotFound, err.Error())
		return
	}
	response.SuccessWithMessage(c, productenums.MsgDetailSuccess, res)
}

// operatorFromContext 从会话取操作人（issue #13 定价留痕 / issue #19 变更记录共用）。
//
// 优先取登录名：留痕与变更记录都要能直接读懂「谁改的」（与库存流水的 operator_id 同口径，
// 后台页面路径也一直是用登录名）。登录名缺失（脚本 / 测试路径）时退回数值 id ——
// 会话中间件写入的 user_id 是 int64，这里只做展示用的文本化，不参与任何鉴权判断。
// 两者都没有时返回空串：留痕字段允许为空。
func operatorFromContext(c *gin.Context) (id string) {
	if name := strings.TrimSpace(builtin.GetUsername(c)); name != "" {
		return name
	}
	value, exists := c.Get("user_id")
	if !exists {
		return ""
	}
	switch v := value.(type) {
	case int64:
		return strconv.FormatInt(v, 10)
	case int:
		return strconv.Itoa(v)
	case string:
		return v
	case uint64:
		return strconv.FormatUint(v, 10)
	}
	return ""
}
