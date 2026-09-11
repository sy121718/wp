// product_variant.go — 变体读写（issue #5 / T3a；归属仓与库存记录见 issue #15）。
//
// 本文件是「商品级默认值 → 新增变体」的**唯一填充入口**（newVariantFromDefaults）。
// 规则（spec §变体模型）：
//
//	· 只走新增路径（单个新增 / 批量生成 / 导入 / 克隆）；
//	· 逐字段判空，只填空着的字段；
//	· 「空」以 NULL 判定 —— 入参用可空类型，0 与 false 视为已填；
//	· 编辑路径一个字都不动，包括调用方主动清空字段。
//
// SKU 编码（issue #15）：建变体时选择的归属仓短码是编码前缀
// （{仓短码}_{商品码}_{序号}，如 SZ_TEE_001）；未选仓则用默认仓短码。
// 前缀表达的是**默认发货仓**，不是「这个 SKU 只属于这个仓」——
// SKU 本身是全局的，货可以在多个仓分布、可以调拨，移动与调拨不改变编码。
package productservice

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	productcontract "go_wp/internal/module/product/contract"
	productdto "go_wp/internal/module/product/dto"
	productenums "go_wp/internal/module/product/enums"
	productmodel "go_wp/internal/module/product/model"
)

// maxSKUProbe SKU 序号 / 随机段的探测次数上限（序号被历史删除的变体占住时向后找空位）。
const maxSKUProbe = 500

// CreateVariant 为商品新增一个变体；未填字段由商品级默认值逐字段补齐。
//
// 归属仓（issue #15）：WarehouseID 为空即兜底该工程的默认仓；无论选没选，
// 该 SKU 都会在归属仓生成一条库存记录（初始 0）—— 库存真源在仓库模块。
func (s *Service) CreateVariant(ctx context.Context, req *productdto.CreateVariantReq) (res *productdto.VariantResp, err error) {
	if req == nil || req.ProductID == "" {
		return nil, errors.New(productenums.ErrInvalidParam)
	}
	p, err := s.m.Get(ctx, req.ProductID)
	if err != nil {
		return nil, mapNotFound(err)
	}
	// 归属仓先解析再落库：解析失败（如工程内没有默认仓）时一条变体都不写。
	ref, err := s.resolveWarehouseRef(ctx, p.ProjectID, req.WarehouseID)
	if err != nil {
		return nil, err
	}
	v := s.newVariantFromDefaults(ctx, p, req, refCode(ref), nil)
	if taken, serr := s.m.SKUExists(ctx, p.ID, v.SKUCode, ""); serr != nil {
		return nil, serr
	} else if taken {
		return nil, errors.New(productenums.ErrSkuTaken)
	}
	if err = s.m.CreateVariant(ctx, v); err != nil {
		return nil, err
	}
	if err = s.ensureVariantStock(ctx, ref, p.ID, v.ID, v.SKUCode); err != nil {
		return nil, err
	}
	// 重算时机之一：变体写操作后 —— 价格 / 对比价参与自动标签规则判定。
	if err = s.recalcProjectAutoTags(ctx, p.ID); err != nil {
		return nil, err
	}
	return toVariantResp(v), nil
}

// UpdateVariant 修改变体。
//
// 编辑路径不做默认值填充：调用方清空某字段就是清空，不回落商品级默认值。
func (s *Service) UpdateVariant(ctx context.Context, req *productdto.UpdateVariantReq) (res *productdto.VariantResp, err error) {
	if req == nil || req.ID == "" {
		return nil, errors.New(productenums.ErrInvalidParam)
	}
	v, err := s.m.GetVariant(ctx, req.ID)
	if err != nil {
		return nil, mapNotFound(err)
	}
	if req.SKUCode != nil {
		code := strings.TrimSpace(*req.SKUCode)
		if code == "" {
			return nil, errors.New(productenums.ErrInvalidParam)
		}
		if taken, serr := s.m.SKUExists(ctx, v.ProductID, code, v.ID); serr != nil {
			return nil, serr
		} else if taken {
			return nil, errors.New(productenums.ErrSkuTaken)
		}
		v.SKUCode = code
	}
	if req.Barcode != nil {
		v.Barcode = *req.Barcode
	}
	if req.Price != nil {
		v.Price = *req.Price
	}
	if req.ComparePrice != nil {
		v.ComparePrice = req.ComparePrice
	}
	if req.CostPrice != nil {
		v.CostPrice = req.CostPrice
	}
	if req.Image != nil {
		v.Image = *req.Image
	}
	if req.OptionValues != nil {
		v.OptionValues = req.OptionValues
	}
	if req.Enabled != nil {
		v.Enabled = *req.Enabled
	}
	if req.Sort != nil {
		v.Sort = *req.Sort
	}
	v.UpdatedAt = time.Now().UTC()
	if err = s.m.UpdateVariant(ctx, v); err != nil {
		return nil, err
	}
	// 重算时机之一：变体写操作后（改价格 / 改启用状态都会改自动标签归属）。
	if err = s.recalcProjectAutoTags(ctx, v.ProductID); err != nil {
		return nil, err
	}
	return toVariantResp(v), nil
}

