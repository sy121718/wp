package projectservice_test

// site_lang_mode_test.go — 工程级语言 URL 方案的读取与「保存即生效」（真库）。
//
// 为什么放在真库上：判据全在「读的是不是这个工程的那一行 settings」上 —— 读错行、缓存住、
// 或者拿别的工程的值顶替，都不会报错，只会让某个站点按另一个站点的方案拼访问路径。
// 表结构来自生产迁移（support.NewMigratedPGTestDB 复制的模板库）。

import (
	"context"
	"testing"

	projectmodel "go_wp/internal/module/project/model"
	projectservice "go_wp/internal/module/project/service"
	"go_wp/public/test/support"
)

// TestSiteLangURLModePerProject 两个工程各配不同方案：各自读到自己的；未配置读空串；
// 改了设置之后**立即**读到新值（保存即生效 —— 读的就是这份 settings，没有进程级副本）。
func TestSiteLangURLModePerProject(t *testing.T) {
	db := support.NewMigratedPGTestDB(t)
	if db == nil {
		return // PG 不可用：库内测试已 t.Skip（与 support 的既定口径一致）
	}
	ctx := context.Background()

	insert := func(id, mode string) {
		t.Helper()
		settings := "{}"
		if mode != "" {
			settings = `{"langURLMode":"` + mode + `"}`
		}
		if err := db.Exec(
			"INSERT INTO projects (id, name, settings, create_time, update_time) VALUES (?, ?, ?::jsonb, now(), now())",
			id, "方案隔离用例", settings).Error; err != nil {
			t.Fatalf("准备工程行失败：%v", err)
		}
	}
	insert("10000000-0000-0000-0000-000000000001", "all_prefix")
	insert("10000000-0000-0000-0000-000000000002", "default_plain")
	insert("10000000-0000-0000-0000-000000000003", "") // 未配置

	svc := projectservice.NewService(projectmodel.NewProjectModel(db))

	got, err := svc.SiteLangURLMode(ctx, "10000000-0000-0000-0000-000000000001")
	if err != nil || got != "all_prefix" {
		t.Fatalf("工程 A 应读到 all_prefix，实际 %q err=%v", got, err)
	}
	got, err = svc.SiteLangURLMode(ctx, "10000000-0000-0000-0000-000000000002")
	if err != nil || got != "default_plain" {
		t.Fatalf("工程 B 应读到 default_plain（不受 A 影响），实际 %q err=%v", got, err)
	}
	got, err = svc.SiteLangURLMode(ctx, "10000000-0000-0000-0000-000000000003")
	if err != nil || got != "" {
		t.Fatalf("未配置的工程应读空串（调用方回退全局默认），实际 %q err=%v", got, err)
	}
	// 不存在的工程按「未配置」处理，不报错（工程刚被删、构建还在跑）。
	if got, err = svc.SiteLangURLMode(ctx, "20000000-0000-0000-0000-00000000000f"); err != nil || got != "" {
		t.Fatalf("不存在的工程应读空串且不报错，实际 %q err=%v", got, err)
	}

	// 保存即生效：改库后**无需任何进程级刷新**，下一次读就是新值。
	if err := db.Exec(
		`UPDATE projects SET settings = '{"langURLMode":"off"}'::jsonb, update_time = now() WHERE id = ?`,
		"10000000-0000-0000-0000-000000000001").Error; err != nil {
		t.Fatalf("更新工程设置失败：%v", err)
	}
	if got, err = svc.SiteLangURLMode(ctx, "10000000-0000-0000-0000-000000000001"); err != nil || got != "off" {
		t.Fatalf("保存后应立即读到 off，实际 %q err=%v", got, err)
	}
	// 且**没有**影响另一个工程（旧实现在这里会互相污染：全局值只有一个）。
	if got, err = svc.SiteLangURLMode(ctx, "10000000-0000-0000-0000-000000000002"); err != nil || got != "default_plain" {
		t.Fatalf("工程 B 不该被 A 的保存影响，实际 %q err=%v", got, err)
	}
}
