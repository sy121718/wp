package adminservice

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	admindto "go_wp/internal/module/admin/dto"
	adminenums "go_wp/internal/module/admin/enums"
	adminmodel "go_wp/internal/module/admin/model"
	"go_wp/pkg/auth"
	"go_wp/pkg/casbin"
	"go_wp/pkg/database"
	"go_wp/pkg/logger"
	"go_wp/pkg/utils"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
)

// AdminList 管理员分页列表。
func (s *Service) AdminList(ctx context.Context, req *admindto.AdminListReq) (res *admindto.AdminListResp, err error) {
	//返回的总条数，go需要提前准备容器
	var total int64
	page := req.GetPage()
	limit := req.GetLimit()

	//默认排除超管
	query := s.am.DB(ctx).Where("is_admin != ?", 1)

	if email := strings.TrimSpace(req.Email); email != "" {
		query = query.Where("email LIKE ? ESCAPE '\\'", "%"+database.EscapeLikePattern(email)+"%")
	}
	if name := strings.TrimSpace(req.Name); name != "" {
		query = query.Where("name LIKE ? ESCAPE '\\'", "%"+database.EscapeLikePattern(name)+"%")
	}
	if req.Status != nil {
		query = query.Where("status = ?", *req.Status)
	}

	if err = query.Count(&total).Error; err != nil {
		return nil, err
	}

	// 排序字段/方向白名单：禁止直接拼接请求值进 ORDER BY（SQL 注入）。
	orderClause := "id DESC"
	if req.SortField != "" && req.SortOrder != "" {
		field := strings.ToLower(strings.TrimSpace(req.SortField))
		dir := strings.ToUpper(strings.TrimSpace(req.SortOrder))
		if dir != "ASC" && dir != "DESC" {
			return nil, errors.New(adminenums.ErrSortDirectionInvalid)
		}
		switch field {
		case "id", "username", "name", "email", "phone", "status", "create_time", "last_login_time":
			orderClause = field + " " + dir
		default:
			return nil, errors.New(adminenums.ErrSortFieldInvalid)
		}
	}

	list := make([]adminmodel.AdminEntity, 0, limit)
	if err = query.
		Order(orderClause).
		Offset((page - 1) * limit).
		Limit(limit).
		Find(&list).Error; err != nil {
		return nil, err
	}

	items := make([]admindto.AdminItem, len(list))
	for i, entity := range list {
		items[i] = admindto.AdminItem{
			ID:         entity.ID,
			Username:   entity.Username,
			Name:       entity.Name,
			Avatar:     entity.Avatar,
			Email:      entity.Email,
			Phone:      entity.Phone,
			Status:     entity.Status,
			CreateTime: utils.NewJSONTimePtr(entity.CreateTime),
		}
	}

	res = &admindto.AdminListResp{
		Total: total,
		List:  items,
	}

	return
}

// AdminCreate 新建管理员。
func (s *Service) AdminCreate(ctx context.Context, req *admindto.AdminCreateReq) (res *admindto.AdminCreateResp, err error) {
	if emailExists, err := database.IsFieldExists(s.am.DB(ctx), &adminmodel.AdminEntity{}, "email", req.Email); err != nil {
		return nil, err
	} else if emailExists {
		return nil, errors.New(adminenums.ErrEmailExists)
	}
	if nameExists, err := database.IsFieldExists(s.am.DB(ctx), &adminmodel.AdminEntity{}, "username", req.Username); err != nil {
		return nil, err
	} else if nameExists {
		return nil, errors.New(adminenums.ErrUsernameExists)
	}

	// 加密密码
	hashed, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		return nil, err
	}

	// 构造实体
	// *string 类型的字段不能直接赋 string 值，需要用 & 取地址
	entity := &adminmodel.AdminEntity{
		Username: req.Username,
		Password: string(hashed),
		Email:    &req.Email,
		Status:   adminmodel.AdminStatusActive,
		Name:     &req.Username, // Username 必填，直接赋值
	}
	// 只要有接收值并且可选字段的（omitempty）：有值才写，没值保持 nil → 数据库写 NULL，
	if req.Phone != "" {
		if phoneExists, err := database.IsFieldExists(s.am.DB(ctx), &adminmodel.AdminEntity{}, "phone", req.Phone); err != nil {
			return nil, err
		} else if phoneExists {
			return nil, errors.New(adminenums.ErrPhoneExists)
		}
		entity.Phone = &req.Phone
	}
	if req.Avatar != "" {
		entity.Avatar = &req.Avatar
	}

	if err := s.am.DB(ctx).Create(entity).Error; err != nil {
		return nil, err
	}
	res = &admindto.AdminCreateResp{
		ID:       entity.ID,
		Username: entity.Username,
	}

	return
}

