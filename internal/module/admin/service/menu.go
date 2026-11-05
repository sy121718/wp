package adminservice

import (
	"cmp"
	"context"
	"slices"
	"strings"
	"sync"
	"time"

	"go_wp/internal/module/admin/dto"
	"go_wp/internal/module/admin/enums"
	"go_wp/internal/module/admin/model"
	"go_wp/pkg/i18n"

	"go_wp/pkg/tree"

	"gorm.io/gorm"
)

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

func buildMenuParentChoices(parents []adminmodel.MenuParentOption, disabled map[uint64]bool) []admindto.MenuParentChoice {
	byID := make(map[uint64]adminmodel.MenuParentOption, len(parents))
	for _, p := range parents {
		byID[p.ID] = p
	}
	out := make([]admindto.MenuParentChoice, 0, len(parents))
	for _, p := range parents {
		depth := 0
		seen := map[uint64]bool{p.ID: true}

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

const menuParentMaxDepth = 8

type menuPageNode struct {
	row      adminmodel.MenuPageRow
	children []*menuPageNode

	matched        bool
	subtreeMatched bool
}

func buildMenuPageForest(rows []adminmodel.MenuPageRow, matched map[uint64]bool) []*menuPageNode {
	sorted := make([]adminmodel.MenuPageRow, len(rows))
	copy(sorted, rows)
	slices.SortStableFunc(sorted, func(a, b adminmodel.MenuPageRow) int {
		return cmp.Or(cmp.Compare(a.SortOrder, b.SortOrder), cmp.Compare(a.ID, b.ID))
	})
	nodes := make(map[uint64]*menuPageNode, len(sorted))
	for _, r := range sorted {
		nodes[r.ID] = &menuPageNode{row: r, matched: matched != nil && matched[r.ID]}
	}
	roots := make([]*menuPageNode, 0, len(sorted))
	for _, r := range sorted {
		node := nodes[r.ID]

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

func (s *Service) MenuDetail(ctx context.Context, req *admindto.MenuDetailReq) (res *admindto.MenuDetailResp, err error) {
	entity, err := s.mm.GetByID(ctx, req.ID)
	if err != nil {
		return nil, err
	}
	if entity == nil {
		return nil, adminenums.Err(adminenums.ErrMenuNotFound)
	}

	return menuEntityToDetailResp(entity), nil
}

func (s *Service) MenuCreate(ctx context.Context, req *admindto.MenuCreateReq) error {
	if err := s.validatePermissionBinding(req.Type, req.PermissionCodes, ctx); err != nil {
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

	// 建子菜单要在事务里先锁父行再校验层级：
	// 校验读的是父链，若父链在「校验完 → 插入」之间被改挂或删掉，建出来的菜单
	// 就落进了一条已经不成立的父链。锁父行让校验与插入看到同一份状态。
	err := s.mm.Transaction(ctx, func(tx *gorm.DB) error {
		if err := s.lockMenuParentTx(ctx, tx, req.ParentID, 0); err != nil {
			return err
		}
		if err := s.validateMenuPlacementTx(ctx, tx, req.ParentID, req.Type); err != nil {
			return err
		}
		return s.mm.CreateWithPermissionCodesTx(ctx, tx, entity, req.PermissionCodes)
	})
	if err != nil {
		return err
	}
	invalidateMenuCache()
	return nil
}

// lockMenuParentTx 在调用方事务内锁住目标父菜单行（parentID 为 0 表示根级，无需锁）。
//
// 锁序与删除端一致：都按主键 id 加锁。selfID 是重挂场景下正在改的那一行 ——
// 它自己也在这批行里，提前锁住可以避免「改挂自己」与「别人改挂它」交叉。
func (s *Service) lockMenuParentTx(ctx context.Context, tx *gorm.DB, parentID, selfID uint64) error {
	ids := make([]uint64, 0, 2)
	if selfID > 0 {
		ids = append(ids, selfID)
	}
	if parentID > 0 && parentID != selfID {
		ids = append(ids, parentID)
	}
	if len(ids) == 0 {
		return nil
	}
	locked, err := s.mm.LockByIDsTx(ctx, tx, ids)
	if err != nil {
		return err
	}
	if len(locked) != len(ids) {
		// 锁不到 = 目标行在并发里被删掉了，这条改挂/新建已经不成立。
		return adminenums.Err(adminenums.ErrMenuParentNotFound)
	}
	return nil
}

// validateMenuPlacementTx 是 validateMenuPlacement 的事务版：父链读本事务的锁内状态。
func (s *Service) validateMenuPlacementTx(ctx context.Context, tx *gorm.DB, parentID uint64, menuType int) error {
	if parentID == 0 {
		return nil
	}
	_ = menuType
	depth := 1
	cur, err := s.mm.GetByIDTx(ctx, tx, parentID)
	if err != nil {
		return err
	}
	for cur != nil {
		depth++
		if depth > maxNavDepth {
			return adminenums.Err(adminenums.ErrMenuDepthExceeded)
		}
		if cur.ParentID == 0 {
			break
		}
		cur, err = s.mm.GetByIDTx(ctx, tx, cur.ParentID)
		if err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) MenuUpdate(ctx context.Context, req *admindto.MenuUpdateReq) error {
	entity, err := s.mm.GetByID(ctx, req.ID)
	if err != nil {
		return err
	}
	if entity == nil {
		return adminenums.Err(adminenums.ErrMenuNotFound)
	}

	if entity.IsSystem == 1 && entity.Type != req.Type {
		return adminenums.Err(adminenums.ErrMenuIsSystem)
	}

	if req.ParentID == req.ID {
		return adminenums.Err(adminenums.ErrMenuCircle)
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

	// 重挂同样在事务里锁「自己 + 新父」两行再校验：
	// 只锁自己挡不住别人同时在改挂它，只锁父挡不住自己这一行被别人改。
	err = s.mm.Transaction(ctx, func(tx *gorm.DB) error {
		if err := s.lockMenuParentTx(ctx, tx, req.ParentID, req.ID); err != nil {
			return err
		}
		if err := s.validateMenuPlacementTx(ctx, tx, req.ParentID, req.Type); err != nil {
			return err
		}
		return s.mm.UpdateWithPermissionCodesTx(ctx, tx, entity, req.PermissionCodes)
	})
	if err != nil {
		return err
	}
	invalidateMenuCache()
	return nil
}

// MenuDelete 删除菜单，单条与批量走同一条全成全败路径。
func (s *Service) MenuDelete(ctx context.Context, req *admindto.MenuDeleteReq) error {
	return s.MenuBulkDelete(ctx, req)
}

func (s *Service) validatePermissionBinding(menuType int, codes []string, ctx context.Context) error {
	unique := adminmodel.DedupePermissionCodes(codes)

	if menuType == adminmodel.MenuTypeDirectory ||
		menuType == adminmodel.MenuTypeIframe ||
		menuType == adminmodel.MenuTypeExternal {
		if len(unique) > 0 {
			return adminenums.Err(adminenums.ErrCodeNotBindable)
		}
		return nil
	}

	if menuType == adminmodel.MenuTypeMenu || menuType == adminmodel.MenuTypeButton {
		if len(unique) == 0 {
			return adminenums.Err(adminenums.ErrCodeRequired)
		}

		for _, code := range unique {
			ok, err := s.ExistsEnabledCode(ctx, code)
			if err != nil {
				return err
			}
			if !ok {
				return adminenums.Err(adminenums.ErrCodeNotEnabled)
			}
		}
	}
	return nil
}

const maxNavDepth = 3

func (s *Service) validateMenuPlacement(ctx context.Context, parentID uint64, menuType int) error {
	if parentID == 0 {
		return nil
	}
	_ = menuType
	depth := 1
	cur, err := s.mm.GetByID(ctx, parentID)
	if err != nil {
		return err
	}
	for cur != nil {
		depth++
		if depth > maxNavDepth {
			return adminenums.Err(adminenums.ErrMenuDepthExceeded)
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

func (s *Service) menuCheckCircle(ctx context.Context, targetID, newParentID uint64) error {

	cursor := newParentID
	for cursor != 0 {
		if cursor == targetID {
			return adminenums.Err(adminenums.ErrMenuCircle)
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

func menuDefaultStatus(status int) int {
	if status == 0 {
		return adminmodel.MenuStatusEnabled
	}
	return status
}

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

func (s *Service) MenuTree(ctx context.Context, req *admindto.MenuTreeReq) ([]admindto.MenuTreeNode, error) {
	all, err := s.mm.ListAll(ctx)
	if err != nil {
		return nil, err
	}

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

func buildMenuTree(list []adminmodel.MenuEntity) []admindto.MenuTreeNode {
	ptrs := make([]*admindto.MenuTreeNode, 0, len(list))
	for _, m := range list {
		n := menuEntityToNode(m)
		ptrs = append(ptrs, &n)
	}
	roots := tree.Build(ptrs)
	out := make([]admindto.MenuTreeNode, 0, len(roots))
	for _, r := range roots {
		out = append(out, *r)
	}
	return out
}

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

func (s *Service) GetPermissionCodesByIDs(ctx context.Context, menuIDs []uint64) ([]string, error) {
	byMenu, err := s.mm.ListPermissionCodesByMenuIDs(ctx, menuIDs)
	if err != nil {
		return nil, err
	}
	return flattenMenuPermissionCodes(byMenu), nil
}

// menuPermissionCodesTx 是 GetPermissionCodesByIDs 的事务版，供授予端在同一事务里取码。
//
// 授予端先锁菜单行，再要读「这些菜单现在挂哪些权限码」：非事务版读的是另一条连接，
// 与本事务的锁不在同一个事务边界内，读到的是锁外面的状态。
func (s *Service) menuPermissionCodesTx(ctx context.Context, tx *gorm.DB, menuIDs []uint64) ([]string, error) {
	byMenu, err := s.mm.ListPermissionCodesByMenuIDsTx(ctx, tx, menuIDs)
	if err != nil {
		return nil, err
	}
	return flattenMenuPermissionCodes(byMenu), nil
}

// flattenMenuPermissionCodes 把「菜单 → 权限码」按菜单 id 升序、码首次出现顺序摊平去重。
func flattenMenuPermissionCodes(byMenu map[uint64][]string) []string {
	ids := make([]uint64, 0, len(byMenu))
	for id := range byMenu {
		ids = append(ids, id)
	}
	slices.Sort(ids)

	seen := make(map[string]struct{})
	codes := make([]string, 0, len(ids))
	for _, id := range ids {
		for _, code := range byMenu[id] {
			if _, ok := seen[code]; ok {
				continue
			}
			seen[code] = struct{}{}
			codes = append(codes, code)
		}
	}
	return codes
}

func matchedMenuCodes(codes []string, codeSet map[string]struct{}) []string {
	var hit []string
	for _, code := range codes {
		if _, ok := codeSet[code]; ok {
			hit = append(hit, code)
		}
	}
	return hit
}

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
	slices.Sort(out)
	return out
}

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

	filtered := make([]adminmodel.MenuEntity, 0, len(keep))
	for _, m := range all {
		if _, ok := keep[m.ID]; ok {
			filtered = append(filtered, m)
		}
	}
	return buildMenuTree(filtered)
}

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

func (s *Service) CountByPermissionCodes(ctx context.Context, codes []string) (count int64, err error) {
	ids, err := s.GetIDsByPermissionCodes(ctx, codes)
	if err != nil {
		return 0, err
	}
	return int64(len(ids)), nil
}

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

func invalidateMenuCache() {
	menuCacheMu.Lock()
	menuCache = nil
	menuCacheAt = time.Time{}
	menuCacheMu.Unlock()
}
