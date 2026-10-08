package adminservice

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"

	"go_wp/internal/module/admin/dto"
	"go_wp/internal/module/admin/enums"
	"go_wp/internal/module/admin/model"
	"go_wp/pkg/auth"
	"go_wp/pkg/captcha"
	"go_wp/pkg/casbin"
	"go_wp/pkg/database"
	"go_wp/pkg/logger"
	"go_wp/pkg/utils"
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

// AdminLogin 管理员登录，返回会话信息（由 handler 写入 cookie session）。
//
// 流程：
//  1. 验证图形验证码
//  2. 查数据库，找不到用户返回模糊错误
//  3. 检查是否被锁定 / 被动禁用
//  4. bcrypt 对比密码
//  5. 失败：累加失败次数，连续 5 次锁定 30 分钟（只写 locked_until_time，到期自动解锁，不改 status）
//  6. 成功：清空失败状态，记录登录 IP 和时间，生成会话 ID 并写入 Redis
func (s *Service) AdminLogin(ctx context.Context, req *admindto.AdminLoginReq, clientIP string) (*admindto.AdminLoginResp, error) {
	// 1) 验证验证码
	var captchaSvc = captcha.Get()
	// Verify-验证验证码是否正确
	if !captchaSvc.Verify(req.CaptchaID, req.Captcha, true) {
		return nil, errors.New(adminenums.ErrCaptchaExpired)
	}

	// 2) 按用户名查用户（区分大小写）
	var entity adminmodel.AdminEntity
	// 二进制比较保证大小写敏感：MySQL 用 BINARY；
	// PostgreSQL 默认 collation 本身大小写敏感，直接等值比较。
	binaryExpr := "CAST(username AS BINARY) = CAST(? AS BINARY)"
	switch s.am.DialectName() {
	case "postgres":
		binaryExpr = "username = ?"
	}
	if err := s.am.DB(ctx).Where(binaryExpr+" OR email = ?", req.Username, req.Username).First(&entity).Error; err != nil {
		logger.Scene("admin").With("username", req.Username).With("reason", "用户不存在").Warn("登录失败")
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errors.New(adminenums.ErrBadCredentials)
		}
		return nil, err
	}

	// 3) 检查是否被锁定
	if entity.IsLocked() {
		logger.Scene("admin").With("username", req.Username).With("reason", "账号已锁定").Warn("登录失败")
		// key|param 协议：ErrAccountLocked 翻译模板含 %s，参数随错误消息传递（pkg/response 统一格式化）。
		// 区分锁定与凭据错误是产品设计（H1 回归测试锁定），账号枚举由验证码前置缓解。
		return nil, fmt.Errorf("%s|%s", adminenums.ErrAccountLocked,
			time.Until(*entity.LockedUntilTime).Round(time.Minute).String())
	}

	// 4) 检查是否被禁用
	if !entity.IsActive() {
		logger.Scene("admin").With("username", req.Username).With("reason", "账号已禁用").Warn("登录失败")
		return nil, errors.New(adminenums.ErrAccountDisabled)
	}

	// 5) 密码校验
	if err := bcrypt.CompareHashAndPassword([]byte(entity.Password), []byte(req.Password)); err != nil {
		logger.Scene("admin").With("username", req.Username).With("reason", "密码错误").Warn("登录失败")
		if ferr := s.am.IncrementLoginFailure(ctx, entity.ID, loginFailureLockThreshold, loginFailureLockDuration); ferr != nil {
			logger.Scene("admin").Error(ferr, "记录登录失败状态失败")
		}
		return nil, errors.New(adminenums.ErrBadCredentials)
	}

	// 6) 登录成功：原子清零失败状态，再记录登录信息
	if err := s.am.ResetLoginFailure(ctx, entity.ID); err != nil {
		return nil, err
	}
	now := time.Now() //获取当前时间
	loginUpdate := map[string]any{"last_login_time": now}
	if clientIP != "" {
		loginUpdate["last_login_ip"] = clientIP
	}
	if err := s.am.DB(ctx).Where("id = ?", entity.ID).
		Select("last_login_time", "last_login_ip").
		Updates(loginUpdate).Error; err != nil {
		return nil, err
	}

	// 7) 生成会话 ID（cookie 会话与 Redis 用户会话绑定）
	sessionID, err := auth.NewSessionID()
	if err != nil {
		return nil, fmt.Errorf("生成会话 ID 失败: %w", err)
	}

	// 8) 写入 Redis 会话
	name := ""
	if entity.Name != nil {
		name = *entity.Name
	}
	avatar := ""
	if entity.Avatar != nil {
		avatar = *entity.Avatar
	}
	email := ""
	if entity.Email != nil {
		email = *entity.Email
	}
	phone := ""
	if entity.Phone != nil {
		phone = *entity.Phone
	}

	if err := auth.SaveUserSession(ctx, &auth.UserSession{
		ID:        entity.ID,
		SessionID: sessionID,
		Username:  entity.Username,
		Name:      name,
		Avatar:    avatar,
		Email:     email,
		Phone:     phone,
		Status:    entity.Status,
		IsAdmin:   entity.IsAdmin,
		DeptID:    entity.DeptID,
	}, auth.SessionTTLFor(req.RememberMe)); err != nil {
		return nil, fmt.Errorf("写入用户会话失败: %w", err)
	}

	// 9) 刷新在线心跳
	if err := auth.RefreshOnline(ctx, entity.ID, 0); err != nil {
		return nil, fmt.Errorf("刷新在线状态失败: %w", err)
	}

	logger.Scene("admin").With("username", entity.Username).Info("登录成功")
	return &admindto.AdminLoginResp{
		UserID:     entity.ID,
		Username:   entity.Username,
		SessionID:  sessionID,
		IssuedAt:   time.Now().Unix(),
		RememberMe: req.RememberMe,
	}, nil
}

