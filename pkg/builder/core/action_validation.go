package core

import (
	"errors"
	"fmt"
	"sort"
)

func ValidateNodeActions(node *Node, comp Component) error {
	provider, supported := comp.(ActionsProvider)
	if !supported {
		if len(node.Actions) > 0 {
			return fmt.Errorf("节点 %s: 组件未声明动作", node.ID)
		}
		return nil
	}
	declared := map[string]ActionSpec{}
	for _, spec := range provider.Actions() {
		declared[spec.Key] = spec
	}
	var incomplete error
	keys := make([]string, 0, len(node.Actions))
	for key := range node.Actions {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		spec, ok := declared[key]
		if !ok {
			return fmt.Errorf("节点 %s: 动作%s未声明", node.ID, key)
		}
		target := node.Actions[key]
		if err := ValidateActionTarget(target); err != nil {
			if !errors.Is(err, ErrIncompleteNode) {
				return fmt.Errorf("节点 %s 动作%s: %w", node.ID, key, err)
			}
			incomplete = err
		}
		allowed := false
		for _, kind := range spec.AllowedKinds {
			if target.Kind == kind {
				allowed = true
				break
			}
		}
		if !allowed {
			return fmt.Errorf("节点 %s: 动作%s不允许目标类型%s", node.ID, key, target.Kind)
		}
	}
	for _, key := range provider.RequiredActions() {
		if _, ok := node.Actions[key]; !ok {
			incomplete = fmt.Errorf("%w: 节点%s缺少动作%s", ErrIncompleteNode, node.ID, key)
		}
	}
	return incomplete
}
