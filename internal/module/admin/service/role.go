package adminservice

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strconv"

	"gorm.io/gorm"

	"go_wp/internal/module/admin/dto"
	"go_wp/internal/module/admin/enums"
	"go_wp/internal/module/admin/model"
	"go_wp/pkg/casbin"
	"go_wp/pkg/database"
)

var pureNumericPattern = regexp.MustCompile(`^[0-9]+$`)

// RoleList 角色分页列表。
func (s *Service) RoleList(ctx context.Context, req *admindto.RoleListReq) (res *admindto.RoleListResp, err error) {
	total, entities, err := s.rm.ListAll(ctx, req.GetPage(), req.GetLimit(), req.Keyword)
	if err != nil {
		return nil, err
	}

	list := make([]admindto.RoleItem, 0, len(entities))
	for _, e := range entities {
		list = append(list, roleEntityToItem(e))
	}
	return &admindto.RoleListResp{Total: total, List: list}, nil
}

// RoleDetail 角色详情。
func (s *Service) RoleDetail(ctx context.Context, req *admindto.RoleDetailReq) (res *admindto.RoleDetailResp, err error) {
	entity, err := s.rm.GetByID(ctx, req.ID)
	if err != nil {
		return nil, err
	}
	if entity == nil {
		return nil, errors.New(adminenums.ErrRoleNotFound)
	}
	return roleEntityToDetailResp(entity), nil
}

// RoleCreate 新建角色。
//
// 事务：sys_role 行与 sys_casbin_rule 的 g2 启用标记是两处持久化写入，必须同事务。
// 旧实现先提交角色行、再写 g2，g2 写失败就留下「角色已建但没启用标记」——
// 角色看起来启用（status=1）却对所有人不生效，且没有任何回滚。
//
// 唯一性：事务外的重复检查只为给出可读错误，真正防并发重复的是 sys_role.role_code 上的
// 唯一索引（并发插入失败会原样上抛，不静默合并/改码）。
func (s *Service) RoleCreate(ctx context.Context, req *admindto.RoleCreateReq) error {
	if pureNumericPattern.MatchString(req.RoleCode) {
		return errors.New(adminenums.ErrRoleCodeNumeric)
	}

	existing, err := s.rm.GetByCode(ctx, req.RoleCode)
	if err != nil {
		return err
	}
	if existing != nil {
		return errors.New(adminenums.ErrRoleCodeExists)
	}

	entity := &adminmodel.RoleEntity{
		RoleCode:  req.RoleCode,
		RoleName:  req.RoleName,
		SortOrder: req.SortOrder,
	}
	// Status 未显式传（nil）时默认启用；显式传 0（禁用）尊重之，不再强制改写。
	if req.Status != nil {
		entity.Status = *req.Status
	} else {
		entity.Status = adminmodel.RoleStatusEnabled
	}
	if req.Remark != "" {
		entity.Remark = &req.Remark
	}

	reloadPolicy := false
	err = s.rm.Transaction(ctx, func(tx *gorm.DB) error {
		if err := s.rm.CreateTx(ctx, tx, entity); err != nil {
			return err
		}
		// 新建角色默认启用：写入 g2（与角色行同事务）
		if entity.Status != adminmodel.RoleStatusEnabled {
			return nil
		}
		changed, err := casbin.ActivateRoleTx(ctx, tx, entity.RoleCode)
		if err != nil {
			return fmt.Errorf("角色已建但启用标记写入失败，本次创建已整体回滚: %w", err)
		}
		reloadPolicy = changed
		return nil
	})
	if err != nil {
		return err
	}
	if reloadPolicy {
		if err = casbin.ReloadPolicy(); err != nil {
			return fmt.Errorf("角色已创建，但权限策略重载失败（内存副本仍是旧策略，请重试或重启服务）: %w", err)
		}
	}
	// 角色已落库：同步重载数据权限快照（快照内含「启用角色 code → id」映射，
	// 否则新建的角色分配给它的规则要等下一次兜底刷新才生效）。
	s.reloadDataRuleSnapshotAfterWrite("角色新建")
	return nil
}

