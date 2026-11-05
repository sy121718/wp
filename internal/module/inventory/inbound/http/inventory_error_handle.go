package inventoryhttp

import (
	"errors"
	"net/http"

	"go_wp/internal/text"
	"go_wp/pkg/facing"
	"go_wp/pkg/i18n"
	"go_wp/pkg/logger"
	"go_wp/pkg/response"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	inventorydto "go_wp/internal/module/inventory/dto"
	inventoryenums "go_wp/internal/module/inventory/enums"
	projectcontract "go_wp/internal/module/project/contract"
	bulkids "go_wp/pkg/bulkids"
)

const inventoryErrScene = "inventory"

func inventoryErrMsg(c *gin.Context, err error) facing.Msg {
	return inventoryErrMsgOf(c, text.FactOf(c, inventoryErrScene, err))
}

func inventoryErrMsgOf(c *gin.Context, f facing.Fact) facing.Msg {
	if f.Key == "" {
		return facing.Msg{}
	}
	if !f.Business {
		return response.InternalMsg()
	}
	if f.Tail != "" {
		if detail := inventoryErrDetailText(text.TranslateFunc(c), f.Tail); detail != "" {

			return facing.Msg{Fallback: response.ArgsText(c, f.Key, inventoryErrFallback(f.Key), nil) + "：" + detail}
		}
	}
	return facing.Msg{Key: f.Key, Fallback: inventoryErrFallback(f.Key)}
}

func inventoryErrText(c *gin.Context, err error) string {
	return inventoryMsgText(c, inventoryErrMsg(c, err))
}

func inventoryErrDetailText(tr func(key, fallback string) string, tail string) string {
	parts, ok := i18n.ParseErrorDetails(tail)
	if !ok {
		return tail
	}
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		fallback, registered := inventoryenums.ErrDetailFallbacks[p.Key]
		if !registered || fallback == "" {
			logger.Scene(inventoryErrScene).With("detail_key", p.Key).
				Warn("业务错误的补充说明词条未登记，已省略该段")
			continue
		}
		out = append(out, i18n.FillTranslate(tr, p.Key, fallback, p.Args))
	}
	return strings.Join(out, "；")
}

const (
	inventoryActionDoneKey      = inventoryenums.InventoryActionDone
	inventoryActionDoneFallback = "操作已完成"
)

var (
	inventoryBulkWarehousePartial   = facing.Msg{Key: inventoryenums.InventoryBulkWarehousePartial, Fallback: "已删除 %s 个，%s 个未能删除（默认仓或仓内仍有非零库存）"}
	inventoryBulkWarehouseDone      = facing.Msg{Key: inventoryenums.InventoryBulkWarehouseDone, Fallback: "已删除 %s 个仓库"}
	inventoryBulkReasonPartial      = facing.Msg{Key: inventoryenums.InventoryBulkReasonPartial, Fallback: "已更新 %s 个原因，%s 个未能更新"}
	inventoryBulkReasonDone         = facing.Msg{Key: inventoryenums.InventoryBulkReasonDone, Fallback: "已更新 %s 个原因"}
	inventoryBulkReasonNoneSelected = facing.Msg{Key: inventoryenums.InventoryBulkReasonNoneSelected, Fallback: "请选择要操作的原因"}
)

func inventoryBulkIDsMsg(c *gin.Context, err error) facing.Msg {
	var tooMany *bulkids.IDsError
	if !errors.As(err, &tooMany) {

		if err != nil {
			logger.Scene(inventoryErrScene).
				With("path", c.Request.URL.Path).
				Error(err, "批量 id 读取失败：未命中受控错误，只对外给归口文案")
		}
		return response.InternalMsg()
	}
	logger.Scene(inventoryErrScene).
		With("path", c.Request.URL.Path).
		With("count", tooMany.Count).
		With("max", tooMany.Max).
		Warn("批量操作被拒：提交的 id 数量超过上限")
	return facing.Msg{Key: bulkids.MsgTooMany, Fallback: bulkids.TooManyTemplate}.
		WithArgs(strconv.Itoa(tooMany.Max), strconv.Itoa(tooMany.Count))
}

func inventoryErrFallback(key string) string { return key }

type inventoryPageMeta struct {
	titleKey      string
	titleFallback string
	menu          string
}

var (
	inventoryMetaHome = inventoryPageMeta{
		titleKey: inventoryenums.InventoryTitle, titleFallback: "库存管理",
		menu: "inventory",
	}
	inventoryMetaWarehouses = inventoryPageMeta{
		titleKey: inventoryenums.InventoryWarehousesTitle, titleFallback: "仓库管理",
		menu: "inventory-warehouses",
	}
	inventoryMetaTransfers = inventoryPageMeta{
		titleKey: inventoryenums.InventoryTransfersTitle, titleFallback: "调拨单",
		menu: "inventory-transfers",
	}
	inventoryMetaReasons = inventoryPageMeta{
		titleKey: inventoryenums.InventoryReasonsTitle, titleFallback: "变动原因字典",
		menu: "inventory-reasons",
	}
	inventoryMetaMovements = inventoryPageMeta{
		titleKey: inventoryenums.InventoryMovementsTitle, titleFallback: "库存流水",
		menu: "inventory-movements",
	}
	inventoryMetaStockSKUs = inventoryPageMeta{
		titleKey: inventoryenums.InventoryStockSKUsTitle, titleFallback: "仓库商品 SKU",
		menu: "inventory-skus",
	}
)

type inventoryLoadFailedView gin.H

func (inventoryLoadFailedView) PageStatus() int   { return http.StatusInternalServerError }
func (inventoryLoadFailedView) PageNoStore() bool { return true }

func inventoryPageErrorData(c *gin.Context, meta inventoryPageMeta, err error) inventoryLoadFailedView {
	return inventoryLoadFailedView{
		"title": inventoryMsgText(c, facing.Msg{Key: meta.titleKey, Fallback: meta.titleFallback}),
		"menu":  meta.menu,

		"Projects":        []projectcontract.ProjectResp{},
		"SelectedProject": "",

		"SelectedVariant":     "",
		"SelectedSKU":         "",
		"Warehouses":          []WarehouseRowResp{},
		"WarehouseTypes":      []gin.H{},
		"WarehouseCreateForm": gin.H{},
		"VariantOptions":      []gin.H{},
		"StockRows":           []StockByWarehouseRowResp{},
		"Reasons":             []ReasonRowResp{},
		"AdjustReasons":       []ReasonRowResp{},
		"AdjustDirections":    []OptionResp{},
		"Directions":          []OptionResp{},
		"Movements":           []MovementRowResp{},
		"ReasonCreateForm":    gin.H{},

		"Rows":            []inventorydto.StockSKURowResp{},
		"FilterKeyword":   "",
		"FilterStatus":    "",
		"StatusOptions":   []OptionResp{},
		"FilterWarehouse": "",
		"FilterDirection": "",
		"FilterReason":    "",
		"FilterTimeFrom":  "",
		"FilterTimeTo":    "",

		"ListQuery": "",

		"LoadFailed": true,
		"LoadError":  inventoryErrText(c, err),
	}
}
