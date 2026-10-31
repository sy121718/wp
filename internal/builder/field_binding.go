package builder

import (
	"fmt"
	"strings"

	"go_wp/internal/builder/core"
)

func CollectFieldRefs(p *Page) (refs []core.FieldRef, err error) {
	if p == nil {
		return nil, nil
	}
	for _, n := range p.Root {
		sub, cerr := collectFieldRefsOfNode(n, "")
		if cerr != nil {
			return nil, cerr
		}
		refs = append(refs, sub...)
	}
	return refs, nil
}

func collectFieldRefsOfNode(n *core.Node, collectionSource string) (refs []core.FieldRef, err error) {
	if n == nil {
		return nil, nil
	}
	comp, lerr := core.Lookup(n.Type)
	if lerr != nil {

		return nil, nil
	}
	if provider, ok := comp.(core.CollectionProvider); ok {
		if src := core.CollectionSourceOf(n, provider); src != "" {
			collectionSource = src
		}
	}
	if provider, ok := comp.(core.FieldBindingProvider); ok {
		sub, perr := provider.FieldBindings(n)
		if perr != nil {
			return nil, fmt.Errorf("节点 %s: %w", n.ID, perr)
		}

		if collectionSource != "" {
			for i := range sub {
				if sub[i].CollectionSource == "" {
					sub[i].CollectionSource = collectionSource
				}
			}
		}
		refs = append(refs, sub...)
	}
	for _, c := range n.Children {
		sub, cerr := collectFieldRefsOfNode(c, collectionSource)
		if cerr != nil {
			return nil, cerr
		}
		refs = append(refs, sub...)
	}
	return refs, nil
}

func ValidateFieldRefs(p *Page, entityType string, reg core.EntitySourceRegistry) (err error) {
	refs, err := CollectFieldRefs(p)
	if err != nil {
		return err
	}
	if len(refs) == 0 {
		return nil
	}
	if reg == nil {
		return fmt.Errorf("实体类型注册表未装配，无法校验字段绑定")
	}
	entityType = strings.TrimSpace(entityType)
	for _, ref := range refs {
		if ref.EntityType == "" || ref.Field == "" {
			return fmt.Errorf("字段绑定不完整：%q.%q", ref.EntityType, ref.Field)
		}
		if !reg.IsValidType(ref.EntityType) {
			return fmt.Errorf("未知的实体类型 %q（字段绑定 %s.%s）", ref.EntityType, ref.EntityType, ref.Field)
		}

		target, fromCollection := entityType, false
		if src := core.CollectionEntityType(ref.CollectionSource); src != "" {
			target, fromCollection = src, true
		}
		if target != "" && ref.EntityType != target {
			if fromCollection {
				return fmt.Errorf("字段绑定 %s.%s 不属于集合源 %s 的数据源（跨数据源绑定被拒绝）",
					ref.EntityType, ref.Field, target)
			}
			return fmt.Errorf("字段绑定 %s.%s 不属于 %s 数据源（跨数据源绑定被拒绝）",
				ref.EntityType, ref.Field, target)
		}
		if !fieldAllowed(reg.FieldWhitelist(ref.EntityType), ref.Field) {
			return fmt.Errorf("字段 %s.%s 不在数据源字段白名单内", ref.EntityType, ref.Field)
		}
	}
	return nil
}

func fieldAllowed(whitelist []string, field string) bool {
	for _, f := range whitelist {
		if f == field {
			return true
		}
	}
	return false
}