// RoleUpdate 更新角色元信息。
//
// 事务：sys_role.status 与 sys_casbin_rule 的 g2 启用标记必须同事务。旧实现先提交角色行、
// 再动 g2：**禁用角色时 g2 删除失败，库里已禁用而 g2 还在 —— 该角色下所有人仍被放行**
// （禁用没生效，且错误被吞在响应里，运维看不出）。现在任一步失败整体回滚，禁用要么完整生效、
// 要么角色保持启用，不会停在「一半」。
//
// 读-改-写加行锁（LockByIDTx）：要先读旧 status 才能判断该不该动 g2，不加锁并发启停会互相覆盖。
func (s *Service) RoleUpdate(ctx context.Context, req *admindto.RoleUpdateReq) error {
	reloadPolicy := false
	err := s.rm.Transaction(ctx, func(tx *gorm.DB) error {
		entity, err := s.rm.LockByIDTx(ctx, tx, req.ID)
		if err != nil {
			return err
		}
		if entity == nil {
			return errors.New(adminenums.ErrRoleNotFound)
		}

		oldStatus := entity.Status
		entity.RoleName = req.RoleName
		entity.Status = req.Status
		entity.SortOrder = req.SortOrder
		if req.Remark != "" {
			entity.Remark = &req.Remark
		} else {
			entity.Remark = nil
		}

		if err = s.rm.UpdateTx(ctx, tx, entity); err != nil {
			return err
		}

		// 状态变更同步到 Casbin g2（与角色行同事务）
		if oldStatus == entity.Status {
			return nil
		}
		changed := false
		if entity.Status == adminmodel.RoleStatusEnabled {
			changed, err = casbin.ActivateRoleTx(ctx, tx, entity.RoleCode)
		} else {
			changed, err = casbin.DeactivateRoleTx(ctx, tx, entity.RoleCode)
		}
		if err != nil {
			return fmt.Errorf("角色状态已改但启用标记同步失败，本次修改已整体回滚: %w", err)
		}
		reloadPolicy = changed
		return nil
	})
	if err != nil {
		return err
	}
	if reloadPolicy {
		if err = casbin.ReloadPolicy(); err != nil {
			return fmt.Errorf("角色已保存，但权限策略重载失败（内存副本仍是旧策略，请重试或重启服务）: %w", err)
		}
	}
	// 角色已更新（含启停）：同步重载数据权限快照，启用状态变化立刻反映到角色映射上。
	s.reloadDataRuleSnapshotAfterWrite("角色更新")
	return nil
}

// RoleDelete 删除角色。
// 系统内置角色不可删除。
// 普通角色删除时：删除 sys_role 行 + Casbin 层面清理 p/g/g2。
//
// 事务：sys_role 行与 sys_casbin_rule 里该角色的全部策略行（p 拥有权限 / g 用户绑定 /
// g2 启用标记）是两处持久化写，必须同事务。旧实现先删策略、后删实体：策略删完而实体
// 删除失败，就留下「角色还在、权限一个不剩」—— 该角色下所有人瞬间失权，而报错只回给
// 这一次请求，列表页上完全看不出异常（与 PermUpdate / RoleUpdate 同一形态）。
// 现在任一步失败整体回滚：要么角色与策略一起消失，要么都保持原样。
//
// 读-改-写加行锁（LockByIDTx）：先读 is_system 才能判断能不能删，不加锁时并发改
// 「普通角色 → 系统角色」会让保护失效。
func (s *Service) RoleDelete(ctx context.Context, req *admindto.RoleDeleteReq) error {
	reloadPolicy := false
	err := s.rm.Transaction(ctx, func(tx *gorm.DB) error {
		entity, err := s.rm.LockByIDTx(ctx, tx, req.ID)
		if err != nil {
			return err
		}
		if entity == nil {
			return errors.New(adminenums.ErrRoleNotFound)
		}
		if entity.IsSystem == 1 {
			return errors.New(adminenums.ErrRoleIsSystem)
		}

		// Casbin 清理（p / g / g2）与该角色的 sys_role 行同事务
		changed, err := casbin.DeleteRoleAllPoliciesTx(ctx, tx, entity.RoleCode)
		if err != nil {
			return fmt.Errorf("角色策略清理失败，本次删除已整体回滚: %w", err)
		}
		if err = s.rm.DeleteTx(ctx, tx, req.ID); err != nil {
			return err
		}
		reloadPolicy = changed
		return nil
	})
	if err != nil {
		return err
	}
	if reloadPolicy {
		if err = casbin.ReloadPolicy(); err != nil {
			return fmt.Errorf("角色已删除，但权限策略重载失败（内存副本仍是旧策略，请重试或重启服务）: %w", err)
		}
	}
	// 角色已删除：同步重载数据权限快照，避免已删角色继续参与规则命中匹配。
	s.reloadDataRuleSnapshotAfterWrite("角色删除")
	return nil
}

