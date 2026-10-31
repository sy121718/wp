package migrations_test

import (
	"testing"

	"go_wp/public/migrations"
	"go_wp/public/test/support"
)

const controlSelectionVersion = "416-i18n-control-selection"

var controlSelectionWants = map[string]map[string]string{

	"admin.coupons.detail.ph.max_uses": {
		"zh-CN": "总次数上限（0 = 不限）",
		"en-US": "Total usage limit (0 = unlimited)",
	},
	"admin.coupons.detail.ph.per_user_limit": {
		"zh-CN": "每人限次（0 = 不限）",
		"en-US": "Per-user limit (0 = unlimited)",
	},
	"admin.coupons.detail.ph.remark": {
		"zh-CN": "备注",
		"en-US": "Note",
	},

	"admin.content.templates.entityGroupStructure": {
		"zh-CN": "结构模板",
		"en-US": "Structure templates",
	},
	"admin.content.templates.entityGroupContent": {
		"zh-CN": "内容实体",
		"en-US": "Content entities",
	},

	"admin.coupons.create.time_hint.lead": {
		"zh-CN": "时间窗用",
		"en-US": "The time window is set with ",
	},
	"admin.coupons.create.time_hint.strong": {
		"zh-CN": "日期时间选择器",
		"en-US": "the date-time picker",
	},
	"admin.coupons.create.time_hint.mid": {
		"zh-CN": "挑选起止时刻（按站点本地时间理解）：",
		"en-US": " (read as the site's local time): ",
	},
	"admin.coupons.create.time_hint.mid2": {
		"zh-CN": "留空 = 该侧不限。",
		"en-US": "Blank means no limit on that side. ",
	},
	"admin.coupons.create.time_hint.tail": {
		"zh-CN": "结束时间必须晚于开始时间，否则这张券永远不会生效 —— 服务端会直接拒绝。",
		"en-US": "The end must be later than the start, otherwise the coupon never takes effect — the server rejects it outright.",
	},

	"admin.coupons.ph.starts_at": {
		"zh-CN": "开始时间（留空 = 不限）",
		"en-US": "Start time (blank = no limit)",
	},
	"admin.coupons.ph.ends_at": {
		"zh-CN": "结束时间（留空 = 不限）",
		"en-US": "End time (blank = no limit)",
	},
}

func TestControlSelectionI18nSeedApplied(t *testing.T) {
	db := support.NewMigratedPGTestDB(t)
	if db == nil {
		return
	}
	if err := migrations.RunSeeds(db); err != nil {
		t.Fatalf("执行种子数据失败: %v", err)
	}

	for key, langs := range controlSelectionWants {
		for lang, want := range langs {
			var got string
			if err := db.Raw(
				"SELECT item_value FROM sys_i18n WHERE item_key = ? AND lang = ?", key, lang,
			).Scan(&got).Error; err != nil {
				t.Fatalf("读取词条 %s/%s 失败: %v", key, lang, err)
			}
			if got != want {
				t.Errorf("词条 %s/%s 的值不对：期望 %q，实际 %q"+
					"（修正类词条拿到旧值 = UPDATE 的旧值条件没命中，且不会报错）", key, lang, want, got)
			}
		}
	}
}

func TestControlSelectionI18nSeedIdempotent(t *testing.T) {
	db := support.NewMigratedPGTestDB(t)
	if db == nil {
		return
	}
	if err := migrations.RunSeeds(db); err != nil {
		t.Fatalf("执行种子数据失败: %v", err)
	}

	var target migrations.Seed
	for _, s := range migrations.AllSeeds() {
		if s.Version == controlSelectionVersion {
			target = s
			break
		}
	}
	if target.Version == "" {
		t.Fatalf("种子 %s 未注册（本文件自带的 init 没跑？）", controlSelectionVersion)
	}

	assertApplied := func(stage string) {
		t.Helper()
		var n int64

		if err := db.Raw(target.ConditionSQL).Scan(&n).Error; err != nil {
			t.Fatalf("%s：执行 ConditionSQL 失败: %v", stage, err)
		}
		if n != 1 {
			t.Fatalf("%s：%s 的判定应为「已应用」，实际 %d —— 判定写错会让它每次启动都重跑",
				stage, target.Version, n)
		}
	}
	assertApplied("首次")

	countRows := func() int64 {
		t.Helper()
		keys := make([]string, 0, len(controlSelectionWants))
		for k := range controlSelectionWants {
			keys = append(keys, k)
		}
		var n int64
		if err := db.Raw(
			"SELECT COUNT(*) FROM sys_i18n WHERE item_key IN ?", keys,
		).Scan(&n).Error; err != nil {
			t.Fatalf("统计词条行数失败: %v", err)
		}
		return n
	}
	before := countRows()

	if err := migrations.RunSeeds(db); err != nil {
		t.Fatalf("重放种子数据失败: %v", err)
	}
	assertApplied("重放后")
	if after := countRows(); after != before {
		t.Fatalf("重放后词条行数变了：%d → %d", before, after)
	}
}
