package adminservice

import (
	"context"
	"errors"
	"sort"
	"strings"
	"sync"
	"time"

	"go_wp/internal/module/admin/dto"
	"go_wp/internal/module/admin/enums"
	"go_wp/internal/module/admin/model"
	"go_wp/pkg/i18n"
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

// GetPermissionCodesByIDs 根据 menu_id 列表收集 permission_code 并去重。
//
// 判据是「这个节点有没有权限码」，**不按菜单类型白名单过滤**：
//   - 目录（type=1）本来就没有权限码（迁移 224 重建的 7 个目录全为空），会被自然跳过；
//   - iframe / 外链（type=4/5）如果配了码，同样应当生效。此前写死 type=2/3 会让它们
//     「勾了保存后静默消失」—— 反查路径不过滤类型，于是它们出现在已勾选列表里、
//     看起来已授权，一保存又被丢掉，表现为「配置随机丢失」。
//
// 一个菜单可以有多个码（迁移 470 的 sys_menu_permission）：返回值是按 menu_id 升序、
// 再按码升序收集的去重集合 —— 顺序确定，因为它的下游是 Casbin 全量替换，
// 不确定的集合顺序会让同一次保存产生不同的策略写入顺序（测试与审计都无法复现）。
//
// 调用方传进来的 id 里混着目录、不存在的 id 都是常态（前端提交的是整棵勾选树）：
// 关联表只回存在的行，其余自然被忽略，不需要在这里做额外校验。
func (s *Service) GetPermissionCodesByIDs(ctx context.Context, menuIDs []uint64) ([]string, error) {
	byMenu, err := s.mm.ListPermissionCodesByMenuIDs(ctx, menuIDs)
	if err != nil {
		return nil, err
	}
	ids := make([]uint64, 0, len(byMenu))
	for id := range byMenu {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })

	seen := make(map[string]struct{})
	var codes []string
	for _, id := range ids {
		for _, code := range byMenu[id] {
			if _, ok := seen[code]; ok {
				continue
			}
			seen[code] = struct{}{}
			codes = append(codes, code)
		}
	}
	return codes, nil
}

// matchedMenuCodes 返回菜单的码里命中用户权限集合的那些（保持集合内的原有顺序）。
//
// 菜单可见性 / 按钮授权的判据都收敛到这一处：一个菜单挂多个码（迁移 470）之后，
// 「有任意一个码被授权」就算这个节点可用 —— 只要有一个动作能调，入口就不该消失，
// 否则会出现「API 能调、侧栏没有入口」的分裂授权（与 withAncestorMenuIDs 同一取舍）。
func matchedMenuCodes(codes []string, codeSet map[string]struct{}) []string {
	var hit []string
	for _, code := range codes {
		if _, ok := codeSet[code]; ok {
			hit = append(hit, code)
		}
	}
	return hit
}

