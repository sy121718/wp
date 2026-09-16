package productservice

// entity_source_resolve.go — 实体字段源的注册与解析（按实体类型取 resolver、字段名拆分、未找到映射）。

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"go_wp/internal/builder/core"
	productcontract "go_wp/internal/module/product/contract"
	productenums "go_wp/internal/module/product/enums"
	productmodel "go_wp/internal/module/product/model"

	"gorm.io/gorm"
)

// entityFieldSource 商品域实体类型的字段来源适配器。
type entityFieldSource struct {
	svc        *Service
	entityType string
}

func (e *entityFieldSource) EntityType() string { return e.entityType }

// FieldWhitelist 实现 core.EntityFieldSource（白名单仍取自本模块契约的唯一来源）。
func (e *entityFieldSource) FieldWhitelist() []string {
	return productcontract.FieldWhitelist(e.entityType)
}

// ResolverFor 实现 core.EntityFieldSource。
func (e *entityFieldSource) ResolverFor(ctx context.Context, entityID string) (core.ContentResolver, error) {
	return e.svc.ResolverFor(ctx, e.entityType, entityID)
}

// RegisterEntityTypes 把本模块的实体类型注册进注册表（装配期调用）。
//
// 注册表为 nil 视为装配缺陷（fail-closed）：静默跳过会让构建层到运行期才发现
// 「类型非法」，比装配期直接报错更难排查（与 content 模块同一口径）。
func (s *Service) RegisterEntityTypes(reg core.EntitySourceRegistry) error {
	if reg == nil {
		return errors.New("实体类型注册表为空")
	}
	for _, t := range productcontract.EntityTypes() {
		if err := reg.Register(&entityFieldSource{svc: s, entityType: t}); err != nil {
			return err
		}
	}
	return nil
}

// ResolverFor 返回绑定单个实体的字段解析器（构建期注入）。
//
// 一次性把白名单字段值算好（含价格区间等派生值、分类/品牌/标签/属性值的译文）
// 并挂上译文，构建期只读内存；不存在的实体返回 ErrNotFound，类型不符返回 ErrInvalidType。
func (s *Service) ResolverFor(ctx context.Context, entityType, entityID string) (r core.ContentResolver, err error) {
	if !productcontract.IsValidType(entityType) {
		return nil, errors.New(productenums.ErrInvalidType)
	}
	lang := core.BuildLang(ctx)
	// 工程作用域例外（审计 DB-009，product 域第三批）：本方法在 builder.Compile **之前**
	// 被 presentation 的 renderHTML 调用，那时 ctx 里**还没有**工程 id ——
	// core.WithBuildProjectID 是 Compile 内部才补上的（见 internal/builder/builder.go 的
	// buildCtx 构造），而 ResolverFor 早于它。所以这条路径走显式无作用域读，
	// 而不是把空串塞进 rls（那会被拒掉，发布直接报错）。
	//
	// 换非超级角色后的影响：这四处会 fail closed（0 行）⇒ 商品 / 分类等实体的
	// 字段源解析失败 ⇒ 发布产物里对应区块缺失。要修的是 presentation 侧在
	// renderHTML 的 buildCtx 上补 core.WithBuildProjectID(ctx, projectID) ——
	// presentation 不在本批的改动域内，见交付清单。
	switch entityType {
	case productcontract.EntityTypeCategory:
		var row *productmodel.ProductCategoryEntity
		if row, err = s.m.GetCategoryWithoutScope(ctx, entityID); err != nil {
			return nil, mapNotFound(err)
		}
		return &entityResolver{entityType: entityType, values: s.categoryValues(ctx, lang, row)}, nil
	case productcontract.EntityTypeBrand:
		var row *productmodel.ProductBrandEntity
		if row, err = s.m.GetBrandWithoutScope(ctx, entityID); err != nil {
			return nil, mapNotFound(err)
		}
		return &entityResolver{entityType: entityType, values: s.brandValues(ctx, lang, row)}, nil
	case productcontract.EntityTypeTag:
		var row *productmodel.ProductTagEntity
		if row, err = s.m.GetTagWithoutScope(ctx, entityID); err != nil {
			return nil, mapNotFound(err)
		}
		return &entityResolver{entityType: entityType, values: s.tagValues(ctx, lang, row)}, nil
	case productcontract.EntityTypeAttribute:
		var row *productmodel.ProductAttributeEntity
		if row, err = s.m.GetAttributeWithoutScope(ctx, entityID); err != nil {
			return nil, mapNotFound(err)
		}
		return &entityResolver{entityType: entityType, values: s.attributeValues(ctx, lang, row)}, nil
	}

	e, gerr := s.m.GetWithoutScope(ctx, entityID)
	if gerr != nil {
		return nil, mapNotFound(gerr)
	}
	variants, verr := s.m.ListVariants(ctx, e.ID)
	if verr != nil {
		return nil, verr
	}
	attrs, aerr := s.m.ListAttributesByIDs(ctx, decodeStrings(e.AttributeIDs))
	if aerr != nil {
		return nil, aerr
	}
	loc, lerr := s.localizeRelated(ctx, lang, e)
	if lerr != nil {
		return nil, lerr
	}
	values := productFieldValues(e, variants, attrs, loc)
	return &entityResolver{entityType: entityType, values: s.translateFields(ctx, lang, productcontract.EntityTypeProduct, values)}, nil
}

// mapNotFound gorm 未命中 → 模块统一的「不存在」错误（其余原样上抛）。
func mapNotFound(err error) error {
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return errors.New(productenums.ErrNotFound)
	}
	return err
}

// —— 各实体类型的取值 ——

// ResolveString 按字段白名单解析实体字段值。
//
// field 形如 "product.name"（entityType.field，与 heading 等组件的绑定格式一致）；
// 前缀类型不符或字段不在白名单内一律报错 —— 白名单是唯一来源，不做静默回退。
func (r *entityResolver) ResolveString(field string) (string, error) {
	entityType, name, ok := splitEntityField(field)
	if !ok {
		return "", fmt.Errorf("%s: %q（期望 entityType.field）", productenums.ErrInvalidField, field)
	}
	if entityType != r.entityType {
		return "", fmt.Errorf("绑定字段 %q 类型 %q 与当前实体 %q 不符", field, entityType, r.entityType)
	}
	if !productcontract.IsValidField(entityType, name) {
		return "", fmt.Errorf("%s: %q", productenums.ErrInvalidField, name)
	}
	return r.values[name], nil
}

// splitEntityField 拆 "entity.field"（两段，字段名不允许再带点）。
func splitEntityField(field string) (entityType, name string, ok bool) {
	parts := strings.SplitN(strings.TrimSpace(field), ".", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", false
	}
	if strings.Contains(parts[1], ".") {
		return "", "", false
	}
	return parts[0], parts[1], true
}

// —— 商品字段值 ——
