package navigationservice

import (
	"context"
	"errors"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"go_wp/internal/module/navigation/dto"
	"go_wp/internal/module/navigation/enums"
	"go_wp/internal/module/navigation/model"
	"go_wp/internal/templates"
	"go_wp/pkg/bulkids"
	"go_wp/pkg/facing"
)

func (s *Service) Create(ctx context.Context, req *navigationdto.CreateReq) (res *navigationdto.NavigationResp, err error) {
	if req == nil {
		return nil, navigationenums.Err(navigationenums.ErrInvalidParam)
	}
	projectID := strings.TrimSpace(req.ProjectID)
	title := strings.TrimSpace(req.Title)
	path := strings.TrimSpace(req.Path)
	kind := strings.TrimSpace(req.Kind)
	if projectID == "" {
		return nil, navigationenums.Err(navigationenums.ErrInvalidParam)
	}
	if err = validateField(title, path, kind); err != nil {
		return nil, err
	}
	parentID := normalizeParentID(req.ParentID)

	sourceType, sourceID, target, err := normalizeSource(req.SourceType, req.SourceID, req.Target)
	if err != nil {
		return nil, err
	}
	panelBlockID, panelWidth, perr := normalizePanel(req.PanelBlockID, req.PanelWidth)
	if perr != nil {
		return nil, perr
	}

	now := time.Now().UTC()
	e := &navigationmodel.NavigationEntity{
		ID: uuid.NewString(), ProjectID: projectID, Title: title, Path: path,
		Kind: kind, ParentID: parentID, SortOrder: req.SortOrder,
		SourceType: sourceType, SourceID: sourceID, Target: target,
		PanelBlockID: panelBlockID, PanelWidth: panelWidth,
		CreatedAt: now, UpdatedAt: now,
	}

	at := time.Now().UTC()
	var out *navigationmodel.NavigationEntity
	if werr := s.withMenuWriteTx(ctx, e.ProjectID, func(tx *gorm.DB) error {

		if verr := s.validateParentTx(ctx, tx, projectID, kind, "", parentID); verr != nil {
			return verr
		}
		exists, xerr := s.m.ExistsPathTx(ctx, tx, projectID, kind, path, "")
		if xerr != nil {
			return xerr
		}
		if exists {
			return navigationenums.Err(navigationenums.ErrPathTaken)
		}

		if e.SortOrder == 0 {
			maxOrder, merr := s.m.MaxSortOrderTx(ctx, tx, projectID, kind, parentID)
			if merr != nil {
				return merr
			}
			e.SortOrder = maxOrder + 1
		}
		if cerr := s.m.CreateTx(ctx, tx, e); cerr != nil {
			return cerr
		}
		if merr := s.markMenuStaleInTx(ctx, tx, e.ProjectID, e.Kind, at); merr != nil {
			return merr
		}
		if merr := s.markNavigationStaleInTx(ctx, tx, e.ProjectID, e.ID); merr != nil {
			return merr
		}

		fresh, ferr := s.m.GetForUpdateTx(ctx, tx, projectID, e.ID)
		if ferr != nil {
			return ferr
		}
		out = fresh
		return nil
	}); werr != nil {
		return nil, werr
	}
	if out == nil {
		return nil, navigationenums.Err(navigationenums.ErrNotFound)
	}
	return toResp(out), nil
}

