package jet

import (
	"strings"
)

// NavItem 是导航的原始事实：菜单树本身 + 当前请求路径。
//
// 它不携带 Active / Open / FirstURL 这类展示派生 —— 那些由模板函数按当前路径算，
// 模板决定摆在侧边还是顶部、如何分组与显隐。
type NavItem struct {
	ID       uint64
	Title    string
	TitleKey string
	Path     string
	Icon     string
	Children []NavItem
}

// navMatchKey 归一化路径：去掉查询串与末尾斜杠，保留大小写。
func navMatchKey(p string) string {
	p = strings.TrimSpace(p)
	if i := strings.IndexByte(p, '?'); i >= 0 {
		p = p[:i]
	}
	if len(p) > 1 {
		p = strings.TrimRight(p, "/")
	}
	return p
}

// NavActive 判断这一项本身是否是当前路径的归属菜单。
//
// 归属是**整棵树**里最长的段前缀匹配（不是只看本子树）：/admin/orders/overview
// 命中时，兄弟项 /admin/orders 不会被同时点亮。模板只需把根树传给 navOwner。
func NavActive(item NavItem, currentPath string) bool {
	return NavActiveIn(item, currentPath, nil)
}

// NavActiveIn 在给定根树范围内判断归属；roots 为空时只看本项子树。
func NavActiveIn(item NavItem, currentPath string, roots []NavItem) bool {
	current := navMatchKey(currentPath)
	if current == "" {
		return false
	}
	self := navMatchKey(item.Path)
	if self == "" || !isSegmentPrefix(self, current) {
		return false
	}
	scope := roots
	if len(scope) == 0 {
		scope = []NavItem{item}
	}
	owner := ""
	for _, r := range scope {
		if p := navOwnerKey(r, current); len(p) > len(owner) {
			owner = p
		}
	}
	return owner == self
}

// NavOwner 返回整棵树里当前路径的归属菜单路径（供模板先算一次再比较）。
func NavOwner(roots []NavItem, currentPath string) string {
	current := navMatchKey(currentPath)
	if current == "" {
		return ""
	}
	owner := ""
	for _, r := range roots {
		if p := navOwnerKey(r, current); len(p) > len(owner) {
			owner = p
		}
	}
	return owner
}

// NavIsOwner 判断该项路径是否就是给定 owner。
func NavIsOwner(item NavItem, owner string) bool {
	self := navMatchKey(item.Path)
	return self != "" && self == owner
}

// NavBranchActive 判断该项的子树里是否包含当前路径的归属（用于分组高亮）。
func NavBranchActive(item NavItem, owner string) bool {
	if owner == "" {
		return false
	}
	return navBranchContains(item, owner)
}

// NavOpen 判断目录节点是否应展开。
func NavOpen(item NavItem, currentPath string) bool {
	current := navMatchKey(currentPath)
	if current == "" {
		return false
	}
	owner := navOwnerKey(item, current)
	if owner == "" {
		return false
	}
	for _, child := range item.Children {
		if navBranchContains(child, owner) {
			return true
		}
	}
	return navMatchKey(item.Path) == owner && len(item.Children) > 0
}

// NavFirstURL 返回该分支里第一个可导航路径（目录按钮点击时用）。
func NavFirstURL(item NavItem) string {
	for _, child := range item.Children {
		if p := strings.TrimSpace(child.Path); p != "" {
			return p
		}
		if p := NavFirstURL(child); p != "" {
			return p
		}
	}
	return ""
}

// navOwnerKey 返回当前路径在本分支里匹配到的最长菜单路径。
func navOwnerKey(item NavItem, current string) string {
	owner := ""
	var walk func(NavItem)
	walk = func(n NavItem) {
		if p := navMatchKey(n.Path); p != "" && isSegmentPrefix(p, current) && len(p) > len(owner) {
			owner = p
		}
		for _, c := range n.Children {
			walk(c)
		}
	}
	walk(item)
	return owner
}

func navBranchContains(item NavItem, owner string) bool {
	if navMatchKey(item.Path) == owner {
		return true
	}
	for _, c := range item.Children {
		if navBranchContains(c, owner) {
			return true
		}
	}
	return false
}

func isSegmentPrefix(prefix, full string) bool {
	return full == prefix || strings.HasPrefix(full, prefix+"/")
}
