package productservice

import (
	"context"
	"strings"

	"github.com/google/uuid"
	productdto "go_wp/internal/module/product/dto"
	productenums "go_wp/internal/module/product/enums"
	productmodel "go_wp/internal/module/product/model"
	"go_wp/pkg/i18n"
	json "go_wp/pkg/jsonx"
)

// PublicProductOptions 与构建字段源共用规格口径，但仅投影公开的启用变体事实。
func (s *Service) PublicProductOptions(ctx context.Context, req *productdto.PublicProductOptionsReq) (*productdto.PublicProductOptionsResp, error) {
	if req == nil || uuid.Validate(req.ProjectID) != nil || uuid.Validate(req.ProductID) != nil {
		return nil, productenums.Err(productenums.ErrInvalidParam)
	}
	row, err := s.m.Get(ctx, req.ProductID, req.ProjectID)
	if err != nil {
		return nil, mapNotFound(err)
	}
	if row.ProjectID != req.ProjectID || row.Status != productenums.StatusPublished {
		return nil, productenums.Err(productenums.ErrNotFound)
	}
	all, err := s.m.ListVariants(ctx, row.ID)
	if err != nil {
		return nil, err
	}
	variants := make([]*productmodel.VariantEntity, 0, len(all))
	for _, variant := range all {
		if variant.Enabled {
			variants = append(variants, variant)
		}
	}
	attrs, err := s.m.ListAttributesByIDs(ctx, decodeStrings(row.AttributeIDs), req.ProjectID)
	if err != nil {
		return nil, err
	}
	lang := strings.TrimSpace(req.Lang)
	if lang == "" {
		lang = i18n.GetDefaultLang()
	}
	loc, err := s.localizeRelated(ctx, lang, row)
	if err != nil {
		return nil, err
	}
	// 规格维度与实体构建源共享唯一实现，避免两处属性启用和变体引用规则漂移。
	var groups []optionGroupJSON
	if err = json.Unmarshal([]byte(optionsJSON(row, variants, attrs, loc)), &groups); err != nil {
		return nil, err
	}
	values := s.translateFields(ctx, lang, "product", map[string]string{"name": row.Name})
	result := &productdto.PublicProductOptionsResp{ID: row.ID, Name: values["name"], Options: []productdto.PublicProductOptionResp{}, Variants: []productdto.PublicProductVariantOptionResp{}}
	for _, group := range groups {
		option := productdto.PublicProductOptionResp{Key: group.Key, Name: group.Name, Values: []productdto.PublicProductOptionValueResp{}}
		for _, value := range group.Values {
			option.Values = append(option.Values, productdto.PublicProductOptionValueResp{Key: value.Key, Name: value.Label})
		}
		result.Options = append(result.Options, option)
	}
	for _, variant := range variants {
		fact := productdto.PublicProductVariantOptionResp{ID: variant.ID, SKU: variant.SKUCode, Options: map[string]string{}, Price: yuanToCents(variant.Price)}
		if variant.ComparePrice != nil {
			value := yuanToCents(*variant.ComparePrice)
			fact.ComparePrice = &value
		}
		if len(variant.OptionValues) != 0 {
			if err = json.Unmarshal(variant.OptionValues, &fact.Options); err != nil {
				return nil, err
			}
		}
		result.Variants = append(result.Variants, fact)
	}
	return result, nil
}
