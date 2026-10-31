package sysconfigservice_test

import (
	"context"
	"errors"
	"testing"

	sysconfigcontract "go_wp/internal/module/sysconfig/contract"
	sysconfigdto "go_wp/internal/module/sysconfig/dto"
	sysconfigenums "go_wp/internal/module/sysconfig/enums"
	sysconfigmodel "go_wp/internal/module/sysconfig/model"
	sysconfigservice "go_wp/internal/module/sysconfig/service"
	"go_wp/public/test/support"
)

func TestSetGroupOptimisticLock(t *testing.T) {
	db := support.NewMigratedPGTestDB(t)
	if db == nil {
		return
	}
	ctx := context.Background()

	if err := db.Exec(
		"INSERT INTO sys_config (group_key, group_name, config_data, status, version, create_by, update_by) " +
			"VALUES ('i18n', '国际化', '{\"default_lang\":\"zh-CN\"}'::jsonb, 1, 1, 0, 0)").Error; err != nil {
		t.Fatalf("准备 sys_config 初始行失败：%v", err)
	}
	if err := db.Exec(
		"INSERT INTO sys_config (group_key, group_name, config_data, status, version, create_by, update_by) " +
			"VALUES ('other', '其它', '{}'::jsonb, 1, 5, 0, 0)").Error; err != nil {
		t.Fatalf("准备 sys_config 初始行失败：%v", err)
	}

	m := sysconfigmodel.NewSysConfigModel(db)
	changed := 0
	svc := sysconfigservice.NewService(m, func() { changed++ })

	saved, err := svc.SetGroup(ctx, &sysconfigdto.SetGroupReq{
		GroupKey: "i18n",
		Data:     map[string]any{"default_lang": "en-US", "site_lang_url_mode": "all_prefix"},
		Version:  1,
	})
	if err != nil {
		t.Fatalf("首次保存应当成功，实际：%v", err)
	}
	if saved.Version != 2 {
		t.Fatalf("保存后版本应为 2，实际 %d", saved.Version)
	}
	if changed != 1 {
		t.Fatalf("保存成功应触发一次主动刷新，实际 %d 次", changed)
	}

	if _, err = svc.SetGroup(ctx, &sysconfigdto.SetGroupReq{
		GroupKey: "i18n",
		Data:     map[string]any{"default_lang": "ja-JP"},
		Version:  1,
	}); err == nil || err.Error() != sysconfigenums.ErrVersionConflict {
		t.Fatalf("旧版本保存应报版本冲突，实际：%v", err)
	}
	if changed != 1 {
		t.Fatalf("冲突不该触发主动刷新，实际 %d 次", changed)
	}
	got, err := svc.GetGroup(ctx, "i18n")
	if err != nil {
		t.Fatalf("读回失败：%v", err)
	}
	if got.Data["default_lang"] != "en-US" || got.Version != 2 {
		t.Fatalf("冲突保存不得落库：data=%v version=%d", got.Data, got.Version)
	}

	if _, err = svc.SetGroup(ctx, &sysconfigdto.SetGroupReq{
		GroupKey: "not-exists", Data: map[string]any{"k": "v"}, Version: 1,
	}); !errors.Is(err, sysconfigcontract.ErrGroupNotFound) {
		t.Fatalf("不存在的组应报 GroupNotFound 哨兵错误，实际：%v", err)
	}

	if _, err = svc.SetGroup(ctx, &sysconfigdto.SetGroupReq{
		GroupKey: "other", Data: map[string]any{"k": "v"}, Version: 0,
	}); err == nil || err.Error() != sysconfigenums.ErrVersionRequired {
		t.Fatalf("缺版本号应被拒绝，实际：%v", err)
	}

	if _, err = svc.SetGroup(ctx, &sysconfigdto.SetGroupReq{
		GroupKey: "other", Data: map[string]any{}, Version: 5,
	}); err == nil || err.Error() != sysconfigenums.ErrInvalidParam {
		t.Fatalf("空数据应被拒绝，实际：%v", err)
	}
}