// AdminLogout 注销当前管理员会话，使该会话立即失效。
func (s *Service) AdminLogout(ctx context.Context, userID uint64) (err error) {
	return auth.DeleteUserSession(ctx, userID)
}

// AdminProfile 获取当前登录用户信息。
// 优先从 Redis 读取，不存在时查询数据库并回填 Redis。
func (s *Service) AdminProfile(ctx context.Context, userID uint64) (*admindto.AdminProfileResp, error) {
	// 1) 优先从 Redis 获取会话
	session, err := auth.GetUserSession(ctx, userID)
	if err == nil && session != nil {
		return &admindto.AdminProfileResp{
			ID:       session.ID,
			Username: session.Username,
			Name:     session.Name,
			Avatar:   session.Avatar,
			Email:    session.Email,
			Phone:    session.Phone,
			Status:   session.Status,
		}, nil
	}

	// 2) Redis 未命中，查询数据库
	entity, err := s.am.GetByID(ctx, userID)
	if err != nil {
		return nil, err
	}
	if entity == nil {
		return nil, fmt.Errorf(adminenums.ErrUserNotFound)
	}

	name := ""
	if entity.Name != nil {
		name = *entity.Name
	}
	avatar := ""
	if entity.Avatar != nil {
		avatar = *entity.Avatar
	}
	email := ""
	if entity.Email != nil {
		email = *entity.Email
	}
	phone := ""
	if entity.Phone != nil {
		phone = *entity.Phone
	}

	// 3) 回填 Redis（保持剩余 TTL：本路径不是登录，不能重置会话有效期）
	if err := auth.RefreshUserSession(ctx, &auth.UserSession{
		ID:       entity.ID,
		Username: entity.Username,
		Name:     name,
		Avatar:   avatar,
		Email:    email,
		Phone:    phone,
		Status:   entity.Status,
		IsAdmin:  entity.IsAdmin,
		DeptID:   entity.DeptID,
	}); err != nil {
		return nil, fmt.Errorf("写入用户会话失败: %w", err)
	}

	return &admindto.AdminProfileResp{
		ID:       entity.ID,
		Username: entity.Username,
		Name:     name,
		Avatar:   avatar,
		Email:    email,
		Phone:    phone,
		Status:   entity.Status,
	}, nil
}

