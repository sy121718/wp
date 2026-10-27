package migrations

// register_presentation_publish_attempt.go — 671：发布尝试明细（发布历史）。
//
// 判据只查系统目录，不引用目标列（637/640/642/651 都踩过「列还没建时 SQL 解析阶段就
// 报 42703，把尚未完成误判成一次失败」）：表在 + 四条约束齐 + 两条索引在 + RLS 已开。

import "sync"

var registerPresentationPublishAttemptOnce sync.Once

func init() { registerPresentationPublishAttempt() }

func registerPresentationPublishAttempt() {
	registerPresentationPublishAttemptOnce.Do(func() {
		register(Migration{
			Version:   "671-presentation-publish-attempts",
			TableName: "presentation_publish_attempts",
			CheckSQL: `SELECT CASE WHEN (
				SELECT COUNT(*) FROM pg_class
				WHERE oid = to_regclass('presentation_publish_attempts') AND relkind = 'r'
			) = 1 AND (
				SELECT COUNT(*) FROM pg_constraint
				WHERE conrelid = to_regclass('presentation_publish_attempts')
				  AND conname IN ('ck_publish_attempts_outcome', 'ck_publish_attempts_trigger',
				                  'ck_publish_attempts_attempts', 'ck_publish_attempts_duration')
			) = 4 AND (
				SELECT COUNT(*) FROM pg_indexes
				WHERE tablename = 'presentation_publish_attempts'
				  AND indexname IN ('idx_publish_attempts_instance', 'idx_publish_attempts_project',
				                   'idx_publish_attempts_entity')
			) = 3 AND (
				SELECT COUNT(*) FROM pg_class
				WHERE oid = to_regclass('presentation_publish_attempts') AND relrowsecurity AND relforcerowsecurity
			) = 1 THEN 1 ELSE 0 END`,
			SQL: mustSQL("671_presentation_publish_attempts.sql"),
		})
	})
}
