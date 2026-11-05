package navigationservice

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"go_wp/internal/module/navigation/dto"
	"go_wp/internal/module/navigation/enums"
	"go_wp/internal/module/navigation/model"
	"go_wp/internal/templates"
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

	e, err := s.locateNavigation(ctx, id)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return navigationenums.Err(navigationenums.ErrNotFound)
	}
	if err != nil {
		return err
	}

	at := time.Now().UTC()
	return s.withMenuWriteTx(ctx, e.ProjectID, func(tx *gorm.DB) error {

		rows, lerr := s.m.ListTx(ctx, tx, e.ProjectID, e.Kind)
		if lerr != nil {
			return lerr
		}
		if derr := s.m.DeleteManyTx(ctx, tx, e.ProjectID, navDescendantIDs(rows, id)); derr != nil {
			return derr
		}
		if merr := s.markMenuStaleInTx(ctx, tx, e.ProjectID, e.Kind, at); merr != nil {
			return merr
		}
		return s.markNavigationStaleInTx(ctx, tx, e.ProjectID, e.ID)
	})
}

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
	entities := make([]*navigationmodel.NavigationEntity, 0, len(req.IDs))
	projectID := ""
	for _, raw := range req.IDs {
		id := strings.TrimSpace(raw)
		if id == "" {
			continue
		}
		e, err := s.locateNavigation(ctx, id)
		if err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return navigationenums.Err(navigationenums.ErrNotFound)
			}
			return err
		}
		if projectID == "" {
			projectID = e.ProjectID
		} else if projectID != e.ProjectID {
			return navigationenums.Err(navigationenums.ErrInvalidParam)
		}
		entities = append(entities, e)
	}
	if len(entities) == 0 {
		return navigationenums.Err(navigationenums.ErrInvalidParam)
	}

	at := time.Now().UTC()
	return s.withMenuWriteTx(ctx, projectID, func(tx *gorm.DB) error {
		rows, lerr := s.m.ListTx(ctx, tx, projectID, entities[0].Kind)
		if lerr != nil {
			return lerr
		}
		for _, e := range entities {
			ids := navDescendantIDs(rows, e.ID)
			if derr := s.m.DeleteManyTx(ctx, tx, projectID, ids); derr != nil {
				return derr
			}
			if nerr := s.markNavigationStaleInTx(ctx, tx, projectID, e.ID); nerr != nil {
				return nerr
			}
		}
		return s.markMenuStaleInTx(ctx, tx, projectID, entities[0].Kind, at)
	})
}
