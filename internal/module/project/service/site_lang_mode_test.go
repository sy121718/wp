package projectservice_test

import (
	"context"
	"testing"

	projectmodel "go_wp/internal/module/project/model"
	projectservice "go_wp/internal/module/project/service"
	"go_wp/public/test/support"
)

func TestSiteLangURLModePerProject(t *testing.T) {
	db := support.NewMigratedPGTestDB(t)
	if db == nil {
		return
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
	insert("10000000-0000-0000-0000-000000000003", "")

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

	if got, err = svc.SiteLangURLMode(ctx, "20000000-0000-0000-0000-00000000000f"); err != nil || got != "" {
		t.Fatalf("不存在的工程应读空串且不报错，实际 %q err=%v", got, err)
	}

	if err := db.Exec(
		`UPDATE projects SET settings = '{"langURLMode":"off"}'::jsonb, update_time = now() WHERE id = ?`,
		"10000000-0000-0000-0000-000000000001").Error; err != nil {
		t.Fatalf("更新工程设置失败：%v", err)
	}
	if got, err = svc.SiteLangURLMode(ctx, "10000000-0000-0000-0000-000000000001"); err != nil || got != "off" {
		t.Fatalf("保存后应立即读到 off，实际 %q err=%v", got, err)
	}

	if got, err = svc.SiteLangURLMode(ctx, "10000000-0000-0000-0000-000000000002"); err != nil || got != "default_plain" {
		t.Fatalf("工程 B 不该被 A 的保存影响，实际 %q err=%v", got, err)
	}
}
