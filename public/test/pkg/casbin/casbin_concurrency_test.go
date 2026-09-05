package casbin_test

import (
	"fmt"
	"sync"
	"testing"
	"time"

	pkgcasbin "go_wp/pkg/casbin"
	"go_wp/pkg/database"

	"github.com/spf13/viper"
)

// 并发回归测试（审计 C1/H1/H2）：
//   - C1：SyncedEnforcer 保证 Enforce 热路径读与策略写并发无数据竞态；
//   - H1：urlCodeMap 以 atomic.Pointer 原子替换，rebuild 与 GetCodeByURL 并发安全；
//   - H2：多步策略写（ReplaceRolePermissions/ReloadPolicy/启停角色）被 policyMu 串行化，
//     读者不会观察到「策略先删空再逐条补回」的中间态。
//
// 运行方式（必须在 -race 下验证）：
//
//	go test -race -run TestCasbinConcurrentEnforceAndPolicyMutation ./public/test/pkg/casbin/
//
// 依赖本地 PostgreSQL（127.0.0.1:5432，wp_test 库），与 casbin_functional_test.go 一致。
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

	// 唯一测试主体，避免与库中既有策略相互干扰
	roleCode := fmt.Sprintf("race_role_%d", time.Now().UnixNano())
	userID := fmt.Sprintf("race_user_%d", time.Now().UnixNano())
	path := "/race/concurrency/api"
	code := "race_code_concurrency"
	cleanupRaceFixtures(t)

	// 建立基线策略：用户绑定角色 + 角色持有一条 p 策略 + 角色启用
	if err := pkgcasbin.ReplaceRolePermissions(roleCode, [][3]string{{path, "GET", code}}); err != nil {
		t.Fatalf("写入角色基线策略失败: %v", err)
	}
	if err := pkgcasbin.ReplaceUserRoleBindings(userID, []string{roleCode}); err != nil {
		t.Fatalf("写入用户角色绑定失败: %v", err)
	}
	if err := pkgcasbin.ActivateRole(roleCode); err != nil {
		t.Fatalf("启用角色失败: %v", err)
	}

	ok, err := pkgcasbin.GetEnforcer().Enforce(userID, path, "GET")
	if err != nil || !ok {
		t.Fatalf("基线 Enforce 应命中: ok=%v err=%v", ok, err)
	}

	// ---------- 并发阶段 ----------
	const (
		readers          = 50 // 并发 Enforce goroutine（审计要求的 50 读并发）
		writers          = 4  // 并发 ReplaceRolePermissions
		reloaders        = 4  // 并发 ReloadPolicy
		roleTogglers     = 2  // 并发 Activate/Deactivate（g2 写入口）
		readLoopPerGorun = 50
		writeLoopPerGoru = 10
	)

	errCh := make(chan error, 4096)
	var wg sync.WaitGroup

	// 读端：50 goroutine 持续 Enforce + GetCodeByURL（H1 读路径）+ 查询接口
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
				pkgcasbin.GetCodeByURL(path, "GET") // 命中与否均合法，只验证并发安全
			}
		}(i)
	}

	// 写端 A：并发替换角色权限（多步 Remove/Add + reload）
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
				// 交替两套策略集，制造「删空再补回」的中间态窗口
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

	// 写端 B：并发 ReloadPolicy（独立入口，走导出 API 自行加锁）
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

	// 写端 C：并发启停角色（g2 命名分组写入口）
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
					if err := pkgcasbin.DeactivateRole(roleCode); err != nil {
						errCh <- fmt.Errorf("toggler %d DeactivateRole: %w", i, err)
						return
					}
					if err := pkgcasbin.ActivateRole(roleCode); err != nil {
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

	// ---------- 收敛断言：并发写结束后系统能收敛到一致状态 ----------
	if err := pkgcasbin.ActivateRole(roleCode); err != nil {
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

// cleanupRaceFixtures 清理并发测试写入 sys_casbin_rule 的 race_ 前缀数据。
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
