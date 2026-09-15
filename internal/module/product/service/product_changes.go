// product_changes.go — 主数据变更记录接入（issue #19）。
//
// 本文件是商品模块与 masterdata 模块之间的**唯一适配面**：
//
//   - 商品侧定义自己的字段白名单（商品 / 变体各一份快照），masterdata 只做 diff 与落库；
//   - 端口（masterdatacontract.MasterDataService）在装配期注入，未注入时静默跳过
//     （纯商品单测路径，与 variantStock 端口同一手法）——生产装配恒注入。
//
// 覆盖的验收字段（issue #19 验收 2）：
//
//	变体默认发货仓 —— 建变体时解析出的归属仓（home_warehouse_id / home_warehouse_code）；
//	SKU 编码       —— 建 / 改 / 删变体都会落行；
//	商品价格       —— 变体售价（含定价工具批量改价与入库成本价回写）；
//	上下架状态     —— 商品 status（draft / published / …）；
//	货源资料       —— 见 inventory 模块的 source 快照。
package productservice

import (
	"context"

	masterdatacontract "go_wp/internal/module/masterdata/contract"
	masterdataenums "go_wp/internal/module/masterdata/enums"
	productcontract "go_wp/internal/module/product/contract"
	productmodel "go_wp/internal/module/product/model"

	"gorm.io/gorm"
)

// productChangeSnapshot 商品主数据的字段白名单快照。
//
// 只收「主数据」：名称 / URL 段 / 上下架状态 / 商品级默认售价 / 品牌。
// 版本元数据（update_time、sort）与派生值（价格区间）一律不进快照 ——
// 进了就会每次写操作都产生一串没有信息量的记录。
func productChangeSnapshot(e *productmodel.ProductEntity) masterdatacontract.FieldSnapshot {
	if e == nil {
		return nil
	}
	return masterdatacontract.NewSnapshot(
		"name", e.Name,
		"slug", e.Slug,
		"status", e.Status,
		"default_price", masterdatacontract.FormatPricePtr(e.DefaultPrice),
		"brand_id", masterdatacontract.FormatStringPtr(e.BrandID),
	)
}

// variantChangeSnapshot 变体主数据的字段白名单快照。
//
// ref 非 nil 时才写「默认发货仓」两个字段：变体的默认发货仓在商品侧没有独立列
// （由 SKU 编码前缀与库存记录行表达），取值只能来自**建变体时**解析出的归属仓。
// 编辑路径拿不到它，于是不写这两个键 —— 否则会把「这次操作根本没涉及该字段」
// 误记成「默认发货仓被清空了」。
func variantChangeSnapshot(v *productmodel.VariantEntity, ref *productcontract.WarehouseRef) masterdatacontract.FieldSnapshot {
	if v == nil {
		return nil
	}
	snap := masterdatacontract.NewSnapshot(
		"sku_code", v.SKUCode,
		"barcode", v.Barcode,
		"price", masterdatacontract.FormatPrice(v.Price),
		"compare_price", masterdatacontract.FormatPricePtr(v.ComparePrice),
		"cost_price", masterdatacontract.FormatPricePtr(v.CostPrice),
		"enabled", masterdatacontract.FormatBool(v.Enabled),
		"option_values", masterdatacontract.FormatJSON(v.OptionValues),
	)
	if ref != nil {
		snap["home_warehouse_id"] = ref.ID
		snap["home_warehouse_code"] = ref.Code
	}
	return snap
}

// variantProjectID 变体所属工程（变更记录按工程隔离，变体行只有 product_id）。
//
// 只在留痕端口已注入时才多查一次商品：未注入的纯商品单测路径一个多余的查询都不发。
func (s *Service) variantProjectID(ctx context.Context, v *productmodel.VariantEntity) (projectID string, err error) {
	if s.changes == nil || v == nil {
		return "", nil
	}
	p, gerr := s.m.Get(ctx, v.ProductID, "")
	if gerr != nil {
		return "", mapNotFound(gerr)
	}
	return p.ProjectID, nil
}

// recordChanges 记录主数据变更（端口未注入时空转：纯商品单测路径）。
//
// 写入发生在业务写操作**之后**：跨模块写不进同一个事务（表隔离约定），
// 端口失败即把错误透出给调用方，不静默丢记录。
func (s *Service) recordChanges(ctx context.Context, inputs ...*masterdatacontract.ChangeInput) (err error) {
	if s.changes == nil || len(inputs) == 0 {
		return nil
	}
	return s.changes.RecordChanges(ctx, inputs)
}

func (s *Service) recordChangesTx(ctx context.Context, tx *gorm.DB, inputs ...*masterdatacontract.ChangeInput) (err error) {
	if s.changes == nil || len(inputs) == 0 {
		return nil
	}
	return s.changes.RecordChangesTx(ctx, tx, inputs)
}

// productChangeInput 组装商品级变更输入。
func productChangeInput(e *productmodel.ProductEntity, action, origin, operator string,
	before, after masterdatacontract.FieldSnapshot) *masterdatacontract.ChangeInput {
	if e == nil {
		return nil
	}
	return &masterdatacontract.ChangeInput{
		ProjectID: e.ProjectID, EntityType: masterdataenums.EntityProduct,
		EntityID: e.ID, EntityLabel: e.Name,
		Action: action, Origin: origin, OperatorID: operator,
		Before: before, After: after,
	}
}

// variantChangeInput 组装变体级变更输入。
func variantChangeInput(projectID string, v *productmodel.VariantEntity, action, origin, operator string,
	before, after masterdatacontract.FieldSnapshot) *masterdatacontract.ChangeInput {
	if v == nil {
		return nil
	}
	return &masterdatacontract.ChangeInput{
		ProjectID: projectID, EntityType: masterdataenums.EntityProductVariant,
		EntityID: v.ID, EntityLabel: v.SKUCode,
		Action: action, Origin: origin, OperatorID: operator,
		Before: before, After: after,
	}
}
