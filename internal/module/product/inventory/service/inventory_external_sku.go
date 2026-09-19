// inventory_external_sku.go — 仓库 SKU 与外部编码映射（迁移 251 / docs/14 §9.3）。
//
// 用户 2026-09-19 补充确认的三条口径，本文件是它们的唯一落点：
//
//  1. **属性属于商品**，仓库侧只回答「这条货在这个仓叫什么」—— 那件事落在
//     inventory_stocks.external_sku 上（第三方仓 / 平台仓的编码我们改不了，只能映射）；
//  2. 映射是 **N:1**：同一个商品的多个变体（十几个口味）在仓库侧可以共用同一个外码。
//     因此 DDL 上**没有**唯一索引，只有一条弱校验 —— 同一仓内同一外码必须指向
//     同一个 product_id（多口味共用合法，两个不同商品共用一个外码报
//     ErrExternalSKUProductConflict）；
//  3. 空串不是「缺失」而是一种合法状态：该仓用我们自己的 SKU（自营仓的常态）。
//
// 「仓库 SKU」不另建目录（docs/14 §4）：它就是库存真源上已有的 (warehouse_id, sku_code)。
// 本文件提供商品侧「从仓库选」需要的三件事：列出候选、按仓库 + 编码定位、写外码。
package inventoryservice

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"gorm.io/gorm"

	inventorydto "go_wp/internal/module/product/inventory/dto"
	inventoryenums "go_wp/internal/module/product/inventory/enums"
	inventorymodel "go_wp/internal/module/product/inventory/model"
	"go_wp/pkg/utils"
)

const (
	// maxExternalSKULen 外部编码长度上限。
	//
	// 对方编码不受我们控制，但上限要挡住明显的脏写（整段 HTML / 备注粘进来）。
	// 128 足够覆盖各平台的实际编码长度。
	maxExternalSKULen = 128
	// 仓库 SKU 候选列表的分页（抽屉里的快捷入口，默认给一屏）。
	defaultWarehouseSKUSize = 50
	maxWarehouseSKUSize     = 200
)

// NormalizeExternalSKU 归一 + 校验外部 / 第三方编码：去首尾空白、长度上限、拒绝控制字符。
//
// 空串是**合法值**（= 该仓用我们自己的 SKU），不报错。归一规则只有这一份：
// 商品侧「创建即入库」与库存侧「绑定」都先过它，再把结果写库。
func NormalizeExternalSKU(raw string) (string, error) {
	code := strings.TrimSpace(raw)
	if code == "" {
		return "", nil
	}
	if utf8.RuneCountInString(code) > maxExternalSKULen {
		return "", errors.New(inventoryenums.ErrExternalSKUInvalid)
	}
	for _, r := range code {
		// 控制字符（含换行 / 制表 / DEL）：编码是给对方系统解析的标识，不是自由文本。
		if r < 0x20 || r == 0x7f {
			return "", errors.New(inventoryenums.ErrExternalSKUInvalid)
		}
	}
	return code, nil
}

// ListWarehouseSKUs 按工程 / 仓库列出可选的仓库 SKU（商品新建抽屉「从仓库选」的数据源）。
//
// 返回每行的 warehouse_id / sku_code / external_sku / has_variant（还要仓码与名称，
// 免得调用方为展示再查一次仓库）。关键字同时命中我们自己的编码与外部编码 ——
// 运营手上可能是其中任意一个。
func (s *Service) ListWarehouseSKUs(ctx context.Context, req *inventorydto.ListWarehouseSKUReq) (list []*inventorydto.WarehouseSKUResp, err error) {
	page, size := warehouseSKUPageArgs(req)
	var projectID string
	if projectID, err = s.resolveProjectID(ctx, listWarehouseSKUProjectID(req)); err != nil {
		return nil, err
	}
	rows, err := s.m.ListWarehouseSKUs(ctx, inventorymodel.WarehouseSKUFilter{
		ProjectID:   projectID,
		WarehouseID: strings.TrimSpace(listWarehouseSKUWarehouseID(req)),
		Keyword:     strings.TrimSpace(listWarehouseSKUKeyword(req)),
	}, size, (page-1)*size)
	if err != nil {
		return nil, err
	}
	list = make([]*inventorydto.WarehouseSKUResp, 0, len(rows))
	for _, r := range rows {
		list = append(list, toWarehouseSKUResp(r))
	}
	return list, nil
}

