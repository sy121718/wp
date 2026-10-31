package productservice

import productmodel "go_wp/internal/module/product/model"

type variantPriceSummary struct {
	HasAny     bool
	MinPrice   float64
	MaxPrice   float64
	MinVariant *productmodel.VariantEntity
}

func summarizeVariantPrices(variants []*productmodel.VariantEntity, onlyEnabled bool) variantPriceSummary {
	var out variantPriceSummary
	for _, v := range variants {
		if v == nil {
			continue
		}
		if onlyEnabled && !v.Enabled {
			continue
		}
		if !out.HasAny {
			out.HasAny = true
			out.MinPrice, out.MaxPrice = v.Price, v.Price
			out.MinVariant = v
		} else {
			if v.Price > out.MaxPrice {
				out.MaxPrice = v.Price
			}
			if v.Price < out.MinPrice || (v.Price == out.MinPrice && cheaperVariantWins(v, out.MinVariant)) {
				out.MinPrice, out.MinVariant = v.Price, v
			}
		}
	}
	return out
}

func (s variantPriceSummary) ComparePrice() *float64 {
	if s.MinVariant == nil {
		return nil
	}
	return s.MinVariant.ComparePrice
}

func cheaperVariantWins(challenger, current *productmodel.VariantEntity) bool {
	if current == nil {
		return true
	}
	return challenger.ID < current.ID
}
