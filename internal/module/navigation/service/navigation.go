package navigationservice

// 写路径先校验父引用与来源三件套、再落库并派发失效；Update/Delete 定位一律带工程作用域。

// 扁平实体列表 → 父子树 → Jet 片段；构建期编译与管理页结构面板共用这棵树。

// 含 kind/path 基础规则、父引用合法性（自引用 / 成环 / 跨工程 / 跨类型）与
// 来源、打开方式、悬浮面板三组白名单归一化，取值口径与迁移里的 CHECK 约束对齐。

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

// Create 新建导航项：校验 kind/path 基础规则 + 同工程同 kind 同 path 唯一。
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
	// 父引用（不存在的父 / 跨工程父 / 跨类型父会让该节点在构建期树装配时成为孤儿，
	// 永远挂不上根、产物里静默消失）在事务内、锁内校验 —— 见下面的 validateParentTx。
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
	// 写入 + 两类失效在**同一个事务、同一把工程内容写锁**下完成：
	// 新菜单项会改变该位置产出的 HTML（位置键），只放某一支的页面只登记了项键
	//（navigation:{itemID}），两条键都要打；任一步失败整笔回滚，绝不留
	// 「菜单已建、产物没失效」或「失效已标、菜单回滚」的半截状态。
	at := time.Now().UTC()
	var out *navigationmodel.NavigationEntity
	if werr := s.withMenuWriteTx(ctx, e.ProjectID, func(tx *gorm.DB) error {
		// 唯一性与排序取号都在**锁内**：锁外查重、锁内写会让两个并发 Create 都通过检查
		//（同 path 重复）或拿到同一个 max+1（同级排序并列）。
		// 父引用同样在锁内复验：锁外校验通过、锁内父项刚被删掉，就会建出孤儿节点。
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
		// 排序：未显式指定（0）时追加到同级末尾，保证同级 sort_order 唯一，
		// 管理页的「上移/下移」才能稳定交换。
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
		// 回读必须**在事务内**：库列是 timestamptz（微秒精度），而内存里的 time.Now() 带纳秒。
		// 直接 toResp(e) 会把纳秒精度的 update_time 当乐观锁 token 发出去，调用方原样回带时
		// 与库内值不等（SaveWithExpected 是 WHERE update_time = ?），表现为「刚建好就报版本
		// 冲突」；把内存值截断到微秒也不行 —— PG 对 timestamptz(6) 的纳秒是**四舍五入**，
		// 截断会差 1 微秒、token 照样比不中。回读是唯一可靠的做法。
		//
		// 放在事务内（提交前）而不是提交后：写已提交、回读却失败时若返 5xx，调用方重试会
		// **新建第二条**。放进事务里，回读失败就整笔回滚 —— 失败即「什么都没发生」，重试安全。
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

// Update 更新导航项：仅更新传入的非空字段，变更 path/kind 时重校验唯一性。
//
// 结构（与 Create 同形）：锁外**只定位归属**（请求只带 id，要先逐工程探测出 project_id），
// 锁内用 GetForUpdateTx 重读当前行（FOR UPDATE），再基于**当前值**合并 req、校验、判新旧
// kind、写、失效、回读。锁外那份实体只用来拿 ProjectID/ID，其余字段一律不参与判定。
//
// 为什么不能在锁外算好再进锁：从「定位」到「拿到工程内容写锁」之间，另一个写者可能已把
// 这一行从 header 改到 footer。用锁外快照的 kind 去校验父引用/路径唯一性会校验错的维度；
// 用锁外快照的旧 kind 打失效，真正被改掉的那个位置一条 stale 都不派发 —— 症状是「保存
// 成功、某个位置的页面/实例停在旧菜单上」，且没有任何错误可查。
func (s *Service) Update(ctx context.Context, req *navigationdto.UpdateReq) (res *navigationdto.NavigationResp, err error) {
	if req == nil || strings.TrimSpace(req.ID) == "" {
		return nil, navigationenums.Err(navigationenums.ErrInvalidParam)
	}
	// 锁外只定位归属（逐工程探测，请求不带工程）。拿到 ProjectID 之后一切都在事务内。
	located, err := s.locateNavigation(ctx, strings.TrimSpace(req.ID))
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, navigationenums.Err(navigationenums.ErrNotFound)
	}
	if err != nil {
		return nil, err
	}
	projectID, navID := located.ProjectID, located.ID

	// 请求侧能算的东西先算（纯函数，不依赖库内值）。与库内值**合并**推迟到锁内。
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
	// 来源三字段联动校验要读到「当前值」，所以整段推迟到锁内（见下）。
	sourceGiven := req.SourceType != nil || req.SourceID != nil || req.Target != nil
	// normalizePanelBlockID 已经返回 *string 且空白归一为 nil（与 Create / 渲染期同一口径），
	// 这里不能再取地址：&v 是 **string，与 updates["panel_block_id"] 的 *string 语义不符，
	// 还会把"选了空块"重新变成非 nil 指针。
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
	// 乐观锁期望值同样先解析（纯字符串处理）。
	expected, perr := parseExpectedUpdatedAt(req.ExpectedUpdatedAt)
	if perr != nil {
		return nil, perr
	}

	at := time.Now().UTC()
	var out *navigationmodel.NavigationEntity
	if werr := s.withMenuWriteTx(ctx, projectID, func(tx *gorm.DB) error {
		// 锁内重读当前行（FOR UPDATE）：以下所有判定都基于这一份快照。
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
		// 来源与打开方式：三字段联动，未给出的那两个沿用**当前值**（不是锁外快照）。
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
			// 面板块必写：请求给了该字段就以归一结果为准，归一为 nil 时把 NULL 落库（清除面板）。
			if panelBlockID == nil {
				updates["panel_block_id"] = nil
			} else {
				updates["panel_block_id"] = *panelBlockID
			}
		}
		if panelWidth != nil {
			updates["panel_width"] = *panelWidth
		}

		// 父引用与路径唯一性都用**当前 kind** 判定（kind 可能刚被本次改掉）。
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
			// 受影响行数 0：行刚在锁内读到过（不存在已在上面返回），所以只可能是
			// 「库内 update_time 已经不是期望值」——另一处入口改过这一项。
			rows, serr := s.m.SaveWithExpectedTx(ctx, tx, projectID, navID, updates, *expected)
			if serr != nil {
				return serr
			}
			if rows == 0 {
				return staleVersionMessage(s.staleTitle(ctx, cur))
			}
		}
		// 新旧两个位置都失效：位置可能被一起改（header ↔ footer），旧位置的产物里同样
		// 烘着这份菜单，只失效新位置会让旧页面上留着已经改掉的菜单项。cur.Kind 是锁内
		// 读到的**当前**旧位置，不是锁外快照。
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
		// 回读也在事务内：写已成功却因回读失败而返 5xx，会让调用方重试一次「同样的写」。
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
		// 事务体没有产出实体：装配层出问题（keep an explicit failure instead of nil deref）。
		return nil, navigationenums.Err(navigationenums.ErrNotFound)
	}
	return toResp(out), nil
}