// GetRoleCodesByUserID 对外契约：查询用户绑定且已启用的角色编码列表。
func (s *Service) GetRoleCodesByUserID(ctx context.Context, userID uint64) ([]string, error) {
	codes, err := casbin.GetRoleCodesByUserID(strconv.FormatUint(userID, 10))
	if err != nil || len(codes) == 0 {
		return codes, err
	}

	roles, err := s.rm.ListByCodes(ctx, codes)
	if err != nil {
		return nil, err
	}
	enabled := make(map[string]struct{}, len(roles))
	for _, role := range roles {
		if role.Status == adminmodel.RoleStatusEnabled {
			enabled[role.RoleCode] = struct{}{}
		}
	}

	activeCodes := make([]string, 0, len(enabled))
	for _, code := range codes {
		if _, ok := enabled[code]; ok {
			activeCodes = append(activeCodes, code)
		}
	}
	return activeCodes, nil
}

// roleEntityToItem RoleEntity 转列表项 RoleItem。
func roleEntityToItem(e adminmodel.RoleEntity) admindto.RoleItem {
	return admindto.RoleItem{
		ID:        e.ID,
		RoleCode:  e.RoleCode,
		RoleName:  e.RoleName,
		Status:    e.Status,
		IsSystem:  e.IsSystem,
		SortOrder: e.SortOrder,
		Remark:    roleRemark(e.Remark),
	}
}

// roleEntityToDetailResp RoleEntity 转详情响应 RoleDetailResp。
func roleEntityToDetailResp(e *adminmodel.RoleEntity) *admindto.RoleDetailResp {
	return &admindto.RoleDetailResp{
		ID:        e.ID,
		RoleCode:  e.RoleCode,
		RoleName:  e.RoleName,
		Status:    e.Status,
		IsSystem:  e.IsSystem,
		SortOrder: e.SortOrder,
		Remark:    roleRemark(e.Remark),
	}
}

// roleRemark 解引用可选备注字段。
func roleRemark(remark *string) string {
	if remark != nil {
		return *remark
	}
	return ""
}

// RoleMenuList 查询角色拥有的菜单 ID 列表。
// 通过 Casbin p 策略反查角色拥有的 permission_codes，
// 再通过菜单反查对应的 menu_ids。
func (s *Service) RoleMenuList(ctx context.Context, req *admindto.RoleMenuListReq) (res *admindto.RoleMenuListResp, err error) {
	role, err := s.rm.GetByID(ctx, req.RoleID)
	if err != nil {
		return nil, err
	}
	if role == nil {
		return nil, errors.New(adminenums.ErrRoleNotFound)
	}

	// 获取角色在 Casbin 中的全部 p 策略
	permissions, err := casbin.GetRolePermissions(role.RoleCode)
	if err != nil {
		return nil, err
	}

	// 提取 permission_code（p 策略的第 3 个元素）
	codes := make([]string, 0, len(permissions))
	for _, p := range permissions {
		codes = append(codes, p[2])
	}

	// 反查 menu_ids
	menuIDs, err := s.GetIDsByPermissionCodes(ctx, codes)
	if err != nil {
		return nil, err
	}

	// 向上补齐祖先（目录 + 父菜单）。目录没有权限码，反查永远查不出它们，
	// 不补的话分配页每次打开都会显示「目录未勾选」（详见 withAncestorMenuIDs）。
	all, err := s.mm.ListAll(ctx)
	if err != nil {
		return nil, err
	}
	menuIDs = withAncestorMenuIDs(all, menuIDs)

	return &admindto.RoleMenuListResp{MenuIDs: menuIDs}, nil
}

