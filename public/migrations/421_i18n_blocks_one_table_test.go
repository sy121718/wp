package migrations_test

import (
	"testing"

	"go_wp/public/migrations"
	"go_wp/public/test/support"
)

const blocksOneTableVersion = "421-i18n-blocks-one-table"

var blocksOneTableWants = map[string]map[string]string{
	"admin.blocks.impact.badgeTail": {
		"zh-CN": " 个页面有更新未发布",
		"en-US": " pages have unpublished changes",
	},

	"admin.blocks.impact.help": {
		"zh-CN": "块、内容、主题或导航改动过，产物的字节还停在旧版本。系统会在改动后自动重建这些页面；若长时间停在这里，说明重建失败或构建队列积压（原因已记入服务日志）。",
		"en-US": "Blocks, content, themes or navigation changed, but the published bytes still carry the old version. These pages are rebuilt automatically; if they stay here for long, the rebuild failed or the build queue is backed up (see the service log).",
	},
	"admin.blocks.list_empty": {
		"zh-CN": "还没有全局块。页眉 / 页脚块在「主题管理 → 设置」里绑定到主题，构建页面时编译期内联；区块用于跨页面复用的结构片段，工作台「全局块」页签可一键引用。",
		"en-US": "No global blocks yet. Bind header / footer blocks to a theme under Theme management → Settings so they are inlined into pages at build time; blocks are structure fragments reused across pages and can be inserted from the Global blocks tab in the workbench.",
	},
}

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

	if err := migrations.RunSeeds(db); err != nil {
		t.Fatalf("重放种子数据失败: %v", err)
	}
	assertApplied("重放后")
	if after := countRows(); after != before {
		t.Fatalf("重放后词条行数变了：%d → %d", before, after)
	}
}
