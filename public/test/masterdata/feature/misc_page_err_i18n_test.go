// misc_page_err_i18n_test.go — 迁移 408 的词条落地与幂等（三域页面降级渲染的文案来源）。
//
// 为什么值得一条真库用例：408 是本批三个页面降级渲染的**文案来源**，
// 而它的失败方式全是静默的 ——
//
//	· SQL 没注册 → 词条永远不落库，页面上回落中文兜底（看起来「能用」，英文界面全中文）；
//	· 列名 / 约束写错 → 只有真跑一次才知道（本用例把 RunSeeds 真正跑过作为前提）；
//	· 门槛（ConditionSQL）写错 → 每次启动重跑，或反过来「判定恒为真」导致永远跳过；
//	· 少了 en-US → 英文界面上那一句回落中文，是全批最难被发现的一半。
//
// 文件级门禁（TestEnumsKeysHaveSeedEntries / TestEmbeddedSQLFilesAllRegistered）只看文本，
// 抓不到「跑起来有没有落库」；本用例补的就是这一段。
package feature

import (
	"testing"

	"go_wp/public/migrations"
)

// miscPageErrI18nKeys 408 的 8 个 key（与 migrations/register_misc_page_err_i18n.go 的门槛同一份清单）。
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

// TestMiscPageErrI18nSeededAndIdempotent 408 的词条：中英成对、落库、且重复执行不产生变化。
func TestMiscPageErrI18nSeededAndIdempotent(t *testing.T) {
	// fixture 装配时已经跑过一次 RunSeeds —— 本用例因此天然带着「第一次执行」的证据，
	// 下面再跑第二次用来验证幂等（ConditionSQL 命中即跳过，词条数不变）。
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

	// 幂等：再跑一次种子，词条既不该报错也不该变化（改值会让「新库 / 老库」出现不可见差异）。
	if err := migrations.RunSeeds(f.db); err != nil {
		t.Fatalf("重复执行种子失败（408 不幂等）: %v", err)
	}
	assertSeeded(t, "重复执行 RunSeeds 后")
}