// 登录失败锁定阈值与时长：连续失败达到阈值锁定 30 分钟。
// 锁定只写 locked_until_time，绝不修改 status：
// status 表达管理员启用/禁用/封禁的管理状态，若被改为 Banned，
// 30 分钟锁定过期后 IsActive() 仍为 false，账号将永久无法登录（DoS）。
// IsLocked() 基于 locked_until_time 与当前时间比较，到期自动解锁。
const (
	loginFailureLockThreshold = 5
	loginFailureLockDuration  = 30 * time.Minute
)

// DevLogin 开发阶段免密登录（仅 debug 模式的路由会调用它）。
//
// 与 AdminLogin 的区别只有「不验证码、不校验密码」，**会话建立那一整段完全共用**：
// 同一个 NewSessionID、同一份 Redis 会话结构、同样的在线心跳。
// 刻意不做「直接塞一个 cookie 就放行」的旁路 —— 那种旁路会让开发环境的会话行为
// 与生产不一致，用它验出来的东西不算数。
//
// 只允许登录超管：这个入口的存在意义是「本机开发便利」，不是「任意管理员后门」。
func (s *Service) DevLogin(ctx context.Context, username string) (*admindto.AdminLoginResp, error) {
	var entity adminmodel.AdminEntity
	q := s.am.DB(ctx).Where("is_admin = ?", 1)
	if username != "" {
		q = q.Where("username = ?", username)
	}
	if err := q.Order("id ASC").First(&entity).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errors.New("没有可用的超管账号（is_admin=1）")
		}
		return nil, err
	}
	if !entity.IsActive() {
		return nil, errors.New(adminenums.ErrAccountDisabled)
	}

	sessionID, err := auth.NewSessionID()
	if err != nil {
		return nil, fmt.Errorf("生成会话 ID 失败: %w", err)
	}
	name, avatar, email, phone := "", "", "", ""
	if entity.Name != nil {
		name = *entity.Name
	}
	if entity.Avatar != nil {
		avatar = *entity.Avatar
	}
	if entity.Email != nil {
		email = *entity.Email
	}
	if entity.Phone != nil {
		phone = *entity.Phone
	}
	if err := auth.SaveUserSession(ctx, &auth.UserSession{
		ID:        entity.ID,
		SessionID: sessionID,
		Username:  entity.Username,
		Name:      name,
		Avatar:    avatar,
		Email:     email,
		Phone:     phone,
		Status:    entity.Status,
		IsAdmin:   entity.IsAdmin,
		DeptID:    entity.DeptID,
	}, auth.SessionTTLFor(false)); err != nil {
		return nil, fmt.Errorf("写入用户会话失败: %w", err)
	}
	if err := auth.RefreshOnline(ctx, entity.ID, 0); err != nil {
		return nil, fmt.Errorf("刷新在线状态失败: %w", err)
	}
	return &admindto.AdminLoginResp{
		UserID:    entity.ID,
		Username:  entity.Username,
		SessionID: sessionID,
		IssuedAt:  time.Now().Unix(),
	}, nil
}

// AdminRoleList 查询用户绑定的角色编码列表。
func (s *Service) AdminRoleList(ctx context.Context, req *admindto.AdminRoleListReq) (res *admindto.AdminRoleListResp, err error) {
	roleCodes, err := casbin.GetRoleCodesByUserID(strconv.FormatUint(req.UserID, 10))
	if err != nil {
		return nil, err
	}

	var list []admindto.AdminRoleListItem
	for _, code := range roleCodes {
		list = append(list, admindto.AdminRoleListItem{RoleCode: code})
	}

	return &admindto.AdminRoleListResp{List: list}, nil
}

