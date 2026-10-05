package inventorymcp

// inventory_tools.go — 库存模块的只读工具。
//
// 为什么补这两个：用户问「VIT-C 还有多少货」「哪个仓快没了」「最近出库了多少」
// 时，答案全在 inventory_stocks / inventory_stock_movements 里，而在此之前
// AI 侧看不到任何库存数据 —— 商品的 product_find 只给一个「不跟踪 / 合计 / 未入库」
// 的三态摘要，具体到哪个仓、多少件是答不出来的。
//
// 只读：依赖收窄到 StockReader（两个方法），手里没有 AdjustStock ——
// 「AI 顺手调一次库存」不会在某次改动里悄悄变得可能，而库存调整会直接改可卖量。
//
// 两个工具而不是一个：库存「现在有多少」与流水「什么时候动的」是两种问题，
// 而流水表随时间线性增长（一次拉太多会挤掉回答），该分开限制条数。

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"go_wp/internal/mcp"
	inventorycontract "go_wp/internal/module/inventory/contract"
	inventorydto "go_wp/internal/module/inventory/dto"
	"go_wp/internal/permission"
)

const (
	stockDefaultLimit = 15
	stockMaxLimit     = 50
)

// Tools 返回库存模块的只读工具集。
func Tools(reader inventorycontract.StockReader) ([]mcp.Tool, error) {
	if reader == nil {
		return nil, errors.New("inventorymcp: 库存查询依赖缺失（装配期接线错误）")
	}
	return []mcp.Tool{stockFind(reader), warehouseList(reader)}, nil
}

// —— 库存 ——

type stockFindArgs struct {
	ProjectID   string `json:"projectId"`
	WarehouseID string `json:"warehouseId"`
	ProductID   string `json:"productId"`
	SKUCode     string `json:"skuCode"`
	ExternalSKU string `json:"externalSku"`
	Limit       int    `json:"limit"`
}

func stockFind(reader inventorycontract.StockReader) mcp.Tool {
	return mcp.New("stock_find", "查库存",
		"按线索查库存，用于回答「VIT-C 还有多少货」「这个商品在哪个仓有货」「哪个仓快没了」。\n"+
			"一行 = 一个仓库里一个 SKU 的存量；线索可以给 SKU 编码、商品 id、仓库 id 或外部编码，任一条都能单独用。\n"+
			"**必须看 trackQuantity**：它为 false 时这个 SKU 不跟踪数量（卖多少都不会少），"+
			"此时 quantity 的 0 表示「无限」而不是「没货」—— 说成缺货会让人去做一次没必要的补货。\n"+
			"要仓库清单用 warehouse_list。要「什么时候动的」用 stock_movements（本工具只有当前存量）。",
		permission.InventoryStockList,
		mcp.Object("查库存参数", map[string]mcp.Schema{
			"projectId":   mcp.String("站点工程 id（uuid）"),
			"warehouseId": mcp.String("限定某个仓库（可选）"),
			"productId":   mcp.String("限定某个商品（可选）"),
			"skuCode":     mcp.String("SKU 编码（可选）"),
			"externalSku": mcp.String("仓库侧的外部 / 第三方编码（可选）"),
			"limit":       mcp.Integer("最多返回几行，默认 15，上限 50"),
		}, "projectId"),
		func(ctx context.Context, args stockFindArgs) (mcp.Result, error) {
			if strings.TrimSpace(args.ProjectID) == "" {
				return mcp.Result{}, &mcp.ArgsError{Msg: "projectId 必填；先用 site_projects 查站点工程 id"}
			}
			list, err := reader.ListStocks(ctx, &inventorydto.ListStockReq{
				ProjectID:   args.ProjectID,
				WarehouseID: args.WarehouseID,
				ProductID:   args.ProductID,
				SKUCode:     args.SKUCode,
				ExternalSKU: args.ExternalSKU,
				Page:        1,
				Size:        clampLimit(args.Limit),
			})
			if err != nil {
				return mcp.Result{}, err
			}
			return mcp.Result{Text: stockListText(list, args), Data: list}, nil
		})
}

