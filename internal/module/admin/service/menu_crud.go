package adminservice

import (
	"context"
	"errors"
	"sort"
	"strings"

	admindto "go_wp/internal/module/admin/dto"
	adminenums "go_wp/internal/module/admin/enums"
	adminmodel "go_wp/internal/module/admin/model"
)

// MenuPage 菜单管理列表（**树状分页**）。
//
// 分页单位是顶级菜单，不是行：
//   - 浏览态：读一页顶级节点 + 它们的完整子树（model.ListMenuRootsPage），
//     列表默认只显示最上级（子行折叠，见 flattenMenuPageRows）；
//   - 搜索态：读「命中项 + 各自到根的祖先路径」（model.ListMenuSearchForest），
//     在内存里按顶级节点分页 —— 一条命中必须连着它的上级路径显示，
//     按命中分页会让同一棵树在多页里重复出现。
//
// 为什么不逐层懒加载：菜单一百多条、树深 3 级，整棵读完的代价远小于为每层各留一套
// 分页与展开态（YAGNI）。真正决定「看得见多少」的是折叠，不是取数。
func (s *Service) MenuPage(ctx context.Context, page, limit int, keyword string) (*admindto.MenuPageResp, error) {
	parents, err := s.mm.ListParentOptions(ctx)
	if err != nil {
		return nil, err
	}
	resp := &admindto.MenuPageResp{
		Rows:    []admindto.MenuPageRow{},
		Parents: buildMenuParentChoices(parents, nil),
	}
	if keyword == "" {
		rows, total, err := s.mm.ListMenuRootsPage(ctx, page, limit)
		if err != nil {
			return nil, err
		}
		resp.Total = total
		resp.Rows = flattenMenuPageRows(buildMenuPageForest(rows, nil))
		return resp, nil
	}
	rows, err := s.mm.ListMenuSearchForest(ctx, keyword)
	if err != nil {
		return nil, err
	}
	matched := make(map[uint64]bool, len(rows))
	for _, r := range rows {
		if r.Matched {
			matched[r.ID] = true
		}
	}
	roots := buildMenuPageForest(rows, matched)
	resp.Total = int64(len(roots))
	resp.Rows = flattenMenuPageRows(paginateMenuRoots(roots, page, limit))
	return resp, nil
}

// buildMenuParentChoices 上级菜单下拉候选：按父链深度算缩进，让下拉里看得出层级。
//
// 保留全量与既有语义（不分页）：缺项会直接表现为「建子菜单时选不到父级」。
// disabled 里的 ID 标成不可选（新建态传 nil）；判定用 map 而不是切片，
// 因为候选是逐条判的、按 ID 查是 O(1)，且这里没有「先后顺序」的语义。
func buildMenuParentChoices(parents []adminmodel.MenuParentOption, disabled map[uint64]bool) []admindto.MenuParentChoice {
	byID := make(map[uint64]adminmodel.MenuParentOption, len(parents))
	for _, p := range parents {
		byID[p.ID] = p
	}
	out := make([]admindto.MenuParentChoice, 0, len(parents))
	for _, p := range parents {
		depth := 0
		seen := map[uint64]bool{p.ID: true}
		// 深度上限与树展开同一口径（防环数据把这里变成死循环）。
		for id := p.ParentID; id != 0 && depth < menuParentMaxDepth; {
			parent, ok := byID[id]
			if !ok || seen[id] {
				break
			}
			seen[id] = true
			depth++
			id = parent.ParentID
		}
		out = append(out, admindto.MenuParentChoice{
			ID: p.ID, Title: p.Title, Type: p.Type, Indent: strings.Repeat("　", depth),
			Disabled: disabled[p.ID],
		})
	}
	return out
}