// AdminRoleSave 全量替换用户绑定的角色。
// 前端直接传 role_codes，写入 Casbin g 策略。
//
// 超管保护（审计项「RBAC 提权无超管保护」）：目标用户是超管账号（is_admin=1）
// 或目标角色含超管权限（权限集覆盖全部启用权限点）时，仅超管可操作——
// 防止普通管理员借「替换角色绑定」把超管账号降权，或给自己绑定超管角色提权。
func (s *Service) AdminRoleSave(ctx context.Context, req *admindto.AdminRoleSaveReq) (res *admindto.AdminRoleSaveResp, err error) {
	targetSuper, err := s.IsSuperAdmin(ctx, req.UserID)
	if err != nil {
		return nil, err
	}
	roleSuper, err := s.roleHasSuperAdminPermission(ctx, req.RoleCodes)
	if err != nil {
		return nil, err
	}
	if err = s.requireSuperAdminForSensitiveTarget(ctx, req.OperatorID, targetSuper || roleSuper); err != nil {
		return nil, err
	}

	userIDStr := strconv.FormatUint(req.UserID, 10)
	if err = casbin.ReplaceUserRoleBindings(userIDStr, req.RoleCodes); err != nil {
		return nil, err
	}
	return &admindto.AdminRoleSaveResp{UserID: req.UserID}, nil
}

// AdminMenuList 查询用户的直接额外菜单和有效菜单（含角色继承）。
func (s *Service) AdminMenuList(ctx context.Context, req *admindto.AdminMenuListReq) (res *admindto.AdminMenuListResp, err error) {
	userIDStr := strconv.FormatUint(req.UserID, 10)

	// 1. 获取用户直接额外权限（p, user_id, ...）
	directPerms, err := casbin.GetUserDirectPermissions(userIDStr)
	if err != nil {
		return nil, err
	}

	// 直接权限的 codes → menu_ids
	directCodes := make([]string, 0, len(directPerms))
	for _, p := range directPerms {
		directCodes = append(directCodes, p[2])
	}

	var directMenuIDs []uint64
	if len(directCodes) > 0 {
		directMenuIDs, err = s.GetIDsByPermissionCodes(ctx, directCodes)
		if err != nil {
			return nil, err
		}
	}

	// 2. 获取用户全部有效权限（直接 + 角色继承）
	// Casbin Enforce 是按请求检查的，不能批量获取。
	// 但角色权限可以从 p, role_code, ... 获取。
	roleCodes, err := s.GetRoleCodesByUserID(ctx, req.UserID)
	if err != nil {
		return nil, err
	}

	// 收集有效 codes：直接 + 所有角色的
	effectiveCodes := make(map[string]struct{})
	for _, c := range directCodes {
		effectiveCodes[c] = struct{}{}
	}
	for _, rc := range roleCodes {
		rolePerms, err := casbin.GetRolePermissions(rc)
		if err != nil {
			return nil, err
		}
		for _, p := range rolePerms {
			effectiveCodes[p[2]] = struct{}{}
		}
	}

	// codes → menu_ids
	var effectiveMenuIDs []uint64
	if len(effectiveCodes) > 0 {
		uniqueCodes := make([]string, 0, len(effectiveCodes))
		for c := range effectiveCodes {
			uniqueCodes = append(uniqueCodes, c)
		}
		effectiveMenuIDs, err = s.GetIDsByPermissionCodes(ctx, uniqueCodes)
		if err != nil {
			return nil, err
		}
	}

	return &admindto.AdminMenuListResp{
		DirectMenuIDs:    directMenuIDs,
		EffectiveMenuIDs: effectiveMenuIDs,
	}, nil
}

