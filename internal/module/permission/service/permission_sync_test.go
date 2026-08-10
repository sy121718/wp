package permissionservice

import (
	"context"
	"strings"
	"testing"

	menucontract "go_wp/internal/module/menu/contract"
	permissiondto "go_wp/internal/module/permission/dto"
	permissionenums "go_wp/internal/module/permission/enums"
	permissionmodel "go_wp/internal/module/permission/model"
	"go_wp/pkg/casbin"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

type menuReferenceStub struct {
	menucontract.MenuService
	count int64
	err   error
}

func (s menuReferenceStub) CountByPermissionCodes(context.Context, []string) (int64, error) {
	return s.count, s.err
}

func TestUpdateSynchronizesAssignedCasbinPolicies(t *testing.T) {
	db := setupPermissionTestDB(t)
	entity := permissionmodel.PermissionEntity{
		ID:             1,
		PermissionCode: "admin:list",
		PermissionName: "管理员列表",
		Module:         "admin",
		APIPath:        "/api/admin/list",
		APIMethod:      "GET",
		Status:         permissionmodel.PermissionStatusEnabled,
	}
	if err := db.Session(&gorm.Session{SkipHooks: true}).Create(&entity).Error; err != nil {
		t.Fatalf("创建权限测试数据失败：%v", err)
	}
	if err := casbin.ReplaceRolePermissions("admin", [][3]string{{entity.APIPath, entity.APIMethod, entity.PermissionCode}}); err != nil {
		t.Fatalf("写入测试权限策略失败：%v", err)
	}

	svc := NewService(permissionmodel.NewPermissionModel(db))
	_, err := svc.Update(context.Background(), &permissiondto.UpdateReq{
		ID:             entity.ID,
		PermissionCode: entity.PermissionCode,
		PermissionName: entity.PermissionName,
		Module:         entity.Module,
		APIPath:        "/api/admin/query",
		APIMethod:      "POST",
		Status:         permissionmodel.PermissionStatusEnabled,
	})
	if err != nil {
		t.Fatalf("更新已分配权限失败：%v", err)
	}

	policies, err := casbin.GetRolePermissions("admin")
	if err != nil {
		t.Fatalf("查询更新后策略失败：%v", err)
	}
	if len(policies) != 1 || policies[0] != [3]string{"/api/admin/query", "POST", entity.PermissionCode} {
		t.Fatalf("Casbin 策略未同步：%v", policies)
	}

	_, err = svc.Update(context.Background(), &permissiondto.UpdateReq{
		ID:             entity.ID,
		PermissionCode: "admin:renamed",
		PermissionName: entity.PermissionName,
		Module:         entity.Module,
		APIPath:        "/api/admin/query",
		APIMethod:      "POST",
		Status:         permissionmodel.PermissionStatusEnabled,
	})
	if err == nil || !strings.Contains(err.Error(), permissionenums.ErrCodeImmutable) {
		t.Fatalf("修改权限编码应被拒绝，实际错误：%v", err)
	}

	_, err = svc.Update(context.Background(), &permissiondto.UpdateReq{
		ID:             entity.ID,
		PermissionCode: entity.PermissionCode,
		PermissionName: entity.PermissionName,
		Module:         entity.Module,
		APIPath:        "/api/admin/query",
		APIMethod:      "POST",
		Status:         permissionmodel.PermissionStatusDisabled,
	})
	if err == nil || !strings.Contains(err.Error(), permissionenums.ErrPermissionAssigned) {
		t.Fatalf("禁用已分配权限应被拒绝，实际错误：%v", err)
	}
}

func TestDeleteRejectsMenuReferences(t *testing.T) {
	db := setupPermissionTestDB(t)
	entity := permissionmodel.PermissionEntity{
		ID:             2,
		PermissionCode: "admin:delete",
		PermissionName: "删除管理员",
		Module:         "admin",
		APIPath:        "/api/admin/delete",
		APIMethod:      "POST",
		Status:         permissionmodel.PermissionStatusEnabled,
	}
	if err := db.Session(&gorm.Session{SkipHooks: true}).Create(&entity).Error; err != nil {
		t.Fatalf("创建权限测试数据失败：%v", err)
	}

	svc := NewService(permissionmodel.NewPermissionModel(db))
	svc.SetMenuService(menuReferenceStub{count: 1})
	_, err := svc.Delete(context.Background(), &permissiondto.DeleteReq{IDs: []uint64{entity.ID}})
	if err == nil || !strings.Contains(err.Error(), permissionenums.ErrMenuReferenced) {
		t.Fatalf("删除菜单引用权限应被拒绝，实际错误：%v", err)
	}
	if current, _ := svc.pm.GetByID(context.Background(), entity.ID); current == nil {
		t.Fatal("引用检查失败后权限记录被错误删除")
	}

	svc.SetMenuService(menuReferenceStub{})
	res, err := svc.Delete(context.Background(), &permissiondto.DeleteReq{IDs: []uint64{entity.ID}})
	if err != nil {
		t.Fatalf("删除未引用权限失败：%v", err)
	}
	if res.DeletedCount != 1 {
		t.Fatalf("权限删除数量错误：%d", res.DeletedCount)
	}
}

func setupPermissionTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("打开权限测试数据库失败：%v", err)
	}
	if err = db.AutoMigrate(&permissionmodel.PermissionEntity{}); err != nil {
		t.Fatalf("创建权限测试表失败：%v", err)
	}
	if err = casbin.InitCasbin(db); err != nil {
		t.Fatalf("初始化权限测试 Casbin 失败：%v", err)
	}
	t.Cleanup(func() { _ = casbin.Close() })
	return db
}
