package builder

import (
	"errors"
	"fmt"
	"sort"
	"strings"

	"go_wp/internal/builder/components/globalref"
	"go_wp/internal/builder/components/layoutslot"
	"go_wp/internal/builder/core"
)

type blockReadSnapshot struct {
	source core.BlockResolver
	roots  map[string][]*core.Node
	errors map[string]error
}

func (s *blockReadSnapshot) ResolveBlockRoot(id string) ([]*core.Node, error) {
	if roots, ok := s.roots[id]; ok {
		return roots, nil
	}
	if err, ok := s.errors[id]; ok {
		return nil, err
	}
	roots, err := s.source.ResolveBlockRoot(id)
	if err != nil {
		s.errors[id] = err
		return nil, err
	}
	roots = core.CloneNodes(roots, func(id string) string { return id })
	s.roots[id] = roots
	return roots, nil
}

func snapshotBlockReads(source core.BlockResolver) core.BlockResolver {
	if source == nil {
		return nil
	}
	return &blockReadSnapshot{source: source, roots: map[string][]*core.Node{}, errors: map[string]error{}}
}

type actionGraph struct {
	nodes     map[string]*core.Node
	ordered   []*core.Node
	resolved  map[string]map[string]core.ResolvedAction
	published core.ActionResolver
	locations map[string]core.ResolvedAction
}

func configureActions(roots []*core.Node, ctx *core.RenderContext, published ...core.ActionResolver) error {
	graph := &actionGraph{nodes: map[string]*core.Node{}, resolved: map[string]map[string]core.ResolvedAction{}, locations: map[string]core.ResolvedAction{}}
	if len(published) > 0 {
		graph.published = published[0]
	}
	if err := graph.collect(roots, ctx.Block, nil); err != nil {
		return err
	}
	for _, node := range graph.ordered {
		if strings.HasPrefix(node.Type, "plugin.") && len(node.Actions) == 0 {
			continue
		}
		comp, err := core.Lookup(node.Type)
		if err != nil {
			return err
		}
		if err := core.ValidateNodeActions(node, comp); err != nil {
			if !ctx.AllowIncomplete || !errors.Is(err, core.ErrIncompleteNode) {
				return err
			}
			markActionIncomplete(ctx, node.ID)
		}
		if len(node.Actions) == 0 {
			continue
		}
		provider := comp.(core.ActionsProvider)
		specs := map[string]core.ActionSpec{}
		for _, spec := range provider.Actions() {
			specs[spec.Key] = spec
		}
		keys := make([]string, 0, len(node.Actions))
		for key := range node.Actions {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		graph.resolved[node.ID] = map[string]core.ResolvedAction{}
		for _, key := range keys {
			action, err := graph.resolve(node, key, specs[key])
			if err != nil {
				if ctx.AllowIncomplete && errors.Is(err, core.ErrIncompleteNode) {
					markActionIncomplete(ctx, node.ID)
					continue
				}
				return fmt.Errorf("节点 %s 动作%s: %w", node.ID, key, err)
			}
			graph.resolved[node.ID][key] = action
		}
	}
	ctx.SetActionResolver(func(node *core.Node, key string) (core.ResolvedAction, error) {
		if node != nil {
			if action, ok := graph.resolved[node.ID][key]; ok {
				return action, nil
			}
		}
		return core.ResolvedAction{}, fmt.Errorf("%w: 动作%s没有有效目标", core.ErrIncompleteNode, key)
	})
	return nil
}

func markActionIncomplete(ctx *core.RenderContext, id string) {
	if ctx.IncompleteNodes == nil {
		ctx.IncompleteNodes = map[string]bool{}
	}
	ctx.IncompleteNodes[id] = true
}

func (g *actionGraph) collect(nodes []*core.Node, resolver core.BlockResolver, stack []string) error {
	for _, node := range nodes {
		if node == nil {
			return fmt.Errorf("动作索引遇到空节点")
		}
		if g.nodes[node.ID] != nil {
			return fmt.Errorf("节点 %s: 展开文档包含重复ID", node.ID)
		}
		g.nodes[node.ID] = node
		g.ordered = append(g.ordered, node)
		if node.Type == globalref.Type || node.Type == layoutslot.Type {
			id, err := globalref.BlockIDOf(node)
			if err != nil {
				return err
			}
			for _, ancestor := range stack {
				if id == ancestor {
					return fmt.Errorf("节点 %s: 全局块循环引用%s", node.ID, id)
				}
			}
			if len(stack) >= maxBlockExpandDepth {
				return fmt.Errorf("节点 %s: 全局块嵌套超过%d层", node.ID, maxBlockExpandDepth)
			}
			_, expanded, err := globalref.BuildView(node, resolver)
			if err != nil {
				return err
			}
			if err := g.collect(expanded, resolver, append(stack, id)); err != nil {
				return err
			}
		}
		if err := g.collect(node.Children, resolver, stack); err != nil {
			return err
		}
	}
	return nil
}

func (g *actionGraph) resolve(node *core.Node, key string, spec core.ActionSpec) (core.ResolvedAction, error) {
	target := node.Actions[key]
	if err := core.ValidateActionTarget(target); err != nil {
		return core.ResolvedAction{}, err
	}
	result := core.ResolvedAction{Kind: target.Kind}
	switch target.Kind {
	case core.ActionExternal:
		result.URL, result.NewTab = target.URL, target.NewTab
		return result, nil
	case core.ActionPage, core.ActionPresentation:
		locationKey := string(target.Kind) + ":" + target.PageID + target.InstanceID
		if location, ok := g.locations[locationKey]; ok {
			return location, nil
		}
		if g.published == nil {
			return result, fmt.Errorf("%w: 尚未取得当前工程和语言的已发布动作目标", core.ErrIncompleteNode)
		}
		resolved, err := g.published(node, key)
		if err != nil {
			return result, err
		}
		if resolved.Kind != target.Kind || resolved.InstanceID != "" || resolved.PartKey != "" || resolved.NewTab || !strings.HasPrefix(resolved.URL, "/") || strings.HasPrefix(resolved.URL, "//") {
			return result, fmt.Errorf("已发布动作目标解析结果不合法")
		}
		g.locations[locationKey] = resolved
		return resolved, nil
	case core.ActionLocal, core.ActionInstance:
		id := node.ID
		if target.Kind == core.ActionInstance {
			id = target.NodeID
		}
		destination := g.nodes[id]
		if destination == nil {
			return result, fmt.Errorf("%w: 目标节点%s不存在", core.ErrIncompleteNode, id)
		}
		comp, err := core.Lookup(destination.Type)
		if err != nil {
			return result, err
		}
		parts, ok := comp.(core.PartsProvider)
		if ok {
			for _, part := range parts.Parts() {
				if part.Key != target.PartKey {
					continue
				}
				if spec.TargetCapability != "" {
					matched := false
					for _, capability := range part.Capabilities {
						if capability == spec.TargetCapability {
							matched = true
							break
						}
					}
					if !matched {
						return result, fmt.Errorf("目标节点%s区域%s不具备能力%s", id, part.Key, spec.TargetCapability)
					}
				}
				result.InstanceID, result.PartKey = id, part.Key
				return result, nil
			}
		}
		return result, fmt.Errorf("%w: 目标节点%s未声明区域%s", core.ErrIncompleteNode, id, target.PartKey)
	}
	return result, fmt.Errorf("未知动作类型%s", target.Kind)
}
