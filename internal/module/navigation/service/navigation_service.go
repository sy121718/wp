// Package navigationservice 实现 navigation 模块业务用例（0-C）。
// navigation 表示公开站点导航，与后台权限菜单 menu 严格隔离。
package navigationservice

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"go_wp/internal/templates"
	"go_wp/pkg/logger"

	navigationcontract "go_wp/internal/module/navigation/contract"
	navigationdto "go_wp/internal/module/navigation/dto"
	navigationenums "go_wp/internal/module/navigation/enums"
	navigationmodel "go_wp/internal/module/navigation/model"
)

// 导航类型白名单（与迁移 046 的 CHECK 约束对齐）。
const (
	kindHeader = "header"
	kindFooter = "footer"
)

// 菜单项来源白名单（与迁移 054 的 CHECK 约束对齐）。
const (
	sourceCustom   = "custom"
	sourcePage     = "page"
	sourceArticle  = "article"
	sourceProduct  = "product"
	sourceCategory = "category"
	sourceBlock    = "block"
)

// 打开方式白名单（与迁移 054 的 CHECK 约束对齐）。
const (
	targetSelf  = "self"
	targetBlank = "blank"
)

// Service navigation 模块业务实现。
type Service struct {
	m *navigationmodel.Model
	// sources 来源实体解析器（装配层注入；未注入时来源项退化为记录自身 title/path）。
	sources navigationcontract.SourceResolver
}

// NewService 构造（model 注入，不持有 *gorm.DB）。
func NewService(m *navigationmodel.Model) *Service { return &Service{m: m} }

// SetSourceResolver 注入来源实体解析器（启动期装配调用一次，之后只读）。
func (s *Service) SetSourceResolver(r navigationcontract.SourceResolver) { s.sources = r }

// SourceGroups 返回该工程可加入菜单的来源候选（未注入解析器时为空）。
func (s *Service) SourceGroups(ctx context.Context, projectID string) (groups []navigationcontract.SourceGroup, err error) {
	if s.sources == nil {
		return nil, nil
	}
	return s.sources.Candidates(ctx, projectID)
}

// 编译期契约断言。
var _ navigationcontract.NavigationService = (*Service)(nil)

// Create 新建导航项：校验 kind/path 基础规则 + 同工程同 kind 同 path 唯一。
func (s *Service) Create(ctx context.Context, req *navigationdto.CreateReq) (res *navigationdto.NavigationResp, err error) {
	if req == nil {
		return nil, errors.New(navigationenums.ErrInvalidParam)
	}
	projectID := strings.TrimSpace(req.ProjectID)
	title := strings.TrimSpace(req.Title)
	path := strings.TrimSpace(req.Path)
	kind := strings.TrimSpace(req.Kind)
	if projectID == "" {
		return nil, errors.New(navigationenums.ErrInvalidParam)
	}
	if err = validateField(title, path, kind); err != nil {
		return nil, err
	}
	parentID := normalizeParentID(req.ParentID)
	// 父引用先校验：不存在的父 / 跨工程父 / 跨类型父都会让该节点在构建期树装配时
	// 成为孤儿（永远挂不上根，产物里静默消失）。
	if verr := s.validateParent(ctx, projectID, kind, "", parentID); verr != nil {
		return nil, verr
	}
	sourceType, sourceID, target, err := normalizeSource(req.SourceType, req.SourceID, req.Target)
	if err != nil {
		return nil, err
	}

	exists, err := s.m.ExistsPath(ctx, projectID, kind, path, "")
	if err != nil {
		return nil, err
	}
	if exists {
		return nil, errors.New(navigationenums.ErrPathTaken)
	}

	// 排序：未显式指定（0）时追加到同级末尾，保证同级 sort_order 唯一，
	// 管理页的「上移/下移」才能稳定交换。
	sortOrder := req.SortOrder
	if sortOrder == 0 {
		maxOrder, merr := s.m.MaxSortOrder(ctx, projectID, kind, parentID)
		if merr != nil {
			return nil, merr
		}
		sortOrder = maxOrder + 1
	}

	now := time.Now().UTC()
	e := &navigationmodel.NavigationEntity{
		ID: uuid.NewString(), ProjectID: projectID, Title: title, Path: path,
		Kind: kind, ParentID: parentID, SortOrder: sortOrder,
		SourceType: sourceType, SourceID: sourceID, Target: target,
		CreatedAt: now, UpdatedAt: now,
	}
	if err = s.m.Create(ctx, e); err != nil {
		return nil, err
	}
	return toResp(e), nil
}