// RoleMenuSave 全量替换角色菜单授权。
// 流程：menu_ids → permission_codes → [path, method, code] → Casbin ReplaceRolePermissions。
//
// 超管保护（审计项「RBAC 提权无超管保护」）：新授权权限点集合覆盖全部启用权限点
// （目标角色将变为超管等价角色），或目标角色当前已是超管等价角色时，仅超管可操作——
// 防止普通管理员给自己所在角色写入全量权限点完成提权。
func (s *Service) RoleMenuSave(ctx context.Context, req *admindto.RoleMenuSaveReq) (res *admindto.RoleMenuSaveResp, err error) {
	role, err := s.rm.GetByID(ctx, req.RoleID)
	if err != nil {
		return nil, err
	}
	if role == nil {
		return nil, errors.New(adminenums.ErrRoleNotFound)
	}

	// menu_ids → permission_codes。
	//
	// 先过一遍 withAncestorMenuIDs 有两个作用：
	//   ① 与读取侧（RoleMenuList / RolePermissionTree）保持同一不变式 —— 即使前端漏补祖先，
	//      落库的权限集仍然自洽，不会出现「有按钮权限点、没有父菜单权限点」的分裂授权；
	//   ② 充当白名单：不在 sys_menus 里的 id 在这里就被丢掉，不会进入下面的 IN 查询。
	//      请求体不受信任，100 万个 id 的提交只做哈希查表，不进 SQL。
	all, err := s.mm.ListAll(ctx)
	if err != nil {
		return nil, err
	}
	menuIDs := withAncestorMenuIDs(all, req.MenuIDs)

	codes, err := s.GetPermissionCodesByIDs(ctx, menuIDs)
	if err != nil {
		return nil, err
	}

	// 超管保护：新权限集是否覆盖全部启用权限点，目标角色当前是否已超管等价
	willBeSuper, err := s.codesCoverAllEnabled(ctx, codes)
	if err != nil {
		return nil, err
	}
	roleSuper, err := s.roleHasSuperAdminPermission(ctx, []string{role.RoleCode})
	if err != nil {
		return nil, err
	}
	if err = s.requireSuperAdminForSensitiveTarget(ctx, req.OperatorID, willBeSuper || roleSuper); err != nil {
		return nil, err
	}

	// permission_codes → [path, method, code] 三元组
	var policies [][3]string
	if len(codes) > 0 {
		briefs, err := s.ListByCodes(ctx, codes)
		if err != nil {
			return nil, err
		}
		for _, b := range briefs {
			policies = append(policies, [3]string{b.APIPath, b.APIMethod, b.PermissionCode})
		}
	}

	// Casbin 全量替换
	if err = casbin.ReplaceRolePermissions(role.RoleCode, policies); err != nil {
		return nil, fmt.Errorf("保存角色权限失败: %w", err)
	}

	return &admindto.RoleMenuSaveResp{RoleID: req.RoleID}, nil
}

// RoleUserList 查询角色下的用户列表。
// 通过 Casbin g 策略反查 user_ids。
func (s *Service) RoleUserList(ctx context.Context, req *admindto.RoleUserListReq) (res *admindto.RoleUserListResp, err error) {
	role, err := s.rm.GetByID(ctx, req.RoleID)
	if err != nil {
		return nil, err
	}
	if role == nil {
		return nil, errors.New(adminenums.ErrRoleNotFound)
	}

	// Casbin 反查用户 ID
	userIDStrs, err := casbin.GetUserIDsByRoleCode(role.RoleCode)
	if err != nil {
		return nil, err
	}

	// 转换为 uint64
	userIDs := make([]uint64, 0, len(userIDStrs))
	for _, s := range userIDStrs {
		id, err := strconv.ParseUint(s, 10, 64)
		if err != nil {
			continue
		}
		userIDs = append(userIDs, id)
	}

	// 内存分页
	total := int64(len(userIDs))
	start := (req.GetPage() - 1) * req.GetLimit()
	end := start + req.GetLimit()
	if start > int(total) {
		start = int(total)
	}
	if end > int(total) {
		end = int(total)
	}
	pageIDs := userIDs[start:end]

	// 同包直调 admin 详情查询用户信息
	list := make([]admindto.RoleUserItem, 0, len(pageIDs))
	for _, id := range pageIDs {
		detail, err := s.AdminDetail(ctx, &admindto.AdminDetailReq{Id: id})
		if err != nil || detail == nil {
			// 查不到时只返回 ID
			list = append(list, admindto.RoleUserItem{ID: id})
			continue
		}
		list = append(list, admindto.RoleUserItem{
			ID:       id,
			Username: detail.Username,
			Name:     detail.Name,
			Email:    detail.Email,
			Status:   detail.Status,
		})
	}

	return &admindto.RoleUserListResp{Total: total, List: list}, nil
}

