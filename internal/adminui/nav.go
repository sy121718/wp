package adminui

import (
	"strings"

	"go_wp/internal/builder/core"

	admindto "go_wp/internal/module/admin/dto"
	"go_wp/pkg/pathkit"
)

type NavNode struct {
	Title    string
	Path     string
	Children []NavNode
	Active   bool
	Open     bool
}

type NavGroup struct {
	ID    uint64
	Title string

	Icon     string
	IconSVG  string
	Path     string
	Nodes    []NavNode
	Active   bool
	FirstURL string
}

func BuildNav(nodes []admindto.MenuTreeNode, currentPath string, t func(key, fallback string) string) []NavGroup {
	current := pathkit.MatchKey(currentPath)
	owner := navOwnerPath(nodes, current)
	out := make([]NavGroup, 0, len(nodes))
	for _, n := range nodes {
		if n.IsHidden == 1 {
			continue
		}
		iconSVG, _ := core.IconSVG(n.Icon)
		g := NavGroup{
			ID:      n.ID,
			Title:   t(n.TitleKey, n.Title),
			Icon:    n.Icon,
			IconSVG: iconSVG,
			Path:    n.Path,
		}
		g.Nodes = buildNavNodes(n.Children, owner, t)
		if len(g.Nodes) > 0 {

			g.Active = containsActive(g.Nodes)
			g.FirstURL = firstNavPath(g.Nodes)
		} else {
			g.Active = n.Path != "" && pathkit.MatchKey(n.Path) == owner
		}

		if len(g.Nodes) == 0 && g.Path == "" {
			continue
		}
		out = append(out, g)
	}
	return out
}

func buildNavNodes(nodes []admindto.MenuTreeNode, owner string, t func(key, fallback string) string) []NavNode {
	out := make([]NavNode, 0, len(nodes))
	for _, n := range nodes {
		if n.IsHidden == 1 {
			continue
		}
		nn := NavNode{
			Title: t(n.TitleKey, n.Title),
			Path:  n.Path,
		}
		nn.Children = buildNavNodes(n.Children, owner, t)
		nn.Active = nn.Path != "" && pathkit.MatchKey(nn.Path) == owner

		if nn.Path == "" && len(nn.Children) == 0 {
			continue
		}
		nn.Open = nn.Active || containsActive(nn.Children)
		out = append(out, nn)
	}
	return out
}

func firstNavPath(nodes []NavNode) string {
	for _, n := range nodes {
		if n.Path != "" {
			return n.Path
		}
		if p := firstNavPath(n.Children); p != "" {
			return p
		}
	}
	return ""
}

func containsActive(nodes []NavNode) bool {
	for _, n := range nodes {
		if n.Active || containsActive(n.Children) {
			return true
		}
	}
	return false
}

func navOwnerPath(nodes []admindto.MenuTreeNode, current string) string {
	owner := ""
	var walk func([]admindto.MenuTreeNode)
	walk = func(list []admindto.MenuTreeNode) {
		for _, n := range list {
			if n.IsHidden != 1 {
				if p := pathkit.MatchKey(n.Path); p != "" && isSegmentPrefix(p, current) && len(p) > len(owner) {
					owner = p
				}
			}
			walk(n.Children)
		}
	}
	walk(nodes)
	return owner
}

func isSegmentPrefix(prefix, full string) bool {
	return full == prefix || strings.HasPrefix(full, prefix+"/")
}