// Get 按 ID 查询导航项。
func (s *Service) Get(ctx context.Context, req *navigationdto.GetReq) (res *navigationdto.NavigationResp, err error) {
	if req == nil || strings.TrimSpace(req.ID) == "" {
		return nil, navigationenums.Err(navigationenums.ErrInvalidParam)
	}
	// 导航项详情只有 id 可依（请求不带工程）：逐工程探测出归属，再在本工程作用域内读。
	// 原先的「不限工程」形态在换非超级角色后是静默的 ErrNotFound，且没有任何错误日志。
	e, err := s.locateNavigation(ctx, strings.TrimSpace(req.ID))
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, navigationenums.Err(navigationenums.ErrNotFound)
	}
	if err != nil {
		return nil, err
	}
	return toResp(e), nil
}

// List 按工程（可选 kind）列出导航项，sort_order 升序。
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

// Delete 删除导航项及其全部子项（导航树是一个聚合：留下孤儿节点会被渲染成顶级项）。
func (s *Service) Delete(ctx context.Context, req *navigationdto.DeleteReq) (err error) {
	if req == nil || strings.TrimSpace(req.ID) == "" {
		return navigationenums.Err(navigationenums.ErrInvalidParam)
	}
	id := strings.TrimSpace(req.ID)
	// 定位这一跳逐工程探测归属（请求只给 id）；拿到实体后的磁盘动作全部带工程作用域：
	// 列表与批量删除都在本工程内，删到别的工程的行在换角色后会被策略拒绝（DB-009）。
	e, err := s.locateNavigation(ctx, id)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return navigationenums.Err(navigationenums.ErrNotFound)
	}
	if err != nil {
		return err
	}
	// 「读子树 → 批量删 → 失效」在同一事务、同一把锁内：锁外读子树、锁内删的写法会在
	// 两步之间漏掉别人刚挂上来的子项（留下孤儿节点，构建期被渲染成顶级项）。
	// 被删的整棵子树会从该位置的产物里消失（位置键），按项引用该项的页面同样要重建
	//（它现在渲染不出根了，项键）。
	at := time.Now().UTC()
	return s.withMenuWriteTx(ctx, e.ProjectID, func(tx *gorm.DB) error {
		// 子树在**锁内**读：锁外读、锁内删会在两步之间漏掉别人刚挂上来的子项。
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

// toResp 实体 → 响应。
//
// UpdatedAt 是乐观锁 token（RFC3339Nano，UpdateReq.ExpectedUpdatedAt 原样回带），与树节点
// NavigationNode.UpdatedAt 同义 —— 同一个值、同一个口径（这里原先发的是分钟展示串，
// 与树节点同名两义，正是「拿展示串当 token」那个陷阱的温床）。
//
// 要求 e 是库内真值：Create 因此回读后再转换（纳秒精度的内存值比不中微秒列）。
func toResp(e *navigationmodel.NavigationEntity) *navigationdto.NavigationResp {
	return &navigationdto.NavigationResp{
		ID: e.ID, ProjectID: e.ProjectID, Title: e.Title, Path: e.Path,
		Kind: e.Kind, ParentID: e.ParentID, SortOrder: e.SortOrder,
		SourceType: e.SourceType, SourceID: e.SourceID, Target: e.Target,
		PanelBlockID: e.PanelBlockID, PanelWidth: e.PanelWidth,
		UpdatedAt: e.UpdatedAt.Format(time.RFC3339Nano),
	}
}

// TreeByID 按具体菜单项 id 返回该菜单项及其子树（core.nav 按项引用时用）。
//
// 取数策略：先按 id 定位该项（带工程归属校验），再一次性取**同位置**的全部行拼子树 ——
// 菜单规模很小（几十行），一次 List 比按 parent_id 逐层查询少 N 次 SQL，而这条路径
// 在构建期会被每个引用该菜单的页面各走一次。
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

// Tree 按工程 + 位置返回导航项树（sort_order 升序，同序按 id 升序）。
// 构建期编译导航组件与管理页结构面板共用同一棵树。
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

// normalizePanel 归一化悬浮面板字段（超级菜单，迁移 285）。
//
// 空白块 id 一律归一成 nil（"没选块" 与 "选了空" 是同一个状态，留空串会让
// 渲染期误以为有面板、去解析一个不存在的块 id）。宽度白名单与 DDL CHECK 对齐，
// 越界在这里显式拒绝，而不是让库层约束报错（那会变成 500 而不是可读的参数错误）。
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

// validateField 校验 title/path/kind 基础规则（不含唯一性）。
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

// isValidKind 判断导航类型是否为受支持的位置（桌面 / 移动端各自的页眉与页脚）。
func isValidKind(kind string) bool {
	return kind == kindHeader || kind == kindHeaderMobile ||
		kind == kindFooter || kind == kindFooterMobile
}

// 父引用校验的判据（不存在 / 跨工程 / 跨类型 / 自引用 / 成环一律 ErrInvalidParent）只在
// validateParentTx 一处实现：它读的是**调用方事务内**的行集合，"所有行同一快照"这件事
// 顺带把成环检测变成一次子树比对（旧实现逐跳上溯 Get，每一跳都是另一个事务快照）。
//
// 为什么旧实现被删掉而不是留着备用：两处实现同一规则时，任何一侧改动都会让另一侧悄悄
// 漂移；而「环上的节点永远到不了根 → 整棵子树在产物里静默消失（后台列表照常显示）」
// 正是这条规则存在的理由，判据不允许有两份。

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
