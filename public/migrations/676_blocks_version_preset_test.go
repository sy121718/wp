package migrations_test

import (
	"testing"

	"go_wp/public/migrations"
	"go_wp/public/test/support"
)

const blocksVersionPresetVersion = "676-blocks-version-preset"

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

	var def string
	if err := db.Raw(`SELECT COALESCE(column_default, '') FROM information_schema.columns
		WHERE table_name = 'blocks' AND column_name = 'version'`).Scan(&def).Error; err != nil {
		t.Fatalf("查 version 默认值失败: %v", err)
	}
	if def != "1" {
		t.Errorf("blocks.version 默认值应为 1，实际 %q（0 会让并发校验永远通过）", def)
	}

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

func TestBlocksVersionPresetConstraintsEnforced(t *testing.T) {
	db := support.NewMigratedPGTestDB(t)

	var projectID string

	if err := db.Raw(`INSERT INTO projects (id, name, settings, create_time, update_time)
		VALUES (gen_random_uuid(), 'b2-676-约束工程', '{}'::jsonb, now(), now()) RETURNING id`).Scan(&projectID).Error; err != nil {
		t.Fatalf("建工程失败: %v", err)
	}

	for _, bad := range []int64{0, -1} {
		err := db.Exec(`INSERT INTO blocks (project_id, name, version) VALUES (?, ?, ?)`,
			projectID, "坏版本", bad).Error
		if err == nil {
			t.Errorf("version = %d 应被 ck_blocks_version_positive 拒绝", bad)
		}
	}

	if err := db.Exec(`INSERT INTO blocks (project_id, name, preset_key) VALUES (?, ?, ?)`,
		projectID, "只有 key", "commerce.cartCheckout").Error; err == nil {
		t.Error("只有 preset_key 没有 preset_version 应被 ck_blocks_preset_pair 拒绝")
	}
	if err := db.Exec(`INSERT INTO blocks (project_id, name, preset_version) VALUES (?, ?, ?)`,
		projectID, "只有 version", 1).Error; err == nil {
		t.Error("只有 preset_version 没有 preset_key 应被 ck_blocks_preset_pair 拒绝")
	}

	if err := db.Exec(`INSERT INTO blocks (project_id, name) VALUES (?, ?)`,
		projectID, "手工块").Error; err != nil {
		t.Errorf("手工块（无预设来源）应能插入: %v", err)
	}
	if err := db.Exec(`INSERT INTO blocks (project_id, name, preset_key, preset_version) VALUES (?, ?, ?, ?)`,
		projectID, "预设块", "commerce.cartCheckout", 1).Error; err != nil {
		t.Errorf("预设块（两列都有）应能插入: %v", err)
	}
}

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

	var version int64
	if err := db.Raw(`SELECT version FROM blocks WHERE name = ?`, "第七版").Scan(&version).Error; err != nil {
		t.Fatalf("查 version 失败: %v", err)
	}
	if version != 7 {
		t.Errorf("重跑迁移把 version 从 7 改成了 %d —— 迁移不得触碰业务数据", version)
	}

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
