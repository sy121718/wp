package adminservice

import (
	"context"
	"errors"
	"regexp"
	"strings"

	admindto "go_wp/internal/module/admin/dto"
	adminenums "go_wp/internal/module/admin/enums"
	adminmodel "go_wp/internal/module/admin/model"
)

// MenuPage keeps the management page bounded while retaining the complete parent selector.
func (s *Service) MenuPage(ctx context.Context, page, limit int, keyword string) (*admindto.MenuPageResp, error) {
	parents, err := s.mm.ListParentOptions(ctx)
	if err != nil {
		return nil, err
	}
	total, list, err := s.mm.ListPage(ctx, page, limit, keyword)
	if err != nil {
		return nil, err
	}
	byID := make(map[uint64]adminmodel.MenuParentOption, len(parents))
	for _, p := range parents {
		byID[p.ID] = p
	}
	resp := &admindto.MenuPageResp{Total: total, Rows: make([]admindto.MenuPageRow, 0, len(list)), Parents: make([]admindto.MenuParentChoice, 0, len(parents))}
	for _, p := range parents {
		depth := 0
		seen := map[uint64]bool{p.ID: true}
		for id := p.ParentID; id != 0 && depth < 8; {
			parent, ok := byID[id]
			if !ok || seen[id] {
				break
			}
			seen[id] = true
			depth++
			id = parent.ParentID
		}
		resp.Parents = append(resp.Parents, admindto.MenuParentChoice{ID: p.ID, Title: p.Title, Type: p.Type, Indent: strings.Repeat("　", depth)})
	}
	for _, item := range list {
		parentTitle := ""
		if parent, ok := byID[item.ParentID]; ok {
			parentTitle = parent.Title
		}
		remark := ""
		if item.Remark != nil {
			remark = *item.Remark
		}
		resp.Rows = append(resp.Rows, admindto.MenuPageRow{
			ID: item.ID, Title: item.Title, Path: item.Path, Type: item.Type, ParentID: item.ParentID,
			ParentTitle: parentTitle, Status: item.Status, SortOrder: item.SortOrder, Remark: remark, Icon: item.Icon,
			PermissionCodes: item.PermissionCodes,
		})
	}
	return resp, nil
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
	req.Component = strings.TrimSpace(req.Component)
	if err := validateComponentBinding(req.Type, req.Component); err != nil {
		return err
	}
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
		Component:   req.Component,
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

	req.Component = strings.TrimSpace(req.Component)
	if err := validateComponentBinding(req.Type, req.Component); err != nil {
		return err
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
	entity.Component = req.Component
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

// component 兼容三种格式：
//   - soybean 目录：layout.base
//   - soybean 叶子页面：view.xxx
//   - 旧 vue-pure-admin 路径：/src/views/xxx/index.vue
var componentPathPattern = regexp.MustCompile(`^(?:layout\.(?:base|blank)|view\.[A-Za-z0-9_-]+|/src/views/(?:[A-Za-z0-9_-]+/)*[A-Za-z0-9_-]+\.vue)$`)

// validateComponentBinding 校验菜单类型与前端组件路径的绑定关系。
func validateComponentBinding(menuType int, component string) error {
	if menuType != adminmodel.MenuTypeMenu {
		if component != "" {
			return errors.New(adminenums.ErrComponentNotAllowed)
		}
		return nil
	}
	if component == "" {
		return errors.New(adminenums.ErrComponentRequired)
	}
	if !componentPathPattern.MatchString(component) {
		return errors.New(adminenums.ErrComponentInvalid)
	}
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
		Component:       e.Component,
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
		Component:       m.Component,
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
