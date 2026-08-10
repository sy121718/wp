package adminservice

import (
	"context"
	"strings"
	"testing"
	"time"

	admindto "go_wp/internal/module/admin/dto"
	adminenums "go_wp/internal/module/admin/enums"
	adminmodel "go_wp/internal/module/admin/model"
	menucontract "go_wp/internal/module/menu/contract"
	rolecontract "go_wp/internal/module/role/contract"
	"go_wp/pkg/auth"
	"go_wp/pkg/cache"
	"go_wp/pkg/casbin"

	"github.com/alicebob/miniredis/v2"
	"github.com/glebarez/sqlite"
	"github.com/spf13/viper"
	"gorm.io/gorm"
)

func TestAdminLifecycle(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("打开测试数据库失败：%v", err)
	}
	if err = db.AutoMigrate(&adminmodel.AdminEntity{}); err != nil {
		t.Fatalf("创建管理员测试表失败：%v", err)
	}

	redisServer := miniredis.RunT(t)
	cfg := viper.New()
	cfg.Set("redis.addrs", []string{redisServer.Addr()})
	cfg.Set("jwt.secret", "admin-lifecycle-test-secret")
	cfg.Set("jwt.expire_time", 24)
	if err = cache.Init(cfg); err != nil {
		t.Fatalf("初始化测试缓存失败：%v", err)
	}
	t.Cleanup(func() { _ = cache.Close() })
	if err = auth.Init(cfg); err != nil {
		t.Fatalf("初始化测试 JWT 失败：%v", err)
	}
	t.Cleanup(func() { _ = auth.Close() })
	if err = casbin.InitCasbin(db); err != nil {
		t.Fatalf("初始化测试 Casbin 失败：%v", err)
	}
	t.Cleanup(func() { _ = casbin.Close() })

	superAdmin := adminmodel.AdminEntity{ID: 1, Username: "root", Password: "hash", Status: adminmodel.AdminStatusActive, IsAdmin: 1}
	ordinary := adminmodel.AdminEntity{ID: 2, Username: "editor", Password: "hash", Status: adminmodel.AdminStatusActive}
	if err = db.Session(&gorm.Session{SkipHooks: true}).Create(&superAdmin).Error; err != nil {
		t.Fatalf("创建超级管理员测试数据失败：%v", err)
	}
	if err = db.Session(&gorm.Session{SkipHooks: true}).Create(&ordinary).Error; err != nil {
		t.Fatalf("创建普通管理员测试数据失败：%v", err)
	}

	svc := NewService(adminmodel.NewAdminModel(db), nil, nil, nil)
	ctx := context.Background()

	if _, err = svc.Delete(ctx, &admindto.DeleteReq{Id: []uint64{2}, OperatorID: 2}); err == nil || !strings.Contains(err.Error(), adminenums.ErrDeleteSelf) {
		t.Fatalf("删除自己应被拒绝，实际错误：%v", err)
	}
	if _, err = svc.Delete(ctx, &admindto.DeleteReq{Id: []uint64{1}, OperatorID: 2}); err == nil || !strings.Contains(err.Error(), adminenums.ErrDeleteSuperAdmin) {
		t.Fatalf("删除超级管理员应被拒绝，实际错误：%v", err)
	}

	if err = auth.SaveUserSession(ctx, &auth.UserSession{ID: ordinary.ID, Username: ordinary.Username}, time.Hour); err != nil {
		t.Fatalf("写入测试会话失败：%v", err)
	}
	if err = casbin.ReplaceUserRoleBindings("2", []string{"editor"}); err != nil {
		t.Fatalf("写入测试角色绑定失败：%v", err)
	}
	if err = casbin.ReplaceUserPermissions("2", [][3]string{{"/api/test", "GET", "test:view"}}); err != nil {
		t.Fatalf("写入测试直接权限失败：%v", err)
	}

	res, err := svc.Delete(ctx, &admindto.DeleteReq{Id: []uint64{2, 2}, OperatorID: 1})
	if err != nil {
		t.Fatalf("删除普通管理员失败：%v", err)
	}
	if res.DeletedCount != 1 {
		t.Fatalf("重复 ID 未去重，实际删除数量：%d", res.DeletedCount)
	}
	deleted, err := svc.am.GetByID(ctx, 2)
	if err != nil || deleted != nil {
		t.Fatalf("管理员记录未删除：entity=%v err=%v", deleted, err)
	}
	if session, _ := auth.GetUserSession(ctx, 2); session != nil {
		t.Fatalf("管理员会话未清理：%+v", session)
	}
	blocked, err := auth.IsBlocked(ctx, 2, time.Now().Add(-time.Minute).Unix())
	if err != nil || !blocked {
		t.Fatalf("管理员旧 token 未撤销：blocked=%v err=%v", blocked, err)
	}
	roles, err := casbin.GetRoleCodesByUserID("2")
	if err != nil || len(roles) != 0 {
		t.Fatalf("管理员角色绑定未清理：roles=%v err=%v", roles, err)
	}
	permissions, err := casbin.GetUserDirectPermissions("2")
	if err != nil || len(permissions) != 0 {
		t.Fatalf("管理员直接权限未清理：permissions=%v err=%v", permissions, err)
	}
}

