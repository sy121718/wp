package projectservice_test

// locale_admission_test.go — 站点级语言准入（U1）：新增 / 新启用的语言必须有界面词条。
//
// 判据的价值在**边界**上：多校一个已存在的语言，会让老站点在判据调整后整体保存不了；
// 少校一次读不到词条数的失败，会静默启用一个空语言（整站回退原文）。
// 两者都不报错、只在运营眼里表现为「后台坏了」，所以放在真库上逐条钉。

import (
	"context"
	"errors"
	"testing"

	"go_wp/config"
	"go_wp/pkg/database"

	projectdto "go_wp/internal/module/project/dto"
	projectmodel "go_wp/internal/module/project/model"
	projectservice "go_wp/internal/module/project/service"
)

// 说明：pkg/i18n 的词条计数口走**全局数据库句柄**（pkg/i18n 持有 sys_i18n），
// 因此本用例连开发库并在其中建一个**临时工程**（用完删除），而不是 support 的隔离 schema
// —— 隔离 schema 里 database.GetDB() 指向的不是测试库，计数口拿不到它。
func TestSaveLocalesRequiresTranslations(t *testing.T) {
	if err := config.Init("../../../../config.yaml"); err != nil {
		t.Skipf("读取配置失败，跳过：%v", err)
	}
	cfg, err := config.GetViper()
	if err != nil {
		t.Skipf("取配置失败，跳过：%v", err)
	}
	if err := database.Init(cfg); err != nil {
		t.Skipf("本地 PostgreSQL 不可用，跳过：%v", err)
	}
	db, err := database.GetDB()
	if err != nil {
		t.Skipf("数据库实例不可用，跳过：%v", err)
	}
	ctx := context.Background()

	// 临时工程（名称带前缀，便于识别与清理）。
	projectID := "40000000-0000-0000-0000-00000000ad01"
	if err := db.Exec("DELETE FROM projects WHERE id = ?", projectID).Error; err != nil {
		t.Fatalf("清理历史临时工程失败：%v", err)
	}
	if err := db.Exec(
		"INSERT INTO projects (id, name, settings, create_time, update_time) VALUES (?, '准入验收临时工程', '{}'::jsonb, now(), now())",
		projectID).Error; err != nil {
		t.Fatalf("建临时工程失败：%v", err)
	}
	t.Cleanup(func() {
		_ = db.Exec("DELETE FROM project_locales WHERE project_id = ?", projectID).Error
		_ = db.Exec("DELETE FROM projects WHERE id = ?", projectID).Error
	})

	svc := projectservice.NewService(projectmodel.NewProjectModel(db))
	save := func(langs ...string) error {
		t.Helper()
		items := make([]projectdto.LocaleItem, 0, len(langs))
		for i, l := range langs {
			enabled := true
			items = append(items, projectdto.LocaleItem{Lang: l, IsDefault: i == 0, Enabled: &enabled})
		}
		_, err := svc.SaveLocales(ctx, &projectdto.LocalesSaveReq{ProjectID: projectID, Locales: items})
		return err
	}
	countOf := func(lang string) int64 {
		var n int64
		if err := db.Raw("SELECT count(*) FROM sys_i18n WHERE lang = ? AND status = 1", lang).Scan(&n).Error; err != nil {
			t.Fatalf("统计词条失败：%v", err)
		}
		return n
	}
	t.Logf("词条数：zh-CN=%d en-AU=%d de-DE=%d qq-ZZ=%d", countOf("zh-CN"), countOf("en-AU"), countOf("de-DE"), countOf("qq-ZZ"))

	// ① 有词条的语言（开发库里只有 zh-CN 有词条：5195 条启用中）→ 放行。
	if err := save("zh-CN"); err != nil {
		t.Fatalf("有词条的语言应放行，实际：%v", err)
	}
	t.Log("① 新增 zh-CN（有词条）→ 放行")

	// ② 无词条的语言 → 拒绝，且是可展示的业务错误（读侧据此 400 + 文案）。
	err = save("zh-CN", "de-DE")
	if !errors.Is(err, projectservice.ErrLocaleNoTranslations) {
		t.Fatalf("无词条语言应被拒绝（ErrLocaleNoTranslations），实际：%v", err)
	}
	t.Logf("② 新增 de-DE（词条数 0）→ 被拒：%v", err)

	// ③ 只校新增 / 新启用：把 zh-CN 的词条临时停用（模拟「判据变化 / 存量数据变动」），
	//    再保存同一份清单仍放行 —— 否则老站点会因为存量数据被整体拒绝保存。
	if err := db.Exec("UPDATE sys_i18n SET status = 0 WHERE lang = 'zh-CN'").Error; err != nil {
		t.Fatalf("停用 zh-CN 词条失败：%v", err)
	}
	t.Cleanup(func() { _ = db.Exec("UPDATE sys_i18n SET status = 1 WHERE lang = 'zh-CN'").Error })
	if err := save("zh-CN"); err != nil {
		t.Fatalf("已存在的语言不该被词条数拦住，实际：%v", err)
	}
	t.Log("③ 已存在的 zh-CN（词条被停用）再保存 → 仍放行（只校新增/新启用）")

	// ④ 同时新增一个无词条语言 → 仍被拒。
	if err := save("zh-CN", "qq-ZZ"); !errors.Is(err, projectservice.ErrLocaleNoTranslations) {
		t.Fatalf("新增无词条语言仍应被拒绝，实际：%v", err)
	}
	t.Log("④ 新增 qq-ZZ（词条数 0）→ 被拒")
}
