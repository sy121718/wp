// Package inventory 库存数据 → 商品页面选项视图的适配器（形状翻译层）。
//
// 位置判据（internal/module/CLAUDE.md §outbound）：这里**要翻译** —— 把库存模块的
// 仓库 / 仓库 SKU 行翻成商品页面要的选项视图（标签口径、SKU 前缀剥离、每仓分组与上限）。
// 签名对得上、不需要翻译的适配才留在 service 同包。
//
// 依赖方向：本包 → 库存模块的 **contract**（不认识对方的 service / model），
// 加上本模块的 contract / dto 与自己的命名规则（`service.StripWarehousePrefix`）。
// 反向没有依赖：product 的 service / inbound 都不 import 本包之外的东西来读库存。
package inventory

import (
	"context"
	"strings"

	inventorycontract "go_wp/internal/module/inventory/contract"
	productcontract "go_wp/internal/module/product/contract"
	productdto "go_wp/internal/module/product/dto"
	productservice "go_wp/internal/module/product/service"
)

// warehouseSKUOptionSize 每个仓最多列多少条候选货。
//
// 候选是**快捷入口**：选不到的去库存页核对（那里有分页与关键字）。把整张库存表拉进内存，
// 在仓库多 / 货多时是纯粹的浪费，而这里只需要「最近常用的一屏」。
const warehouseSKUOptionSize = 100

// Options 实现 productcontract.WarehouseOptionsPort。
type Options struct {
	svc inventorycontract.InventoryService
}

// New 构造适配器。svc 为 nil 时端口恒返回空清单（未接库存契约的降级路径，
// 行为与「商品页没有仓库下拉」逐字一致）。
func New(svc inventorycontract.InventoryService) *Options {
	return &Options{svc: svc}
}

var _ productcontract.WarehouseOptionsPort = (*Options)(nil)

// WarehouseOptions 某工程的仓库下拉项（默认仓在最前并标注）。
//
// Label 只拼**数据**（仓名 + 编码）；「默认仓」后缀是词条，交模板按请求语言取词
// （模板见 admin/product/product_create_form.html 的 `{{w.Label}}{{if w.IsDefault}}`）。
// 控制器提前翻好会把这一句锁死在一种语言 —— 英文界面上留一句中文，不报错、门禁也不红。
//
// 除展示用的 Label 外还带上 Code 与 Name：列表「库存」列的分仓明细要按**工程仓库清单**
// 逐仓显示三态（服务端只返回有库存行的仓，「这个仓没有这一行」= 未入库，是页面才知道的事实）。
func (o *Options) WarehouseOptions(ctx context.Context, projectID string) (out []productdto.WarehouseOptionResp, err error) {
	out = []productdto.WarehouseOptionResp{}
	if o.svc == nil || strings.TrimSpace(projectID) == "" {
		return out, nil
	}
	rows, lerr := o.svc.ListWarehouses(ctx, &inventorycontract.ListWarehouseReq{ProjectID: projectID})
	if lerr != nil {
		return nil, lerr
	}
	for _, w := range rows {
		if w == nil {
			continue
		}
		out = append(out, productdto.WarehouseOptionResp{
			ID:                    w.ID,
			Code:                  w.Code,
			Name:                  w.Name,
			Label:     w.Name + "（" + w.Code + "）",
			IsDefault: w.IsDefault,
		})
	}
	return out, nil
}

// WarehouseSKUOptions 每仓的候选货（迁移 251 的「从仓库选」）。
//
// 逐仓取：一次取全库再在内存分组会把整张表拉进内存；每仓上限见 warehouseSKUOptionSize。
// 没有货的仓整组跳过（模板据 len 判断，不给空 optgroup）。
func (o *Options) WarehouseSKUOptions(ctx context.Context, projectID string, warehouses []productdto.WarehouseOptionResp) (out []productdto.WarehouseSKUGroupResp, err error) {
	out = []productdto.WarehouseSKUGroupResp{}
	if o.svc == nil || strings.TrimSpace(projectID) == "" {
		return out, nil
	}
	for _, w := range warehouses {
		if strings.TrimSpace(w.ID) == "" {
			continue
		}
		rows, lerr := o.svc.ListWarehouseSKUs(ctx, &inventorycontract.ListWarehouseSKUReq{
			ProjectID: projectID, WarehouseID: w.ID, Size: warehouseSKUOptionSize,
		})
		if lerr != nil {
			return nil, lerr
		}
		if len(rows) == 0 {
			continue
		}
		items := make([]productdto.WarehouseSKUItemResp, 0, len(rows))
		warehouseCode := ""
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
			if warehouseCode == "" {
				warehouseCode = r.WarehouseCode
			}
			items = append(items, productdto.WarehouseSKUItemResp{
				SKUCode: code, ExternalSKU: r.ExternalSKU, Label: label,
			})
		}
		out = append(out, productdto.WarehouseSKUGroupResp{
			WarehouseID:   w.ID,
			WarehouseCode: warehouseCode,
			Label:         w.Label,
			IsDefault:     w.IsDefault,
			Items:         items,
		})
	}
	return out, nil
}
