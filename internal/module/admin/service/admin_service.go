package adminservice

import (
	admincontract "go_wp/internal/module/admin/contract"
	adminmodel "go_wp/internal/module/admin/model"
	"go_wp/pkg/datarule"

	"gorm.io/gorm"
)

var _ datarule.RuleProvider = (*Service)(nil)

var (
	_ admincontract.AdminService = (*Service)(nil)
	_ admincontract.RoleService  = (*Service)(nil)
	_ admincontract.PermService  = (*Service)(nil)
	_ admincontract.MenuService  = (*Service)(nil)
	_ admincontract.DeptService  = (*Service)(nil)
	_ admincontract.RuleService  = (*Service)(nil)

	_ admincontract.AuthzContextService = (*Service)(nil)
)

type Service struct {
	am   *adminmodel.AdminModel
	rm   *adminmodel.RoleModel
	pm   *adminmodel.PermissionModel
	mm   *adminmodel.MenuModel
	dm   *adminmodel.DeptModel
	drm  *adminmodel.SysRuleModel
	dram *adminmodel.SysRuleAssignmentModel

	ruleSnapshot *dataruleSnapshotStore

	ruleSnapshotLoader dataRuleSnapshotLoader
}

func NewService(db *gorm.DB) *Service {
	return &Service{
		am:           adminmodel.NewAdminModel(db),
		rm:           adminmodel.NewRoleModel(db),
		pm:           adminmodel.NewPermissionModel(db),
		mm:           adminmodel.NewMenuModel(db),
		dm:           adminmodel.NewDeptModel(db),
		drm:          adminmodel.NewSysRuleModel(db),
		dram:         adminmodel.NewSysRuleAssignmentModel(db),
		ruleSnapshot: &dataruleSnapshotStore{},
	}
}