// MenuParentOptions 是编辑抽屉的上级菜单候选：全量候选，其中「自己 + 自己的子孙」标不可选。
//
// 为什么这段判定不交给模板或 UI 的 JS 去算：成环与否是**数据关系**，
// 前端手里的候选行里没有可靠的 parent 链（缩进只是显示用的空格），
// 由后端给出唯一答案，UI 才能只负责渲染。
//
// excludeID 为 0（或该菜单已不存在）时与新建态等价：没有任何不可选项。
func (s *Service) MenuParentOptions(ctx context.Context, excludeID uint64) ([]admindto.MenuParentChoice, error) {
	parents, err := s.mm.ListParentOptions(ctx)
	if err != nil {
		return nil, err
	}
	disabled, err := s.mm.ListSubtreeIDs(ctx, excludeID)
	if err != nil {
		return nil, err
	}
	return buildMenuParentChoices(parents, disabled), nil
}

// menuParentMaxDepth 上级菜单下拉算缩进时的向上追溯上限（与 model 的展开上限同量级）。
const menuParentMaxDepth = 8

// menuPageNode 是建树过程中的临时节点，只在本文件内流转。
type menuPageNode struct {
	row      adminmodel.MenuPageRow
	children []*menuPageNode
	// matched：本节点自身命中；subtreeMatched：以它为根的子树里有命中（含自身）。
	// 搜索态下 subtreeMatched 决定节点初始展开 —— 否则命中行会被折叠的祖先挡住，
	// 用户搜到了东西却什么都看不见。
	matched        bool
	subtreeMatched bool
}

// buildMenuPageForest 把平铺的行集合组装成森林（父不在集合里的行按根处理）。
//
// 排序在这里做而不是在 SQL 里：逐层读回来的集合顺序是「根 → 第一层 → 第二层」的
// BFS 序，同层兄弟必须按 sort_order / id 定序，才对得上用户在表单里看到的顺序。
func buildMenuPageForest(rows []adminmodel.MenuPageRow, matched map[uint64]bool) []*menuPageNode {
	sorted := make([]adminmodel.MenuPageRow, len(rows))
	copy(sorted, rows)
	sort.SliceStable(sorted, func(i, j int) bool {
		if sorted[i].SortOrder != sorted[j].SortOrder {
			return sorted[i].SortOrder < sorted[j].SortOrder
		}
		return sorted[i].ID < sorted[j].ID
	})
	nodes := make(map[uint64]*menuPageNode, len(sorted))
	for _, r := range sorted {
		nodes[r.ID] = &menuPageNode{row: r, matched: matched != nil && matched[r.ID]}
	}
	roots := make([]*menuPageNode, 0, len(sorted))
	for _, r := range sorted {
		node := nodes[r.ID]
		// ParentID 为 0、父不在集合里、指向自身、或父子成环（坏数据）：
		// 一律按根处理。这几种情况下「挂到父上」都会让行从页面上消失 ——
		// 坏数据最不该表现成「列表里少了东西」（那看起来像数据被删了）。
		if r.ParentID != 0 && !menuPageParentCyclic(nodes, r) {
			if parent, ok := nodes[r.ParentID]; ok {
				parent.children = append(parent.children, node)
				continue
			}
		}
		roots = append(roots, node)
	}
	var mark func(n *menuPageNode) bool
	mark = func(n *menuPageNode) bool {
		hit := n.matched
		for _, c := range n.children {
			if mark(c) {
				hit = true
			}
		}
		n.subtreeMatched = hit
		return hit
	}
	for _, r := range roots {
		mark(r)
	}
	return roots
}

// menuPageParentCyclic 判断把 r 挂到它的父上是否会形成环（沿 parent 链向上，边看边记）。
//
// 环数据（a.parent=b、b.parent=a）在树里无处安放：不管挂给谁，两条都会从根集合里消失 ——
// 表现为「列表突然短了」。这类行统一按根处理，用户至少看得见、能在界面上改掉它。
func menuPageParentCyclic(nodes map[uint64]*menuPageNode, r adminmodel.MenuPageRow) bool {
	seen := map[uint64]bool{r.ID: true}
	for id := r.ParentID; id != 0; {
		if seen[id] {
			return true
		}
		seen[id] = true
		parent, ok := nodes[id]
		if !ok {
			return false
		}
		id = parent.row.ParentID
	}
	return false
}