// AdminMenuSave 全量替换用户的直接额外权限。
// menu_ids → permission_codes → [path, method, code] → Casbin ReplaceUserPermissions。
//
// 超管保护：**与 AdminRoleSave 同款，此前这条路径漏了**（它没有任何界面调用方，
// 所以缺口一直没被走到）。直接权限是 p 策略里 r.sub == p.sub 那一支，写入集合一旦覆盖
// 全部启用权限点，目标账号即刻等价超管 —— 而入口只需要 admin:menu_save 这一个权限点。
// 三个判据与 RoleMenuSave 一致：新集合是否超管等价、目标账号是否本来就是超管，
// 任一为真则仅超管可操作（requireSuperAdminForSensitiveTarget）。
func (s *Service) AdminMenuSave(ctx context.Context, req *admindto.AdminMenuSaveReq) (res *admindto.AdminMenuSaveResp, err error) {
	// menu_ids → permission_codes
	codes, err := s.GetPermissionCodesByIDs(ctx, req.MenuIDs)
	if err != nil {
		return nil, err
	}

	willBeSuper, err := s.codesCoverAllEnabled(ctx, codes)
	if err != nil {
		return nil, err
	}
	targetSuper, err := s.IsSuperAdmin(ctx, req.UserID)
	if err != nil {
		return nil, err
	}
	if err = s.requireSuperAdminForSensitiveTarget(ctx, req.OperatorID, willBeSuper || targetSuper); err != nil {
		return nil, err
	}

	// permission_codes → [path, method, code]
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

	// Casbin 全量替换用户直接权限
	userIDStr := strconv.FormatUint(req.UserID, 10)
	if err = casbin.ReplaceUserPermissions(userIDStr, policies); err != nil {
		return nil, err
	}

	return &admindto.AdminMenuSaveResp{UserID: req.UserID}, nil
}

// collectEffectiveCodes 收集用户全部有效权限码（用户直接权限 + 角色继承权限），
// 返回（启用角色码列表, 排序后的有效权限码列表）。
//
// 供 AdminRoutes（动态路由）与 EffectivePermissionCodes（渲染层过滤）复用，
// 保证「API 鉴权放行的权限」与「页面/按钮显示的权限」永远同源。
func (s *Service) collectEffectiveCodes(ctx context.Context, userID uint64) (roleCodes, codes []string, err error) {
	userIDStr := strconv.FormatUint(userID, 10)

	// 1. 启用角色编码列表，保持与 Casbin g2 角色状态语义一致。
	roleCodes, err = s.GetRoleCodesByUserID(ctx, userID)
	if err != nil {
		return nil, nil, err
	}

	// 2. 收集全部有效 permission codes
	effectiveCodes := make(map[string]struct{})

	// 2a. 用户直接权限
	directPerms, err := casbin.GetUserDirectPermissions(userIDStr)
	if err != nil {
		return nil, nil, err
	}
	for _, p := range directPerms {
		effectiveCodes[p[2]] = struct{}{}
	}

	// 2b. 角色继承权限
	for _, rc := range roleCodes {
		rolePerms, err := casbin.GetRolePermissions(rc)
		if err != nil {
			return nil, nil, err
		}
		for _, p := range rolePerms {
			effectiveCodes[p[2]] = struct{}{}
		}
	}

	// 3. 转为稳定数组（确定性渲染顺序）。
	codes = make([]string, 0, len(effectiveCodes))
	for c := range effectiveCodes {
		codes = append(codes, c)
	}
	sort.Strings(codes)
	if roleCodes == nil {
		roleCodes = []string{}
	}
	return roleCodes, codes, nil
}

// EffectivePermissionCodes 返回用户全部有效权限码（直接 + 角色继承）。
//
// 渲染层入口：dashboard 页面据此注入 HasPerm，做菜单 / 按钮 / 字段的可见性过滤，
// 与 Casbin API 鉴权共用同一份权限来源，避免「看得到但点不了」或反向不一致。
func (s *Service) EffectivePermissionCodes(ctx context.Context, userID uint64) ([]string, error) {
	_, codes, err := s.collectEffectiveCodes(ctx, userID)
	if err != nil {
		return nil, err
	}
	return codes, nil
}
