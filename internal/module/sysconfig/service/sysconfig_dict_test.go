package sysconfigservice_test

import (
	"context"
	"testing"

	sysconfigmodel "go_wp/internal/module/sysconfig/model"
	sysconfigservice "go_wp/internal/module/sysconfig/service"
	"go_wp/public/migrations"
	"go_wp/public/test/support"
)

func TestDictOptionsMatchEnabledRows(t *testing.T) {
	db := support.NewMigratedPGTestDB(t)
	if db == nil {
		t.Skip("本地 PostgreSQL 不可用，跳过")
	}
	if err := migrations.RunSeeds(db); err != nil {
		t.Fatalf("执行种子数据失败: %v", err)
	}
	svc := sysconfigservice.NewService(sysconfigmodel.NewSysConfigModel(db), nil)
	ctx := context.Background()

	langs, err := svc.ListDictOptions(ctx, "language")
	if err != nil {
		t.Fatalf("读语言字典失败: %v", err)
	}
	var enabledLangs int64
	if err := db.Raw("SELECT count(*) FROM sys_dict WHERE type='language' AND enabled").Scan(&enabledLangs).Error; err != nil {
		t.Fatalf("统计启用语言失败: %v", err)
	}
	if int64(len(langs)) != enabledLangs {
		t.Fatalf("语言选项数 %d 与库内启用语言数 %d 不一致", len(langs), enabledLangs)
	}

	currencies, err := svc.ListDictOptions(ctx, "currency")
	if err != nil {
		t.Fatalf("读货币字典失败: %v", err)
	}
	countries, err := svc.ListCountryOptions(ctx, "zh-CN")
	if err != nil {
		t.Fatalf("读国家字典失败: %v", err)
	}

	var uiCount int
	for _, o := range langs {
		if o.UIAvailable {
			uiCount++
		}
	}
	t.Logf("语言选项 %d 条（= 库内启用语言数 %d），其中标记「界面已有译文」%d 条；货币 %d 条；国家 %d 条",
		len(langs), enabledLangs, uiCount, len(currencies), len(countries))
}

func TestCountryLabelPicksLanguageAndFallsBack(t *testing.T) {
	db := support.NewMigratedPGTestDB(t)
	if db == nil {
		t.Skip("本地 PostgreSQL 不可用，跳过")
	}
	if err := migrations.RunSeeds(db); err != nil {
		t.Fatalf("执行种子数据失败: %v", err)
	}
	svc := sysconfigservice.NewService(sysconfigmodel.NewSysConfigModel(db), nil)
	ctx := context.Background()

	tests := []struct {
		name string
		lang string
		code string
		want string
	}{
		{name: "中文界面取中文名", lang: "zh-CN", code: "CN", want: "中国"},
		{name: "英文界面取英文名", lang: "en-US", code: "CN", want: "China"},
		{name: "中文方言（zh-Hant）也算中文界面", lang: "zh-Hant", code: "US", want: "美国"},
		{name: "非中文一律英文列", lang: "ja-JP", code: "US", want: "United States"},
		{name: "小写代码同样能查到（大小写不归一会静默查不到）", lang: "zh-CN", code: "cn", want: "中国"},
		{name: "两侧空白去掉", lang: "zh-CN", code: " CN ", want: "中国"},
		{name: "字典里没有的码回落显示代码", lang: "zh-CN", code: "ZZ", want: "ZZ"},
		{name: "空码返回空串（不是占位符）", lang: "zh-CN", code: "   ", want: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := svc.CountryLabel(ctx, tt.lang, tt.code); got != tt.want {
				t.Fatalf("CountryLabel(%q, %q) = %q，期望 %q", tt.lang, tt.code, got, tt.want)
			}
		})
	}

	if got := svc.CountryLabel(ctx, "zh-CN", "cn"); got != "中国" {
		t.Fatalf("缓存路径上的小写代码应同样能查到，实际 %q", got)
	}
	if got := svc.CountryLabel(ctx, "zh-CN", "QQ"); got != "QQ" {
		t.Fatalf("缓存路径上未知代码应回落显示代码，实际 %q", got)
	}

	if err := db.Exec("UPDATE sys_area SET name_zh = '被改掉的名字' WHERE code = 'CN'").Error; err != nil {
		t.Fatalf("改字面失败: %v", err)
	}
	if got := svc.CountryLabel(ctx, "zh-CN", "CN"); got != "中国" {
		t.Fatalf("TTL 未到时应命中缓存而不再查库，实际读到 %q（读到新值就说明缓存没生效）", got)
	}
}
