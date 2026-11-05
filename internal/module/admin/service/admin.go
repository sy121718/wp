package adminservice

import (
	"context"
	"errors"
	"fmt"
	"net/http"
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
	"go_wp/pkg/facing"
	"go_wp/pkg/logger"
	"go_wp/pkg/utils"
)

func (s *Service) AdminList(ctx context.Context, req *admindto.AdminListReq) (res *admindto.AdminListResp, err error) {
	// 排序白名单是业务判断：不合法的字段/方向必须给出业务错误，不能悄悄退回默认排序。
	sortField, sortOrder, err := normalizeAdminSort(req.SortField, req.SortOrder)
	if err != nil {
		return nil, err
	}

	total, list, err := s.am.ListPage(ctx, adminmodel.AdminListFilter{
		Email:     strings.TrimSpace(req.Email),
		Name:      strings.TrimSpace(req.Name),
		Status:    req.Status,
		SortField: sortField,
		SortOrder: sortOrder,
		Page:      req.GetPage(),
		Limit:     req.GetLimit(),
	})
	if err != nil {
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

// normalizeAdminSort 把请求里的排序字段/方向归一成白名单内的值。
func normalizeAdminSort(sortField, sortOrder string) (field, dir string, err error) {
	if sortField == "" || sortOrder == "" {
		return "", "", nil
	}
	field = strings.ToLower(strings.TrimSpace(sortField))
	dir = strings.ToUpper(strings.TrimSpace(sortOrder))
	if dir != "ASC" && dir != "DESC" {
		return "", "", adminenums.Err(adminenums.ErrSortDirectionInvalid)
	}
	switch field {
	case "id", "username", "name", "email", "phone", "status", "create_time", "last_login_time":
		return field, dir, nil
	default:
		return "", "", adminenums.Err(adminenums.ErrSortFieldInvalid)
	}
}

func (s *Service) AdminCreate(ctx context.Context, req *admindto.AdminCreateReq) (res *admindto.AdminCreateResp, err error) {
	if emailExists, err := s.am.FieldExists(ctx, "email", req.Email); err != nil {
		return nil, err
	} else if emailExists {
		return nil, adminenums.Err(adminenums.ErrEmailExists)
	}
	if nameExists, err := s.am.FieldExists(ctx, "username", req.Username); err != nil {
		return nil, err
	} else if nameExists {
		return nil, adminenums.Err(adminenums.ErrUsernameExists)
	}

	hashed, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		return nil, err
	}

	entity := &adminmodel.AdminEntity{
		Username: req.Username,
		Password: string(hashed),
		Email:    &req.Email,
		Status:   adminmodel.AdminStatusActive,
		Name:     &req.Username,
	}

	if req.Phone != "" {
		if phoneExists, err := s.am.FieldExists(ctx, "phone", req.Phone); err != nil {
			return nil, err
		} else if phoneExists {
			return nil, adminenums.Err(adminenums.ErrPhoneExists)
		}
		entity.Phone = &req.Phone
	}
	if req.Avatar != "" {
		entity.Avatar = &req.Avatar
	}

	// 预检与插入之间还有窗口，库里的唯一约束才是最后一道：
	// 撞了就把约束名翻成业务 key，别让 SQLSTATE 原文流到出口。
	if err := s.am.Create(ctx, entity); err != nil {
		if mapped, ok := adminUniqueViolationKey(err); ok {
			return nil, mapped
		}
		return nil, err
	}
	res = &admindto.AdminCreateResp{
		ID:       entity.ID,
		Username: entity.Username,
	}

	return
}

func (s *Service) AdminDetail(ctx context.Context, req *admindto.AdminDetailReq) (res *admindto.AdminDetailResp, err error) {
	entity, err := s.am.DetailByID(ctx, req.Id)
	if err != nil {
		return nil, err
	}
	if entity == nil {
		return nil, adminenums.Err(adminenums.ErrAdminNotFound)
	}

	res = &admindto.AdminDetailResp{
		ID:                entity.ID,
		Username:          entity.Username,
		Name:              stringOrEmpty(entity.Name),
		Avatar:            stringOrEmpty(entity.Avatar),
		Email:             stringOrEmpty(entity.Email),
		Phone:             stringOrEmpty(entity.Phone),
		Status:            entity.Status,
		IsAdmin:           entity.IsAdmin,
		RegisterIP:        stringOrEmpty(entity.RegisterIP),
		RegisterLocation:  stringOrEmpty(entity.RegisterLocation),
		LastLoginIP:       stringOrEmpty(entity.LastLoginIP),
		LastLoginLocation: stringOrEmpty(entity.LastLoginLocation),
		LastLoginTime:     utils.NewJSONTimePtr(entity.LastLoginTime),
		CreateBy:          entity.CreateBy,
		CreateTime:        utils.NewJSONTimePtr(entity.CreateTime),
		Remark:            stringOrEmpty(entity.Remark),
		Roles:             []any{},
		Menus:             []any{},
	}
	return
}

func (s *Service) AdminEdit(ctx context.Context, req *admindto.AdminEditReq) (res *admindto.AdminEditResp, err error) {
	entity := &adminmodel.AdminEntity{
		Username: req.Username,
		Email:    &req.Email,
	}
	if req.Phone != "" {
		entity.Phone = &req.Phone
	}
	if req.Remark != "" {
		entity.Remark = &req.Remark
	}

	var taskID int64
	err = s.am.Transaction(ctx, func(tx *gorm.DB) error {
		// 存在性、重名预检、更新都在同一个事务里：
		// 先锁住这一行，再判断重名，最后落库；否则两个并发编辑会各自通过预检。
		locked, err := s.am.LockByIDsTx(ctx, tx, []uint64{req.Id})
		if err != nil {
			return err
		}
		if len(locked) == 0 {
			return adminenums.Err(adminenums.ErrAdminNotFound)
		}

		if emailExists, err := s.am.FieldExistsTxExcept(ctx, tx, "email", req.Email, req.Id); err != nil {
			return err
		} else if emailExists {
			return adminenums.Err(adminenums.ErrEmailExists)
		}
		if nameExists, err := s.am.FieldExistsTxExcept(ctx, tx, "username", req.Username, req.Id); err != nil {
			return err
		} else if nameExists {
			return adminenums.Err(adminenums.ErrUsernameExists)
		}
		if req.Phone != "" {
			if phoneExists, err := s.am.FieldExistsTxExcept(ctx, tx, "phone", req.Phone, req.Id); err != nil {
				return err
			} else if phoneExists {
				return adminenums.Err(adminenums.ErrPhoneExists)
			}
		}

		if err := s.am.UpdateByIDTx(ctx, tx, req.Id, entity); err != nil {
			if mapped, ok := adminUniqueViolationKey(err); ok {
				return mapped
			}
			return err
		}

		// 会话吊销是跨库副作用，删不掉就回滚不了：登记成持久任务，提交后立即尝试，
		// 失败也留在表里可重放，而不是只写一行日志就丢掉。
		taskID, err = s.appendRevokeSessionTx(ctx, tx, req.Id)
		return err
	})
	if err != nil {
		return nil, err
	}

	s.FlushSideEffect(ctx, taskID)

	res = &admindto.AdminEditResp{
		ID: req.Id,
	}
	return
}

// adminUniqueViolationKey 把库里的唯一约束冲突翻成对应的业务 key。
//
// 只有 sys_admin 上真实存在的唯一约束才映射得出来 ── 当前仅 uk_sys_admin_username；
// email / phone 只有普通索引，重复只能靠事务内的预检发现，不要在这里假装能兜住。
func adminUniqueViolationKey(err error) (error, bool) {
	if !database.IsUniqueViolation(err) {
		return nil, false
	}
	msg := strings.ToLower(err.Error())
	switch {
	case strings.Contains(msg, "uk_sys_admin_username"):
		return adminenums.Err(adminenums.ErrUsernameExists), true
	default:
		return nil, false
	}
}

// AdminDelete 删除管理员，走与批量相同的同事务校验 + 删除路径。
func (s *Service) AdminDelete(ctx context.Context, req *admindto.AdminDeleteReq) (*admindto.AdminDeleteResp, error) {
	return s.AdminBulkDelete(ctx, req)
}

func (s *Service) AdminListByDeptID(ctx context.Context, req *admindto.AdminListByDeptIDReq) (res *admindto.AdminListByDeptIDResp, err error) {
	total, entities, err := s.am.ListPageByDept(ctx, req.DeptID, req.GetPage(), req.GetLimit())
	if err != nil {
		return nil, err
	}

	list := make([]admindto.DeptAdminItem, 0, len(entities))
	for _, e := range entities {
		list = append(list, admindto.DeptAdminItem{
			ID:       e.ID,
			Username: e.Username,
			Name:     stringOrEmpty(e.Name),
			Email:    stringOrEmpty(e.Email),
			Status:   e.Status,
		})
	}
	return &admindto.AdminListByDeptIDResp{Total: total, List: list}, nil
}

// AdminBatchSetDeptID 批量把成员调到某个部门。
//
// 与部门删除端协调：删除部门要数「还有没有成员」，改归属要挪成员。
// 两边都锁 sys_admin 的行，且在同一个事务里完成，
// 才不会出现「删除端数完 0 个成员 → 改归属插进来 → 部门被删掉留下悬挂 dept_id」。
func (s *Service) AdminBatchSetDeptID(ctx context.Context, req *admindto.AdminBatchSetDeptIDReq) error {
	ids := uniqueIDs(req.UserIDs)
	if len(ids) == 0 {
		return nil
	}
	return s.am.Transaction(ctx, func(tx *gorm.DB) error {
		locked, err := s.am.LockByIDsTx(ctx, tx, ids)
		if err != nil {
			return err
		}
		if len(locked) != len(ids) {
			return adminenums.Err(adminenums.ErrAdminNotFound)
		}
		if req.DeptID != 0 {
			// 必须是锁不是读：DeptBulkDelete 的「有成员就打回」也是先数 sys_admin 再删。
			// 这里只读一次部门存在性，删除端就可能刚数完 0 个成员、没看到本事务待插入的行，
			// 于是删掉部门、留下指向空部门的成员。锁住这个 dept 实体让两端在同一行上排队。
			dept, err := s.dm.LockByIDTx(ctx, tx, req.DeptID)
			if err != nil {
				return err
			}
			if dept == nil {
				return adminenums.Err(adminenums.ErrDeptNotFound)
			}
		}
		_, err = s.am.UpdateDeptIDByIDsTx(ctx, tx, ids, req.DeptID)
		return err
	})
}

func (s *Service) AdminCountByDeptID(ctx context.Context, deptID uint64) (int64, error) {
	return s.am.CountByDeptID(ctx, deptID)
}

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

func (s *Service) requireSuperAdminForSensitiveTarget(ctx context.Context, operatorID uint64, targetSensitive bool) error {
	if !targetSensitive {
		return nil
	}
	opSuper, err := s.IsSuperAdmin(ctx, operatorID)
	if err != nil {
		return err
	}
	if !opSuper {
		return adminenums.Err(adminenums.ErrSuperAdminOnly)
	}
	return nil
}

func (s *Service) AdminLogin(ctx context.Context, req *admindto.AdminLoginReq, clientIP string) (*admindto.AdminLoginResp, error) {

	var captchaSvc = captcha.Get()

	if !captchaSvc.Verify(req.CaptchaID, req.Captcha, true) {
		return nil, adminenums.Err(adminenums.ErrCaptchaExpired)
	}

	entity, err := s.am.FindByLoginName(ctx, req.Username)
	if err != nil {
		return nil, err
	}
	if entity == nil {
		logger.Scene("admin").With("username", req.Username).With("reason", "用户不存在").Warn("登录失败")
		return nil, adminenums.Err(adminenums.ErrBadCredentials)
	}

	if entity.IsLocked() {
		logger.Scene("admin").With("username", req.Username).With("reason", "账号已锁定").Warn("登录失败")

		// 423 Locked 是这条错误自己的事实：账号存在、凭证也许没错，只是此刻被锁住。
		// 用 errors.New + WithStatus 而不是 fmt.Errorf("%s|%s")：后者只是个普通
		// fmt 错误，状态只能靠出口默认成 400，把「锁多久」和「参数错了」混成同一档。
		return nil, facing.WithStatus(
			errors.New(adminenums.ErrAccountLocked+"|"+
				time.Until(*entity.LockedUntilTime).Round(time.Minute).String()),
			http.StatusLocked,
		)
	}

	if !entity.IsActive() {
		logger.Scene("admin").With("username", req.Username).With("reason", "账号已禁用").Warn("登录失败")
		return nil, adminenums.Err(adminenums.ErrAccountDisabled)
	}

	if err := bcrypt.CompareHashAndPassword([]byte(entity.Password), []byte(req.Password)); err != nil {
		logger.Scene("admin").With("username", req.Username).With("reason", "密码错误").Warn("登录失败")
		if ferr := s.am.IncrementLoginFailure(ctx, entity.ID, loginFailureLockThreshold, loginFailureLockDuration); ferr != nil {
			logger.Scene("admin").Error(ferr, "记录登录失败状态失败")
		}
		return nil, adminenums.Err(adminenums.ErrBadCredentials)
	}

	if err := s.am.ResetLoginFailure(ctx, entity.ID); err != nil {
		return nil, err
	}
	if err := s.am.TouchLastLogin(ctx, entity.ID, time.Now(), clientIP); err != nil {
		return nil, err
	}

	sessionID, err := auth.NewSessionID()
	if err != nil {
		return nil, fmt.Errorf("生成会话 ID 失败: %w", err)
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

func (s *Service) AdminLogout(ctx context.Context, userID uint64) (err error) {
	return auth.DeleteUserSession(ctx, userID)
}

func (s *Service) AdminProfile(ctx context.Context, userID uint64) (*admindto.AdminProfileResp, error) {

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

const (
	loginFailureLockThreshold = 5
	loginFailureLockDuration  = 30 * time.Minute
)

func (s *Service) DevLogin(ctx context.Context, username string) (*admindto.AdminLoginResp, error) {
	entity, err := s.am.FirstActiveSuperAdmin(ctx, username)
	if err != nil {
		return nil, err
	}
	if entity == nil {
		return nil, errors.New("没有可用的超管账号（is_admin=1）")
	}
	if !entity.IsActive() {
		return nil, adminenums.Err(adminenums.ErrAccountDisabled)
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

// AdminRoleSave 保存某个管理员的角色集合。
//
// 与 RoleUserSave 是同一张绑定表的两侧，必须用同一套锁与同一套事务写：
// 先按角色 id 升序锁角色行，再锁被改的人，绑定写在本事务里。
// 只锁人会让「两个入口同时改同一个角色」互相看不见；只写 enforcer 连接会让
// 本事务回滚后绑定还留着。
func (s *Service) AdminRoleSave(ctx context.Context, req *admindto.AdminRoleSaveReq) (res *admindto.AdminRoleSaveResp, err error) {
	var reloadTaskID int64

	err = s.am.Transaction(ctx, func(tx *gorm.DB) error {

		codes := uniqueStrings(req.RoleCodes)
		if len(codes) > 0 {
			locked, err := s.rm.LockByCodesTx(ctx, tx, codes)
			if err != nil {
				return err
			}
			if len(locked) != len(codes) {
				return adminenums.Err(adminenums.ErrRoleNotFound)
			}
		}
		lockedAdmins, err := s.am.LockByIDsTx(ctx, tx, []uint64{req.UserID})
		if err != nil {
			return err
		}
		if len(lockedAdmins) != 1 {
			return adminenums.Err(adminenums.ErrAdminNotFound)
		}

		targetSuper := lockedAdmins[0].IsSuperAdmin()
		roleSuper, err := s.roleHasSuperAdminPermission(ctx, codes)
		if err != nil {
			return err
		}
		if err = s.requireSuperAdminForSensitiveTarget(ctx, req.OperatorID, targetSuper || roleSuper); err != nil {
			return err
		}

		userIDStr := strconv.FormatUint(req.UserID, 10)
		changed, err := casbin.ReplaceUserRoleBindingsTx(ctx, tx, userIDStr, codes)
		if err != nil {
			return err
		}
		if changed {
			reloadTaskID, err = s.appendPolicyReloadTx(ctx, tx)
			if err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	s.FlushSideEffect(ctx, reloadTaskID)
	return &admindto.AdminRoleSaveResp{UserID: req.UserID}, nil
}

func (s *Service) AdminMenuList(ctx context.Context, req *admindto.AdminMenuListReq) (res *admindto.AdminMenuListResp, err error) {
	userIDStr := strconv.FormatUint(req.UserID, 10)

	directPerms, err := casbin.GetUserDirectPermissions(userIDStr)
	if err != nil {
		return nil, err
	}

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

	roleCodes, err := s.GetRoleCodesByUserID(ctx, req.UserID)
	if err != nil {
		return nil, err
	}

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

func (s *Service) AdminMenuSave(ctx context.Context, req *admindto.AdminMenuSaveReq) (res *admindto.AdminMenuSaveResp, err error) {
	// 授予与本事务的写在同一事务里：先锁住这次要授予的菜单与权限点行，再落 p 策略。
	//
	// READ COMMITTED 下「A 读菜单还在 → B 授予后提交 → A 删菜单」是真实反例，
	// 只在删除端加锁挡不住它 —— 授予端必须锁同一批实体，双方才在同一个锁上排队。
	// 锁序：菜单与权限点都按各自的 id 升序（见 LockByIDsTx / LockByCodesTx 里的 ORDER BY id），
	// 与删除端 PermBulkDelete / MenuBulkDelete 一致，两端在同一批行上排同一个队。
	menuIDs := uniqueIDs(req.MenuIDs)

	var policies [][3]string
	var changed bool
	var reloadTaskID int64

	err = s.mm.Transaction(ctx, func(tx *gorm.DB) error {
		if len(menuIDs) > 0 {
			lockedMenus, err := s.mm.LockByIDsTx(ctx, tx, menuIDs)
			if err != nil {
				return err
			}
			if len(lockedMenus) != len(menuIDs) {
				// 锁不到 = 有菜单在并发里被删掉了。授予这批已经不成立，整体打回。
				return adminenums.Err(adminenums.ErrMenuNotFound)
			}
		}

		codes, err := s.menuPermissionCodesTx(ctx, tx, menuIDs)
		if err != nil {
			return err
		}

		willBeSuper, err := s.codesCoverAllEnabled(ctx, codes)
		if err != nil {
			return err
		}
		targetSuper, err := s.IsSuperAdmin(ctx, req.UserID)
		if err != nil {
			return err
		}
		if err = s.requireSuperAdminForSensitiveTarget(ctx, req.OperatorID, willBeSuper || targetSuper); err != nil {
			return err
		}

		if len(codes) > 0 {
			// 权限点行也锁住：删除权限点端要能看到「这条码刚被授予」，
			// 靠的就是同一批 sys_permission 行上的锁，而不是内存 enforcer 的旧副本。
			lockedPerms, err := s.pm.LockByCodesTx(ctx, tx, codes)
			if err != nil {
				return err
			}
			if len(lockedPerms) != len(codes) {
				return adminenums.Err(adminenums.ErrPermissionNotFound)
			}
			for _, b := range lockedPerms {
				policies = append(policies, [3]string{b.APIPath, b.APIMethod, b.PermissionCode})
			}
		}

		userIDStr := strconv.FormatUint(req.UserID, 10)
		changed, err = casbin.ReplaceUserPermissionsTx(ctx, tx, userIDStr, policies)
		if err != nil {
			return err
		}
		if changed {
			// 重载作用在进程内 enforcer 副本上，回滚不了：落成任务，
			// 提交后立刻试一次，失败交给 worker，绝不在已提交后报整体失败。
			reloadTaskID, err = s.appendPolicyReloadTx(ctx, tx)
			if err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	s.FlushSideEffect(ctx, reloadTaskID)
	return &admindto.AdminMenuSaveResp{UserID: req.UserID}, nil
}

func (s *Service) collectEffectiveCodes(ctx context.Context, userID uint64) (roleCodes, codes []string, err error) {
	userIDStr := strconv.FormatUint(userID, 10)

	roleCodes, err = s.GetRoleCodesByUserID(ctx, userID)
	if err != nil {
		return nil, nil, err
	}

	effectiveCodes := make(map[string]struct{})

	directPerms, err := casbin.GetUserDirectPermissions(userIDStr)
	if err != nil {
		return nil, nil, err
	}
	for _, p := range directPerms {
		effectiveCodes[p[2]] = struct{}{}
	}

	for _, rc := range roleCodes {
		rolePerms, err := casbin.GetRolePermissions(rc)
		if err != nil {
			return nil, nil, err
		}
		for _, p := range rolePerms {
			effectiveCodes[p[2]] = struct{}{}
		}
	}

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

func (s *Service) EffectivePermissionCodes(ctx context.Context, userID uint64) ([]string, error) {
	_, codes, err := s.collectEffectiveCodes(ctx, userID)
	if err != nil {
		return nil, err
	}
	return codes, nil
}

// stringOrEmpty 把可空列读成空串 —— 对外 JSON 只给字符串事实，不把 nil 指针交给上层。
func stringOrEmpty(v *string) string {
	if v == nil {
		return ""
	}
	return *v
}
