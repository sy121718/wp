package sysconfigservice_test

// sysconfig_dict_test.go — 字典只读口（Y3 的供数来源）：条数与「启用」判据。
//
// 判据是**与库对齐**：datalist / option 的条数必须等于「已收录且启用」的语言数 ——
// 写死的清单会随字典演进静默过期（漏语言 / 显示已停用的语言）。

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
	// ui_available（界面译文是否已有）：目前只有 zh-CN / en-US 两行为 true，
	// 供 i18n 词条页的「新建词条」下拉做标记用。
	var uiCount int
	for _, o := range langs {
		if o.UIAvailable {
			uiCount++
		}
	}
	t.Logf("语言选项 %d 条（= 库内启用语言数 %d），其中标记「界面已有译文」%d 条；货币 %d 条；国家 %d 条",
		len(langs), enabledLangs, uiCount, len(currencies), len(countries))
}

// TestCountryLabelPicksLanguageAndFallsBack 国家代码 → 显示名。
//
// 四个判据都是「错了不会报错、只会显示成别的东西」的那类：
//
//	· 语言挑错列 → 英文界面显示中文名；
//	· 大小写不归一 → 同一个码有时显示 CN、有时显示中国；
//	· 缺行 / 空码不回落 → 地址里出现空段或占位符。
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

	// 缓存命中后的行为必须与首读一致（第二次读走的是缓存里的码索引，
	// 归一化与回落判据都要在缓存路径上同样成立）。
	if got := svc.CountryLabel(ctx, "zh-CN", "cn"); got != "中国" {
		t.Fatalf("缓存路径上的小写代码应同样能查到，实际 %q", got)
	}
	if got := svc.CountryLabel(ctx, "zh-CN", "QQ"); got != "QQ" {
		t.Fatalf("缓存路径上未知代码应回落显示代码，实际 %q", got)
	}

	// 缓存生效的**正向证据**：把库里的名字改掉，TTL 未到时再读应仍拿到旧值
	// （命中缓存、没有重新查库）。没有这条，「缓存真的在用」在本文件里就没有证据 ——
	// 而那正是运营最在意的事：每次渲染都回查库扛不住。
	if err := db.Exec("UPDATE sys_area SET name_zh = '被改掉的名字' WHERE code = 'CN'").Error; err != nil {
		t.Fatalf("改字面失败: %v", err)
	}
	if got := svc.CountryLabel(ctx, "zh-CN", "CN"); got != "中国" {
		t.Fatalf("TTL 未到时应命中缓存而不再查库，实际读到 %q（读到新值就说明缓存没生效）", got)
	}
}
