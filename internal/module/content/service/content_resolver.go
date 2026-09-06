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
	contentmodel "go_wp/internal/module/content/model"

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
	return &entityResolver{entityType: entityType, data: data}, nil
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

// 引用 contentmodel 避免未用（Entity 类型经 s.m.Get 返回，隐式使用）。
var _ = contentmodel.Entity{}
