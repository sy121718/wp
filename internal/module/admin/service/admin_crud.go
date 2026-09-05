package adminservice

import (
	"context"
	"errors"
	"strconv"
	"strings"

	admindto "go_wp/internal/module/admin/dto"
	adminenums "go_wp/internal/module/admin/enums"
	adminmodel "go_wp/internal/module/admin/model"
	"go_wp/pkg/auth"
	"go_wp/pkg/casbin"
	"go_wp/pkg/database"

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
		query = query.Where("email LIKE ?", "%"+email+"%")
	}
	if name := strings.TrimSpace(req.Name); name != "" {
		query = query.Where("name LIKE ?", "%"+name+"%")
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
			return nil, errors.New("无效的排序方向")
		}
		switch field {
		case "id", "username", "name", "email", "phone", "status", "create_time", "last_login_time":
			orderClause = field + " " + dir
		default:
			return nil, errors.New("无效的排序字段")
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
			CreateTime: entity.CreateTime,
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
	if err := auth.DeleteUserSession(ctx, req.Id); err != nil {
		return nil, err
	}

	res = &admindto.AdminEditResp{
		ID: req.Id,
	}
	return
}

// AdminDelete 删除普通管理员，并撤销其会话和授权。
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

	deleted, err := s.am.DeleteByIDs(ctx, ids)
	if err != nil {
		return nil, err
	}
	if deleted != int64(len(ids)) {
		return nil, errors.New(adminenums.ErrAdminNotFound)
	}

	for _, id := range ids {
		if err = casbin.DeleteUserAllPolicies(strconv.FormatUint(id, 10)); err != nil {
			return nil, err
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