func (s *Service) Update(ctx context.Context, req *navigationdto.UpdateReq) (res *navigationdto.NavigationResp, err error) {
	if req == nil || strings.TrimSpace(req.ID) == "" {
		return nil, navigationenums.Err(navigationenums.ErrInvalidParam)
	}

	located, err := s.locateNavigation(ctx, strings.TrimSpace(req.ID))
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, navigationenums.Err(navigationenums.ErrNotFound)
	}
	if err != nil {
		return nil, err
	}
	projectID, navID := located.ProjectID, located.ID

	var (
		reqTitle    *string
		reqPath     *string
		reqKind     *string
		newParent   *string
		parentGiven bool
	)
	if req.Title != nil {
		v := strings.TrimSpace(*req.Title)
		reqTitle = &v
	}
	if req.Path != nil {
		v := strings.TrimSpace(*req.Path)
		reqPath = &v
	}
	if req.Kind != nil {
		v := strings.TrimSpace(*req.Kind)
		reqKind = &v
	}
	if req.ParentID != nil {
		parentGiven = true
		newParent = normalizeParentID(req.ParentID)
	}

	sourceGiven := req.SourceType != nil || req.SourceID != nil || req.Target != nil

	var panelBlockID *string
	if req.PanelBlockID != nil {
		panelBlockID = normalizePanelBlockID(req.PanelBlockID)
	}
	var panelWidth *string
	if req.PanelWidth != nil {
		w, werr := normalizePanelWidth(*req.PanelWidth)
		if werr != nil {
			return nil, werr
		}
		panelWidth = &w
	}

	expected, perr := parseExpectedUpdatedAt(req.ExpectedUpdatedAt)
	if perr != nil {
		return nil, perr
	}

	at := time.Now().UTC()
	var out *navigationmodel.NavigationEntity
	if werr := s.withMenuWriteTx(ctx, projectID, func(tx *gorm.DB) error {

		cur, gerr := s.m.GetForUpdateTx(ctx, tx, projectID, navID)
		if errors.Is(gerr, gorm.ErrRecordNotFound) {
			return navigationenums.Err(navigationenums.ErrNotFound)
		}
		if gerr != nil {
			return gerr
		}

		updates := map[string]any{"update_time": at}
		title, path, kind := cur.Title, cur.Path, cur.Kind
		if reqTitle != nil {
			title = *reqTitle
			updates["title"] = title
		}
		if reqPath != nil {
			path = *reqPath
			updates["path"] = path
		}
		if reqKind != nil {
			kind = *reqKind
			updates["kind"] = kind
		}
		if verr := validateField(title, path, kind); verr != nil {
			return verr
		}
		if parentGiven {
			updates["parent_id"] = newParent
		}
		if req.SortOrder != nil {
			updates["sort_order"] = *req.SortOrder
		}

		if sourceGiven {
			sourceType, sourceID, target := cur.SourceType, cur.SourceID, cur.Target
			if req.SourceType != nil {
				sourceType = *req.SourceType
			}
			if req.SourceID != nil {
				sourceID = normalizeSourceID(req.SourceID)
			}
			if req.Target != nil {
				target = *req.Target
			}
			st, sid, tg, serr := normalizeSource(sourceType, sourceID, target)
			if serr != nil {
				return serr
			}
			updates["source_type"], updates["source_id"], updates["target"] = st, sid, tg
		}
		if req.PanelBlockID != nil {

			if panelBlockID == nil {
				updates["panel_block_id"] = nil
			} else {
				updates["panel_block_id"] = *panelBlockID
			}
		}
		if panelWidth != nil {
			updates["panel_width"] = *panelWidth
		}

		if parentGiven {
			if verr := s.validateParentTx(ctx, tx, projectID, kind, navID, newParent); verr != nil {
				return verr
			}
		}
		if reqPath != nil || reqKind != nil {
			exists, xerr := s.m.ExistsPathTx(ctx, tx, projectID, kind, path, navID)
			if xerr != nil {
				return xerr
			}
			if exists {
				return navigationenums.Err(navigationenums.ErrPathTaken)
			}
		}
		if expected == nil {
			if uerr := s.m.SaveTx(ctx, tx, projectID, navID, updates); uerr != nil {
				return uerr
			}
		} else {

			rows, serr := s.m.SaveWithExpectedTx(ctx, tx, projectID, navID, updates, *expected)
			if serr != nil {
				return serr
			}
			if rows == 0 {
				return staleVersionMessage(s.staleTitle(ctx, cur))
			}
		}

		if merr := s.markMenuStaleInTx(ctx, tx, projectID, kind, at); merr != nil {
			return merr
		}
		if cur.Kind != kind {
			if merr := s.markMenuStaleInTx(ctx, tx, projectID, cur.Kind, at); merr != nil {
				return merr
			}
		}
		if merr := s.markNavigationStaleInTx(ctx, tx, projectID, navID); merr != nil {
			return merr
		}

		fresh, ferr := s.m.GetForUpdateTx(ctx, tx, projectID, navID)
		if ferr != nil {
			return ferr
		}
		out = fresh
		return nil
	}); werr != nil {
		return nil, werr
	}
	if out == nil {

		return nil, navigationenums.Err(navigationenums.ErrNotFound)
	}
	return toResp(out), nil
}