// paginateMenuRoots 按顶级节点分页（页码越界回落到最后一页，与 model 的浏览态同一口径）。
func paginateMenuRoots(roots []*menuPageNode, page, limit int) []*menuPageNode {
	if limit < 1 {
		limit = 1
	}
	if page < 1 {
		page = 1
	}
	total := len(roots)
	if total == 0 {
		return nil
	}
	if last := (total-1)/limit + 1; page > last {
		page = last
	}
	lo := (page - 1) * limit
	hi := lo + limit
	if hi > total {
		hi = total
	}
	return roots[lo:hi]
}

// flattenMenuPageRows 把森林按 DFS 前序摊平成表格行，并算好 Depth / HasChildren / Hidden / Expanded。
//
// 初始可见性在这里一次算清（服务端给初始态、前端只负责切换，前端不做第二套父子规则）：
//   - 浏览态：所有带子行的节点都折叠 —— 列表默认只显示最上级；
//   - 搜索态：命中路径上的祖先展开，命中行才露得出来；命中项自己的子树没读出来
//     （HasChildren 为假），于是也不会渲染一个点开什么都没有的折叠三角。
func flattenMenuPageRows(roots []*menuPageNode) []admindto.MenuPageRow {
	out := make([]admindto.MenuPageRow, 0, len(roots))
	var walk func(nodes []*menuPageNode, depth int, hiddenByAncestor bool)
	walk = func(nodes []*menuPageNode, depth int, hiddenByAncestor bool) {
		for _, n := range nodes {
			hasChildren := len(n.children) > 0
			expanded := hasChildren && n.subtreeMatched
			out = append(out, menuPageRowDTO(n.row, depth, hasChildren, hiddenByAncestor, expanded, n.matched))
			if hasChildren {
				walk(n.children, depth+1, hiddenByAncestor || !expanded)
			}
		}
	}
	walk(roots, 0, false)
	return out
}

// menuPageRowDTO 把 model 的页行连同算好的层级与可见性搬进 dto。
//
// matched 由调用方从建树节点取，而不是再从 r.Matched 读一遍：展开判断用的就是节点上那个值，
// 两处各取一次迟早会不一致 —— 那时页面会出现「行标着「匹配」但祖先没展开」这种自相矛盾的画面。
func menuPageRowDTO(r adminmodel.MenuPageRow, depth int, hasChildren, hidden, expanded, matched bool) admindto.MenuPageRow {
	remark := ""
	if r.Remark != nil {
		remark = *r.Remark
	}
	return admindto.MenuPageRow{
		ID: r.ID, ParentID: r.ParentID, Title: r.Title, Path: r.Path, Type: r.Type,
		Status: r.Status, SortOrder: r.SortOrder, Remark: remark, Icon: r.Icon,
		PermissionCodes: r.PermissionCodes,
		Depth:           depth, HasChildren: hasChildren, Hidden: hidden,
		Expanded: expanded, Matched: matched,
	}
}

// MenuDetail 查询单个菜单详情。
func (s *Service) MenuDetail(ctx context.Context, req *admindto.MenuDetailReq) (res *admindto.MenuDetailResp, err error) {
	entity, err := s.mm.GetByID(ctx, req.ID)
	if err != nil {
		return nil, err
	}
	if entity == nil {
		return nil, errors.New(adminenums.ErrMenuNotFound)
	}

	return menuEntityToDetailResp(entity), nil
}

