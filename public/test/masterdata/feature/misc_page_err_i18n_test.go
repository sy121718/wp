package feature

import (
	"testing"

	"go_wp/public/migrations"
)

var miscPageErrI18nKeys = []string{
	"admin.masterdata.loadFailed.lead",
	"admin.masterdata.loadFailed.title",
	"admin.masterdata.loadFailed.desc",
	"admin.analytics.no_project.loadFailed",
	"admin.content.templates.loadFailed.title",
	"admin.content.templates.loadFailed",
	"admin.content.templates.impact.loadFailed",
	"ErrAnalyticsInternal",
}

func TestMiscPageErrI18nSeededAndIdempotent(t *testing.T) {

	f := newMDFixture(t)
	if f == nil {
		return
	}

	langsOf := func(t *testing.T) map[string]map[string]bool {
		t.Helper()
		var rows []struct {
			ItemKey string `gorm:"column:item_key"`
			Lang    string `gorm:"column:lang"`
		}
		if err := f.db.Raw(
			"SELECT item_key, lang FROM sys_i18n WHERE item_key IN ?", miscPageErrI18nKeys,
		).Scan(&rows).Error; err != nil {
			t.Fatalf("查询 sys_i18n 失败（408 的 SQL 或门槛有问题）: %v", err)
		}
		out := make(map[string]map[string]bool, len(miscPageErrI18nKeys))
		for _, r := range rows {
			if out[r.ItemKey] == nil {
				out[r.ItemKey] = map[string]bool{}
			}
			out[r.ItemKey][r.Lang] = true
		}
		return out
	}

	assertSeeded := func(t *testing.T, when string) {
		t.Helper()
		langs := langsOf(t)
		for _, key := range miscPageErrI18nKeys {
			l := langs[key]
			if l == nil {
				t.Errorf("%s：词条 %q 没落库（页面上会回落中文兜底 / 裸 key）", when, key)
				continue
			}
			if !l["zh-CN"] || !l["en-US"] {
				t.Errorf("%s：词条 %q 语言不成对（zh-CN=%v en-US=%v）", when, key, l["zh-CN"], l["en-US"])
			}
		}
	}

	assertSeeded(t, "首次执行 RunSeeds 后")

	if err := migrations.RunSeeds(f.db); err != nil {
		t.Fatalf("重复执行种子失败（408 不幂等）: %v", err)
	}
	assertSeeded(t, "重复执行 RunSeeds 后")
}