func (s *Service) Get(ctx context.Context, req *navigationdto.GetReq) (res *navigationdto.NavigationResp, err error) {
	if req == nil || strings.TrimSpace(req.ID) == "" {
		return nil, navigationenums.Err(navigationenums.ErrInvalidParam)
	}

	e, err := s.locateNavigation(ctx, strings.TrimSpace(req.ID))
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, navigationenums.Err(navigationenums.ErrNotFound)
	}
	if err != nil {
		return nil, err
	}
	return toResp(e), nil
}

func (s *Service) List(ctx context.Context, req *navigationdto.ListReq) (list []*navigationdto.NavigationResp, err error) {
	if req == nil || strings.TrimSpace(req.ProjectID) == "" {
		return nil, navigationenums.Err(navigationenums.ErrInvalidParam)
	}
	kind := strings.TrimSpace(req.Kind)
	if kind != "" && !isValidKind(kind) {
		return nil, navigationenums.Err(navigationenums.ErrInvalidKind)
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

func (s *Service) Delete(ctx context.Context, req *navigationdto.DeleteReq) (err error) {
	if req == nil || strings.TrimSpace(req.ID) == "" {
		return navigationenums.Err(navigationenums.ErrInvalidParam)
	}
	id := strings.TrimSpace(req.ID)
	parsed, perr := uuid.Parse(id)
	// 非 uuid 无法定位到任何工程：按参数错误拒绝，不让驱动把 22P02 抛成 500。
	if perr != nil {
		return navigationenums.Err(navigationenums.ErrInvalidParam)
	}
	canonical := parsed.String()

	// 事务外只做定位：FORCE RLS 下必须逐工程探测归属，不能退化成不限工程查询。
	// 事务内的存在性、最新 kind 与子树都在锁内重新读取，不沿用这里的事实。
	e, err := s.locateNavigation(ctx, canonical)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return navigationenums.Err(navigationenums.ErrNotFound)
	}
	if err != nil {
		return err
	}

	return s.withMenuWriteTx(ctx, e.ProjectID, func(tx *gorm.DB) error {
		return s.deleteNavigationTx(ctx, tx, e.ProjectID, []string{canonical})
	})
}

// deleteNavigationTx 是单条与批量删除共用的批删规则：
//
//   - 全部目标根在同一事务内按 id 升序加锁后重新核对存在性；
//   - kind 只用锁内读到的行，不用事务外的旧 kind 计算子树；
//   - 完整读取工程内所有导航行，跨 kind 也按 parent 关系求子树并合并重叠；
//   - 只删合并后的根（都被选中的父子项只删一次），子孙由外键级联删除；
//   - 每个实际被删的行都标记按项失效，涉及的每个 kind 都标记菜单失效。
func (s *Service) deleteNavigationTx(ctx context.Context, tx *gorm.DB, projectID string, rootIDs []string) error {
	if len(rootIDs) == 0 {
		return navigationenums.Err(navigationenums.ErrInvalidParam)
	}
	locked, err := s.m.LockByIDsTx(ctx, tx, projectID, rootIDs)
	if err != nil {
		return err
	}
	if len(locked) != len(rootIDs) {
		found := make(map[string]struct{}, len(locked))
		for _, row := range locked {
			found[row.ID] = struct{}{}
		}
		blocked := make([]facing.BlockedItem, 0, len(rootIDs)-len(locked))
		for _, id := range rootIDs {
			if _, ok := found[id]; !ok {
				blocked = append(blocked, facing.BlockedItem{ID: id, ReasonKey: navigationenums.ErrNotFound})
			}
		}
		return RejectBatch(len(rootIDs), blocked)
	}

	rows, err := s.m.ListTx(ctx, tx, projectID, "")
	if err != nil {
		return err
	}
	byID := make(map[string]*navigationmodel.NavigationEntity, len(rows))
	for _, row := range rows {
		byID[row.ID] = row
	}
	// 锁内复核：加锁后行必须仍在本工程可见；并发删除时整批拒绝。
	blocked := make([]facing.BlockedItem, 0)
	for _, id := range rootIDs {
		if _, ok := byID[id]; !ok {
			blocked = append(blocked, facing.BlockedItem{ID: id, ReasonKey: navigationenums.ErrNotFound})
		}
	}
	if len(blocked) > 0 {
		return RejectBatch(len(rootIDs), blocked)
	}

	// 稳定顺序合并重叠子树：先求每个根的完整子树，再丢掉「已被别的选中根
	// 覆盖」的根。否则父项与子项同时被选中且 kind 不同时，父项先删会级联掉子项，
	// 轮到子项那批就命中零行而被误判成不存在。
	sortedRoots := append([]string(nil), rootIDs...)
	sort.Strings(sortedRoots)
	subtrees := make(map[string]map[string]struct{}, len(sortedRoots))
	for _, root := range sortedRoots {
		ids := navDescendantIDs(rows, root)
		set := make(map[string]struct{}, len(ids))
		for _, id := range ids {
			set[id] = struct{}{}
		}
		subtrees[root] = set
	}
	mergedRoots := make([]string, 0, len(sortedRoots))
	for _, root := range sortedRoots {
		covered := false
		for _, other := range sortedRoots {
			if other == root {
				continue
			}
			if _, inOther := subtrees[other][root]; !inOther {
				continue
			}
			// root 被 other 的子树覆盖：跨 kind 的父子同时选中也只保留祖先。
			// 异常环里两个根互相覆盖时，保留字典序更早的那个，
			// 不会两个都被丢掉而变成「什么都没删」。
			if _, otherInRoot := subtrees[root][other]; !otherInRoot || other < root {
				covered = true
				break
			}
		}
		if !covered {
			mergedRoots = append(mergedRoots, root)
		}
	}
	claimed := make(map[string]struct{}, len(rows))
	deletedIDs := make([]string, 0, len(rows))
	for _, root := range mergedRoots {
		for id := range subtrees[root] {
			if _, seen := claimed[id]; seen {
				continue
			}
			claimed[id] = struct{}{}
			deletedIDs = append(deletedIDs, id)
		}
	}
	deletedIDs = uniqueStrings(deletedIDs)
	sort.Strings(deletedIDs)
	rootKind := make(map[string]string, len(mergedRoots))
	for _, id := range mergedRoots {
		rootKind[id] = byID[id].Kind
	}
	kinds := make([]string, 0, 4)
	for _, id := range deletedIDs {
		kinds = append(kinds, byID[id].Kind)
	}
	kinds = uniqueStrings(kinds)
	sort.Strings(kinds)

	at := time.Now().UTC()
	for _, kind := range kinds {
		// 只删合并后的根：子孙由外键级联删除，父项与子项同时被选中时也不会
		// 出现「第二遍删已删空集」。
		kindRoots := make([]string, 0, len(mergedRoots))
		for _, id := range mergedRoots {
			if rootKind[id] == kind {
				kindRoots = append(kindRoots, id)
			}
		}
		if len(kindRoots) > 0 {
			deleted, derr := s.m.DeleteManyTx(ctx, tx, projectID, kindRoots)
			if derr != nil {
				return derr
			}
			if deleted != int64(len(kindRoots)) {
				return navigationenums.Err(navigationenums.ErrNotFound)
			}
		}
		// 每个实际被删的行（含已由级联删除的子孙）都按项标记失效。
		for _, id := range deletedIDs {
			if byID[id].Kind != kind {
				continue
			}
			if nerr := s.markNavigationStaleInTx(ctx, tx, projectID, id); nerr != nil {
				return nerr
			}
		}
		if merr := s.markMenuStaleInTx(ctx, tx, projectID, kind, at); merr != nil {
			return merr
		}
	}

	// 核对真实删除结果：合并后的整棵子树（含级联子孙）必须一行不剩，
	// 不把「根删掉了」当成「子树都删掉了」。
	remaining, rerr := s.m.ListTx(ctx, tx, projectID, "")
	if rerr != nil {
		return rerr
	}
	stillThere := make(map[string]struct{}, len(deletedIDs))
	for _, row := range remaining {
		stillThere[row.ID] = struct{}{}
	}
	blocked = blocked[:0]
	for _, id := range deletedIDs {
		if _, exists := stillThere[id]; exists {
			blocked = append(blocked, facing.BlockedItem{ID: id, ReasonKey: navigationenums.ErrNotFound})
		}
	}
	if len(blocked) > 0 {
		return RejectBatch(len(rootIDs), blocked)
	}
	return nil
}

// uniqueStrings 去重并保持首次出现顺序。
func uniqueStrings(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, v := range values {
		if _, ok := seen[v]; ok {
			continue
		}
		seen[v] = struct{}{}
		out = append(out, v)
	}
	return out
}

// navDescendantIDs 返回 root 及其全部子孙。visited 集合保证已有的异常环
// （parent_id 互相指向）也能终止，不会无限展开。
func navDescendantIDs(rows []*navigationmodel.NavigationEntity, rootID string) []string {
	children := make(map[string][]string, len(rows))
	for _, r := range rows {
		if r.ParentID != nil && *r.ParentID != "" {
			children[*r.ParentID] = append(children[*r.ParentID], r.ID)
		}
	}
	out := make([]string, 0, 4)
	visited := make(map[string]struct{}, 4)
	stack := []string{rootID}
	for len(stack) > 0 {
		cur := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if _, seen := visited[cur]; seen {
			continue
		}
		visited[cur] = struct{}{}
		out = append(out, cur)
		for _, child := range children[cur] {
			if _, seen := visited[child]; seen {
				continue
			}
			stack = append(stack, child)
		}
	}
	return out
}

func toResp(e *navigationmodel.NavigationEntity) *navigationdto.NavigationResp {
	return &navigationdto.NavigationResp{
		ID: e.ID, ProjectID: e.ProjectID, Title: e.Title, Path: e.Path,
		Kind: e.Kind, ParentID: e.ParentID, SortOrder: e.SortOrder,
		SourceType: e.SourceType, SourceID: e.SourceID, Target: e.Target,
		PanelBlockID: e.PanelBlockID, PanelWidth: e.PanelWidth,
		UpdatedAt: e.UpdatedAt.Format(time.RFC3339Nano),
	}
}

func (s *Service) TreeByID(ctx context.Context, projectID, navigationID string) (nodes []*navigationdto.NavigationNode, err error) {
	projectID = strings.TrimSpace(projectID)
	id := strings.TrimSpace(navigationID)
	if projectID == "" || id == "" {
		return nil, navigationenums.Err(navigationenums.ErrInvalidParam)
	}
	item, err := s.m.Get(ctx, projectID, id)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, navigationenums.Err(navigationenums.ErrNotFound)
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
		return nil, navigationenums.Err(navigationenums.ErrNotFound)
	}
	nodes = []*navigationdto.NavigationNode{node}
	s.resolveSourceTitles(ctx, projectID, nodes)
	return nodes, nil
}

