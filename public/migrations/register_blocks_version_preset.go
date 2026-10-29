package migrations

// register_blocks_version_preset.go — 676：blocks 加并发版本与预设来源（业务区块解耦 B2）。
//
// 判据只查系统目录（pg_attribute / pg_constraint），**不显式引用 version / preset_key
// 这两列本身**：迁移器先跑 CheckSQL 再决定是否执行，而 PG 在解析阶段就会对不存在的列
// 报错 —— 那会把「尚未完成」误判成一次失败（637/640/642/651/652 都踩过这个坑）。
//
// 为什么三样都要判：只判 version 会让「列已加、约束没加」的半成品库被静默跳过，
// 而那条约束恰好挡住 version = 0（见 676 文件头：0 会被当成"不校验"的漏洞）。

import "sync"

var registerBlocksVersionPresetOnce sync.Once

func init() { registerBlocksVersionPreset() }

func registerBlocksVersionPreset() {
	registerBlocksVersionPresetOnce.Do(func() {
		register(Migration{
			Version:   "676-blocks-version-preset",
			TableName: "blocks",
			// 三列齐 + 两条约束在。
			CheckSQL: `SELECT CASE WHEN (
				SELECT COUNT(*) FROM pg_attribute
				WHERE attrelid = to_regclass('blocks')
				  AND attname IN ('version', 'preset_key', 'preset_version')
				  AND NOT attisdropped
			) = 3 AND (
				SELECT COUNT(*) FROM pg_constraint
				WHERE conrelid = to_regclass('blocks')
				  AND conname IN ('ck_blocks_version_positive', 'ck_blocks_preset_pair')
			) = 2 THEN 1 ELSE 0 END`,
			SQL: mustSQL("676_blocks_version_preset.sql"),
		})
	})
}
