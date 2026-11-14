package inventoryhttp

import (
	"context"
	"go_wp/internal/middleware/builtin"
	"go_wp/pkg/binding"
	pageview "go_wp/pkg/view"
	"net/http"

	"github.com/gin-gonic/gin"

	inventorydto "go_wp/internal/module/inventory/dto"
)

func (h *inventoryPageHandle) InventoryStockChange(c *gin.Context) {
	form, err := binding.ParseForm[inventorydto.StockChangeFormReq](c)
	if err != nil {
		pageview.FailStatus(c, http.StatusBadRequest, inventoryErrScene, err)
		return
	}

	stockRespond(c, func() error {
		if err := form.Validate(); err != nil {
			return err
		}
		_, err := h.inventory.ChangeStock(c.Request.Context(), form.ChangeReq(builtin.GetUsername(c)))
		return err
	})
}

func (h *inventoryPageHandle) InventoryExternalSKUUpdate(c *gin.Context) {
	form, err := binding.ParseForm[inventorydto.ExternalSKUFormReq](c)
	if err != nil {
		pageview.FailStatus(c, http.StatusBadRequest, inventoryErrScene, err)
		return
	}

	if _, err := h.inventory.BindExternalSKU(c.Request.Context(), form.BindReq()); err != nil {
		inventoryFail(c, err)
		return
	}
	inventoryWriteOK(c)
}

func (h *inventoryPageHandle) InventoryStockTrackingUpdate(c *gin.Context) {
	form, err := binding.ParseForm[inventorydto.StockTrackingFormReq](c)
	if err != nil {
		pageview.FailStatus(c, http.StatusBadRequest, inventoryErrScene, err)
		return
	}

	stockRespond(c, func() error {
		if err := form.Validate(); err != nil {
			return err
		}
		_, err := h.inventory.UpdateStockTracking(c.Request.Context(), form.TrackingReq())
		return err
	})
}

func (h *inventoryPageHandle) variantOptions(ctx context.Context, projectID string) (out []VariantOptionResp, err error) {
	out = []VariantOptionResp{}
	if projectID == "" || h.products == nil {
		return out, nil
	}
	options, err := h.products.ListAllVariantOptions(ctx, projectID)
	if err != nil {
		return nil, err
	}
	for _, o := range options {
		out = append(out, VariantOptionResp{
			VariantID: o.VariantID, ProductID: o.ProductID, SKUCode: o.SKUCode,
			Label: o.ProductName + " · " + o.SKUCode,
		})
	}
	return out, nil
}

func skuOfVariant(options []VariantOptionResp, variantID string) string {
	for _, o := range options {
		if o.VariantID == variantID {
			return o.SKUCode
		}
	}
	return ""
}

// stockRespond 是库存写端点的公共出口：失败交共享失败出口，成功给统一提示。
func stockRespond(c *gin.Context, run func() error) {
	if err := run(); err != nil {
		inventoryFail(c, err)
		return
	}
	inventoryWriteOK(c)
}
