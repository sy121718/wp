package migrations_test

import (
	"strings"
	"testing"

	"go_wp/public/migrations"
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