func (s *Service) Tree(ctx context.Context, projectID, kind string) (nodes []*navigationdto.NavigationNode, err error) {
	projectID = strings.TrimSpace(projectID)
	kind = strings.TrimSpace(kind)
	if projectID == "" {
		return nil, navigationenums.Err(navigationenums.ErrInvalidParam)
	}
	if !isValidKind(kind) {
		return nil, navigationenums.Err(navigationenums.ErrInvalidKind)
	}
	rows, err := s.m.List(ctx, projectID, kind)
	if err != nil {
		return nil, err
	}
	nodes = buildTree(rows)
	s.resolveSourceTitles(ctx, projectID, nodes)
	return nodes, nil
}

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

func buildNode(n *navigationmodel.NavigationEntity, children map[string][]*navigationmodel.NavigationEntity) *navigationdto.NavigationNode {
	node := &navigationdto.NavigationNode{
		ID: n.ID, Title: n.Title, Path: n.Path,
		SourceType: n.SourceType, SourceID: n.SourceID, Target: n.Target,
		SortOrder:    n.SortOrder,
		PanelBlockID: n.PanelBlockID, PanelWidth: n.PanelWidth,

		UpdatedAt: n.UpdatedAt.Format(time.RFC3339Nano),
	}
	for _, k := range children[n.ID] {
		node.Children = append(node.Children, buildNode(k, children))
	}
	return node
}

