package migrations_test

// 676_blocks_version_preset_test.go — 迁移 676（blocks 加并发版本与预设来源）的落地结果。
//
// 为什么值得单独钉住：B2 之后的一切都建立在这三列上 —— 保存要带 expectedVersion、
// 冲突要回 409、编译要锁定版本、预设来源要能追溯。如果列或约束没真正落地，
// 服务层写出来的 409 就是"永远不冲突"（expectedVersion 恒等于默认 1），
// 而这种失败不会报错，只在两个管理员同时编辑时表现为"我改的东西又回去了"。
//
// 断言的顺序刻意是「约束先于列」：只判列存在时，`version = 0` 的写入会静默成功，
// 而 0 会被下层的"0 表示不校验"约定当成漏洞。

import (
	"testing"

	"go_wp/public/migrations"
	"go_wp/public/test/support"
)

// blocksVersionPresetVersion 676 的版本号（与 register_blocks_version_preset.go 一致）。
const blocksVersionPresetVersion = "676-blocks-version-preset"

// TestBlocksVersionPresetColumnsLand 三列落地，且默认值是 1 而不是 0。
func TestBlocksVersionPresetColumnsLand(t *testing.T) {
	db := support.NewMigratedPGTestDB(t)

	for _, col := range []string{"version", "preset_key", "preset_version"} {
		var n int64
		if err := db.Raw(`SELECT COUNT(*) FROM pg_attribute
			WHERE attrelid = to_regclass('blocks') AND attname = ? AND NOT attisdropped`, col).
			Scan(&n).Error; err != nil {
			t.Fatalf("查列 %s 失败: %v", col, err)
		}
		if n != 1 {
			t.Errorf("blocks 缺列 %s", col)
		}
	}

	// 默认值必须是 1：0 会让「expectedVersion 不匹配才拒」退化成「永远匹配」。
	var def string
	if err := db.Raw(`SELECT COALESCE(column_default, '') FROM information_schema.columns
		WHERE table_name = 'blocks' AND column_name = 'version'`).Scan(&def).Error; err != nil {
		t.Fatalf("查 version 默认值失败: %v", err)
	}
	if def != "1" {
		t.Errorf("blocks.version 默认值应为 1，实际 %q（0 会让并发校验永远通过）", def)
	}

	// 预设来源两列必须可空：手工创建的块没有预设来源，用空串会让
	// 「来自哪个预设」与「不是来自预设」无法区分。
	for _, col := range []string{"preset_key", "preset_version"} {
		var nullable string
		if err := db.Raw(`SELECT is_nullable FROM information_schema.columns
			WHERE table_name = 'blocks' AND column_name = ?`, col).Scan(&nullable).Error; err != nil {
			t.Fatalf("查 %s 可空性失败: %v", col, err)
		}
		if nullable != "YES" {
			t.Errorf("blocks.%s 应可空（手工块没有预设来源），实际 %q", col, nullable)
		}
	}
}

