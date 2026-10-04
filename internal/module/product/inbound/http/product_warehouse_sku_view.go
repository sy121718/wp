// product_page_warehouse_sku.go — 新建商品抽屉「从仓库选」的候选数据（docs/14 §1.1 / §9.3，迁移 251）。
//
// 为什么单独成文件：这一段是「仓库侧只回答这条货在这个仓叫什么」在商品页的唯一落点 ——
// 候选按仓分组、每仓有上限、未接库存契约时整块降级为空。与商品页其余部分（列表 / 详情 /
// 抽屉业务字段）没有耦合，放在一起只会让两边互相牵制。
//
// 前端只是便捷入口：ProductsCreate 会带着仓库 id 让 service 在**该仓**重新复核一遍。
package producthttp

import (
	"context"
	"strings"

	"github.com/gin-gonic/gin"

	inventorycontract "go_wp/internal/module/inventory/contract"
	productservice "go_wp/internal/module/product/service"
)

// warehouseSKUOptionSize 抽屉里每个仓最多列多少条货。
//
// 抽屉是**快捷入口**：选不到的去库存页核对（那里有分页与关键字）。把整张库存表拉进内存，
// 在仓库多 / 货多时是纯粹的浪费，而这里只需要「最近常用的一屏」。
const warehouseSKUOptionSize = 100

// warehouseSKUOptions 新建商品抽屉「从仓库选」的候选：(仓库 → 该仓的货) 分组（迁移 251）。
//
// 逐仓取：一次取全库再在内存分组会把整张表拉进内存；每仓上限见 warehouseSKUOptionSize。
// 未注入库存契约（装配缺陷 / 直接渲染模板的单测）时返回空 —— 抽屉照常渲染，
// 只是没有「从仓库选」这条入口（接口路径传来的 skuSource=warehouse 仍由 service 校验）。
func (h *productPageHandle) warehouseSKUOptions(ctx context.Context, projectID string, warehouses []gin.H) (out []gin.H, err error) {
	out = []gin.H{}
	if h.inventories == nil || strings.TrimSpace(projectID) == "" {
		return out, nil
	}
	for _, w := range warehouses {
		id, _ := w["ID"].(string)
		if strings.TrimSpace(id) == "" {
			continue
		}
		rows, lerr := h.inventories.ListWarehouseSKUs(ctx, &inventorycontract.ListWarehouseSKUReq{
			ProjectID: projectID, WarehouseID: id, Size: warehouseSKUOptionSize,
		})
		if lerr != nil {
			return nil, lerr
		}
		if len(rows) == 0 {
			continue
		}
		items := make([]gin.H, 0, len(rows))
		for _, r := range rows {
			// 候选一律取**裸码**（仓库侧的名字，不带仓码前缀）：前端把它填进「SKU 编码」框时
			// 服务端按 (仓, 裸码) 复核并**复用**那一行 —— 带上前缀会被当成一条新货去建行。
			// 历史行可能还带着前缀（迁移 262 只订正过一次存量），这里按同一条规则剥一次（幂等）。
			code := productservice.StripWarehousePrefix(r.SKUCode, r.WarehouseCode)
			// 候选显示「我们的 SKU」，登记的对方编码跟在后面 —— 运营手上可能是其中任意一个。
			label := code
			if r.ExternalSKU != "" && r.ExternalSKU != code {
				label += " · " + r.ExternalSKU
			}
			items = append(items, gin.H{
				"SKUCode": code, "ExternalSKU": r.ExternalSKU, "Label": label,
			})
		}
		label, _ := w["Label"].(string)
		isDefault, _ := w["IsDefault"].(bool)
		out = append(out, gin.H{
			"WarehouseID": id, "Label": label, "IsDefault": isDefault,
			// 仓短码用于前端预览「将要生成的主体 SKU」（服务端才是真源）。
			"WarehouseCode": rows[0].WarehouseCode,
			"Items":         items,
		})
	}
	return out, nil
}
