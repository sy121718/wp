package contentservice

// content_resolver.go — core.ContentResolver 实现（0-A2 构建期内容解析）。
// 绑定单个内容实体：ResolveString(field) 按字段白名单解析实体的 data 字段
// 为字符串字面量（heading 等组件的 Binding 在构建期静态填入）。

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"go_wp/internal/builder/core"
	contentcontract "go_wp/internal/module/content/contract"
	contentenums "go_wp/internal/module/content/enums"

	"gorm.io/gorm"
)

// ResolverFor 返回绑定单个实体的内容解析器。
// 构建期注入：presentation 模块派生 DocumentSnapshot 时按 entityType+entityID
// 取实体，把 Binding 字段解析为字面量（文档已解析，后续编译零依赖实体）。
func (s *Service) ResolverFor(ctx context.Context, entityType, entityID string) (r core.ContentResolver, err error) {
	if !contentcontract.IsValidType(entityType) {
		return nil, errors.New(contentenums.ErrInvalidType)
	}
	e, gerr := s.m.Get(ctx, entityID)
	if gerr != nil {
		if errors.Is(gerr, gorm.ErrRecordNotFound) {
			return nil, errors.New(contentenums.ErrNotFound)
		}
		return nil, gerr
	}
	if e.EntityType != entityType {
		return nil, fmt.Errorf("实体 %s 类型 %s 与请求 %s 不符", entityID, e.EntityType, entityType)
	}
	var data map[string]any
	if err = json.Unmarshal(e.Data, &data); err != nil {
		return nil, fmt.Errorf("%s: %w", contentenums.ErrDataInvalid, err)
	}
	// 多语言（审计 I18N-006）：字段值按当前构建语言取译文。
	//
	// 语言从 ctx 取（core.BuildLang）—— 与商品域的实体解析同一约定：
	// 签名里再传一个 lang 就有「两处可能不一致」的空间，而它只在英文站点上才暴露。
	// 没有译文时逐字节回退原文（TranslateContent 的语义），英文站点缺译文是常态。
	lang := core.BuildLang(ctx)
	data = s.translateData(ctx, lang, entityType, data)
	mergeSEOFields(entityType, data)
	return &entityResolver{entityType: entityType, data: data}, nil
}

// mergeSEOFields 把已合并的 SEO 字段指向正文字段（2026-09-30）。
//
// 文章标题即 <title>、摘要即 meta description，编辑页不再有单独的 SEO 输入框。
// 归一放在**翻译之后**：seoTitle / seoDescription 直接取已翻译的 title / excerpt，
// 历史数据里遗留的旧 SEO 值因此也漏不进发布产物（否则一篇没重新保存过的老文章，
// 线上 <title> 与编辑页看到的标题会不是同一个）。字段白名单仍保留这两个字段
// （旧文档里的 Binding 可能绑着它们），所以这里是「值从哪来」的唯一收口。
func mergeSEOFields(entityType string, data map[string]any) {
	if entityType != "article" || data == nil {
		return
	}
	if v, ok := data["title"]; ok {
		data["seoTitle"] = v
	}
	if v, ok := data["excerpt"]; ok {
		data["seoDescription"] = v
	}
}

// entityResolver 绑定单实体的字段解析器。
type entityResolver struct {
	entityType string
	data       map[string]any
}

// ResolveString 按字段白名单解析字段值为字符串（不存在返回空串）。
// field 形如 "product.name"（entityType.field，与 heading 组件 fieldPathRe
// 的两段格式一致）；拆前缀校验类型匹配 + 字段名白名单（不变量 4）。
func (r *entityResolver) ResolveString(field string) (string, error) {
	parts := strings.SplitN(field, ".", 2)
	if len(parts) != 2 {
		return "", fmt.Errorf("%s: %q（期望 entityType.field）", contentenums.ErrInvalidField, field)
	}
	entityType, fieldName := parts[0], parts[1]
	if entityType != r.entityType {
		return "", fmt.Errorf("绑定字段 %q 类型 %q 与当前实体 %q 不符", field, entityType, r.entityType)
	}
	if !contentcontract.IsValidField(entityType, fieldName) {
		return "", fmt.Errorf("%s: %q", contentenums.ErrInvalidField, fieldName)
	}
	v, ok := r.data[fieldName]
	if !ok || v == nil {
		return "", nil
	}
	return scalarString(v), nil
}

// scalarString 字段值归一为字符串：string 原样；数值转字符串；
// 数组取首元素（如 images 的封面）；其他 JSON 序列化。
func scalarString(v any) string {
	switch x := v.(type) {
	case string:
		return x
	case float64:
		if x == float64(int64(x)) {
			return strconv.FormatInt(int64(x), 10)
		}
		return strconv.FormatFloat(x, 'g', -1, 64)
	case bool:
		return strconv.FormatBool(x)
	case []any:
		if len(x) > 0 {
			return scalarString(x[0])
		}
		return ""
	default:
		b, err := json.Marshal(v)
		if err != nil {
			return ""
		}
		return string(b)
	}
}

// 编译期断言：entityResolver 实现 core.ContentResolver。
var _ core.ContentResolver = (*entityResolver)(nil)
