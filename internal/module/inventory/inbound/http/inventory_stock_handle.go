package inventoryhttp

import (
	"context"
	"go_wp/internal/middleware/builtin"
	"go_wp/pkg/binding"
	"go_wp/pkg/response"

	"github.com/gin-gonic/gin"

	inventorydto "go_wp/internal/module/inventory/dto"
	inventoryenums "go_wp/internal/module/inventory/enums"
)

func (h *inventoryPageHandle) InventoryStockChange(c *gin.Context) {
	form, ok := binding.Form[inventorydto.StockChangeFormReq](c)
	if !ok {
		return
	}

	respond := func(err error) {
		if err != nil {
			inventoryFail(c, err)
			return
		}
		response.Success(c, nil)
	}

	if !form.ValidDirection() {
		respond(inventoryenums.Err(inventoryenums.ErrStockDirectionInvalid))
		return
	}
	if form.RemarkValue() == "" {
		respond(inventoryenums.Err(inventoryenums.ErrStockRemarkRequired))
		return
	}
	if _, err := h.inventory.ChangeStock(c.Request.Context(), form.ChangeReq(builtin.GetUsername(c))); err != nil {
		respond(err)
		return
	}
	respond(nil)
}

func (h *inventoryPageHandle) InventoryExternalSKUUpdate(c *gin.Context) {
	form, ok := binding.Form[inventorydto.ExternalSKUFormReq](c)
	if !ok {
		return
	}
	if _, err := h.inventory.BindExternalSKU(c.Request.Context(), form.BindReq()); err != nil {
		inventoryFail(c, err)
		return
	}
	response.Success(c, nil)
}

func (h *inventoryPageHandle) InventoryStockTrackingUpdate(c *gin.Context) {
	form, ok := binding.Form[inventorydto.StockTrackingFormReq](c)
	if !ok {
		return
	}

	respond := func(err error) {
		if err != nil {
			inventoryFail(c, err)
			return
		}
		response.Success(c, nil)
	}

	if form.Tracked() && form.QuantityValue() == "" {
		respond(inventoryenums.Err(inventoryenums.ErrStockQuantityRequired))
		return
	}
	if _, err := h.inventory.UpdateStockTracking(c.Request.Context(), form.TrackingReq()); err != nil {
		respond(err)
		return
	}
	respond(nil)
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
