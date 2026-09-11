// product_variant.go — 变体读写（issue #5 / T3a）。
//
// 本文件是「商品级默认值 → 新增变体」的**唯一填充入口**（newVariantFromDefaults）。
// 规则（spec §变体模型）：
//
//	· 只走新增路径（单个新增 / 批量生成 / 导入 / 克隆）；
//	· 逐字段判空，只填空着的字段；
//	· 「空」以 NULL 判定 —— 入参用可空类型，0 与 false 视为已填；
//	· 编辑路径一个字都不动，包括调用方主动清空字段。
package productservice

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"

	productdto "go_wp/internal/module/product/dto"
	productenums "go_wp/internal/module/product/enums"
	productmodel "go_wp/internal/module/product/model"
)

// CreateVariant 为商品新增一个变体；未填字段由商品级默认值逐字段补齐。
func (s *Service) CreateVariant(ctx context.Context, req *productdto.CreateVariantReq) (res *productdto.VariantResp, err error) {
	if req == nil || req.ProductID == "" {
		return nil, errors.New(productenums.ErrInvalidParam)
	}
	p, err := s.m.Get(ctx, req.ProductID)
	if err != nil {
		return nil, mapNotFound(err)
	}
	v := s.newVariantFromDefaults(p, req)
	if taken, serr := s.m.SKUExists(ctx, p.ID, v.SKUCode, ""); serr != nil {
		return nil, serr
	} else if taken {
		return nil, errors.New(productenums.ErrSkuTaken)
	}
	if err = s.m.CreateVariant(ctx, v); err != nil {
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
	return toVariantResp(v), nil
}

// DeleteVariant 删除变体。
func (s *Service) DeleteVariant(ctx context.Context, req *productdto.DeleteVariantReq) (err error) {
	if req == nil || req.ID == "" {
		return errors.New(productenums.ErrInvalidParam)
	}
	if _, gerr := s.m.GetVariant(ctx, req.ID); gerr != nil {
		return mapNotFound(gerr)
	}
	return s.m.DeleteVariant(ctx, req.ID)
}

// newVariantFromDefaults 商品级默认值 → 新变体的**唯一填充入口**。
//
// req 为 nil 表示无表单路径（如商品创建时的首个变体、批量生成、导入）：全部字段取默认值。
// 有 req 时逐字段判空：只有调用方没给（nil）的字段才回落商品级默认值。
func (s *Service) newVariantFromDefaults(p *productmodel.ProductEntity, req *productdto.CreateVariantReq) *productmodel.VariantEntity {
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
		v.SKUCode = generateSKUCode(p.Slug)
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

// generateSKUCode 无编码时生成：{商品 slug}-{6 位随机}，足以避免同商品内碰撞。
func generateSKUCode(productSlug string) string {
	base := productSlug
	if base == "" {
		base = "sku"
	}
	suffix := strings.ReplaceAll(uuid.NewString(), "-", "")[:6]
	return base + "-" + suffix
}

// defaultFloat 可空浮点取默认。
func defaultFloat(v *float64, fallback float64) float64 {
	if v == nil {
		return fallback
	}
	return *v
}
