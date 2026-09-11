package productservice

// entity_source.go — 商品模块对实体类型注册表的适配 + 构建期商品字段解析（issue #6）。
//
// 本模块是自身实体类型字段白名单的唯一来源；装配期把 product 注册进注册表，
// 内容模板与发布实例据此校验类型与字段绑定，构建期由本文件的解析器把
// 「商品实体的数据」静态填入组件（发布产物零查库，不变量 1）。
//
// 语言（多语言）：解析器按构建语言（core.BuildLang）对「作者填写的文本」
// （name / subtitle / description）取内容译文，语境固定 product.<字段名>
// （docs/06-D §7.5）；未设置语言或未注入译文端口时一律回退原文，绝不报错。

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"go_wp/internal/builder/core"
	productcontract "go_wp/internal/module/product/contract"
	productenums "go_wp/internal/module/product/enums"
	productmodel "go_wp/internal/module/product/model"
	"go_wp/pkg/i18n"

	"gorm.io/gorm"
)

// entityFieldSource 商品实体类型的字段来源适配器。
type entityFieldSource struct {
	svc        *Service
	entityType string
}

// EntityType 实现 core.EntityFieldSource。
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
// 「商品类型非法」，比装配期直接报错更难排查（与 content 模块同一口径）。
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

// ResolverFor 返回绑定单个商品的字段解析器（构建期注入）。
//
// 一次性把白名单字段值算好（含价格区间等派生值）并挂上译文，构建期只读内存；
// 不存在的商品返回 ErrNotFound，类型不符返回 ErrInvalidType。
func (s *Service) ResolverFor(ctx context.Context, entityType, entityID string) (r core.ContentResolver, err error) {
	if !productcontract.IsValidType(entityType) {
		return nil, errors.New(productenums.ErrInvalidType)
	}
	e, gerr := s.m.Get(ctx, entityID)
	if gerr != nil {
		if errors.Is(gerr, gorm.ErrRecordNotFound) {
			return nil, errors.New(productenums.ErrNotFound)
		}
		return nil, gerr
	}
	variants, verr := s.m.ListVariants(ctx, e.ID)
	if verr != nil {
		return nil, verr
	}
	values := productFieldValues(e, variants)
	s.translateFields(ctx, values)
	return &productResolver{entityType: entityType, values: values}, nil
}

// translateFields 按构建语言把可翻译字段替换为译文（无译文保留原文）。
//
// 候选集固定为「本商品当前的可翻译字段原文」，一次批量取译文（§7.7 唯一查询形态），
// 渲染期零查库。端口未注入 / 语言为空 / 查询失败 → 全部保留原文。
func (s *Service) translateFields(ctx context.Context, values map[string]string) {
	lang := core.BuildLang(ctx)
	if lang == "" || s.contentStore == nil {
		return
	}
	fields := make([]string, 0, len(values))
	hashes := make([]string, 0, len(values))
	for _, f := range productcontract.FieldWhitelist(productcontract.EntityTypeProduct) {
		if !productcontract.IsTranslatableField(productcontract.EntityTypeProduct, f) {
			continue
		}
		if !i18n.ShouldTranslateContent(values[f]) {
			continue
		}
		fields = append(fields, f)
		hashes = append(hashes, i18n.ContentHash(values[f]))
	}
	if len(hashes) == 0 {
		return
	}
	tr := i18n.NewContentTranslatorWith(ctx, s.contentStore, lang, hashes)
	for _, f := range fields {
		values[f] = tr.TranslateContent(values[f], i18n.ContentContext(productcontract.EntityTypeProduct, f))
	}
}

// productResolver 绑定单个商品的字段解析器（值已预计算并翻译）。
type productResolver struct {
	entityType string
	values     map[string]string
}

// ResolveString 按字段白名单解析商品字段值。
//
// field 形如 "product.name"（entityType.field，与 heading 等组件的绑定格式一致）；
// 前缀类型不符或字段不在白名单内一律报错 —— 白名单是唯一来源，不做静默回退。
func (r *productResolver) ResolveString(field string) (string, error) {
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

// productFieldValues 计算白名单字段的展示值（纯函数，便于单测）。
//
// 价格全部落在变体上（商品主体不存价格，issue #5 已定语义），这里给出的是
// 由变体派生的只读值：price 取最低变体价，priceRange 在有多价时输出 "最低 ~ 最高"。
func productFieldValues(p *productmodel.ProductEntity, variants []*productmodel.VariantEntity) map[string]string {
	out := map[string]string{
		"name":         p.Name,
		"subtitle":     p.Subtitle,
		"description":  descriptionHTML(p.Description),
		"slug":         p.Slug,
		"unit":         p.Unit,
		"images":       imagesJSON(p),
		"defaultImage": p.DefaultImage,
	}
	if len(variants) > 0 {
		out["sku"] = variants[0].SKUCode
		minPrice, maxPrice := variants[0].Price, variants[0].Price
		var compare float64
		hasCompare := false
		for _, v := range variants {
			if v.Price < minPrice {
				minPrice = v.Price
			}
			if v.Price > maxPrice {
				maxPrice = v.Price
			}
			if v.ComparePrice != nil && (!hasCompare || *v.ComparePrice > compare) {
				compare, hasCompare = *v.ComparePrice, true
			}
		}
		out["price"] = formatPrice(minPrice)
		out["minPrice"] = formatPrice(minPrice)
		out["maxPrice"] = formatPrice(maxPrice)
		if minPrice == maxPrice {
			out["priceRange"] = formatPrice(minPrice)
		} else {
			out["priceRange"] = formatPrice(minPrice) + " ~ " + formatPrice(maxPrice)
		}
		if hasCompare {
			out["comparePrice"] = formatPrice(compare)
		}
	}
	return out
}

// formatPrice 数值 → 展示字符串：整数不带小数尾巴，其余按最短表示。
func formatPrice(v float64) string {
	return strconv.FormatFloat(v, 'f', -1, 64)
}

// imagesJSON 图集 JSON 数组字符串（core.gallery 等组件按 JSON 数组解析绑定值）；
// 商品未配图集但有主图时退化为单元素数组，避免详情页出现空图区。
func imagesJSON(p *productmodel.ProductEntity) string {
	urls := []string{}
	if len(p.Images) > 0 {
		_ = json.Unmarshal(p.Images, &urls)
	}
	if len(urls) == 0 && p.DefaultImage != "" {
		urls = []string{p.DefaultImage}
	}
	b, err := json.Marshal(urls)
	if err != nil {
		return "[]"
	}
	return string(b)
}

// descriptionHTML 商品描述（jsonb）→ HTML 片段。
//
// 约定两种形态：{"html": "..."}（富文本）与 JSON 字符串（纯文本）；
// 其余（{} 或未知结构）返回空串，由组件侧决定是否输出占位。
func descriptionHTML(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var obj struct {
		HTML string `json:"html"`
	}
	if err := json.Unmarshal(raw, &obj); err == nil && obj.HTML != "" {
		return obj.HTML
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return s
	}
	return ""
}

// 编译期断言：适配器实现 core.EntityFieldSource，解析器实现 core.ContentResolver。
var (
	_ core.EntityFieldSource = (*entityFieldSource)(nil)
	_ core.ContentResolver   = (*productResolver)(nil)
)
