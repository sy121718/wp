package contentservice

// entity_source.go — content 模块对实体类型注册表的适配。
//
// 本模块是自身实体类型字段白名单的唯一来源；装配期把它们注册进注册表，
// 构建层（内容模板 / 发布实例）据此校验与解析，不再直接依赖本模块的类型判断函数。

import (
	"context"
	"errors"

	"go_wp/internal/builder/core"
	contentcontract "go_wp/internal/module/content/contract"
)

// entityFieldSource 单个内容实体类型的字段来源适配器。
type entityFieldSource struct {
	svc        *Service
	entityType string
}

// EntityType 实现 core.EntityFieldSource。
func (e *entityFieldSource) EntityType() string { return e.entityType }

// FieldWhitelist 实现 core.EntityFieldSource（白名单仍取自本模块契约的唯一来源）。
func (e *entityFieldSource) FieldWhitelist() []string {
	return contentcontract.FieldWhitelist(e.entityType)
}

// ResolverFor 实现 core.EntityFieldSource。
func (e *entityFieldSource) ResolverFor(ctx context.Context, entityID string) (core.ContentResolver, error) {
	return e.svc.ResolverFor(ctx, e.entityType, entityID)
}

// RegisterEntityTypes 把本模块全部实体类型注册进注册表（装配期调用）。
//
// 注册表为 nil 视为装配缺陷（fail-closed）：静默跳过会让构建层到运行期
// 才发现「所有类型都非法」，比装配期直接报错更难排查。
func (s *Service) RegisterEntityTypes(reg core.EntitySourceRegistry) error {
	if reg == nil {
		return errors.New("实体类型注册表为空")
	}
	for _, t := range contentcontract.EntityTypes() {
		if err := reg.Register(&entityFieldSource{svc: s, entityType: t}); err != nil {
			return err
		}
	}
	return nil
}

// 编译期断言：适配器实现 core.EntityFieldSource。
var _ core.EntityFieldSource = (*entityFieldSource)(nil)