// DeleteVariant 删除变体。
//
// 该 SKU 在各仓的库存记录由外键 ON DELETE CASCADE 连带删除
// （变体不再存在即无货可存；库存流水与账目留给 issue #16 的流水表）。
func (s *Service) DeleteVariant(ctx context.Context, req *productdto.DeleteVariantReq) (err error) {
	if req == nil || req.ID == "" {
		return errors.New(productenums.ErrInvalidParam)
	}
	v, gerr := s.m.GetVariant(ctx, req.ID)
	if gerr != nil {
		return mapNotFound(gerr)
	}
	if err = s.m.DeleteVariant(ctx, req.ID); err != nil {
		return err
	}
	// 重算时机之一：变体写操作后（删掉唯一命中价格区间的变体会让商品脱钩）。
	return s.recalcProjectAutoTags(ctx, v.ProductID)
}

// newVariantFromDefaults 商品级默认值 → 新变体的**唯一填充入口**。
//
// req 为 nil 表示无表单路径（如商品创建时的首个变体、批量生成、导入）：全部字段取默认值。
// 有 req 时逐字段判空：只有调用方没给（nil）的字段才回落商品级默认值。
//
// warehouseCode 是归属仓短码（issue #15）：非空时 SKU 编码为
// {仓短码}_{商品码}_{序号}；为空则退回 {商品码}-{随机段}（端口未注入的纯商品路径）。
// taken 是「本批已占用」的 SKU 编码集合（批量生成一次写多行时逐个探测序号，
// 否则同一批里的新变体都会取到同一个「下一个序号」而互相撞号）；单个新增传 nil。
func (s *Service) newVariantFromDefaults(ctx context.Context, p *productmodel.ProductEntity, req *productdto.CreateVariantReq, warehouseCode string, taken map[string]bool) *productmodel.VariantEntity {
	now := time.Now().UTC()
	v := &productmodel.VariantEntity{
		ID: uuid.NewString(), ProductID: p.ID,
		Price:        defaultFloat(p.DefaultPrice, 0),
		ComparePrice: p.DefaultComparePrice,
		CostPrice:    p.DefaultCostPrice,
		Image:        p.DefaultImage,
		OptionValues: json.RawMessage("{}"),
		Enabled:      true,
		Metadata:     json.RawMessage("{}"),
		CreatedAt:    now,
		UpdatedAt:    now,
	}
	if req != nil {
		if req.Price != nil {
			v.Price = *req.Price
		}
		if req.ComparePrice != nil {
			v.ComparePrice = req.ComparePrice
		}
		if req.CostPrice != nil {
			v.CostPrice = req.CostPrice
		}
		if req.Image != "" {
			v.Image = req.Image
		}
		if len(req.OptionValues) > 0 {
			v.OptionValues = req.OptionValues
		}
		if req.Enabled != nil {
			v.Enabled = *req.Enabled
		}
		v.Sort = req.Sort
		v.Barcode = req.Barcode
	}
	v.SKUCode = strings.TrimSpace(skuOrEmpty(req))
	if v.SKUCode == "" {
		v.SKUCode = s.generateSKUCode(ctx, p, warehouseCode, taken)
	}
	return v
}