// AdminDetail 查询管理员详情。
func (s *Service) AdminDetail(ctx context.Context, req *admindto.AdminDetailReq) (res *admindto.AdminDetailResp, err error) {
	res = &admindto.AdminDetailResp{}

	err = s.am.DB(ctx).
		Select("id", "username", "name", "avatar", "email", "phone", "status", "is_admin",
			"register_ip", "register_location", "last_login_ip", "last_login_location",
			"last_login_time", "create_by", "create_time", "remark").
		Where("id = ?", req.Id).
		Scan(res).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errors.New(adminenums.ErrAdminNotFound)
		}
		return nil, err
	}
	// Scan 不命中不返回 gorm.ErrRecordNotFound（RowsAffected=0），须显式判空：
	// 避免查询不存在返回空结构 + nil error（对比 perm_crud.go 同款检查）。
	if res.ID == 0 {
		return nil, errors.New(adminenums.ErrAdminNotFound)
	}

	res.Roles = []any{}
	res.Menus = []any{}
	return
}

// AdminEdit 修改管理员信息，并使旧会话失效。
func (s *Service) AdminEdit(ctx context.Context, req *admindto.AdminEditReq) (res *admindto.AdminEditResp, err error) {
	entityExists, err := s.am.GetByID(ctx, req.Id)
	if err != nil {
		return nil, err
	}
	if entityExists == nil {
		return nil, errors.New(adminenums.ErrAdminNotFound)
	}

	//判断邮箱唯一
	if emailExists, err := database.IsFieldExists(s.am.DB(ctx), &adminmodel.AdminEntity{}, "email", req.Email, req.Id); err != nil {
		return nil, err
	} else if emailExists {
		return nil, errors.New(adminenums.ErrEmailExists)
	}

	// 判断用户名唯一
	if nameExists, err := database.IsFieldExists(s.am.DB(ctx), &adminmodel.AdminEntity{}, "username", req.Username, req.Id); err != nil {
		return nil, err
	} else if nameExists {
		return nil, errors.New(adminenums.ErrUsernameExists)
	}

	// 构造实体
	// *string 类型的字段不能直接赋 string 值，需要用 & 取地址
	entity := &adminmodel.AdminEntity{
		Username: req.Username,
		Email:    &req.Email,
	}
	if req.Phone != "" {
		// 判断手机号码是否唯一
		if phoneExists, err := database.IsFieldExists(s.am.DB(ctx), &adminmodel.AdminEntity{}, "phone", req.Phone, req.Id); err != nil {
			return nil, err
		} else if phoneExists {
			return nil, errors.New(adminenums.ErrPhoneExists)
		}
		entity.Phone = &req.Phone
	}

	if req.Remark != "" {
		entity.Remark = &req.Remark
	}

	// 执行更新
	if err := s.am.DB(ctx).Where("id = ?", req.Id).Updates(entity).Error; err != nil {
		return nil, err
	}
	// 使旧会话失效：管理员资料已保存成功，这是主操作；会话失效失败只记日志，
	// 不将整个操作视为失败（避免调用方收到错误但改动已生效的部分失败语义）。
	if err := auth.DeleteUserSession(ctx, req.Id); err != nil {
		logger.Scene("admin").With("user_id", req.Id).Error(err, "编辑管理员后失效旧会话失败")
	}

	res = &admindto.AdminEditResp{
		ID: req.Id,
	}
	return
}

