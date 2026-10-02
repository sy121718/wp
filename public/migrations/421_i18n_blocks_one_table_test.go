package migrations_test

// 421_i18n_blocks_one_table_test.go — 迁移 421 的落地结果与幂等。
//
// 为什么值得单独钉住：这条 seed 的门槛是**逐条枚举本批 3 个 key 计数**（挑的是 en-US 行），
// 门槛写错有两种静默失败 —— 门槛太低（比如按 zh-CN 计数、而中文行与模板兜底同形，别处被顺手
// 加上）会让判定在「本批还没跑」时就成立，整批词条被静默跳过；门槛太高（多算或写错一个 key）
// 则让这条 seed 每次启动都重跑。两种都不会报错。
//
// 所以这里断言的是「跑完之后库里真的有什么」，而不是「迁移跑过了」。

import (
	"testing"

	"go_wp/public/migrations"
	"go_wp/public/test/support"
)

// blocksOneTableVersion 421 的版本号（与 register_blocks_one_table_i18n.go 一致）。
const blocksOneTableVersion = "421-i18n-blocks-one-table"

// blocksOneTableWants 421 之后库里应有的值（key → lang → 期望值）。
//
// 三条都是本批新增（blocks.html 合并成一张表 + 影响面降级）；
// 被取代的旧词条（headers_empty / footers_empty / blocks_empty）**不在本批退役** ——
// 它们由 192 seed，而 register_admin_i18n.go 的幂等条件逐条枚举这些 key，
// 只删词条会被那条 seed 每次启动插回来（AGENTS.md「删能力时要连 seed 的 SQL 与幂等条件一起收口」）。
var blocksOneTableWants = map[string]map[string]string{
	"admin.blocks.impact.badgeTail": {
		"zh-CN": " 个页面有更新未发布",
		"en-US": " pages have unpublished changes",
	},
	// `admin.blocks.impact.help` 的期望值是 **473 覆盖之后**的文案：473 把行为从
	// 「手动重建」改成「自动重建」（块/主题/组件版本三条 stale 来源都接上了自动重建），
	// 文案随之改写 —— 原文案在教用户去找一个不存在的重建按钮。
	// 这里跟的是 473 的最终值，不是 421 的初值：**覆盖迁移之后的真源是后者**，
	// 谁下次看到不一致，先查这条 key 有没有更晚的覆盖迁移，别当成漂移改回去。
	"admin.blocks.impact.help": {
		"zh-CN": "块、内容、主题或导航改动过，产物的字节还停在旧版本。系统会在改动后自动重建这些页面；若长时间停在这里，说明重建失败或构建队列积压（原因已记入服务日志）。",
		"en-US": "Blocks, content, themes or navigation changed, but the published bytes still carry the old version. These pages are rebuilt automatically; if they stay here for long, the rebuild failed or the build queue is backed up (see the service log).",
	},
	"admin.blocks.list_empty": {
		"zh-CN": "还没有全局块。页眉 / 页脚块在「主题管理 → 设置」里绑定到主题，构建页面时编译期内联；区块用于跨页面复用的结构片段，工作台「全局块」页签可一键引用。",
		"en-US": "No global blocks yet. Bind header / footer blocks to a theme under Theme management → Settings so they are inlined into pages at build time; blocks are structure fragments reused across pages and can be inserted from the Global blocks tab in the workbench.",
	},
}

// TestBlocksOneTableI18nSeedApplied 421 的每一条词条都按期望值落库。
func TestBlocksOneTableI18nSeedApplied(t *testing.T) {
	db := support.NewMigratedPGTestDB(t)
	if db == nil {
		return
	}
	if err := migrations.RunSeeds(db); err != nil {
		t.Fatalf("执行种子数据失败: %v", err)
	}

	for key, langs := range blocksOneTableWants {
		for lang, want := range langs {
			var got string
			if err := db.Raw(
				"SELECT item_value FROM sys_i18n WHERE item_key = ? AND lang = ?", key, lang,
			).Scan(&got).Error; err != nil {
				t.Fatalf("读取词条 %s/%s 失败: %v", key, lang, err)
			}
			if got != want {
				t.Errorf("词条 %s/%s 的值不对：期望 %q，实际 %q"+
					"（空值 = 本批 seed 没跑：门槛判定或 key 列表写错）", key, lang, want, got)
			}
		}
	}
}

// TestBlocksOneTableI18nSeedIdempotent 判定命中「已应用」，且整条 SQL 可重放。
func TestBlocksOneTableI18nSeedIdempotent(t *testing.T) {
	db := support.NewMigratedPGTestDB(t)
	if db == nil {
		return
	}
	if err := migrations.RunSeeds(db); err != nil {
		t.Fatalf("执行种子数据失败: %v", err)
	}

	var target migrations.Seed
	for _, s := range migrations.AllSeeds() {
		if s.Version == blocksOneTableVersion {
			target = s
			break
		}
	}
	if target.Version == "" {
		t.Fatalf("种子 %s 未注册（本文件自带的 init 没跑？）", blocksOneTableVersion)
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
		keys := make([]string, 0, len(blocksOneTableWants))
		for k := range blocksOneTableWants {
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

	// 重放：判定命中即跳过；即使真的再跑一次，INSERT ... ON CONFLICT DO NOTHING 也不产生新行。
	if err := migrations.RunSeeds(db); err != nil {
		t.Fatalf("重放种子数据失败: %v", err)
	}
	assertApplied("重放后")
	if after := countRows(); after != before {
		t.Fatalf("重放后词条行数变了：%d → %d", before, after)
	}
}