// RoleUserSave 全量替换角色用户绑定。
//
// 超管保护（审计项「RBAC 提权无超管保护」）：目标角色含超管权限
// （权限集覆盖全部启用权限点），或目标用户列表含超管账号时，仅超管可操作——
// 防止普通管理员把任意账号加进超管角色提权，或改绑超管账号。
func (s *Service) RoleUserSave(ctx context.Context, req *admindto.RoleUserSaveReq) (res *admindto.RoleUserSaveResp, err error) {
	role, err := s.rm.GetByID(ctx, req.RoleID)
	if err != nil {
		return nil, err
	}
	if role == nil {
		return nil, errors.New(adminenums.ErrRoleNotFound)
	}

	roleSuper, err := s.roleHasSuperAdminPermission(ctx, []string{role.RoleCode})
	if err != nil {
		return nil, err
	}
	targetSuper := false
	for _, id := range req.UserIDs {
		super, err := s.IsSuperAdmin(ctx, id)
		if err != nil {
			return nil, err
		}
		if super {
			targetSuper = true
			break
		}
	}
	if err = s.requireSuperAdminForSensitiveTarget(ctx, req.OperatorID, roleSuper || targetSuper); err != nil {
		return nil, err
	}

	// 转换 userIDs → string
	userIDStrs := make([]string, 0, len(req.UserIDs))
	for _, id := range req.UserIDs {
		userIDStrs = append(userIDStrs, strconv.FormatUint(id, 10))
	}

	// Casbin 全量替换 g 策略
	if err = casbin.ReplaceRoleUsers(role.RoleCode, userIDStrs); err != nil {
		return nil, fmt.Errorf("保存角色用户失败: %w", err)
	}

	return &admindto.RoleUserSaveResp{RoleID: req.RoleID}, nil
}

// RolePermissionTree 返回角色权限分配所需的完整授权树与该角色当前的勾选集合。
//
// 这是「角色分权」的唯一读入口，页面（/admin/roles/permissions）与 JSON 接口共用它。
// 权限来源只有一份（sys_casbin_rule 里 sub=role_code 的 p 策略）：
//
//	p 策略的 code 列 → 反查 sys_menus.permission_code → menu_id 集合 → 向上补齐祖先
//
// 目录（type=1）没有权限码，反查永远查不出它们；按钮（type=3）的父菜单也不会自动
// 出现在结果里 —— 两件事都由 withAncestorMenuIDs 补齐（那里有完整论证）。
//
// 树里包含**已被禁用的已勾选项**：角色先勾、菜单后禁用的情况下，如果树里没有它，
// 页面渲染不出这个勾选，管理员一保存就把授权静默删掉了（见 buildPermissionTree）。
func (s *Service) RolePermissionTree(ctx context.Context, roleID uint64) (res *admindto.RolePermissionTreeResp, err error) {
	role, err := s.rm.GetByID(ctx, roleID)
	if err != nil {
		return nil, err
	}
	if role == nil {
		return nil, errors.New(adminenums.ErrRoleNotFound)
	}

	all, err := s.mm.ListAll(ctx)
	if err != nil {
		return nil, err
	}

	permissions, err := casbin.GetRolePermissions(role.RoleCode)
	if err != nil {
		return nil, err
	}
	codes := make([]string, 0, len(permissions))
	for _, p := range permissions {
		codes = append(codes, p[2])
	}

	ids, err := s.GetIDsByPermissionCodes(ctx, codes)
	if err != nil {
		return nil, err
	}
	checked := withAncestorMenuIDs(all, ids)

	return &admindto.RolePermissionTreeResp{
		RoleID:   role.ID,
		RoleCode: role.RoleCode,
		RoleName: role.RoleName,
		MenuIDs:  checked,
		Tree:     buildPermissionTree(all, checked),
	}, nil
}

