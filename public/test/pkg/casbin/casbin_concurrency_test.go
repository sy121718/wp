package casbin_test

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"gorm.io/gorm"

	pkgcasbin "go_wp/pkg/casbin"
	"go_wp/pkg/database"

	"github.com/spf13/viper"
)

// activateRole / deactivateRole：角色启用标记是事务内原子写（ActivateRoleTx /
// DeactivateRoleTx），非事务包装已随事务边界收口移除，测试侧自己开一个事务。
func activateRole(roleCode string) error {
	return inTx(func(tx *gorm.DB) error {
		_, err := pkgcasbin.ActivateRoleTx(context.Background(), tx, roleCode)
		return err
	})
}

func deactivateRole(roleCode string) error {
	return inTx(func(tx *gorm.DB) error {
		_, err := pkgcasbin.DeactivateRoleTx(context.Background(), tx, roleCode)
		return err
	})
}

func inTx(fn func(tx *gorm.DB) error) error {
	db, err := database.GetDB()
	if err != nil {
		return err
	}
	return db.Transaction(fn)
}

func TestCasbinConcurrentEnforceAndPolicyMutation(t *testing.T) {
	cfg := viper.New()
	cfg.Set("server.mode", "test")
	cfg.Set("database.driver", "postgres")
	cfg.Set("database.dbname", "wp_test")
	cfg.Set("database.host", "127.0.0.1")
	cfg.Set("database.port", 5432)
	cfg.Set("database.user", "root")
	cfg.Set("database.password", "root")
	cfg.Set("database.max_idle_conns", 2)
	cfg.Set("database.max_open_conns", 4)

	if err := database.Init(cfg); err != nil {
		t.Fatalf("初始化数据库失败: %v", err)
	}
	if err := pkgcasbin.Init(cfg); err != nil {
		t.Fatalf("初始化 Casbin 失败: %v", err)
	}
	t.Cleanup(func() {
		cleanupRaceFixtures(t)
		if err := pkgcasbin.Close(); err != nil {
			t.Fatalf("关闭 Casbin 失败: %v", err)
		}
		if err := database.Close(); err != nil {
			t.Fatalf("关闭数据库失败: %v", err)
		}
	})

	roleCode := fmt.Sprintf("race_role_%d", time.Now().UnixNano())
	userID := fmt.Sprintf("race_user_%d", time.Now().UnixNano())
	path := "/race/concurrency/api"
	code := "race_code_concurrency"
	cleanupRaceFixtures(t)

	if err := pkgcasbin.ReplaceRolePermissions(roleCode, [][3]string{{path, "GET", code}}); err != nil {
		t.Fatalf("写入角色基线策略失败: %v", err)
	}
	if err := pkgcasbin.ReplaceUserRoleBindings(userID, []string{roleCode}); err != nil {
		t.Fatalf("写入用户角色绑定失败: %v", err)
	}
	if err := activateRole(roleCode); err != nil {
		t.Fatalf("启用角色失败: %v", err)
	}

	ok, err := pkgcasbin.GetEnforcer().Enforce(userID, path, "GET")
	if err != nil || !ok {
		t.Fatalf("基线 Enforce 应命中: ok=%v err=%v", ok, err)
	}

	const (
		readers          = 50
		writers          = 4
		reloaders        = 4
		roleTogglers     = 2
		readLoopPerGorun = 50
		writeLoopPerGoru = 10
	)

	errCh := make(chan error, 4096)
	var wg sync.WaitGroup

	for i := 0; i < readers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			defer func() {
				if r := recover(); r != nil {
					errCh <- fmt.Errorf("reader %d panic: %v", i, r)
				}
			}()
			for j := 0; j < readLoopPerGorun; j++ {
				if _, err := pkgcasbin.GetEnforcer().Enforce(userID, path, "GET"); err != nil {
					errCh <- fmt.Errorf("reader %d Enforce: %w", i, err)
					return
				}
				pkgcasbin.GetCodeByURL(path, "GET")
			}
		}(i)
	}

	for i := 0; i < writers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			defer func() {
				if r := recover(); r != nil {
					errCh <- fmt.Errorf("writer %d panic: %v", i, r)
				}
			}()
			for j := 0; j < writeLoopPerGoru; j++ {

				policies := [][3]string{{path, "GET", code}}
				if j%2 == 1 {
					policies = [][3]string{{path, "GET", code}, {"/race/concurrency/alt", "POST", code}}
				}
				if err := pkgcasbin.ReplaceRolePermissions(roleCode, policies); err != nil {
					errCh <- fmt.Errorf("writer %d ReplaceRolePermissions: %w", i, err)
					return
				}
			}
		}(i)
	}

	for i := 0; i < reloaders; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			defer func() {
				if r := recover(); r != nil {
					errCh <- fmt.Errorf("reloader %d panic: %v", i, r)
				}
			}()
			for j := 0; j < writeLoopPerGoru; j++ {
				if err := pkgcasbin.ReloadPolicy(); err != nil {
					errCh <- fmt.Errorf("reloader %d ReloadPolicy: %w", i, err)
					return
				}
			}
		}(i)
	}

	for i := 0; i < roleTogglers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			defer func() {
				if r := recover(); r != nil {
					errCh <- fmt.Errorf("toggler %d panic: %v", i, r)
				}
			}()
			for j := 0; j < writeLoopPerGoru; j++ {
				if j%2 == 0 {
					if err := deactivateRole(roleCode); err != nil {
						errCh <- fmt.Errorf("toggler %d DeactivateRole: %w", i, err)
						return
					}
					if err := activateRole(roleCode); err != nil {
						errCh <- fmt.Errorf("toggler %d ActivateRole: %w", i, err)
						return
					}
				}
			}
		}(i)
	}

	wg.Wait()
	close(errCh)
	for err := range errCh {
		t.Errorf("并发阶段错误: %v", err)
	}

	if err := activateRole(roleCode); err != nil {
		t.Fatalf("收敛阶段启用角色失败: %v", err)
	}
	if err := pkgcasbin.ReplaceRolePermissions(roleCode, [][3]string{{path, "GET", code}}); err != nil {
		t.Fatalf("收敛阶段重写角色权限失败: %v", err)
	}
	if err := pkgcasbin.ReloadPolicy(); err != nil {
		t.Fatalf("收敛阶段刷新策略失败: %v", err)
	}
	ok, err = pkgcasbin.GetEnforcer().Enforce(userID, path, "GET")
	if err != nil || !ok {
		t.Fatalf("收敛后 Enforce 应命中: ok=%v err=%v", ok, err)
	}
	if _, hit := pkgcasbin.GetCodeByURL(path, "GET"); !hit {
		t.Fatalf("收敛后 GetCodeByURL 应命中")
	}
}

func cleanupRaceFixtures(t *testing.T) {
	t.Helper()
	db, err := database.GetDB()
	if err != nil {
		t.Logf("清理测试数据跳过（数据库未初始化）: %v", err)
		return
	}
	if err := db.Exec(
		`DELETE FROM sys_casbin_rule WHERE v0 LIKE 'race\_%' ESCAPE '\' OR v1 LIKE 'race\_%' ESCAPE '\'`,
	).Error; err != nil {
		t.Logf("清理测试数据失败: %v", err)
	}
}