type roleCodesStub struct {
	rolecontract.RoleService
	codes []string
}

func (s roleCodesStub) GetRoleCodesByUserID(context.Context, uint64) ([]string, error) {
	return s.codes, nil
}

type permissionMenuStub struct {
	menucontract.MenuService
	idsByCode map[string]uint64
}

func (s permissionMenuStub) GetIDsByPermissionCodes(_ context.Context, codes []string) ([]uint64, error) {
	ids := make([]uint64, 0, len(codes))
	for _, code := range codes {
		if id, exists := s.idsByCode[code]; exists {
			ids = append(ids, id)
		}
	}
	return ids, nil
}

func TestMenuListUsesEnabledRolesOnly(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("打开测试数据库失败：%v", err)
	}
	if err = casbin.InitCasbin(db); err != nil {
		t.Fatalf("初始化测试 Casbin 失败：%v", err)
	}
	t.Cleanup(func() { _ = casbin.Close() })

	if err = casbin.ReplaceUserPermissions("9", [][3]string{{"/direct", "GET", "direct:view"}}); err != nil {
		t.Fatalf("写入用户直接权限失败：%v", err)
	}
	if err = casbin.ReplaceRolePermissions("active", [][3]string{{"/active", "GET", "active:view"}}); err != nil {
		t.Fatalf("写入启用角色权限失败：%v", err)
	}
	if err = casbin.ReplaceRolePermissions("disabled", [][3]string{{"/disabled", "GET", "disabled:view"}}); err != nil {
		t.Fatalf("写入禁用角色权限失败：%v", err)
	}

	svc := NewService(
		nil,
		permissionMenuStub{idsByCode: map[string]uint64{
			"direct:view":   1,
			"active:view":   2,
			"disabled:view": 3,
		}},
		roleCodesStub{codes: []string{"active"}},
		nil,
	)
	res, err := svc.MenuList(context.Background(), &admindto.MenuListReq{UserID: 9})
	if err != nil {
		t.Fatalf("查询管理员有效菜单失败：%v", err)
	}

	effective := make(map[uint64]struct{}, len(res.EffectiveMenuIDs))
	for _, id := range res.EffectiveMenuIDs {
		effective[id] = struct{}{}
	}
	if _, exists := effective[1]; !exists {
		t.Fatal("用户直接权限未计入有效菜单")
	}
	if _, exists := effective[2]; !exists {
		t.Fatal("启用角色权限未计入有效菜单")
	}
	if _, exists := effective[3]; exists {
		t.Fatal("禁用角色权限被错误计入有效菜单")
	}
}

func TestEditInvalidatesUserSession(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("打开测试数据库失败：%v", err)
	}
	if err = db.AutoMigrate(&adminmodel.AdminEntity{}); err != nil {
		t.Fatalf("创建管理员测试表失败：%v", err)
	}

	redisServer := miniredis.RunT(t)
	cfg := viper.New()
	cfg.Set("redis.addrs", []string{redisServer.Addr()})
	if err = cache.Init(cfg); err != nil {
		t.Fatalf("初始化测试缓存失败：%v", err)
	}
	t.Cleanup(func() { _ = cache.Close() })

	entity := adminmodel.AdminEntity{ID: 3, Username: "old-name", Password: "hash", Status: adminmodel.AdminStatusActive}
	if err = db.Session(&gorm.Session{SkipHooks: true}).Create(&entity).Error; err != nil {
		t.Fatalf("创建管理员测试数据失败：%v", err)
	}
	ctx := context.Background()
	if err = auth.SaveUserSession(ctx, &auth.UserSession{ID: entity.ID, Username: entity.Username}, time.Hour); err != nil {
		t.Fatalf("写入测试会话失败：%v", err)
	}

	svc := NewService(adminmodel.NewAdminModel(db), nil, nil, nil)
	if _, err = svc.Edit(ctx, &admindto.EditReq{Id: entity.ID, Username: "new-name", Email: "new@example.com"}); err != nil {
		t.Fatalf("编辑管理员失败：%v", err)
	}
	if session, _ := auth.GetUserSession(ctx, entity.ID); session != nil {
		t.Fatalf("管理员编辑后会话未失效：%+v", session)
	}
}