// Update 更新导航项：仅更新传入的非空字段，变更 path/kind 时重校验唯一性。
func (s *Service) Update(ctx context.Context, req *navigationdto.UpdateReq) (res *navigationdto.NavigationResp, err error) {
	if req == nil || strings.TrimSpace(req.ID) == "" {
		return nil, errors.New(navigationenums.ErrInvalidParam)
	}
	// 定位这一跳没有工程可用（请求只给 id）：拿到实体后**全程带工程作用域**，
	// 写入与回读都受 RLS 约束（DB-009 第二批）。定位本身在换非超级角色后
	// 会 fail closed（0 行 → ErrNotFound），列入剩余清单。
	e, err := s.m.Get(ctx, "", strings.TrimSpace(req.ID))
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, errors.New(navigationenums.ErrNotFound)
	}
	if err != nil {
		return nil, err
	}

	updates := map[string]any{"update_time": time.Now().UTC()}
	title, path, kind := e.Title, e.Path, e.Kind

	if req.Title != nil {
		title = strings.TrimSpace(*req.Title)
		updates["title"] = title
	}
	if req.Path != nil {
		path = strings.TrimSpace(*req.Path)
		updates["path"] = path
	}
	if req.Kind != nil {
		kind = strings.TrimSpace(*req.Kind)
		updates["kind"] = kind
	}
	if err = validateField(title, path, kind); err != nil {
		return nil, err
	}
	if req.ParentID != nil {
		newParent := normalizeParentID(req.ParentID)
		// 自引用、或挂到自己的后代下都会成环：构建期树装配永远到不了根节点，
		// 整棵子树从产物里静默消失（后台列表仍显示正常）。
		if verr := s.validateParent(ctx, e.ProjectID, kind, e.ID, newParent); verr != nil {
			return nil, verr
		}
		updates["parent_id"] = newParent
	}
	if req.SortOrder != nil {
		updates["sort_order"] = *req.SortOrder
	}
	// 来源与打开方式：三字段联动校验（来源切到非 custom 时必须同时给出来源实体）。
	if req.SourceType != nil || req.SourceID != nil || req.Target != nil {
		sourceType := e.SourceType
		if req.SourceType != nil {
			sourceType = *req.SourceType
		}
		sourceID := e.SourceID
		if req.SourceID != nil {
			sourceID = normalizeSourceID(req.SourceID)
		}
		target := e.Target
		if req.Target != nil {
			target = *req.Target
		}
		st, sid, tg, serr := normalizeSource(sourceType, sourceID, target)
		if serr != nil {
			return nil, serr
		}
		updates["source_type"], updates["source_id"], updates["target"] = st, sid, tg
	}

	// path/kind 任一变化时重校验同工程同 kind 同 path 唯一（排除自身）。
	if req.Path != nil || req.Kind != nil {
		exists, err := s.m.ExistsPath(ctx, e.ProjectID, kind, path, e.ID)
		if err != nil {
			return nil, err
		}
		if exists {
			return nil, errors.New(navigationenums.ErrPathTaken)
		}
	}

	if err = s.m.Save(ctx, e.ProjectID, e.ID, updates); err != nil {
		return nil, err
	}
	updated, err := s.m.Get(ctx, e.ProjectID, e.ID)
	if err != nil {
		return nil, err
	}
	return toResp(updated), nil
}

// Get 按 ID 查询导航项。
func (s *Service) Get(ctx context.Context, req *navigationdto.GetReq) (res *navigationdto.NavigationResp, err error) {
	if req == nil || strings.TrimSpace(req.ID) == "" {
		return nil, errors.New(navigationenums.ErrInvalidParam)
	}
	// 导航项详情只有 id 可依（请求不带工程）：按「不限工程」形态定位。
	// 换非超级角色后这条路径会 fail closed（0 行 → ErrNotFound），列入剩余清单 ——
	// 修复它需要调用方带工程（后台导航页已持有选中工程，属 dashboard，本批不动）。
	e, err := s.m.Get(ctx, "", strings.TrimSpace(req.ID))
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, errors.New(navigationenums.ErrNotFound)
	}
	if err != nil {
		return nil, err
	}
	return toResp(e), nil
}