// AdminDelete 删除普通管理员，并撤销其会话和授权。
//
// 事务：sys_admin 行与 sys_casbin_rule 里这批账号的全部策略行（p 直接权限 + g 角色绑定）
// 是两处持久化写，必须同事务。旧实现先提交删除、再逐条删策略：中途失败就留下
// 「管理员没了、策略还在」（残留下一次同名同 id 复用即静默继承旧权限），
// 反向的半截状态（策略先删、实体没删）则是账号还在却已失权。
//
// 三个动作分属三种存储边界，按这个顺序：
//  1. 事务外**只读**校验 —— 给出可读错误，并保证「被拒的删除请求」不会先撤掉目标账号的
//     会话（撤会话是不可回滚的 Redis 写，被拒的请求不该有副作用）；
//  2. 撤销会话（Redis，**在事务之外**，不能用事务覆盖）：删除提交后会话若还在，
//     已删除账号仍能凭 Redis 会话通过认证。失败即中止（fail closed）。
//     反向的半截状态（会话已撤、账号还在）只是要求重新登录，不产生权限真空；
//  3. 一个数据库事务：**行锁复核** + 删 sys_admin 行 + 删策略行，任一步失败整体回滚。
func (s *Service) AdminDelete(ctx context.Context, req *admindto.AdminDeleteReq) (res *admindto.AdminDeleteResp, err error) {
	ids := uniqueAdminIDs(req.Id)
	if len(ids) == 0 {
		return nil, errors.New(adminenums.ErrAdminNotFound)
	}

	for _, id := range ids {
		entity, err := s.am.GetByID(ctx, id)
		if err != nil {
			return nil, err
		}
		if entity == nil {
			return nil, errors.New(adminenums.ErrAdminNotFound)
		}
		if id == req.OperatorID {
			return nil, errors.New(adminenums.ErrDeleteSelf)
		}
		if entity.IsSuperAdmin() {
			return nil, errors.New(adminenums.ErrDeleteSuperAdmin)
		}
	}

	// 先撤销旧会话，确保后续任一步骤失败时都不会继续放行已删除账号。
	for _, id := range ids {
		if err = auth.RevokeUserSession(ctx, id); err != nil {
			return nil, err
		}
	}

	reloadPolicy := false
	var deleted int64
	err = s.am.Transaction(ctx, func(tx *gorm.DB) error {
		// 行锁复核：从上面那次校验到这里之间，目标行可能已被并发改成超管 ——
		// 不加锁的校验会被绕过（AGENTS.md：读-改-写必须有行锁或原子 SQL）。
		locked, err := s.am.LockByIDsTx(ctx, tx, ids)
		if err != nil {
			return err
		}
		if len(locked) != len(ids) {
			return errors.New(adminenums.ErrAdminNotFound)
		}
		for _, entity := range locked {
			if entity.ID == req.OperatorID {
				return errors.New(adminenums.ErrDeleteSelf)
			}
			if entity.IsSuperAdmin() {
				return errors.New(adminenums.ErrDeleteSuperAdmin)
			}
		}

		deleted, err = s.am.DeleteByIDsTx(ctx, tx, ids)
		if err != nil {
			return err
		}
		if deleted != int64(len(ids)) {
			return errors.New(adminenums.ErrAdminNotFound)
		}

		// 该账号的全部策略行与 sys_admin 行同事务（跨模块只传 *gorm.DB 给 …Tx 方法）。
		for _, id := range ids {
			changed, err := casbin.DeleteUserAllPoliciesTx(ctx, tx, strconv.FormatUint(id, 10))
			if err != nil {
				return fmt.Errorf("管理员行已删但策略清理失败，本次删除已整体回滚: %w", err)
			}
			reloadPolicy = reloadPolicy || changed
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	if reloadPolicy {
		if err = casbin.ReloadPolicy(); err != nil {
			return nil, fmt.Errorf("管理员已删除，但权限策略重载失败（内存副本仍是旧策略，请重试或重启服务）: %w", err)
		}
	}
	return &admindto.AdminDeleteResp{DeletedCount: deleted}, nil
}

// --- 部门关联 ---

// AdminListByDeptID 按部门 ID 分页查询管理员列表。
func (s *Service) AdminListByDeptID(ctx context.Context, req *admindto.AdminListByDeptIDReq) (res *admindto.AdminListByDeptIDResp, err error) {
	page := req.GetPage()
	limit := req.GetLimit()

	query := s.am.DB(ctx).Where("dept_id = ?", req.DeptID)

	var total int64
	if err = query.Count(&total).Error; err != nil {
		return nil, err
	}

	var entities []adminmodel.AdminEntity
	offset := (page - 1) * limit
	if err = query.Select("id, username, name, email, status").
		Offset(offset).Limit(limit).
		Order("id ASC").
		Find(&entities).Error; err != nil {
		return nil, err
	}

	list := make([]admindto.DeptAdminItem, 0, len(entities))
	for _, e := range entities {
		name := ""
		if e.Name != nil {
			name = *e.Name
		}
		email := ""
		if e.Email != nil {
			email = *e.Email
		}
		list = append(list, admindto.DeptAdminItem{
			ID:       e.ID,
			Username: e.Username,
			Name:     name,
			Email:    email,
			Status:   e.Status,
		})
	}
	return &admindto.AdminListByDeptIDResp{Total: total, List: list}, nil
}

// AdminBatchSetDeptID 批量设置用户的部门 ID。
func (s *Service) AdminBatchSetDeptID(ctx context.Context, req *admindto.AdminBatchSetDeptIDReq) error {
	if len(req.UserIDs) == 0 {
		return nil
	}
	return s.am.DB(ctx).Model(&adminmodel.AdminEntity{}).
		Where("id IN ?", req.UserIDs).
		Update("dept_id", req.DeptID).Error
}

// AdminCountByDeptID 统计部门下管理员数量。
func (s *Service) AdminCountByDeptID(ctx context.Context, deptID uint64) (int64, error) {
	var count int64
	err := s.am.DB(ctx).Where("dept_id = ?", deptID).Count(&count).Error
	return count, err
}

func uniqueAdminIDs(ids []uint64) []uint64 {
	seen := make(map[uint64]struct{}, len(ids))
	result := make([]uint64, 0, len(ids))
	for _, id := range ids {
		if id == 0 {
			continue
		}
		if _, exists := seen[id]; exists {
			continue
		}
		seen[id] = struct{}{}
		result = append(result, id)
	}
	return result
}

// --- 超管判定 helper（RBAC 提权保护共用） ---

// IsSuperAdmin 查询指定管理员是否为超管（is_admin=1）。
// 用户不存在或 userID 为 0 时按非超管处理（存在性由调用方自行校验）。
func (s *Service) IsSuperAdmin(ctx context.Context, userID uint64) (bool, error) {
	if userID == 0 {
		return false, nil
	}
	entity, err := s.am.GetByID(ctx, userID)
	if err != nil {
		return false, err
	}
	if entity == nil {
		return false, nil
	}
	return entity.IsSuperAdmin(), nil
}

// roleHasSuperAdminPermission 判定目标角色是否「含超管权限」：
// 角色的权限点集合覆盖全部启用权限点（sys_permission.status=1）时视为超管角色，
// 与 031 迁移「超管 is_admin=1 全量业务策略」的语义对齐。
// 权限点全集为空（权限体系未配置/未 seed）时保守判定为 false，
// 避免空集被任意角色恒真覆盖导致保护失效。
func (s *Service) roleHasSuperAdminPermission(ctx context.Context, roleCodes []string) (bool, error) {
	if len(roleCodes) == 0 {
		return false, nil
	}
	all, err := s.pm.IsEnabledAll(ctx)
	if err != nil {
		return false, err
	}
	if len(all) == 0 {
		return false, nil
	}

	codes := make(map[string]struct{})
	for _, rc := range roleCodes {
		perms, err := casbin.GetRolePermissions(rc)
		if err != nil {
			return false, err
		}
		for _, p := range perms {
			codes[p[2]] = struct{}{}
		}
	}
	for _, p := range all {
		if _, ok := codes[p.PermissionCode]; !ok {
			return false, nil
		}
	}
	return true, nil
}

// codesCoverAllEnabled 判定一个权限点集合是否覆盖全部启用权限点（超管等价）。
// 权限点全集为空（权限体系未配置/未 seed）时保守判定为 false，
// 避免空集被任意权限点集合恒真覆盖导致保护失效。
func (s *Service) codesCoverAllEnabled(ctx context.Context, codes []string) (bool, error) {
	all, err := s.pm.IsEnabledAll(ctx)
	if err != nil {
		return false, err
	}
	if len(all) == 0 {
		return false, nil
	}
	set := make(map[string]struct{}, len(codes))
	for _, c := range codes {
		set[c] = struct{}{}
	}
	for _, p := range all {
		if _, ok := set[p.PermissionCode]; !ok {
			return false, nil
		}
	}
	return true, nil
}

// requireSuperAdminForSensitiveTarget 目标敏感（超管账号或超管角色）时，
// 当前操作者必须也是超管，否则返回 ErrSuperAdminOnly 拒绝操作。
// 目标不敏感时直接放行（不要求操作者为超管，保持既有普通授权流程）。
func (s *Service) requireSuperAdminForSensitiveTarget(ctx context.Context, operatorID uint64, targetSensitive bool) error {
	if !targetSensitive {
		return nil
	}
	opSuper, err := s.IsSuperAdmin(ctx, operatorID)
	if err != nil {
		return err
	}
	if !opSuper {
		return errors.New(adminenums.ErrSuperAdminOnly)
	}
	return nil
}