// withAncestorMenuIDs 向上补齐祖先 id（目录与父菜单），返回去重且升序的集合。
//
// 为什么必须补（「只勾了按钮、没勾它所属的菜单」这个陷阱的根因）：
//   - 反查路径（GetIDsByPermissionCodes）只按 permission_code 命中节点，而**目录没有码**，
//     所以目录永远不会出现在已勾选列表里 —— 重新打开分配页时目录一律显示未勾选，
//     看起来像「上次勾的目录没保存」；
//   - 授权一个按钮（type=3）时，它的父菜单（type=2）不会被带上，该角色拿到的是
//     「API 能调、侧栏没有入口」的分裂状态：按钮的权限点在 Casbin 里是通的，
//     而 BuildAuthorizedTree 按 permission_code 过滤菜单，父菜单的码不在 codes 里，
//     于是侧栏根本不生成这个入口。
//
// 让「子节点被授权 ⇒ 祖先全部在集合里」成为不变式，读取与保存两端共用同一个实现。
//
// 语义后果（刻意，写下来避免后来者当成 bug 修）：补齐会**连带授予祖先菜单的权限码**。
// 这是「勾了按钮就必须能看到入口」的必然代价。反方向的联动（勾菜单自动勾全部子按钮）
// 才是提权 —— 一次「页面管理」会顺手给出「删除页面 / 发布页面」，本项目不做。
//
// 不存在的 id 会被丢弃（byID 查不到即停），这同时充当保存入口的白名单过滤。
func withAncestorMenuIDs(all []adminmodel.MenuEntity, ids []uint64) []uint64 {
	if len(ids) == 0 {
		return nil
	}
	byID := make(map[uint64]adminmodel.MenuEntity, len(all))
	for _, m := range all {
		byID[m.ID] = m
	}

	seen := make(map[uint64]struct{}, len(ids))
	out := make([]uint64, 0, len(ids))
	for _, id := range ids {
		cursor := id
		// seen 兼作防环：脏数据（parent_id 成环）时上溯会立刻停下，不死循环。
		for cursor != 0 {
			if _, ok := seen[cursor]; ok {
				break
			}
			m, ok := byID[cursor]
			if !ok {
				break
			}
			seen[cursor] = struct{}{}
			out = append(out, cursor)
			cursor = m.ParentID
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

// buildPermissionTree 构建角色权限分配树的节点集合：
// 启用节点 ∪ 已勾选节点 ∪ 它们的全部祖先。
//
// 为什么不是「只给启用节点」：角色可能先勾选、后被禁用的菜单。若树里没有它，
// 前端就渲染不出这个勾选，管理员一保存就把这条授权静默删掉（数据丢失、且没有任何提示）。
// 因此把禁用项与祖先一起保留在树里，由页面标灰呈现，保存时原样带回。
func buildPermissionTree(all []adminmodel.MenuEntity, checkedIDs []uint64) []admindto.MenuTreeNode {
	byID := make(map[uint64]adminmodel.MenuEntity, len(all))
	for _, m := range all {
		byID[m.ID] = m
	}

	keep := make(map[uint64]struct{}, len(all))
	for _, m := range all {
		if m.Status == adminmodel.MenuStatusEnabled {
			keep[m.ID] = struct{}{}
		}
	}
	for _, id := range checkedIDs {
		keep[id] = struct{}{}
	}

	// 补祖先用显式队列，不在 range map 的同时增删（Go 允许，但迭代结果不确定）。
	queue := make([]uint64, 0, len(keep))
	for id := range keep {
		queue = append(queue, id)
	}
	for i := 0; i < len(queue); i++ {
		m, ok := byID[queue[i]]
		if !ok || m.ParentID == 0 {
			continue
		}
		if _, exists := keep[m.ParentID]; exists {
			continue
		}
		keep[m.ParentID] = struct{}{}
		queue = append(queue, m.ParentID)
	}

	// all 已按 sort_order, id 排序，过滤后顺序保持，因此建出来的树是确定的。
	filtered := make([]adminmodel.MenuEntity, 0, len(keep))
	for _, m := range all {
		if _, ok := keep[m.ID]; ok {
			filtered = append(filtered, m)
		}
	}
	return buildMenuTree(filtered)
}

// GetIDsByPermissionCodes 根据 permission_code 列表反查 menu_id。
//
// 两步而不是一次 join：model 层不做多表关联（internal/module/CLAUDE.md），
// 所以先按码取关联行里的 menu_id，再用 ListByIDs 过滤掉不存在与已软删的菜单。
// 顺序沿用 ListByIDs 的 sort_order, id —— 与迁移 470 之前逐条扫菜单表时的顺序一致，
// 回显勾选态不受影响。
func (s *Service) GetIDsByPermissionCodes(ctx context.Context, codes []string) ([]uint64, error) {
	candidates, err := s.mm.ListMenuIDsByPermissionCodes(ctx, codes)
	if err != nil {
		return nil, err
	}
	menus, err := s.mm.ListByIDs(ctx, candidates)
	if err != nil {
		return nil, err
	}

	ids := make([]uint64, 0, len(menus))
	for _, m := range menus {
		ids = append(ids, m.ID)
	}
	return ids, nil
}

// CountByPermissionCodes 统计引用指定权限编码的未删除菜单数。
//
// 判据是「有几个菜单在用这个码」，**按菜单去重**：一个菜单同时挂两个待删权限点时只算一个，
// 否则「删 2 个权限点会波及 3 个菜单」这种数字会凭空出现，操作者无法核对。
// 与 GetIDsByPermissionCodes 同路：关联表取候选 → ListByIDs 过滤软删。
func (s *Service) CountByPermissionCodes(ctx context.Context, codes []string) (count int64, err error) {
	ids, err := s.GetIDsByPermissionCodes(ctx, codes)
	if err != nil {
		return 0, err
	}
	return int64(len(ids)), nil
}

// AuthorizedButtonCodes 返回当前用户有权触发的按钮码集合（type=3 节点的 title_key）。
//
// 判据与菜单可见性同源（matchedMenuCodes）：节点绑的**任一**权限码在用户的有效码集合里，
// 这个节点就算可用 —— 一个节点挂多个码时「有一个能调就不该藏」，与菜单的取舍一致。
//
// 没填码的节点直接跳过：它的按钮码是空串，收进集合只会让模板里出现一个空键。
func (s *Service) AuthorizedButtonCodes(ctx context.Context, codes []string) ([]string, error) {
	all, err := s.listEnabledMenusCached(ctx)
	if err != nil {
		return nil, err
	}
	codeSet := make(map[string]struct{}, len(codes))
	for _, c := range codes {
		codeSet[c] = struct{}{}
	}
	out := make([]string, 0, 64)
	for _, m := range all {
		if m.Type != adminmodel.MenuTypeButton || m.Status != adminmodel.MenuStatusEnabled {
			continue
		}
		key := ""
		if m.TitleKey != nil {
			key = strings.TrimSpace(*m.TitleKey)
		}
		if key == "" {
			continue
		}
		if len(matchedMenuCodes(m.PermissionCodes, codeSet)) > 0 {
			out = append(out, key)
		}
	}
	return out, nil
}

// BuildAuthorizedTree 根据有效 permission_code 列表构建用户可见菜单树。
// 自动补齐祖先目录，只包含 type=1（目录）和 type=2（菜单）。
func (s *Service) BuildAuthorizedTree(ctx context.Context, codes []string) ([]admindto.MenuTreeNode, error) {
	all, err := s.listEnabledMenusCached(ctx)
	if err != nil {
		return nil, err
	}
	return buildAuthorizedTree(all, codes), nil
}

func buildAuthorizedTree(all []adminmodel.MenuEntity, codes []string) []admindto.MenuTreeNode {
	codeSet := make(map[string]struct{}, len(codes))
	for _, c := range codes {
		codeSet[c] = struct{}{}
	}

	// 收集启用且公开或已授权的 type=2 菜单
	visibleIDs := make(map[uint64]struct{})
	for _, m := range all {
		if m.Status != adminmodel.MenuStatusEnabled || m.Type != adminmodel.MenuTypeMenu {
			continue
		}
		if m.IsPublic == 1 {
			visibleIDs[m.ID] = struct{}{}
			continue
		}
		if len(matchedMenuCodes(m.PermissionCodes, codeSet)) > 0 {
			visibleIDs[m.ID] = struct{}{}
		}
	}

	// 补齐启用的祖先目录
	byID := make(map[uint64]adminmodel.MenuEntity, len(all))
	for _, m := range all {
		if m.Status == adminmodel.MenuStatusEnabled {
			byID[m.ID] = m
		}
	}

	for id := range visibleIDs {
		cursor := byID[id].ParentID
		for cursor != 0 {
			parent, ok := byID[cursor]
			if !ok {
				break
			}
			if _, exists := visibleIDs[cursor]; exists {
				break
			}
			visibleIDs[cursor] = struct{}{}
			cursor = parent.ParentID
		}
	}

	// 过滤出可见节点（只含目录和菜单，排除按钮/iframe/外链）
	filtered := make([]adminmodel.MenuEntity, 0, len(visibleIDs))
	for _, m := range all {
		if m.Type != adminmodel.MenuTypeDirectory && m.Type != adminmodel.MenuTypeMenu {
			continue
		}
		if _, ok := visibleIDs[m.ID]; ok {
			filtered = append(filtered, m)
		}
	}

	return buildMenuTree(filtered)
}

// translateMenuTitle 菜单标题翻译：title_key 命中 i18n 资源则用翻译，否则回退 title 原文。
func translateMenuTitle(titleKey, title, lang string) string {
	if titleKey == "" {
		return title
	}
	if translated := i18n.GetText(titleKey, lang); translated != titleKey {
		return translated
	}
	return title
}

const menuCacheTTL = 30 * time.Second

var (
	menuCacheMu sync.RWMutex
	menuCache   []adminmodel.MenuEntity
	menuCacheAt time.Time
)

// listEnabledMenusCached 带短 TTL 的「启用菜单」缓存（PERF-010）。
// 菜单变更频率低，权限路由构建却每次请求都触发；缓存 30s 可显著减少 DB 读。
//
// 三点关于「在哪一层过滤」的取舍，改这里之前先读：
//
//  1. status=1 已下推到 SQL（ListEnabled）：它与用户无关，禁用项从不进树；
//  2. type 不下推：同一份缓存要同时服务导航树（只要 type=1/2）与动态路由
//     （还要 type=3 的按钮权限点算按钮授权），砍掉 type 就得缓存两份；
//  3. permission_code 与用户权限码的交集留在内存：它因人而异，下推会让这条 SQL
//     的文本随用户变化（无法复用执行计划），缓存键也得带上用户权限集合 ——
//     等于放弃跨用户共享这份缓存，每次请求都回落到查库。
//     菜单表当前百来行，一次全表读 + 内存过滤比按用户查更划算。
func (s *Service) listEnabledMenusCached(ctx context.Context) ([]adminmodel.MenuEntity, error) {
	menuCacheMu.RLock()
	if len(menuCache) > 0 && time.Since(menuCacheAt) < menuCacheTTL {
		cached := append([]adminmodel.MenuEntity(nil), menuCache...)
		menuCacheMu.RUnlock()
		return cached, nil
	}
	menuCacheMu.RUnlock()

	all, err := s.mm.ListEnabled(ctx)
	if err != nil {
		return nil, err
	}
	menuCacheMu.Lock()
	menuCache = append([]adminmodel.MenuEntity(nil), all...)
	menuCacheAt = time.Now()
	menuCacheMu.Unlock()
	return all, nil
}

// invalidateMenuCache 菜单写操作后清缓存。
func invalidateMenuCache() {
	menuCacheMu.Lock()
	menuCache = nil
	menuCacheAt = time.Time{}
	menuCacheMu.Unlock()
}