func stockListText(list []*inventorydto.StockResp, args stockFindArgs) string {
	scope := describeStockScope(args)
	if len(list) == 0 {
		// 「没有库存行」与「这个 SKU 不存在」是两件事，但对用户的下一步动作是同一句：
		// 去商品 / 仓库那边确认。所以这里把替代解释一并说清，而不是只回一个空。
		return fmt.Sprintf("没有查到库存行（%s）。可能是这个 SKU 还没有入库记录，"+
			"也可能是筛选条件写得太窄 —— 用 product_find 或 warehouse_list 确认线索是否对得上。", scope)
	}
	var b strings.Builder
	fmt.Fprintf(&b, "查到 %d 行库存（%s）：\n", len(list), scope)
	for i, s := range list {
		fmt.Fprintf(&b, "%d. %s · %s · %s%s\n",
			i+1, emptyAsDash(s.SKUCode), warehouseLabel(s), quantityText(s), externalSkuSuffix(s))
		// 这一行必须给出 **variantId**：它是下一个动作的钥匙，而且只能从库存行上拿到。
		// 实测踩到过 —— 模型用 product_find 找到商品、用 stock_find 找到库存行，
		// 但正文里只有 SKU 没有变体 id，于是它只能停下来说「你给我一下变体 id」。
		// 「工具的输出要让下一步动作有可能完成」在这里就是这一行。
		ids := "      变体 " + emptyAsDash(s.VariantID)
		if s.ProductID != "" {
			ids += " · 商品 " + s.ProductID
		}
		b.WriteString(ids + "\n")
	}
	return strings.TrimRight(b.String(), "\n")
}

func describeStockScope(args stockFindArgs) string {
	parts := make([]string, 0, 4)
	if args.WarehouseID != "" {
		parts = append(parts, "仓库 "+args.WarehouseID)
	}
	if args.ProductID != "" {
		parts = append(parts, "商品 "+args.ProductID)
	}
	if args.SKUCode != "" {
		parts = append(parts, "SKU "+args.SKUCode)
	}
	if args.ExternalSKU != "" {
		parts = append(parts, "外部编码 "+args.ExternalSKU)
	}
	if len(parts) == 0 {
		return "全部仓库"
	}
	return strings.Join(parts, "、")
}

func warehouseLabel(s *inventorydto.StockResp) string {
	name := firstNonEmpty(s.WarehouseName, s.WarehouseCode, s.WarehouseID)
	return name
}

// quantityText 存量 + 是否跟踪。
//
// 这两个必须一起说：quantity = 0 有两义（跟踪且卖光 / 不跟踪 = 无限），
// 只读数量会把「无限」在回答里写成「没货」。
func quantityText(s *inventorydto.StockResp) string {
	if !s.TrackQuantity {
		return "不跟踪数量（可无限售出）"
	}
	return fmt.Sprintf("%d 件", s.Quantity)
}

func externalSkuSuffix(s *inventorydto.StockResp) string {
	if strings.TrimSpace(s.ExternalSKU) == "" {
		return ""
	}
	return " · 外部编码 " + s.ExternalSKU
}

// —— 仓库 ——

type warehouseListArgs struct {
	ProjectID string `json:"projectId"`
}

func warehouseList(reader inventorycontract.StockReader) mcp.Tool {
	return mcp.New("warehouse_list", "仓库清单",
		"列出站点工程的仓库，用于回答「有几个仓」「默认仓是哪个」「有第三方仓吗」。\n"+
			"库存工具（stock_find）的结果里带仓库名，但用户会直接问仓库本身 —— "+
			"而且一个仓库还没建任何库存时，只有这个工具答得出来。",
		permission.InventoryWarehouseList,
		mcp.Object("列仓库参数", map[string]mcp.Schema{
			"projectId": mcp.String("站点工程 id（uuid）"),
		}, "projectId"),
		func(ctx context.Context, args warehouseListArgs) (mcp.Result, error) {
			if strings.TrimSpace(args.ProjectID) == "" {
				return mcp.Result{}, &mcp.ArgsError{Msg: "projectId 必填；先用 site_projects 查站点工程 id"}
			}
			list, err := reader.ListWarehouses(ctx, &inventorydto.ListWarehouseReq{ProjectID: args.ProjectID})
			if err != nil {
				return mcp.Result{}, err
			}
			return mcp.Result{Text: warehouseListText(list), Data: list}, nil
		})
}

func warehouseListText(list []*inventorydto.WarehouseResp) string {
	if len(list) == 0 {
		return "这个工程还没有建仓库。"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "共 %d 个仓库：\n", len(list))
	for i, w := range list {
		fmt.Fprintf(&b, "%d. id=%s %s（编码 %s，类型 %s，状态 %s）%s\n",
			i+1, w.ID, emptyAsDash(w.Name), emptyAsDash(w.Code),
			emptyAsDash(w.Type), emptyAsDash(w.Status), defaultSuffix(w.IsDefault))
	}
	return strings.TrimRight(b.String(), "\n")
}

func defaultSuffix(isDefault bool) string {
	if isDefault {
		return " —— **默认仓**"
	}
	return ""
}

// —— 共用 ——

func clampLimit(n int) int {
	if n <= 0 {
		return stockDefaultLimit
	}
	if n > stockMaxLimit {
		return stockMaxLimit
	}
	return n
}

func emptyAsDash(s string) string {
	if strings.TrimSpace(s) == "" {
		return "-"
	}
	return s
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return "-"
}