type navNodeView struct {
	Title    string
	Path     string
	Children []navNodeView
}

func toNavNodeView(n *navigationdto.NavigationNode) navNodeView {
	v := navNodeView{Title: n.Title, Path: n.Path}
	for _, c := range n.Children {
		v.Children = append(v.Children, toNavNodeView(c))
	}
	return v
}

func normalizePanel(blockID *string, width string) (outID *string, outWidth string, err error) {
	outWidth, err = normalizePanelWidth(width)
	if err != nil {
		return nil, "", err
	}
	return normalizePanelBlockID(blockID), outWidth, nil
}

func normalizePanelBlockID(blockID *string) *string {
	if blockID == nil {
		return nil
	}
	id := strings.TrimSpace(*blockID)
	if id == "" {
		return nil
	}
	return &id
}

func normalizePanelWidth(width string) (string, error) {
	switch strings.TrimSpace(width) {
	case "", "auto":
		return "auto", nil
	case "full":
		return "full", nil
	}
	return "", navigationenums.Err(navigationenums.ErrInvalidParam)
}

func validateField(title, path, kind string) error {
	if title == "" {
		return navigationenums.Err(navigationenums.ErrInvalidParam)
	}
	if !strings.HasPrefix(path, "/") {
		return navigationenums.Err(navigationenums.ErrInvalidParam)
	}
	if !isValidKind(kind) {
		return navigationenums.Err(navigationenums.ErrInvalidKind)
	}
	return nil
}