// GetWarehouseSKU 「从仓库选」的最小查询：给定仓库 + 我们自己那条仓库 SKU，返回该行。
//
// 不存在时明确报 ErrWarehouseSKUNotFound（不返回空行、也不静默当作「没选」）：
// 商品侧据此拒绝创建，运营拿到的是「换个仓库或先建这条货」这种可行动的提示。
func (s *Service) GetWarehouseSKU(ctx context.Context, req *inventorydto.GetWarehouseSKUReq) (res *inventorydto.WarehouseSKUResp, err error) {
	if req == nil || strings.TrimSpace(req.WarehouseID) == "" || strings.TrimSpace(req.SKUCode) == "" {
		return nil, errors.New(inventoryenums.ErrWarehouseSKURequired)
	}
	var projectID string
	if projectID, err = s.resolveProjectID(ctx, req.ProjectID); err != nil {
		return nil, err
	}
	e, err := s.lookupWarehouseSKU(ctx, projectID, strings.TrimSpace(req.WarehouseID), strings.TrimSpace(req.SKUCode))
	if err != nil {
		return nil, err
	}
	return s.toWarehouseSKURespFromEntity(ctx, e), nil
}

// BindExternalSKU 绑定 / 更新某 (仓库, 变体) 库存行的外部编码。
//
// 三条判定，顺序不能换：
//  1. 归一编码（空串合法 = 清空，该仓改回用我们自己的 SKU）；
//  2. 那一行必须真实存在 —— 否则 UPDATE 匹配 0 行却返回成功，「绑定成功」是假的；
//  3. N:1 弱校验：同一仓内同一外码必须指向同一个商品（多口味共用合法，跨商品报错）。
func (s *Service) BindExternalSKU(ctx context.Context, req *inventorydto.BindExternalSKUReq) (res *inventorydto.StockResp, err error) {
	if req == nil || strings.TrimSpace(req.WarehouseID) == "" || strings.TrimSpace(req.VariantID) == "" {
		return nil, errors.New(inventoryenums.ErrInvalidParam)
	}
	var projectID string
	if projectID, err = s.resolveProjectID(ctx, req.ProjectID); err != nil {
		return nil, err
	}
	warehouseID := strings.TrimSpace(req.WarehouseID)
	variantID := strings.TrimSpace(req.VariantID)
	code, err := NormalizeExternalSKU(req.ExternalSKU)
	if err != nil {
		return nil, err
	}
	row, err := s.m.GetStockByVariantWarehouse(ctx, variantID, warehouseID, projectID)
	if err != nil {
		return nil, mapStockNotFound(err)
	}
	if err = s.assertExternalSKUProductScope(ctx, projectID, warehouseID, code, row.ProductID); err != nil {
		return nil, err
	}
	affected, err := s.m.SetExternalSKUByVariantWarehouse(ctx, projectID, warehouseID, variantID, code)
	if err != nil {
		return nil, err
	}
	if affected == 0 {
		// 行在读取与写入之间被删（或工程作用域不一致）：不谎报成功。
		return nil, errors.New(inventoryenums.ErrStockNotFound)
	}
	row.ExternalSKU = code
	return s.toStockResp(ctx, row), nil
}

// CheckExternalSKUProductScope 商品侧「创建即入库」前的 N:1 弱校验（按目标商品判定）。
//
// 与 BindExternalSKU 内部那条校验是同一条规则（共用 assertExternalSKUProductScope）：
// 新商品的库存行还没建，所以由调用方把**将要成为归属**的 product_id 传进来 ——
// 该仓若已有别的商品占了同一个外码，这里就会拦住。
func (s *Service) CheckExternalSKUProductScope(ctx context.Context, projectID, warehouseID, externalSKU, productID string) (err error) {
	code, err := NormalizeExternalSKU(externalSKU)
	if err != nil {
		return err
	}
	return s.assertExternalSKUProductScope(ctx, projectID, warehouseID, code, productID)
}

// CheckStockSKUCodeFree 仓内 sku_code 唯一预检（写库存行之前调用）。
//
// 判据与 DDL 上的 UNIQUE (warehouse_id, sku_code)（迁移 244）一致，只是提前到写入之前：
// 直接撞约束只会拿到一个没有上下文的 23505，运营看不到「哪个仓、哪条编码」。
func (s *Service) CheckStockSKUCodeFree(ctx context.Context, projectID, warehouseID, skuCode string) (err error) {
	exists, err := s.m.StockSKUCodeExists(ctx, projectID, warehouseID, strings.TrimSpace(skuCode), "")
	if err != nil {
		return err
	}
	if exists {
		return errors.New(inventoryenums.ErrWarehouseSKUCodeTaken)
	}
	return nil
}

