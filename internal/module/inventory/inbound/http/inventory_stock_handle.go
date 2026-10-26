// 库存写动作：手工调整、第三方 SKU、批次追踪开关。
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

// InventoryStockChange 后台「库存调整（盘点 / 报损）」。
//
// 方向只允许两类：adjust（盘点：填的是目标绝对量）与 out（报损：填的是本次减少量）。
// 入库（in）与销售出库不在本页 —— 它们必须挂在采购单 / 发货 / 退货单上，
// 由那些单据驱动同一套变动契约（见文件头）。
//
// 原因与备注**都必填**：调整是「没有单据承载」的写入口，没有原因与备注的调整
// 在事后无法解释，等于把账目变成不可审计的。
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

// InventoryExternalSKUUpdate 库存页行内编辑：登记 / 修改 / 清空某 (仓库, 变体) 行的外部编码。
//
// 为什么需要它：迁移 251 给 inventory_stocks 加了 external_sku，但库存页原先只有只读列 ——
// 早于 251 建的老商品事后没有地方登记外码。本入口就是那个「事后登记」。
//
// 三条口径全部来自 service（本 handler 只透传，不重复实现）：
//
//	· 空串是**合法值**：清空 = 该仓改回用我们自己的 SKU（撤销映射），
//	  不是「保存失败」，也不动 sku_code / quantity / cost_price；
//	· N:1 弱校验：同一仓同一外码必须属于同一个商品（多口味共用合法，跨商品报错）；
//	· 写入只动 external_sku 一列（model.SetExternalSKUByVariantWarehouse）。
//
// 权限：复用库存页既有写入口的权限点（inventory:stock_change，见 inventory_page_router.go），
// 不新增权限点、不新增 authorizedAPI 路由。
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

// InventoryStockTrackingUpdate 库存页行内编辑：切换某 (仓库, 变体) 行的跟踪开关并写入数量。
//
// 为什么需要它：迁移 261 给 inventory_stocks 加了 track_quantity，而库存页原先只有
// 一个只读的数量列 —— 运营没有任何地方能把「这条货是无限的」改成「按数量跟踪」，
// 也没有地方把卖光的行改回无限。本入口就是那个开关。
//
// 三条口径全部来自 service（本 handler 只透传，不重复实现）：
//
//	· 数量走变动契约（手工调整）：有变动必有流水，行内编辑不是例外；
//	· 不跟踪（无限）的行不接受非 0 数量（ErrStockUntrackedQuantity）；表单在无限态
//	  把数量框禁用并留空，**绝不预填 0** —— 0 是「卖光」这个具体事实，要写就得自己打；
//	· 跟踪态必须显式给数量：留空不等于 0（否则「忘了填」会被静默记成「没货」）。
//
// 权限：复用库存页既有写入口的权限点（inventory:stock_change，见 inventory_page_router.go），
// 不新增权限点、不新增 authorizedAPI 路由。
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

	// 跟踪态必须显式给数量：无限态的数量框是 disabled 的（浏览器不提交该字段），
	// 所以「空」在这里有两种来源，只有跟踪态下的空是错误。
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

// variantOptions 某工程全部商品的变体下拉项（库存页「按 SKU 看库存」的入口）。
//
// 一次批量的读法：商品清单与变体各查一次，查询次数与商品数无关
// （原先是「List 一页 + 逐个 Get」，100 个商品 = 101 次往返）。
// 商品契约未注入（装配漏接）时下拉为空、页面照常渲染 —— 不因一处装配缺失 500。
func (h *inventoryPageHandle) variantOptions(ctx context.Context, projectID string) (out []inventorydto.VariantOptionResp, err error) {
	out = []inventorydto.VariantOptionResp{}
	if projectID == "" || h.products == nil {
		return out, nil
	}
	options, err := h.products.ListAllVariantOptions(ctx, projectID)
	if err != nil {
		return nil, err
	}
	for _, o := range options {
		out = append(out, inventorydto.VariantOptionResp{
			VariantID: o.VariantID, ProductID: o.ProductID, SKUCode: o.SKUCode,
			Label: o.ProductName + " · " + o.SKUCode,
		})
	}
	return out, nil
}

// skuOfVariant 从下拉项里按变体 id 取 SKU 编码（找不到返回空串，页面提示无匹配）。
func skuOfVariant(options []inventorydto.VariantOptionResp, variantID string) string {
	for _, o := range options {
		if o.VariantID == variantID {
			return o.SKUCode
		}
	}
	return ""
}