// TestBlocksVersionPresetConstraintsEnforced 两条约束真的拦得住坏数据。
//
// 判据用**真实 INSERT** 而不是读 pg_constraint：约束存在但没生效（例如加了 NOT VALID
// 或者写成另一个表）时，读目录照样通过。
func TestBlocksVersionPresetConstraintsEnforced(t *testing.T) {
	db := support.NewMigratedPGTestDB(t)

	var projectID string
	// projects 表没有 status 列（实测 \d projects：id/name/settings/create_time/update_time
	// + 两个 retention 列），只插最小必填三列。
	if err := db.Raw(`INSERT INTO projects (id, name, settings, create_time, update_time)
		VALUES (gen_random_uuid(), 'b2-676-约束工程', '{}'::jsonb, now(), now()) RETURNING id`).Scan(&projectID).Error; err != nil {
		t.Fatalf("建工程失败: %v", err)
	}

	// ① version >= 1：0 与负数都必须被拒。
	for _, bad := range []int64{0, -1} {
		err := db.Exec(`INSERT INTO blocks (project_id, name, version) VALUES (?, ?, ?)`,
			projectID, "坏版本", bad).Error
		if err == nil {
			t.Errorf("version = %d 应被 ck_blocks_version_positive 拒绝", bad)
		}
	}

	// ② preset_key 与 preset_version 同时有或同时无。
	if err := db.Exec(`INSERT INTO blocks (project_id, name, preset_key) VALUES (?, ?, ?)`,
		projectID, "只有 key", "commerce.cartCheckout").Error; err == nil {
		t.Error("只有 preset_key 没有 preset_version 应被 ck_blocks_preset_pair 拒绝")
	}
	if err := db.Exec(`INSERT INTO blocks (project_id, name, preset_version) VALUES (?, ?, ?)`,
		projectID, "只有 version", 1).Error; err == nil {
		t.Error("只有 preset_version 没有 preset_key 应被 ck_blocks_preset_pair 拒绝")
	}

	// ③ 合法形态必须能写入：手工块（两个都 NULL）与预设块（两个都有）。
	if err := db.Exec(`INSERT INTO blocks (project_id, name) VALUES (?, ?)`,
		projectID, "手工块").Error; err != nil {
		t.Errorf("手工块（无预设来源）应能插入: %v", err)
	}
	if err := db.Exec(`INSERT INTO blocks (project_id, name, preset_key, preset_version) VALUES (?, ?, ?, ?)`,
		projectID, "预设块", "commerce.cartCheckout", 1).Error; err != nil {
		t.Errorf("预设块（两列都有）应能插入: %v", err)
	}
}

// TestBlocksVersionPresetIdempotent 重跑 676 不报错、不改数据。
//
// 幂等是迁移的硬要求（AGENTS.md §数据库）：ADD COLUMN 已存在、
// 约束已存在都必须成为空操作。这里跑第二遍然后断言行数与版本值没变 ——
// "重跑不报错"不够，它可能悄悄把 version 重置回 1。
func TestBlocksVersionPresetIdempotent(t *testing.T) {
	db := support.NewMigratedPGTestDB(t)

	var projectID string
	if err := db.Raw(`INSERT INTO projects (id, name, settings, create_time, update_time)
		VALUES (gen_random_uuid(), 'b2-676-幂等工程', '{}'::jsonb, now(), now()) RETURNING id`).Scan(&projectID).Error; err != nil {
		t.Fatalf("建工程失败: %v", err)
	}
	if err := db.Exec(`INSERT INTO blocks (project_id, name, version) VALUES (?, ?, 7)`,
		projectID, "第七版").Error; err != nil {
		t.Fatalf("建块失败: %v", err)
	}

	if err := migrations.Run(db); err != nil {
		t.Fatalf("重跑迁移失败: %v", err)
	}

	// version 是**业务数据**，重跑迁移不得重置它。
	var version int64
	if err := db.Raw(`SELECT version FROM blocks WHERE name = ?`, "第七版").Scan(&version).Error; err != nil {
		t.Fatalf("查 version 失败: %v", err)
	}
	if version != 7 {
		t.Errorf("重跑迁移把 version 从 7 改成了 %d —— 迁移不得触碰业务数据", version)
	}

	// 完成判据成立：这条迁移自己的 CheckSQL 在跑完之后必须为真。
	// 直接跑 CheckSQL 而不是查台账 —— 判据就是"列与约束齐备"，它才是真源；
	// 台账行存在但 CheckSQL 不成立，说明登记与事实不符（迁移器会重跑，但这里先红）。
	check := ""
	for _, m := range migrations.All() {
		if m.Version == blocksVersionPresetVersion {
			check = m.CheckSQL
			break
		}
	}
	if check == "" {
		t.Fatalf("登记表里找不到 %s", blocksVersionPresetVersion)
	}
	var done int
	if err := db.Raw(check).Scan(&done).Error; err != nil {
		t.Fatalf("CheckSQL 执行失败: %v", err)
	}
	if done != 1 {
		t.Errorf("%s 的 CheckSQL 在列与约束齐备后应为 1，实际 %d", blocksVersionPresetVersion, done)
	}
}
