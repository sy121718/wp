package sysconfigservice_test

// sysconfig_set_test.go — 整组保存的乐观锁真库验证。
//
// 为什么不放在纯逻辑单测里：本用例的价值全在 SQL 与并发语义上 —— 「守卫写进 WHERE」
// 与「先读出来比对再写回」在单进程顺序调用下**表现完全一样**，两种实现在编译期与
// 单元测试里都看不出差别；能区分它们的是「带旧版本号再写一次」的结果：前者 0 行拒绝、
// 后者静默覆盖。这条判据错了不会报错，只会让人以为「保存成功了」而别人的改动没了。
//
// 表结构来自**生产迁移**（support.NewMigratedPGTestDB 复制的模板库）：sys_config 由
// 迁移 484 建出 —— 不手抄 CREATE TABLE（手抄会与生产 schema 静默分叉）。

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

// TestSetGroupOptimisticLock 乐观锁三连：成功推进版本 → 旧版本被拒 → 结构不存在被区分。
func TestSetGroupOptimisticLock(t *testing.T) {
	db := support.NewMigratedPGTestDB(t)
	if db == nil {
		return // PG 不可用：库内测试已 t.Skip（与 support 的既定口径一致）
	}
	ctx := context.Background()

	// 铺初始行：只给本用例自己的分组键，不用 seed 的全量数据。
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

	// ① 带正确版本保存：成功，版本推进到 2（返回体必须给出新版本号，否则后台下一次
	//    保存必然用旧版本号撞冲突）。
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

	// ② 再用**同一个旧版本号**保存（另一个管理员手里那份）：必须拒绝，且库内不被覆盖。
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

	// ③ 组不存在与版本冲突必须分开报（提示语不同：一个让人刷新重试，一个让人先建组）。
	if _, err = svc.SetGroup(ctx, &sysconfigdto.SetGroupReq{
		GroupKey: "not-exists", Data: map[string]any{"k": "v"}, Version: 1,
	}); !errors.Is(err, sysconfigcontract.ErrGroupNotFound) {
		t.Fatalf("不存在的组应报 GroupNotFound 哨兵错误，实际：%v", err)
	}

	// ④ 缺版本号直接拒绝（不给「省略即强制覆盖」留口子），且不落库。
	if _, err = svc.SetGroup(ctx, &sysconfigdto.SetGroupReq{
		GroupKey: "other", Data: map[string]any{"k": "v"}, Version: 0,
	}); err == nil || err.Error() != sysconfigenums.ErrVersionRequired {
		t.Fatalf("缺版本号应被拒绝，实际：%v", err)
	}

	// ⑤ 空数据同样拒绝：整组替换成空对象 = 把这一组的所有键悄悄删掉。
	if _, err = svc.SetGroup(ctx, &sysconfigdto.SetGroupReq{
		GroupKey: "other", Data: map[string]any{}, Version: 5,
	}); err == nil || err.Error() != sysconfigenums.ErrInvalidParam {
		t.Fatalf("空数据应被拒绝，实际：%v", err)
	}
}