// lookupWarehouseSKU 按 (仓库, 仓库 SKU) 定位那一行；不存在时给业务错误。
func (s *Service) lookupWarehouseSKU(ctx context.Context, projectID, warehouseID, skuCode string) (e *inventorymodel.StockEntity, err error) {
	e, err = s.m.FindStockByWarehouseSKU(ctx, projectID, warehouseID, skuCode)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errors.New(inventoryenums.ErrWarehouseSKUNotFound)
		}
		return nil, err
	}
	return e, nil
}

// assertExternalSKUProductScope N:1 弱校验：同一个 external_sku 在同一个仓库内必须指向
// 同一个 product_id。空串（= 该仓用我们自己的 SKU）不参与校验。
//
// 错误里带上冲突的商品 id：只说「冲突了」等于让人去猜哪一条占了它。
func (s *Service) assertExternalSKUProductScope(ctx context.Context, projectID, warehouseID, externalSKU, productID string) (err error) {
	if externalSKU == "" {
		return nil
	}
	owner, err := s.m.ExternalSKUProductConflict(ctx, projectID, warehouseID, externalSKU, productID)
	if err != nil {
		return err
	}
	if owner != "" {
		return fmt.Errorf("%s：该外部编码在本仓已属于商品 %s", inventoryenums.ErrExternalSKUProductConflict, owner)
	}
	return nil
}

// toWarehouseSKUResp 投影行 → 响应。
func toWarehouseSKUResp(r *inventorymodel.WarehouseSKURow) *inventorydto.WarehouseSKUResp {
	return &inventorydto.WarehouseSKUResp{
		WarehouseID: r.WarehouseID, WarehouseCode: r.WarehouseCode, WarehouseName: r.WarehouseName,
		IsDefault: r.IsDefault, SKUCode: r.SKUCode, ExternalSKU: r.ExternalSKU,
		ProductID: r.ProductID, VariantID: r.VariantID, HasVariant: r.HasVariant,
	}
}

// toWarehouseSKURespFromEntity 库存实体 → 响应（补仓库展示信息；查不到仓库时留空，
// 与 toStockResp 同一兜底：展示字段读不到不影响主结果）。
func (s *Service) toWarehouseSKURespFromEntity(ctx context.Context, e *inventorymodel.StockEntity) *inventorydto.WarehouseSKUResp {
	if e == nil {
		return nil
	}
	resp := &inventorydto.WarehouseSKUResp{
		WarehouseID: e.WarehouseID, SKUCode: e.SKUCode, ExternalSKU: e.ExternalSKU,
		ProductID: e.ProductID, VariantID: e.VariantID, HasVariant: strings.TrimSpace(e.VariantID) != "",
	}
	if wh, werr := s.m.GetWarehouse(ctx, e.WarehouseID, e.ProjectID); werr == nil {
		resp.WarehouseCode, resp.WarehouseName, resp.IsDefault = wh.Code, wh.Name, wh.IsDefault
	}
	return resp
}

// warehouseSKUPageArgs 归一化仓库 SKU 列表的分页参数。
func warehouseSKUPageArgs(req *inventorydto.ListWarehouseSKUReq) (page, size int) {
	inPage, inSize := 0, 0
	if req != nil {
		inPage, inSize = req.Page, req.Size
	}
	paging := utils.NormalizePaging(inPage, inSize, defaultWarehouseSKUSize, maxWarehouseSKUSize)
	return paging.Page, paging.Size
}

// 下面三个小取值器只为「req 可能为 nil」这一件事存在，避免在方法体里散落三处判空。
// 名字带 listWarehouseSKU 前缀：inventory_source.go 已有一组同用途的
// projectIDOf / keywordOf（那个是货源列表的 req），同名会在包级直接撞车。
func listWarehouseSKUProjectID(req *inventorydto.ListWarehouseSKUReq) string {
	if req == nil {
		return ""
	}
	return req.ProjectID
}

func listWarehouseSKUWarehouseID(req *inventorydto.ListWarehouseSKUReq) string {
	if req == nil {
		return ""
	}
	return req.WarehouseID
}

func listWarehouseSKUKeyword(req *inventorydto.ListWarehouseSKUReq) string {
	if req == nil {
		return ""
	}
	return req.Keyword
}