// PermList 权限点分页查询。
func (s *Service) PermList(ctx context.Context, req *admindto.PermListReq) (res *admindto.PermListResp, err error) {
	query := s.pm.DB(ctx)

	if req.Module != "" {
		query = query.Where("module = ?", req.Module)
	}
	if req.Code != "" {
		query = query.Where("permission_code LIKE ? ESCAPE '\\'", "%"+database.EscapeLikePattern(req.Code)+"%")
	}
	if req.APIPath != "" {
		query = query.Where("api_path LIKE ? ESCAPE '\\'", "%"+database.EscapeLikePattern(req.APIPath)+"%")
	}
	if req.Status != nil {
		query = query.Where("status = ?", *req.Status)
	}

	var total int64
	if err = query.Count(&total).Error; err != nil {
		return nil, err
	}

	var items []admindto.PermItem
	offset := (req.GetPage() - 1) * req.GetLimit()
	err = query.Order("id DESC").
		Offset(offset).Limit(req.GetLimit()).
		Scan(&items).Error
	if err != nil {
		return nil, err
	}

	// 补齐 remark 的空值处理
	list := make([]admindto.PermItem, 0, len(items))
	for _, item := range items {
		list = append(list, item)
	}

	return &admindto.PermListResp{Total: total, List: list}, nil
}

// PermDetail 查询单个权限点详情。
func (s *Service) PermDetail(ctx context.Context, req *admindto.PermDetailReq) (res *admindto.PermDetailResp, err error) {
	res = &admindto.PermDetailResp{}
	err = s.pm.DB(ctx).
		Select("id", "permission_code", "permission_name", "module", "api_path", "api_method",
			"status", "remark", "create_time", "update_time").
		Where("id = ?", req.ID).
		Scan(res).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errors.New(adminenums.ErrPermissionNotFound)
		}
		return nil, err
	}
	if res.ID == 0 {
		return nil, errors.New(adminenums.ErrPermissionNotFound)
	}
	return res, nil
}

// PermOptions 返回启用权限选项（供菜单表单选择 permission_code）。
func (s *Service) PermOptions(ctx context.Context, req *admindto.PermOptionsReq) (res *admindto.PermOptionsResp, err error) {
	var entities []adminmodel.PermissionEntity
	query := s.pm.DB(ctx).Where("status = ?", adminmodel.PermissionStatusEnabled)
	if req.Module != "" {
		query = query.Where("module = ?", req.Module)
	}
	err = query.Order("module ASC, id ASC").Find(&entities).Error
	if err != nil {
		return nil, err
	}

	list := make([]admindto.PermOptionItem, 0, len(entities))
	for _, e := range entities {
		list = append(list, admindto.PermOptionItem{
			ID:             e.ID,
			PermissionCode: e.PermissionCode,
			PermissionName: e.PermissionName,
			Module:         e.Module,
			APIPath:        e.APIPath,
			APIMethod:      e.APIMethod,
		})
	}
	return &admindto.PermOptionsResp{List: list}, nil
}

// PermCreate 新建权限点。
func (s *Service) PermCreate(ctx context.Context, req *admindto.PermCreateReq) (res *admindto.PermCreateResp, err error) {
	// 检查 code 唯一性
	existing, err := s.pm.GetByCode(ctx, req.PermissionCode)
	if err != nil {
		return nil, err
	}
	if existing != nil {
		return nil, errors.New(adminenums.ErrCodeExists)
	}

	entity := &adminmodel.PermissionEntity{
		PermissionCode: req.PermissionCode,
		PermissionName: req.PermissionName,
		Module:         req.Module,
		APIPath:        req.APIPath,
		APIMethod:      req.APIMethod,
		Status:         req.Status,
	}
	if entity.Status == 0 {
		entity.Status = adminmodel.PermissionStatusEnabled
	}
	if req.Remark != "" {
		entity.Remark = &req.Remark
	}

	if err = s.pm.Create(ctx, entity); err != nil {
		return nil, err
	}

	return &admindto.PermCreateResp{ID: entity.ID}, nil
}