func isValidKind(kind string) bool {
	return kind == kindHeader || kind == kindHeaderMobile ||
		kind == kindFooter || kind == kindFooterMobile
}

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

func normalizeSource(sourceType string, sourceID *string, target string) (st string, sid *string, tg string, err error) {
	st = normalizeSourceType(sourceType)
	if !isValidSourceType(st) {
		return "", nil, "", navigationenums.Err(navigationenums.ErrInvalidSource)
	}
	tg = normalizeTarget(target)
	if !isValidTarget(tg) {
		return "", nil, "", navigationenums.Err(navigationenums.ErrInvalidTarget)
	}
	sid = normalizeSourceID(sourceID)
	if st == sourceCustom {
		return st, nil, tg, nil
	}
	if sid == nil {
		return "", nil, "", navigationenums.Err(navigationenums.ErrInvalidSource)
	}
	return st, sid, tg, nil
}

func isValidSourceType(v string) bool {
	switch v {
	case sourceCustom, sourcePage, sourceArticle, sourceProduct, sourceCategory, sourceBlock:
		return true
	}
	return false
}

func isValidTarget(v string) bool { return v == targetSelf || v == targetBlank }

func normalizeSourceType(v string) string {
	if v = strings.TrimSpace(v); v == "" {
		return sourceCustom
	}
	return v
}

