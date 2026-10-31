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

func (s *Service) AdminList(ctx context.Context, req *admindto.AdminListReq) (res *admindto.AdminListResp, err error) {

	var total int64
	page := req.GetPage()
	limit := req.GetLimit()

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

	orderClause := "id DESC"
	if req.SortField != "" && req.SortOrder != "" {
		field := strings.ToLower(strings.TrimSpace(req.SortField))
		dir := strings.ToUpper(strings.TrimSpace(req.SortOrder))
		if dir != "ASC" && dir != "DESC" {
			return nil, adminenums.Err(adminenums.ErrSortDirectionInvalid)
		}
		switch field {
		case "id", "username", "name", "email", "phone", "status", "create_time", "last_login_time":
			orderClause = field + " " + dir
		default:
			return nil, adminenums.Err(adminenums.ErrSortFieldInvalid)
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

func (s *Service) AdminCreate(ctx context.Context, req *admindto.AdminCreateReq) (res *admindto.AdminCreateResp, err error) {
	if emailExists, err := database.IsFieldExists(s.am.DB(ctx), &adminmodel.AdminEntity{}, "email", req.Email); err != nil {
		return nil, err
	} else if emailExists {
		return nil, adminenums.Err(adminenums.ErrEmailExists)
	}
	if nameExists, err := database.IsFieldExists(s.am.DB(ctx), &adminmodel.AdminEntity{}, "username", req.Username); err != nil {
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
		if phoneExists, err := database.IsFieldExists(s.am.DB(ctx), &adminmodel.AdminEntity{}, "phone", req.Phone); err != nil {
			return nil, err
		} else if phoneExists {
			return nil, adminenums.Err(adminenums.ErrPhoneExists)
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
			return nil, adminenums.Err(adminenums.ErrAdminNotFound)
		}
		return nil, err
	}

	if res.ID == 0 {
		return nil, adminenums.Err(adminenums.ErrAdminNotFound)
	}

	res.Roles = []any{}
	res.Menus = []any{}
	return
}

func (s *Service) AdminEdit(ctx context.Context, req *admindto.AdminEditReq) (res *admindto.AdminEditResp, err error) {
	entityExists, err := s.am.GetByID(ctx, req.Id)
	if err != nil {
		return nil, err
	}
	if entityExists == nil {
		return nil, adminenums.Err(adminenums.ErrAdminNotFound)
	}

	if emailExists, err := database.IsFieldExists(s.am.DB(ctx), &adminmodel.AdminEntity{}, "email", req.Email, req.Id); err != nil {
		return nil, err
	} else if emailExists {
		return nil, adminenums.Err(adminenums.ErrEmailExists)
	}

	if nameExists, err := database.IsFieldExists(s.am.DB(ctx), &adminmodel.AdminEntity{}, "username", req.Username, req.Id); err != nil {
		return nil, err
	} else if nameExists {
		return nil, adminenums.Err(adminenums.ErrUsernameExists)
	}

	entity := &adminmodel.AdminEntity{
		Username: req.Username,
		Email:    &req.Email,
	}
	if req.Phone != "" {

		if phoneExists, err := database.IsFieldExists(s.am.DB(ctx), &adminmodel.AdminEntity{}, "phone", req.Phone, req.Id); err != nil {
			return nil, err
		} else if phoneExists {
			return nil, adminenums.Err(adminenums.ErrPhoneExists)
		}
		entity.Phone = &req.Phone
	}

	if req.Remark != "" {
		entity.Remark = &req.Remark
	}

	if err := s.am.DB(ctx).Where("id = ?", req.Id).Updates(entity).Error; err != nil {
		return nil, err
	}

	if err := auth.DeleteUserSession(ctx, req.Id); err != nil {
		logger.Scene("admin").With("user_id", req.Id).Error(err, "编辑管理员后失效旧会话失败")
	}

	res = &admindto.AdminEditResp{
		ID: req.Id,
	}
	return
}

func (s *Service) AdminDelete(ctx context.Context, req *admindto.AdminDeleteReq) (res *admindto.AdminDeleteResp, err error) {
	ids := uniqueAdminIDs(req.Id)
	if len(ids) == 0 {
		return nil, adminenums.Err(adminenums.ErrAdminNotFound)
	}

	for _, id := range ids {
		entity, err := s.am.GetByID(ctx, id)
		if err != nil {
			return nil, err
		}
		if entity == nil {
			return nil, adminenums.Err(adminenums.ErrAdminNotFound)
		}
		if id == req.OperatorID {
			return nil, adminenums.Err(adminenums.ErrDeleteSelf)
		}
		if entity.IsSuperAdmin() {
			return nil, adminenums.Err(adminenums.ErrDeleteSuperAdmin)
		}
	}

	for _, id := range ids {
		if err = auth.RevokeUserSession(ctx, id); err != nil {
			return nil, err
		}
	}

	reloadPolicy := false
	var deleted int64
	err = s.am.Transaction(ctx, func(tx *gorm.DB) error {

		locked, err := s.am.LockByIDsTx(ctx, tx, ids)
		if err != nil {
			return err
		}
		if len(locked) != len(ids) {
			return adminenums.Err(adminenums.ErrAdminNotFound)
		}
		for _, entity := range locked {
			if entity.ID == req.OperatorID {
				return adminenums.Err(adminenums.ErrDeleteSelf)
			}
			if entity.IsSuperAdmin() {
				return adminenums.Err(adminenums.ErrDeleteSuperAdmin)
			}
		}

		deleted, err = s.am.DeleteByIDsTx(ctx, tx, ids)
		if err != nil {
			return err
		}
		if deleted != int64(len(ids)) {
			return adminenums.Err(adminenums.ErrAdminNotFound)
		}

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

func (s *Service) AdminBatchSetDeptID(ctx context.Context, req *admindto.AdminBatchSetDeptIDReq) error {
	if len(req.UserIDs) == 0 {
		return nil
	}
	return s.am.DB(ctx).Model(&adminmodel.AdminEntity{}).
		Where("id IN ?", req.UserIDs).
		Update("dept_id", req.DeptID).Error
}

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

	var entity adminmodel.AdminEntity

	binaryExpr := "CAST(username AS BINARY) = CAST(? AS BINARY)"
	switch s.am.DialectName() {
	case "postgres":
		binaryExpr = "username = ?"
	}
	if err := s.am.DB(ctx).Where(binaryExpr+" OR email = ?", req.Username, req.Username).First(&entity).Error; err != nil {
		logger.Scene("admin").With("username", req.Username).With("reason", "用户不存在").Warn("登录失败")
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, adminenums.Err(adminenums.ErrBadCredentials)
		}
		return nil, err
	}

	if entity.IsLocked() {
		logger.Scene("admin").With("username", req.Username).With("reason", "账号已锁定").Warn("登录失败")

		return nil, fmt.Errorf("%s|%s", adminenums.ErrAccountLocked,
			time.Until(*entity.LockedUntilTime).Round(time.Minute).String())
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
	now := time.Now()
	loginUpdate := map[string]any{"last_login_time": now}
	if clientIP != "" {
		loginUpdate["last_login_ip"] = clientIP
	}
	if err := s.am.DB(ctx).Where("id = ?", entity.ID).
		Select("last_login_time", "last_login_ip").
		Updates(loginUpdate).Error; err != nil {
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

	userIDStr := strconv.FormatUint(req.UserID, 10)
	if err = casbin.ReplaceUserPermissions(userIDStr, policies); err != nil {
		return nil, err
	}

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