// PermUpdate 更新权限点定义，并同步已分配的 Casbin 策略。
//
// 事务：sys_permission 行与 sys_casbin_rule 里该权限点的 p 行是**两处持久化写入**，
// 必须同事务（见 AGENTS.md「写操作的事务与回滚」）。旧实现分两次提交、失败时手工回滚
// 权限点行，手工回滚本身再失败就会留下「旧策略已删、新策略没写完」——该权限点连带
// 超管全员 403（072/077/078/079 踩过的同一形态）。现在两处写在一个事务里，
// 任一步失败由数据库整体回滚，不再需要补偿。
//
// 读-改-写加行锁（LockByIDTx）：先读旧定义、再算新定义、再写回，不加锁会与并发更新互相覆盖。
// 策略行的刷新是派生动作：事务提交成功后重建 Enforcer 内存副本（回滚时不动副本，两者一致）。
func (s *Service) PermUpdate(ctx context.Context, req *admindto.PermUpdateReq) (res *admindto.PermUpdateResp, err error) {
	reloadPolicy := false
	err = s.pm.Transaction(ctx, func(tx *gorm.DB) error {
		entity, err := s.pm.LockByIDTx(ctx, tx, req.ID)
		if err != nil {
			return err
		}
		if entity == nil {
			return errors.New(adminenums.ErrPermissionNotFound)
		}
		if req.PermissionCode != entity.PermissionCode {
			return errors.New(adminenums.ErrCodeImmutable)
		}

		assigned, err := casbin.HasPermissionPolicies(entity.PermissionCode)
		if err != nil {
			return err
		}
		if req.Status == adminmodel.PermissionStatusDisabled && assigned {
			return errors.New(adminenums.ErrPermissionAssigned)
		}

		definitionChanged := req.APIPath != entity.APIPath || req.APIMethod != entity.APIMethod
		entity.PermissionName = req.PermissionName
		entity.Module = req.Module
		entity.APIPath = req.APIPath
		entity.APIMethod = req.APIMethod
		entity.Status = req.Status
		if req.Remark != "" {
			entity.Remark = &req.Remark
		} else {
			entity.Remark = nil
		}

		if err = s.pm.UpdateTx(ctx, tx, entity); err != nil {
			return err
		}
		if !definitionChanged || !assigned {
			return nil
		}
		changed, err := casbin.ReplacePermissionDefinitionTx(ctx, tx, entity.PermissionCode, entity.APIPath, entity.APIMethod)
		if err != nil {
			return fmt.Errorf("权限点已改但策略同步失败，本次修改已整体回滚: %w", err)
		}
		reloadPolicy = changed
		return nil
	})
	if err != nil {
		return nil, err
	}
	if reloadPolicy {
		if err = casbin.ReloadPolicy(); err != nil {
			return nil, fmt.Errorf("权限点已保存，但权限策略重载失败（内存副本仍是旧策略，请重试或重启服务）: %w", err)
		}
	}
	return &admindto.PermUpdateResp{ID: req.ID}, nil
}

// PermDelete 批量删除未分配的权限点。
func (s *Service) PermDelete(ctx context.Context, req *admindto.PermDeleteReq) (res *admindto.PermDeleteResp, err error) {
	entities, err := s.pm.ListByIDs(ctx, req.IDs)
	if err != nil {
		return nil, err
	}
	if len(entities) != len(req.IDs) {
		return nil, errors.New(adminenums.ErrPermissionNotFound)
	}
	codes := make([]string, 0, len(entities))
	for _, entity := range entities {
		assigned, err := casbin.HasPermissionPolicies(entity.PermissionCode)
		if err != nil {
			return nil, err
		}
		if assigned {
			return nil, errors.New(adminenums.ErrPermissionAssigned)
		}
		codes = append(codes, entity.PermissionCode)
	}

	// 同包直调菜单引用检查，不再有未初始化问题
	references, err := s.CountByPermissionCodes(ctx, codes)
	if err != nil {
		return nil, err
	}
	if references > 0 {
		return nil, errors.New(adminenums.ErrMenuReferenced)
	}

	deleted, err := s.pm.DeleteByIDs(ctx, req.IDs)
	if err != nil {
		return nil, err
	}
	return &admindto.PermDeleteResp{DeletedCount: deleted}, nil
}

// ListByCodes 按 permission_code 列表批量查权限摘要。
// 供 menu/role/admin 把 code 转换为 path/method。
func (s *Service) ListByCodes(ctx context.Context, codes []string) ([]admindto.PermBrief, error) {
	entities, err := s.pm.ListByCodes(ctx, codes)
	if err != nil {
		return nil, err
	}

	result := make([]admindto.PermBrief, 0, len(entities))
	for _, e := range entities {
		result = append(result, admindto.PermBrief{
			PermissionCode: e.PermissionCode,
			APIPath:        e.APIPath,
			APIMethod:      e.APIMethod,
		})
	}
	return result, nil
}

// ExistsEnabledCode 检查 code 是否存在且启用。
func (s *Service) ExistsEnabledCode(ctx context.Context, code string) (bool, error) {
	return s.pm.ExistsEnabledCode(ctx, code)
}
