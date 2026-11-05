package migrations_test

import (
	"strings"
	"testing"

	"go_wp/public/migrations"
	"go_wp/public/test/support"
)

// 743 是「启动不再反写权限目录」之后唯一的全量落点。
// 这里钉住它确实覆盖全部已声明码，且仅补缺、不覆盖人工列。
func TestPermissionDirectoryBackfillCoversDeclaredCodes(t *testing.T) {
	var found migrations.Seed
	for _, s := range migrations.AllSeeds() {
		if s.Version == "743-permission-directory-backfill" {
			found = s
			break
		}
	}
	if found.Version == "" {
		t.Fatal("743-permission-directory-backfill 未注册：新库会缺 60 个只由旧启动反写补入的权限点")
	}
	if !strings.Contains(found.SQL, "ON CONFLICT (permission_code) DO NOTHING") {
		t.Fatal("补目录必须是 ON CONFLICT DO NOTHING：覆盖会改掉用户自定义的名称/备注/启停")
	}
	if strings.Contains(found.SQL, "UPDATE ") {
		t.Fatal("补目录不得带 UPDATE：路由漂移要由显式版本迁移修，不能每次启动重写")
	}
	// 363 是任务书核销的常量总数（不是历史报告里的 359）。
	if !strings.Contains(found.ConditionSQL, ">= 363") {
		t.Errorf("条件应按 363 个已知码判定，实际：%s", found.ConditionSQL)
	}
	var n int
	for _, line := range strings.Split(found.SQL, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "('") {
			n++
		}
	}
	if n != 363 {
		t.Errorf("VALUES 行数应为 363，实际 %d", n)
	}
}

// TestFreshDatabaseHasCompletePermissionDirectory 是任务书「权限单源」的落地判据：
// 新装库跑完迁移与 seed 后，363 个已声明码必须全部落库、启用、且路径/方法可查。
func TestFreshDatabaseHasCompletePermissionDirectory(t *testing.T) {
	db := support.NewMigratedPGTestDB(t)
	if err := migrations.RunSeeds(db); err != nil {
		t.Fatalf("跑种子失败：%v", err)
	}

	var total, enabled int64
	if err := db.Raw(`SELECT COUNT(*), COUNT(*) FILTER (WHERE status = 1) FROM sys_permission`).
		Row().Scan(&total, &enabled); err != nil {
		t.Fatalf("统计权限点失败：%v", err)
	}
	if total < 363 {
		t.Errorf("新装库应有 ≥363 个权限点，实际 %d —— 743 没有补齐只靠旧启动反写的那批", total)
	}
	if enabled < 363 {
		t.Errorf("新装库应有 ≥363 个启用权限点，实际 %d", enabled)
	}

	var blank int64
	if err := db.Raw(`SELECT COUNT(*) FROM sys_permission
		WHERE coalesce(permission_name, '') = '' OR coalesce(api_path, '') = '' OR coalesce(api_method, '') = ''`).
		Scan(&blank).Error; err != nil {
		t.Fatalf("检查空列失败：%v", err)
	}
	if blank != 0 {
		t.Errorf("有 %d 个权限点缺名称或路由（授权界面勾不上、策略永不命中）", blank)
	}
}