// List 按工程（可选 kind）列出导航项，sort_order 升序。
func (s *Service) List(ctx context.Context, req *navigationdto.ListReq) (list []*navigationdto.NavigationResp, err error) {
	if req == nil || strings.TrimSpace(req.ProjectID) == "" {
		return nil, errors.New(navigationenums.ErrInvalidParam)
	}
	kind := strings.TrimSpace(req.Kind)
	if kind != "" && !isValidKind(kind) {
		return nil, errors.New(navigationenums.ErrInvalidKind)
	}
	rows, err := s.m.List(ctx, strings.TrimSpace(req.ProjectID), kind)
	if err != nil {
		return nil, err
	}
	out := make([]*navigationdto.NavigationResp, 0, len(rows))
	for _, r := range rows {
		out = append(out, toResp(r))
	}
	return out, nil
}

// Delete 删除导航项及其全部子项（导航树是一个聚合：留下孤儿节点会被渲染成顶级项）。
func (s *Service) Delete(ctx context.Context, req *navigationdto.DeleteReq) (err error) {
	if req == nil || strings.TrimSpace(req.ID) == "" {
		return errors.New(navigationenums.ErrInvalidParam)
	}
	id := strings.TrimSpace(req.ID)
	// 定位这一跳没有工程可用（请求只给 id）；拿到实体后的磁盘动作全部带工程作用域：
	// 列表与批量删除都在本工程内，删到别的工程的行在换角色后会被策略拒绝（DB-009 第二批）。
	e, err := s.m.Get(ctx, "", id)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return errors.New(navigationenums.ErrNotFound)
	}
	if err != nil {
		return err
	}
	rows, err := s.m.List(ctx, e.ProjectID, e.Kind)
	if err != nil {
		return err
	}
	return s.m.DeleteMany(ctx, e.ProjectID, navDescendantIDs(rows, id))
}

// navDescendantIDs 返回自身 + 全部子孙 ID（深度优先）。
func navDescendantIDs(rows []*navigationmodel.NavigationEntity, rootID string) []string {
	children := make(map[string][]string, len(rows))
	for _, r := range rows {
		if r.ParentID != nil && *r.ParentID != "" {
			children[*r.ParentID] = append(children[*r.ParentID], r.ID)
		}
	}
	out := make([]string, 0, 4)
	stack := []string{rootID}
	for len(stack) > 0 {
		cur := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		out = append(out, cur)
		stack = append(stack, children[cur]...)
	}
	return out
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

// resolveSourceTitles 递归把来源实体的标题/URL 写回菜单树。
// 解析失败保留记录自身值（构建期不因单个来源实体缺失而整页失败）。
func (s *Service) resolveSourceTitles(ctx context.Context, projectID string, nodes []*navigationdto.NavigationNode) {
	if s.sources == nil {
		return
	}
	for _, n := range nodes {
		if n.SourceType != sourceCustom && n.SourceID != nil && *n.SourceID != "" {
			title, url, err := s.sources.ResolveSource(ctx, projectID, n.SourceType, *n.SourceID)
			if err != nil {
				logger.Scene("navigation").
					With("sourceType", n.SourceType).With("sourceId", *n.SourceID).
					Warn("导航来源实体解析失败，回退记录自身标题/链接")
			} else {
				if title != "" {
					n.Title = title
				}
				if url != "" {
					n.Path = url
				}
			}
		}
		s.resolveSourceTitles(ctx, projectID, n.Children)
	}
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
		SortOrder: n.SortOrder,
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

// validateField 校验 title/path/kind 基础规则（不含唯一性）。
func validateField(title, path, kind string) error {
	if title == "" {
		return errors.New(navigationenums.ErrInvalidParam)
	}
	if !strings.HasPrefix(path, "/") {
		return errors.New(navigationenums.ErrInvalidParam)
	}
	if !isValidKind(kind) {
		return errors.New(navigationenums.ErrInvalidKind)
	}
	return nil
}

// isValidKind 判断导航类型是否为 header/footer。
func isValidKind(kind string) bool {
	return kind == kindHeader || kind == kindFooter
}

// maxParentDepth 父链上溯深度上限：兜底历史脏数据形成的环（正常菜单不超过 3~4 层）。
const maxParentDepth = 64

// validateParent 校验父引用合法：自引用、成环、跨工程、跨类型、父项不存在一律拒绝。
//
// 此前 parent_id 直接落库、零校验：一旦把节点挂到自己或自己的后代下就形成环，
// 构建期树装配从根节点出发递归，环上的节点永远到不了根 —— 整棵子树在产物里
// 静默消失（后台列表照常显示），排查成本极高。
func (s *Service) validateParent(ctx context.Context, projectID, kind, selfID string, parentID *string) error {
	if parentID == nil {
		return nil
	}
	cur := strings.TrimSpace(*parentID)
	if cur == "" {
		return nil
	}
	if selfID != "" && cur == selfID {
		return errors.New(navigationenums.ErrInvalidParent)
	}
	for depth := 0; depth < maxParentDepth; depth++ {
		// 父链上溯带工程作用域：跨工程父引用本来就要拒绝，作用域让它同时受策略约束。
		parent, err := s.m.Get(ctx, projectID, cur)
		if err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return errors.New(navigationenums.ErrInvalidParent)
			}
			return err
		}
		if parent.ProjectID != projectID {
			return errors.New(navigationenums.ErrInvalidParent)
		}
		if kind != "" && parent.Kind != kind {
			return errors.New(navigationenums.ErrInvalidParent)
		}
		if parent.ParentID == nil || strings.TrimSpace(*parent.ParentID) == "" {
			return nil // 到达根节点，链路合法
		}
		next := strings.TrimSpace(*parent.ParentID)
		if selfID != "" && next == selfID {
			return errors.New(navigationenums.ErrInvalidParent)
		}
		cur = next
	}
	return errors.New(navigationenums.ErrInvalidParent)
}

