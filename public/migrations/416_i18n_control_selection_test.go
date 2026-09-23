package migrations_test

// 416_i18n_control_selection_test.go — 迁移 416 的落地结果与幂等。
//
// 为什么值得单独钉住：这条迁移的绝大部分是 **UPDATE ... WHERE item_value = '<旧值>'**（与 399 同形，
// 为了不覆盖运营改过的词条）。旧值字面量是从 190 的 seed 里抄过来的，抄错一个空格、一个标点，
// UPDATE 就命中 0 行 —— **不报错、不失败**，生产环境继续显示旧文案（时间窗那一处更是与换成
// 日期时间控件的表单直接矛盾）。所以这里断言的是**改完之后库里的实际值**，而不是「迁移跑过了」。
//
// 幂等一侧同样重要：种子台账每次启动都会走一遍判定，判定写错会让这条迁移每次都重跑。

import (
	"testing"

	"go_wp/public/migrations"
	"go_wp/public/test/support"
)

// controlSelectionVersion 416 的版本号（与 register_control_selection_i18n.go 一致）。
const controlSelectionVersion = "416-i18n-control-selection"

// controlSelectionWants 416 之后库里应有的值（key → lang → 期望值）。
//
// 一半是新增的词条，一半是**被修正的既有词条**（后者的期望值就是新文案；
// 旧文案留在库里就说明 UPDATE 没命中，这正是本用例要抓的静默失败）。
var controlSelectionWants = map[string]map[string]string{
	// —— 新增：优惠码编辑抽屉的三个字段标签 ——
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
	// —— 新增：内容模板实体类型下拉的分组标签 ——
	"admin.content.templates.entityGroupStructure": {
		"zh-CN": "结构模板",
		"en-US": "Structure templates",
	},
	"admin.content.templates.entityGroupContent": {
		"zh-CN": "内容实体",
		"en-US": "Content entities",
	},
	// —— 修正：时间窗说明（旧文案描述的是「手打格式」，控件换成日期时间选择器后它自相矛盾）——
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
	// —— 修正：时间窗字段标题去掉原生控件用不到的格式示例 ——
	"admin.coupons.ph.starts_at": {
		"zh-CN": "开始时间（留空 = 不限）",
		"en-US": "Start time (blank = no limit)",
	},
	"admin.coupons.ph.ends_at": {
		"zh-CN": "结束时间（留空 = 不限）",
		"en-US": "End time (blank = no limit)",
	},
}

// TestControlSelectionI18nSeedApplied 416 的每一条词条都按期望值落库。
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

// TestControlSelectionI18nSeedIdempotent 判定命中「已应用」，且整条 SQL 可重放。
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
		// 与迁移器同形（migrator.go 对种子判定就是 db.Raw(ConditionSQL)，不传参）：
		// 判定的 key 必须写死在 SQL 里，写成 ? 永远查不到行 —— 判定恒为 0、每次启动重跑。
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

	// 重放：判定命中就跳过，即使真的再跑一次，UPDATE 的旧值条件也已不匹配（0 行）。
	if err := migrations.RunSeeds(db); err != nil {
		t.Fatalf("重放种子数据失败: %v", err)
	}
	assertApplied("重放后")
	if after := countRows(); after != before {
		t.Fatalf("重放后词条行数变了：%d → %d", before, after)
	}
}
