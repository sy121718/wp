package casbin_test

import (
	"fmt"
	"testing"
	"time"

	pkgcasbin "go_wp/pkg/casbin"
	"go_wp/pkg/database"

	"github.com/spf13/viper"
)

var businessPermissionDefs = [][3]string{
	{"/api/page/list", "GET", "page:list"},
	{"/api/page/detail", "GET", "page:detail"},
	{"/api/page/create", "POST", "page:create"},
	{"/api/page/draft/save", "POST", "page:draft_save"},
	{"/api/page/revision/list", "GET", "page:revision_list"},
	{"/api/page/build", "POST", "page:build"},
	{"/api/page/publish", "POST", "page:publish"},
	{"/api/page/rollback", "POST", "page:rollback"},
	{"/api/page/url/update", "POST", "page:url_update"},
	{"/api/project/list", "GET", "project:list"},
	{"/api/project/detail", "GET", "project:detail"},
	{"/api/project/create", "POST", "project:create"},
	{"/api/project/update", "POST", "project:update"},
	{"/api/block/list", "GET", "block:list"},
	{"/api/block/detail", "GET", "block:detail"},
	{"/api/block/create", "POST", "block:create"},
	{"/api/block/update", "POST", "block:update"},
	{"/api/block/delete", "POST", "block:delete"},
	{"/api/media/list", "GET", "media:list"},
	{"/api/media/detail", "GET", "media:detail"},
	{"/api/media/upload", "POST", "media:upload"},
	{"/api/media/update", "POST", "media:update"},
	{"/api/media/delete", "POST", "media:delete"},
	{"/api/media/category/tree", "GET", "media:category_tree"},
	{"/api/media/category/create", "POST", "media:category_create"},
	{"/api/media/category/update", "POST", "media:category_update"},
	{"/api/media/category/delete", "POST", "media:category_delete"},
	{"/api/artifact/detail", "GET", "artifact:detail"},
	{"/api/publication/receipts/pending", "GET", "publication:receipts_pending"},
}

func newCasbinTestEnv(t *testing.T) {
	t.Helper()

	cfg := viper.New()
	cfg.Set("server.mode", "test")
	cfg.Set("database.driver", "postgres")
	cfg.Set("database.dbname", "wp_test")
	cfg.Set("database.host", "127.0.0.1")
	cfg.Set("database.port", 5432)
	cfg.Set("database.user", "root")
	cfg.Set("database.password", "root")
	cfg.Set("database.max_idle_conns", 1)
	cfg.Set("database.max_open_conns", 2)

	if err := database.Init(cfg); err != nil {
		t.Fatalf("初始化数据库失败: %v", err)
	}
	if err := pkgcasbin.Init(cfg); err != nil {
		t.Fatalf("初始化 Casbin 失败: %v", err)
	}
	t.Cleanup(func() {
		cleanupBusinessFixtures(t)
		if err := pkgcasbin.Close(); err != nil {
			t.Fatalf("关闭 Casbin 失败: %v", err)
		}
		if err := database.Close(); err != nil {
			t.Fatalf("关闭数据库失败: %v", err)
		}
	})
}

func TestBusinessPermissionSuperAdminDirectPolicies(t *testing.T) {
	newCasbinTestEnv(t)

	superAdmin := fmt.Sprintf("biz_super_%d", time.Now().UnixNano())
	if err := pkgcasbin.ReplaceUserPermissions(superAdmin, businessPermissionDefs); err != nil {
		t.Fatalf("写入超管直接策略失败: %v", err)
	}

	for _, def := range businessPermissionDefs {
		ok, err := pkgcasbin.GetEnforcer().Enforce(superAdmin, def[0], def[1])
		if err != nil || !ok {
			t.Fatalf("超管应命中 %s %s (code=%s): ok=%v err=%v", def[1], def[0], def[2], ok, err)
		}

		code, hit := pkgcasbin.GetCodeByURL(def[0], def[1])
		if !hit || code != def[2] {
			t.Fatalf("GetCodeByURL(%s,%s) 应命中 %s: got code=%q hit=%v", def[0], def[1], def[2], code, hit)
		}
	}

	ok, err := pkgcasbin.GetEnforcer().Enforce(superAdmin, "/api/project/delete", "POST")
	if err != nil || ok {
		t.Fatalf("未定义权限点的路径应拒绝: ok=%v err=%v", ok, err)
	}
}

func TestBusinessPermissionNonSuperAdminDeniedByDefault(t *testing.T) {
	newCasbinTestEnv(t)

	nobody := fmt.Sprintf("biz_nobody_%d", time.Now().UnixNano())
	for _, def := range businessPermissionDefs {
		ok, err := pkgcasbin.GetEnforcer().Enforce(nobody, def[0], def[1])
		if err != nil || ok {
			t.Fatalf("无策略用户应拒绝 %s %s (code=%s): ok=%v err=%v", def[1], def[0], def[2], ok, err)
		}
	}
}

func TestBusinessPermissionRoleInheritance(t *testing.T) {
	newCasbinTestEnv(t)

	ts := time.Now().UnixNano()
	roleCode := fmt.Sprintf("biz_role_%d", ts)
	member := fmt.Sprintf("biz_member_%d", ts)

	if err := pkgcasbin.ReplaceRolePermissions(roleCode, businessPermissionDefs); err != nil {
		t.Fatalf("写入角色业务权限失败: %v", err)
	}
	if err := pkgcasbin.ActivateRole(roleCode); err != nil {
		t.Fatalf("启用角色失败: %v", err)
	}
	if err := pkgcasbin.ReplaceUserRoleBindings(member, []string{roleCode}); err != nil {
		t.Fatalf("绑定用户角色失败: %v", err)
	}

	ok, err := pkgcasbin.GetEnforcer().Enforce(member, "/api/page/list", "GET")
	if err != nil || !ok {
		t.Fatalf("角色成员应命中 /api/page/list GET: ok=%v err=%v", ok, err)
	}
	ok, err = pkgcasbin.GetEnforcer().Enforce(member, "/api/page/publish", "POST")
	if err != nil || !ok {
		t.Fatalf("角色成员应命中 /api/page/publish POST: ok=%v err=%v", ok, err)
	}

	ok, err = pkgcasbin.GetEnforcer().Enforce(member, "/api/project/delete", "POST")
	if err != nil || ok {
		t.Fatalf("未授权路径应拒绝: ok=%v err=%v", ok, err)
	}
}

func cleanupBusinessFixtures(t *testing.T) {
	t.Helper()
	db, err := database.GetDB()
	if err != nil {
		t.Logf("清理测试数据跳过（数据库未初始化）: %v", err)
		return
	}
	if err := db.Exec(
		`DELETE FROM sys_casbin_rule WHERE v0 LIKE 'biz\_%' ESCAPE '\' OR v1 LIKE 'biz\_%' ESCAPE '\'`,
	).Error; err != nil {
		t.Logf("清理测试数据失败: %v", err)
	}
}
