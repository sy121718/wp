package core

import (
	json "go_wp/pkg/jsonx"
	"sort"
	"strings"
)

type CollectionProvider interface {
	CollectionProp() string
}

const contentSourcePrefix = "content:"

func CollectionEntityType(source string) string {
	rest, ok := strings.CutPrefix(strings.TrimSpace(source), contentSourcePrefix)
	if !ok {
		return ""
	}
	return strings.TrimSpace(rest)
}

func CollectionSourceOf(n *Node, p CollectionProvider) string {
	if n == nil || p == nil {
		return ""
	}
	return collectionSourcePropValue(n.Props, p.CollectionProp())
}

func CollectionSourcesOf(roots []*Node) []string {
	if len(roots) == 0 {
		return nil
	}
	seen := map[string]bool{}
	var out []string
	var walk func(n *Node)
	walk = func(n *Node) {
		if n == nil {
			return
		}
		if c, ok := registry[n.Type]; ok {
			if p, ok := c.(CollectionProvider); ok {
				if src := CollectionSourceOf(n, p); src != "" && !seen[src] {
					seen[src] = true
					out = append(out, src)
				}
			}
		}
		for _, child := range n.Children {
			walk(child)
		}
	}
	for _, r := range roots {
		walk(r)
	}
	sort.Strings(out)
	return out
}

func collectionSourcePropValue(raw json.RawMessage, prop string) string {
	prop = strings.TrimSpace(prop)
	if len(raw) == 0 || prop == "" {
		return ""
	}
	var props map[string]any
	if err := json.Unmarshal(raw, &props); err != nil {
		return ""
	}
	s, ok := props[prop].(string)
	if !ok {
		return ""
	}
	return strings.TrimSpace(s)
}