// MenuCreate 新建菜单。
func (s *Service) MenuCreate(ctx context.Context, req *admindto.MenuCreateReq) error {
	if err := s.validatePermissionBinding(req.Type, req.PermissionCodes, ctx); err != nil {
		return err
	}
	if err := s.validateMenuPlacement(ctx, req.ParentID, req.Type); err != nil {
		return err
	}

	entity := &adminmodel.MenuEntity{
		Title:       req.Title,
		TitleKey:    req.TitleKey,
		ParentID:    req.ParentID,
		Type:        req.Type,
		Path:        req.Path,
		ExternalURL: req.ExternalURL,
		Icon:        req.Icon,
		Status:      menuDefaultStatus(req.Status),
		IsHidden:    req.IsHidden,
		IsPublic:    req.IsPublic,
		SortOrder:   req.SortOrder,
	}
	if req.Remark != "" {
		entity.Remark = &req.Remark
	}

	// 菜单行与它的权限码集合在同一事务内写入（见 model.CreateWithPermissionCodes）：
	// 只写菜单行会让新菜单在授权树里勾不出来，而列表里看着一切正常。
	if err := s.mm.CreateWithPermissionCodes(ctx, entity, req.PermissionCodes); err != nil {
		return err
	}
	invalidateMenuCache()
	return nil
}

// MenuUpdate 更新菜单。
func (s *Service) MenuUpdate(ctx context.Context, req *admindto.MenuUpdateReq) error {
	entity, err := s.mm.GetByID(ctx, req.ID)
	if err != nil {
		return err
	}
	if entity == nil {
		return errors.New(adminenums.ErrMenuNotFound)
	}

	// 系统内置菜单不允许修改类型
	if entity.IsSystem == 1 && entity.Type != req.Type {
		return errors.New(adminenums.ErrMenuIsSystem)
	}

	// 防止父子环
	if req.ParentID == req.ID {
		return errors.New(adminenums.ErrMenuCircle)
	}
	if req.ParentID != entity.ParentID && req.ParentID != 0 {
		if err := s.menuCheckCircle(ctx, req.ID, req.ParentID); err != nil {
			return err
		}
	}

	if err := s.validatePermissionBinding(req.Type, req.PermissionCodes, ctx); err != nil {
		return err
	}
	if err := s.validateMenuPlacement(ctx, req.ParentID, req.Type); err != nil {
		return err
	}

	entity.Title = req.Title
	entity.TitleKey = req.TitleKey
	entity.ParentID = req.ParentID
	entity.Type = req.Type
	entity.Path = req.Path
	entity.ExternalURL = req.ExternalURL
	entity.Icon = req.Icon
	entity.Status = req.Status
	entity.IsHidden = req.IsHidden
	entity.IsPublic = req.IsPublic
	entity.SortOrder = req.SortOrder
	if req.Remark != "" {
		entity.Remark = &req.Remark
	} else {
		entity.Remark = nil
	}

	// 权限码单独走一张表（迁移 470）：旧列 permission_code 由 model 写「集合首码」，
	// 这里不再手工赋值 —— 两处都写会得到两个真源，而它们迟早会分叉。
	if err := s.mm.UpdateWithPermissionCodes(ctx, entity, req.PermissionCodes); err != nil {
		return err
	}
	invalidateMenuCache()
	return nil
}

// MenuDelete 批量删除菜单（软删除）。
func (s *Service) MenuDelete(ctx context.Context, req *admindto.MenuDeleteReq) error {
	// 检查系统菜单
	for _, id := range req.IDs {
		entity, err := s.mm.GetByID(ctx, id)
		if err != nil {
			return err
		}
		if entity == nil {
			continue
		}
		if entity.IsSystem == 1 {
			return errors.New(adminenums.ErrMenuIsSystem)
		}
		// 检查是否有子菜单
		childCount, err := s.mm.CountByParentID(ctx, id)
		if err != nil {
			return err
		}
		if childCount > 0 {
			return errors.New(adminenums.ErrMenuHasChildren)
		}
	}

	_, err := s.mm.SoftDeleteWithPermissionCodes(ctx, req.IDs)
	if err != nil {
		return err
	}
	invalidateMenuCache()
	return nil
}