func normalizeTarget(v string) string {
	if v = strings.TrimSpace(v); v == "" {
		return targetSelf
	}
	return v
}

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

// BulkDelete 在一个事务里删除多个导航项：任一失败整批回滚。
//
// 与单条删除共用同一条删除规则（含子孙项与失效标记），只是把事务边界提到批量外层：
// 一次用户可感知的批量操作要么全成、要么全败。
func (s *Service) BulkDelete(ctx context.Context, req *navigationdto.BulkDeleteReq) error {
	if req == nil || len(req.IDs) == 0 {
		return navigationenums.Err(navigationenums.ErrInvalidParam)
	}
	// 先判形状与上限，再触库：超限在解析 uuid 之前就整批拒绝，
	// 空标识与非法 uuid 也在这里明确拒绝，不让驱动错误变成 500。
	blocked := make([]facing.BlockedItem, 0, len(req.IDs))
	distinct := make([]string, 0, len(req.IDs))
	seenRaw := make(map[string]struct{}, len(req.IDs))
	requested := 0
	for _, raw := range req.IDs {
		id := strings.TrimSpace(raw)
		// 请求项数按 uuid 身份去重后计算：合法重复不能让单条请求变成「多项」；
		// 空标识同样是一个请求项，单条空串仍给该条具体原因而不是公共整批文案。
		identity := id
		if parsed, perr := uuid.Parse(id); perr == nil {
			identity = parsed.String()
		}
		if _, dup := seenRaw[identity]; dup {
			continue
		}
		seenRaw[identity] = struct{}{}
		requested++
		if id == "" {
			blocked = append(blocked, facing.BlockedItem{ID: raw, ReasonKey: navigationenums.ErrInvalidParam})
			continue
		}
		distinct = append(distinct, id)
	}
	if len(blocked) == 0 && len(distinct) > bulkids.MaxIDs {
		return &bulkids.IDsError{Count: len(distinct), Max: bulkids.MaxIDs}
	}
	seen := make(map[string]struct{}, len(distinct))
	rootIDs := make([]string, 0, len(distinct))
	for _, id := range distinct {
		parsed, perr := uuid.Parse(id)
		if perr != nil {
			blocked = append(blocked, facing.BlockedItem{ID: id, ReasonKey: navigationenums.ErrInvalidParam})
			continue
		}
		canonical := parsed.String()
		if _, dup := seen[canonical]; dup {
			continue
		}
		seen[canonical] = struct{}{}
		rootIDs = append(rootIDs, canonical)
	}
	// 空 ID / 不合法项都让整批失败：不推算 skipped、不返回部分成功。
	if len(blocked) > 0 {
		return RejectBatch(requested, blocked)
	}
	if len(rootIDs) == 0 {
		return navigationenums.Err(navigationenums.ErrInvalidParam)
	}
	if len(rootIDs) > bulkids.MaxIDs {
		return &bulkids.IDsError{Count: len(rootIDs), Max: bulkids.MaxIDs}
	}

	// 事务外只定位工程：FORCE RLS 下逐工程探测归属，不 bypass。
	projectID := ""
	for _, id := range rootIDs {
		e, err := s.locateNavigation(ctx, id)
		if err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				blocked = append(blocked, facing.BlockedItem{ID: id, ReasonKey: navigationenums.ErrNotFound})
				continue
			}
			return err
		}
		if projectID == "" {
			projectID = e.ProjectID
		} else if projectID != e.ProjectID {
			// 一个用户可感知的批量删除只作用于一个工程；跨工程整批拒绝。
			return navigationenums.Err(navigationenums.ErrInvalidParam)
		}
	}
	if len(blocked) > 0 {
		return RejectBatch(requested, blocked)
	}

	return s.withMenuWriteTx(ctx, projectID, func(tx *gorm.DB) error {
		return s.deleteNavigationTx(ctx, tx, projectID, rootIDs)
	})
}
