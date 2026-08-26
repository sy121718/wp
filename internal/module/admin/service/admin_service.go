package adminservice

import (
	admincontract "go_wp/internal/module/admin/contract"
	adminmodel "go_wp/internal/module/admin/model"
	"go_wp/pkg/datarule"

	"gorm.io/gorm"
)

var _ datarule.RuleProvider = (*Service)(nil)

// 编译期断言：合并后的 Service 实现全部领域契约接口。
var (
	_ admincontract.AdminService = (*Service)(nil)
	_ admincontract.RoleService  = (*Service)(nil)
	_ admincontract.PermService  = (*Service)(nil)
	_ admincontract.MenuService  = (*Service)(nil)
	_ admincontract.DeptService  = (*Service)(nil)
	_ admincontract.RuleService  = (*Service)(nil)
)

// Service 定义 admin 模块（管理员/角色/权限点/菜单/部门/数据权限）的业务逻辑。
//
// 合并自原 admin/role/permission/menu/dept/datarule 六个独立模块：
//   - am：只碰 sys_admin
//   - rm：只碰 sys_role
//   - pm：只碰 sys_permission
//   - mm：只碰 sys_menus
//   - dm：只碰 sys_dept
//   - drm：只碰 sys_rule
//   - dram：只碰 sys_rule_assignment
//
// 同包内方法直接互调，不再需要跨模块契约与运行时 Setter 注入。
type Service struct {
	am   *adminmodel.AdminModel
	rm   *adminmodel.RoleModel
	pm   *adminmodel.PermissionModel
	mm   *adminmodel.MenuModel
	dm   *adminmodel.DeptModel
	drm  *adminmodel.SysRuleModel
	dram *adminmodel.SysRuleAssignmentModel
}

// NewService 创建合并后的 admin Service，内部自建全部 model。
func NewService(db *gorm.DB) *Service {
	return &Service{
		am:   adminmodel.NewAdminModel(db),
		rm:   adminmodel.NewRoleModel(db),
		pm:   adminmodel.NewPermissionModel(db),
		mm:   adminmodel.NewMenuModel(db),
		dm:   adminmodel.NewDeptModel(db),
		drm:  adminmodel.NewSysRuleModel(db),
		dram: adminmodel.NewSysRuleAssignmentModel(db),
	}
}
