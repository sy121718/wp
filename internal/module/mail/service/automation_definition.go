package mailservice

import (
	"sort"
	"strings"

	mailenums "go_wp/internal/module/mail/enums"
)

const automationSchemaV2 = 2

const automationSchemaV1 = 1

const (
	outletNext        = "next"
	outletYes         = "yes"
	outletNo          = "no"
	outletNotInList   = "not_in_list"
	outletActive      = "active"
	outletNonActive   = "non_active"
	outletABVariantID = "v"
)

var automationV1OnlyTypes = map[string]string{
	NodeTypeBranch: NodeTypeFilter,
}

func nodeOutlets(nodeType string) []string {
	if mapped, ok := automationV1OnlyTypes[nodeType]; ok {
		nodeType = mapped
	}
	if nodeType == NodeTypeABTest {
		return []string{}
	}
	return mailenums.AutomationNodeTypeOutlets(nodeType)
}

func normalizeDefinition(raw map[string]any) (map[string]any, int) {
	out, _ := normalizeDefinitionVersioned(raw)
	return out, automationSchemaV2
}

func normalizeDefinitionVersioned(raw map[string]any) (map[string]any, int) {
	if len(raw) == 0 {
		return map[string]any{}, automationSchemaV1
	}
	source := schemaOf(raw)
	dst := make(map[string]any, len(raw)+1)
	for k, v := range raw {
		dst[k] = deepCopyJSON(v)
	}
	dst["schema"] = automationSchemaV2

	if entry, ok := raw["entry"].(string); ok {
		dst["entry"] = entry
	}
	nodes, ok := raw["nodes"].([]any)
	if !ok {
		return dst, source
	}
	normalized := make([]any, 0, len(nodes))
	for _, item := range nodes {
		node, isMap := item.(map[string]any)
		if !isMap {

			normalized = append(normalized, deepCopyJSON(item))
			continue
		}
		normalized = append(normalized, normalizeNode(node))
	}
	dst["nodes"] = normalized
	return dst, source
}

func schemaOf(raw map[string]any) int {
	switch v := raw["schema"].(type) {
	case float64:
		return int(v)
	case int:
		return v
	case int64:
		return int(v)
	}
	return automationSchemaV1
}

func normalizeNode(node map[string]any) map[string]any {
	typ, _ := node["type"].(string)
	renamed := typ
	if v2Type, ok := automationV1OnlyTypes[typ]; ok {
		renamed = v2Type
	}
	out := make(map[string]any, 3)
	if existing, ok := node["out"].(map[string]any); ok {
		for k, v := range existing {
			if s, isStr := v.(string); isStr && strings.TrimSpace(s) != "" {
				out[k] = s
			}
		}
	}
	if len(out) == 0 {
		for _, name := range nodeOutlets(typ) {
			if v, ok := node[name].(string); ok && strings.TrimSpace(v) != "" {
				out[name] = v
			}
		}
	}
	dst := make(map[string]any, len(node)+3)
	for k, v := range node {
		switch k {
		case "next", "yes", "no", "out", "type":
			continue
		default:
			dst[k] = deepCopyJSON(v)
		}
	}
	dst["type"] = renamed
	dst["out"] = out

	if renamed == NodeTypeFilter {
		params, _ := dst["params"].(map[string]any)
		if params == nil {
			params = map[string]any{}
			dst["params"] = params
		}
		if mode, _ := params["mode"].(string); strings.TrimSpace(mode) == "" {
			params["mode"] = ConditionModeAll
		}
	}
	return dst
}

func deepCopyJSON(v any) any {
	switch t := v.(type) {
	case map[string]any:
		dst := make(map[string]any, len(t))
		for k, item := range t {
			dst[k] = deepCopyJSON(item)
		}
		return dst
	case []any:
		dst := make([]any, len(t))
		for i, item := range t {
			dst[i] = deepCopyJSON(item)
		}
		return dst
	default:
		return v
	}
}

func (n AutomationNode) outlet(name string) string {
	if n.Out != nil {
		if v, ok := n.Out[name]; ok {
			return v
		}
	}
	switch name {
	case outletNext:
		return n.Next
	case outletYes:
		return n.Yes
	case outletNo:
		return n.No
	}
	return ""
}

func (n AutomationNode) allOutlets() map[string]string {
	seen := make(map[string]string, len(n.Out)+3)
	add := func(name, target string) {
		if strings.TrimSpace(target) != "" {
			seen[name] = target
		}
	}
	if len(n.Out) > 0 {
		for name, target := range n.Out {
			add(name, target)
		}
	} else {

		add(outletNext, n.Next)
		add(outletYes, n.Yes)
		add(outletNo, n.No)
	}
	return seen
}

func (n AutomationNode) outletNames() []string {
	filled := n.allOutlets()
	names := make([]string, 0, len(filled))
	for name := range filled {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func displayOutlets(n AutomationNode) []string {
	if n.Type == NodeTypeABTest {
		return abVariantIDs(n.Params)
	}
	return nodeOutlets(n.Type)
}

func abVariantIDs(params map[string]any) []string {
	if params == nil {
		return nil
	}
	list, ok := params["variants"].([]any)
	if !ok {
		return nil
	}
	ids := make([]string, 0, len(list))
	for _, item := range list {
		v, isMap := item.(map[string]any)
		if !isMap {
			continue
		}
		if id, isStr := v["id"].(string); isStr && strings.TrimSpace(id) != "" {
			ids = append(ids, strings.TrimSpace(id))
		}
	}
	return ids
}

func unknownOutlets(n AutomationNode) []string {
	allowed := make(map[string]bool, 6)
	for _, name := range displayOutlets(n) {
		allowed[name] = true
	}
	var unknown []string
	for name := range n.allOutlets() {
		if !allowed[name] {
			unknown = append(unknown, name)
		}
	}
	sort.Strings(unknown)
	return unknown
}

func outletDisplayName(name string) string {
	if name == "" {
		return "(空)"
	}
	return name
}

func graphErrUnknownOutlet(key, name string) error {
	return graphErr(mailenums.DetailGraphUnknownOutlet,
		"节点 {node}: 未知出口名: {outlet}", "node", key, "outlet", outletDisplayName(name))
}