// normalizeParentID 把空字符串父 ID 规范为 nil（表示根导航项）。
func normalizeParentID(p *string) *string {
	if p == nil {
		return nil
	}
	if strings.TrimSpace(*p) == "" {
		return nil
	}
	v := strings.TrimSpace(*p)
	return &v
}

// normalizeSource 规范化并校验菜单项来源三件套（sourceType/sourceID/target）。
// 规则：空值按 custom/self 处理；custom 来源忽略来源实体；非 custom 必须给出来源实体。
func normalizeSource(sourceType string, sourceID *string, target string) (st string, sid *string, tg string, err error) {
	st = normalizeSourceType(sourceType)
	if !isValidSourceType(st) {
		return "", nil, "", errors.New(navigationenums.ErrInvalidSource)
	}
	tg = normalizeTarget(target)
	if !isValidTarget(tg) {
		return "", nil, "", errors.New(navigationenums.ErrInvalidTarget)
	}
	sid = normalizeSourceID(sourceID)
	if st == sourceCustom {
		return st, nil, tg, nil
	}
	if sid == nil {
		return "", nil, "", errors.New(navigationenums.ErrInvalidSource)
	}
	return st, sid, tg, nil
}

// isValidSourceType 判断菜单项来源是否在白名单内。
func isValidSourceType(v string) bool {
	switch v {
	case sourceCustom, sourcePage, sourceArticle, sourceProduct, sourceCategory, sourceBlock:
		return true
	}
	return false
}

// isValidTarget 判断打开方式是否在白名单内。
func isValidTarget(v string) bool { return v == targetSelf || v == targetBlank }

// normalizeSourceType 空值规范为 custom。
func normalizeSourceType(v string) string {
	if v = strings.TrimSpace(v); v == "" {
		return sourceCustom
	}
	return v
}

// normalizeTarget 空值规范为 self。
func normalizeTarget(v string) string {
	if v = strings.TrimSpace(v); v == "" {
		return targetSelf
	}
	return v
}

// normalizeSourceID 空字符串来源实体规范为 nil。
func normalizeSourceID(p *string) *string {
	if p == nil {
		return nil
	}
	v := strings.TrimSpace(*p)
	if v == "" {
		return nil
	}
	return &v
}

// toResp 实体 → 响应。
func toResp(e *navigationmodel.NavigationEntity) *navigationdto.NavigationResp {
	return &navigationdto.NavigationResp{
		ID: e.ID, ProjectID: e.ProjectID, Title: e.Title, Path: e.Path,
		Kind: e.Kind, ParentID: e.ParentID, SortOrder: e.SortOrder,
		SourceType: e.SourceType, SourceID: e.SourceID, Target: e.Target,
		UpdatedAt: e.UpdatedAt.Format("2006-01-02 15:04"),
	}
}