// validatePermissionBinding 校验 type 与权限码集合的绑定约束。
//
// 迁移 470 之后一个菜单可以挂多个码，规则与单值时代一致，只是逐码展开：
//   - 目录 / iframe / 外链：一个码都不许绑（它们的可见性由 is_public 或父级决定）；
//   - 菜单 / 按钮：至少要绑一个码，否则这个节点在授权树里勾了也换不出任何 API 权限 ——
//     「勾了却没授权」是静默失败里最难查的一类；每个码都必须存在且启用。
//
// 判据用 model 的 DedupePermissionCodes，与写入侧同一份实现（两份去重迟早分叉）。
func (s *Service) validatePermissionBinding(menuType int, codes []string, ctx context.Context) error {
	unique := adminmodel.DedupePermissionCodes(codes)

	// 目录、iframe、外链不得绑定权限
	if menuType == adminmodel.MenuTypeDirectory ||
		menuType == adminmodel.MenuTypeIframe ||
		menuType == adminmodel.MenuTypeExternal {
		if len(unique) > 0 {
			return errors.New(adminenums.ErrCodeNotBindable)
		}
		return nil
	}

	// 菜单和按钮必须绑定权限
	if menuType == adminmodel.MenuTypeMenu || menuType == adminmodel.MenuTypeButton {
		if len(unique) == 0 {
			return errors.New(adminenums.ErrCodeRequired)
		}
		// 逐码校验存在且启用（同包直调）：停用的码照挂会让「勾了保存却没生效」无从解释。
		for _, code := range unique {
			ok, err := s.ExistsEnabledCode(ctx, code)
			if err != nil {
				return err
			}
			if !ok {
				return errors.New(adminenums.ErrCodeNotEnabled)
			}
		}
	}
	return nil
}

// maxNavDepth 导航最大层级（目录链 + 菜单 = 3 级）。
//
// 侧边栏渲染（partials/nav-nodes.html）支持任意深度并做缩进封顶，
// 但超过 3 级后后台可用性急剧下降（缩进吃宽度、认知负担），故在写入侧拦截。
const maxNavDepth = 3

// validateMenuPlacement 校验菜单层级深度（不超过 maxNavDepth）。
//
// 不限制父级的 type 语义：父级菜单本身也可以有子菜单（主菜单既是页面又是分组），
// 可点击性由「有没有页面路径」决定，而非由类型决定。
// 父级不存在时不拦截——树构建侧对孤儿节点做降级处理，保持既有语义不变。
func (s *Service) validateMenuPlacement(ctx context.Context, parentID uint64, menuType int) error {
	if parentID == 0 {
		return nil
	}
	_ = menuType // 保留参数：未来若需按类型收紧约束（如按钮必须挂菜单）在此扩展
	depth := 1
	cur, err := s.mm.GetByID(ctx, parentID)
	if err != nil {
		return err
	}
	for cur != nil {
		depth++
		if depth > maxNavDepth {
			return errors.New(adminenums.ErrMenuDepthExceeded)
		}
		if cur.ParentID == 0 {
			break
		}
		cur, err = s.mm.GetByID(ctx, cur.ParentID)
		if err != nil {
			return err
		}
	}
	return nil
}

// menuCheckCircle 检查将 targetID 的 parent 设为 newParentID 是否形成环。
func (s *Service) menuCheckCircle(ctx context.Context, targetID, newParentID uint64) error {
	// 向上追溯 newParentID 的祖先链
	cursor := newParentID
	for cursor != 0 {
		if cursor == targetID {
			return errors.New(adminenums.ErrMenuCircle)
		}
		parent, err := s.mm.GetByID(ctx, cursor)
		if err != nil {
			return err
		}
		if parent == nil {
			break
		}
		cursor = parent.ParentID
	}
	return nil
}

// menuDefaultStatus 返回默认启用状态（1），当传入 status 为 0 时使用。
func menuDefaultStatus(status int) int {
	if status == 0 {
		return adminmodel.MenuStatusEnabled
	}
	return status
}

