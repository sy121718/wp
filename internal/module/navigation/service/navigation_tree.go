package navigationservice

// navigation_tree.go — 导航树装配与渲染（Tree / TreeByID / Render）。
// 扁平实体列表 → 父子树 → Jet 片段；构建期编译与管理页结构面板共用这棵树。

import (
	"context"
	"errors"
	"strings"
	"time"

	"gorm.io/gorm"

	"go_wp/internal/templates"

	navigationdto "go_wp/internal/module/navigation/dto"
	navigationenums "go_wp/internal/module/navigation/enums"
	navigationmodel "go_wp/internal/module/navigation/model"
)

// TreeByID 按具体菜单项 id 返回该菜单项及其子树（core.nav 按项引用时用）。
//
// 取数策略：先按 id 定位该项（带工程归属校验），再一次性取**同位置**的全部行拼子树 ——
// 菜单规模很小（几十行），一次 List 比按 parent_id 逐层查询少 N 次 SQL，而这条路径
// 在构建期会被每个引用该菜单的页面各走一次。
func (s *Service) TreeByID(ctx context.Context, projectID, navigationID string) (nodes []*navigationdto.NavigationNode, err error) {
	projectID = strings.TrimSpace(projectID)
	id := strings.TrimSpace(navigationID)
	if projectID == "" || id == "" {
		return nil, errors.New(navigationenums.ErrInvalidParam)
	}
	item, err := s.m.Get(ctx, projectID, id)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errors.New(navigationenums.ErrNotFound)
		}
		return nil, err
	}
	rows, err := s.m.List(ctx, projectID, item.Kind)
	if err != nil {
		return nil, err
	}
	children := make(map[string][]*navigationmodel.NavigationEntity)
	for _, r := range rows {
		if r.ParentID != nil && *r.ParentID != "" {
			children[*r.ParentID] = append(children[*r.ParentID], r)
		}
	}
	node := buildNode(item, children)
	if node == nil {
		return nil, errors.New(navigationenums.ErrNotFound)
	}
	nodes = []*navigationdto.NavigationNode{node}
	s.resolveSourceTitles(ctx, projectID, nodes)
	return nodes, nil
}

// Tree 按工程 + 位置返回导航项树（sort_order 升序，同序按 id 升序）。
// 构建期编译导航组件与管理页结构面板共用同一棵树。
func (s *Service) Tree(ctx context.Context, projectID, kind string) (nodes []*navigationdto.NavigationNode, err error) {
	projectID = strings.TrimSpace(projectID)
	kind = strings.TrimSpace(kind)
	if projectID == "" {
		return nil, errors.New(navigationenums.ErrInvalidParam)
	}
	if !isValidKind(kind) {
		return nil, errors.New(navigationenums.ErrInvalidKind)
	}
	rows, err := s.m.List(ctx, projectID, kind)
	if err != nil {
		return nil, err
	}
	nodes = buildTree(rows)
	s.resolveSourceTitles(ctx, projectID, nodes)
	return nodes, nil
}

// Render 渲染该工程该 kind 的导航 HTML 片段（Jet 模板渲染）。
// 根节点平铺 <a>，子节点按 parent_id 嵌套 <ul><li>；title/path 由 Jet 默认转义。
func (s *Service) Render(ctx context.Context, projectID, kind string) (htmlStr string, err error) {
	nodes, err := s.Tree(ctx, projectID, kind)
	if err != nil {
		return "", err
	}
	rootViews := make([]navNodeView, 0, len(nodes))
	for _, n := range nodes {
		rootViews = append(rootViews, toNavNodeView(n))
	}
	return templates.RenderFragment("navigation", struct {
		Roots []navNodeView
	}{Roots: rootViews})
}

// buildTree 由扁平实体列表组装树（父缺失的孤儿节点按根处理，避免丢项）。
func buildTree(rows []*navigationmodel.NavigationEntity) []*navigationdto.NavigationNode {
	children := make(map[string][]*navigationmodel.NavigationEntity)
	roots := make([]*navigationmodel.NavigationEntity, 0, len(rows))
	for _, r := range rows {
		if r.ParentID == nil || *r.ParentID == "" {
			roots = append(roots, r)
			continue
		}
		children[*r.ParentID] = append(children[*r.ParentID], r)
	}
	out := make([]*navigationdto.NavigationNode, 0, len(roots))
	for _, root := range roots {
		out = append(out, buildNode(root, children))
	}
	return out
}

// buildNode 把实体转成树节点（父 → 子递归）。
func buildNode(n *navigationmodel.NavigationEntity, children map[string][]*navigationmodel.NavigationEntity) *navigationdto.NavigationNode {
	node := &navigationdto.NavigationNode{
		ID: n.ID, Title: n.Title, Path: n.Path,
		SourceType: n.SourceType, SourceID: n.SourceID, Target: n.Target,
		SortOrder:    n.SortOrder,
		PanelBlockID: n.PanelBlockID, PanelWidth: n.PanelWidth,
		// 乐观锁 token：管理页用它原样回带（精确到微秒，展示串只到分钟，不能拿来比较）。
		UpdatedAt: n.UpdatedAt.Format(time.RFC3339Nano),
	}
	for _, k := range children[n.ID] {
		node.Children = append(node.Children, buildNode(k, children))
	}
	return node
}

// navNodeView 导航节点视图（Jet 模板渲染数据，树形）。
type navNodeView struct {
	Title    string
	Path     string
	Children []navNodeView
}

// toNavNodeView 树节点 → 模板视图。
func toNavNodeView(n *navigationdto.NavigationNode) navNodeView {
	v := navNodeView{Title: n.Title, Path: n.Path}
	for _, c := range n.Children {
		v.Children = append(v.Children, toNavNodeView(c))
	}
	return v
}
