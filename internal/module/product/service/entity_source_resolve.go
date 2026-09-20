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
	// 工程作用域（审计 DB-009 第四批收口）：**必须**从构建上下文取，取不到就显式报错。
	//
	// 这条链曾经是「显式例外」：本方法在 builder.Compile **之前**被 presentation 的
	// renderHTML 调用，而 core.WithBuildProjectID 原先只在 Compile 内部补，于是那时 ctx 里
	// 没有工程 id，只能走无作用域读。presentation 侧现在已在调用本方法之前补上
	// （presentation/service/presentation_render.go 的 buildCtx），例外条件不再成立 ——
	// 换非超级角色后继续裸读会 fail closed（0 行）⇒ 实体字段源解析失败 ⇒ 产物里对应区块
	// 静默缺失，所以这里改成带作用域读。
	//
	// 缺工程时**报错而不是退回裸读**：退回等于把「调用链漏了注入」伪装成「实体不存在」
	// （mapNotFound 会把它翻成 ErrNotFound），排查成本高得多。
	projectID := strings.TrimSpace(core.BuildProjectID(ctx))
	if projectID == "" {
		return nil, fmt.Errorf("%s: 构建上下文缺少工程 id（core.WithBuildProjectID），无法按工程隔离读取实体字段源",
			productenums.ErrMissingProjectContext)
	}
	switch entityType {
	case productcontract.EntityTypeCategory:
		var row *productmodel.ProductCategoryEntity
		if row, err = s.m.GetCategory(ctx, entityID, projectID); err != nil {
			return nil, mapNotFound(err)
		}
		return &entityResolver{entityType: entityType, values: s.categoryValues(ctx, lang, row)}, nil
	case productcontract.EntityTypeBrand:
		var row *productmodel.ProductBrandEntity
		if row, err = s.m.GetBrand(ctx, entityID, projectID); err != nil {
			return nil, mapNotFound(err)
		}
		return &entityResolver{entityType: entityType, values: s.brandValues(ctx, lang, row)}, nil
	case productcontract.EntityTypeTag:
		var row *productmodel.ProductTagEntity
		if row, err = s.m.GetTag(ctx, entityID, projectID); err != nil {
			return nil, mapNotFound(err)
		}
		return &entityResolver{entityType: entityType, values: s.tagValues(ctx, lang, row)}, nil
	case productcontract.EntityTypeAttribute:
		var row *productmodel.ProductAttributeEntity
		if row, err = s.m.GetAttribute(ctx, entityID, projectID); err != nil {
			return nil, mapNotFound(err)
		}
		return &entityResolver{entityType: entityType, values: s.attributeValues(ctx, lang, row)}, nil
	}

	e, gerr := s.m.Get(ctx, entityID, projectID)
	if gerr != nil {
		return nil, mapNotFound(gerr)
	}
	variants, verr := s.m.ListVariants(ctx, e.ID)
	if verr != nil {
		return nil, verr
	}
	// 读的是 e 自己引用的属性组：作用域用行自己的工程（与入参一致，这里显式用 e.ProjectID
	// 表达「读的是这一行的引用面」）。
	attrs, aerr := s.m.ListAttributesByIDs(ctx, decodeStrings(e.AttributeIDs), e.ProjectID)
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