// menuEntityToDetailResp MenuEntity 转 DetailResp。
func menuEntityToDetailResp(e *adminmodel.MenuEntity) *admindto.MenuDetailResp {
	remark := ""
	if e.Remark != nil {
		remark = *e.Remark
	}
	return &admindto.MenuDetailResp{
		ID:              e.ID,
		PermissionCodes: e.PermissionCodes,
		Title:           e.Title,
		TitleKey:        e.TitleKey,
		ParentID:        e.ParentID,
		Type:            e.Type,
		Path:            e.Path,
		ExternalURL:     e.ExternalURL,
		Icon:            e.Icon,
		Status:          e.Status,
		IsHidden:        e.IsHidden,
		IsPublic:        e.IsPublic,
		IsSystem:        e.IsSystem,
		SortOrder:       e.SortOrder,
		Remark:          remark,
	}
}

// MenuTree 查询完整菜单树，支持 status/type/search 筛选。
func (s *Service) MenuTree(ctx context.Context, req *admindto.MenuTreeReq) ([]admindto.MenuTreeNode, error) {
	all, err := s.mm.ListAll(ctx)
	if err != nil {
		return nil, err
	}

	// 内存过滤
	filtered := make([]adminmodel.MenuEntity, 0, len(all))
	for _, m := range all {
		if req.Status != nil && m.Status != *req.Status {
			continue
		}
		if req.Type != nil && m.Type != *req.Type {
			continue
		}
		if req.Search != "" {
			if !strings.Contains(m.Title, req.Search) {
				continue
			}
		}
		filtered = append(filtered, m)
	}

	return buildMenuTree(filtered), nil
}

// buildMenuTree 将扁平列表构建为树结构。
//
// 递归构建：先完整构建子树，再组装父节点——Children 是值切片
// （[]MenuTreeNode），若用「roots 值拷贝 + nodeMap 指针挂载」的两遍组装，
// 顶级节点副本的 Children 恒为空（子节点挂在指针上，拷贝时未完成），
// 整棵菜单树会丢失全部子树。
func buildMenuTree(list []adminmodel.MenuEntity) []admindto.MenuTreeNode {
	childrenOf := make(map[uint64][]adminmodel.MenuEntity, len(list))
	ids := make(map[uint64]bool, len(list))
	for _, m := range list {
		childrenOf[m.ParentID] = append(childrenOf[m.ParentID], m)
		ids[m.ID] = true
	}
	// 孤儿节点（ParentID 非 0 但父不在列表中）归入顶级分组，与旧行为一致。
	var orphans []adminmodel.MenuEntity
	for _, m := range list {
		if m.ParentID != 0 && !ids[m.ParentID] {
			orphans = append(orphans, m)
		}
	}
	childrenOf[0] = append(childrenOf[0], orphans...)

	var build func(parentID uint64) []admindto.MenuTreeNode
	build = func(parentID uint64) []admindto.MenuTreeNode {
		var nodes []admindto.MenuTreeNode
		for _, m := range childrenOf[parentID] {
			n := menuEntityToNode(m)
			n.Children = build(m.ID)
			nodes = append(nodes, n)
		}
		return nodes
	}
	return build(0)
}

// menuEntityToNode MenuEntity 转树节点 MenuTreeNode。
func menuEntityToNode(m adminmodel.MenuEntity) admindto.MenuTreeNode {
	remark := ""
	if m.Remark != nil {
		remark = *m.Remark
	}
	titleKey := ""
	if m.TitleKey != nil {
		titleKey = *m.TitleKey
	}
	return admindto.MenuTreeNode{
		ID:              m.ID,
		PermissionCodes: m.PermissionCodes,
		Title:           m.Title,
		TitleKey:        titleKey,
		ParentID:        m.ParentID,
		Type:            m.Type,
		Path:            m.Path,
		ExternalURL:     m.ExternalURL,
		Icon:            m.Icon,
		Status:          m.Status,
		IsHidden:        m.IsHidden,
		IsPublic:        m.IsPublic,
		IsSystem:        m.IsSystem,
		SortOrder:       m.SortOrder,
		Remark:          remark,
	}
}