// skuOrEmpty 取调用方给的 SKU 编码（req 为 nil 时为空串，交由生成规则兜底）。
func skuOrEmpty(req *productdto.CreateVariantReq) string {
	if req == nil {
		return ""
	}
	return req.SKUCode
}

// generateSKUCode 生成 SKU 编码（issue #15 起的两段规则入口）。
//
// 有归属仓短码：{仓短码}_{商品码}_{序号}，形如 SZ_TEE_001 ——
// 序号从「现有变体数 + 1」起向后探测，被占用的序号跳过（删除变体会留下空洞）。
//
// 无归属仓短码（端口未注入的纯商品路径）：退回 {商品 slug}-{随机段}，
// 随机段是既有实现，保证 SKU 在商品内唯一。
func (s *Service) generateSKUCode(ctx context.Context, p *productmodel.ProductEntity, warehouseCode string, taken map[string]bool) string {
	if code := strings.TrimSpace(warehouseCode); code != "" {
		prefix := strings.ToUpper(code) + "_" + productCodeSegment(p.Slug) + "_"
		start := 1
		if n, err := s.m.CountVariants(ctx, p.ID); err == nil && n > 0 {
			start = int(n) + 1
		}
		for i := start; i < start+maxSKUProbe; i++ {
			candidate := prefix + fmt.Sprintf("%03d", i)
			if taken[candidate] {
				continue
			}
			exists, err := s.m.SKUExists(ctx, p.ID, candidate, "")
			if err != nil || !exists {
				return candidate
			}
		}
	}
	for i := 0; i < maxSKUProbe; i++ {
		candidate := legacySKUCode(p.Slug)
		if !taken[candidate] {
			return candidate
		}
	}
	return legacySKUCode(p.Slug)
}

// productCodeSegment 商品 slug → SKU 编码里的商品段（大写字母数字；中文 slug 兜底 SKU）。
func productCodeSegment(slug string) string {
	var b strings.Builder
	for _, r := range strings.ToUpper(slug) {
		if (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		}
	}
	if b.Len() == 0 {
		return "SKU"
	}
	return b.String()
}

// legacySKUCode 无归属仓短码时的 SKU 编码：{商品 slug}-{6 位随机段}。
func legacySKUCode(productSlug string) string {
	base := productSlug
	if base == "" {
		base = "sku"
	}
	return base + "-" + randomSegment()
}

// randomSegment 6 位随机段（由 uuid 取前 6 位十六进制，足以避免同商品内碰撞）。
func randomSegment() string {
	return strings.ReplaceAll(uuid.NewString(), "-", "")[:6]
}

// —— 归属仓与库存记录（issue #15，端口由 inventory 模块实现）——

// resolveWarehouseRef 解析变体归属仓。
//
// 端口（productcontract.VariantStockPort）由 inventory 模块实现、装配期注入；
// 未注入时返回 nil —— 表示「不生成库存记录、SKU 不带仓前缀」（纯商品单测路径），
// 生产装配恒注入。
func (s *Service) resolveWarehouseRef(ctx context.Context, projectID, warehouseID string) (ref *productcontract.WarehouseRef, err error) {
	if s.variantStock == nil {
		return nil, nil
	}
	return s.variantStock.ResolveWarehouse(ctx, projectID, warehouseID)
}

// ensureVariantStock 在归属仓为该 SKU 生成库存记录（初始 0，幂等）。
//
// 库存真源在仓库模块；商品侧只保留 stock_total 这个列表展示用缓存，
// 不做任何可用量判断（spec §库存 死线）。端口未注入 / 未解析到归属仓时空转。
func (s *Service) ensureVariantStock(ctx context.Context, ref *productcontract.WarehouseRef, productID, variantID, skuCode string) (err error) {
	if s.variantStock == nil || ref == nil {
		return nil
	}
	return s.variantStock.EnsureVariantStock(ctx, ref, productID, variantID, skuCode)
}

// refCode 归属仓短码（ref 为 nil 时为空串，即「无仓前缀」路径）。
func refCode(ref *productcontract.WarehouseRef) string {
	if ref == nil {
		return ""
	}
	return ref.Code
}

// defaultFloat 可空浮点取默认。
func defaultFloat(v *float64, fallback float64) float64 {
	if v == nil {
		return fallback
	}
	return *v
}
